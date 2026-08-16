package report

import (
	"testing"

	"wasteoil/internal/model"
	"wasteoil/internal/seed"
)

func builder(t *testing.T) *Builder {
	t.Helper()
	reg, ledger, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	return NewBuilder(reg, ledger)
}

func TestIntakeTotals(t *testing.T) {
	b := builder(t)
	rep, err := b.Intake()
	if err != nil {
		t.Fatalf("Intake 返回错误: %v", err)
	}
	if rep.Pickups != len(seed.Pickups()) {
		t.Fatalf("回收单数 = %d, 期望 %d", rep.Pickups, len(seed.Pickups()))
	}
	if rep.TotalMassKG != seed.TotalPickupMassKG() {
		t.Fatalf("质量合计 = %.3f, 期望 %.3f", rep.TotalMassKG, seed.TotalPickupMassKG())
	}
	// 仅 P-004 不合格，可转化应为 6 条。
	if rep.Convertible != 6 {
		t.Fatalf("可转化回收单数 = %d, 期望 6", rep.Convertible)
	}

	var pickupSum int
	var massSum float64
	for _, l := range rep.Collectors {
		pickupSum += l.Pickups
		massSum += l.MassKG
	}
	if pickupSum != rep.Pickups {
		t.Fatalf("回收单位回收单之和 = %d, 报表合计 = %d", pickupSum, rep.Pickups)
	}
	if diff := massSum - rep.TotalMassKG; diff > 0.01 || diff < -0.01 {
		t.Fatalf("回收单位质量之和 = %.3f, 报表合计 = %.3f", massSum, rep.TotalMassKG)
	}
}

func TestIntakeSourceAndGradeFixedOrder(t *testing.T) {
	b := builder(t)
	rep, err := b.Intake()
	if err != nil {
		t.Fatalf("Intake 返回错误: %v", err)
	}
	sources := model.AllSources()
	if len(rep.Sources) != len(sources) {
		t.Fatalf("来源行数 = %d, 期望 %d", len(rep.Sources), len(sources))
	}
	for i, s := range sources {
		if rep.Sources[i].Source != string(s) {
			t.Fatalf("第 %d 行来源 = %s, 期望 %s", i, rep.Sources[i].Source, s)
		}
	}
	grades := model.AllGrades()
	if len(rep.Grades) != len(grades) {
		t.Fatalf("等级行数 = %d, 期望 %d", len(rep.Grades), len(grades))
	}
	var graded int
	for _, g := range rep.Grades {
		graded += g.Count
	}
	if graded != len(seed.Assays()) {
		t.Fatalf("等级计数之和 = %d, 期望 %d", graded, len(seed.Assays()))
	}
}

func TestIntakeCollectorFlags(t *testing.T) {
	b := builder(t)
	rep, err := b.Intake()
	if err != nil {
		t.Fatalf("Intake 返回错误: %v", err)
	}
	byID := map[string]CollectorLine{}
	for _, l := range rep.Collectors {
		byID[l.CollectorID] = l
	}
	// C-004 资质停用，没有回收单。
	if got := byID["C-004"]; got.Active || got.Pickups != 0 {
		t.Fatalf("C-004 = %+v, 期望停用且无回收单", got)
	}
	// C-002 的 P-004 不合格。
	if got := byID["C-002"]; got.Rejected != 1 {
		t.Fatalf("C-002 不合格数 = %d, 期望 1", got.Rejected)
	}
}

// TestConversionRatiosPlausible 断言全部批次得率落在行业正常区间且质量平衡闭合。
func TestConversionRatiosPlausible(t *testing.T) {
	b := builder(t)
	rep, err := b.Conversion(seed.Batches())
	if err != nil {
		t.Fatalf("Conversion 返回错误: %v", err)
	}
	if len(rep.Batches) != len(seed.Batches()) {
		t.Fatalf("批次行数 = %d, 期望 %d", len(rep.Batches), len(seed.Batches()))
	}
	if rep.Implausible != 0 {
		t.Fatalf("异常得率批次数 = %d, 期望 0", rep.Implausible)
	}
	if rep.Unbalanced != 0 {
		t.Fatalf("质量平衡不闭合批次数 = %d, 期望 0", rep.Unbalanced)
	}
	for _, l := range rep.Batches {
		if l.Ratio < 0.90 || l.Ratio > 1.0 {
			t.Errorf("批次 %s 得率 = %.4f, 落在行业正常区间之外", l.BatchID, l.Ratio)
		}
		if l.ProductMassKG <= 0 {
			t.Errorf("批次 %s 产出质量 = %.4f, 应为正", l.BatchID, l.ProductMassKG)
		}
		// 产出质量应等于体积乘密度，不得直接使用体积数值。
		if l.ProductMassKG >= l.ProductVolumeL {
			t.Errorf("批次 %s 产出质量 %.4f 不应大于等于体积 %.4f（密度小于 1）",
				l.BatchID, l.ProductMassKG, l.ProductVolumeL)
		}
	}
	if rep.Summary.MeanRatio < 0.90 || rep.Summary.MeanRatio > 1.0 {
		t.Fatalf("加权平均得率 = %.4f, 期望落在 0.90-1.0", rep.Summary.MeanRatio)
	}
}

func TestConversionSummaryTotals(t *testing.T) {
	b := builder(t)
	rep, err := b.Conversion(seed.Batches())
	if err != nil {
		t.Fatalf("Conversion 返回错误: %v", err)
	}
	var feed, product float64
	for _, l := range rep.Batches {
		feed += l.FeedMassKG
		product += l.ProductMassKG
	}
	if diff := feed - rep.Summary.FeedMassKG; diff > 0.01 || diff < -0.01 {
		t.Fatalf("投料之和 = %.3f, 汇总 = %.3f", feed, rep.Summary.FeedMassKG)
	}
	if diff := product - rep.Summary.ProductMassKG; diff > 0.01 || diff < -0.01 {
		t.Fatalf("产出之和 = %.3f, 汇总 = %.3f", product, rep.Summary.ProductMassKG)
	}
}

func TestConversionRejectsInvalidBatch(t *testing.T) {
	b := builder(t)
	bad := seed.Batches()
	bad[0].FeedMassKG = 0
	if _, err := b.Conversion(bad); err == nil {
		t.Fatalf("非法批次应返回错误")
	}
}

func TestQuotaReport(t *testing.T) {
	b := builder(t)
	rep := b.Quota()
	if rep.Snapshot.Year != seed.Year {
		t.Fatalf("年度 = %d, 期望 %d", rep.Snapshot.Year, seed.Year)
	}
	if rep.Snapshot.CapacityKG != seed.QuotaCapacityKG {
		t.Fatalf("额度 = %.1f, 期望 %.1f", rep.Snapshot.CapacityKG, seed.QuotaCapacityKG)
	}
	if rep.UsedRatio != 0 {
		t.Fatalf("初始使用率 = %.4f, 期望 0", rep.UsedRatio)
	}
	if rep.Snapshot.Oversold {
		t.Fatalf("初始不应超卖")
	}
}
