package items

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// ExternalStockDefaultTimeout 是外部库存接口的默认超时，覆盖常见边缘节点的慢响应。
	ExternalStockDefaultTimeout = 20 * time.Second
	// ExternalStockMinTimeout 是允许保存的最短超时，避免把一次检查变成忙等。
	ExternalStockMinTimeout = 5 * time.Second
	// ExternalStockMaxTimeout 是允许保存的最长超时，避免一个商品占住检查队列过久。
	ExternalStockMaxTimeout = 60 * time.Second
	// ExternalStockDefaultCron 是每天 00:01 的五段 cron，供库存按自然日恢复后重新上架。
	ExternalStockDefaultCron = "1 0 * * *"
	// ExternalStockDefaultTimezone 是默认调度时区，与闲鱼卖家常用的北京时间一致。
	ExternalStockDefaultTimezone = "Asia/Shanghai"
	// ExternalStockDefaultInterval 是默认定时复查间隔；0 表示只按 cron 检查。
	ExternalStockDefaultInterval = 30 * time.Minute
	// ExternalStockBackoff 是风控、平台 5xx、未知状态或不支持编辑后的停止时间，期间调度不再请求平台。
	ExternalStockBackoff = 30 * time.Minute
	// ExternalStockCoalesceWindow 是重叠触发合并窗口，窗口内的非手动检查复用刚完成的结果。
	ExternalStockCoalesceWindow = 15 * time.Second
	// ExternalStockAccountGap 是同一账号两次平台读写之间的最短间隔，后台节流器仍会另行生效。
	ExternalStockAccountGap = 5 * time.Second
	// ExternalStockMaxChecksPerPass 是一次调度扫描最多处理的已启用商品数，其余留到下一轮。
	ExternalStockMaxChecksPerPass = 3
	// ExternalStockPassInterval 是调度器醒来查看到期商品的间隔。
	ExternalStockPassInterval = 30 * time.Second
	// ExternalStockMaxHeaders 是单个商品允许保存的自定义请求头数量。
	ExternalStockMaxHeaders = 20
	// ExternalStockHeaderValueMax 是单个请求头值的最大字节数，超长值通常不是合法凭证。
	ExternalStockHeaderValueMax = 2048
	// ExternalStockURLMax 是外部库存地址允许的最大字符数。
	ExternalStockURLMax = 2048
	// ExternalStockUserAgentMax 是自定义 User-Agent 的最大字符数。
	ExternalStockUserAgentMax = 256
	// DefaultExternalStockUserAgent 是桌面 Chrome 的 User-Agent，避免 Cloudflare 用 1010 拒绝默认客户端。
	DefaultExternalStockUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

const (
	// ListingOnSale 表示平台当前详情里的商品处于在售。
	ListingOnSale = "on_sale"
	// ListingOffSale 表示平台当前详情里的商品已下架。
	ListingOffSale = "off_sale"
	// ListingUnknown 表示没能确认在售或下架，此时禁止改动商品。
	ListingUnknown = "unknown"
)

const (
	// ExternalStockActionNone 表示当前在售状态已经符合外部库存，不调用平台写接口。
	ExternalStockActionNone = "none"
	// ExternalStockActionUnpublish 表示应下架原商品。
	ExternalStockActionUnpublish = "unpublish"
	// ExternalStockActionRelist 表示应把原商品重新上架，而不是发布新商品。
	ExternalStockActionRelist = "relist"
	// ExternalStockActionSyncStock 表示商品已在售，只需要把平台库存改成外部可用数。
	ExternalStockActionSyncStock = "sync_stock"
	// ExternalStockActionStop 表示本次必须停止，不能继续下架、上架或改库存。
	ExternalStockActionStop = "stop"
)

const (
	// ExternalStockBackoffAll 表示在退避结束前禁止一切平台读写。
	ExternalStockBackoffAll = "all"
	// ExternalStockBackoffRelist 表示只禁止依赖网页编辑的上架和改库存，下架仍可执行。
	ExternalStockBackoffRelist = "relist"
)

