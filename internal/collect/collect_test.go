package collect

import (
	"errors"
	"testing"
	"time"

	"wasteoil/internal/model"
)

func day(d int) time.Time {
	return time.Date(2026, time.August, d, 8, 0, 0, 0, time.UTC)
}

func activeCollector() model.Collector {
	return model.Collector{ID: "C-001", Name: "某回收公司", City: "上海", LicenseNo: "SH-2024-0117", Active: true}
}

func pickup(id, collector string) model.Pickup {
	return model.Pickup{
		ID: id, CollectorID: collector, Source: model.SourceRestaurant,
		SiteName: "某餐馆", MassKG: 4200, PickedAt: day(1),
	}
}

func seeded(t *testing.T) *Registry {
	t.Helper()
	r := New()
	if err := r.AddCollector(activeCollector()); err != nil {
		t.Fatalf("AddCollector 失败: %v", err)
	}
	return r
}

func TestAddPickupRequiresActiveCollector(t *testing.T) {
	r := New()
	if err := r.AddPickup(pickup("P-001", "C-001")); !errors.Is(err, model.ErrCollectorUnknown) {
		t.Fatalf("回收单位未登记应返回 ErrCollectorUnknown, 实际 %v", err)
	}

	inactive := activeCollector()
	inactive.ID = "C-002"
	inactive.Active = false
	if err := r.AddCollector(inactive); err != nil {
		t.Fatalf("AddCollector 失败: %v", err)
	}
	if err := r.AddPickup(pickup("P-002", "C-002")); !errors.Is(err, model.ErrCollectorInactive) {
		t.Fatalf("资质停用应返回 ErrCollectorInactive, 实际 %v", err)
	}
}

func TestAddPickupValidation(t *testing.T) {
	r := seeded(t)
	mutations := []func(p *model.Pickup){
		func(p *model.Pickup) { p.ID = "" },
		func(p *model.Pickup) { p.CollectorID = "" },
		func(p *model.Pickup) { p.Source = "meteor" },
		func(p *model.Pickup) { p.MassKG = 0 },
		func(p *model.Pickup) { p.PickedAt = time.Time{} },
	}
	for i, mutate := range mutations {
		p := pickup("P-001", "C-001")
		mutate(&p)
		if err := r.AddPickup(p); !errors.Is(err, model.ErrInvalidPickup) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidPickup, 实际 %v", i, err)
		}
	}
}

func TestPickupLookup(t *testing.T) {
	r := seeded(t)
	if err := r.AddPickup(pickup("P-001", "C-001")); err != nil {
		t.Fatalf("AddPickup 失败: %v", err)
	}
	if _, err := r.Pickup("P-001"); err != nil {
		t.Fatalf("Pickup 失败: %v", err)
	}
	if _, err := r.Pickup("NOPE"); !errors.Is(err, model.ErrPickupUnknown) {
		t.Fatalf("未知回收单应返回 ErrPickupUnknown, 实际 %v", err)
	}
	if got := r.TotalMassKG(); got != 4200 {
		t.Fatalf("质量合计 = %.1f, 期望 4200", got)
	}
}

func TestRecordAssayAndConvertible(t *testing.T) {
	r := seeded(t)
	for _, id := range []string{"P-001", "P-002"} {
		if err := r.AddPickup(pickup(id, "C-001")); err != nil {
			t.Fatalf("AddPickup 失败: %v", err)
		}
	}

	good := model.Assay{PickupID: "P-001", FFAPercent: 6.2, MoisturePercent: 1.4,
		DensityKGPerL: 0.918, Grade: model.GradeFor(6.2, 1.4), TestedAt: day(1)}
	bad := model.Assay{PickupID: "P-002", FFAPercent: 28.4, MoisturePercent: 9.6,
		DensityKGPerL: 0.933, Grade: model.GradeFor(28.4, 9.6), TestedAt: day(2)}

	if err := r.RecordAssay(good); err != nil {
		t.Fatalf("RecordAssay 失败: %v", err)
	}
	if err := r.RecordAssay(bad); err != nil {
		t.Fatalf("RecordAssay 失败: %v", err)
	}

	if got := r.ConvertiblePickups(); len(got) != 1 || got[0] != "P-001" {
		t.Fatalf("可转化回收单 = %v, 期望 [P-001]", got)
	}
	if _, err := r.Assay("NOPE"); !errors.Is(err, model.ErrAssayMissing) {
		t.Fatalf("未检测回收单应返回 ErrAssayMissing, 实际 %v", err)
	}
	// 检测必须挂在已登记的回收单上。
	orphan := good
	orphan.PickupID = "P-999"
	if err := r.RecordAssay(orphan); !errors.Is(err, model.ErrPickupUnknown) {
		t.Fatalf("回收单不存在应返回 ErrPickupUnknown, 实际 %v", err)
	}
}

