package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// externalStockHeaderScope 是商品外部库存请求头的加密用途，与账号 Cookie 分开，避免跨字段解密。
const externalStockHeaderScope = "item-external-stock-headers"

// externalStockHeader 是加密前的一条请求头。Value 是明文，不能写入日志或普通查询结果。
type externalStockHeader struct {
	// Name 是规范化后的请求头名称。
	Name string `json:"name"`
	// Value 是明文值，只在加密和解密的短暂内存中存在。
	Value string `json:"value"`
}

// ItemExternalStockRow 是商品外部库存配置的非秘密视图。请求头只保留名称。
type ItemExternalStockRow struct {
	// CookieID 是商品所属账号。
	CookieID string
	// ItemID 是闲鱼商品 ID。
	ItemID string
	// Enabled 表示启用外部库存同步。
	Enabled bool
	// SourceURL 是外部库存地址。
	SourceURL string
	// Method 固定为 GET。
	Method string
	// JSONPath 是可用库存的点分路径。
	JSONPath string
	// TimeoutSeconds 是外部请求超时秒数。
	TimeoutSeconds int
	// UserAgent 是自定义 User-Agent，空表示由应用层使用默认浏览器标识。
	UserAgent string
	// CronExpr 是五段 cron。
	CronExpr string
	// Timezone 是 IANA 时区。
	Timezone string
	// IntervalMinutes 是周期检查间隔，0 表示不按周期检查。
	IntervalMinutes int
	// DryRun 表示只记录将要执行的动作。
	DryRun bool
	// SyncPlatformStock 表示把平台库存同步到外部可用数。
	SyncPlatformStock bool
	// StockCap 是同步库存上限。
	StockCap int
	// HeaderNames 是已保存请求头名称，按保存顺序排列，不含值。
	HeaderNames []string
	// LastCheckedUnix 是上次检查的 Unix 秒，0 表示尚未检查。
	LastCheckedUnix int64
	// LastAvailable 是上次成功读数；nil 表示没有。
	LastAvailable *int
	// LastAction 是上次动作。
	LastAction string
	// LastStatus 是上次状态。
	LastStatus string
	// LastError 是上次可展示错误。
	LastError string
	// BackoffUntilUnix 是退避结束的 Unix 秒。
	BackoffUntilUnix int64
	// BackoffClass 是 all、relist 或空。
	BackoffClass string
	// AppliedAction 是最近一次真正成功的下架或重新上架。
	AppliedAction string
	// AppliedAvailable 是那次成功时的外部可用库存；nil 表示还没有成功记录。
	AppliedAvailable *int
	// AppliedUnix 是那次成功的 Unix 秒，0 表示还没有。
	AppliedUnix int64
	// Found 表示数据库里已经有独立配置。
	Found bool
}

// ItemExternalStock 保存每个商品的外部库存来源。请求头密文只在本类型内解密。
type ItemExternalStock struct {
	// DB 是仓储连接池。
	DB *sql.DB
	// Dialect 决定 UPSERT 语法。
	Dialect Dialect
	// codec 加密请求头；未配置数据密钥时按既有规则保留明文兼容。
	codec *secretCodec
}

// defaultItemExternalStockRow 返回没有独立记录时的默认配置，默认关闭。
func defaultItemExternalStockRow(cookieID, itemID string) ItemExternalStockRow {
	return ItemExternalStockRow{
		CookieID: cookieID, ItemID: itemID, Method: "GET", JSONPath: "available", TimeoutSeconds: 20,
		CronExpr: "1 0 * * *", Timezone: "Asia/Shanghai", IntervalMinutes: 30, StockCap: 999999,
	}
}

// Get 读取非秘密配置。没有记录时返回默认值且 Found 为 false。
func (repo *ItemExternalStock) Get(ctx context.Context, cookieID, itemID string) (ItemExternalStockRow, error) {
	// stored、headers、err 是数据库行、解密后的请求头和读取错误。请求头只用来提取名称。
	stored, headers, err := repo.load(ctx, cookieID, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultItemExternalStockRow(cookieID, itemID), nil
	}
	if err != nil {
		return ItemExternalStockRow{}, err
	}
	stored.HeaderNames = headerNames(headers)
	return stored, nil
}

