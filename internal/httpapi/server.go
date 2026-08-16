// Package httpapi 提供废弃油脂回收与生物柴油出口溯源平台的 HTTP 接口。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wasteoil/internal/collect"
	"wasteoil/internal/convert"
	"wasteoil/internal/gateway"
	"wasteoil/internal/model"
	"wasteoil/internal/quota"
	"wasteoil/internal/report"
	"wasteoil/internal/seed"
)

// ErrorCode 是对外暴露的机器可读错误码。
type ErrorCode string

const (
	// CodePickupUnknown 回收单不存在。
	CodePickupUnknown ErrorCode = "pickup_unknown"
	// CodeCollectorUnknown 回收单位不存在。
	CodeCollectorUnknown ErrorCode = "collector_unknown"
	// CodeCollectorInactive 回收单位资质已停用。
	CodeCollectorInactive ErrorCode = "collector_inactive"
	// CodeBatchUnknown 转化批次不存在。
	CodeBatchUnknown ErrorCode = "batch_unknown"
	// CodeAssayMissing 缺少检测结果。
	CodeAssayMissing ErrorCode = "assay_missing"
	// CodeGradeRejected 品质不合格。
	CodeGradeRejected ErrorCode = "grade_rejected"
	// CodeQuotaInsufficient 出口配额不足。
	CodeQuotaInsufficient ErrorCode = "quota_insufficient"
	// CodeMassBalance 质量平衡不闭合。
	CodeMassBalance ErrorCode = "mass_balance"
	// CodeTraceBroken 溯源链断裂。
	CodeTraceBroken ErrorCode = "trace_broken"
	// CodeManifestWrite 出口清单写出失败。
	CodeManifestWrite ErrorCode = "manifest_write"
	// CodeCustomsUnavailable 海关申报通道不可用。
	CodeCustomsUnavailable ErrorCode = "customs_unavailable"
	// CodeBadRequest 请求参数非法。
	CodeBadRequest ErrorCode = "bad_request"
	// CodeUnavailable 请求被取消或超时。
	CodeUnavailable ErrorCode = "unavailable"
	// CodeInternal 未归类的内部错误。
	CodeInternal ErrorCode = "internal"
)

var errorMapping = []struct {
	sentinel error
	status   int
	code     ErrorCode
}{
	{model.ErrCollectorInactive, http.StatusConflict, CodeCollectorInactive},
	{model.ErrCollectorUnknown, http.StatusNotFound, CodeCollectorUnknown},
	{model.ErrPickupUnknown, http.StatusNotFound, CodePickupUnknown},
	{model.ErrBatchUnknown, http.StatusNotFound, CodeBatchUnknown},
	{model.ErrAssayMissing, http.StatusNotFound, CodeAssayMissing},
	{model.ErrGradeRejected, http.StatusConflict, CodeGradeRejected},
	{model.ErrQuotaInsufficient, http.StatusConflict, CodeQuotaInsufficient},
	{model.ErrMassBalance, http.StatusUnprocessableEntity, CodeMassBalance},
	{model.ErrTraceBroken, http.StatusUnprocessableEntity, CodeTraceBroken},
	{model.ErrManifestWrite, http.StatusUnprocessableEntity, CodeManifestWrite},
	{model.ErrCustomsUnavailable, http.StatusBadGateway, CodeCustomsUnavailable},
	{model.ErrInvalidPickup, http.StatusBadRequest, CodeBadRequest},
	{model.ErrInvalidAssay, http.StatusBadRequest, CodeBadRequest},
	{model.ErrInvalidBatch, http.StatusBadRequest, CodeBadRequest},
	{model.ErrUnknownGrade, http.StatusBadRequest, CodeBadRequest},
	{model.ErrUnknownSource, http.StatusBadRequest, CodeBadRequest},
	{context.Canceled, http.StatusServiceUnavailable, CodeUnavailable},
	{context.DeadlineExceeded, http.StatusServiceUnavailable, CodeUnavailable},
}

// Classify 依据错误链把领域错误映射为 HTTP 状态码与错误码。
func Classify(err error) (int, ErrorCode) {
	if err == nil {
		return http.StatusOK, ""
	}
	for _, m := range errorMapping {
		if errors.Is(err, m.sentinel) {
			return m.status, m.code
		}
	}
	return http.StatusInternalServerError, CodeInternal
}

// Server 是平台 HTTP 服务。
type Server struct {
	registry *collect.Registry
	ledger   *quota.Ledger
	gateway  *gateway.Client
	reports  *report.Builder
}

// Options 是构造 Server 所需的依赖。
type Options struct {
	Registry *collect.Registry
	Ledger   *quota.Ledger
	Gateway  *gateway.Client
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	return &Server{
		registry: opts.Registry,
		ledger:   opts.Ledger,
		gateway:  opts.Gateway,
		reports:  report.NewBuilder(opts.Registry, opts.Ledger),
	}
}

