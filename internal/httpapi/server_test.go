package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wasteoil/internal/gateway"
	"wasteoil/internal/model"
	"wasteoil/internal/seed"
)

func newTestServer(t *testing.T, latency time.Duration) (http.Handler, *gateway.Client) {
	t.Helper()
	reg, ledger, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	gw := gateway.New(gateway.Options{Latency: latency})
	srv := New(Options{Registry: reg, Ledger: ledger, Gateway: gw})
	return srv.Handler(), gw
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s 响应不是合法 JSON: %v\n%s", method, path, err, rec.Body.String())
		}
	}
	return rec, payload
}

func errorCode(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, ok := payload["error"]
	if !ok {
		t.Fatalf("响应缺少 error 字段: %+v", payload)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("error 字段格式异常: %+v", raw)
	}
	code, _ := obj["code"].(string)
	return code
}

func TestHealth(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if payload["status"] != "ok" {
		t.Fatalf("响应 = %+v", payload)
	}
}

// TestSubmitHonoursTimeout 断言申报接口在超时后立即返回 503。
func TestSubmitHonoursTimeout(t *testing.T) {
	h, gw := newTestServer(t, 5*time.Second)
	defer gw.Close()

	begin := time.Now()
	rec, payload := doJSON(t, h, http.MethodPost, "/api/customs/submit?timeout_ms=100",
		`{"shipment_id":"S-001","mass_kg":9525.4}`)
	elapsed := time.Since(begin)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, 期望 503, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeUnavailable) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeUnavailable)
	}
	if elapsed > time.Second {
		t.Fatalf("耗时 = %v, 期望远小于 1s（通道耗时 5s）", elapsed)
	}
}

// TestProbeHonoursTimeout 断言探测接口在超时后立即返回。
func TestProbeHonoursTimeout(t *testing.T) {
	h, gw := newTestServer(t, 5*time.Second)
	defer gw.Close()

	begin := time.Now()
	rec, payload := doJSON(t, h, http.MethodGet, "/api/customs/probe?timeout_ms=80", "")
	elapsed := time.Since(begin)

	if rec.Code == http.StatusOK {
		t.Fatalf("超时应返回失败状态码, 响应 %+v", payload)
	}
	if elapsed > time.Second {
		t.Fatalf("耗时 = %v, 期望远小于 1s", elapsed)
	}
}

func TestSubmitFast(t *testing.T) {
	h, gw := newTestServer(t, time.Millisecond)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodPost, "/api/customs/submit",
		`{"shipment_id":"S-001","mass_kg":9525.4}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	receipt, ok := payload["receipt"].(map[string]any)
	if !ok || receipt["serial_no"] == "" {
		t.Fatalf("回执 = %+v", payload["receipt"])
	}
}

// TestReserveQuotaRejectsOverCapacity 断言超出额度的预留返回 409。
func TestReserveQuotaRejectsOverCapacity(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodPost, "/api/quota/reserve",
		fmt.Sprintf(`{"shipment_id":"S-001","mass_kg":%f}`, seed.QuotaCapacityKG+1))
	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d, 期望 409, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeQuotaInsufficient) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestReserveQuotaSucceeds(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodPost, "/api/quota/reserve",
		`{"shipment_id":"S-001","mass_kg":9525.4}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	snap, ok := payload["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 snapshot: %+v", payload)
	}
	if oversold, _ := snap["oversold"].(bool); oversold {
		t.Fatalf("不应超卖: %+v", snap)
	}
	if reserved, _ := snap["reserved_kg"].(float64); reserved != 9525.4 {
		t.Fatalf("已预留 = %v, 期望 9525.4", reserved)
	}
}

// TestYieldUsesMassRatio 断言批次得率接口按质量口径给出结果。
func TestYieldUsesMassRatio(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodGet, "/api/batches/B-001/yield", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	if plausible, _ := payload["plausible"].(bool); !plausible {
		t.Fatalf("得率应落在行业正常区间: %+v", payload["yield"])
	}
	y, ok := payload["yield"].(map[string]any)
	if !ok {
		t.Fatalf("yield = %+v", payload["yield"])
	}
	volume, _ := y["product_volume_l"].(float64)
	mass, _ := y["product_mass_kg"].(float64)
	if mass >= volume {
		t.Fatalf("产出质量 %.3f 不应大于等于体积 %.3f（密度小于 1）", mass, volume)
	}
	if balanced, _ := y["balanced"].(bool); !balanced {
		t.Fatalf("质量平衡应闭合: %+v", y)
	}
}

