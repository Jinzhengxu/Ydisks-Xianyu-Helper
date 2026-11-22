package items

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// externalStockRepoFake 是外部库存仓储替身，保存一份配置和请求头明文。
type externalStockRepoFake struct {
	// mu 保护配置和请求头。
	mu sync.Mutex
	// config 是当前配置。
	config ExternalStockConfig
	// headers 是请求头明文，测试用它确认日志没有泄漏。
	headers map[string]string
	// owned 表示账号属于用户。
	owned bool
	// exists 表示商品存在。
	exists bool
	// userID 是账号所属用户。
	userID int64
	// localStatus、localPresent 是商品同步记录里的 item_status。
	localStatus  int
	localPresent bool
}

// AccountOwned 返回预设归属。
func (repository *externalStockRepoFake) AccountOwned(context.Context, int64, string) (bool, error) {
	return repository.owned, nil
}

// ItemExists 返回预设商品存在性。
func (repository *externalStockRepoFake) ItemExists(context.Context, string, string) (bool, error) {
	return repository.exists, nil
}

// AccountUserID 返回预设用户。
func (repository *externalStockRepoFake) AccountUserID(context.Context, string) (int64, error) {
	return repository.userID, nil
}

// Enabled 返回配置开关。
func (repository *externalStockRepoFake) Enabled(context.Context, string, string) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.config.Enabled, nil
}

// Get 返回配置副本。
func (repository *externalStockRepoFake) Get(context.Context, string, string) (ExternalStockConfig, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.config, nil
}

// Headers 返回请求头副本。
func (repository *externalStockRepoFake) Headers(context.Context, string, string) (map[string]string, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	// copied 避免测试和被测代码共享映射。
	copied := map[string]string{}
	// name、value 是一条请求头。
	for name, value := range repository.headers {
		copied[name] = value
	}
	return copied, nil
}

// Save 保存配置，不保存调用方未提供的额外秘密。
func (repository *externalStockRepoFake) Save(_ context.Context, config ExternalStockConfig, headers map[string]string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.config = config
	repository.headers = headers
	return nil
}

// SaveStatus 只更新状态字段。
func (repository *externalStockRepoFake) SaveStatus(_ context.Context, _, _ string, config ExternalStockConfig) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.config.LastChecked = config.LastChecked
	repository.config.LastAvailable = config.LastAvailable
	repository.config.LastAction = config.LastAction
	repository.config.LastStatus = config.LastStatus
	repository.config.LastError = config.LastError
	repository.config.BackoffUntil = config.BackoffUntil
	repository.config.BackoffClass = config.BackoffClass
	repository.config.AppliedAction = config.AppliedAction
	repository.config.AppliedAvailable = config.AppliedAvailable
	repository.config.AppliedAt = config.AppliedAt
	return nil
}

// LocalItemStatus 返回预设的本地同步状态。
func (repository *externalStockRepoFake) LocalItemStatus(context.Context, string, string) (int, bool, error) {
	return repository.localStatus, repository.localPresent, nil
}

// ListEnabled 返回启用中的配置。
func (repository *externalStockRepoFake) ListEnabled(context.Context) ([]ExternalStockConfig, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !repository.config.Enabled {
		return nil, nil
	}
	return []ExternalStockConfig{repository.config}, nil
}

// externalStockReaderFake 返回固定库存或错误。
type externalStockReaderFake struct {
	// available 是外部可用库存。
	available int
	// err 是读取失败。
	err error
	// calls 是读取次数。
	calls int
}

// ReadAvailable 返回预设库存。
func (reader *externalStockReaderFake) ReadAvailable(context.Context, ExternalStockRead) (int, error) {
	reader.calls++
	if reader.err != nil {
		return 0, reader.err
	}
	return reader.available, nil
}

