package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"wasteoil/internal/seed"
)

func run(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var payload map[string]any
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			payload = nil
		}
	}
	return code, payload, stderr.String()
}

func TestHelpAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != ExitOK {
		t.Fatalf("无参数退出码 = %d", code)
	}
	if stdout.Len() == 0 {
		t.Fatalf("应输出用法说明")
	}
	stdout.Reset()
	if code := Run([]string{"version"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("version 退出码 = %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(Version)) {
		t.Fatalf("version 输出 = %q", stdout.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	if code, _, _ := run(t, "teleport"); code != ExitUsage {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitUsage)
	}
}

// TestCustomsSubmitHonoursTimeout 断言海关申报在调用方超时后立即返回、退出码 4。
func TestCustomsSubmitHonoursTimeout(t *testing.T) {
	code, payload, stderr := run(t,
		"customs", "submit", "--shipment", "S-001", "--mass", "9525.4",
		"--timeout", "120ms", "--customs-latency", "5s",
	)
	if code != ExitAborted {
		t.Fatalf("退出码 = %d, 期望 %d; stdout=%+v stderr=%s", code, ExitAborted, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("超时不应报成功: %+v", payload)
	}
	if elapsed, _ := payload["elapsed_ms"].(float64); elapsed > 1500 {
		t.Fatalf("耗时 = %v ms, 期望远小于 1500 ms（通道耗时 5s）", elapsed)
	}
}

// TestCustomsProbeHonoursTimeout 断言通道探测在超时后立即返回。
func TestCustomsProbeHonoursTimeout(t *testing.T) {
	code, payload, stderr := run(t, "customs", "probe", "--timeout", "80ms", "--customs-latency", "5s")
	if code != ExitAborted {
		t.Fatalf("退出码 = %d, 期望 %d; stdout=%+v stderr=%s", code, ExitAborted, payload, stderr)
	}
	if elapsed, _ := payload["elapsed_ms"].(float64); elapsed > 1500 {
		t.Fatalf("耗时 = %v ms, 期望远小于 1500 ms", elapsed)
	}
}

func TestCustomsSubmitFast(t *testing.T) {
	code, payload, stderr := run(t, "customs", "submit", "--timeout", "3s")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if serial, _ := payload["serial_no"].(string); serial == "" {
		t.Fatalf("缺少申报流水号: %+v", payload)
	}
}

// TestQuotaReserveNeverOversells 断言并发预留出口配额不会超卖。
func TestQuotaReserveNeverOversells(t *testing.T) {
	code, payload, stderr := run(t,
		"quota", "reserve",
		"--workers", "64", "--per-worker", "5", "--mass", "10", "--capacity", "1000",
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if oversold, _ := payload["oversold"].(bool); oversold {
		t.Fatalf("发生超卖: %+v", payload)
	}
	grants, _ := payload["grants"].(float64)
	maxGrants, _ := payload["max_grants"].(float64)
	if grants > maxGrants {
		t.Fatalf("成功预留 %v 次, 上限 %v 次", grants, maxGrants)
	}
	if remaining, _ := payload["remaining_kg"].(float64); remaining < 0 {
		t.Fatalf("剩余额度为负: %v", remaining)
	}
	if grants != maxGrants {
		t.Fatalf("成功预留 %v 次, 期望恰好用满 %v 次", grants, maxGrants)
	}
}

// TestQuotaReserveExactFit 断言请求量刚好等于额度时全部成功。
func TestQuotaReserveExactFit(t *testing.T) {
	code, payload, stderr := run(t,
		"quota", "reserve",
		"--workers", "50", "--per-worker", "1", "--mass", "20", "--capacity", "1000",
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if grants, _ := payload["grants"].(float64); grants != 50 {
		t.Fatalf("成功预留 %v 次, 期望 50", grants)
	}
	if rejects, _ := payload["rejects"].(float64); rejects != 0 {
		t.Fatalf("拒绝 %v 次, 期望 0", rejects)
	}
}

func TestQuotaReserveBadArgs(t *testing.T) {
	if code, _, _ := run(t, "quota", "reserve", "--workers", "0"); code != ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

// TestTraceBuildChainsIndependent 断言溯源链分叉后各链末节点互不相同。
func TestTraceBuildChainsIndependent(t *testing.T) {
	code, payload, stderr := run(t, "trace", "build")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("ok = false: %+v", payload)
	}
	chains, _ := payload["chains"].(float64)
	if int(chains) != len(seed.Shipments()) {
		t.Fatalf("链数 = %v, 期望 %d", chains, len(seed.Shipments()))
	}
	tails, _ := payload["tails"].([]any)
	if len(tails) != len(seed.Shipments()) {
		t.Fatalf("末节点数 = %d, 期望 %d", len(tails), len(seed.Shipments()))
	}
	seen := map[string]bool{}
	for _, raw := range tails {
		id, _ := raw.(string)
		if seen[id] {
			t.Fatalf("末节点 %s 重复, 说明各链共享了底层存储: %v", id, tails)
		}
		seen[id] = true
	}
	for _, s := range seed.Shipments() {
		if !seen[s.ShipmentID] {
			t.Fatalf("货物 %s 的末节点缺失: %v", s.ShipmentID, tails)
		}
	}
}

// TestManifestWriteReportsFailure 断言清单写出失败时如实上报，不报成功。
func TestManifestWriteReportsFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "manifest.json")
	code, payload, stderr := run(t, "manifest", "write", "--out", out, "--inject-nonfinite")
	if code != ExitData {
		t.Fatalf("退出码 = %d, 期望 %d; stdout=%+v stderr=%s", code, ExitData, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("写出失败不应报成功: %+v", payload)
	}
	if readable, _ := payload["readable"].(bool); readable {
		t.Fatalf("写出失败后清单竟可正常回读: %+v", payload)
	}
}

// TestManifestWriteSucceeds 断言正常清单可写出并回读。
func TestManifestWriteSucceeds(t *testing.T) {
	out := filepath.Join(t.TempDir(), "manifest.json")
	code, payload, stderr := run(t, "manifest", "write", "--out", out)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if readable, _ := payload["readable"].(bool); !readable {
		t.Fatalf("清单应可回读: %+v", payload)
	}
	if lines, _ := payload["lines"].(float64); int(lines) != len(seed.Shipments()) {
		t.Fatalf("明细行数 = %v, 期望 %d", lines, len(seed.Shipments()))
	}
}

// TestConvertComputeRatioPlausible 断言转化得率按质量口径计算并落在正常区间。
func TestConvertComputeRatioPlausible(t *testing.T) {
	code, payload, stderr := run(t, "convert", "compute")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if impl, _ := payload["implausible"].(float64); impl != 0 {
		t.Fatalf("异常得率批次数 = %v, 期望 0", impl)
	}
	if unbal, _ := payload["unbalanced"].(float64); unbal != 0 {
		t.Fatalf("质量平衡不闭合批次数 = %v, 期望 0", unbal)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 summary: %+v", payload)
	}
	mean, _ := summary["mean_ratio"].(float64)
	if mean < 0.90 || mean > 1.0 {
		t.Fatalf("加权平均得率 = %v, 期望落在 0.90-1.0", mean)
	}
}

func TestConvertComputeSingleBatch(t *testing.T) {
	code, payload, stderr := run(t, "convert", "compute", "--batch", "B-001")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if batches, _ := payload["batches"].([]any); len(batches) != 1 {
		t.Fatalf("批次数 = %d, 期望 1", len(batches))
	}
	if code, _, _ := run(t, "convert", "compute", "--batch", "NOPE"); code != ExitNotFound {
		t.Fatalf("未知批次退出码 = %d, 期望 %d", code, ExitNotFound)
	}
}

// TestSelfcheckPasses 断言内置自检全部通过。
func TestSelfcheckPasses(t *testing.T) {
	code, payload, stderr := run(t, "selfcheck")
	if code != ExitOK {
		t.Fatalf("自检退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if failed, _ := payload["failed"].(float64); failed != 0 {
		t.Fatalf("自检失败项 = %v, 期望 0; 输出 %+v", payload["failed"], payload)
	}
}

func TestListCommands(t *testing.T) {
	code, payload, stderr := run(t, "collector", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if items, _ := payload["collectors"].([]any); len(items) != len(seed.Collectors()) {
		t.Fatalf("回收单位数 = %d", len(items))
	}

	code, payload, stderr = run(t, "pickup", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if items, _ := payload["pickups"].([]any); len(items) != len(seed.Pickups()) {
		t.Fatalf("回收单数 = %d", len(items))
	}
	if total, _ := payload["total_mass_kg"].(float64); total != seed.TotalPickupMassKG() {
		t.Fatalf("质量合计 = %v, 期望 %v", total, seed.TotalPickupMassKG())
	}

	code, payload, stderr = run(t, "assay", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	// 7 条检测中仅 P-004（FFA 28.4%、水杂 9.6%）不合格，可转化应为 6 条。
	if items, _ := payload["convertible"].([]any); len(items) != 6 {
		t.Fatalf("可转化回收单数 = %d, 期望 6", len(items))
	}
}

func TestPickupListBySource(t *testing.T) {
	code, payload, stderr := run(t, "pickup", "list", "--source", "restaurant")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if items, _ := payload["pickups"].([]any); len(items) != 3 {
		t.Fatalf("餐饮门店回收单数 = %d, 期望 3", len(items))
	}
	if code, _, _ := run(t, "pickup", "list", "--source", "nope"); code != ExitBadRequest {
		t.Fatalf("非法来源退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

func TestReportCommands(t *testing.T) {
	code, payload, stderr := run(t, "report", "intake")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if pickups, _ := payload["pickups"].(float64); int(pickups) != len(seed.Pickups()) {
		t.Fatalf("回收单数 = %v", pickups)
	}

	code, payload, stderr = run(t, "report", "conversion")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if batches, _ := payload["batches"].([]any); len(batches) != len(seed.Batches()) {
		t.Fatalf("批次数 = %d", len(batches))
	}

	code, payload, stderr = run(t, "quota", "show")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["snapshot"] == nil {
		t.Fatalf("缺少 snapshot: %+v", payload)
	}
}
