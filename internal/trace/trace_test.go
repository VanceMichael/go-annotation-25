package trace

import (
	"errors"
	"testing"
	"time"

	"wasteoil/internal/model"
)

func at(day int) time.Time {
	return time.Date(2026, time.August, day, 8, 0, 0, 0, time.UTC)
}

// prefixBuilder 构造一个预留了额外容量的公共前缀（回收 -> 检测 -> 转化 -> 调配）。
// 预留容量大于实际节点数，是真实场景中常见的做法。
func prefixBuilder() *Builder {
	b := NewBuilder(8)
	b.Add(model.Link{Kind: model.LinkPickup, RefID: "P-001", Actor: "回收单位甲", At: at(1), MassKG: 20000})
	b.Add(model.Link{Kind: model.LinkAssay, RefID: "A-001", Actor: "检测中心", At: at(2), MassKG: 20000})
	b.Add(model.Link{Kind: model.LinkConvert, RefID: "B-001", Actor: "转化厂", At: at(3), MassKG: 18501})
	b.Add(model.Link{Kind: model.LinkBlend, RefID: "M-001", Actor: "调配站", At: at(4), MassKG: 18501})
	return b
}

func exportLink(shipmentID string, day int, massKG float64) model.Link {
	return model.Link{Kind: model.LinkExport, RefID: shipmentID, Actor: "报关行", At: at(day), MassKG: massKG}
}

// TestForkDoesNotShareStorage 断言从同一前缀派生的两条链互不覆盖。
func TestForkDoesNotShareStorage(t *testing.T) {
	b := prefixBuilder()
	first := Fork(b.Prefix(), exportLink("S-001", 5, 10000))
	second := Fork(b.Prefix(), exportLink("S-002", 5, 8501))

	if len(first) != 5 || len(second) != 5 {
		t.Fatalf("链长 = %d / %d, 期望均为 5", len(first), len(second))
	}
	if first[4].RefID != "S-001" {
		t.Fatalf("第一条链末节点 = %s, 期望 S-001（不应被第二条覆盖）", first[4].RefID)
	}
	if second[4].RefID != "S-002" {
		t.Fatalf("第二条链末节点 = %s, 期望 S-002", second[4].RefID)
	}
	if first[4].MassKG != 10000 {
		t.Fatalf("第一条链末节点质量 = %.1f, 期望 10000", first[4].MassKG)
	}
}

// TestForkTwiceKeepsBothChains 断言两次派生后两条链的末节点互不相同。
func TestForkTwiceKeepsBothChains(t *testing.T) {
	b := prefixBuilder()
	chains := []Chain{
		{ShipmentID: "S-001", Links: Fork(b.Prefix(), exportLink("S-001", 5, 10000))},
		{ShipmentID: "S-002", Links: Fork(b.Prefix(), exportLink("S-002", 6, 8501))},
	}
	if err := ValidateAll(chains); err != nil {
		t.Fatalf("两条独立链应校验通过: %v", err)
	}
	t1, _ := chains[0].Tail()
	t2, _ := chains[1].Tail()
	if t1.RefID == t2.RefID {
		t.Fatalf("两条链末节点相同（均为 %s）, 说明共享了底层存储", t1.RefID)
	}
}

// TestForkThreeWaySplit 断言三路分叉时每条链保留自己的出口节点。
func TestForkThreeWaySplit(t *testing.T) {
	b := prefixBuilder()
	ids := []string{"S-001", "S-002", "S-003"}
	masses := []float64{7000, 6000, 5501}
	chains := make([]Chain, 0, len(ids))
	for i, id := range ids {
		chains = append(chains, Chain{ShipmentID: id, Links: Fork(b.Prefix(), exportLink(id, 5+i, masses[i]))})
	}
	for i, c := range chains {
		tail, ok := c.Tail()
		if !ok {
			t.Fatalf("第 %d 条链为空", i)
		}
		if tail.RefID != ids[i] {
			t.Fatalf("第 %d 条链末节点 = %s, 期望 %s", i, tail.RefID, ids[i])
		}
		if tail.MassKG != masses[i] {
			t.Fatalf("第 %d 条链末节点质量 = %.1f, 期望 %.1f", i, tail.MassKG, masses[i])
		}
	}
	if err := ValidateAll(chains); err != nil {
		t.Fatalf("三条独立链应校验通过: %v", err)
	}
}

// TestSplitProducesIndependentChains 断言 Split 产出的链互相独立。
func TestSplitProducesIndependentChains(t *testing.T) {
	b := prefixBuilder()
	exports := []model.Link{
		exportLink("S-001", 5, 10000),
		exportLink("S-002", 6, 8501),
	}
	chains, err := b.Split(exports, []string{"S-001", "S-002"})
	if err != nil {
		t.Fatalf("Split 返回错误: %v", err)
	}
	if len(chains) != 2 {
		t.Fatalf("链数 = %d, 期望 2", len(chains))
	}
	if err := ValidateAll(chains); err != nil {
		t.Fatalf("Split 产出的链应校验通过: %v", err)
	}
	if chains[0].MassKG() == chains[1].MassKG() {
		t.Fatalf("两条链质量相同（%.1f）, 疑似共享存储", chains[0].MassKG())
	}
}

