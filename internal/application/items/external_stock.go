package items

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ExternalStockHeader 是一条自定义请求头。保存时 Value 为空且 Keep 为真表示沿用已加密的旧值。
type ExternalStockHeader struct {
	// Name 是请求头名称，不允许 Host 等由客户端控制的传输头。
	Name string
	// Value 是明文值，只存在于保存请求和出站调用的内存中，不能写入日志或 HTTP 响应。
	Value string
	// Keep 表示本次没有提供新值，应保留已保存的秘密值。
	Keep bool
}

// ExternalStockConfig 是商品级外部库存配置的非秘密视图。
type ExternalStockConfig struct {
	// CookieID 是商品所属账号。
	CookieID string
	// ItemID 是闲鱼原商品 ID，重新上架时继续使用它。
	ItemID string
	// Enabled 表示该商品参与外部库存同步；关闭时调度、发货和手动检查都不访问平台。
	Enabled bool
	// SourceURL 是返回可用库存的 HTTP(S) 地址。
	SourceURL string
	// Method 固定为 GET。
	Method string
	// JSONPath 是可用库存的点分路径，例如 available 或 data.available。
	JSONPath string
	// Timeout 是单次外部请求的上限。
	Timeout time.Duration
	// UserAgent 为空时使用桌面浏览器默认值。
	UserAgent string
	// Cron 是五段调度表达式，按 Timezone 解释。
	Cron string
	// Timezone 是 IANA 时区名。
	Timezone string
	// Interval 是额外的周期检查；0 表示只按 Cron。
	Interval time.Duration
	// DryRun 表示自动触发只记录将要执行的动作。
	DryRun bool
	// SyncPlatformStock 表示上架或已在售时把平台库存改成外部可用数（不超过 StockCap）。
	SyncPlatformStock bool
	// StockCap 是同步到平台的库存上限。
	StockCap int
	// HeaderNames 是已配置请求头的名称，不包含值。
	HeaderNames []string
	// LastChecked 是上次检查完成时间，零值表示还没检查过。
	LastChecked time.Time
	// LastAvailable 是上次成功读到的可用库存；nil 表示没有成功读数。
	LastAvailable *int
	// LastAction 是上次决策动作。
	LastAction string
	// LastStatus 是 ok、dry_run、error 或 backed_off。
	LastStatus string
	// LastError 是上次可展示的失败说明，不含请求头和响应正文。
	LastError string
	// BackoffUntil 是平台动作恢复前的时刻。
	BackoffUntil time.Time
	// BackoffClass 是 all 或 relist。
	BackoffClass string
	// AppliedAction 是最近一次真正成功的下架或重新上架。演练和跳过不更新它。
	AppliedAction string
	// AppliedAvailable 是那次成功时的外部可用库存。
	AppliedAvailable *int
	// AppliedAt 是那次成功的时间，用来判断是否仍在同一个配置日。
	AppliedAt time.Time
}

// ExternalStockCheckOptions 描述一次检查的触发来源。
type ExternalStockCheckOptions struct {
	// Trigger 是 manual、schedule 或 delivery。
	Trigger string
	// CronMatch 表示这次调度正好命中 cron，相同库存也要重新执行一次。
	CronMatch bool
	// ForceDryRun 让这一次只演练，不改变已保存的演练开关。它不能关闭已保存的演练。
	ForceDryRun bool
	// ExecuteOnce 只在手动检查时生效，允许这一次真实下架或重新上架，不改已保存的演练开关。
	ExecuteOnce bool
	// BypassBackoff 允许手动检查越过退避；失败后仍会重新退避。
	BypassBackoff bool
}