// externalStockPlatformFake 记录平台动作次数和当前在售状态。
type externalStockPlatformFake struct {
	// mu 保护计数和状态。
	mu sync.Mutex
	// listing 是 Inspect 返回的状态。
	listing ListingSnapshot
	// inspectErr 是读取状态的失败。
	inspectErr error
	// actionErr 是写操作失败。
	actionErr error
	// inspects、unpublishes、relists、syncs 分别是各类平台调用次数。
	inspects, unpublishes, relists, syncs int
	// diagnostic 是成功回执里的脱敏说明。
	diagnostic string
	// noop 表示平台报告商品已经处于目标状态。
	noop bool
}

// Inspect 返回预设状态并计数。
func (platform *externalStockPlatformFake) Inspect(context.Context, int64, string, string) (ListingSnapshot, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.inspects++
	return platform.listing, platform.inspectErr
}

// Unpublish 记录下架。
func (platform *externalStockPlatformFake) Unpublish(context.Context, int64, string, string) (ExternalStockApplyResult, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.unpublishes++
	if platform.actionErr != nil {
		return ExternalStockApplyResult{}, platform.actionErr
	}
	platform.listing.State = ListingOffSale
	return ExternalStockApplyResult{Diagnostic: platform.diagnostic, ListingState: ListingOffSale, Noop: platform.noop}, nil
}

// Relist 记录重新上架。
func (platform *externalStockPlatformFake) Relist(context.Context, int64, string, string) (ExternalStockApplyResult, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.relists++
	if platform.actionErr != nil {
		return ExternalStockApplyResult{}, platform.actionErr
	}
	return ExternalStockApplyResult{Diagnostic: platform.diagnostic, ListingState: platform.listing.State, Noop: platform.noop}, nil
}

// SyncStock 记录改库存。
func (platform *externalStockPlatformFake) SyncStock(context.Context, int64, string, string, int) error {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.syncs++
	return platform.actionErr
}

// newExternalStockFixture 创建启用中的服务、日志缓冲和替身。
func newExternalStockFixture(t *testing.T, available int, listing ListingSnapshot) (*ExternalStockService, *externalStockRepoFake, *externalStockReaderFake, *externalStockPlatformFake, *strings.Builder) {
	t.Helper()
	// repo 保存一条启用配置和秘密请求头。
	repo := &externalStockRepoFake{
		owned: true, exists: true, userID: 7,
		config: ExternalStockConfig{
			CookieID: "acc", ItemID: "item", Enabled: true, SourceURL: "https://stock.example/available", Method: "GET",
			JSONPath: "available", Timeout: ExternalStockDefaultTimeout, Cron: ExternalStockDefaultCron,
			Timezone: ExternalStockDefaultTimezone, Interval: ExternalStockDefaultInterval, StockCap: 1,
		},
		headers: map[string]string{"Authorization": "Bearer super-secret-token"},
	}
	// reader、platform 是外部库存和闲鱼替身。
	reader := &externalStockReaderFake{available: available}
	// platform 是闲鱼上下架替身。
	platform := &externalStockPlatformFake{listing: listing}
	// logs 收集日志，用来确认秘密没有输出。
	logs := &strings.Builder{}
	// service 是被测服务，账号间隔缩到最小以免测试等待。
	service, err := NewExternalStockService(repo, reader, platform, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	service.accountGap = time.Nanosecond
	return service, repo, reader, platform, logs
}

// TestExternalStockCheckUnpublishesAndSkipsAutoRelistWindow 验证库存为 0 时下架，演练不写平台，重叠触发只读一次。
func TestExternalStockCheckUnpublishesAndSkipsAutoRelistWindow(t *testing.T) {
	// service、repo、reader、platform、logs 是售罄且仍在售的检查夹具。
	service, repo, reader, platform, logs := newExternalStockFixture(t, 0, ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true})
	// first 是第一次检查结果。
	first, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil || first.Action != ExternalStockActionUnpublish || first.Status != "ok" || platform.unpublishes != 1 || platform.relists != 0 {
		t.Fatalf("result=%+v err=%v unpublish=%d relist=%d", first, err, platform.unpublishes, platform.relists)
	}
	// second 与第一次重叠窗口内的调度检查，应复用结果而不再下架。
	second, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "schedule"})
	if err != nil || second.Action != ExternalStockActionUnpublish || platform.unpublishes != 1 || reader.calls != 1 {
		t.Fatalf("重叠触发应合并 result=%+v calls=%d unpublish=%d err=%v", second, reader.calls, platform.unpublishes, err)
	}
	if strings.Contains(logs.String(), "super-secret-token") {
		t.Fatal("日志包含请求头秘密")
	}
	// dry 是强制演练，平台已下架，不应再写。
	platform.listing = ListingSnapshot{State: ListingOffSale, Quantity: 0, QuantityKnown: true}
	repo.mu.Lock()
	repo.config.DryRun = true
	repo.mu.Unlock()
	// dryResult 是演练结果。
	dryResult, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", ForceDryRun: true, BypassBackoff: true})
	if err != nil || !dryResult.DryRun || platform.relists != 0 {
		t.Fatalf("演练不应上架 result=%+v err=%v relist=%d", dryResult, err, platform.relists)
	}
}

