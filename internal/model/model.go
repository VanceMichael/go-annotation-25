// Package model 定义废弃油脂回收与生物柴油出口溯源平台的领域模型。
//
// 平台覆盖餐饮废弃油脂的回收登记、品质检测、转化计量、出口配额管理、
// 溯源链构建与出口清单导出。
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Grade 表示废弃油脂品质等级。
type Grade string

const (
	// GradeA 一级，游离脂肪酸含量最低。
	GradeA Grade = "a"
	// GradeB 二级。
	GradeB Grade = "b"
	// GradeC 三级。
	GradeC Grade = "c"
	// GradeReject 不合格，不得进入转化。
	GradeReject Grade = "reject"
)

// AllGrades 返回全部品质等级。
func AllGrades() []Grade {
	return []Grade{GradeA, GradeB, GradeC, GradeReject}
}

// DisplayName 返回品质等级中文名。
func (g Grade) DisplayName() string {
	switch g {
	case GradeA:
		return "一级"
	case GradeB:
		return "二级"
	case GradeC:
		return "三级"
	case GradeReject:
		return "不合格"
	default:
		return string(g)
	}
}

// Convertible 报告该等级是否允许进入转化工序。
func (g Grade) Convertible() bool {
	return g == GradeA || g == GradeB || g == GradeC
}

// ParseGrade 解析品质等级代码。
func ParseGrade(s string) (Grade, error) {
	v := Grade(strings.ToLower(strings.TrimSpace(s)))
	for _, g := range AllGrades() {
		if v == g {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownGrade, s)
}

// Source 表示废弃油脂来源类型。
type Source string

const (
	// SourceRestaurant 餐饮门店。
	SourceRestaurant Source = "restaurant"
	// SourceCanteen 单位食堂。
	SourceCanteen Source = "canteen"
	// SourceProcessor 食品加工企业。
	SourceProcessor Source = "processor"
	// SourceGreaseTrap 隔油池清捞。
	SourceGreaseTrap Source = "grease-trap"
)

// AllSources 返回全部来源类型。
func AllSources() []Source {
	return []Source{SourceRestaurant, SourceCanteen, SourceProcessor, SourceGreaseTrap}
}

// DisplayName 返回来源类型中文名。
func (s Source) DisplayName() string {
	switch s {
	case SourceRestaurant:
		return "餐饮门店"
	case SourceCanteen:
		return "单位食堂"
	case SourceProcessor:
		return "食品加工企业"
	case SourceGreaseTrap:
		return "隔油池清捞"
	default:
		return string(s)
	}
}

// ParseSource 解析来源类型代码。
func ParseSource(s string) (Source, error) {
	v := Source(strings.ToLower(strings.TrimSpace(s)))
	for _, k := range AllSources() {
		if v == k {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownSource, s)
}

// Collector 表示回收单位。
type Collector struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	City      string `json:"city"`
	LicenseNo string `json:"license_no"`
	Active    bool   `json:"active"`
}

// Pickup 表示一次回收登记。
type Pickup struct {
	ID          string `json:"id"`
	CollectorID string `json:"collector_id"`
	Source      Source `json:"source"`
	SiteName    string `json:"site_name"`
	// MassKG 是回收质量，单位千克。
	MassKG   float64   `json:"mass_kg"`
	PickedAt time.Time `json:"picked_at"`
}

// Validate 校验回收登记。
func (p Pickup) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("%w: 回收单号为空", ErrInvalidPickup)
	}
	if strings.TrimSpace(p.CollectorID) == "" {
		return fmt.Errorf("%w: 回收单 %s 缺少回收单位", ErrInvalidPickup, p.ID)
	}
	if _, err := ParseSource(string(p.Source)); err != nil {
		return fmt.Errorf("%w: 回收单 %s 来源类型非法", ErrInvalidPickup, p.ID)
	}
	if p.MassKG <= 0 {
		return fmt.Errorf("%w: 回收单 %s 质量必须为正", ErrInvalidPickup, p.ID)
	}
	if p.PickedAt.IsZero() {
		return fmt.Errorf("%w: 回收单 %s 缺少回收时间", ErrInvalidPickup, p.ID)
	}
	return nil
}

// Assay 表示一次品质检测结果。
type Assay struct {
	PickupID string `json:"pickup_id"`
	// FFAPercent 是游离脂肪酸含量，单位百分比。
	FFAPercent float64 `json:"ffa_percent"`
	// MoisturePercent 是水分与杂质含量，单位百分比。
	MoisturePercent float64 `json:"moisture_percent"`
	// DensityKGPerL 是密度，单位千克每升。
	DensityKGPerL float64   `json:"density_kg_per_l"`
	Grade         Grade     `json:"grade"`
	TestedAt      time.Time `json:"tested_at"`
}

