package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// externalStockHeaderRequest 是保存时提交的一条请求头。value 只在本次请求内存中使用。
type externalStockHeaderRequest struct {
	// Name 是请求头名称。
	Name string `json:"name"`
	// Value 是新的明文值。留空且 keep 为真时保留已保存的值。
	Value string `json:"value"`
	// Keep 表示不修改已保存的秘密值。
	Keep bool `json:"keep"`
}

// externalStockUpdateRequest 是保存外部库存配置的请求体。
type externalStockUpdateRequest struct {
	// Enabled 表示启用该商品的外部库存同步。
	Enabled bool `json:"enabled"`
	// SourceURL 是返回可用库存的地址。
	SourceURL string `json:"source_url"`
	// JSONPath 是可用库存的点分路径。
	JSONPath string `json:"json_path"`
	// TimeoutSeconds 是外部请求超时秒数。
	TimeoutSeconds int `json:"timeout_seconds"`
	// UserAgent 为空时使用桌面浏览器默认标识。
	UserAgent string `json:"user_agent"`
	// Cron 是五段调度表达式。
	Cron string `json:"cron"`
	// Timezone 是 IANA 时区。
	Timezone string `json:"timezone"`
	// IntervalMinutes 是周期检查间隔，0 表示只按 cron。
	IntervalMinutes int `json:"interval_minutes"`
	// DryRun 表示自动检查只记录将要执行的动作。
	DryRun bool `json:"dry_run"`
	// SyncPlatformStock 表示把平台库存改成外部可用数。
	SyncPlatformStock bool `json:"sync_platform_stock"`
	// StockCap 是同步到平台的库存上限。
	StockCap int `json:"stock_cap"`
	// Headers 是本次要保存的完整请求头集合。
	Headers []externalStockHeaderRequest `json:"headers"`
}

// externalStockCheckRequest 是手动检查的可选请求体。
type externalStockCheckRequest struct {
	// DryRun 为真时这一次只演练。为假时不能关闭已保存的演练开关。
	DryRun *bool `json:"dry_run"`
	// ExecuteOnce 为真时，这一次手动检查真实下架或重新上架，不修改已保存的演练开关。
	ExecuteOnce bool `json:"execute_once"`
}

// externalStockHeaderResponse 只公开请求头名称，不返回值。
type externalStockHeaderResponse struct {
	// Name 是已保存的请求头名称。
	Name string `json:"name"`
	// Set 固定为真，表示服务端保存了值。
	Set bool `json:"set"`
}

// externalStockResponse 是外部库存配置和最近状态，不含请求头明文。
type externalStockResponse struct {
	// CookieID 是商品所属账号。
	CookieID string `json:"cookie_id"`
	// ItemID 是闲鱼原商品 ID。
	ItemID string `json:"item_id"`
	// Enabled 表示是否启用。
	Enabled bool `json:"enabled"`
	// SourceURL 是外部库存地址。
	SourceURL string `json:"source_url"`
	// Method 固定为 GET。
	Method string `json:"method"`
	// JSONPath 是可用库存路径。
	JSONPath string `json:"json_path"`
	// TimeoutSeconds 是超时秒数。
	TimeoutSeconds int `json:"timeout_seconds"`
	// UserAgent 是自定义标识，空表示使用默认桌面浏览器标识。
	UserAgent string `json:"user_agent"`
	// Cron 是五段表达式。
	Cron string `json:"cron"`
	// Timezone 是时区。
	Timezone string `json:"timezone"`
	// IntervalMinutes 是周期间隔分钟数。
	IntervalMinutes int `json:"interval_minutes"`
	// DryRun 表示自动检查是否演练。
	DryRun bool `json:"dry_run"`
	// SyncPlatformStock 表示是否同步平台库存。
	SyncPlatformStock bool `json:"sync_platform_stock"`
	// StockCap 是库存上限。
	StockCap int `json:"stock_cap"`
	// Headers 只有名称。
	Headers []externalStockHeaderResponse `json:"headers"`
	// LastCheckedUnix 是上次检查的 Unix 秒，0 表示尚未检查。
	LastCheckedUnix int64 `json:"last_checked_unix"`
	// LastAvailable 是上次成功读数，没有成功读数时为空。
	LastAvailable *int `json:"last_available"`
	// LastAction 是上次动作。
	LastAction string `json:"last_action"`
	// LastStatus 是上次状态。
	LastStatus string `json:"last_status"`
	// LastError 是上次可展示错误。
	LastError string `json:"last_error"`
	// BackoffUntilUnix 是退避结束时间。
	BackoffUntilUnix int64 `json:"backoff_until_unix"`
	// BackoffClass 是 all、relist 或空。
	BackoffClass string `json:"backoff_class"`
}