// TestExternalStockDoesNotRelistAtZeroAndStopsOnRisk 验证库存为 0 时只下架，恢复后上架遇风控则停止并退避。
func TestExternalStockDoesNotRelistAtZeroAndStopsOnRisk(t *testing.T) {
	// service、repo、platform、_ 是库存为 0 的夹具，不提供在售状态。
	service, repo, _, platform, _ := newExternalStockFixture(t, 0, ListingSnapshot{})
	// idle 是库存为 0 时的结果。
	idle, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "schedule"})
	if err != nil || idle.Action != ExternalStockActionUnpublish || platform.relists != 0 || platform.unpublishes != 1 {
		t.Fatalf("零库存应下架且不上架 result=%+v err=%v", idle, err)
	}
	repo.mu.Lock()
	repo.config.LastChecked = time.Time{}
	repo.mu.Unlock()
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	platform.actionErr = ErrExternalStockRisk
	// restored 改用新的读取器。
	service.reader = &externalStockReaderFake{available: 1}
	// risky 是上架被风控拒绝的结果。
	risky, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", BypassBackoff: true})
	if err != nil || risky.Status != "backed_off" || platform.relists != 1 || !strings.Contains(risky.Message, "安全验证") {
		t.Fatalf("风控应停止 result=%+v err=%v relist=%d", risky, err, platform.relists)
	}
	repo.mu.Lock()
	// class 是写入的退避类别。
	class := repo.config.BackoffClass
	repo.mu.Unlock()
	if class != ExternalStockBackoffAll {
		t.Fatalf("退避类别=%s", class)
	}
}

// TestExternalStockFetchFailureDoesNotTouchPlatform 验证外部接口失败时不读取也不修改闲鱼商品。
func TestExternalStockFetchFailureDoesNotTouchPlatform(t *testing.T) {
	// service、_、reader、platform、_ 是外部接口返回 500 的夹具。
	service, _, reader, platform, _ := newExternalStockFixture(t, 0, ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true})
	reader.err = errors.Join(ErrExternalStockFetch, errors.New("HTTP 503"))
	// result 是失败结果。
	result, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil || result.Status != "backed_off" || platform.inspects != 0 || platform.unpublishes != 0 {
		t.Fatalf("result=%+v err=%v inspect=%d", result, err, platform.inspects)
	}
}

