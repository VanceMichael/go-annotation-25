package convert

import (
	"errors"
	"math"
	"testing"
	"time"

	"wasteoil/internal/model"
)

// batch 构造一个质量平衡闭合的批次。
// 投料 20000kg，产出 21000L × 0.881kg/L = 18501kg，甘油 1200kg，损耗 299kg。
func batch() model.Batch {
	return model.Batch{
		ID:                   "B-001",
		PickupIDs:            []string{"P-001", "P-002"},
		FeedMassKG:           20000,
		ProductVolumeL:       21000,
		ProductDensityKGPerL: 0.881,
		GlycerolMassKG:       1200,
		LossMassKG:           299,
		ConvertedAt:          time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC),
	}
}

// TestProductMassUsesDensity 断言产出质量由体积与密度换算得到。
func TestProductMassUsesDensity(t *testing.T) {
	cases := []struct {
		volumeL, density, want float64
	}{
		{1000, 0.881, 881},
		{21000, 0.881, 18501},
		{500, 0.9, 450},
		{0, 0.881, 0},
	}
	for _, tc := range cases {
		got := ProductMassKG(tc.volumeL, tc.density)
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("ProductMassKG(%.1fL, %.3fkg/L) = %.4f, 期望 %.4f",
				tc.volumeL, tc.density, got, tc.want)
		}
	}
}

// TestYieldUsesDensity 断言得率按质量口径计算，落在行业正常区间。
func TestYieldUsesDensity(t *testing.T) {
	y, err := Compute(batch())
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	if math.Abs(y.ProductMassKG-18501) > 0.01 {
		t.Fatalf("产出质量 = %.4f kg, 期望 18501（21000L × 0.881kg/L）", y.ProductMassKG)
	}
	// 得率 = 18501 / 20000 = 0.92505
	if math.Abs(y.Ratio-0.9251) > 0.001 {
		t.Fatalf("得率 = %.4f, 期望约 0.9251（按质量口径）", y.Ratio)
	}
	if !Plausible(y) {
		t.Fatalf("得率 %.4f 落在行业正常区间之外", y.Ratio)
	}
}

// TestYieldRatioNotComputedFromVolume 断言得率不是用体积直接除以投料质量。
func TestYieldRatioNotComputedFromVolume(t *testing.T) {
	b := batch()
	y, err := Compute(b)
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	volumeRatio := b.ProductVolumeL / b.FeedMassKG // 1.05，量纲不一致
	if math.Abs(y.Ratio-volumeRatio) < 0.01 {
		t.Fatalf("得率 = %.4f 与体积比 %.4f 接近, 说明未做密度换算", y.Ratio, volumeRatio)
	}
}

// TestMassBalanceCloses 断言质量平衡在正常批次上闭合。
func TestMassBalanceCloses(t *testing.T) {
	y, err := Compute(batch())
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	if !y.Balanced {
		t.Fatalf("质量平衡应闭合: 投料 %.3f, 产出 %.3f, 甘油 %.3f, 损耗 %.3f, 缺口 %.3f",
			y.FeedMassKG, y.ProductMassKG, y.GlycerolMassKG, y.LossMassKG, y.BalanceGapKG)
	}
	if math.Abs(y.BalanceGapKG) > 1 {
		t.Fatalf("质量平衡缺口 = %.3f kg, 期望接近 0", y.BalanceGapKG)
	}
	if err := CheckBalance(y); err != nil {
		t.Fatalf("CheckBalance 应通过: %v", err)
	}
}

// TestMassBalanceDetectsGap 断言质量平衡缺口过大时能被识别。
func TestMassBalanceDetectsGap(t *testing.T) {
	b := batch()
	b.LossMassKG = 5000 // 人为放大损耗，制造不闭合
	y, err := Compute(b)
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	if y.Balanced {
		t.Fatalf("质量平衡应不闭合: 缺口 %.3f", y.BalanceGapKG)
	}
	if err := CheckBalance(y); !errors.Is(err, model.ErrMassBalance) {
		t.Fatalf("CheckBalance 应返回 ErrMassBalance, 实际 %v", err)
	}
}

