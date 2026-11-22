package automation

import (
	"context"
	"log/slog"
	"testing"
)

// TestNotifyResultObservesPaidDelivery 验证只有付款发货成功才会通知外部库存检查。
func TestNotifyResultObservesPaidDelivery(t *testing.T) {
	// calls 记录观察者收到的商品标识。
	var calls []string
	// center 是只带观察者的自动化中心，通知器为空所以不会外发。
	center := &Center{
		notifications: deliveryNotifier{current: func() Notifier { return nil }, logger: slog.New(slog.NewTextHandler(&discardBuffer{}, nil))},
		dependencies:  centerDependencies{paidDeliveryObserver: func(accountID, itemID, _ string) { calls = append(calls, accountID+"/"+itemID) }},
		logger:        slog.New(slog.NewTextHandler(&discardBuffer{}, nil)),
	}
	center.notifyResult(context.Background(), Task{TriggerType: TriggerOrderPaid, AccountID: "acc", ItemID: "item", OrderID: "order"}, 1, "success", 1, "")
	center.notifyResult(context.Background(), Task{TriggerType: TriggerOrderPaid, AccountID: "acc", ItemID: "item"}, 2, "failed", 0, "失败")
	center.notifyResult(context.Background(), Task{TriggerType: TriggerBuyerReviewed, AccountID: "acc", ItemID: "item"}, 3, "success", 1, "")
	if len(calls) != 1 || calls[0] != "acc/item" {
		t.Fatalf("calls=%v", calls)
	}
}

// discardBuffer 丢弃日志字节，避免测试依赖真实输出。
type discardBuffer struct{}

// Write 实现 io.Writer 并忽略内容。
func (discardBuffer) Write(p []byte) (int, error) { return len(p), nil }