// externalStockCheckResponse 是一次检查的非秘密结果。
type externalStockCheckResponse struct {
	// Action 是决策动作。
	Action string `json:"action"`
	// Status 是 ok、dry_run、error 或 backed_off。
	Status string `json:"status"`
	// Available 是本次外部可用库存。
	Available *int `json:"available"`
	// Message 是给界面的说明。
	Message string `json:"message"`
	// DryRun 表示没有写平台。
	DryRun bool `json:"dry_run"`
	// ListingState 是检查时的在售状态。
	ListingState string `json:"listing_state"`
	// Config 是检查后的最新非秘密配置。
	Config externalStockResponse `json:"config"`
}

// getItemExternalStock 返回商品外部库存配置，不返回请求头明文。
func (s *Server) getItemExternalStock(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID 是路径中的账号和商品。
	cookieID, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// session 是当前登录用户。
	session := auth.SessionFromContext(r.Context())
	// config、err 是应用服务返回的非秘密配置。
	config, err := s.itemExternalStockApplication().Get(r.Context(), session.UserID, cookieID, itemID)
	if err != nil {
		writeExternalStockError(w, err)
		return
	}
	writeExternalStockJSON(w, http.StatusOK, externalStockHTTPResponse(config))
}

// updateItemExternalStock 保存配置。请求头值不会出现在响应中。
func (s *Server) updateItemExternalStock(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID 是路径中的账号和商品。
	cookieID, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// request 是解码后的配置。
	var request externalStockUpdateRequest
	if // err 是请求正文无法解码。
	err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// session 是当前登录用户。
	session := auth.SessionFromContext(r.Context())
	// headers 是交给应用服务的请求头，响应阶段不再使用。
	headers := make([]itemapp.ExternalStockHeader, 0, len(request.Headers))
	// header 是本次提交的一条请求头。
	for _, header := range request.Headers {
		headers = append(headers, itemapp.ExternalStockHeader{Name: header.Name, Value: header.Value, Keep: header.Keep})
	}
	// config 是要保存的非秘密配置。
	config := itemapp.ExternalStockConfig{
		CookieID: cookieID, ItemID: itemID, Enabled: request.Enabled, SourceURL: request.SourceURL, JSONPath: request.JSONPath,
		Timeout: time.Duration(request.TimeoutSeconds) * time.Second, UserAgent: request.UserAgent, Cron: request.Cron,
		Timezone: request.Timezone, Interval: time.Duration(request.IntervalMinutes) * time.Minute, DryRun: request.DryRun,
		SyncPlatformStock: request.SyncPlatformStock, StockCap: request.StockCap,
	}
	if // err 是配置保存失败。
	err := s.itemExternalStockApplication().Save(r.Context(), session.UserID, config, headers); err != nil {
		writeExternalStockError(w, err)
		return
	}
	// saved、err 是保存后重新读取的配置，确认响应不含请求头值。
	saved, err := s.itemExternalStockApplication().Get(r.Context(), session.UserID, cookieID, itemID)
	if err != nil {
		writeExternalStockError(w, err)
		return
	}
	writeExternalStockJSON(w, http.StatusOK, externalStockHTTPResponse(saved))
}

