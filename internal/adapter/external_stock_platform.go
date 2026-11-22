package adapter

import (
	"context"
	"errors"
	"strconv"
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

// Unpublish 下架原商品。已经不在售的平台回执视为成功，不另建商品。
func (p *ItemPublishPort) Unpublish(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.ExternalStockApplyResult, error) {
	// outcome、err 是平台下架结果。
	outcome, err := p.DownShelf(externalStockContext(ctx), userID, cookieID, itemID)
	if err == nil {
		return itemapp.ExternalStockApplyResult{Diagnostic: redactDownShelfMessage(outcome.Message), ListingState: itemapp.ListingOffSale}, nil
	}
	if downShelfAlreadyOff(err) {
		return itemapp.ExternalStockApplyResult{Diagnostic: "下架接口表示商品已不在售", ListingState: itemapp.ListingOffSale, Noop: true}, nil
	}
	return itemapp.ExternalStockApplyResult{}, mapExternalStockPlatformError(err)
}

// Relist 重新上架同一个原商品。itemStatus 读数不会跳过编辑。sync 为假时不改库存；平台库存为 0 时因此不提交。
func (p *ItemPublishPort) Relist(ctx context.Context, userID int64, cookieID, itemID string, quantity int, sync bool) (itemapp.ExternalStockApplyResult, error) {
	// applied 由平台调用闭包写入。
	var applied itemapp.ExternalStockApplyResult
	// persistErr、callErr 分别是会话写回错误和平台提交错误。
	persistErr, callErr := p.runItemEditCall(externalStockContext(ctx), userID, cookieID, 90*time.Second, itemapp.ErrExternalStockSession, func(callCtx context.Context, client mtop.Client, cookies string) (string, error) {
		// ensurer、ok 是只改在售状态的幂等上架能力。
		ensurer, ok := client.(mtop.ItemEnsureOnSaleClient)
		if !ok {
			return "", itemapp.ErrExternalStockEditUnsupported
		}
		// result、err 是原商品上架结果。
		result, err := ensurer.EnsureItemOnSale(callCtx, cookies, itemID, mtop.EnsureItemOnSaleRequest{SyncQuantity: sync, Quantity: quantity})
		if result != nil {
			applied = externalStockApplyFromRelist(result)
		}
		if err != nil {
			return "", err
		}
		if result == nil || !result.Submitted {
			return "", errors.New("闲鱼商品重新上架未返回已接受的编辑回执")
		}
		return result.UpdatedCookies, nil
	}, mapExternalStockPlatformError)
	if callErr != nil {
		applied.Outcome = "refused"
		if applied.ListingState == "" {
			applied.ListingState = "unverified"
		}
		return applied, callErr
	}
	if persistErr != nil && p.logger != nil {
		p.logger.WarnContext(ctx, "外部库存同步后保存账号会话失败", "account", cookieID, "error", persistErr)
	}
	return applied, nil
}

// externalStockApplyFromRelist 把上架回执收成不含凭证的诊断。itemStatus 读数只原样记录。
func externalStockApplyFromRelist(result *mtop.ItemRelistResult) itemapp.ExternalStockApplyResult {
	// raw 是原始 itemStatus；空字符串显示为“空”。
	raw := strings.TrimSpace(result.RawStatus)
	if raw == "" {
		raw = "空"
	}
	// ret 是脱敏后的平台回执。
	ret := sanitizePlatformText(result.PlatformRet)
	// common 是读数诊断，不能把 itemStatus 或库存 0 写成已经在售。
	common := itemapp.ExternalStockApplyResult{ListingState: "unverified", ReadItemStatus: raw, ReadQuantity: result.ReadQuantity, StockAdjusted: result.StockAdjusted, PlatformRet: ret}
	if result.Submitted {
		// note 标明平台接受了编辑，并写明库存有没有被改。
		note := "platform_outcome=submitted；read_item_status=" + raw + "；read_quantity=" + strconv.Itoa(result.ReadQuantity) + "；该读数不能证明商品当前在售；平台已接受 edit，itemStatus 写成 0"
		if result.StockAdjusted {
			note += "；stock_adjusted=true；written_quantity=" + strconv.Itoa(result.WrittenQuantity) + "；这是已开启同步平台库存后的明确改库存"
		} else {
			note += "；stock_adjusted=false；价格、库存和描述沿用读取值"
		}
		note += "；platform_ret=" + ret
		common.Diagnostic = note
		common.Outcome = "submitted"
		return common
	}
	if result.ReadQuantity <= 0 {
		common.Diagnostic = "platform_outcome=refused；read_item_status=" + raw + "；read_quantity=0；stock_adjusted=false；未提交编辑；网页编辑不接受数量小于 1 的上架，未开启同步平台库存时不会把库存改成 1 或外部可用数"
		common.Outcome = "refused"
		common.PlatformRet = "QUANTITY_ZERO_NOT_ADJUSTED"
		return common
	}
	common.Diagnostic = "platform_outcome=refused；read_item_status=" + raw + "；read_quantity=" + strconv.Itoa(result.ReadQuantity) + "；未获得平台接受的编辑回执"
	common.Outcome = "refused"
	return common
}

// sanitizePlatformText 保留平台回执里的错误码和短说明，去掉凭证和过长正文。
func sanitizePlatformText(message string) string {
	// text 是回执的第一行。
	text := strings.TrimSpace(strings.Split(message, "\n")[0])
	// lower 用来识别凭证字样。
	lower := strings.ToLower(text)
	if text == "" || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "bearer") {
		if strings.Contains(strings.ToUpper(message), "PC_NOT_SUPPORT_PUBLISH_OR_EDIT") {
			return "FAIL_BIZ_PC_NOT_SUPPORT_PUBLISH_OR_EDIT"
		}
		return ""
	}
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}