// ExternalStockCheckResult 是一次检查的非秘密结果。
type ExternalStockCheckResult struct {
	// Action 是实际决策；演练时仍使用原动作名，并由 DryRun 标明没有写平台。
	Action string
	// Status 是 ok、dry_run、error 或 backed_off。
	Status string
	// Available 是本次读到的外部可用库存。
	Available *int
	// Message 是给界面和日志的说明。
	Message string
	// DryRun 表示没有提交下架、上架或改库存。
	DryRun bool
	// ListingState 是检查时读到的在售状态。
	ListingState string
}

// ExternalStockRepository 是外部库存配置的持久化端口，HTTP 响应不得使用带明文请求头的方法。
type ExternalStockRepository interface {
	// AccountOwned 判断账号是否属于用户，不读取凭证。
	AccountOwned(context.Context, int64, string) (bool, error)
	// ItemExists 判断账号下未删除商品是否存在。
	ItemExists(context.Context, string, string) (bool, error)
	// AccountUserID 返回账号所属用户。
	AccountUserID(context.Context, string) (int64, error)
	// Enabled 返回该商品是否启用外部库存；没有配置时为 false。
	Enabled(context.Context, string, string) (bool, error)
	// Get 返回不含请求头明文的配置；没有记录时返回默认值。
	Get(context.Context, string, string) (ExternalStockConfig, error)
	// Headers 解密该商品的自定义请求头，只供出站调用使用。
	Headers(context.Context, string, string) (map[string]string, error)
	// Save 校验后的配置和替换后的请求头明文；实现负责加密。
	Save(context.Context, ExternalStockConfig, map[string]string) error
	// SaveStatus 只更新最近一次检查的状态，不改配置和请求头。
	SaveStatus(context.Context, string, string, ExternalStockConfig) error
	// ListEnabled 返回全部已启用商品的非秘密配置，供调度器挑选到期项。
	ListEnabled(context.Context) ([]ExternalStockConfig, error)
	// LocalItemStatus 返回最近一次商品同步写入 item_detail 的 item_status。没有该字段时 present 为 false。
	LocalItemStatus(context.Context, string, string) (status int, present bool, err error)
}

// ExternalStockReader 读取外部可用库存，不访问闲鱼。
type ExternalStockReader interface {
	// ReadAvailable 按配置发起一次 GET，并返回非负可用库存。
	ReadAvailable(context.Context, ExternalStockRead) (int, error)
}

// ExternalStockRead 是一次外部库存请求的参数。Headers 含明文秘密，不能记录。
type ExternalStockRead struct {
	// URL 是已校验的 HTTP(S) 地址。
	URL string
	// Headers 是自定义请求头明文。
	Headers map[string]string
	// JSONPath 是可用库存路径。
	JSONPath string
	// Timeout 是本次请求超时。
	Timeout time.Duration
	// UserAgent 是实际发送的 User-Agent。
	UserAgent string
}

// ExternalStockApplyResult 是一次下架或重新上架的非秘密回执。
type ExternalStockApplyResult struct {
	// Diagnostic 是脱敏后的平台状态说明，不含 Cookie、请求头或响应正文。
	Diagnostic string
	// ListingState 是回执里看到的在售状态；未知时调用方仍然不得因此停止。
	ListingState string
	// Noop 表示商品已经处于目标状态，没有提交会改内容的编辑。
	Noop bool
}

// ExternalStockPlatform 是原商品下架和重新上架的最小端口。动作本身必须幂等，不能另建商品。
type ExternalStockPlatform interface {
	// Inspect 读取当前在售状态和库存，不修改商品。决策不再依赖它。
	Inspect(context.Context, int64, string, string) (ListingSnapshot, error)
	// Unpublish 下架原商品。商品已经不在售时返回成功。
	Unpublish(context.Context, int64, string, string) (ExternalStockApplyResult, error)
	// Relist 重新上架同一个原商品。已经在售时不提交编辑。
	Relist(context.Context, int64, string, string) (ExternalStockApplyResult, error)
	// SyncStock 保留给端口兼容；外部库存的目标状态动作不会改写平台库存。
	SyncStock(context.Context, int64, string, string, int) error
}