// Headers 解密请求头明文，供出站调用使用。没有记录时返回空映射。
func (repo *ItemExternalStock) Headers(ctx context.Context, cookieID, itemID string) (map[string]string, error) {
	// stored 被忽略，这里只需要解密后的请求头。
	_, headers, err := repo.load(ctx, cookieID, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	// values 是名称到明文的映射，调用方不得记录。
	values := make(map[string]string, len(headers))
	// header 是一条已解密的请求头。
	for _, header := range headers {
		values[header.Name] = header.Value
	}
	return values, nil
}

// Enabled 返回商品是否启用外部库存。没有记录时为 false。
func (repo *ItemExternalStock) Enabled(ctx context.Context, cookieID, itemID string) (bool, error) {
	if repo == nil || repo.DB == nil {
		return false, errors.New("外部库存存储未初始化")
	}
	// enabled 是库存开关的 0/1 值。
	var enabled int
	// queryErr 是读取开关的错误；没有记录按未启用处理。
	queryErr := repo.DB.QueryRowContext(ctx, `SELECT enabled FROM item_external_stock WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(&enabled)
	if errors.Is(queryErr, sql.ErrNoRows) {
		return false, nil
	}
	if queryErr != nil {
		return false, queryErr
	}
	return enabled == 1, nil
}

// Save 加密请求头并写入完整配置。headers 的键必须覆盖 row.HeaderNames。
func (repo *ItemExternalStock) Save(ctx context.Context, row ItemExternalStockRow, headers map[string]string) error {
	if repo == nil || repo.DB == nil {
		return errors.New("外部库存存储未初始化")
	}
	// ordered 按名称顺序组装待加密的请求头，避免 map 迭代顺序影响密文以外的展示。
	ordered := make([]externalStockHeader, 0, len(row.HeaderNames))
	// name 是按保存顺序排列的请求头名称。
	for _, name := range row.HeaderNames {
		ordered = append(ordered, externalStockHeader{Name: name, Value: headers[name]})
	}
	// encoded、err 是请求头 JSON 及其编码错误。
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return err
	}
	// ciphertext 是加密后的请求头；空列表保存空串，不产生无意义密文。
	ciphertext := ""
	if len(ordered) > 0 {
		ciphertext, err = repo.encryptHeaders(row.CookieID, row.ItemID, string(encoded))
		if err != nil {
			return err
		}
	}
	// statement 按方言生成插入或更新。
	statement := `INSERT INTO item_external_stock (
		cookie_id, item_id, enabled, source_url, http_method, headers_ciphertext, json_path, timeout_seconds,
		user_agent, cron_expr, timezone, interval_minutes, dry_run, sync_platform_stock, stock_cap, updated_at_unix)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectUpsert(repo.Dialect, []string{"cookie_id", "item_id"}, map[string]string{
		"enabled": "EXCLUDED.enabled", "source_url": "EXCLUDED.source_url", "http_method": "EXCLUDED.http_method",
		"headers_ciphertext": "EXCLUDED.headers_ciphertext", "json_path": "EXCLUDED.json_path",
		"timeout_seconds": "EXCLUDED.timeout_seconds", "user_agent": "EXCLUDED.user_agent",
		"cron_expr": "EXCLUDED.cron_expr", "timezone": "EXCLUDED.timezone", "interval_minutes": "EXCLUDED.interval_minutes",
		"dry_run": "EXCLUDED.dry_run", "sync_platform_stock": "EXCLUDED.sync_platform_stock",
		"stock_cap": "EXCLUDED.stock_cap", "updated_at_unix": "EXCLUDED.updated_at_unix",
	})
	// execErr 是写入配置的数据库错误。
	_, execErr := repo.DB.ExecContext(ctx, statement,
		row.CookieID, row.ItemID, externalStockBool(row.Enabled), row.SourceURL, "GET", ciphertext, row.JSONPath, row.TimeoutSeconds,
		row.UserAgent, row.CronExpr, row.Timezone, row.IntervalMinutes, externalStockBool(row.DryRun), externalStockBool(row.SyncPlatformStock),
		row.StockCap, time.Now().Unix())
	return execErr
}

// SaveStatus 只更新最近一次检查结果，不改地址和请求头。
func (repo *ItemExternalStock) SaveStatus(ctx context.Context, row ItemExternalStockRow) error {
	if repo == nil || repo.DB == nil {
		return errors.New("外部库存存储未初始化")
	}
	// available 是可空的上次读数。
	var available any
	if row.LastAvailable != nil {
		available = *row.LastAvailable
	}
	// appliedAvailable 是可空的上次成功库存。
	var appliedAvailable any
	if row.AppliedAvailable != nil {
		appliedAvailable = *row.AppliedAvailable
	}
	// execErr 是状态更新错误；没有配置行时更新零行，不另建记录。
	_, execErr := repo.DB.ExecContext(ctx, `UPDATE item_external_stock SET last_checked_unix=?, last_available=?, last_action=?, last_status=?, last_error=?, backoff_until_unix=?, backoff_class=?, applied_action=?, applied_available=?, applied_unix=? WHERE cookie_id=? AND item_id=?`,
		row.LastCheckedUnix, available, row.LastAction, row.LastStatus, row.LastError, row.BackoffUntilUnix, row.BackoffClass, row.AppliedAction, appliedAvailable, row.AppliedUnix, row.CookieID, row.ItemID)
	return execErr
}

// ListEnabled 返回全部已启用商品的非秘密配置。
func (repo *ItemExternalStock) ListEnabled(ctx context.Context) ([]ItemExternalStockRow, error) {
	if repo == nil || repo.DB == nil {
		return nil, errors.New("外部库存存储未初始化")
	}
	// rows、queryErr 是启用配置的游标及查询错误。
	rows, queryErr := repo.DB.QueryContext(ctx, externalStockSelectSQL+` WHERE enabled=1 ORDER BY cookie_id, item_id`)
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()
	// listed 是本轮调度可见的配置。
	listed := []ItemExternalStockRow{}
	for rows.Next() {
		// row、headers、scanErr 是一行配置、解密后的请求头和扫描错误。名称之外的明文随即丢弃。
		row, headers, scanErr := repo.scanLoaded(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		row.HeaderNames = headerNames(headers)
		listed = append(listed, row)
	}
	return listed, rows.Err()
}

// externalStockSelectSQL 选出配置列。headers_ciphertext 只在仓储内部解密。
const externalStockSelectSQL = `SELECT cookie_id, item_id, enabled, source_url, http_method, headers_ciphertext, json_path,
	timeout_seconds, user_agent, cron_expr, timezone, interval_minutes, dry_run, sync_platform_stock, stock_cap,
	last_checked_unix, last_available, last_action, last_status, last_error, backoff_until_unix, backoff_class,
	applied_action, applied_available, applied_unix
	FROM item_external_stock`

// load 读取一行并解密请求头。没有记录时返回 sql.ErrNoRows。
func (repo *ItemExternalStock) load(ctx context.Context, cookieID, itemID string) (ItemExternalStockRow, []externalStockHeader, error) {
	if repo == nil || repo.DB == nil {
		return ItemExternalStockRow{}, nil, errors.New("外部库存存储未初始化")
	}
	// row 是查询游标，扫描后即可关闭。
	row := repo.DB.QueryRowContext(ctx, externalStockSelectSQL+` WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return repo.scanLoaded(row)
}

// scanLoaded 扫描一行并解密请求头。解密失败不会把密文当成可用请求头。
func (repo *ItemExternalStock) scanLoaded(scanner externalStockScanner) (ItemExternalStockRow, []externalStockHeader, error) {
	// row、ciphertext、scanErr 是非秘密字段、请求头密文和扫描错误。
	row, ciphertext, scanErr := scanExternalStock(scanner)
	if scanErr != nil {
		return ItemExternalStockRow{}, nil, scanErr
	}
	// plaintext、err 是解密后的请求头 JSON。
	plaintext, err := repo.decryptHeaders(row.CookieID, row.ItemID, ciphertext)
	if err != nil {
		return ItemExternalStockRow{}, nil, err
	}
	// headers 是解析后的请求头明文。
	headers, err := decodeExternalStockHeaders(plaintext)
	if err != nil {
		return ItemExternalStockRow{}, nil, err
	}
	return row, headers, nil
}

// externalStockScanner 是 QueryRow 和 Rows 都能扫描的最小接口。
type externalStockScanner interface {
	// Scan 把当前行写入目标。
	Scan(dest ...any) error
}

// scanExternalStock 扫描一行。ciphertext 仍是库中的原值，由调用方解密。
func scanExternalStock(scanner externalStockScanner) (ItemExternalStockRow, string, error) {
	// enabled、dryRun、syncStock 是 0/1 整型列。
	var enabled, dryRun, syncStock int
	// ciphertext 是请求头密文或历史明文。
	var ciphertext string
	// available、appliedAvailable 是可空的上次读数和上次成功库存。
	var available, appliedAvailable sql.NullInt64
	// row 是扫描中的非秘密字段。
	var row ItemExternalStockRow
	// scanErr 是列扫描错误。
	scanErr := scanner.Scan(&row.CookieID, &row.ItemID, &enabled, &row.SourceURL, &row.Method, &ciphertext, &row.JSONPath,
		&row.TimeoutSeconds, &row.UserAgent, &row.CronExpr, &row.Timezone, &row.IntervalMinutes, &dryRun, &syncStock, &row.StockCap,
		&row.LastCheckedUnix, &available, &row.LastAction, &row.LastStatus, &row.LastError, &row.BackoffUntilUnix, &row.BackoffClass,
		&row.AppliedAction, &appliedAvailable, &row.AppliedUnix)
	if scanErr != nil {
		return ItemExternalStockRow{}, "", scanErr
	}
	row.Enabled = enabled == 1
	row.DryRun = dryRun == 1
	row.SyncPlatformStock = syncStock == 1
	row.Found = true
	if available.Valid {
		// count 是上次可用库存。
		count := int(available.Int64)
		row.LastAvailable = &count
	}
	if appliedAvailable.Valid {
		// applied 是上次成功时的可用库存。
		applied := int(appliedAvailable.Int64)
		row.AppliedAvailable = &applied
	}
	return row, ciphertext, nil
}

// decodeExternalStockHeaders 在调用方完成解密后解析 JSON。密文不能直接解析。
func decodeExternalStockHeaders(plaintext string) ([]externalStockHeader, error) {
	if strings.TrimSpace(plaintext) == "" {
		return nil, nil
	}
	if strings.HasPrefix(plaintext, encryptedValuePrefix) {
		return nil, errors.New("外部库存请求头尚未解密")
	}
	// headers 是明文请求头列表。
	var headers []externalStockHeader
	if // err 是明文请求头 JSON 无法解析。
	err := json.Unmarshal([]byte(plaintext), &headers); err != nil {
		return nil, errors.New("外部库存请求头无法读取")
	}
	return headers, nil
}

// encryptHeaders 用商品身份绑定请求头密文。value 是明文 JSON。
func (repo *ItemExternalStock) encryptHeaders(cookieID, itemID, value string) (string, error) {
	if repo.codec == nil {
		return value, nil
	}
	return repo.codec.encrypt(externalStockHeaderScope, cookieID+"\x00"+itemID, value)
}

// decryptHeaders 解密请求头。错误密钥不会返回密文。
func (repo *ItemExternalStock) decryptHeaders(cookieID, itemID, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if repo.codec == nil {
		if strings.HasPrefix(value, encryptedValuePrefix) {
			return "", errors.New("数据库包含加密请求头，但 XIANYU_DATA_KEY 未配置")
		}
		return value, nil
	}
	return repo.codec.decrypt(externalStockHeaderScope, cookieID+"\x00"+itemID, value)
}

// headerNames 按保存顺序提取请求头名称。
func headerNames(headers []externalStockHeader) []string {
	// names 是展示用名称。
	names := make([]string, 0, len(headers))
	// header 是一条请求头。
	for _, header := range headers {
		names = append(names, header.Name)
	}
	return names
}

// externalStockBool 把开关写成三方言通用的 0/1。
func externalStockBool(value bool) int {
	if value {
		return 1
	}
	return 0
}
