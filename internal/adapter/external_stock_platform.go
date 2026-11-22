package adapter

import (
	"context"
	"errors"
	"strings"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// Inspect 读取商品当前在售状态和库存，不提交修改。
func (p *ItemPublishPort) Inspect(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.ListingSnapshot, error) {
	// paced 在后台检查时拉长账号级 MTOP 间隔。
	paced := externalStockContext(ctx)
	// snapshot 由平台详情闭包写入。
	var snapshot itemapp.ListingSnapshot
	// persistErr、callErr 分别是会话写回错误和平台读取错误。
	_, callErr := p.runStockCall(paced, userID, cookieID, 30*time.Second, func(callCtx context.Context, client mtop.ItemStockClient, cookies string) (string, error) {
		// detail、err 是最新编辑详情。
		detail, err := client.FetchItemEditDetail(callCtx, cookies, itemID)
		if err != nil {
			return "", err
		}
		if detail == nil {
			return "", errors.New("闲鱼商品编辑详情接口未返回结果")
		}
		snapshot = listingFromDetail(detail)
		return detail.UpdatedCookies, nil
	})
	if callErr != nil {
		return itemapp.ListingSnapshot{}, mapExternalStockPlatformError(callErr)
	}
	return snapshot, nil
}

// Unpublish 下架原商品。调用既有下架端口，不发布新商品。
func (p *ItemPublishPort) Unpublish(ctx context.Context, userID int64, cookieID, itemID string) error {
	// outcome、err 是平台下架结果。
	_, err := p.DownShelf(externalStockContext(ctx), userID, cookieID, itemID)
	if err != nil {
		return mapExternalStockPlatformError(err)
	}
	return nil
}

// Relist 重新上架原商品。sync 为真时写入指定单规格库存。
func (p *ItemPublishPort) Relist(ctx context.Context, userID int64, cookieID, itemID string, quantity int, sync bool) error {
	if !sync {
		// outcome、err 是沿用既有最小库存补回的重新上架结果。
		_, err := p.RelistItem(externalStockContext(ctx), userID, cookieID, itemID)
		return mapExternalStockPlatformError(err)
	}
	return p.SyncStock(ctx, userID, cookieID, itemID, quantity)
}

// SyncStock 把单规格库存改成 quantity，并保证商品处于在售。固定最多两次平台请求。
func (p *ItemPublishPort) SyncStock(ctx context.Context, userID int64, cookieID, itemID string, quantity int) error {
	// paced 是可能带后台节流的上下文。
	paced := externalStockContext(ctx)
	// persistErr、callErr 分别是会话写回错误和平台提交错误。
	persistErr, callErr := p.runItemEditCall(paced, userID, cookieID, 90*time.Second, itemapp.ErrExternalStockSession, func(callCtx context.Context, client mtop.Client, cookies string) (string, error) {
		// relister、ok 是按指定库存重新上架的可选能力。
		relister, ok := client.(mtop.ItemRelistQuantityClient)
		if !ok {
			return "", itemapp.ErrExternalStockEditUnsupported
		}
		// result、err 是平台提交结果。
		result, err := relister.RelistItemWithQuantity(callCtx, cookies, itemID, quantity)
		if err != nil {
			return "", err
		}
		if result == nil {
			return "", errors.New("闲鱼商品重新上架未返回结果")
		}
		return result.UpdatedCookies, nil
	}, mapExternalStockPlatformError)
	if callErr != nil {
		return callErr
	}
	if persistErr != nil && p.logger != nil {
		p.logger.WarnContext(ctx, "外部库存同步后保存账号会话失败", "account", cookieID, "error", persistErr)
	}
	return nil
}

// externalStockContext 在应用层标记后台任务时启用 MTOP 后台节流。
func externalStockContext(ctx context.Context) context.Context {
	if itemapp.ExternalStockBackground(ctx) {
		return mtop.WithBackgroundPacing(ctx)
	}
	return ctx
}

// listingFromDetail 把编辑详情的 itemStatus 映射为在售、下架或未知。
func listingFromDetail(detail *mtop.ItemEditDetail) itemapp.ListingSnapshot {
	// state 默认未知，只有明确的在售和下架取值才会放行后续动作。
	state := itemapp.ListingUnknown
	switch strings.TrimSpace(detail.Status) {
	case "0":
		state = itemapp.ListingOnSale
	case "-2":
		state = itemapp.ListingOffSale
	}
	return itemapp.ListingSnapshot{State: state, Quantity: detail.Quantity, QuantityKnown: true, MultiSpec: detail.MultiSpec}
}

// mapExternalStockPlatformError 把平台错误收成外部库存可退避的哨兵错误。
func mapExternalStockPlatformError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, itemapp.ErrExternalStockEditUnsupported), errors.Is(err, itemapp.ErrExternalStockRisk), errors.Is(err, itemapp.ErrExternalStockSession), errors.Is(err, itemapp.ErrExternalStockPlatform), errors.Is(err, itemapp.ErrExternalStockMultiSpec), errors.Is(err, itemapp.ErrExternalStockUnknownState):
		return err
	case errors.Is(err, mtop.ErrItemEditUnsupported), errors.Is(err, itemapp.ErrStockEditUnsupported), errors.Is(err, itemapp.ErrRelistEditUnsupported):
		return itemapp.ErrExternalStockEditUnsupported
	case errors.Is(err, mtop.ErrItemMultiSpecExternalQuantity):
		return itemapp.ErrExternalStockMultiSpec
	case errors.Is(err, itemapp.ErrStockRiskVerification), errors.Is(err, itemapp.ErrRelistRiskVerification), mtop.IsRiskVerificationErr(err):
		return itemapp.ErrExternalStockRisk
	case errors.Is(err, itemapp.ErrStockSessionExpired), errors.Is(err, itemapp.ErrRelistSessionExpired), errors.Is(err, itemapp.ErrDownShelfCredentialChanged), mtop.IsSessionExpiredErr(err):
		return itemapp.ErrExternalStockSession
	default:
		// text 是平台错误的首行，用来识别 HTTP 5xx，不回传响应正文。
		text := err.Error()
		if strings.Contains(text, "HTTP 5") {
			return errors.Join(itemapp.ErrExternalStockPlatform, errors.New("平台返回 5xx"))
		}
		return errors.Join(itemapp.ErrExternalStockPlatform, errors.New("平台动作失败"))
	}
}

// 确保发布端口实现外部库存平台能力。
var _ itemapp.ExternalStockPlatform = (*ItemPublishPort)(nil)
