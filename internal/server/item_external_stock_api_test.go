package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestItemExternalStockConfigHidesHeaderSecrets 验证保存请求头后，响应和错误都不回显明文。
func TestItemExternalStockConfigHidesHeaderSecrets(t *testing.T) {
	// srv、store、cleanup 是带迁移的测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入商品夹具的上下文。
	ctx := context.Background()
	if // insertErr 是写入商品夹具失败。
	_, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery) VALUES ('acc1','stock-item','游泳票','','','1.00','{}',0,0)`); insertErr != nil {
		t.Fatalf("insert item: %v", insertErr)
	}
	// handler 是完整路由。
	handler := srv.Router()
	// sessionCookie 是管理员会话。
	sessionCookie := loginHelper(t, handler)
	// secret 是不能出现在响应里的请求头值。
	secret := "Bearer super-secret-token"
	// saveBody 是启用前的配置，地址合法但开关关闭，因此不会访问外部接口。
	saveBody := `{"enabled":true,"source_url":"http://127.0.0.1:1/slots","json_path":"data.available","timeout_seconds":5,"user_agent":"","cron":"1 0 * * *","timezone":"Asia/Shanghai","interval_minutes":30,"dry_run":true,"sync_platform_stock":false,"stock_cap":1,"headers":[{"name":"Authorization","value":"` + secret + `"}]}`
	// saveReq 保存配置。
	saveReq := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/stock-item/external-stock", strings.NewReader(saveBody))
	saveReq.AddCookie(sessionCookie)
	// saveRecorder 捕获保存响应。
	saveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(saveRecorder, saveReq)
	assertOpenAPISuccessResponse(t, saveReq, saveRecorder)
	if strings.Contains(saveRecorder.Body.String(), secret) {
		t.Fatalf("保存响应包含请求头明文: %s", saveRecorder.Body.String())
	}
	// getReq 读取配置。
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/stock-item/external-stock", nil)
	getReq.AddCookie(sessionCookie)
	// getRecorder 捕获读取响应。
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	assertOpenAPISuccessResponse(t, getReq, getRecorder)
	if strings.Contains(getRecorder.Body.String(), secret) || !strings.Contains(getRecorder.Body.String(), `"name":"Authorization"`) {
		t.Fatalf("读取响应异常: %s", getRecorder.Body.String())
	}
	// checkReq 是一次会因外部地址不可达而停止的检查，响应仍是成功契约且不含秘密。
	checkReq := httptest.NewRequest(http.MethodPost, "/api/v1/items/acc1/stock-item/external-stock/check", strings.NewReader(`{"dry_run":true}`))
	checkReq.AddCookie(sessionCookie)
	// checkRecorder 捕获检查响应。
	checkRecorder := httptest.NewRecorder()
	handler.ServeHTTP(checkRecorder, checkReq)
	assertOpenAPISuccessResponse(t, checkReq, checkRecorder)
	if strings.Contains(checkRecorder.Body.String(), secret) {
		t.Fatalf("检查响应包含请求头明文: %s", checkRecorder.Body.String())
	}
}
