package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ExternalStockRepository 把外部库存应用端口适配到加密仓储。
type ExternalStockRepository struct {
	// store 提供归属查询和外部库存配置。
	store *db.Store
}

// NewExternalStockRepository 从商品依赖创建仓储适配器。
func NewExternalStockRepository(dependencies *ItemDependencies) *ExternalStockRepository {
	if dependencies == nil {
		return &ExternalStockRepository{}
	}
	return &ExternalStockRepository{store: dependencies.store}
}

// AccountOwned 判断账号属于指定用户，不读取凭证。
func (repository *ExternalStockRepository) AccountOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	if repository == nil || repository.store == nil || repository.store.Cookies == nil {
		return false, errors.New("外部库存账号仓储未初始化")
	}
	return repository.store.Cookies.ExistsOwned(ctx, userID, cookieID)
}

// ItemExists 判断账号下未删除商品是否存在。
func (repository *ExternalStockRepository) ItemExists(ctx context.Context, cookieID, itemID string) (bool, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return false, errors.New("外部库存商品仓储未初始化")
	}
	// itemErr 是商品详情查询错误；不存在按 false 返回。
	_, itemErr := repository.store.Items.Get(ctx, cookieID, itemID)
	if errors.Is(itemErr, db.ErrNotFound) {
		return false, nil
	}
	if itemErr != nil {
		return false, itemErr
	}
	return true, nil
}

// AccountUserID 返回账号所属用户。
func (repository *ExternalStockRepository) AccountUserID(ctx context.Context, cookieID string) (int64, error) {
	if repository == nil || repository.store == nil || repository.store.Cookies == nil {
		return 0, errors.New("外部库存账号仓储未初始化")
	}
	return repository.store.Cookies.GetOwnerID(ctx, cookieID)
}

// Enabled 返回商品是否启用外部库存。
func (repository *ExternalStockRepository) Enabled(ctx context.Context, cookieID, itemID string) (bool, error) {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return false, errors.New("外部库存存储未初始化")
	}
	return repository.store.ItemExternalStock.Enabled(ctx, cookieID, itemID)
}

// Get 返回不含请求头明文的配置。
func (repository *ExternalStockRepository) Get(ctx context.Context, cookieID, itemID string) (itemapp.ExternalStockConfig, error) {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return itemapp.ExternalStockConfig{}, errors.New("外部库存存储未初始化")
	}
	// row、err 是数据库中的非秘密配置。
	row, err := repository.store.ItemExternalStock.Get(ctx, cookieID, itemID)
	if err != nil {
		return itemapp.ExternalStockConfig{}, err
	}
	return externalStockConfigFromRow(row), nil
}

// Headers 解密请求头，只供出站调用。
func (repository *ExternalStockRepository) Headers(ctx context.Context, cookieID, itemID string) (map[string]string, error) {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return nil, errors.New("外部库存存储未初始化")
	}
	return repository.store.ItemExternalStock.Headers(ctx, cookieID, itemID)
}

// Save 加密请求头并保存配置。
func (repository *ExternalStockRepository) Save(ctx context.Context, config itemapp.ExternalStockConfig, headers map[string]string) error {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return errors.New("外部库存存储未初始化")
	}
	return repository.store.ItemExternalStock.Save(ctx, externalStockRowFromConfig(config), headers)
}

// SaveStatus 只更新最近一次检查状态。
func (repository *ExternalStockRepository) SaveStatus(ctx context.Context, cookieID, itemID string, config itemapp.ExternalStockConfig) error {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return errors.New("外部库存存储未初始化")
	}
	// row 只填状态列和定位键。
	row := externalStockRowFromConfig(config)
	row.CookieID = cookieID
	row.ItemID = itemID
	return repository.store.ItemExternalStock.SaveStatus(ctx, row)
}

// LocalItemStatus 从商品同步写入的 item_detail 读取 item_status。没有记录或没有该字段时 present 为 false。
func (repository *ExternalStockRepository) LocalItemStatus(ctx context.Context, cookieID, itemID string) (int, bool, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return 0, false, errors.New("外部库存商品仓储未初始化")
	}
	// row、err 是本地商品详情及读取错误。
	row, err := repository.store.Items.GetByCookieItem(ctx, cookieID, itemID)
	if errors.Is(err, db.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return localSyncedItemStatus(row.ItemDetail)
}

