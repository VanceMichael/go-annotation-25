// Package convert 实现废弃油脂到生物柴油的转化计量。
//
// 计量口径统一为质量（千克）。产出生物柴油以体积（升）计量，
// 转化批次同时记录产出密度（千克每升）。
//
//	得率 = 产出质量 / 投料质量
//	质量平衡: 投料质量 = 产出质量 + 副产甘油质量 + 工艺损耗质量
//
// 行业正常得率区间为 0.94 至 0.99，落在区间之外的批次标记为不合常理。
package convert

import (
	"fmt"
	"math"

	"wasteoil/internal/model"
)

// 质量平衡允许的相对偏差。
const BalanceTolerance = 0.005

// Yield 是一次转化的计量结果。
type Yield struct {
	BatchID string `json:"batch_id"`
	// FeedMassKG 是投料质量。
	FeedMassKG float64 `json:"feed_mass_kg"`
	// ProductVolumeL 是产出体积。
	ProductVolumeL float64 `json:"product_volume_l"`
	// ProductMassKG 是产出质量，由体积与密度换算得到。
	ProductMassKG float64 `json:"product_mass_kg"`
	// GlycerolMassKG 是副产甘油质量。
	GlycerolMassKG float64 `json:"glycerol_mass_kg"`
	// LossMassKG 是工艺损耗质量。
	LossMassKG float64 `json:"loss_mass_kg"`
	// Ratio 是质量得率。
	Ratio float64 `json:"ratio"`
	// BalanceGapKG 是质量平衡缺口，理想为 0。
	BalanceGapKG float64 `json:"balance_gap_kg"`
	// Balanced 报告质量平衡是否闭合。
	Balanced bool `json:"balanced"`
}

// ProductMassKG 依据体积与密度换算产出质量。
func ProductMassKG(volumeL, densityKGPerL float64) float64 {
	return round4(volumeL * densityKGPerL)
}

// Compute 计算一个转化批次的计量结果。
func Compute(b model.Batch) (Yield, error) {
	if err := b.Validate(); err != nil {
		return Yield{}, err
	}
	productMass := ProductMassKG(b.ProductVolumeL, b.ProductDensityKGPerL)
	y := Yield{
		BatchID:        b.ID,
		FeedMassKG:     round4(b.FeedMassKG),
		ProductVolumeL: round4(b.ProductVolumeL),
		ProductMassKG:  productMass,
		GlycerolMassKG: round4(b.GlycerolMassKG),
		LossMassKG:     round4(b.LossMassKG),
	}
	y.Ratio = round4(productMass / b.FeedMassKG)
	y.BalanceGapKG = round4(b.FeedMassKG - (productMass + b.GlycerolMassKG + b.LossMassKG))
	y.Balanced = math.Abs(y.BalanceGapKG) <= b.FeedMassKG*BalanceTolerance
	return y, nil
}

// CheckBalance 校验质量平衡是否闭合。
func CheckBalance(y Yield) error {
	if !y.Balanced {
		return fmt.Errorf("%w: 批次 %s 投料 %.3fkg, 产出 %.3fkg + 甘油 %.3fkg + 损耗 %.3fkg, 缺口 %.3fkg",
			model.ErrMassBalance, y.BatchID, y.FeedMassKG, y.ProductMassKG,
			y.GlycerolMassKG, y.LossMassKG, y.BalanceGapKG)
	}
	return nil
}

// Plausible 报告得率是否落在行业正常区间内。
func Plausible(y Yield) bool {
	return y.Ratio >= 0.90 && y.Ratio <= 1.0
}

// Summary 汇总多个批次的计量结果。
type Summary struct {
	Batches        int     `json:"batches"`
	FeedMassKG     float64 `json:"feed_mass_kg"`
	ProductMassKG  float64 `json:"product_mass_kg"`
	GlycerolMassKG float64 `json:"glycerol_mass_kg"`
	LossMassKG     float64 `json:"loss_mass_kg"`
	// MeanRatio 是加权平均得率（按投料质量加权）。
	MeanRatio   float64 `json:"mean_ratio"`
	Balanced    int     `json:"balanced"`
	Implausible int     `json:"implausible"`
}

// Aggregate 汇总多个批次的计量结果。
func Aggregate(items []Yield) Summary {
	s := Summary{Batches: len(items)}
	if len(items) == 0 {
		return s
	}
	for _, y := range items {
		s.FeedMassKG += y.FeedMassKG
		s.ProductMassKG += y.ProductMassKG
		s.GlycerolMassKG += y.GlycerolMassKG
		s.LossMassKG += y.LossMassKG
		if y.Balanced {
			s.Balanced++
		}
		if !Plausible(y) {
			s.Implausible++
		}
	}
	s.FeedMassKG = round4(s.FeedMassKG)
	s.ProductMassKG = round4(s.ProductMassKG)
	s.GlycerolMassKG = round4(s.GlycerolMassKG)
	s.LossMassKG = round4(s.LossMassKG)
	if s.FeedMassKG > 0 {
		s.MeanRatio = round4(s.ProductMassKG / s.FeedMassKG)
	}
	return s
}

// Describe 返回计量结果的可读描述。
func (y Yield) Describe() string {
	status := "闭合"
	if !y.Balanced {
		status = "不闭合"
	}
	return fmt.Sprintf("批次 %s: 投料 %.3fkg, 产出 %.3fL(%.3fkg), 得率 %.4f, 质量平衡%s(缺口 %.3fkg)",
		y.BatchID, y.FeedMassKG, y.ProductVolumeL, y.ProductMassKG, y.Ratio, status, y.BalanceGapKG)
}

func round4(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*10000) / 10000
}