var (
	// ErrExternalStockInvalid 表示保存的外部库存配置不满足地址、cron、时区或请求头约束。
	ErrExternalStockInvalid = errors.New("外部库存配置无效")
	// ErrExternalStockForbidden 表示当前用户无权配置该账号的商品。
	ErrExternalStockForbidden = errors.New("无权限操作该账号")
	// ErrExternalStockNotFound 表示账号下没有这个未删除商品。
	ErrExternalStockNotFound = errors.New("商品不存在")
	// ErrExternalStockDisabled 表示商品没有启用外部库存，检查不会访问平台。
	ErrExternalStockDisabled = errors.New("商品未启用外部库存")
	// ErrExternalStockFetch 表示外部库存接口超时、返回 5xx/4xx 或响应无法解析，本次不改动商品。
	ErrExternalStockFetch = errors.New("读取外部库存失败")
	// ErrExternalStockEditUnsupported 表示闲鱼拒绝网页端编辑，常见于虚拟商品或鱼小铺。
	ErrExternalStockEditUnsupported = errors.New("该商品不支持网页端编辑或重新上架")
	// ErrExternalStockRisk 表示闲鱼要求安全验证，本次已停止且不会自动重试。
	ErrExternalStockRisk = errors.New("闲鱼要求安全验证，已停止外部库存同步")
	// ErrExternalStockSession 表示账号登录已失效。
	ErrExternalStockSession = errors.New("账号登录已失效，已停止外部库存同步")
	// ErrExternalStockPlatform 表示平台返回 5xx 或其他不确定结果。
	ErrExternalStockPlatform = errors.New("闲鱼平台暂时失败，已停止外部库存同步")
	// ErrExternalStockUnknownState 表示没能确认商品在售状态。
	ErrExternalStockUnknownState = errors.New("无法确认商品在售状态，已停止外部库存同步")
	// ErrExternalStockMultiSpec 表示多规格商品不能把一个外部库存数写进每一行规格。
	ErrExternalStockMultiSpec = errors.New("多规格商品暂不能按外部库存改写各规格库存")
)

// ListingSnapshot 是一次在售状态读取的结果，可以来自网页编辑详情或本地同步记录。
type ListingSnapshot struct {
	// State 是 on_sale、off_sale 或 unknown。
	State string
	// Quantity 是单规格库存；多规格时为各规格之和。
	Quantity int
	// QuantityKnown 表示 Quantity 来自本次平台读取而不是缺省 0。
	QuantityKnown bool
	// MultiSpec 表示库存由多行规格组成。
	MultiSpec bool
	// RawStatus 是来源里的原始状态码，只用于诊断，不是凭证。
	RawStatus string
	// Source 是 editDetail 或 local_item_detail。
	Source string
}

// ClassifyEditDetailStatus 解释网页编辑详情里的 itemStatus。
// 真机编辑详情对在售商品返回 "1"（库存编辑夹具沿用该形态）；提交重新上架时写入 "0"，模拟器也会把在售读回 "0"。下架为 "-2"。
// 其他取值视为未知，调用方应保留原始码并改用本地同步状态，不能把它当成下架。
func ClassifyEditDetailStatus(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "0", "1":
		return ListingOnSale, true
	case "-2":
		return ListingOffSale, true
	default:
		return ListingUnknown, false
	}
}

// ClassifyLocalItemStatus 解释商品同步写进 item_detail.item_status 的列表状态。
// 卖家在售列表和平台模拟器都用 0 表示在售。其他取值不猜测为下架，避免用过期列表把仍在售的商品重新上架。
func ClassifyLocalItemStatus(status int, present bool) (string, bool) {
	if present && status == 0 {
		return ListingOnSale, true
	}
	return ListingUnknown, false
}

// ExternalStockDecision 是纯决策结果，不包含平台调用。
type ExternalStockDecision struct {
	// Action 是 none、unpublish、relist、sync_stock 或 stop。
	Action string
	// Reason 是写入状态和日志的中文说明，不含凭证。
	Reason string
	// Quantity 是上架或改库存时期望写入的数量；0 表示沿用既有“售罄补 1”的重新上架。
	Quantity int
	// Sync 表示本次上架或改库存要写入 Quantity，而不是补最小库存。
	Sync bool
	// DryRun 表示只记录将要执行的动作，不提交平台写请求。
	DryRun bool
	// BackoffClass 非空时记录退避；all 暂停全部平台动作，relist 只暂停网页编辑。
	BackoffClass string
}

// DecideExternalStock 只根据外部可用库存决定目标状态，不读取也不依赖当前在售状态。
// available 小于 0 视为无效读数。available 小于等于 0 时只下架，大于 0 时只重新上架原商品。
// dryRun 不改变动作种类，只标记不要写平台。在售状态未知不会产生停止或退避。
func DecideExternalStock(available int, dryRun bool) ExternalStockDecision {
	if available < 0 {
		return ExternalStockDecision{Action: ExternalStockActionStop, DryRun: dryRun, Reason: "外部可用库存无效", BackoffClass: ExternalStockBackoffAll}
	}
	if available <= 0 {
		return ExternalStockDecision{Action: ExternalStockActionUnpublish, DryRun: dryRun, Reason: "外部可用库存为 0，下架原商品"}
	}
	return ExternalStockDecision{Action: ExternalStockActionRelist, DryRun: dryRun, Reason: "外部可用库存大于 0，重新上架原商品"}
}