// Validate 校验检测结果。
func (a Assay) Validate() error {
	if strings.TrimSpace(a.PickupID) == "" {
		return fmt.Errorf("%w: 缺少回收单号", ErrInvalidAssay)
	}
	if a.FFAPercent < 0 || a.FFAPercent > 100 {
		return fmt.Errorf("%w: 回收单 %s 游离脂肪酸含量越界", ErrInvalidAssay, a.PickupID)
	}
	if a.MoisturePercent < 0 || a.MoisturePercent > 100 {
		return fmt.Errorf("%w: 回收单 %s 水杂含量越界", ErrInvalidAssay, a.PickupID)
	}
	if a.DensityKGPerL <= 0 {
		return fmt.Errorf("%w: 回收单 %s 密度必须为正", ErrInvalidAssay, a.PickupID)
	}
	if _, err := ParseGrade(string(a.Grade)); err != nil {
		return fmt.Errorf("%w: 回收单 %s 品质等级非法", ErrInvalidAssay, a.PickupID)
	}
	return nil
}

// Batch 表示一个转化批次。
type Batch struct {
	ID string `json:"id"`
	// PickupIDs 是纳入该批次的回收单号。
	PickupIDs []string `json:"pickup_ids"`
	// FeedMassKG 是投料质量，单位千克。
	FeedMassKG float64 `json:"feed_mass_kg"`
	// ProductVolumeL 是产出生物柴油体积，单位升。
	ProductVolumeL float64 `json:"product_volume_l"`
	// ProductDensityKGPerL 是产出生物柴油密度，单位千克每升。
	ProductDensityKGPerL float64 `json:"product_density_kg_per_l"`
	// GlycerolMassKG 是副产甘油质量，单位千克。
	GlycerolMassKG float64 `json:"glycerol_mass_kg"`
	// LossMassKG 是工艺损耗质量，单位千克。
	LossMassKG  float64   `json:"loss_mass_kg"`
	ConvertedAt time.Time `json:"converted_at"`
}

// Validate 校验转化批次。
func (b Batch) Validate() error {
	if strings.TrimSpace(b.ID) == "" {
		return fmt.Errorf("%w: 批次号为空", ErrInvalidBatch)
	}
	if len(b.PickupIDs) == 0 {
		return fmt.Errorf("%w: 批次 %s 未纳入任何回收单", ErrInvalidBatch, b.ID)
	}
	if b.FeedMassKG <= 0 {
		return fmt.Errorf("%w: 批次 %s 投料质量必须为正", ErrInvalidBatch, b.ID)
	}
	if b.ProductVolumeL <= 0 {
		return fmt.Errorf("%w: 批次 %s 产出体积必须为正", ErrInvalidBatch, b.ID)
	}
	if b.ProductDensityKGPerL <= 0 {
		return fmt.Errorf("%w: 批次 %s 产出密度必须为正", ErrInvalidBatch, b.ID)
	}
	if b.GlycerolMassKG < 0 || b.LossMassKG < 0 {
		return fmt.Errorf("%w: 批次 %s 副产与损耗不得为负", ErrInvalidBatch, b.ID)
	}
	if b.ConvertedAt.IsZero() {
		return fmt.Errorf("%w: 批次 %s 缺少转化时间", ErrInvalidBatch, b.ID)
	}
	return nil
}

// LinkKind 表示溯源链节点类型。
type LinkKind string

const (
	// LinkPickup 回收环节。
	LinkPickup LinkKind = "pickup"
	// LinkAssay 检测环节。
	LinkAssay LinkKind = "assay"
	// LinkConvert 转化环节。
	LinkConvert LinkKind = "convert"
	// LinkBlend 调配环节。
	LinkBlend LinkKind = "blend"
	// LinkExport 出口环节。
	LinkExport LinkKind = "export"
)

// DisplayName 返回节点类型中文名。
func (k LinkKind) DisplayName() string {
	switch k {
	case LinkPickup:
		return "回收"
	case LinkAssay:
		return "检测"
	case LinkConvert:
		return "转化"
	case LinkBlend:
		return "调配"
	case LinkExport:
		return "出口"
	default:
		return string(k)
	}
}

// Link 是溯源链上的一个节点。
type Link struct {
	Kind   LinkKind  `json:"kind"`
	RefID  string    `json:"ref_id"`
	Actor  string    `json:"actor"`
	At     time.Time `json:"at"`
	MassKG float64   `json:"mass_kg"`
}

// Shipment 表示一票出口货物。
type Shipment struct {
	ID          string   `json:"id"`
	BatchIDs    []string `json:"batch_ids"`
	Destination string   `json:"destination"`
	// MassKG 是出口质量，单位千克。
	MassKG     float64   `json:"mass_kg"`
	DeclaredAt time.Time `json:"declared_at"`
}

// SortPickups 按回收时间与单号排序，保证输出稳定。
func SortPickups(items []Pickup) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].PickedAt.Equal(items[j].PickedAt) {
			return items[i].PickedAt.Before(items[j].PickedAt)
		}
		return items[i].ID < items[j].ID
	})
}

// GradeFor 依据游离脂肪酸与水杂含量判定品质等级。
func GradeFor(ffaPercent, moisturePercent float64) Grade {
	switch {
	case ffaPercent > 25 || moisturePercent > 8:
		return GradeReject
	case ffaPercent <= 8 && moisturePercent <= 2:
		return GradeA
	case ffaPercent <= 15 && moisturePercent <= 4:
		return GradeB
	default:
		return GradeC
	}
}