// TestForkDoesNotMutatePrefix 断言派生不会改动公共前缀。
func TestForkDoesNotMutatePrefix(t *testing.T) {
	b := prefixBuilder()
	before := len(b.Prefix())
	beforeTail := b.Prefix()[before-1]

	Fork(b.Prefix(), exportLink("S-001", 5, 10000))
	Fork(b.Prefix(), exportLink("S-002", 6, 8501))

	if got := len(b.Prefix()); got != before {
		t.Fatalf("前缀长度 = %d, 期望 %d", got, before)
	}
	if b.Prefix()[before-1] != beforeTail {
		t.Fatalf("前缀末节点被改动\n期望 %+v\n实际 %+v", beforeTail, b.Prefix()[before-1])
	}
}

func TestValidateRequiresPickupFirst(t *testing.T) {
	c := Chain{ShipmentID: "S-001", Links: []model.Link{
		{Kind: model.LinkConvert, RefID: "B-001", At: at(3)},
		exportLink("S-001", 5, 100),
	}}
	if err := Validate(c); !errors.Is(err, model.ErrTraceBroken) {
		t.Fatalf("未从回收开始应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestValidateRequiresExportTail(t *testing.T) {
	b := prefixBuilder()
	c := Chain{ShipmentID: "S-001", Links: b.Prefix()}
	if err := Validate(c); !errors.Is(err, model.ErrTraceBroken) {
		t.Fatalf("未终止于出口应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestValidateRequiresMatchingTail(t *testing.T) {
	b := prefixBuilder()
	c := Chain{ShipmentID: "S-001", Links: Fork(b.Prefix(), exportLink("S-999", 5, 100))}
	if err := Validate(c); !errors.Is(err, model.ErrTraceBroken) {
		t.Fatalf("末节点指向不一致应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestValidateRejectsTimeRegression(t *testing.T) {
	b := prefixBuilder()
	c := Chain{ShipmentID: "S-001", Links: Fork(b.Prefix(), exportLink("S-001", 1, 100))}
	if err := Validate(c); !errors.Is(err, model.ErrTraceBroken) {
		t.Fatalf("时间回退应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestValidateEmptyChain(t *testing.T) {
	if err := Validate(Chain{ShipmentID: "S-001"}); !errors.Is(err, model.ErrTraceBroken) {
		t.Fatalf("空链应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestSplitValidation(t *testing.T) {
	b := prefixBuilder()
	if _, err := b.Split([]model.Link{exportLink("S-001", 5, 1)}, []string{"S-001", "S-002"}); err == nil {
		t.Errorf("数量不一致应返回错误")
	}
	if _, err := b.Split(nil, nil); !errors.Is(err, model.ErrTraceBroken) {
		t.Errorf("空出口节点应返回 ErrTraceBroken, 实际 %v", err)
	}
}

func TestChainHelpers(t *testing.T) {
	b := prefixBuilder()
	c := Chain{ShipmentID: "S-001", Links: Fork(b.Prefix(), exportLink("S-001", 5, 10000))}
	if c.Len() != 5 {
		t.Fatalf("Len = %d, 期望 5", c.Len())
	}
	if got := c.Kinds(); len(got) != 5 || got[0] != "pickup" || got[4] != "export" {
		t.Fatalf("Kinds = %v", got)
	}
	if c.MassKG() != 10000 {
		t.Fatalf("MassKG = %.1f, 期望 10000", c.MassKG())
	}
	if c.Describe() == "" {
		t.Fatalf("描述为空")
	}
	if _, ok := (Chain{}).Tail(); ok {
		t.Fatalf("空链 Tail 应返回 false")
	}
	if (Chain{}).MassKG() != 0 {
		t.Fatalf("空链质量应为 0")
	}
}

func TestSortChains(t *testing.T) {
	items := []Chain{{ShipmentID: "S-003"}, {ShipmentID: "S-001"}, {ShipmentID: "S-002"}}
	SortChains(items)
	if items[0].ShipmentID != "S-001" || items[2].ShipmentID != "S-003" {
		t.Fatalf("排序结果 = %+v", items)
	}
}

func TestNewBuilderNegativeCapacity(t *testing.T) {
	b := NewBuilder(-5)
	if len(b.Prefix()) != 0 {
		t.Fatalf("空构建器前缀应为空")
	}
	b.Add(model.Link{Kind: model.LinkPickup, RefID: "P-001", At: at(1)})
	if len(b.Prefix()) != 1 {
		t.Fatalf("追加后前缀长度 = %d, 期望 1", len(b.Prefix()))
	}
}