// SameDesiredStockApplied 判断今天是否已经成功执行过相同动作和相同可用库存。
// 调用方仍会在 cron 和手动检查时重新执行。时区无法加载时不把记录当成已成功，避免漏做。
func SameDesiredStockApplied(action string, available int, appliedAction string, appliedAvailable *int, appliedAt, now time.Time, timezone string) bool {
	if action != ExternalStockActionRelist && action != ExternalStockActionUnpublish {
		return false
	}
	if appliedAction != action || appliedAvailable == nil || *appliedAvailable != available || appliedAt.IsZero() {
		return false
	}
	// location、err 是配置时区；加载失败时不跳过。
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return false
	}
	// appliedLocal 是上次成功在配置时区中的时间。
	appliedLocal := appliedAt.In(location)
	// current 是本次检查在同一时区中的时间。
	current := now.In(location)
	return appliedLocal.Year() == current.Year() && appliedLocal.YearDay() == current.YearDay()
}

// ExternalStockCronHit 判断 now 在配置时区是否命中 cron 的这一分钟。表达式无效时不算命中。
func ExternalStockCronHit(cronExpr, timezone string, now time.Time) bool {
	if ValidateExternalStockCron(cronExpr) != nil {
		return false
	}
	// location、err 是配置时区。
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return false
	}
	return CronMatches(cronExpr, now.In(location))
}

// ParseAvailableCount 从 JSON 正文按点分路径读取非负整数。
// path 为空或为 available 时，若顶层没有 available，再尝试 data.available。
func ParseAvailableCount(body []byte, path string) (int, error) {
	// decoder 使用 JSON 数字原文，避免大整数先变成浮点。
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	// payload 是外部接口的任意 JSON 值。
	var payload any
	if // err 是 JSON 解码失败。
	err := decoder.Decode(&payload); err != nil {
		return 0, fmt.Errorf("%w: 响应不是 JSON", ErrExternalStockFetch)
	}
	// configured 是去掉空白后的路径；空路径按 available 处理。
	configured := strings.TrimSpace(path)
	if configured == "" {
		configured = "available"
	}
	// value、found 是按配置路径找到的节点。
	value, found := walkJSONPath(payload, configured)
	if !found && (configured == "available") {
		value, found = walkJSONPath(payload, "data.available")
	}
	if !found {
		return 0, fmt.Errorf("%w: 找不到可用库存路径 %s", ErrExternalStockFetch, configured)
	}
	// count 是解析出的非负整数。
	count, err := jsonInteger(value)
	if err != nil {
		return 0, fmt.Errorf("%w: 可用库存不是整数", ErrExternalStockFetch)
	}
	return count, nil
}

// walkJSONPath 按点分路径读取对象字段；任一段缺失或不是对象时返回 false。
func walkJSONPath(root any, path string) (any, bool) {
	// current 是当前走到的 JSON 节点。
	current := root
	// part 是路径中的一段字段名。
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, false
		}
		// object 是当前节点的对象形式。
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		// next、exists 是下一段字段及其是否存在。
		next, exists := object[part]
		if !exists {
			return nil, false
		}
		current = next
	}
	return current, true
}

// jsonInteger 把 JSON 数字或整数字符串变成非负 int；小数和负数都拒绝。
func jsonInteger(value any) (int, error) {
	// typed 是 JSON 节点的具体类型，用来区分数字和字符串。
	switch typed := value.(type) {
	case json.Number:
		// parsed 是 JSON 数字的十进制整数。
		parsed, err := strconv.Atoi(typed.String())
		if err != nil || parsed < 0 {
			return 0, errors.New("可用库存不是非负整数")
		}
		return parsed, nil
	case string:
		// parsed 是字符串形式的非负整数。
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || parsed < 0 {
			return 0, errors.New("可用库存不是非负整数")
		}
		return parsed, nil
	default:
		return 0, errors.New("不是整数")
	}
}

// ValidateExternalStockCron 检查五段 cron。支持 *、单个数字、逗号列表和闭区间，不支持步长和英文月份。
func ValidateExternalStockCron(expr string) error {
	// fields 是按空白分开的分、时、日、月、周。
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("%w: cron 必须是五段", ErrExternalStockInvalid)
	}
	// limits 依次是分、时、日、月、周允许的闭区间。
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	// index 是当前字段下标；field 是该段表达式。
	for index, field := range fields {
		if // err 是这一段超出允许范围或包含步长。
		err := validateCronField(field, limits[index][0], limits[index][1]); err != nil {
			return err
		}
	}
	return nil
}

