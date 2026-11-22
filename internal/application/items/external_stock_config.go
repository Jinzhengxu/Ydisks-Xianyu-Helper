package items

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

// normalizeExternalStockConfig 补齐默认值并拒绝危险地址、时区和请求参数。
func normalizeExternalStockConfig(config ExternalStockConfig) (ExternalStockConfig, error) {
	// normalized 是去掉空白后的配置。
	normalized := config
	normalized.CookieID = strings.TrimSpace(config.CookieID)
	normalized.ItemID = strings.TrimSpace(config.ItemID)
	normalized.SourceURL = strings.TrimSpace(config.SourceURL)
	normalized.Method = "GET"
	normalized.JSONPath = strings.TrimSpace(config.JSONPath)
	if normalized.JSONPath == "" {
		normalized.JSONPath = "available"
	}
	normalized.UserAgent = strings.TrimSpace(config.UserAgent)
	normalized.Cron = strings.TrimSpace(config.Cron)
	if normalized.Cron == "" {
		normalized.Cron = ExternalStockDefaultCron
	}
	normalized.Timezone = strings.TrimSpace(config.Timezone)
	if normalized.Timezone == "" {
		normalized.Timezone = ExternalStockDefaultTimezone
	}
	if normalized.Timeout == 0 {
		normalized.Timeout = ExternalStockDefaultTimeout
	}
	if normalized.StockCap == 0 {
		normalized.StockCap = StockMax
	}
	if normalized.CookieID == "" || normalized.ItemID == "" {
		return ExternalStockConfig{}, ErrExternalStockInvalid
	}
	if normalized.Enabled {
		if // err 是启用时库存地址不合法。
		err := validateExternalStockURL(normalized.SourceURL); err != nil {
			return ExternalStockConfig{}, err
		}
	} else if normalized.SourceURL != "" {
		if // err 是未启用时仍填写了不合法地址。
		err := validateExternalStockURL(normalized.SourceURL); err != nil {
			return ExternalStockConfig{}, err
		}
	}
	if !validJSONPath(normalized.JSONPath) {
		return ExternalStockConfig{}, fmtError("库存路径无效")
	}
	if normalized.Timeout < ExternalStockMinTimeout || normalized.Timeout > ExternalStockMaxTimeout {
		return ExternalStockConfig{}, fmtError("超时必须在 5 到 60 秒之间")
	}
	if normalized.UserAgent != "" && !validExternalStockText(normalized.UserAgent, ExternalStockUserAgentMax) {
		return ExternalStockConfig{}, fmtError("User-Agent 过长或不是有效文本")
	}
	if // err 是 cron 表达式不合法。
	err := ValidateExternalStockCron(normalized.Cron); err != nil {
		return ExternalStockConfig{}, err
	}
	if // err 是时区名称无法加载。
	_, err := time.LoadLocation(normalized.Timezone); err != nil {
		return ExternalStockConfig{}, fmtError("时区无效")
	}
	if normalized.Interval < 0 || normalized.Interval > 24*time.Hour {
		return ExternalStockConfig{}, fmtError("周期检查间隔无效")
	}
	if normalized.Interval > 0 && normalized.Interval < time.Minute {
		return ExternalStockConfig{}, fmtError("周期检查间隔至少 1 分钟")
	}
	if normalized.StockCap < 1 || normalized.StockCap > StockMax {
		return ExternalStockConfig{}, fmtError("库存上限无效")
	}
	return normalized, nil
}

// validateExternalStockURL 只接受没有用户信息的 http 或 https 地址。
func validateExternalStockURL(raw string) error {
	if !validExternalStockText(raw, ExternalStockURLMax) {
		return fmtError("库存地址无效")
	}
	// parsed 是解析后的地址。
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmtError("库存地址无效")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmtError("库存地址必须是 http 或 https")
	}
	return nil
}

// validJSONPath 限制路径为字母、数字、下划线和点，避免把它当成表达式执行。
func validJSONPath(path string) bool {
	if path == "" || len(path) > 128 || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return false
	}
	// char 是路径中的一个字符。
	for _, char := range path {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

// mergeExternalStockHeaders 用本次提交替换请求头。Keep 且值为空时保留旧明文。
func mergeExternalStockHeaders(existing map[string]string, updates []ExternalStockHeader) (map[string]string, []string, error) {
	if len(updates) > ExternalStockMaxHeaders {
		return nil, nil, fmtError("请求头过多")
	}
	// merged 是新的明文请求头。
	merged := map[string]string{}
	// names 按提交顺序记录名称。
	names := make([]string, 0, len(updates))
	// update 是本次提交的一条请求头。
	for _, update := range updates {
		// name 是规范化后的请求头名称。
		name := canonicalHeaderName(update.Name)
		if name == "" || headerForbidden(name) {
			return nil, nil, fmtError("请求头名称无效")
		}
		// duplicated 表示同一请求头名称出现了两次。
		if _, duplicated := merged[name]; duplicated {
			return nil, nil, fmtError("请求头重复")
		}
		// value 是本次明文；Keep 时沿用旧值。
		value := update.Value
		if strings.TrimSpace(value) == "" && update.Keep {
			value = existing[name]
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil, fmtError("请求头值不能为空")
		}
		if len(value) > ExternalStockHeaderValueMax || !utf8Valid(value) {
			return nil, nil, fmtError("请求头值无效")
		}
		merged[name] = value
		names = append(names, name)
	}
	return merged, names, nil
}

// canonicalHeaderName 把请求头名称整理成首字母大写的连字符形式。
func canonicalHeaderName(name string) string {
	// cleaned 是去掉空白后的原名。
	cleaned := strings.TrimSpace(name)
	if cleaned == "" || len(cleaned) > 64 {
		return ""
	}
	// part 是按连字符分开的一段。
	parts := strings.Split(cleaned, "-")
	// built 是规范化后的各段。
	built := make([]string, 0, len(parts))
	// part 是请求头名称中按连字符分开的一段。
	for _, part := range parts {
		if part == "" {
			return ""
		}
		// char 是名称中的一个字符。
		for _, char := range part {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
				return ""
			}
		}
		built = append(built, strings.ToUpper(part[:1])+strings.ToLower(part[1:]))
	}
	return strings.Join(built, "-")
}

// headerForbidden 拒绝会改写传输或覆盖受控 User-Agent 的请求头。
func headerForbidden(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "transfer-encoding", "connection", "user-agent":
		return true
	default:
		return false
	}
}

// safeExternalStockError 返回可展示的失败说明，去掉可能夹带的响应正文。
func safeExternalStockError(err error) string {
	if err == nil {
		return ""
	}
	// text 是错误文本的第一行，避免把多行响应带进界面。
	text := strings.TrimSpace(strings.Split(err.Error(), "\n")[0])
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}

// utf8Valid 判断请求头值是有效 Unicode。
func utf8Valid(value string) bool {
	return validExternalStockText(value, len(value)+1)
}

// fmtError 包装配置校验失败。
func fmtError(message string) error {
	return errors.Join(ErrExternalStockInvalid, errors.New(message))
}
