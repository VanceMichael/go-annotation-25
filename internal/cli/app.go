// Package cli 实现 oilctl 命令行界面。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"wasteoil/internal/collect"
	"wasteoil/internal/convert"
	"wasteoil/internal/gateway"
	"wasteoil/internal/httpapi"
	"wasteoil/internal/manifest"
	"wasteoil/internal/model"
	"wasteoil/internal/quota"
	"wasteoil/internal/report"
	"wasteoil/internal/seed"
	"wasteoil/internal/trace"
)

// 退出码约定。上层脚本依赖这些取值区分失败类别。
const (
	// ExitOK 正常结束。
	ExitOK = 0
	// ExitUsage 命令行用法错误或未归类的内部错误。
	ExitUsage = 1
	// ExitBadRequest 参数非法。
	ExitBadRequest = 2
	// ExitConflict 业务冲突：配额不足、品质不合格、资质停用等。
	ExitConflict = 3
	// ExitAborted 外部通道被取消或超时。
	ExitAborted = 4
	// ExitNotFound 资源不存在。
	ExitNotFound = 5
	// ExitData 数据一致性问题：质量平衡不闭合、溯源链断裂、清单写出失败。
	ExitData = 6
)

// Version 是当前构建版本。
const Version = "0.4.0"

const usage = `oilctl —— 废弃油脂回收与生物柴油出口溯源平台命令行

用法:
  oilctl <命令> [子命令] [参数]

命令:
  collector list     列出回收单位
  pickup list        列出回收登记
  assay list         列出检测结果
  convert compute    计算批次转化计量
  quota reserve      并发预留出口配额
  quota show         输出配额台账快照
  trace build        构建并校验出口溯源链
  manifest write     组装并写出出口清单
  customs submit     向海关申报通道提交出口申报
  customs probe      探测海关申报通道可用性
  report intake      生成回收入库报表
  report conversion  生成转化计量报表
  serve              启动 HTTP 服务
  selfcheck          运行内置自检
  version            输出版本信息

退出码:
  0 成功  1 用法或内部错误  2 参数非法  3 业务冲突
  4 外部通道中止  5 资源不存在  6 数据一致性问题
`

type app struct {
	registry *collect.Registry
	ledger   *quota.Ledger
	reports  *report.Builder
	gateway  *gateway.Client
	stdout   io.Writer
	stderr   io.Writer
}

func newApp(stdout, stderr io.Writer, latency time.Duration) (*app, error) {
	reg, ledger, err := seed.Load()
	if err != nil {
		return nil, err
	}
	gw := gateway.New(gateway.Options{Latency: latency})
	return &app{
		registry: reg,
		ledger:   ledger,
		reports:  report.NewBuilder(reg, ledger),
		gateway:  gw,
		stdout:   stdout,
		stderr:   stderr,
	}, nil
}

// Run 执行一次命令行调用并返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	a, err := newApp(stdout, stderr, parseLatency(args))
	if err != nil {
		fmt.Fprintf(stderr, "初始化失败: %v\n", err)
		return ExitUsage
	}
	defer a.gateway.Close()

	code, err := a.route(args)
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
	}
	return code
}

// parseLatency 从参数中提取 --customs-latency，供网关客户端初始化。
func parseLatency(args []string) time.Duration {
	for i := 0; i < len(args); i++ {
		if args[i] == "--customs-latency" && i+1 < len(args) {
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				return d
			}
		}
	}
	return 0
}

func (a *app) route(args []string) (int, error) {
	switch args[0] {
	case "version":
		fmt.Fprintf(a.stdout, "oilctl %s\n", Version)
		return ExitOK, nil
	case "collector":
		return a.runCollector(args[1:])
	case "pickup":
		return a.runPickup(args[1:])
	case "assay":
		return a.runAssay(args[1:])
	case "convert":
		return a.runConvert(args[1:])
	case "quota":
		return a.runQuota(args[1:])
	case "trace":
		return a.runTrace(args[1:])
	case "manifest":
		return a.runManifest(args[1:])
	case "customs":
		return a.runCustoms(args[1:])
	case "report":
		return a.runReport(args[1:])
	case "serve":
		return a.runServe(args[1:])
	case "selfcheck":
		return a.runSelfcheck(args[1:])
	default:
		fmt.Fprint(a.stderr, usage)
		return ExitUsage, fmt.Errorf("未知命令 %q", args[0])
	}
}

