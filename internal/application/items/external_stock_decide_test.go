package items

import (
	"strings"
	"testing"
	"time"
)

// TestParseAvailableCount 验证顶层和 data 下的整数，以及拒绝小数、负数和缺失路径。
func TestParseAvailableCount(t *testing.T) {
	// cases 覆盖库存接口可能返回的 JSON 形态。
	cases := []struct {
		name    string
		body    string
		path    string
		want    int
		wantErr bool
	}{
		{name: "top", body: `{"available":1,"total":1,"allocated":0,"date":"2026-11-22"}`, path: "available", want: 1},
		{name: "nested", body: `{"data":{"available":0,"total":1}}`, path: "data.available", want: 0},
		{name: "fallback", body: `{"data":{"available":2}}`, path: "available", want: 2},
		{name: "string", body: `{"available":"3"}`, path: "available", want: 3},
		{name: "fraction", body: `{"available":1.5}`, path: "available", wantErr: true},
		{name: "negative", body: `{"available":-1}`, path: "available", wantErr: true},
		{name: "missing", body: `{"total":1}`, path: "data.stock", wantErr: true},
		{name: "not-json", body: `not-json`, path: "available", wantErr: true},
	}
	// tc 是一种响应形态。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got、err 是解析出的可用库存和失败原因。
			got, err := ParseAvailableCount([]byte(tc.body), tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("应拒绝无效库存")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got=%d err=%v", got, err)
			}
		})
	}
}

// TestClassifyListingStatus 验证真机编辑详情的在售状态码，以及本地同步只把 0 当成在售。
func TestClassifyListingStatus(t *testing.T) {
	// state、known 是编辑详情状态码的解释。
	state, known := ClassifyEditDetailStatus("1")
	if state != ListingOnSale || !known {
		t.Fatalf("编辑详情 1 应是在售: %s %v", state, known)
	}
	state, known = ClassifyEditDetailStatus("0")
	if state != ListingOnSale || !known {
		t.Fatalf("编辑详情 0 应是在售: %s %v", state, known)
	}
	state, known = ClassifyEditDetailStatus("-2")
	if state != ListingOffSale || !known {
		t.Fatalf("编辑详情 -2 应是下架: %s %v", state, known)
	}
	state, known = ClassifyEditDetailStatus("9")
	if state != ListingUnknown || known {
		t.Fatalf("未知状态码不能当成下架: %s %v", state, known)
	}
	state, known = ClassifyLocalItemStatus(0, true)
	if state != ListingOnSale || !known {
		t.Fatalf("本地 0 应是在售: %s %v", state, known)
	}
	state, known = ClassifyLocalItemStatus(1, true)
	if known {
		t.Fatalf("本地其他状态不能猜测为下架: %s", state)
	}
}

// TestDecideExternalStockNeverRelistsWhenEmpty 验证库存为 0 时只下架或保持下架，绝不上架。
func TestDecideExternalStockNeverRelistsWhenEmpty(t *testing.T) {
	// onSale 是仍在售的商品。
	onSale := ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true}
	// offSale 是已经下架的商品。
	offSale := ListingSnapshot{State: ListingOffSale, Quantity: 0, QuantityKnown: true}
	// unpublish 是库存耗尽且仍在售时的决策。
	unpublish := DecideExternalStock(0, onSale, false, true, 1)
	if unpublish.Action != ExternalStockActionUnpublish || unpublish.DryRun {
		t.Fatalf("应下架: %+v", unpublish)
	}
	// stay 是库存耗尽且已下架时的决策。
	stay := DecideExternalStock(0, offSale, false, true, 1)
	if stay.Action != ExternalStockActionNone {
		t.Fatalf("不应上架: %+v", stay)
	}
	// unknown 是状态不明时的决策，必须停止。
	unknown := DecideExternalStock(4, ListingSnapshot{State: ListingUnknown}, false, true, 1)
	if unknown.Action != ExternalStockActionStop || unknown.BackoffClass != ExternalStockBackoffAll {
		t.Fatalf("未知状态应停止: %+v", unknown)
	}
}

