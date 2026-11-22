package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
)

// ItemExternalStockPort 是商品外部库存配置和检查用例。响应不得包含请求头明文。
type ItemExternalStockPort interface {
	// Get 返回不含请求头明文的配置。
	Get(context.Context, int64, string, string) (itemapp.ExternalStockConfig, error)
	// Save 保存配置和替换后的请求头。请求头明文只进入应用服务。
	Save(context.Context, int64, itemapp.ExternalStockConfig, []itemapp.ExternalStockHeader) error
	// Check 立即检查一个商品。手动触发可以越过退避并强制演练。
	Check(context.Context, int64, string, string, itemapp.ExternalStockCheckOptions) (itemapp.ExternalStockCheckResult, error)
}
