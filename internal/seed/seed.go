// Package seed 提供内置的回收单位、回收登记、检测与批次样例数据。
//
// 样例取材于餐饮废弃油脂制生物柴油出口的典型链路，仅用于本地演练。
package seed

import (
	"fmt"
	"time"

	"wasteoil/internal/collect"
	"wasteoil/internal/model"
	"wasteoil/internal/quota"
)

// Year 是内置数据所属年度。
const Year = 2026

// QuotaCapacityKG 是内置年度出口配额上限。
const QuotaCapacityKG = 1_000_000.0

func day(d int) time.Time {
	return time.Date(2026, time.August, d, 8, 0, 0, 0, time.UTC)
}

// Collectors 返回内置回收单位。
func Collectors() []model.Collector {
	return []model.Collector{
		{ID: "C-001", Name: "沪东城市废油回收有限公司", City: "上海", LicenseNo: "SH-2024-0117", Active: true},
		{ID: "C-002", Name: "穗南餐厨资源循环公司", City: "广州", LicenseNo: "GD-2023-0482", Active: true},
		{ID: "C-003", Name: "蓉城油脂回收合作社", City: "成都", LicenseNo: "SC-2025-0093", Active: true},
		{ID: "C-004", Name: "旧证照回收站", City: "杭州", LicenseNo: "ZJ-2019-0021", Active: false},
	}
}

// Pickups 返回内置回收登记。
func Pickups() []model.Pickup {
	rows := []struct {
		id, collector string
		source        model.Source
		site          string
		mass          float64
		d             int
	}{
		{"P-001", "C-001", model.SourceRestaurant, "南京西路某火锅店", 4200, 1},
		{"P-002", "C-001", model.SourceCanteen, "某高校第三食堂", 6100, 2},
		{"P-003", "C-002", model.SourceRestaurant, "天河某粤菜馆", 3800, 3},
		{"P-004", "C-002", model.SourceGreaseTrap, "珠江新城隔油池", 2600, 4},
		{"P-005", "C-003", model.SourceProcessor, "某食品加工厂", 8300, 5},
		{"P-006", "C-003", model.SourceRestaurant, "春熙路某川菜馆", 3100, 6},
		{"P-007", "C-001", model.SourceGreaseTrap, "陆家嘴隔油池", 1900, 7},
	}
	out := make([]model.Pickup, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Pickup{
			ID: r.id, CollectorID: r.collector, Source: r.source,
			SiteName: r.site, MassKG: r.mass, PickedAt: day(r.d),
		})
	}
	return out
}

// Assays 返回内置检测结果。
func Assays() []model.Assay {
	rows := []struct {
		pickup        string
		ffa, moisture float64
		density       float64
		d             int
	}{
		{"P-001", 6.2, 1.4, 0.918, 1},
		{"P-002", 11.8, 3.1, 0.921, 2},
		{"P-003", 7.5, 1.9, 0.917, 3},
		{"P-004", 28.4, 9.6, 0.933, 4}, // 不合格
		{"P-005", 9.1, 2.6, 0.919, 5},
		{"P-006", 14.2, 3.8, 0.922, 6},
		{"P-007", 22.7, 6.4, 0.929, 7},
	}
	out := make([]model.Assay, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Assay{
			PickupID:        r.pickup,
			FFAPercent:      r.ffa,
			MoisturePercent: r.moisture,
			DensityKGPerL:   r.density,
			Grade:           model.GradeFor(r.ffa, r.moisture),
			TestedAt:        day(r.d),
		})
	}
	return out
}

// Batches 返回内置转化批次。
//
// 各批次的质量平衡均闭合：投料 = 产出质量 + 副产甘油 + 工艺损耗。
func Batches() []model.Batch {
	return []model.Batch{
		{
			ID: "B-001", PickupIDs: []string{"P-001", "P-002"},
			FeedMassKG: 10300, ProductVolumeL: 10812, ProductDensityKGPerL: 0.881,
			GlycerolMassKG: 618, LossMassKG: 156.6, ConvertedAt: day(9),
		},
		{
			ID: "B-002", PickupIDs: []string{"P-003", "P-005"},
			FeedMassKG: 12100, ProductVolumeL: 12700, ProductDensityKGPerL: 0.881,
			GlycerolMassKG: 726, LossMassKG: 185.3, ConvertedAt: day(10),
		},
		{
			ID: "B-003", PickupIDs: []string{"P-006"},
			FeedMassKG: 3100, ProductVolumeL: 3253, ProductDensityKGPerL: 0.881,
			GlycerolMassKG: 186, LossMassKG: 47.1, ConvertedAt: day(11),
		},
	}
}

// ShipmentPlan 描述一票出口货物的构成。
type ShipmentPlan struct {
	ShipmentID  string
	Destination string
	BatchIDs    []string
	MassKG      float64
}

// Shipments 返回内置出口计划。
func Shipments() []ShipmentPlan {
	return []ShipmentPlan{
		{ShipmentID: "S-001", Destination: "鹿特丹", BatchIDs: []string{"B-001"}, MassKG: 9525.4},
		{ShipmentID: "S-002", Destination: "新加坡", BatchIDs: []string{"B-002"}, MassKG: 11188.7},
		{ShipmentID: "S-003", Destination: "釜山", BatchIDs: []string{"B-003"}, MassKG: 2865.9},
	}
}

// TotalPickupMassKG 返回内置回收质量合计，供自检使用。
func TotalPickupMassKG() float64 {
	var sum float64
	for _, p := range Pickups() {
		sum += p.MassKG
	}
	return sum
}

// Load 构造带内置样例数据的回收台账与配额台账。
func Load() (*collect.Registry, *quota.Ledger, error) {
	reg := collect.New()
	for _, c := range Collectors() {
		if err := reg.AddCollector(c); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记回收单位 %s 失败: %w", c.ID, err)
		}
	}
	for _, p := range Pickups() {
		if err := reg.AddPickup(p); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记回收单 %s 失败: %w", p.ID, err)
		}
	}
	for _, a := range Assays() {
		if err := reg.RecordAssay(a); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记检测 %s 失败: %w", a.PickupID, err)
		}
	}
	ledger, err := quota.New(Year, QuotaCapacityKG)
	if err != nil {
		return nil, nil, fmt.Errorf("seed: 构造配额台账失败: %w", err)
	}
	return reg, ledger, nil
}
