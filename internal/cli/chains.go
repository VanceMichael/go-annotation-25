package cli

import (
	"time"

	"wasteoil/internal/model"
	"wasteoil/internal/seed"
	"wasteoil/internal/trace"
)

func day(d int) time.Time {
	return time.Date(2026, time.August, d, 8, 0, 0, 0, time.UTC)
}

// buildChains 依据内置数据构建各票出口货物的溯源链。
//
// 各票货物共享「回收 -> 检测 -> 转化 -> 调配」的前缀，在调配环节之后
// 各自追加一个出口节点。
func buildChains() ([]trace.Chain, error) {
	// 公共前缀预留了额外容量，是真实场景中常见的做法。
	b := trace.NewBuilder(8)
	b.Add(model.Link{Kind: model.LinkPickup, RefID: "P-001", Actor: "回收单位甲", At: day(1), MassKG: seed.TotalPickupMassKG()})
	b.Add(model.Link{Kind: model.LinkAssay, RefID: "A-001", Actor: "检测中心", At: day(2), MassKG: seed.TotalPickupMassKG()})
	b.Add(model.Link{Kind: model.LinkConvert, RefID: "B-001", Actor: "转化厂", At: day(9), MassKG: 23580})
	b.Add(model.Link{Kind: model.LinkBlend, RefID: "M-001", Actor: "调配站", At: day(12), MassKG: 23580})

	plans := seed.Shipments()
	exports := make([]model.Link, 0, len(plans))
	ids := make([]string, 0, len(plans))
	for i, p := range plans {
		exports = append(exports, model.Link{
			Kind: model.LinkExport, RefID: p.ShipmentID, Actor: "报关行",
			At: day(14 + i), MassKG: p.MassKG,
		})
		ids = append(ids, p.ShipmentID)
	}
	return b.Split(exports, ids)
}

// chainTails 返回各条链的末节点引用编号。
func chainTails(chains []trace.Chain) []string {
	out := make([]string, 0, len(chains))
	for _, c := range chains {
		if tail, ok := c.Tail(); ok {
			out = append(out, tail.RefID)
		}
	}
	return out
}

// describeChains 返回各条链的可读描述。
func describeChains(chains []trace.Chain) []string {
	out := make([]string, 0, len(chains))
	for _, c := range chains {
		out = append(out, c.Describe())
	}
	return out
}