// externalStockPaceKey 标记本次检查属于后台任务，适配器据此启用更长的 MTOP 间隔。
type externalStockPaceKey struct{}

// WithExternalStockBackground 标记 ctx 中的平台调用使用后台节流。
func WithExternalStockBackground(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, externalStockPaceKey{}, true)
}

// ExternalStockBackground 返回 ctx 是否要求后台节流。
func ExternalStockBackground(ctx context.Context) bool {
	// marked 是上下文中的后台标记。
	marked, _ := ctx.Value(externalStockPaceKey{}).(bool)
	return marked
}

// ExternalStockService 编排单个商品的外部库存配置、检查和调度。
// mu 保护 locks、recent、nextPlatform 和启动状态；持有 mu 时不做网络或数据库 I/O。
// 每个商品还有一把独立互斥锁，重叠触发会串行，锁内允许外部 HTTP 和平台 I/O。
type ExternalStockService struct {
	// repository 保存配置和状态。
	repository ExternalStockRepository
	// reader 读取外部库存。
	reader ExternalStockReader
	// platform 读取并修改闲鱼原商品。
	platform ExternalStockPlatform
	// logger 记录动作和失败原因，不记录请求头、Cookie 或响应正文。
	logger *slog.Logger
	// mu 保护下面的内存状态。
	mu sync.Mutex
	// locks 是按商品串行检查的互斥锁。
	locks map[string]*sync.Mutex
	// recent 保存短时间内已完成的非手动检查，用于重叠触发去重。
	recent map[string]externalStockRecent
	// nextPlatform 是每个账号下一次允许发起平台读写的时间。
	nextPlatform map[string]time.Time
	// accountGap 非 0 时覆盖默认账号间隔，测试用它避免真实等待。
	accountGap time.Duration
	// runCtx 是 Start 创建的生命周期上下文；Close 取消它。
	runCtx context.Context
	// cancel 取消调度循环和发货后的检查。
	cancel context.CancelFunc
	// wg 等待调度循环和发货触发的检查结束。
	wg sync.WaitGroup
	// started 表示 Start 已经启动过循环。
	started bool
	// deliverySlots 是尚未结束的发货后检查数量，用来限制突发协程。
	deliverySlots int
}

// externalStockRecent 是一次已完成检查的时间和结果。
type externalStockRecent struct {
	// at 是检查完成时间。
	at time.Time
	// result 是可复用的非秘密结果。
	result ExternalStockCheckResult
}