// TestYieldAcrossDensities 断言不同密度下得率随密度同向变化。
func TestYieldAcrossDensities(t *testing.T) {
	prev := 0.0
	for _, density := range []float64{0.85, 0.87, 0.881, 0.90} {
		b := batch()
		b.ProductDensityKGPerL = density
		y, err := Compute(b)
		if err != nil {
			t.Fatalf("density=%.3f: Compute 返回错误: %v", density, err)
		}
		if y.Ratio <= prev {
			t.Fatalf("密度 %.3f 得率 %.4f 应高于更低密度的 %.4f", density, y.Ratio, prev)
		}
		prev = y.Ratio
	}
}

func TestComputeRejectsInvalidBatch(t *testing.T) {
	mutations := []func(b *model.Batch){
		func(b *model.Batch) { b.ID = "" },
		func(b *model.Batch) { b.PickupIDs = nil },
		func(b *model.Batch) { b.FeedMassKG = 0 },
		func(b *model.Batch) { b.ProductVolumeL = 0 },
		func(b *model.Batch) { b.ProductDensityKGPerL = 0 },
		func(b *model.Batch) { b.GlycerolMassKG = -1 },
		func(b *model.Batch) { b.ConvertedAt = time.Time{} },
	}
	for i, mutate := range mutations {
		b := batch()
		mutate(&b)
		if _, err := Compute(b); !errors.Is(err, model.ErrInvalidBatch) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidBatch, 实际 %v", i, err)
		}
	}
}

// TestAggregateWeightedMeanRatio 断言汇总得率按投料质量加权。
func TestAggregateWeightedMeanRatio(t *testing.T) {
	small := batch()
	small.ID = "B-002"
	small.FeedMassKG = 5000
	small.ProductVolumeL = 5250
	small.GlycerolMassKG = 300
	small.LossMassKG = 75

	y1, err := Compute(batch())
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	y2, err := Compute(small)
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}

	s := Aggregate([]Yield{y1, y2})
	if s.Batches != 2 {
		t.Fatalf("批次数 = %d, 期望 2", s.Batches)
	}
	wantFeed := 25000.0
	if math.Abs(s.FeedMassKG-wantFeed) > 0.01 {
		t.Fatalf("投料合计 = %.3f, 期望 %.3f", s.FeedMassKG, wantFeed)
	}
	wantMean := s.ProductMassKG / s.FeedMassKG
	if math.Abs(s.MeanRatio-wantMean) > 0.001 {
		t.Fatalf("加权平均得率 = %.4f, 期望 %.4f", s.MeanRatio, wantMean)
	}
	if s.Balanced != 2 {
		t.Fatalf("闭合批次数 = %d, 期望 2", s.Balanced)
	}
	if s.Implausible != 0 {
		t.Fatalf("异常得率批次数 = %d, 期望 0", s.Implausible)
	}
}

func TestAggregateEmpty(t *testing.T) {
	s := Aggregate(nil)
	if s.Batches != 0 || s.MeanRatio != 0 {
		t.Fatalf("空输入汇总 = %+v", s)
	}
}

func TestDescribe(t *testing.T) {
	y, err := Compute(batch())
	if err != nil {
		t.Fatalf("Compute 返回错误: %v", err)
	}
	if y.Describe() == "" {
		t.Fatalf("描述为空")
	}
}

func TestPlausibleBounds(t *testing.T) {
	if Plausible(Yield{Ratio: 0.85}) {
		t.Errorf("0.85 应判定为异常得率")
	}
	if !Plausible(Yield{Ratio: 0.95}) {
		t.Errorf("0.95 应判定为正常得率")
	}
	if Plausible(Yield{Ratio: 1.05}) {
		t.Errorf("1.05 应判定为异常得率")
	}
}
