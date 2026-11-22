package adapter

import (
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// TestListingFromDetailRecognizesLiveOnSaleStatus 验证真机编辑详情用 itemStatus=1 表示在售。
func TestListingFromDetailRecognizesLiveOnSaleStatus(t *testing.T) {
	// onSale 是数量为 1 的在售编辑详情。
	onSale := listingFromDetail(&mtop.ItemEditDetail{Status: "1", Quantity: 1})
	if onSale.State != itemapp.ListingOnSale || onSale.RawStatus != "1" || onSale.Source != "editDetail" {
		t.Fatalf("在售详情映射错误: %+v", onSale)
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
