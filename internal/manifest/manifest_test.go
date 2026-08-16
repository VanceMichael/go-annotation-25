package manifest

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wasteoil/internal/model"
)

func issuedAt() time.Time {
	return time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
}

func goodManifest() Manifest {
	return Manifest{
		Year:     2026,
		IssuedAt: issuedAt(),
		Lines: []Line{
			{ShipmentID: "S-001", Destination: "鹿特丹", BatchIDs: []string{"B-001"},
				MassKG: 18000, Ratio: 0.9612, TraceKinds: []string{"pickup", "convert", "export"}},
			{ShipmentID: "S-002", Destination: "新加坡", BatchIDs: []string{"B-002"},
				MassKG: 12500, Ratio: 0.9584, TraceKinds: []string{"pickup", "convert", "export"}},
		},
		TotalMassKG: 30500,
		MeanRatio:   0.9601,
	}
}

// TestWriteRoundTrip 断言正常清单写出后可完整读回。
func TestWriteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "manifest.json")
	m := goodManifest()
	if err := Write(path, m); err != nil {
		t.Fatalf("Write 返回错误: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read 返回错误: %v", err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("读回明细行数 = %d, 期望 2", len(got.Lines))
	}
	if got.TotalMassKG != m.TotalMassKG {
		t.Fatalf("合计质量 = %.3f, 期望 %.3f", got.TotalMassKG, m.TotalMassKG)
	}
}

// TestWriteSurfacesEncodeError 断言序列化失败必须通过返回值上报，不得被收尾逻辑吞掉。
func TestWriteSurfacesEncodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := goodManifest()
	// 得率为非有限值时无法表示为合法 JSON，序列化必然失败。
	m.Lines[1].Ratio = math.NaN()

	err := Write(path, m)
	if err == nil {
		t.Fatalf("序列化失败时 Write 应返回错误, 实际返回 nil")
	}
	if !errors.Is(err, model.ErrManifestWrite) {
		t.Fatalf("errors.Is(err, model.ErrManifestWrite) = false, 错误为 %v", err)
	}
}

// TestWriteSurfacesEncodeErrorForInfinity 断言无穷值同样必须上报。
func TestWriteSurfacesEncodeErrorForInfinity(t *testing.T) {
	for name, v := range map[string]float64{
		"正无穷": math.Inf(1),
		"负无穷": math.Inf(-1),
		"NaN": math.NaN(),
	} {
		path := filepath.Join(t.TempDir(), "manifest.json")
		m := goodManifest()
		m.MeanRatio = v
		if err := Write(path, m); err == nil {
			t.Errorf("%s: Write 应返回错误, 实际返回 nil", name)
		}
	}
}

// TestWriteFailureLeavesNoUsableManifest 断言写出失败时不得留下看起来成功的清单。
func TestWriteFailureLeavesNoUsableManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := goodManifest()
	m.Lines[0].MassKG = math.NaN()

	writeErr := Write(path, m)
	if writeErr == nil {
		t.Fatalf("Write 应返回错误")
	}
	// 写出失败后即使文件已创建，其内容也不应能被解析为完整清单。
	if _, rerr := Read(path); rerr == nil {
		t.Fatalf("写出失败后清单竟可正常解析, 说明失败未被如实上报")
	}
}

// TestWriteReportsErrorBeforeSuccessPath 断言成功与失败路径的返回值互不混淆。
func TestWriteReportsErrorBeforeSuccessPath(t *testing.T) {
	dir := t.TempDir()

	okPath := filepath.Join(dir, "ok.json")
	if err := Write(okPath, goodManifest()); err != nil {
		t.Fatalf("正常清单写出应成功: %v", err)
	}
	if _, err := Read(okPath); err != nil {
		t.Fatalf("正常清单应可读回: %v", err)
	}

	badPath := filepath.Join(dir, "bad.json")
	bad := goodManifest()
	bad.Lines[0].Ratio = math.NaN()
	if err := Write(badPath, bad); err == nil {
		t.Fatalf("非法清单写出应失败")
	}
}

func TestValidateDetectsNonFinite(t *testing.T) {
	if err := goodManifest().Validate(); err != nil {
		t.Fatalf("正常清单校验应通过: %v", err)
	}
	empty := goodManifest()
	empty.Lines = nil
	if err := empty.Validate(); !errors.Is(err, model.ErrManifestWrite) {
		t.Fatalf("空清单应返回 ErrManifestWrite, 实际 %v", err)
	}

	mutations := []func(m *Manifest){
		func(m *Manifest) { m.TotalMassKG = math.NaN() },
		func(m *Manifest) { m.MeanRatio = math.Inf(1) },
		func(m *Manifest) { m.Lines[0].MassKG = math.NaN() },
		func(m *Manifest) { m.Lines[1].Ratio = math.Inf(-1) },
	}
	for i, mutate := range mutations {
		m := goodManifest()
		mutate(&m)
		if err := m.Validate(); !errors.Is(err, model.ErrManifestWrite) {
			t.Errorf("第 %d 项非有限值应返回 ErrManifestWrite, 实际 %v", i, err)
		}
	}
}

func TestWriteCreatesMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "manifest.json")
	if err := Write(path, goodManifest()); err != nil {
		t.Fatalf("Write 返回错误: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("清单文件未生成: %v", err)
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, model.ErrManifestWrite) {
		t.Fatalf("读取不存在的文件应返回 ErrManifestWrite, 实际 %v", err)
	}
}
