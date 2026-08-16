// Package report 生成回收与出口统计报表。
package report

import (
	"fmt"
	"sort"

	"wasteoil/internal/collect"
	"wasteoil/internal/convert"
	"wasteoil/internal/model"
	"wasteoil/internal/quota"
)

// CollectorLine 是回收单位维度的报表行。
type CollectorLine struct {
	CollectorID string  `json:"collector_id"`
	Name        string  `json:"name"`
	City        string  `json:"city"`
	Active      bool    `json:"active"`
	Pickups     int     `json:"pickups"`
	MassKG      float64 `json:"mass_kg"`
	Convertible int     `json:"convertible"`
	Rejected    int     `json:"rejected"`
}

// SourceLine 是来源类型维度的报表行。
type SourceLine struct {
	Source     string  `json:"source"`
	SourceName string  `json:"source_name"`
	Pickups    int     `json:"pickups"`
	MassKG     float64 `json:"mass_kg"`
}

// GradeLine 是品质等级维度的报表行。
type GradeLine struct {
	Grade     string `json:"grade"`
	GradeName string `json:"grade_name"`
	Count     int    `json:"count"`
}

// Intake 是回收入库报表。
type Intake struct {
	Collectors  []CollectorLine `json:"collectors"`
	Sources     []SourceLine    `json:"sources"`
	Grades      []GradeLine     `json:"grades"`
	TotalMassKG float64         `json:"total_mass_kg"`
	Pickups     int             `json:"pickups"`
	Convertible int             `json:"convertible"`
}

// Builder 组装报表所需的数据源。
type Builder struct {
	registry *collect.Registry
	ledger   *quota.Ledger
}

// NewBuilder 构造报表生成器。
func NewBuilder(reg *collect.Registry, ledger *quota.Ledger) *Builder {
	return &Builder{registry: reg, ledger: ledger}
}

// Intake 生成回收入库报表。
func (b *Builder) Intake() (Intake, error) {
	out := Intake{}
	byCollector := make(map[string]*CollectorLine)
	for _, c := range b.registry.Collectors() {
		byCollector[c.ID] = &CollectorLine{
			CollectorID: c.ID, Name: c.Name, City: c.City, Active: c.Active,
		}
	}
	bySource := make(map[model.Source]*SourceLine)
	for _, s := range model.AllSources() {
		bySource[s] = &SourceLine{Source: string(s), SourceName: s.DisplayName()}
	}
	byGrade := make(map[model.Grade]*GradeLine)
	for _, g := range model.AllGrades() {
		byGrade[g] = &GradeLine{Grade: string(g), GradeName: g.DisplayName()}
	}

	for _, p := range b.registry.Pickups() {
		out.Pickups++
		out.TotalMassKG += p.MassKG

		if line, ok := byCollector[p.CollectorID]; ok {
			line.Pickups++
			line.MassKG += p.MassKG
		}
		if line, ok := bySource[p.Source]; ok {
			line.Pickups++
			line.MassKG += p.MassKG
		}

		a, aerr := b.registry.Assay(p.ID)
		if aerr != nil {
			continue
		}
		if line, ok := byGrade[a.Grade]; ok {
			line.Count++
		}
		if line, ok := byCollector[p.CollectorID]; ok {
			if a.Grade.Convertible() {
				line.Convertible++
			} else {
				line.Rejected++
			}
		}
		if a.Grade.Convertible() {
			out.Convertible++
		}
	}

	for _, c := range b.registry.Collectors() {
		out.Collectors = append(out.Collectors, *byCollector[c.ID])
	}
	for _, s := range model.AllSources() {
		out.Sources = append(out.Sources, *bySource[s])
	}
	for _, g := range model.AllGrades() {
		out.Grades = append(out.Grades, *byGrade[g])
	}
	sort.Slice(out.Collectors, func(i, j int) bool {
		return out.Collectors[i].CollectorID < out.Collectors[j].CollectorID
	})
	out.TotalMassKG = round3(out.TotalMassKG)
	return out, nil
}

// BatchLine 是批次维度的报表行。
type BatchLine struct {
	BatchID        string  `json:"batch_id"`
	FeedMassKG     float64 `json:"feed_mass_kg"`
	ProductVolumeL float64 `json:"product_volume_l"`
	ProductMassKG  float64 `json:"product_mass_kg"`
	Ratio          float64 `json:"ratio"`
	BalanceGapKG   float64 `json:"balance_gap_kg"`
	Balanced       bool    `json:"balanced"`
	Plausible      bool    `json:"plausible"`
}

// Conversion 是转化计量报表。
type Conversion struct {
	Batches []BatchLine     `json:"batches"`
	Summary convert.Summary `json:"summary"`
	// Implausible 是得率落在行业正常区间之外的批次数。
	Implausible int `json:"implausible"`
	// Unbalanced 是质量平衡不闭合的批次数。
	Unbalanced int `json:"unbalanced"`
}

// Conversion 生成转化计量报表。
func (b *Builder) Conversion(batches []model.Batch) (Conversion, error) {
	out := Conversion{}
	yields := make([]convert.Yield, 0, len(batches))
	for _, batch := range batches {
		y, err := convert.Compute(batch)
		if err != nil {
			return Conversion{}, fmt.Errorf("report: 批次 %s 计量失败: %w", batch.ID, err)
		}
		yields = append(yields, y)
		line := BatchLine{
			BatchID:        y.BatchID,
			FeedMassKG:     y.FeedMassKG,
			ProductVolumeL: y.ProductVolumeL,
			ProductMassKG:  y.ProductMassKG,
			Ratio:          y.Ratio,
			BalanceGapKG:   y.BalanceGapKG,
			Balanced:       y.Balanced,
			Plausible:      convert.Plausible(y),
		}
		if !line.Plausible {
			out.Implausible++
		}
		if !line.Balanced {
			out.Unbalanced++
		}
		out.Batches = append(out.Batches, line)
	}
	sort.Slice(out.Batches, func(i, j int) bool { return out.Batches[i].BatchID < out.Batches[j].BatchID })
	out.Summary = convert.Aggregate(yields)
	return out, nil
}

// QuotaReport 是配额使用报表。
type QuotaReport struct {
	Snapshot quota.Snapshot `json:"snapshot"`
	// UsedRatio 是额度使用率。
	UsedRatio float64  `json:"used_ratio"`
	Shipments []string `json:"shipments"`
}

// Quota 生成配额使用报表。
func (b *Builder) Quota() QuotaReport {
	snap := b.ledger.Snapshot()
	rep := QuotaReport{Snapshot: snap, Shipments: b.ledger.Shipments()}
	if snap.CapacityKG > 0 {
		rep.UsedRatio = round4(snap.ReservedKG / snap.CapacityKG)
	}
	return rep
}

func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
