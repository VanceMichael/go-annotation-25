package model

import (
	"errors"
	"testing"
	"time"
)

func TestParseGradeAndConvertible(t *testing.T) {
	convertible := map[Grade]bool{GradeA: true, GradeB: true, GradeC: true, GradeReject: false}
	for _, g := range AllGrades() {
		got, err := ParseGrade(string(g))
		if err != nil || got != g {
			t.Fatalf("ParseGrade(%q) = %q, %v", g, got, err)
		}
		if g.Convertible() != convertible[g] {
			t.Errorf("%s.Convertible() = %v, 期望 %v", g, g.Convertible(), convertible[g])
		}
		if g.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", g)
		}
	}
	if _, err := ParseGrade("  A "); err != nil {
		t.Fatalf("应忽略大小写与空白: %v", err)
	}
	if _, err := ParseGrade("platinum"); !errors.Is(err, ErrUnknownGrade) {
		t.Fatalf("未知等级应返回 ErrUnknownGrade, 实际 %v", err)
	}
	if Grade("x").DisplayName() != "x" {
		t.Errorf("未知等级应回落为原值")
	}
}

func TestParseSource(t *testing.T) {
	for _, s := range AllSources() {
		got, err := ParseSource(string(s))
		if err != nil || got != s {
			t.Fatalf("ParseSource(%q) = %q, %v", s, got, err)
		}
		if s.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", s)
		}
	}
	if _, err := ParseSource("meteor"); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("未知来源应返回 ErrUnknownSource, 实际 %v", err)
	}
	if Source("x").DisplayName() != "x" {
		t.Errorf("未知来源应回落为原值")
	}
}

// TestGradeFor 断言品质等级判定依据游离脂肪酸与水杂含量。
func TestGradeFor(t *testing.T) {
	cases := []struct {
		ffa, moisture float64
		want          Grade
	}{
		{6.2, 1.4, GradeA},
		{8.0, 2.0, GradeA},
		{8.1, 2.0, GradeB},
		{11.8, 3.1, GradeB},
		{15.0, 4.0, GradeB},
		{15.1, 4.0, GradeC},
		{22.7, 6.4, GradeC},
		{25.1, 1.0, GradeReject},
		{10.0, 8.1, GradeReject},
	}
	for _, tc := range cases {
		if got := GradeFor(tc.ffa, tc.moisture); got != tc.want {
			t.Errorf("GradeFor(%.1f, %.1f) = %s, 期望 %s", tc.ffa, tc.moisture, got, tc.want)
		}
	}
}

func TestPickupValidate(t *testing.T) {
	base := Pickup{
		ID: "P-001", CollectorID: "C-001", Source: SourceRestaurant,
		SiteName: "某餐馆", MassKG: 4200,
		PickedAt: time.Date(2026, time.August, 1, 8, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法回收单不应报错: %v", err)
	}
	mutations := []func(p *Pickup){
		func(p *Pickup) { p.ID = "" },
		func(p *Pickup) { p.CollectorID = " " },
		func(p *Pickup) { p.Source = "meteor" },
		func(p *Pickup) { p.MassKG = 0 },
		func(p *Pickup) { p.MassKG = -1 },
		func(p *Pickup) { p.PickedAt = time.Time{} },
	}
	for i, mutate := range mutations {
		p := base
		mutate(&p)
		if err := p.Validate(); !errors.Is(err, ErrInvalidPickup) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidPickup, 实际 %v", i, err)
		}
	}
}

func TestAssayValidate(t *testing.T) {
	base := Assay{
		PickupID: "P-001", FFAPercent: 6.2, MoisturePercent: 1.4,
		DensityKGPerL: 0.918, Grade: GradeA,
		TestedAt: time.Date(2026, time.August, 1, 8, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法检测不应报错: %v", err)
	}
	mutations := []func(a *Assay){
		func(a *Assay) { a.PickupID = "" },
		func(a *Assay) { a.FFAPercent = -1 },
		func(a *Assay) { a.FFAPercent = 101 },
		func(a *Assay) { a.MoisturePercent = -1 },
		func(a *Assay) { a.MoisturePercent = 101 },
		func(a *Assay) { a.DensityKGPerL = 0 },
		func(a *Assay) { a.Grade = "platinum" },
	}
	for i, mutate := range mutations {
		x := base
		mutate(&x)
		if err := x.Validate(); !errors.Is(err, ErrInvalidAssay) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidAssay, 实际 %v", i, err)
		}
	}
}

func TestBatchValidate(t *testing.T) {
	base := Batch{
		ID: "B-001", PickupIDs: []string{"P-001"},
		FeedMassKG: 10000, ProductVolumeL: 10500, ProductDensityKGPerL: 0.881,
		GlycerolMassKG: 600, LossMassKG: 150,
		ConvertedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法批次不应报错: %v", err)
	}
	mutations := []func(b *Batch){
		func(b *Batch) { b.ID = "" },
		func(b *Batch) { b.PickupIDs = nil },
		func(b *Batch) { b.FeedMassKG = 0 },
		func(b *Batch) { b.ProductVolumeL = 0 },
		func(b *Batch) { b.ProductDensityKGPerL = 0 },
		func(b *Batch) { b.GlycerolMassKG = -1 },
		func(b *Batch) { b.LossMassKG = -1 },
		func(b *Batch) { b.ConvertedAt = time.Time{} },
	}
	for i, mutate := range mutations {
		x := base
		mutate(&x)
		if err := x.Validate(); !errors.Is(err, ErrInvalidBatch) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidBatch, 实际 %v", i, err)
		}
	}
}

func TestLinkKindDisplay(t *testing.T) {
	kinds := []LinkKind{LinkPickup, LinkAssay, LinkConvert, LinkBlend, LinkExport}
	for _, k := range kinds {
		if k.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", k)
		}
	}
	if LinkKind("x").DisplayName() != "x" {
		t.Errorf("未知节点类型应回落为原值")
	}
}

func TestSortPickups(t *testing.T) {
	t0 := time.Date(2026, time.August, 1, 8, 0, 0, 0, time.UTC)
	items := []Pickup{
		{ID: "P-003", PickedAt: t0.Add(24 * time.Hour)},
		{ID: "P-002", PickedAt: t0},
		{ID: "P-001", PickedAt: t0},
	}
	SortPickups(items)
	if items[0].ID != "P-001" || items[1].ID != "P-002" || items[2].ID != "P-003" {
		t.Fatalf("排序结果 = %+v", items)
	}
}