// checkItemExternalStock 立即检查一个商品。空请求体表示按已保存的演练开关执行。
func (s *Server) checkItemExternalStock(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID 是路径中的账号和商品。
	cookieID, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// raw、readErr 是可选请求体。空正文表示按已保存配置检查。
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBytes+1))
	if readErr != nil || len(raw) > maxJSONRequestBytes {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// request 是可选的演练覆盖。
	var request externalStockCheckRequest
	if len(bytes.TrimSpace(raw)) > 0 {
		if // err 是检查请求正文无法解码。
		err := json.Unmarshal(raw, &request); err != nil {
			writeErr(w, http.StatusBadRequest, "请求格式错误")
			return
		}
	}
	// session 是当前登录用户。
	session := auth.SessionFromContext(r.Context())
	if request.ExecuteOnce && request.DryRun != nil && *request.DryRun {
		writeErr(w, http.StatusBadRequest, "execute_once 不能与演练同时指定")
		return
	}
	// options 是手动检查选项，允许越过退避。调度和发货不会带上 ExecuteOnce。
	options := itemapp.ExternalStockCheckOptions{Trigger: "manual", BypassBackoff: true, ExecuteOnce: request.ExecuteOnce}
	if request.DryRun != nil {
		options.ForceDryRun = *request.DryRun
	}
	// result、err 是检查结果。
	result, err := s.itemExternalStockApplication().Check(r.Context(), session.UserID, cookieID, itemID, options)
	if err != nil {
		writeExternalStockError(w, err)
		return
	}
	// config、configErr 是检查后的最新状态。
	config, configErr := s.itemExternalStockApplication().Get(r.Context(), session.UserID, cookieID, itemID)
	if configErr != nil {
		writeExternalStockError(w, configErr)
		return
	}
	writeExternalStockJSON(w, http.StatusOK, externalStockCheckResponse{
		Action: result.Action, Status: result.Status, Available: result.Available, Message: result.Message,
		DryRun: result.DryRun, ListingState: result.ListingState, Config: externalStockHTTPResponse(config),
	})
}

// writeExternalStockError 把应用错误映射为 HTTP 状态。
func writeExternalStockError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, itemapp.ErrExternalStockForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, itemapp.ErrExternalStockNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, itemapp.ErrExternalStockInvalid), errors.Is(err, itemapp.ErrExternalStockDisabled):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "外部库存操作失败")
	}
}

// writeExternalStockJSON 写不缓存的 JSON，避免浏览器保存配置响应。
func writeExternalStockJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, payload)
}

// externalStockHTTPResponse 把应用配置转成不含秘密的响应。
func externalStockHTTPResponse(config itemapp.ExternalStockConfig) externalStockResponse {
	// headers 只有名称。
	headers := make([]externalStockHeaderResponse, 0, len(config.HeaderNames))
	// name 是一个已保存的请求头名称。
	for _, name := range config.HeaderNames {
		headers = append(headers, externalStockHeaderResponse{Name: name, Set: true})
	}
	// response 是对外配置。
	response := externalStockResponse{
		CookieID: config.CookieID, ItemID: config.ItemID, Enabled: config.Enabled, SourceURL: config.SourceURL,
		Method: config.Method, JSONPath: config.JSONPath, TimeoutSeconds: int(config.Timeout / time.Second),
		UserAgent: config.UserAgent, Cron: config.Cron, Timezone: config.Timezone,
		IntervalMinutes: int(config.Interval / time.Minute), DryRun: config.DryRun,
		SyncPlatformStock: config.SyncPlatformStock, StockCap: config.StockCap, Headers: headers,
		LastAvailable: config.LastAvailable, LastAction: config.LastAction, LastStatus: config.LastStatus,
		LastError: config.LastError, BackoffClass: config.BackoffClass,
	}
	if response.Method == "" {
		response.Method = "GET"
	}
	if !config.LastChecked.IsZero() {
		response.LastCheckedUnix = config.LastChecked.Unix()
	}
	if !config.BackoffUntil.IsZero() {
		response.BackoffUntilUnix = config.BackoffUntil.Unix()
	}
	return response
}
