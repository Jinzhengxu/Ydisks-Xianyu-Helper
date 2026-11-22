package mtopsim_test

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件用真实的 mtop.ClientImpl 对接平台模拟器，验证“售出后重新上架”的协议链路与失败分类。

// TestRelistItemSingleSpecSoldOut 验证单规格售罄商品：补回库存 1、状态回到在售，固定两次请求。
func TestRelistItemSingleSpecSoldOut(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是库存 1 的单规格商品，随后被卖出。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 1)
	gateway.SellOut(itemID)

	// result、err 是重新上架结果。
	result, err := client.RelistItem(context.Background(), cookie, itemID)
	if err != nil || result.AlreadyOnSale || !result.RestoredQuantity {
		t.Fatalf("重新上架失败: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	item, _ := gateway.Item(itemID)
	if !item.OnSale || item.Quantity != 1 || item.EditCount != 1 {
		t.Fatalf("平台状态不符: %+v", item)
	}
	if gateway.CallCount(mtopsim.APIItemEditDetail) != 1 || gateway.CallCount(mtopsim.APIItemEdit) != 1 {
		t.Fatalf("应固定读一次写一次: %v", gateway.CallSequence())
	}
}

// TestRelistItemMultiSpecSoldOut 验证多规格全部售罄时每个规格行补 1，并还原 itemProperties。
func TestRelistItemMultiSpecSoldOut(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是两规格商品，随后全部售罄。
	itemID := gateway.AddItemWithSKUs(sellerID, "多规格商品", redBlueSKUs())
	gateway.SellOut(itemID)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || !result.RestoredQuantity {
		t.Fatalf("多规格重新上架失败: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	item, _ := gateway.Item(itemID)
	if !item.OnSale || item.Quantity != 2 || item.SKUs[0].Quantity != 1 || item.SKUs[1].Quantity != 1 {
		t.Fatalf("多规格平台状态不符: %+v", item)
	}
}

// TestRelistItemKeepsRemainingStock 验证下架但仍有库存的商品只改状态，不改库存。
func TestRelistItemKeepsRemainingStock(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是库存 5 的商品，被下架但库存保留。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)
	gateway.SetOnSale(itemID, false)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || result.RestoredQuantity {
		t.Fatalf("不应补库存: %+v err=%v", result, err)
	}
	// item 是平台侧当前商品状态。
	if item, _ := gateway.Item(itemID); !item.OnSale || item.Quantity != 5 {
		t.Fatalf("应保留原库存: %+v", item)
	}
}

// TestRelistItemAlreadyOnSaleSkipsEdit 验证已在售且有库存时只读取、不提交 edit（重复事件幂等）。
func TestRelistItemAlreadyOnSaleSkipsEdit(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// itemID 是在售商品。
	itemID := gateway.AddItemWithStock(sellerID, "单规格商品", "9.90", 5)

	// err 是本次调用返回的错误，用于断言错误映射或失败分类；result 是重新上架调用的返回结果。
	if result, err := client.RelistItem(context.Background(), cookie, itemID); err != nil || !result.AlreadyOnSale {
		t.Fatalf("应识别为已在售: %+v err=%v", result, err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 0 {
		t.Fatalf("已在售不应提交 edit: %v", gateway.CallSequence())
	}
}

// TestRelistItemFailures 验证不支持类目、空商品 ID、风控和编辑拒绝的分类，且失败不修改平台数据、不重试。
func TestRelistItemFailures(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// ctx 是全部请求共用的上下文。
	ctx := context.Background()
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, " "); !errors.Is(err, mtop.ErrItemStockInvalid) {
		t.Fatalf("空商品 ID 应报输入错误: %v", err)
	}
	if gateway.CallCount(mtopsim.APIItemEditDetail) != 0 {
		t.Fatal("空商品 ID 不应发请求")
	}
	// unsupported 是类目不支持网页端编辑的已售商品。
	unsupported := gateway.AddItemWithStock(sellerID, "不支持类目", "9.90", 1)
	gateway.SellOut(unsupported)
	gateway.MarkEditUnsupported(unsupported)
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, unsupported); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("不支持类目应返回哨兵错误: %v", err)
	}
	// supported 是支持编辑但 edit 阶段被平台判定不支持的商品。
	supported := gateway.AddItemWithStock(sellerID, "编辑阶段拒绝", "9.90", 1)
	gateway.SellOut(supported)
	gateway.InjectFault(mtopsim.APIItemEdit, mtopsim.Fault{Ret: []string{mtopsim.RetEditUnsupported}})
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := client.RelistItem(ctx, cookie, supported); !errors.Is(err, mtop.ErrItemEditUnsupported) {
		t.Fatalf("edit 阶段不支持应返回哨兵错误: %v", err)
	}
	// item 是平台侧当前的商品状态副本。
	if item, _ := gateway.Item(supported); item.OnSale || item.EditCount != 0 {
		t.Fatalf("失败不应修改平台数据: %+v", item)
	}
	// missing 是不存在的商品，editDetail 即失败，不应继续提交。
	if _, err := client.RelistItem(ctx, cookie, "999"); err == nil {
		t.Fatal("不存在的商品应失败")
	}
}

// TestEnsureItemOnSaleKeepsExistingListing 验证 itemStatus=1 仍要提交编辑，并且不改价格和库存。
func TestEnsureItemOnSaleKeepsExistingListing(t *testing.T) {
	// gateway、client、cookie 是独立的平台状态、真实客户端和卖家 Cookie。
	gateway, client, cookie := newSellerGateway(t)
	// misleading 是 App 可能已下架、但编辑详情仍返回 itemStatus=1 的商品。
	misleading := gateway.AddItemWithStock(sellerID, "在售虚拟商品", "9.90", 4)
	gateway.SetOnSale(misleading, false)
	gateway.SetEditStatus(misleading, "1")
	// listed、err 是必须实际提交编辑的结果。
	listed, err := client.EnsureItemOnSale(context.Background(), cookie, misleading)
	if err != nil || listed.AlreadyOnSale || !listed.Submitted || listed.RawStatus != "1" || listed.PlatformRet == "" {
		t.Fatalf("itemStatus=1 必须提交编辑: %+v err=%v", listed, err)
	}
	if gateway.CallCount(mtopsim.APIItemEdit) != 1 {
		t.Fatal("itemStatus=1 不能不提交编辑")
	}
	// kept 是平台侧商品，价格和库存必须保持原值，状态改为提交后的在售。
	kept, _ := gateway.Item(misleading)
	// submittedQuantity 是编辑载荷里的库存。
	submittedQuantity, _ := kept.EditData["quantity"].(string)
	if !kept.OnSale || kept.Quantity != 4 || kept.Price != "9.90" || kept.EditCount != 1 || kept.EditData["itemStatus"] != "0" || submittedQuantity != "4" {
		t.Fatalf("提交改动了价格或库存: %+v quantity=%s", kept, submittedQuantity)
	}
	// unsupported 是类目不支持网页编辑的商品，必须返回拒绝而不是成功。
	unsupported := gateway.AddItemWithStock(sellerID, "不支持类目", "6.60", 2)
	gateway.SetOnSale(unsupported, false)
	gateway.SetEditStatus(unsupported, "1")
	gateway.MarkEditUnsupported(unsupported)
	// refused、refuseErr 是平台拒绝结果。
	refused, refuseErr := client.EnsureItemOnSale(context.Background(), cookie, unsupported)
	if refuseErr == nil || !errors.Is(refuseErr, mtop.ErrItemEditUnsupported) || (refused != nil && refused.Submitted) {
		t.Fatalf("不支持编辑应拒绝而不是成功: %+v err=%v", refused, refuseErr)
	}
	// refusedItem 是被拒绝的商品，平台数据不能被改写。
	refusedItem, _ := gateway.Item(unsupported)
	if refusedItem.EditCount != 0 || refusedItem.OnSale {
		t.Fatalf("拒绝后不应改商品: %+v", refusedItem)
	}
	// offSale 是仍有库存但已下架的原商品。
	offSale := gateway.AddItemWithStock(sellerID, "待上架商品", "8.80", 3)
	gateway.SetOnSale(offSale, false)
	// restored、err 是把原商品改回在售的结果。
	restored, err := client.EnsureItemOnSale(context.Background(), cookie, offSale)
	if err != nil || restored.AlreadyOnSale || restored.RawStatus != "-2" {
		t.Fatalf("下架商品应重新上架: %+v err=%v", restored, err)
	}
	// after 是提交后的平台商品。
	after, _ := gateway.Item(offSale)
	// offQuantity 是编辑载荷里的库存，必须仍是原来的 3。
	offQuantity, _ := after.EditData["quantity"].(string)
	if !after.OnSale || after.Quantity != 3 || after.Price != "8.80" || after.EditCount != 1 || after.EditData["itemStatus"] != "0" || offQuantity != "3" || !restored.Submitted {
		t.Fatalf("重新上架改动了原详情: %+v quantity=%s", after, offQuantity)
	}
	// again 是读回 itemStatus=0 后的再次上架。读数仍不能跳过提交。
	again, err := client.EnsureItemOnSale(context.Background(), cookie, offSale)
	// repeated 是第二次提交后的平台商品。
	repeated, _ := gateway.Item(offSale)
	if err != nil || again.AlreadyOnSale || !again.Submitted || repeated.EditCount != 2 || repeated.Quantity != 3 {
		t.Fatalf("itemStatus=0 也不能跳过提交: %+v err=%v edits=%d", again, err, repeated.EditCount)
	}
	// unknown 是状态码无法识别但仍应尝试上架的原商品。
	unknown := gateway.AddItemWithStock(sellerID, "未知状态", "7.70", 2)
	gateway.SetOnSale(unknown, false)
	gateway.SetEditStatus(unknown, "9")
	// attempted、err 是未知状态码的上架结果。
	attempted, err := client.EnsureItemOnSale(context.Background(), cookie, unknown)
	if err != nil || attempted.AlreadyOnSale || attempted.RawStatus != "9" {
		t.Fatalf("未知状态码仍应上架: %+v err=%v", attempted, err)
	}
	// unknownItem 是未知状态码商品提交后的平台状态。
	unknownItem, _ := gateway.Item(unknown)
	if !unknownItem.OnSale || unknownItem.Quantity != 2 || unknownItem.EditCount != 1 {
		t.Fatalf("未知状态码上架结果异常: %+v", unknownItem)
	}
}
