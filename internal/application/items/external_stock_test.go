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
	return nil
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
}

// Inspect 返回预设状态并计数。
func (platform *externalStockPlatformFake) Inspect(context.Context, int64, string, string) (ListingSnapshot, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.inspects++
	return platform.listing, platform.inspectErr
}

// Unpublish 记录下架。
func (platform *externalStockPlatformFake) Unpublish(context.Context, int64, string, string) error {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.unpublishes++
	if platform.actionErr == nil {
		platform.listing.State = ListingOffSale
	}
	return platform.actionErr
}

// Relist 记录重新上架。
func (platform *externalStockPlatformFake) Relist(context.Context, int64, string, string, int, bool) error {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.relists++
	return platform.actionErr
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

// TestExternalStockDoesNotRelistAtZeroAndStopsOnRisk 验证库存恢复才上架，风控时停止并退避。
func TestExternalStockDoesNotRelistAtZeroAndStopsOnRisk(t *testing.T) {
	// service、repo、platform、_ 是已下架且库存为 0 的夹具。
	service, repo, _, platform, _ := newExternalStockFixture(t, 0, ListingSnapshot{State: ListingOffSale, Quantity: 0, QuantityKnown: true})
	// idle 是库存为 0 时的结果。
	idle, err := service.Check(context.Background(), 7, "acc", "item", ExternalStockCheckOptions{Trigger: "schedule"})
	if err != nil || idle.Action != ExternalStockActionNone || platform.relists != 0 || platform.unpublishes != 0 {
		t.Fatalf("零库存不应上架 result=%+v err=%v", idle, err)
	}
	repo.mu.Lock()
	repo.config.LastChecked = time.Time{}
	repo.mu.Unlock()
	service.mu.Lock()
	delete(service.recent, "acc\x00item")
	service.mu.Unlock()
	platform.listing = ListingSnapshot{State: ListingOffSale, Quantity: 0, QuantityKnown: true}
	platform.actionErr = ErrExternalStockRisk
	// reader 通过夹具字段改为有库存。需要拿到 reader。
	_ = repo
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