// NewExternalStockService 创建外部库存服务。三个端口都必填。
func NewExternalStockService(repository ExternalStockRepository, reader ExternalStockReader, platform ExternalStockPlatform, logger *slog.Logger) (*ExternalStockService, error) {
	if repository == nil || reader == nil || platform == nil {
		return nil, errors.New("外部库存依赖不能为空")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ExternalStockService{
		repository:   repository,
		reader:       reader,
		platform:     platform,
		logger:       logger,
		locks:        map[string]*sync.Mutex{},
		recent:       map[string]externalStockRecent{},
		nextPlatform: map[string]time.Time{},
	}, nil
}

// DefaultExternalStockConfig 返回未配置商品的默认值，默认关闭。
func DefaultExternalStockConfig(cookieID, itemID string) ExternalStockConfig {
	return ExternalStockConfig{
		CookieID: cookieID, ItemID: itemID, Method: "GET", JSONPath: "available",
		Timeout: ExternalStockDefaultTimeout, Cron: ExternalStockDefaultCron,
		Timezone: ExternalStockDefaultTimezone, Interval: ExternalStockDefaultInterval, StockCap: StockMax,
	}
}

// Get 返回当前用户可见的配置，不包含请求头明文。
func (service *ExternalStockService) Get(ctx context.Context, userID int64, cookieID, itemID string) (ExternalStockConfig, error) {
	// cookie、item 是去掉空白后的账号和商品标识。
	cookie, item := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if // err 是账号或商品归属校验失败。
	err := service.authorize(ctx, userID, cookie, item); err != nil {
		return ExternalStockConfig{}, err
	}
	return service.repository.Get(ctx, cookie, item)
}

// Save 校验并保存配置。headers 是本次提交的完整请求头集合，未出现的旧请求头会被删除。
func (service *ExternalStockService) Save(ctx context.Context, userID int64, config ExternalStockConfig, headers []ExternalStockHeader) error {
	if service == nil || service.repository == nil {
		return errors.New("外部库存服务未初始化")
	}
	// normalized 是补齐默认值并去掉空白后的配置。
	normalized, err := normalizeExternalStockConfig(config)
	if err != nil {
		return err
	}
	if err = service.authorize(ctx, userID, normalized.CookieID, normalized.ItemID); err != nil {
		return err
	}
	// existing 是已保存的请求头明文，仅用于 Keep 语义，不写入日志。
	existing, err := service.repository.Headers(ctx, normalized.CookieID, normalized.ItemID)
	if err != nil {
		return err
	}
	// merged 是替换后的请求头明文。
	merged, names, err := mergeExternalStockHeaders(existing, headers)
	if err != nil {
		return err
	}
	normalized.HeaderNames = names
	return service.repository.Save(ctx, normalized, merged)
}

// Check 对一个商品读取外部库存并按决策下架、重新上架或改库存。
// 同一商品的重叠调用会串行；15 秒内的非手动调用复用上一次结果。失败不自动重试。
func (service *ExternalStockService) Check(ctx context.Context, userID int64, cookieID, itemID string, options ExternalStockCheckOptions) (ExternalStockCheckResult, error) {
	if service == nil || service.repository == nil || service.reader == nil || service.platform == nil {
		return ExternalStockCheckResult{}, errors.New("外部库存服务未初始化")
	}
	// cookie、item 是去掉空白后的账号和商品标识。
	cookie, item := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if userID <= 0 {
		// owner、ownerErr 是系统触发时从账号反查的所属用户。
		owner, ownerErr := service.repository.AccountUserID(ctx, cookie)
		if ownerErr != nil || owner <= 0 {
			return ExternalStockCheckResult{}, ErrExternalStockForbidden
		}
		userID = owner
	}
	if // err 是检查前的账号或商品归属校验失败。
	err := service.authorize(ctx, userID, cookie, item); err != nil {
		return ExternalStockCheckResult{}, err
	}
	// lock 保证同一商品不会并发下架和上架。
	lock := service.itemLock(cookie + "\x00" + item)
	lock.Lock()
	defer lock.Unlock()
	if options.Trigger != "manual" {
		service.mu.Lock()
		// cached 是窗口内可复用的结果；ok 表示确实有缓存。
		cached, ok := service.recent[cookie+"\x00"+item]
		service.mu.Unlock()
		if ok && time.Since(cached.at) < ExternalStockCoalesceWindow {
			return cached.result, nil
		}
	}
	// result、err 是本次独占执行的结果。
	result, err := service.checkLocked(ctx, cookie, item, options)
	if err == nil {
		service.mu.Lock()
		service.recent[cookie+"\x00"+item] = externalStockRecent{at: time.Now(), result: result}
		service.mu.Unlock()
	}
	return result, err
}

// Enabled 供售出后自动上架查询。启用外部库存的商品必须跳过自动上架。
func (service *ExternalStockService) Enabled(ctx context.Context, cookieID, itemID string) (bool, error) {
	if service == nil || service.repository == nil {
		return false, errors.New("外部库存服务未初始化")
	}
	return service.repository.Enabled(ctx, strings.TrimSpace(cookieID), strings.TrimSpace(itemID))
}

// AfterPaidDelivery 在付款发货成功后排队检查该商品。它立即返回，不在自动化收口里访问平台。
// 协程由本服务拥有，Close 会取消并等待；同时最多 8 个，超出的交给调度器补查。
func (service *ExternalStockService) AfterPaidDelivery(accountID, itemID, orderID string) {
	if service == nil {
		return
	}
	// account、item 是发货成功的账号和商品。
	account, item := strings.TrimSpace(accountID), strings.TrimSpace(itemID)
	if account == "" || item == "" {
		return
	}
	service.mu.Lock()
	if service.deliverySlots >= 8 {
		service.mu.Unlock()
		service.logger.Warn("外部库存发货后检查已跳过，等待调度补查", "account", account, "item_id", item, "order_id", strings.TrimSpace(orderID))
		return
	}
	service.deliverySlots++
	service.wg.Add(1)
	service.mu.Unlock()
	go service.deliveryCheck(account, item, strings.TrimSpace(orderID))
}

// Start 启动调度循环。ctx 取消后循环退出；重复调用直接返回。
func (service *ExternalStockService) Start(ctx context.Context) error {
	if service == nil {
		return errors.New("外部库存服务未初始化")
	}
	if ctx == nil {
		return errors.New("外部库存调度缺少上下文")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.started {
		return nil
	}
	// runCtx、cancel 是调度循环和发货后检查共用的生命周期上下文。
	runCtx, cancel := context.WithCancel(ctx)
	service.cancel = cancel
	service.runCtx = runCtx
	service.started = true
	service.wg.Add(1)
	go service.loop(runCtx)
	return nil
}

// Close 取消调度和发货后检查并等待它们结束。重复调用是安全的。
func (service *ExternalStockService) Close(ctx context.Context) error {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	// cancel 是 Start 时保存的取消函数。
	cancel := service.cancel
	service.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// done 在全部后台检查退出后关闭。
	done := make(chan struct{})
	go func() {
		service.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop 按固定间隔挑选到期商品。由 Start 启动，由 Close 取消并等待。
func (service *ExternalStockService) loop(ctx context.Context) {
	defer service.wg.Done()
	// ticker 是调度间隔，退出时释放。
	ticker := time.NewTicker(ExternalStockPassInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.runDue(ctx)
		}
	}
}

// runDue 顺序检查最多 ExternalStockMaxChecksPerPass 个到期商品，不并行扇出。
func (service *ExternalStockService) runDue(ctx context.Context) {
	// configs、err 是当前启用的商品配置。
	configs, err := service.repository.ListEnabled(ctx)
	if err != nil {
		service.logger.WarnContext(ctx, "读取外部库存调度列表失败", "err", err)
		return
	}
	// now 是本轮挑选到期商品的时间。
	now := time.Now()
	// checked 是本轮已经执行的检查数。
	checked := 0
	// config 是一个已启用商品。
	for _, config := range configs {
		if checked >= ExternalStockMaxChecksPerPass {
			return
		}
		if !config.Enabled || !ExternalStockDue(config.Cron, config.Timezone, config.Interval, config.LastChecked, now) {
			continue
		}
		if config.BackoffClass == ExternalStockBackoffAll && now.Before(config.BackoffUntil) {
			continue
		}
		// checkCtx 使用后台节流，并给单次检查留出外部超时和两次平台调用的时间。
		checkCtx, cancel := context.WithTimeout(WithExternalStockBackground(ctx), config.Timeout+2*time.Minute)
		// checkErr 是本轮该商品检查失败的原因；调度取消时停止继续挑选。
		_, checkErr := service.Check(checkCtx, 0, config.CookieID, config.ItemID, ExternalStockCheckOptions{Trigger: "schedule", CronMatch: ExternalStockCronHit(config.Cron, config.Timezone, now)})
		cancel()
		if checkErr != nil && ctx.Err() != nil {
			return
		}
		checked++
	}
}

// deliveryCheck 在服务生命周期内执行一次发货后的库存检查。
func (service *ExternalStockService) deliveryCheck(account, item, orderID string) {
	defer service.wg.Done()
	defer func() {
		service.mu.Lock()
		service.deliverySlots--
		service.mu.Unlock()
	}()
	service.mu.Lock()
	// parent 是调度生命周期；服务尚未启动时退回独立背景上下文，仍受超时限制。
	parent := service.runCtx
	service.mu.Unlock()
	if parent == nil {
		parent = context.Background()
	}
	// ctx 在 Close 或超时后取消，避免发货回调长期占用名额。
	ctx, stop := context.WithTimeout(WithExternalStockBackground(parent), ExternalStockMaxTimeout+2*time.Minute)
	defer stop()
	// result、err 是发货后的检查结果。
	result, err := service.Check(ctx, 0, account, item, ExternalStockCheckOptions{Trigger: "delivery"})
	if err != nil {
		service.logger.Warn("外部库存发货后检查失败", "account", account, "item_id", item, "order_id", orderID, "err", err)
		return
	}
	service.logger.Info("外部库存发货后检查完成", "account", account, "item_id", item, "order_id", orderID, "action", result.Action, "status", result.Status)
}

// checkLocked 在已持有商品锁时执行一次完整检查。它不先读取在售状态。
func (service *ExternalStockService) checkLocked(ctx context.Context, cookieID, itemID string, options ExternalStockCheckOptions) (ExternalStockCheckResult, error) {
	// config、err 是不含请求头明文的当前配置。
	config, err := service.repository.Get(ctx, cookieID, itemID)
	if err != nil {
		return ExternalStockCheckResult{}, err
	}
	if !config.Enabled {
		return ExternalStockCheckResult{}, ErrExternalStockDisabled
	}
	// now 是本次检查开始时间，用于判断退避和是否同一天。
	now := time.Now()
	if !options.BypassBackoff && config.BackoffClass == ExternalStockBackoffAll && now.Before(config.BackoffUntil) {
		// skipped 记录仍在退避中，不访问外部接口和平台。
		skipped := ExternalStockCheckResult{Action: ExternalStockActionStop, Status: "backed_off", Message: "上次失败后仍在退避，已跳过本次检查"}
		return skipped, nil
	}
	// headers 是出站用的明文请求头。
	headers, err := service.repository.Headers(ctx, cookieID, itemID)
	if err != nil {
		return ExternalStockCheckResult{}, err
	}
	// available、readErr 是外部可用库存及读取失败。
	available, readErr := service.reader.ReadAvailable(ctx, ExternalStockRead{
		URL: config.SourceURL, Headers: headers, JSONPath: config.JSONPath, Timeout: config.Timeout,
		UserAgent: NormalizeExternalStockUserAgent(config.UserAgent),
	})
	if readErr != nil {
		return service.finish(ctx, config, ExternalStockCheckResult{}, ExternalStockDecision{
			Action: ExternalStockActionStop, Reason: safeExternalStockError(readErr), BackoffClass: ExternalStockBackoffAll,
		}, options)
	}
	// count 是本次成功读到的可用库存副本，供状态保存。
	count := available
	// dryRun 默认沿用已保存演练；只有手动且明确要求执行一次时才写平台。
	dryRun := externalStockDryRun(config, options)
	// decision 只由外部可用库存决定下架或重新上架。
	decision := DecideExternalStock(available, dryRun)
	if decision.Action == ExternalStockActionRelist && !options.BypassBackoff && config.BackoffClass == ExternalStockBackoffRelist && now.Before(config.BackoffUntil) {
		return service.finish(ctx, config, ExternalStockCheckResult{Available: &count}, ExternalStockDecision{
			Action: ExternalStockActionStop, Reason: "网页编辑仍在退避，已跳过重新上架", BackoffClass: ExternalStockBackoffRelist,
		}, options)
	}
	if options.Trigger != "manual" && !options.CronMatch && SameDesiredStockApplied(decision.Action, available, config.AppliedAction, config.AppliedAvailable, config.AppliedAt, now, config.Timezone) {
		decision.Action = ExternalStockActionNone
		decision.Reason = "今天相同可用库存已成功处理，已跳过平台请求"
		decision.BackoffClass = ""
	}
	return service.finish(ctx, config, ExternalStockCheckResult{Available: &count}, decision, options)
}

// annotateListingReason 把脱敏诊断附在状态说明后面，方便下一次检查直接看到来源。
func annotateListingReason(reason, diagnostic string) string {
	if diagnostic == "" {
		return reason
	}
	// text 是界面和 last_error 共用的说明，限制长度以免超过状态列。
	text := reason + "（" + diagnostic + "）"
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}

// finish 按决策执行最多一次平台写操作，并保存可展示状态。
func (service *ExternalStockService) finish(ctx context.Context, config ExternalStockConfig, partial ExternalStockCheckResult, decision ExternalStockDecision, options ExternalStockCheckOptions) (ExternalStockCheckResult, error) {
	// result 是最终返回给调用方的非秘密结果。
	result := partial
	result.Action = decision.Action
	result.DryRun = decision.DryRun
	result.Message = decision.Reason
	if decision.DryRun && decision.Action != ExternalStockActionStop && decision.Action != ExternalStockActionNone {
		result.Status = "dry_run"
		service.logger.InfoContext(ctx, "外部库存演练", "account", config.CookieID, "item_id", config.ItemID, "trigger", options.Trigger, "action", decision.Action, "available", result.Available)
		return service.persist(ctx, config, result, decision.BackoffClass)
	}
	if decision.Action == ExternalStockActionStop {
		result.Status = "backed_off"
		if decision.BackoffClass == "" {
			decision.BackoffClass = ExternalStockBackoffAll
		}
		service.logger.WarnContext(ctx, "外部库存检查停止", "account", config.CookieID, "item_id", config.ItemID, "trigger", options.Trigger, "reason", decision.Reason)
		return service.persist(ctx, config, result, decision.BackoffClass)
	}
	if decision.Action == ExternalStockActionNone {
		result.Status = "ok"
		service.logger.InfoContext(ctx, "外部库存无需改动商品", "account", config.CookieID, "item_id", config.ItemID, "trigger", options.Trigger, "reason", decision.Reason)
		return service.persist(ctx, config, result, "")
	}
	// userID 已在 Check 中解析，这里从账号再取一次，避免 finish 依赖未传入的身份。
	userID, userErr := service.repository.AccountUserID(ctx, config.CookieID)
	if userErr != nil || userID <= 0 {
		return ExternalStockCheckResult{}, ErrExternalStockForbidden
	}
	if // err 是等待账号级平台间隔时被取消。
	err := service.paceAccount(ctx, config.CookieID); err != nil {
		return ExternalStockCheckResult{}, err
	}
	// actionErr 是唯一一次平台写操作的失败。
	var actionErr error
	// applied 是平台回执；失败时不采用其中的状态。
	var applied ExternalStockApplyResult
	switch decision.Action {
	case ExternalStockActionUnpublish:
		applied, actionErr = service.platform.Unpublish(ctx, userID, config.CookieID, config.ItemID)
	case ExternalStockActionRelist:
		applied, actionErr = service.platform.Relist(ctx, userID, config.CookieID, config.ItemID)
	default:
		actionErr = ErrExternalStockUnknownState
	}
	if actionErr != nil {
		// class 是写操作失败后的退避类别。在售状态未知不会进入这里。
		class := ExternalStockBackoffAll
		if errors.Is(actionErr, ErrExternalStockEditUnsupported) || errors.Is(actionErr, ErrExternalStockMultiSpec) {
			class = ExternalStockBackoffRelist
		}
		result.Action = ExternalStockActionStop
		result.Status = "backed_off"
		result.Message = safeExternalStockError(actionErr)
		service.logger.WarnContext(ctx, "外部库存平台动作失败", "account", config.CookieID, "item_id", config.ItemID, "trigger", options.Trigger, "action", decision.Action, "err", result.Message)
		return service.persist(ctx, config, result, class)
	}
	result.Status = "ok"
	result.ListingState = applied.ListingState
	result.Message = annotateListingReason(decision.Reason, applied.Diagnostic)
	service.logger.InfoContext(ctx, "外部库存已同步商品状态", "account", config.CookieID, "item_id", config.ItemID, "trigger", options.Trigger, "action", decision.Action, "noop", applied.Noop)
	return service.persist(ctx, config, result, "")
}

// persist 把本次结果写入配置状态列。
func (service *ExternalStockService) persist(ctx context.Context, config ExternalStockConfig, result ExternalStockCheckResult, backoffClass string) (ExternalStockCheckResult, error) {
	// status 只携带需要更新的状态字段，配置正文保持不变。
	status := config
	status.LastChecked = time.Now()
	status.LastAvailable = result.Available
	status.LastAction = result.Action
	status.LastStatus = result.Status
	// LastError 也保存成功和演练的脱敏诊断，商品页优先展示它。
	status.LastError = result.Message
	status.BackoffClass = ""
	status.BackoffUntil = time.Time{}
	if result.Status == "ok" && (result.Action == ExternalStockActionRelist || result.Action == ExternalStockActionUnpublish) {
		status.AppliedAction = result.Action
		status.AppliedAvailable = result.Available
		status.AppliedAt = status.LastChecked
	}
	if backoffClass != "" && result.Status != "ok" && result.Status != "dry_run" {
		status.BackoffClass = backoffClass
		status.BackoffUntil = status.LastChecked.Add(ExternalStockBackoff)
		result.Status = "backed_off"
		status.LastStatus = result.Status
	}
	if // err 是检查状态落库失败。
	err := service.repository.SaveStatus(ctx, config.CookieID, config.ItemID, status); err != nil {
		return ExternalStockCheckResult{}, err
	}
	return result, nil
}

// currentGap 返回账号级平台间隔；测试可把 accountGap 设成极小值。
func (service *ExternalStockService) currentGap() time.Duration {
	if service.accountGap > 0 {
		return service.accountGap
	}
	return ExternalStockAccountGap
}

// paceAccount 保证同一账号的平台读写起点至少间隔 ExternalStockAccountGap。
func (service *ExternalStockService) paceAccount(ctx context.Context, cookieID string) error {
	for {
		service.mu.Lock()
		// wait 是距离该账号下一次允许平台调用的剩余时间。
		wait := time.Until(service.nextPlatform[cookieID])
		if wait <= 0 {
			service.nextPlatform[cookieID] = time.Now().Add(service.currentGap())
			service.mu.Unlock()
			return nil
		}
		service.mu.Unlock()
		// timer 在间隔结束后唤醒，取消时释放。
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// itemLock 返回商品检查串行锁，不在持有服务锁时执行 I/O。
func (service *ExternalStockService) itemLock(key string) *sync.Mutex {
	service.mu.Lock()
	defer service.mu.Unlock()
	// lock 是该商品已有的互斥锁。
	lock := service.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		service.locks[key] = lock
	}
	return lock
}

// authorize 校验用户、账号和未删除商品。
func (service *ExternalStockService) authorize(ctx context.Context, userID int64, cookieID, itemID string) error {
	if userID <= 0 || cookieID == "" {
		return ErrExternalStockForbidden
	}
	if itemID == "" {
		return ErrExternalStockNotFound
	}
	// owned、err 是账号归属。
	owned, err := service.repository.AccountOwned(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrExternalStockForbidden
	}
	// exists 表示本地商品目录仍有该商品。
	exists, err := service.repository.ItemExists(ctx, cookieID, itemID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrExternalStockNotFound
	}
	return nil
}
