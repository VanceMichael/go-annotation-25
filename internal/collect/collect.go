// Package collect 维护回收单位与回收登记台账。
package collect

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"wasteoil/internal/model"
)

// Registry 是线程安全的回收台账。
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]model.Collector
	pickups    map[string]model.Pickup
	assays     map[string]model.Assay
}

// New 构造空台账。
func New() *Registry {
	return &Registry{
		collectors: make(map[string]model.Collector),
		pickups:    make(map[string]model.Pickup),
		assays:     make(map[string]model.Assay),
	}
}

// AddCollector 登记回收单位。
func (r *Registry) AddCollector(c model.Collector) error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("collect: 回收单位编号为空")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("collect: 回收单位 %s 缺少名称", c.ID)
	}
	if strings.TrimSpace(c.LicenseNo) == "" {
		return fmt.Errorf("collect: 回收单位 %s 缺少许可证号", c.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[c.ID] = c
	return nil
}

// Collector 返回回收单位。
func (r *Registry) Collector(id string) (model.Collector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.collectors[id]
	if !ok {
		return model.Collector{}, fmt.Errorf("%w: %s", model.ErrCollectorUnknown, id)
	}
	return c, nil
}

// ActiveCollector 返回资质有效的回收单位。
func (r *Registry) ActiveCollector(id string) (model.Collector, error) {
	c, err := r.Collector(id)
	if err != nil {
		return model.Collector{}, err
	}
	if !c.Active {
		return model.Collector{}, fmt.Errorf("%w: %s", model.ErrCollectorInactive, id)
	}
	return c, nil
}

// Collectors 返回全部回收单位，按编号排序。
func (r *Registry) Collectors() []model.Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Collector, 0, len(r.collectors))
	for _, c := range r.collectors {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AddPickup 登记一次回收。
func (r *Registry) AddPickup(p model.Pickup) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if _, err := r.ActiveCollector(p.CollectorID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pickups[p.ID] = p
	return nil
}

// Pickup 返回回收登记。
func (r *Registry) Pickup(id string) (model.Pickup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pickups[id]
	if !ok {
		return model.Pickup{}, fmt.Errorf("%w: %s", model.ErrPickupUnknown, id)
	}
	return p, nil
}

// Pickups 返回全部回收登记，按时间排序。
func (r *Registry) Pickups() []model.Pickup {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Pickup, 0, len(r.pickups))
	for _, p := range r.pickups {
		out = append(out, p)
	}
	model.SortPickups(out)
	return out
}

// PickupsBySource 返回指定来源类型的回收登记。
func (r *Registry) PickupsBySource(s model.Source) []model.Pickup {
	all := r.Pickups()
	out := make([]model.Pickup, 0, len(all))
	for _, p := range all {
		if p.Source == s {
			out = append(out, p)
		}
	}
	return out
}

// RecordAssay 登记一次检测结果。
func (r *Registry) RecordAssay(a model.Assay) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if _, err := r.Pickup(a.PickupID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assays[a.PickupID] = a
	return nil
}

// Assay 返回某回收单的检测结果。
func (r *Registry) Assay(pickupID string) (model.Assay, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.assays[pickupID]
	if !ok {
		return model.Assay{}, fmt.Errorf("%w: 回收单 %s", model.ErrAssayMissing, pickupID)
	}
	return a, nil
}

// ConvertiblePickups 返回检测合格、可进入转化的回收单号，按字典序排序。
func (r *Registry) ConvertiblePickups() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.assays))
	for id, a := range r.assays {
		if a.Grade.Convertible() {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// TotalMassKG 返回全部回收登记的质量合计。
func (r *Registry) TotalMassKG() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sum float64
	for _, p := range r.pickups {
		sum += p.MassKG
	}
	return sum
}

// Counts 汇总台账规模。
type Counts struct {
	Collectors int `json:"collectors"`
	Active     int `json:"active_collectors"`
	Pickups    int `json:"pickups"`
	Assays     int `json:"assays"`
	Rejected   int `json:"rejected"`
}

// Counts 返回台账规模统计。
func (r *Registry) Counts() Counts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := Counts{Collectors: len(r.collectors), Pickups: len(r.pickups), Assays: len(r.assays)}
	for _, x := range r.collectors {
		if x.Active {
			c.Active++
		}
	}
	for _, a := range r.assays {
		if !a.Grade.Convertible() {
			c.Rejected++
		}
	}
	return c
}