// Handler 返回注册好全部路由的处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/collectors", s.handleCollectors)
	mux.HandleFunc("GET /api/collectors/{id}", s.handleCollector)
	mux.HandleFunc("GET /api/pickups", s.handlePickups)
	mux.HandleFunc("GET /api/pickups/{id}", s.handlePickup)
	mux.HandleFunc("GET /api/pickups/{id}/assay", s.handleAssay)
	mux.HandleFunc("GET /api/batches", s.handleBatches)
	mux.HandleFunc("GET /api/batches/{id}/yield", s.handleYield)
	mux.HandleFunc("GET /api/report/intake", s.handleIntake)
	mux.HandleFunc("GET /api/report/conversion", s.handleConversion)
	mux.HandleFunc("GET /api/quota", s.handleQuota)
	mux.HandleFunc("POST /api/quota/reserve", s.handleReserve)
	mux.HandleFunc("POST /api/customs/submit", s.handleSubmit)
	mux.HandleFunc("GET /api/customs/probe", s.handleProbe)
	return mux
}

type errorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status, code := Classify(err)
	s.writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: err.Error()}})
}

func (s *Server) badRequest(w http.ResponseWriter, msg string) {
	s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: msg}})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	c := s.registry.Counts()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"service":    "wasteoil",
		"collectors": c.Collectors,
		"pickups":    c.Pickups,
		"assays":     c.Assays,
		"quota_year": s.ledger.Year(),
	})
}

func (s *Server) handleCollectors(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"collectors": s.registry.Collectors()})
}

func (s *Server) handleCollector(w http.ResponseWriter, r *http.Request) {
	c, err := s.registry.Collector(r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, c)
}

func (s *Server) handlePickups(w http.ResponseWriter, r *http.Request) {
	if raw := strings.TrimSpace(r.URL.Query().Get("source")); raw != "" {
		src, err := model.ParseSource(raw)
		if err != nil {
			s.writeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"pickups": s.registry.PickupsBySource(src)})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"pickups":       s.registry.Pickups(),
		"total_mass_kg": s.registry.TotalMassKG(),
		"convertible":   s.registry.ConvertiblePickups(),
	})
}

func (s *Server) handlePickup(w http.ResponseWriter, r *http.Request) {
	p, err := s.registry.Pickup(r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleAssay(w http.ResponseWriter, r *http.Request) {
	a, err := s.registry.Assay(r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"assay":       a,
		"grade_name":  a.Grade.DisplayName(),
		"convertible": a.Grade.Convertible(),
	})
}

func (s *Server) handleBatches(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"batches": seed.Batches()})
}

func (s *Server) handleYield(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, b := range seed.Batches() {
		if b.ID != id {
			continue
		}
		y, err := convert.Compute(b)
		if err != nil {
			s.writeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"yield":     y,
			"plausible": convert.Plausible(y),
			"describe":  y.Describe(),
		})
		return
	}
	s.writeError(w, errBatchUnknown(id))
}

type wrapped struct {
	msg string
	err error
}

func (w *wrapped) Error() string { return w.msg }
func (w *wrapped) Unwrap() error { return w.err }

func errBatchUnknown(id string) error {
	return &wrapped{msg: "httpapi: 转化批次 " + id + " 不存在", err: model.ErrBatchUnknown}
}

func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	rep, err := s.reports.Intake()
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleConversion(w http.ResponseWriter, r *http.Request) {
	rep, err := s.reports.Conversion(seed.Batches())
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.reports.Quota())
}

type reserveRequest struct {
	ShipmentID string  `json:"shipment_id"`
	MassKG     float64 `json:"mass_kg"`
}

func (s *Server) handleReserve(w http.ResponseWriter, r *http.Request) {
	var body reserveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "httpapi: 请求体不是合法 JSON")
		return
	}
	if err := s.ledger.Reserve(body.ShipmentID, body.MassKG); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.reports.Quota())
}

type submitRequest struct {
	ShipmentID string  `json:"shipment_id"`
	MassKG     float64 `json:"mass_kg"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var body submitRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "httpapi: 请求体不是合法 JSON")
		return
	}
	timeout := 5 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_ms")); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms <= 0 {
			s.badRequest(w, "httpapi: timeout_ms 需为正整数")
			return
		}
		timeout = time.Duration(ms) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	begin := time.Now()
	receipt, err := s.gateway.Submit(ctx, body.ShipmentID, body.MassKG)
	elapsed := time.Since(begin)
	if err != nil {
		status, code := Classify(err)
		s.writeJSON(w, status, map[string]any{
			"error":      errorBody{Code: code, Message: err.Error()},
			"elapsed_ms": elapsed.Milliseconds(),
			"timeout_ms": timeout.Milliseconds(),
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"receipt":    receipt,
		"elapsed_ms": elapsed.Milliseconds(),
		"timeout_ms": timeout.Milliseconds(),
	})
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	timeout := 2 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_ms")); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms <= 0 {
			s.badRequest(w, "httpapi: timeout_ms 需为正整数")
			return
		}
		timeout = time.Duration(ms) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	begin := time.Now()
	err := s.gateway.Probe(ctx)
	elapsed := time.Since(begin)
	if err != nil {
		status, code := Classify(err)
		s.writeJSON(w, status, map[string]any{
			"error":      errorBody{Code: code, Message: err.Error()},
			"elapsed_ms": elapsed.Milliseconds(),
			"timeout_ms": timeout.Milliseconds(),
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"channel":    s.gateway.Channel(),
		"elapsed_ms": elapsed.Milliseconds(),
	})
}
