package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestItemExternalStockEncryptsHeaders 验证请求头加密落库，读取配置时只返回名称。
func TestItemExternalStockEncryptsHeaders(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "external-stock-test-key")
	// database 是隔离的 SQLite 文件。
	database, _, openErr := Open(context.Background(), filepath.Join(t.TempDir(), "external-stock.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// store 在密钥生效后创建，保证新写入的请求头被加密。
	store := NewStore(database, DialectSQLite)
	// ctx 是本测试的数据库上下文。
	ctx := context.Background()
	if // createErr 是创建测试用户失败。
	_, createErr := store.Users.Create(ctx, "stock-owner", "stock@example.com", "password"); createErr != nil {
		t.Fatal(createErr)
	}
	// owner 是测试账号所属用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "stock-owner")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if // saveErr 是保存测试账号失败。
	saveErr := store.Cookies.Save(ctx, "stock-account", "unb=1", owner.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// missing 是没有配置时的默认值，必须关闭。
	missing, missingErr := store.ItemExternalStock.Get(ctx, "stock-account", "item-1")
	if missingErr != nil || missing.Found || missing.Enabled {
		t.Fatalf("默认配置异常: %+v err=%v", missing, missingErr)
	}
	// secret 是不能出现在配置视图和日志里的请求头值。
	secret := "Bearer super-secret-token"
	// row 是启用中的外部库存配置。
	row := ItemExternalStockRow{
		CookieID: "stock-account", ItemID: "item-1", Enabled: true, SourceURL: "https://stock.example/slots",
		Method: "GET", JSONPath: "data.available", TimeoutSeconds: 20, CronExpr: "1 0 * * *",
		Timezone: "Asia/Shanghai", IntervalMinutes: 30, StockCap: 1, HeaderNames: []string{"Authorization"},
	}
	if // saveErr 是写入外部库存配置失败。
	saveErr := store.ItemExternalStock.Save(ctx, row, map[string]string{"Authorization": secret}); saveErr != nil {
		t.Fatal(saveErr)
	}
	// stored 是读回的非秘密配置。
	stored, storedErr := store.ItemExternalStock.Get(ctx, "stock-account", "item-1")
	if storedErr != nil || !stored.Enabled || len(stored.HeaderNames) != 1 || stored.HeaderNames[0] != "Authorization" {
		t.Fatalf("读回配置异常: %+v err=%v", stored, storedErr)
	}
	// ciphertext 是库中的请求头列，必须是密文且不含明文。
	var ciphertext string
	if // scanErr 是读取请求头密文失败。
	scanErr := store.DB.QueryRowContext(ctx, `SELECT headers_ciphertext FROM item_external_stock WHERE cookie_id=? AND item_id=?`, "stock-account", "item-1").Scan(&ciphertext); scanErr != nil {
		t.Fatal(scanErr)
	}
	if !strings.HasPrefix(ciphertext, encryptedValuePrefix) || strings.Contains(ciphertext, secret) {
		t.Fatal("请求头未加密")
	}
	// headers 是解密后的出站明文。
	headers, headerErr := store.ItemExternalStock.Headers(ctx, "stock-account", "item-1")
	if headerErr != nil || headers["Authorization"] != secret {
		t.Fatalf("解密失败 headers=%v err=%v", headers, headerErr)
	}
	// enabled、enabledErr 是自动上架需要的开关。
	enabled, enabledErr := store.ItemExternalStock.Enabled(ctx, "stock-account", "item-1")
	if enabledErr != nil || !enabled {
		t.Fatalf("enabled=%v err=%v", enabled, enabledErr)
	}
	// count 是要写回的可用库存。
	count := 0
	row.LastCheckedUnix = 10
	row.LastAvailable = &count
	row.LastAction = "unpublish"
	row.LastStatus = "ok"
	if // statusErr 是写入检查状态失败。
	statusErr := store.ItemExternalStock.SaveStatus(ctx, row); statusErr != nil {
		t.Fatal(statusErr)
	}
	// after 是状态更新后的配置，请求头应仍在。
	after, afterErr := store.ItemExternalStock.Get(ctx, "stock-account", "item-1")
	if afterErr != nil || after.LastAction != "unpublish" || after.LastAvailable == nil || *after.LastAvailable != 0 || len(after.HeaderNames) != 1 {
		t.Fatalf("状态更新异常: %+v err=%v", after, afterErr)
	}
}
