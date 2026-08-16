// Package trace 构建废弃油脂到出口成品的溯源链。
//
// 一批油脂在调配环节常常被拆成多票出口货物，每票各自延续一条溯源链。
// 因此从同一前缀派生多条链时，各条链必须持有独立存储：
// 每条链的末节点必须是它自己追加的那个出口节点。
package trace

import (
	"fmt"
	"sort"
	"strings"

	"wasteoil/internal/model"
)

// Chain 是一条溯源链。
type Chain struct {
	ShipmentID string       `json:"shipment_id"`
	Links      []model.Link `json:"links"`
}

// Len 返回链上节点数。
func (c Chain) Len() int {
	return len(c.Links)
}

// Tail 返回链尾节点。
func (c Chain) Tail() (model.Link, bool) {
	if len(c.Links) == 0 {
		return model.Link{}, false
	}
	return c.Links[len(c.Links)-1], true
}

// Kinds 返回链上节点类型序列。
func (c Chain) Kinds() []string {
	out := make([]string, 0, len(c.Links))
	for _, l := range c.Links {
		out = append(out, string(l.Kind))
	}
	return out
}

// Fork 从公共前缀派生一条新的溯源链并追加一个节点。
//
// 返回的链持有独立存储：对同一 base 多次 Fork，各条链互不覆盖。
func Fork(base []model.Link, extra model.Link) []model.Link {
	out := make([]model.Link, len(base), len(base)+1)
	copy(out, base)
	return append(out, extra)
}

// Builder 逐步构建溯源链前缀。
type Builder struct {
	links []model.Link
}

// NewBuilder 构造溯源链构建器，预留 capacity 个节点位置。
func NewBuilder(capacity int) *Builder {
	if capacity < 0 {
		capacity = 0
	}
	return &Builder{links: make([]model.Link, 0, capacity)}
}

// Add 追加一个节点。
func (b *Builder) Add(l model.Link) *Builder {
	b.links = append(b.links, l)
	return b
}

// Prefix 返回当前前缀。
func (b *Builder) Prefix() []model.Link {
	return b.links
}

// Split 把当前前缀分叉为多条出口溯源链。
//
// 每条链在公共前缀之后各自追加一个出口节点，彼此独立。
func (b *Builder) Split(exports []model.Link, shipmentIDs []string) ([]Chain, error) {
	if len(exports) != len(shipmentIDs) {
		return nil, fmt.Errorf("trace: 出口节点数 %d 与货物编号数 %d 不一致",
			len(exports), len(shipmentIDs))
	}
	if len(exports) == 0 {
		return nil, fmt.Errorf("%w: 未提供出口节点", model.ErrTraceBroken)
	}
	out := make([]Chain, 0, len(exports))
	for i, ex := range exports {
		out = append(out, Chain{ShipmentID: shipmentIDs[i], Links: Fork(b.links, ex)})
	}
	return out, nil
}

// Validate 校验一条溯源链的完整性：节点类型顺序合规且时间非递减。
func Validate(c Chain) error {
	if len(c.Links) == 0 {
		return fmt.Errorf("%w: 货物 %s 溯源链为空", model.ErrTraceBroken, c.ShipmentID)
	}
	if c.Links[0].Kind != model.LinkPickup {
		return fmt.Errorf("%w: 货物 %s 溯源链未从回收环节开始（首节点为 %s）",
			model.ErrTraceBroken, c.ShipmentID, c.Links[0].Kind)
	}
	tail, _ := c.Tail()
	if tail.Kind != model.LinkExport {
		return fmt.Errorf("%w: 货物 %s 溯源链未终止于出口环节（末节点为 %s）",
			model.ErrTraceBroken, c.ShipmentID, tail.Kind)
	}
	if tail.RefID != c.ShipmentID {
		return fmt.Errorf("%w: 货物 %s 溯源链末节点指向 %s",
			model.ErrTraceBroken, c.ShipmentID, tail.RefID)
	}
	for i := 1; i < len(c.Links); i++ {
		if c.Links[i].At.Before(c.Links[i-1].At) {
			return fmt.Errorf("%w: 货物 %s 溯源链第 %d 个节点时间回退",
				model.ErrTraceBroken, c.ShipmentID, i)
		}
	}
	return nil
}

// ValidateAll 校验多条链，并检查各链末节点互不相同。
func ValidateAll(chains []Chain) error {
	seenTails := make(map[string]string, len(chains))
	for _, c := range chains {
		if err := Validate(c); err != nil {
			return err
		}
		tail, _ := c.Tail()
		key := string(tail.Kind) + "|" + tail.RefID
		if other, dup := seenTails[key]; dup {
			return fmt.Errorf("%w: 货物 %s 与 %s 的溯源链末节点相同（%s）",
				model.ErrTraceBroken, c.ShipmentID, other, tail.RefID)
		}
		seenTails[key] = c.ShipmentID
	}
	return nil
}

// Describe 返回链的可读描述。
func (c Chain) Describe() string {
	parts := make([]string, 0, len(c.Links))
	for _, l := range c.Links {
		parts = append(parts, l.Kind.DisplayName()+"("+l.RefID+")")
	}
	return c.ShipmentID + ": " + strings.Join(parts, " -> ")
}

// MassKG 返回链尾节点的质量。
func (c Chain) MassKG() float64 {
	tail, ok := c.Tail()
	if !ok {
		return 0
	}
	return tail.MassKG
}

// SortChains 按货物编号排序。
func SortChains(items []Chain) {
	sort.Slice(items, func(i, j int) bool { return items[i].ShipmentID < items[j].ShipmentID })
}
