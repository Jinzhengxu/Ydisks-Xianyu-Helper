package mtop

import (
	"context"
	"errors"
	"strings"
)

// ErrItemMultiSpecExternalQuantity 表示不能把一个外部库存数写进多规格商品的每一行。
var ErrItemMultiSpecExternalQuantity = errors.New("多规格商品不能按单一外部库存改写各规格库存")

// itemStatusOnSale 是 editDetail/edit 中 itemStatus 表示“在售”的取值；下架为 "-2"。
const itemStatusOnSale = "0"

// ItemRelistResult 是一次“售出后重新上架”调用的结果。
type ItemRelistResult struct {
	// ItemID 是被处理的商品 ID。
	ItemID string
	// AlreadyOnSale 表示读取详情时商品已在售，未提交 edit。
	AlreadyOnSale bool
	// RestoredQuantity 表示库存为 0 的商品被补回了最小库存（单规格为 1，多规格为各规格行 1）。
	RestoredQuantity bool
	// RawStatus 是 editDetail 返回的原始 itemStatus，只用于诊断，不含 Cookie。
	RawStatus string
	// UpdatedCookies 是请求累计更新后的 Cookie；为空表示没有变化。
	UpdatedCookies string
}

// RelistItem 以最新 editDetail 为底，把商品状态改回在售并全量提交。固定最多两次平台请求（读取 + 提交），不重试。
// 商品已在售且有库存时只读取、不提交；类目不支持网页端编辑时返回 ErrItemEditUnsupported 且不修改任何数据。
// 售出后库存为 0 的商品会补回最小库存（单规格 1；多规格全部规格行为 0 时每行 1），否则平台会拒绝上架。
func (c *ClientImpl) RelistItem(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error) {
	// id 是规范化后的商品 ID。
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, ErrItemStockInvalid
	}
	// detail、err 保存提交前读取的最新快照。
	detail, err := c.FetchItemEditDetail(ctx, cookiesStr, id)
	if err != nil {
		return nil, err
	}
	if detail.ItemID != id {
		return nil, ErrItemEditDetailUnusable
	}
	// result 是调用结果；读取阶段拿到的 Cookie 更新始终带回。
	result := &ItemRelistResult{ItemID: id, UpdatedCookies: detail.UpdatedCookies, RawStatus: strings.TrimSpace(detail.Status)}
	if strings.TrimSpace(detail.Status) == itemStatusOnSale && detail.Quantity > 0 {
		result.AlreadyOnSale = true
		return result, nil
	}
	// payload、restored 是改回在售后的全量覆盖载荷及是否补回了库存。
	payload, restored, err := buildItemRelistPayload(detail, id)
	if err != nil {
		return nil, err
	}
	// _、_、cookies、err 忽略 ret 文本与字段名，只保留累计更新后的 Cookie。
	_, _, cookies, err := c.submitItemEdit(ctx, cookiesStr, detail, id, payload)
	if err != nil {
		return nil, err
	}
	result.RestoredQuantity = restored
	result.UpdatedCookies = cookies
	return result, nil
}

// buildItemRelistPayload 在快照全量载荷上把 itemStatus 改为在售，并在库存为 0 时补回最小库存。
func buildItemRelistPayload(detail *ItemEditDetail, itemID string) (map[string]any, bool, error) {
	if detail.ItemID != itemID {
		return nil, false, ErrItemEditDetailUnusable
	}
	// payload 是深拷贝后的全量载荷。
	payload, err := cloneItemEditPayload(detail, itemID)
	if err != nil {
		return nil, false, err
	}
	payload["itemStatus"] = itemStatusOnSale
	// restored 表示是否补回了库存。
	restored := false
	if !detail.MultiSpec {
		if detail.Quantity <= 0 {
			payload["quantity"] = typedLike(detail.raw["quantity"], 1)
			restored = true
		}
		return payload, restored, nil
	}
	// rows 是载荷里的规格行。
	rows, _ := payload["itemSkuList"].([]any)
	if detail.Quantity <= 0 {
		// row 是载荷里的当前规格行，全部为 0 时逐行补 1。
		for _, row := range rows {
			// fields 是规格行的对象形式，可直接修改其库存字段。
			fields := mapFromAny(row)
			fields["quantity"] = typedLike(fields["quantity"], 1)
		}
		restored = true
	}
	// exists 是载荷中是否已存在该字段。
	if _, exists := payload["quantity"]; !exists {
		payload["quantity"] = "1"
	}
	// exists 是载荷中是否已存在该字段。
	if _, exists := payload["itemProperties"]; !exists {
		payload["itemProperties"] = rebuildItemProperties(rows)
	}
	return payload, restored, nil
}