func (a *app) emit(payload any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// classify 把领域错误映射为退出码，映射依据是错误链中的哨兵错误。
func classify(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, model.ErrCustomsUnavailable):
		return ExitAborted
	case errors.Is(err, model.ErrMassBalance), errors.Is(err, model.ErrTraceBroken),
		errors.Is(err, model.ErrManifestWrite):
		return ExitData
	case errors.Is(err, model.ErrQuotaInsufficient), errors.Is(err, model.ErrGradeRejected),
		errors.Is(err, model.ErrCollectorInactive):
		return ExitConflict
	case errors.Is(err, model.ErrPickupUnknown), errors.Is(err, model.ErrCollectorUnknown),
		errors.Is(err, model.ErrBatchUnknown), errors.Is(err, model.ErrAssayMissing):
		return ExitNotFound
	case errors.Is(err, model.ErrInvalidPickup), errors.Is(err, model.ErrInvalidAssay),
		errors.Is(err, model.ErrInvalidBatch), errors.Is(err, model.ErrUnknownGrade),
		errors.Is(err, model.ErrUnknownSource):
		return ExitBadRequest
	default:
		return ExitUsage
	}
}

func (a *app) runCollector(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("collector 需要子命令: list")
	}
	return ExitOK, a.emit(map[string]any{
		"collectors": a.registry.Collectors(),
		"counts":     a.registry.Counts(),
	})
}

func (a *app) runPickup(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("pickup 需要子命令: list")
	}
	fs := flag.NewFlagSet("pickup list", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	sourceFlag := fs.String("source", "", "按来源类型筛选")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *sourceFlag != "" {
		s, err := model.ParseSource(*sourceFlag)
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(map[string]any{"pickups": a.registry.PickupsBySource(s)})
	}
	return ExitOK, a.emit(map[string]any{
		"pickups":       a.registry.Pickups(),
		"total_mass_kg": a.registry.TotalMassKG(),
	})
}

func (a *app) runAssay(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("assay 需要子命令: list")
	}
	rows := make([]model.Assay, 0, len(seed.Assays()))
	for _, p := range a.registry.Pickups() {
		x, err := a.registry.Assay(p.ID)
		if err != nil {
			continue
		}
		rows = append(rows, x)
	}
	return ExitOK, a.emit(map[string]any{
		"assays":      rows,
		"convertible": a.registry.ConvertiblePickups(),
	})
}

