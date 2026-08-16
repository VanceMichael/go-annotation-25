// Package manifest 负责出口清单的组装与写出。
//
// 写出过程中的任何失败都必须通过返回值上报给调用方，
// 失败时不得留下可以正常回读的清单文件。
package manifest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"wasteoil/internal/convert"
	"wasteoil/internal/model"
	"wasteoil/internal/trace"
)

// Line 是清单中的一行。
type Line struct {
	ShipmentID  string   `json:"shipment_id"`
	Destination string   `json:"destination"`
	BatchIDs    []string `json:"batch_ids"`
	MassKG      float64  `json:"mass_kg"`
	// Ratio 是该票货物对应批次的加权平均得率。
	Ratio float64 `json:"ratio"`
	// TraceKinds 是溯源链节点类型序列。
	TraceKinds []string `json:"trace_kinds"`
}

// Manifest 是一份出口清单。
type Manifest struct {
	Year        int       `json:"year"`
	IssuedAt    time.Time `json:"issued_at"`
	Lines       []Line    `json:"lines"`
	TotalMassKG float64   `json:"total_mass_kg"`
	// MeanRatio 是全部批次的加权平均得率。
	MeanRatio float64 `json:"mean_ratio"`
}

// Validate 校验清单内容是否可以安全序列化并上报。
//
// 得率或质量出现非有限值（NaN、±Inf）时清单不得写出：
// 这类取值无法表示为合法 JSON，也说明上游计量存在问题。
func (m Manifest) Validate() error {
	if len(m.Lines) == 0 {
		return fmt.Errorf("%w: 清单没有任何明细行", model.ErrManifestWrite)
	}
	if !finite(m.TotalMassKG) {
		return fmt.Errorf("%w: 合计质量取值非法（%v）", model.ErrManifestWrite, m.TotalMassKG)
	}
	if !finite(m.MeanRatio) {
		return fmt.Errorf("%w: 平均得率取值非法（%v）", model.ErrManifestWrite, m.MeanRatio)
	}
	for _, l := range m.Lines {
		if !finite(l.MassKG) {
			return fmt.Errorf("%w: 货物 %s 质量取值非法（%v）",
				model.ErrManifestWrite, l.ShipmentID, l.MassKG)
		}
		if !finite(l.Ratio) {
			return fmt.Errorf("%w: 货物 %s 得率取值非法（%v）",
				model.ErrManifestWrite, l.ShipmentID, l.Ratio)
		}
	}
	return nil
}

// Build 依据溯源链与批次计量组装出口清单。
func Build(year int, issuedAt time.Time, chains []trace.Chain, yields map[string]convert.Yield, batchesOf map[string][]string, destinations map[string]string) (Manifest, error) {
	if len(chains) == 0 {
		return Manifest{}, fmt.Errorf("%w: 未提供溯源链", model.ErrManifestWrite)
	}
	m := Manifest{Year: year, IssuedAt: issuedAt}
	var feedTotal, productTotal float64

	trace.SortChains(chains)
	for _, c := range chains {
		batchIDs := batchesOf[c.ShipmentID]
		line := Line{
			ShipmentID:  c.ShipmentID,
			Destination: destinations[c.ShipmentID],
			BatchIDs:    batchIDs,
			MassKG:      c.MassKG(),
			TraceKinds:  c.Kinds(),
		}
		var feed, product float64
		for _, id := range batchIDs {
			y, ok := yields[id]
			if !ok {
				return Manifest{}, fmt.Errorf("%w: 批次 %s", model.ErrBatchUnknown, id)
			}
			feed += y.FeedMassKG
			product += y.ProductMassKG
		}
		if feed > 0 {
			line.Ratio = product / feed
		}
		feedTotal += feed
		productTotal += product
		m.TotalMassKG += line.MassKG
		m.Lines = append(m.Lines, line)
	}
	if feedTotal > 0 {
		m.MeanRatio = productTotal / feedTotal
	}
	return m, nil
}

// Write 把清单写出为缩进 JSON 文件。
//
// 写出过程中的任何失败都必须通过返回值上报，
// 且失败时不得留下可以正常回读的清单文件。
func Write(path string, m Manifest) (err error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return fmt.Errorf("%w: 创建目录 %s 失败: %v", model.ErrManifestWrite, dir, mkErr)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("%w: 创建清单文件 %s 失败: %v", model.ErrManifestWrite, path, err)
	}
	w := bufio.NewWriter(f)
	defer func() {
		if flushErr := w.Flush(); err == nil && flushErr != nil {
			err = fmt.Errorf("%w: 刷新清单文件 %s 失败: %v", model.ErrManifestWrite, path, flushErr)
		}
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("%w: 关闭清单文件 %s 失败: %v", model.ErrManifestWrite, path, closeErr)
		}
	}()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(m); encErr != nil {
		return fmt.Errorf("%w: 序列化清单 %s 失败: %v", model.ErrManifestWrite, path, encErr)
	}
	return nil
}

// Read 从文件读回清单。
func Read(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: 读取清单 %s 失败: %v", model.ErrManifestWrite, path, err)
	}
	var m Manifest
	if uerr := json.Unmarshal(data, &m); uerr != nil {
		return Manifest{}, fmt.Errorf("%w: 解析清单 %s 失败: %v", model.ErrManifestWrite, path, uerr)
	}
	return m, nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