// redactDownShelfMessage 保留下架回执的短文本。疑似凭证或过长文本改成固定说明。
func redactDownShelfMessage(message string) string {
	// text 是去掉首尾空白后的平台回执。
	text := strings.TrimSpace(message)
	// lower 用来识别凭证字样，不回传原文中的秘密。
	lower := strings.ToLower(text)
	if text == "" || len(text) > 80 || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "bearer") {
		return "downshelf 已确认"
	}
	return "downshelf " + text
}

// downShelfAlreadyOff 判断下架失败是否表示商品已经不在售。其他失败仍要退避。
func downShelfAlreadyOff(err error) bool {
	if err == nil {
		return false
	}
	// text 是平台错误的首行。
	text := err.Error()
	// marker 是明确表示已经下架的短码或中文。
	for _, marker := range []string{"已下架", "已经下架", "不在架", "ALREADY_DOWN", "ITEM_NOT_ONLINE", "NOT_ON_SALE"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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

// listingFromDetail 把编辑详情的 itemStatus 映射为在售、下架或未知，并保留原始状态码供诊断。
func listingFromDetail(detail *mtop.ItemEditDetail) itemapp.ListingSnapshot {
	// raw 是平台原样返回的 itemStatus，不含 Cookie 或签名。
	raw := strings.TrimSpace(detail.Status)
	// state、known 是该状态码能否对应到在售或下架。
	state, known := itemapp.ClassifyEditDetailStatus(raw)
	if !known {
		state = itemapp.ListingUnknown
	}
	return itemapp.ListingSnapshot{State: state, Quantity: detail.Quantity, QuantityKnown: true, MultiSpec: detail.MultiSpec, RawStatus: raw, Source: "editDetail"}
}

// mapExternalStockPlatformError 把平台错误收成外部库存可退避的哨兵错误。
func mapExternalStockPlatformError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, itemapp.ErrExternalStockEditUnsupported), errors.Is(err, itemapp.ErrExternalStockRisk), errors.Is(err, itemapp.ErrExternalStockSession), errors.Is(err, itemapp.ErrExternalStockPlatform), errors.Is(err, itemapp.ErrExternalStockMultiSpec), errors.Is(err, itemapp.ErrExternalStockUnknownState):
		return err
	case errors.Is(err, mtop.ErrItemRelistZeroQuantity):
		return errors.Join(itemapp.ErrExternalStockZeroQuantity, err)
	case errors.Is(err, mtop.ErrItemEditUnsupported), errors.Is(err, itemapp.ErrStockEditUnsupported), errors.Is(err, itemapp.ErrRelistEditUnsupported):
		return errors.Join(itemapp.ErrExternalStockEditUnsupported, errors.New("FAIL_BIZ_PC_NOT_SUPPORT_PUBLISH_OR_EDIT"))
	case errors.Is(err, mtop.ErrItemMultiSpecExternalQuantity):
		return itemapp.ErrExternalStockMultiSpec
	case errors.Is(err, itemapp.ErrStockRiskVerification), errors.Is(err, itemapp.ErrRelistRiskVerification), mtop.IsRiskVerificationErr(err):
		return itemapp.ErrExternalStockRisk
	case errors.Is(err, itemapp.ErrStockSessionExpired), errors.Is(err, itemapp.ErrRelistSessionExpired), errors.Is(err, itemapp.ErrDownShelfCredentialChanged), mtop.IsSessionExpiredErr(err):
		return itemapp.ErrExternalStockSession
	default:
		// text 是平台错误的脱敏首行，保留拒绝码。
		text := sanitizePlatformText(err.Error())
		if strings.Contains(err.Error(), "HTTP 5") {
			return errors.Join(itemapp.ErrExternalStockPlatform, errors.New("平台返回 5xx"))
		}
		if text == "" {
			text = "平台动作失败"
		}
		return errors.Join(itemapp.ErrExternalStockPlatform, errors.New(text))
	}
}

// 确保发布端口实现外部库存平台能力。
var _ itemapp.ExternalStockPlatform = (*ItemPublishPort)(nil)
