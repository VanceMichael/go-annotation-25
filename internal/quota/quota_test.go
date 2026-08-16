package quota

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"wasteoil/internal/model"
)

// TestReserveSequential 断言顺序预留与释放正确记账。
func TestReserveSequential(t *testing.T) {
	l, err := New(2026, 1000)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if err := l.Reserve("S-001", 400); err != nil {
		t.Fatalf("Reserve 失败: %v", err)
	}
	if err := l.Reserve("S-002", 600); err != nil {
		t.Fatalf("Reserve 失败: %v", err)
	}
	if got := l.RemainingKG(); got != 0 {
		t.Fatalf("剩余额度 = %.3f, 期望 0", got)
	}
	if err := l.Reserve("S-003", 1); !errors.Is(err, model.ErrQuotaInsufficient) {
		t.Fatalf("额度用尽应返回 ErrQuotaInsufficient, 实际 %v", err)
	}
	if err := l.Release("S-001"); err != nil {
		t.Fatalf("Release 失败: %v", err)
	}
	if got := l.RemainingKG(); got != 400 {
		t.Fatalf("释放后剩余额度 = %.3f, 期望 400", got)
	}
}

// TestReserveNeverOversells 断言并发预留不会超卖：成功次数与已预留量都不越界。
func TestReserveNeverOversells(t *testing.T) {
	const capacityKG = 1000.0
	const perReserve = 10.0
	const workers = 64
	const perWorker = 5
	// 总请求量 64*5*10 = 3200kg，远超 1000kg 额度。

	l, err := New(2026, capacityKG)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	var granted int64
	var mu sync.Mutex

	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("S-%02d-%02d", w, i)
				if rerr := l.Reserve(id, perReserve); rerr == nil {
					mu.Lock()
					granted++
					mu.Unlock()
				}
			}
		}(w)
	}
	close(start)
	wg.Wait()

	snap := l.Snapshot()
	maxGrants := int64(capacityKG / perReserve)

	if snap.Oversold {
		t.Fatalf("发生超卖: 额度 %.1fkg, 已预留 %.1fkg, 剩余 %.1fkg",
			snap.CapacityKG, snap.ReservedKG, snap.RemainingKG)
	}
	if snap.ReservedKG > capacityKG {
		t.Fatalf("已预留 %.3fkg 超过额度 %.3fkg", snap.ReservedKG, capacityKG)
	}
	if snap.RemainingKG < 0 {
		t.Fatalf("剩余额度为负: %.3fkg", snap.RemainingKG)
	}
	if granted > maxGrants {
		t.Fatalf("成功预留 %d 次, 上限应为 %d 次", granted, maxGrants)
	}
	if int64(snap.Grants) != granted {
		t.Fatalf("台账成功计数 %d 与实际成功次数 %d 不一致", snap.Grants, granted)
	}
}

// TestReserveConcurrentExactFit 断言并发请求刚好等于额度时全部成功且剩余为 0。
func TestReserveConcurrentExactFit(t *testing.T) {
	const workers = 50
	const perReserve = 20.0
	const capacityKG = workers * perReserve // 1000kg

	l, err := New(2026, capacityKG)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			_ = l.Reserve(fmt.Sprintf("S-%03d", w), perReserve)
		}(w)
	}
	close(start)
	wg.Wait()

	snap := l.Snapshot()
	if snap.Oversold {
		t.Fatalf("发生超卖: 已预留 %.3f, 额度 %.3f", snap.ReservedKG, snap.CapacityKG)
	}
	if snap.ReservedKG != capacityKG {
		t.Fatalf("已预留 = %.3f, 期望 %.3f（请求量刚好等于额度应全部成功）",
			snap.ReservedKG, capacityKG)
	}
	if snap.Grants != workers {
		t.Fatalf("成功次数 = %d, 期望 %d", snap.Grants, workers)
	}
	if snap.Rejects != 0 {
		t.Fatalf("拒绝次数 = %d, 期望 0", snap.Rejects)
	}
}

// TestReserveConcurrentHalfCapacity 断言额度只够一半请求时恰好一半成功。
func TestReserveConcurrentHalfCapacity(t *testing.T) {
	const workers = 80
	const perReserve = 25.0
	const capacityKG = 1000.0 // 只够 40 次

	l, err := New(2026, capacityKG)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			_ = l.Reserve(fmt.Sprintf("S-%03d", w), perReserve)
		}(w)
	}
	close(start)
	wg.Wait()

	snap := l.Snapshot()
	if snap.Oversold {
		t.Fatalf("发生超卖: 已预留 %.3f, 额度 %.3f, 剩余 %.3f",
			snap.ReservedKG, snap.CapacityKG, snap.RemainingKG)
	}
	if snap.Grants != 40 {
		t.Fatalf("成功次数 = %d, 期望 40（1000kg / 25kg）", snap.Grants)
	}
	if snap.Rejects != workers-40 {
		t.Fatalf("拒绝次数 = %d, 期望 %d", snap.Rejects, workers-40)
	}
	if snap.ReservedKG != capacityKG {
		t.Fatalf("已预留 = %.3f, 期望恰好用满 %.3f", snap.ReservedKG, capacityKG)
	}
}

func TestNewRejectsBadCapacity(t *testing.T) {
	for _, c := range []float64{0, -1} {
		if _, err := New(2026, c); err == nil {
			t.Errorf("额度 %.1f 应返回错误", c)
		}
	}
}

func TestReserveValidation(t *testing.T) {
	l, err := New(2026, 1000)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if err := l.Reserve("", 10); err == nil {
		t.Errorf("空编号应返回错误")
	}
	if err := l.Reserve("S-001", 0); err == nil {
		t.Errorf("预留量为 0 应返回错误")
	}
	if err := l.Release("NOPE"); err == nil {
		t.Errorf("释放未预留的货物应返回错误")
	}
}

func TestHeldAndShipments(t *testing.T) {
	l, err := New(2026, 1000)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if err := l.Reserve("S-002", 100); err != nil {
		t.Fatalf("Reserve 失败: %v", err)
	}
	if err := l.Reserve("S-001", 200); err != nil {
		t.Fatalf("Reserve 失败: %v", err)
	}
	if err := l.Reserve("S-001", 50); err != nil {
		t.Fatalf("Reserve 失败: %v", err)
	}
	if got := l.Held("S-001"); got != 250 {
		t.Fatalf("S-001 已预留 = %.3f, 期望 250", got)
	}
	got := l.Shipments()
	if len(got) != 2 || got[0] != "S-001" || got[1] != "S-002" {
		t.Fatalf("货物列表 = %v", got)
	}
	if l.Year() != 2026 || l.CapacityKG() != 1000 {
		t.Fatalf("年度或额度异常: %d, %.1f", l.Year(), l.CapacityKG())
	}
	if l.ReservedKG() != 350 {
		t.Fatalf("已预留 = %.3f, 期望 350", l.ReservedKG())
	}
}