// TestExternalStockRelistsWithoutListingState 验证未知在售状态不会停止，平台回执里的状态码会写进说明。
func TestExternalStockRelistsWithoutListingState(t *testing.T) {
	// service、repo、_、platform、_ 是外部库存为 1、平台回执带真机在售码的夹具。
	service, repo, _, platform, _ := newExternalStockFixture(t, 1, ListingSnapshot{State: ListingUnknown})
	platform.diagnostic = "editDetail itemStatus=1；已在售，未提交编辑"
	platform.noop = true
	// live 是不先确认在售状态的正式检查。
	live, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", BypassBackoff: true})
	if err != nil || live.Action != ExternalStockActionRelist || live.Status != "ok" || live.Status == "backed_off" || platform.relists != 1 || platform.inspects != 0 || !strings.Contains(live.Message, "itemStatus=1") {
		t.Fatalf("未知状态仍应上架: %+v err=%v inspect=%d", live, err, platform.inspects)
	}
	repo.mu.Lock()
	// applied 是成功后记住的目标状态。
	applied := repo.config.AppliedAction
	repo.mu.Unlock()
	if applied != ExternalStockActionRelist {
		t.Fatalf("应记住成功的上架 applied=%s", applied)
	}
}

// TestExternalStockSkipsRepeatUntilChangeCronOrManual 验证同一天相同库存跳过平台，变化、cron 和手动检查会再执行。
func TestExternalStockSkipsRepeatUntilChangeCronOrManual(t *testing.T) {
	// service、repo、reader、platform、_ 是已经成功上架过的夹具。
	service, repo, reader, platform, _ := newExternalStockFixture(t, 1, ListingSnapshot{})
	platform.diagnostic = "editDetail itemStatus=1；已在售，未提交编辑"
	// first 是第一次上架。
	first, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil || first.Action != ExternalStockActionRelist || platform.relists != 1 {
		t.Fatalf("首次应上架: %+v err=%v", first, err)
	}
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// repeated 是发货后的重复检查，库存没变。
	repeated, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil || repeated.Action != ExternalStockActionNone || platform.relists != 1 || !strings.Contains(repeated.Message, "已跳过") {
		t.Fatalf("相同库存应跳过: %+v err=%v relist=%d", repeated, err, platform.relists)
	}
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	reader.available = 0
	// emptied 是可用数变为 0 后的下架。
	emptied, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil || emptied.Action != ExternalStockActionUnpublish || platform.unpublishes != 1 || platform.relists != 1 {
		t.Fatalf("库存变 0 应下架: %+v err=%v", emptied, err)
	}
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// cron 即使库存相同也再执行一次。
	cronned, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "schedule", CronMatch: true})
	if err != nil || cronned.Action != ExternalStockActionUnpublish || platform.unpublishes != 2 {
		t.Fatalf("cron 应重新下架: %+v err=%v unpublish=%d", cronned, err, platform.unpublishes)
	}
	repo.mu.Lock()
	if repo.config.AppliedAction != ExternalStockActionUnpublish || repo.config.AppliedAvailable == nil || *repo.config.AppliedAvailable != 0 {
		t.Fatalf("成功状态未记住: %+v", repo.config)
	}
	repo.mu.Unlock()
}