// TestDecideExternalStockRelistAndSync 验证有库存时重新上架、同步上限，以及演练不改变动作。
func TestDecideExternalStockRelistAndSync(t *testing.T) {
	// offSale 是待重新上架的原商品。
	offSale := ListingSnapshot{State: ListingOffSale, Quantity: 0, QuantityKnown: true}
	// relist 是不改库存数量的重新上架。
	relist := DecideExternalStock(2, offSale, false, false, 9)
	if relist.Action != ExternalStockActionRelist || relist.Sync || relist.Quantity != 0 {
		t.Fatalf("应只重新上架: %+v", relist)
	}
	// synced 是把平台库存封顶到配置上限。
	synced := DecideExternalStock(5, offSale, false, true, 2)
	if synced.Action != ExternalStockActionRelist || !synced.Sync || synced.Quantity != 2 {
		t.Fatalf("应同步上限: %+v", synced)
	}
	// dry 是同一次决策的演练标记。
	dry := DecideExternalStock(5, offSale, true, true, 2)
	if !dry.DryRun || dry.Action != ExternalStockActionRelist {
		t.Fatalf("演练应保留动作: %+v", dry)
	}
	// onSale 是已在售但库存偏大的商品。
	onSale := ListingSnapshot{State: ListingOnSale, Quantity: 4, QuantityKnown: true}
	// adjust 是只改库存的决策。
	adjust := DecideExternalStock(1, onSale, false, true, 9)
	if adjust.Action != ExternalStockActionSyncStock || adjust.Quantity != 1 {
		t.Fatalf("应只改库存: %+v", adjust)
	}
	// same 是库存已经一致时的决策。
	same := DecideExternalStock(1, ListingSnapshot{State: ListingOnSale, Quantity: 1, QuantityKnown: true}, false, true, 9)
	if same.Action != ExternalStockActionNone {
		t.Fatalf("库存一致不应改动: %+v", same)
	}
	// multi 是多规格商品不能按单一数字改库存。
	multi := DecideExternalStock(2, ListingSnapshot{State: ListingOnSale, Quantity: 2, QuantityKnown: true, MultiSpec: true}, false, true, 9)
	if multi.Action != ExternalStockActionStop || multi.BackoffClass != ExternalStockBackoffRelist || !strings.Contains(multi.Reason, "多规格") {
		t.Fatalf("多规格应停止: %+v", multi)
	}
}

// TestExternalStockSchedule 验证默认 cron、时区和周期到期。
func TestExternalStockSchedule(t *testing.T) {
	if // err 是默认 cron 不合法。
	err := ValidateExternalStockCron(ExternalStockDefaultCron); err != nil {
		t.Fatal(err)
	}
	if // err 是步长表达式被错误接受时的空错误。
	err := ValidateExternalStockCron("*/5 * * * *"); err == nil {
		t.Fatal("不应接受步长")
	}
	// location 是北京时间。
	location, err := time.LoadLocation(ExternalStockDefaultTimezone)
	if err != nil {
		t.Fatal(err)
	}
	// atReset 是默认 cron 命中的 00:01。
	atReset := time.Date(2026, 11, 22, 0, 1, 8, 0, location)
	if !CronMatches(ExternalStockDefaultCron, atReset) {
		t.Fatal("00:01 应命中默认 cron")
	}
	if CronMatches(ExternalStockDefaultCron, atReset.Add(time.Minute)) {
		t.Fatal("00:02 不应命中")
	}
	if !ExternalStockDue(ExternalStockDefaultCron, ExternalStockDefaultTimezone, ExternalStockDefaultInterval, time.Time{}, atReset) {
		t.Fatal("从未检查过应到期")
	}
	if ExternalStockDue(ExternalStockDefaultCron, ExternalStockDefaultTimezone, 0, atReset, atReset.Add(20*time.Second)) {
		t.Fatal("同一分钟不应重复到期")
	}
	if !ExternalStockDue(ExternalStockDefaultCron, ExternalStockDefaultTimezone, 30*time.Minute, atReset, atReset.Add(31*time.Minute)) {
		t.Fatal("周期到达后应到期")
	}
}

// TestMergeExternalStockHeadersKeepsSecret 验证留空保留旧请求头，并且不接受空值和传输头。
func TestMergeExternalStockHeadersKeepsSecret(t *testing.T) {
	// merged、names、err 是替换后的明文、名称顺序和校验错误。
	merged, names, err := mergeExternalStockHeaders(map[string]string{"Authorization": "Bearer old"}, []ExternalStockHeader{{Name: "authorization", Keep: true}})
	if err != nil || merged["Authorization"] != "Bearer old" || len(names) != 1 {
		t.Fatalf("merged=%v names=%v err=%v", merged, names, err)
	}
	if _, _, err = mergeExternalStockHeaders(nil, []ExternalStockHeader{{Name: "Host", Value: "evil"}}); err == nil {
		t.Fatal("应拒绝 Host")
	}
	if _, _, err = mergeExternalStockHeaders(nil, []ExternalStockHeader{{Name: "Authorization", Value: ""}}); err == nil {
		t.Fatal("应拒绝空值")
	}
}