// RelistItemWithQuantity 以最新详情为底重新上架，并把单规格库存设为 quantity。
// 商品已在售且库存已经等于 quantity 时只读取、不提交。多规格商品直接拒绝，不修改平台数据。
// 固定最多两次平台请求，失败不重试。
func (c *ClientImpl) RelistItemWithQuantity(ctx context.Context, cookiesStr, itemID string, quantity int) (*ItemRelistResult, error) {
	if quantity < 1 || quantity > itemStockMax {
		return nil, ErrItemStockInvalid
	}
	// id 是规范化后的商品 ID。
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, ErrItemStockInvalid
	}
	// detail、err 保存提交前读取的最新快照。
	detail, err := c.FetchItemEditDetail(ctx, cookiesStr, id)
	if err != nil {
		return nil, err
	}
	if detail.ItemID != id {
		return nil, ErrItemEditDetailUnusable
	}
	if detail.MultiSpec {
		return nil, ErrItemMultiSpecExternalQuantity
	}
	// result 是调用结果；读取阶段拿到的 Cookie 更新始终带回。
	result := &ItemRelistResult{ItemID: id, UpdatedCookies: detail.UpdatedCookies, RawStatus: strings.TrimSpace(detail.Status)}
	if strings.TrimSpace(detail.Status) == itemStatusOnSale && detail.Quantity == quantity {
		result.AlreadyOnSale = true
		return result, nil
	}
	// payload 是改回在售并写入指定库存的全量覆盖载荷。
	payload, _, err := buildItemRelistPayload(detail, id)
	if err != nil {
		return nil, err
	}
	payload["itemStatus"] = itemStatusOnSale
	payload["quantity"] = typedLike(detail.raw["quantity"], quantity)
	// cookies 是提交后累计更新的 Cookie。
	_, _, cookies, err := c.submitItemEdit(ctx, cookiesStr, detail, id, payload)
	if err != nil {
		return nil, err
	}
	result.RestoredQuantity = detail.Quantity != quantity
	result.UpdatedCookies = cookies
	return result, nil
}

// editDetailAlreadyOnSale 判断编辑详情的 itemStatus 是否已经表示在售。
// 真机在售商品返回 "1"，提交在售和模拟器读回使用 "0"。这两种都不需要再提交编辑。
func editDetailAlreadyOnSale(status string) bool {
	switch strings.TrimSpace(status) {
	case "0", "1":
		return true
	default:
		return false
	}
}

// EnsureItemOnSale 把原商品置于在售。已在售时只读取详情并成功返回，不提交编辑，因此不改价格、库存和描述。
// 不在售或状态码无法识别时，用当前详情原样回填，只把 itemStatus 写成在售值 "0"。固定最多两次平台请求，失败不重试。
func (c *ClientImpl) EnsureItemOnSale(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error) {
	// id 是规范化后的原商品 ID，不会另建商品。
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, ErrItemStockInvalid
	}
	// detail、err 是提交前读取的最新快照。读取失败时不提交。
	detail, err := c.FetchItemEditDetail(ctx, cookiesStr, id)
	if err != nil {
		return nil, err
	}
	if detail.ItemID != id {
		return nil, ErrItemEditDetailUnusable
	}
	// result 带回原始状态码和读取阶段的 Cookie 更新。
	result := &ItemRelistResult{ItemID: id, UpdatedCookies: detail.UpdatedCookies, RawStatus: strings.TrimSpace(detail.Status)}
	if editDetailAlreadyOnSale(detail.Status) {
		result.AlreadyOnSale = true
		return result, nil
	}
	// payload 是原详情的全量副本，随后只改在售状态。
	payload, err := cloneItemEditPayload(detail, id)
	if err != nil {
		return nil, err
	}
	payload["itemStatus"] = itemStatusOnSale
	// cookies 是提交后累计更新的 Cookie。
	_, _, cookies, err := c.submitItemEdit(ctx, cookiesStr, detail, id, payload)
	if err != nil {
		return nil, err
	}
	result.UpdatedCookies = cookies
	return result, nil
}

// ItemEnsureOnSaleClient 是外部库存把原商品置于在售的可选能力，避免扩大所有客户端替身。
type ItemEnsureOnSaleClient interface {
	// EnsureItemOnSale 保证原商品在售。已在售时不提交编辑。
	EnsureItemOnSale(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error)
}

// 确保真实客户端实现外部库存的幂等上架。
var _ ItemEnsureOnSaleClient = (*ClientImpl)(nil)

// ItemRelistQuantityClient 是按指定库存重新上架的可选扩展，避免扩大所有客户端替身。
type ItemRelistQuantityClient interface {
	// RelistItemWithQuantity 把原商品改回在售并设置单规格库存，固定最多两次平台请求。
	RelistItemWithQuantity(ctx context.Context, cookiesStr, itemID string, quantity int) (*ItemRelistResult, error)
}

// 确保真实客户端实现按库存重新上架。
var _ ItemRelistQuantityClient = (*ClientImpl)(nil)

// ItemRelistClient 是商品重新上架的可选扩展接口，避免扩大所有 MTOP 客户端替身的基础契约。
type ItemRelistClient interface {
	// RelistItem 以最新详情为底把商品改回在售，固定最多两次平台请求。
	RelistItem(ctx context.Context, cookiesStr, itemID string) (*ItemRelistResult, error)
}

// 确保真实客户端实现重新上架扩展接口。
var _ ItemRelistClient = (*ClientImpl)(nil)