func (a *app) runConvert(args []string) (int, error) {
	if len(args) == 0 || args[0] != "compute" {
		return ExitUsage, errors.New("convert 需要子命令: compute")
	}
	fs := flag.NewFlagSet("convert compute", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	batchFlag := fs.String("batch", "", "批次号，默认全部")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	batches := seed.Batches()
	if *batchFlag != "" {
		filtered := make([]model.Batch, 0, 1)
		for _, b := range batches {
			if b.ID == *batchFlag {
				filtered = append(filtered, b)
			}
		}
		if len(filtered) == 0 {
			err := fmt.Errorf("%w: %s", model.ErrBatchUnknown, *batchFlag)
			return classify(err), err
		}
		batches = filtered
	}

	rep, err := a.reports.Conversion(batches)
	if err != nil {
		return classify(err), err
	}
	if err := a.emit(rep); err != nil {
		return ExitUsage, err
	}
	if rep.Unbalanced > 0 {
		err := fmt.Errorf("%w: %d 个批次质量平衡不闭合", model.ErrMassBalance, rep.Unbalanced)
		return classify(err), err
	}
	return ExitOK, nil
}

func (a *app) runQuota(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("quota 需要子命令: reserve 或 show")
	}
	fs := flag.NewFlagSet("quota "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	workersFlag := fs.Int("workers", 64, "并发预留通道数")
	perWorkerFlag := fs.Int("per-worker", 5, "每个通道的预留次数")
	massFlag := fs.Float64("mass", 10, "单次预留量，单位千克")
	capacityFlag := fs.Float64("capacity", 1000, "额度上限，单位千克")
	contestFlag := fs.Bool("contest", false, "先预留到只剩一份额度，再让全部通道同时争抢这一份")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	switch args[0] {
	case "show":
		return ExitOK, a.emit(a.reports.Quota())

	case "reserve":
		if *workersFlag <= 0 || *perWorkerFlag <= 0 || *massFlag <= 0 || *capacityFlag <= 0 {
			return ExitBadRequest, errors.New("--workers/--per-worker/--mass/--capacity 必须为正")
		}
		ledger, err := quota.New(seed.Year, *capacityFlag)
		if err != nil {
			return ExitBadRequest, err
		}
		maxGrants := int(*capacityFlag / *massFlag)

		// contest 模式：先把额度预留到只剩一份，再让全部通道同时争抢这一份。
		preloaded := 0
		if *contestFlag {
			if maxGrants < 1 {
				return ExitBadRequest, errors.New("--capacity 至少要能容纳一次预留")
			}
			for i := 0; i < maxGrants-1; i++ {
				if rerr := ledger.Reserve(fmt.Sprintf("PRE-%04d", i), *massFlag); rerr != nil {
					return classify(rerr), rerr
				}
				preloaded++
			}
		}

		var wg sync.WaitGroup
		wg.Add(*workersFlag)
		start := make(chan struct{})
		var won int64
		var mu sync.Mutex
		for w := 0; w < *workersFlag; w++ {
			go func(w int) {
				defer wg.Done()
				<-start
				rounds := *perWorkerFlag
				if *contestFlag {
					rounds = 1
				}
				for i := 0; i < rounds; i++ {
					if rerr := ledger.Reserve(fmt.Sprintf("S-%03d-%03d", w, i), *massFlag); rerr == nil {
						mu.Lock()
						won++
						mu.Unlock()
					}
				}
			}(w)
		}
		close(start)
		wg.Wait()

		snap := ledger.Snapshot()
		payload := map[string]any{
			"capacity_kg":  snap.CapacityKG,
			"reserved_kg":  snap.ReservedKG,
			"remaining_kg": snap.RemainingKG,
			"grants":       snap.Grants,
			"rejects":      snap.Rejects,
			"max_grants":   maxGrants,
			"oversold":     snap.Oversold,
			"contest":      *contestFlag,
			"preloaded":    preloaded,
			"contest_won":  won,
		}
		if err := a.emit(payload); err != nil {
			return ExitUsage, err
		}
		if snap.Oversold || snap.Grants > maxGrants {
			return ExitUsage, fmt.Errorf("出口配额超卖: 成功预留 %d 次（上限 %d）, 剩余 %.3fkg",
				snap.Grants, maxGrants, snap.RemainingKG)
		}
		if *contestFlag && won != 1 {
			return ExitUsage, fmt.Errorf("最后一份额度被 %d 个请求同时抢到, 期望恰好 1 个", won)
		}
		return ExitOK, nil

	default:
		return ExitUsage, fmt.Errorf("未知子命令 quota %q", args[0])
	}
}

func (a *app) runTrace(args []string) (int, error) {
	if len(args) == 0 || args[0] != "build" {
		return ExitUsage, errors.New("trace 需要子命令: build")
	}
	chains, err := buildChains()
	if err != nil {
		return classify(err), err
	}
	if verr := trace.ValidateAll(chains); verr != nil {
		payload := map[string]any{
			"chains":   len(chains),
			"ok":       false,
			"message":  verr.Error(),
			"describe": describeChains(chains),
		}
		if eerr := a.emit(payload); eerr != nil {
			return ExitUsage, eerr
		}
		return classify(verr), verr
	}
	return ExitOK, a.emit(map[string]any{
		"chains":   len(chains),
		"ok":       true,
		"tails":    chainTails(chains),
		"describe": describeChains(chains),
	})
}

func (a *app) runManifest(args []string) (int, error) {
	if len(args) == 0 || args[0] != "write" {
		return ExitUsage, errors.New("manifest 需要子命令: write")
	}
	fs := flag.NewFlagSet("manifest write", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	outFlag := fs.String("out", "", "清单输出路径，默认写入临时目录")
	injectFlag := fs.Bool("inject-nonfinite", false, "把某行得率置为非有限值，用于演练写出失败上报")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	chains, err := buildChains()
	if err != nil {
		return classify(err), err
	}
	yields := make(map[string]convert.Yield, len(seed.Batches()))
	for _, b := range seed.Batches() {
		y, cerr := convert.Compute(b)
		if cerr != nil {
			return classify(cerr), cerr
		}
		yields[b.ID] = y
	}
	batchesOf := make(map[string][]string)
	destinations := make(map[string]string)
	for _, s := range seed.Shipments() {
		batchesOf[s.ShipmentID] = s.BatchIDs
		destinations[s.ShipmentID] = s.Destination
	}

	m, berr := manifest.Build(seed.Year, time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC),
		chains, yields, batchesOf, destinations)
	if berr != nil {
		return classify(berr), berr
	}
	if *injectFlag && len(m.Lines) > 0 {
		m.Lines[len(m.Lines)-1].Ratio = math.NaN()
	}

	path := *outFlag
	cleanup := func() {}
	if path == "" {
		dir, terr := os.MkdirTemp("", "oilctl-manifest-")
		if terr != nil {
			return ExitUsage, terr
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
		path = filepath.Join(dir, "manifest.json")
	}
	defer cleanup()

	writeErr := manifest.Write(path, m)
	readable := false
	if _, rerr := manifest.Read(path); rerr == nil {
		readable = true
	}

	payload := map[string]any{
		"lines":         len(m.Lines),
		"total_mass_kg": m.TotalMassKG,
		"path":          path,
		"ok":            writeErr == nil,
		"readable":      readable,
	}
	if writeErr != nil {
		payload["message"] = writeErr.Error()
		payload["exit_code"] = classify(writeErr)
	}
	if err := a.emit(payload); err != nil {
		return ExitUsage, err
	}
	if writeErr != nil {
		return classify(writeErr), writeErr
	}
	return ExitOK, nil
}

func (a *app) runCustoms(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("customs 需要子命令: submit 或 probe")
	}
	fs := flag.NewFlagSet("customs "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	shipmentFlag := fs.String("shipment", "S-001", "出口货物编号")
	massFlag := fs.Float64("mass", 9525.4, "出口质量，单位千克")
	timeoutFlag := fs.Duration("timeout", 5*time.Second, "单次调用超时")
	_ = fs.Duration("customs-latency", 0, "模拟海关通道响应耗时")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	begin := time.Now()
	var opErr error
	var receipt gateway.Receipt
	switch args[0] {
	case "submit":
		receipt, opErr = a.gateway.Submit(ctx, *shipmentFlag, *massFlag)
	case "probe":
		opErr = a.gateway.Probe(ctx)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 customs %q", args[0])
	}
	elapsed := time.Since(begin)

	payload := map[string]any{
		"channel":    a.gateway.Channel(),
		"timeout_ms": timeoutFlag.Milliseconds(),
		"elapsed_ms": elapsed.Milliseconds(),
		"alive":      a.gateway.Alive(),
		"ok":         opErr == nil,
	}
	if args[0] == "submit" {
		payload["shipment_id"] = *shipmentFlag
		if opErr == nil {
			payload["serial_no"] = receipt.SerialNo
		}
	}
	if opErr != nil {
		payload["message"] = opErr.Error()
		payload["exit_code"] = classify(opErr)
	}
	if err := a.emit(payload); err != nil {
		return ExitUsage, err
	}
	if opErr != nil {
		return classify(opErr), opErr
	}
	return ExitOK, nil
}

func (a *app) runReport(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("report 需要子命令: intake 或 conversion")
	}
	switch args[0] {
	case "intake":
		rep, err := a.reports.Intake()
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(rep)
	case "conversion":
		rep, err := a.reports.Conversion(seed.Batches())
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(rep)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 report %q", args[0])
	}
}

func (a *app) runServe(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	addrFlag := fs.String("addr", "127.0.0.1:8080", "监听地址")
	_ = fs.Duration("customs-latency", 0, "模拟海关通道响应耗时")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	srv := httpapi.New(httpapi.Options{
		Registry: a.registry,
		Ledger:   a.ledger,
		Gateway:  a.gateway,
	})
	fmt.Fprintf(a.stdout, "oilctl serve 监听 %s\n", *addrFlag)
	server := &http.Server{
		Addr:              *addrFlag,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return ExitUsage, err
	}
	return ExitOK, nil
}

func (a *app) runSelfcheck(args []string) (int, error) {
	fs := flag.NewFlagSet("selfcheck", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	_ = fs.Duration("customs-latency", 0, "模拟海关通道响应耗时")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}

	checks := make([]map[string]any, 0, 8)
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"check": name, "ok": ok, "detail": detail})
	}

	c := a.registry.Counts()
	add("registry", c.Collectors == len(seed.Collectors()) && c.Pickups == len(seed.Pickups()),
		fmt.Sprintf("回收单位 %d 个, 回收单 %d 张, 检测 %d 条", c.Collectors, c.Pickups, c.Assays))

	// 转化得率必须按质量口径计算并落在行业正常区间。
	conv, err := a.reports.Conversion(seed.Batches())
	if err != nil {
		return classify(err), err
	}
	add("conversion-ratio-plausible", conv.Implausible == 0,
		fmt.Sprintf("异常得率批次 %d 个, 加权平均得率 %.4f", conv.Implausible, conv.Summary.MeanRatio))
	add("conversion-mass-balance", conv.Unbalanced == 0,
		fmt.Sprintf("质量平衡不闭合批次 %d 个", conv.Unbalanced))

	// 出口配额并发预留不得超卖。
	probeLedger, qerr := quota.New(seed.Year, 1000)
	if qerr != nil {
		return ExitUsage, qerr
	}
	var wg sync.WaitGroup
	const workers = 64
	wg.Add(workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < 5; i++ {
				_ = probeLedger.Reserve(fmt.Sprintf("S-%03d-%03d", w, i), 10)
			}
		}(w)
	}
	close(start)
	wg.Wait()
	snap := probeLedger.Snapshot()
	add("quota-no-oversell", !snap.Oversold && snap.Grants <= 100,
		fmt.Sprintf("成功预留 %d 次（上限 100）, 剩余 %.3fkg, 超卖 = %v",
			snap.Grants, snap.RemainingKG, snap.Oversold))

	// 溯源链分叉后各链必须独立。
	chains, cerr := buildChains()
	if cerr != nil {
		return classify(cerr), cerr
	}
	traceErr := trace.ValidateAll(chains)
	add("trace-chains-independent", traceErr == nil && len(chains) == len(seed.Shipments()),
		fmt.Sprintf("构建 %d 条链, 校验结果 = %v", len(chains), traceErr))

	// 清单写出失败必须如实上报。
	dir, terr := os.MkdirTemp("", "oilctl-selfcheck-")
	if terr != nil {
		return ExitUsage, terr
	}
	defer os.RemoveAll(dir)
	bad := manifest.Manifest{
		Year: seed.Year, IssuedAt: time.Now(),
		Lines:       []manifest.Line{{ShipmentID: "S-PROBE", MassKG: 1, Ratio: math.NaN()}},
		TotalMassKG: 1, MeanRatio: 0.95,
	}
	badPath := filepath.Join(dir, "bad.json")
	badErr := manifest.Write(badPath, bad)
	_, readBack := manifest.Read(badPath)
	add("manifest-reports-write-failure", badErr != nil && readBack != nil,
		fmt.Sprintf("非法清单写出返回错误 = %v, 回读失败 = %v", badErr != nil, readBack != nil))

	// 海关通道必须尊重调用方超时。
	slow := gateway.New(gateway.Options{Latency: 5 * time.Second})
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	begin := time.Now()
	_, gerr := slow.Submit(ctx, "S-PROBE", 100)
	add("customs-honours-timeout", gerr != nil && time.Since(begin) < time.Second,
		fmt.Sprintf("40ms 超时下申报返回错误 = %v, 耗时 %v",
			gerr != nil, time.Since(begin).Round(time.Millisecond)))

	sort.Slice(checks, func(i, j int) bool {
		return checks[i]["check"].(string) < checks[j]["check"].(string)
	})
	failed := 0
	for _, ck := range checks {
		if !ck["ok"].(bool) {
			failed++
		}
	}
	if err := a.emit(map[string]any{"checks": checks, "failed": failed}); err != nil {
		return ExitUsage, err
	}
	if failed > 0 {
		return ExitUsage, fmt.Errorf("自检失败 %d 项", failed)
	}
	return ExitOK, nil
}