func TestYieldUnknownBatch(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodGet, "/api/batches/NOPE/yield", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeBatchUnknown) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestPickupAndAssayEndpoints(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()

	rec, payload := doJSON(t, h, http.MethodGet, "/api/pickups", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if items, _ := payload["pickups"].([]any); len(items) != len(seed.Pickups()) {
		t.Fatalf("回收单数 = %d", len(items))
	}
	if items, _ := payload["convertible"].([]any); len(items) != 6 {
		t.Fatalf("可转化回收单数 = %d, 期望 6", len(items))
	}

	rec, payload = doJSON(t, h, http.MethodGet, "/api/pickups/P-004/assay", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if convertible, _ := payload["convertible"].(bool); convertible {
		t.Fatalf("P-004 不合格, convertible 应为 false")
	}

	rec, payload = doJSON(t, h, http.MethodGet, "/api/pickups/NOPE", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodePickupUnknown) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestCollectorEndpoints(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodGet, "/api/collectors", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if items, _ := payload["collectors"].([]any); len(items) != len(seed.Collectors()) {
		t.Fatalf("回收单位数 = %d", len(items))
	}
	rec, payload = doJSON(t, h, http.MethodGet, "/api/collectors/NOPE", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeCollectorUnknown) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestReportEndpoints(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	rec, payload := doJSON(t, h, http.MethodGet, "/api/report/intake", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if got, _ := payload["total_mass_kg"].(float64); got != seed.TotalPickupMassKG() {
		t.Fatalf("质量合计 = %v, 期望 %v", got, seed.TotalPickupMassKG())
	}

	rec, payload = doJSON(t, h, http.MethodGet, "/api/report/conversion", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if got, _ := payload["implausible"].(float64); got != 0 {
		t.Fatalf("异常得率批次数 = %v, 期望 0", got)
	}

	rec, _ = doJSON(t, h, http.MethodGet, "/api/quota", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	rec, _ = doJSON(t, h, http.MethodGet, "/api/batches", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
}

func TestBadRequests(t *testing.T) {
	h, gw := newTestServer(t, 0)
	defer gw.Close()
	if rec, _ := doJSON(t, h, http.MethodGet, "/api/pickups?source=nope", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法来源状态码 = %d, 期望 400", rec.Code)
	}
	if rec, _ := doJSON(t, h, http.MethodPost, "/api/customs/submit?timeout_ms=0", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("timeout_ms=0 状态码 = %d, 期望 400", rec.Code)
	}
	if rec, _ := doJSON(t, h, http.MethodPost, "/api/quota/reserve", `not-json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 状态码 = %d, 期望 400", rec.Code)
	}
}

func TestClassifyMapsSentinels(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{fmt.Errorf("上层: %w", model.ErrPickupUnknown), http.StatusNotFound, CodePickupUnknown},
		{fmt.Errorf("上层: %w", model.ErrCollectorInactive), http.StatusConflict, CodeCollectorInactive},
		{fmt.Errorf("上层: %w", model.ErrQuotaInsufficient), http.StatusConflict, CodeQuotaInsufficient},
		{fmt.Errorf("上层: %w", model.ErrMassBalance), http.StatusUnprocessableEntity, CodeMassBalance},
		{fmt.Errorf("上层: %w", model.ErrTraceBroken), http.StatusUnprocessableEntity, CodeTraceBroken},
		{fmt.Errorf("上层: %w", model.ErrManifestWrite), http.StatusUnprocessableEntity, CodeManifestWrite},
		{fmt.Errorf("上层: %w", model.ErrCustomsUnavailable), http.StatusBadGateway, CodeCustomsUnavailable},
		{fmt.Errorf("上层: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, CodeUnavailable},
		{errors.New("未归类"), http.StatusInternalServerError, CodeInternal},
	}
	for _, tc := range cases {
		status, code := Classify(tc.err)
		if status != tc.wantStatus || code != tc.wantCode {
			t.Errorf("Classify(%v) = %d/%s, 期望 %d/%s", tc.err, status, code, tc.wantStatus, tc.wantCode)
		}
	}
}