// validateCronField 检查一段 cron 是否只包含允许范围内的数字、列表和区间。
func validateCronField(field string, minValue, maxValue int) error {
	if field == "*" {
		return nil
	}
	// part 是逗号分隔的一个数字或区间。
	for _, part := range strings.Split(field, ",") {
		if part == "" || strings.Contains(part, "/") {
			return fmt.Errorf("%w: cron 字段 %s 无效", ErrExternalStockInvalid, field)
		}
		// bounds 是区间的两端；单个数字只有一端。
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return fmt.Errorf("%w: cron 字段 %s 无效", ErrExternalStockInvalid, field)
		}
		// bound 是区间的一个端点文本。
		for _, bound := range bounds {
			// number、err 是端点的整数值及解析错误。
			number, err := strconv.Atoi(bound)
			if err != nil || number < minValue || number > maxValue {
				return fmt.Errorf("%w: cron 字段 %s 超出范围", ErrExternalStockInvalid, field)
			}
		}
		if len(bounds) == 2 {
			// start 是已经确认合法的区间起点。
			start, _ := strconv.Atoi(bounds[0])
			// end 是已经确认合法的区间终点。
			end, _ := strconv.Atoi(bounds[1])
			if start > end {
				return fmt.Errorf("%w: cron 字段 %s 无效", ErrExternalStockInvalid, field)
			}
		}
	}
	return nil
}

// CronMatches 判断本地时间的分、时、日、月、周是否命中五段 cron。调用前必须已通过 ValidateExternalStockCron。
func CronMatches(expr string, local time.Time) bool {
	// fields 是已校验过的五段表达式。
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	// weekday 把 Go 的 Sunday=0 对齐到 cron 的 0–6。
	weekday := int(local.Weekday())
	// values 依次是当前的分、时、日、月、周。
	values := []int{local.Minute(), local.Hour(), local.Day(), int(local.Month()), weekday}
	// index 是字段下标；field 是对应表达式。
	for index, field := range fields {
		if !cronFieldMatches(field, values[index]) {
			return false
		}
	}
	return true
}

// cronFieldMatches 判断一个数值是否落在 *、列表或区间中。
func cronFieldMatches(field string, value int) bool {
	if field == "*" {
		return true
	}
	// part 是一个数字或区间。
	for _, part := range strings.Split(field, ",") {
		// bounds 是区间端点。
		bounds := strings.Split(part, "-")
		if len(bounds) == 1 {
			// number 是单个允许值。
			number, err := strconv.Atoi(bounds[0])
			if err == nil && number == value {
				return true
			}
			continue
		}
		// start、startErr 是区间起点及其解析错误。
		start, startErr := strconv.Atoi(bounds[0])
		// end、endErr 是区间终点及其解析错误。
		end, endErr := strconv.Atoi(bounds[1])
		if startErr == nil && endErr == nil && value >= start && value <= end {
			return true
		}
	}
	return false
}

// ExternalStockDue 判断一条已启用配置在 now 是否该由调度器检查。
// cron 命中当前分钟且这一分钟还没检查过，或周期间隔已经走完，都算到期。
func ExternalStockDue(cronExpr, timezone string, interval time.Duration, lastChecked, now time.Time) bool {
	// location、err 是配置时区；加载失败时不把商品当成到期，避免用错误时间上架。
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return false
	}
	// local 是 now 在配置时区中的时间。
	local := now.In(location)
	if interval > 0 && (lastChecked.IsZero() || now.Sub(lastChecked) >= interval) {
		return true
	}
	if ValidateExternalStockCron(cronExpr) != nil || !CronMatches(cronExpr, local) {
		return false
	}
	if lastChecked.IsZero() {
		return true
	}
	// previous 是上次检查在同一时区的时间，用来判断是否仍处在命中的这一分钟。
	previous := lastChecked.In(location)
	return previous.Year() != local.Year() || previous.YearDay() != local.YearDay() || previous.Hour() != local.Hour() || previous.Minute() != local.Minute()
}

// NormalizeExternalStockUserAgent 返回实际发出的 User-Agent；空白配置使用桌面浏览器默认值。
func NormalizeExternalStockUserAgent(configured string) string {
	// agent 是去掉首尾空白后的自定义标识。
	agent := strings.TrimSpace(configured)
	if agent == "" {
		return DefaultExternalStockUserAgent
	}
	return agent
}

// validExternalStockText 判断文本是有效 Unicode 且不超过字符上限。
func validExternalStockText(value string, maxRunes int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes
}