func TestAssayValidation(t *testing.T) {
	r := seeded(t)
	if err := r.AddPickup(pickup("P-001", "C-001")); err != nil {
		t.Fatalf("AddPickup 失败: %v", err)
	}
	base := model.Assay{PickupID: "P-001", FFAPercent: 6.2, MoisturePercent: 1.4,
		DensityKGPerL: 0.918, Grade: model.GradeA, TestedAt: day(1)}
	mutations := []func(a *model.Assay){
		func(a *model.Assay) { a.PickupID = "" },
		func(a *model.Assay) { a.FFAPercent = -1 },
		func(a *model.Assay) { a.FFAPercent = 101 },
		func(a *model.Assay) { a.MoisturePercent = -1 },
		func(a *model.Assay) { a.DensityKGPerL = 0 },
		func(a *model.Assay) { a.Grade = "platinum" },
	}
	for i, mutate := range mutations {
		x := base
		mutate(&x)
		if err := r.RecordAssay(x); !errors.Is(err, model.ErrInvalidAssay) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidAssay, 实际 %v", i, err)
		}
	}
}

func TestCollectorValidationAndCounts(t *testing.T) {
	r := New()
	if err := r.AddCollector(model.Collector{Name: "x", LicenseNo: "y"}); err == nil {
		t.Errorf("空编号应返回错误")
	}
	if err := r.AddCollector(model.Collector{ID: "C-1", LicenseNo: "y"}); err == nil {
		t.Errorf("空名称应返回错误")
	}
	if err := r.AddCollector(model.Collector{ID: "C-1", Name: "x"}); err == nil {
		t.Errorf("缺少许可证号应返回错误")
	}

	if err := r.AddCollector(activeCollector()); err != nil {
		t.Fatalf("AddCollector 失败: %v", err)
	}
	inactive := activeCollector()
	inactive.ID = "C-002"
	inactive.Active = false
	if err := r.AddCollector(inactive); err != nil {
		t.Fatalf("AddCollector 失败: %v", err)
	}
	c := r.Counts()
	if c.Collectors != 2 || c.Active != 1 {
		t.Fatalf("Counts = %+v", c)
	}
	if _, err := r.Collector("NOPE"); !errors.Is(err, model.ErrCollectorUnknown) {
		t.Fatalf("未知回收单位应返回 ErrCollectorUnknown, 实际 %v", err)
	}
	if got := len(r.Collectors()); got != 2 {
		t.Fatalf("回收单位数 = %d", got)
	}
}

func TestPickupsBySourceAndSorting(t *testing.T) {
	r := seeded(t)
	specs := []struct {
		id     string
		source model.Source
		d      int
	}{
		{"P-003", model.SourceCanteen, 3},
		{"P-001", model.SourceRestaurant, 1},
		{"P-002", model.SourceRestaurant, 2},
	}
	for _, s := range specs {
		p := pickup(s.id, "C-001")
		p.Source = s.source
		p.PickedAt = day(s.d)
		if err := r.AddPickup(p); err != nil {
			t.Fatalf("AddPickup 失败: %v", err)
		}
	}
	all := r.Pickups()
	if len(all) != 3 || all[0].ID != "P-001" || all[2].ID != "P-003" {
		t.Fatalf("回收单排序异常: %+v", all)
	}
	if got := r.PickupsBySource(model.SourceRestaurant); len(got) != 2 {
		t.Fatalf("餐饮门店回收单数 = %d, 期望 2", len(got))
	}
	if got := r.PickupsBySource(model.SourceProcessor); len(got) != 0 {
		t.Fatalf("食品加工回收单数 = %d, 期望 0", len(got))
	}
}
