package items

import (
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
	if state != ListingUnknown || known {
		t.Fatalf("编辑详情 1 不能当成在售: %s %v", state, known)
	}
	state, known = ClassifyEditDetailStatus("0")
	if state != ListingOnSale || !known {
		t.Fatalf("编辑详情 0 仍是提交用的在售值: %s %v", state, known)
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

// TestDecideExternalStockNeverRelistsWhenEmpty 验证库存为 0 时只下架，有库存时只重新上架，且不看在售状态。
func TestDecideExternalStockNeverRelistsWhenEmpty(t *testing.T) {
	// unpublish 是库存耗尽时的决策，与当前是否在售无关。
	unpublish := DecideExternalStock(0, false)
	if unpublish.Action != ExternalStockActionUnpublish || unpublish.BackoffClass != "" {
		t.Fatalf("应下架且不因状态未知退避: %+v", unpublish)
	}
	// relist 是有库存时的决策，即使调用方还不知道在售状态。
	relist := DecideExternalStock(1, false)
	if relist.Action != ExternalStockActionRelist || relist.BackoffClass != "" {
		t.Fatalf("应重新上架: %+v", relist)
	}
	// dry 是演练标记，动作仍是重新上架。
	dry := DecideExternalStock(4, true)
	if !dry.DryRun || dry.Action != ExternalStockActionRelist {
		t.Fatalf("演练应保留上架动作: %+v", dry)
	}
	// invalid 是负数读数，不能当成下架或上架。
	invalid := DecideExternalStock(-1, false)
	if invalid.Action != ExternalStockActionStop {
		t.Fatalf("无效读数应停止: %+v", invalid)
	}
}

// TestSameDesiredStockAppliedSkipsOnlySameDay 验证相同动作和可用数只在同一配置日跳过。
func TestSameDesiredStockAppliedSkipsOnlySameDay(t *testing.T) {
	// location 是北京时间。
	location, err := time.LoadLocation(ExternalStockDefaultTimezone)
	if err != nil {
		t.Fatal(err)
	}
	// now 是当天中午。
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, location)
	// available 是上次成功时的可用库存。
	available := 1
	if !SameDesiredStockApplied(ExternalStockActionRelist, 1, ExternalStockActionRelist, &available, now.Add(-time.Hour), now, ExternalStockDefaultTimezone) {
		t.Fatal("同一天相同库存应视为已处理")
	}
	if SameDesiredStockApplied(ExternalStockActionRelist, 0, ExternalStockActionRelist, &available, now.Add(-time.Hour), now, ExternalStockDefaultTimezone) {
		t.Fatal("可用数变化后应重新执行")
	}
	if SameDesiredStockApplied(ExternalStockActionUnpublish, 1, ExternalStockActionRelist, &available, now.Add(-time.Hour), now, ExternalStockDefaultTimezone) {
		t.Fatal("动作变化后应重新执行")
	}
	if SameDesiredStockApplied(ExternalStockActionRelist, 1, ExternalStockActionRelist, &available, now.Add(-24*time.Hour), now, ExternalStockDefaultTimezone) {
		t.Fatal("跨日应重新执行")
	}
	if !ExternalStockCronHit(ExternalStockDefaultCron, ExternalStockDefaultTimezone, time.Date(2026, 11, 22, 0, 1, 0, 0, location)) {
		t.Fatal("00:01 应算 cron 命中")
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