// localSyncedItemStatus 解析同步详情里的 item_status。正文其他字段不返回。
func localSyncedItemStatus(detail string) (int, bool, error) {
	if strings.TrimSpace(detail) == "" {
		return 0, false, nil
	}
	// payload 是商品同步保存的详情 JSON。
	var payload map[string]any
	if // err 是详情不是 JSON 时的解码错误，此时只当作没有状态码。
	err := json.Unmarshal([]byte(detail), &payload); err != nil {
		return 0, false, nil
	}
	// raw、present 是 item_status 原值及其是否存在。
	raw, present := payload["item_status"]
	if !present {
		return 0, false, nil
	}
	// typed 是 item_status 的 JSON 类型，数字和字符串都接受。
	switch typed := raw.(type) {
	case float64:
		return int(typed), true, nil
	case string:
		// parsed、err 是字符串状态码及其解析错误。
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false, nil
		}
		return parsed, true, nil
	default:
		return 0, false, nil
	}
}

// ListEnabled 返回已启用商品的非秘密配置。
func (repository *ExternalStockRepository) ListEnabled(ctx context.Context) ([]itemapp.ExternalStockConfig, error) {
	if repository == nil || repository.store == nil || repository.store.ItemExternalStock == nil {
		return nil, errors.New("外部库存存储未初始化")
	}
	// rows、err 是数据库中的启用配置。
	rows, err := repository.store.ItemExternalStock.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	// configs 是应用层配置。
	configs := make([]itemapp.ExternalStockConfig, 0, len(rows))
	// row 是一条启用配置。
	for _, row := range rows {
		configs = append(configs, externalStockConfigFromRow(row))
	}
	return configs, nil
}

// externalStockConfigFromRow 把数据库行转成应用配置，不包含请求头明文。
func externalStockConfigFromRow(row db.ItemExternalStockRow) itemapp.ExternalStockConfig {
	// config 是非秘密配置。
	config := itemapp.ExternalStockConfig{
		CookieID: row.CookieID, ItemID: row.ItemID, Enabled: row.Enabled, SourceURL: row.SourceURL, Method: row.Method,
		JSONPath: row.JSONPath, Timeout: time.Duration(row.TimeoutSeconds) * time.Second, UserAgent: row.UserAgent,
		Cron: row.CronExpr, Timezone: row.Timezone, Interval: time.Duration(row.IntervalMinutes) * time.Minute,
		DryRun: row.DryRun, SyncPlatformStock: row.SyncPlatformStock, StockCap: row.StockCap, HeaderNames: row.HeaderNames,
		LastAction: row.LastAction, LastStatus: row.LastStatus, LastError: row.LastError, BackoffClass: row.BackoffClass,
		LastAvailable: row.LastAvailable, AppliedAction: row.AppliedAction, AppliedAvailable: row.AppliedAvailable,
	}
	if row.LastCheckedUnix > 0 {
		config.LastChecked = time.Unix(row.LastCheckedUnix, 0)
	}
	if row.BackoffUntilUnix > 0 {
		config.BackoffUntil = time.Unix(row.BackoffUntilUnix, 0)
	}
	if row.AppliedUnix > 0 {
		config.AppliedAt = time.Unix(row.AppliedUnix, 0)
	}
	return config
}

// externalStockRowFromConfig 把应用配置转成数据库行。分钟和秒由时长截断得到。
func externalStockRowFromConfig(config itemapp.ExternalStockConfig) db.ItemExternalStockRow {
	// row 是待写入的数据库行。
	row := db.ItemExternalStockRow{
		CookieID: config.CookieID, ItemID: config.ItemID, Enabled: config.Enabled, SourceURL: config.SourceURL,
		Method: "GET", JSONPath: config.JSONPath, TimeoutSeconds: int(config.Timeout / time.Second), UserAgent: config.UserAgent,
		CronExpr: config.Cron, Timezone: config.Timezone, IntervalMinutes: int(config.Interval / time.Minute),
		DryRun: config.DryRun, SyncPlatformStock: config.SyncPlatformStock, StockCap: config.StockCap,
		HeaderNames: config.HeaderNames, LastAvailable: config.LastAvailable, LastAction: config.LastAction,
		LastStatus: config.LastStatus, LastError: config.LastError, BackoffClass: config.BackoffClass,
		AppliedAction: config.AppliedAction, AppliedAvailable: config.AppliedAvailable,
	}
	if !config.LastChecked.IsZero() {
		row.LastCheckedUnix = config.LastChecked.Unix()
	}
	if !config.BackoffUntil.IsZero() {
		row.BackoffUntilUnix = config.BackoffUntil.Unix()
	}
	if !config.AppliedAt.IsZero() {
		row.AppliedUnix = config.AppliedAt.Unix()
	}
	return row
}

// 确保适配器实现应用层仓储端口。
var _ itemapp.ExternalStockRepository = (*ExternalStockRepository)(nil)
