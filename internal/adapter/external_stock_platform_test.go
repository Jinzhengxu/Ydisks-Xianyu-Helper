package adapter

import (
	"errors"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// TestListingFromDetailRecognizesLiveOnSaleStatus 验证真机编辑详情用 itemStatus=1 表示在售。
func TestListingFromDetailRecognizesLiveOnSaleStatus(t *testing.T) {
	// onSale 是数量为 1 的在售编辑详情。
	onSale := listingFromDetail(&mtop.ItemEditDetail{Status: "1", Quantity: 1})
	if onSale.State != itemapp.ListingUnknown || onSale.RawStatus != "1" || onSale.Source != "editDetail" {
		t.Fatalf("itemStatus=1 不能映射为在售: %+v", onSale)
	}
	// offSale 是下架编辑详情。
	offSale := listingFromDetail(&mtop.ItemEditDetail{Status: "-2"})
	if offSale.State != itemapp.ListingOffSale {
		t.Fatalf("下架详情映射错误: %+v", offSale)
	}
}

// TestLocalSyncedItemStatusReadsOnlyStatusCode 验证只取出同步详情里的状态码。
func TestLocalSyncedItemStatusReadsOnlyStatusCode(t *testing.T) {
	// status、present、err 是解析结果。
	status, present, err := localSyncedItemStatus(`{"title":"游泳票","item_status":0,"pic_info":{"picUrl":"https://cdn/a.jpg"}}`)
	if err != nil || !present || status != 0 {
		t.Fatalf("status=%d present=%v err=%v", status, present, err)
	}
	if _, present, err = localSyncedItemStatus(`{"title":"没有状态"}`); err != nil || present {
		t.Fatal("缺少状态字段时应视为不存在")
	}
}

// TestExternalStockRelistDiagnosticKeepsStatusCode 验证上架回执只保留状态码，已在售时标记为空操作。
func TestExternalStockRelistDiagnosticKeepsStatusCode(t *testing.T) {
	// applied 是真机在售码的回执。
	applied := externalStockApplyFromRelist(&mtop.ItemRelistResult{RawStatus: "1", Submitted: true, PlatformRet: "SUCCESS::调用成功"})
	if applied.Noop || applied.Outcome != "submitted" || applied.ReadItemStatus != "1" || applied.ListingState != "unverified" || strings.Contains(applied.Diagnostic, "已在售，未提交编辑") || !strings.Contains(applied.Diagnostic, "platform_outcome=submitted") {
		t.Fatalf("itemStatus=1 不能当成未提交的成功: %+v", applied)
	}
	// unknown 是未识别状态码仍要上架时的回执。
	unknown := externalStockApplyFromRelist(&mtop.ItemRelistResult{RawStatus: "9"})
	if unknown.Noop || unknown.Outcome != "refused" || unknown.ListingState != "unverified" || strings.Contains(unknown.Diagnostic, "已在售") {
		t.Fatalf("未提交不能报成功: %+v", unknown)
	}
}

// TestDownShelfAlreadyOffIsIdempotent 验证只有明确的已下架回执算成功，其他失败仍要退避。
func TestDownShelfAlreadyOffIsIdempotent(t *testing.T) {
	if !downShelfAlreadyOff(errors.New("FAIL_BIZ_ITEM_NOT_ONLINE::商品已下架")) {
		t.Fatal("已下架应视为成功")
	}
	if downShelfAlreadyOff(errors.New("RGV587_ERROR::请进行安全验证")) || redactDownShelfMessage("cookie=secret") != "downshelf 已确认" {
		t.Fatal("风控和凭证文本不能当成已下架")
	}
}