// TestExternalStockExecuteOnceLeavesSavedDryRun 验证手动执行一次会真实上架，但不关闭已保存演练；调度和发货仍不写平台。
func TestExternalStockExecuteOnceLeavesSavedDryRun(t *testing.T) {
	// service、repo、_、platform、_ 是已保存演练开关的夹具。
	service, repo, _, platform, _ := newExternalStockFixture(t, 1, ListingSnapshot{})
	repo.mu.Lock()
	repo.config.DryRun = true
	repo.mu.Unlock()
	// saved 是只传 dry_run=false 的手动检查，已保存演练仍然生效。
	saved, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", BypassBackoff: true})
	if err != nil || !saved.DryRun || saved.Status != "dry_run" || platform.relists != 0 {
		t.Fatalf("已保存演练不应被普通手动检查关闭: %+v err=%v relist=%d", saved, err, platform.relists)
	}
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// once 是明确要求执行一次的手动检查。
	once, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", ExecuteOnce: true, BypassBackoff: true})
	if err != nil || once.DryRun || once.Status != "ok" || once.Action != ExternalStockActionRelist || platform.relists != 1 {
		t.Fatalf("执行一次应真实上架: %+v err=%v relist=%d", once, err, platform.relists)
	}
	repo.mu.Lock()
	if repo.config.DryRun != true {
		t.Fatal("执行一次不应关闭已保存的演练开关")
	}
	// 清掉成功记录，让后续调度确实走到演练分支，而不是因为同一天相同库存被跳过。
	repo.config.AppliedAction = ""
	repo.config.AppliedAvailable = nil
	repo.config.AppliedAt = time.Time{}
	repo.mu.Unlock()
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// scheduled 是周期检查，即使误带执行一次标记也不能写平台。
	scheduled, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "schedule", ExecuteOnce: true})
	if err != nil || !scheduled.DryRun || scheduled.Status != "dry_run" || platform.relists != 1 {
		t.Fatalf("调度应保持演练: %+v err=%v relist=%d", scheduled, err, platform.relists)
	}
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// delivered 是付款发货后的检查，必须继续演练。
	delivered, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "delivery", ExecuteOnce: true})
	if err != nil || !delivered.DryRun || platform.relists != 1 || platform.unpublishes != 0 {
		t.Fatalf("发货后检查应保持演练: %+v err=%v", delivered, err)
	}
	platform.actionErr = ErrExternalStockRisk
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	// failed 是执行一次遇到风控，只请求一次平台。
	failed, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", ExecuteOnce: true, BypassBackoff: true})
	if err != nil || failed.Status != "backed_off" || platform.relists != 2 {
		t.Fatalf("失败只能尝试一次: %+v err=%v relist=%d", failed, err, platform.relists)
	}
	repo.mu.Lock()
	if !repo.config.DryRun {
		t.Fatal("失败后演练开关仍应保持开启")
	}
	repo.mu.Unlock()
}

// TestBackedOffExternalStockDoesNotDelayDelivery 验证退避中的库存检查在发货回调里立即返回，并且不读取平台。
func TestBackedOffExternalStockDoesNotDelayDelivery(t *testing.T) {
	// service、repo、reader、platform、_ 是已经退避的夹具。
	service, repo, reader, platform, _ := newExternalStockFixture(t, 1, ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true, RawStatus: "1"})
	repo.mu.Lock()
	repo.config.BackoffClass = ExternalStockBackoffAll
	repo.config.BackoffUntil = time.Now().Add(time.Hour)
	repo.mu.Unlock()
	// started 是回调返回前的时间，用来确认没有等待平台。
	started := time.Now()
	service.AfterPaidDelivery("acc", "item", "order-1")
	if time.Since(started) > time.Second {
		t.Fatal("发货后的库存检查不应阻塞发货回调")
	}
	// deadline 是等待后台检查退出的时刻。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		// slots 是仍在运行的发货后检查数量。
		slots := service.deliverySlots
		service.mu.Unlock()
		if slots == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reader.calls != 0 || platform.inspects != 0 {
		t.Fatalf("退避中的检查不应访问外部库存或平台 reads=%d inspects=%d", reader.calls, platform.inspects)
	}
}

// TestExternalStockDisabledIsUntouched 验证未启用的商品不会被检查。
func TestExternalStockDisabledIsUntouched(t *testing.T) {
	// service、repo、_、platform、_ 是关闭开关的夹具。
	service, repo, _, platform, _ := newExternalStockFixture(t, 0, ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true})
	repo.mu.Lock()
	repo.config.Enabled = false
	repo.mu.Unlock()
	// resultErr 应是未启用。
	_, resultErr := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "manual", BypassBackoff: true})
	if !errors.Is(resultErr, ErrExternalStockDisabled) || platform.inspects != 0 {
		t.Fatalf("err=%v inspect=%d", resultErr, platform.inspects)
	}
}
