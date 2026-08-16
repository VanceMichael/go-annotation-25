package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"wasteoil/internal/model"
)

// TestSubmitFast 断言通道响应足够快时申报成功。
func TestSubmitFast(t *testing.T) {
	c := New(Options{Latency: time.Millisecond})
	defer c.Close()
	r, err := c.Submit(context.Background(), "S-001", 18000)
	if err != nil {
		t.Fatalf("Submit 返回错误: %v", err)
	}
	if r.ShipmentID != "S-001" || r.SerialNo == "" {
		t.Fatalf("回执 = %+v", r)
	}
	if c.Calls() != 1 {
		t.Fatalf("申报次数 = %d, 期望 1", c.Calls())
	}
}

// TestSubmitHonoursCallerTimeout 断言调用方超时后立即返回，不等待外部通道。
func TestSubmitHonoursCallerTimeout(t *testing.T) {
	c := New(Options{Latency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	begin := time.Now()
	_, err := c.Submit(ctx, "S-001", 18000)
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatalf("超时后 Submit 应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false, 错误为 %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("耗时 = %v, 期望远小于 1s（外部通道耗时 5s）", elapsed)
	}
}

// TestSubmitHonoursCallerCancel 断言调用方取消后立即返回。
func TestSubmitHonoursCallerCancel(t *testing.T) {
	c := New(Options{Latency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	begin := time.Now()
	_, err := c.Submit(ctx, "S-001", 18000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false, 错误为 %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("取消后未及时返回")
	}
}

// TestSubmitAliveClientNotAffectedByOtherCallTimeout 断言一次调用超时不会让客户端整体失效。
func TestSubmitAliveClientNotAffectedByOtherCallTimeout(t *testing.T) {
	c := New(Options{Latency: 300 * time.Millisecond})
	defer c.Close()

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelShort()
	if _, err := c.Submit(shortCtx, "S-001", 100); err == nil {
		t.Fatalf("短超时申报应失败")
	}
	if !c.Alive() {
		t.Fatalf("单次调用超时不应使客户端失效")
	}

	longCtx, cancelLong := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelLong()
	if _, err := c.Submit(longCtx, "S-002", 200); err != nil {
		t.Fatalf("后续足够长的超时下申报应成功, 实际 %v", err)
	}
}

// TestQueryHonoursCallerTimeout 断言回执查询同样尊重调用方超时。
func TestQueryHonoursCallerTimeout(t *testing.T) {
	c := New(Options{Latency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	begin := time.Now()
	_, err := c.Query(ctx, "S-001")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false, 错误为 %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("耗时过长")
	}
}

// TestProbeHonoursCallerTimeout 断言探测尊重调用方超时并归类为通道不可用。
func TestProbeHonoursCallerTimeout(t *testing.T) {
	c := New(Options{Latency: 5 * time.Second})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	begin := time.Now()
	err := c.Probe(ctx)
	if err == nil {
		t.Fatalf("超时应返回错误")
	}
	if !errors.Is(err, model.ErrCustomsUnavailable) {
		t.Fatalf("errors.Is(err, model.ErrCustomsUnavailable) = false, 错误为 %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("耗时过长")
	}
}

func TestSubmitAlreadyCancelled(t *testing.T) {
	c := New(Options{Latency: time.Second})
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Submit(ctx, "S-001", 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消应返回 context.Canceled, 实际 %v", err)
	}
}

func TestSubmitValidation(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	if _, err := c.Submit(context.Background(), "", 100); err == nil {
		t.Errorf("缺少货物编号应返回错误")
	}
	if _, err := c.Submit(context.Background(), "S-001", 0); err == nil {
		t.Errorf("质量为 0 应返回错误")
	}
	if _, err := c.Query(context.Background(), ""); err == nil {
		t.Errorf("查询缺少编号应返回错误")
	}
}

func TestCloseMakesClientUnavailable(t *testing.T) {
	c := New(Options{Latency: time.Millisecond})
	if !c.Alive() {
		t.Fatalf("新建客户端应可用")
	}
	c.Close()
	c.Close() // 幂等
	if c.Alive() {
		t.Fatalf("关闭后客户端应不可用")
	}
	if _, err := c.Submit(context.Background(), "S-001", 100); !errors.Is(err, model.ErrCustomsUnavailable) {
		t.Fatalf("关闭后申报应返回 ErrCustomsUnavailable, 实际 %v", err)
	}
}

func TestChannelAndNowOverride(t *testing.T) {
	if got := New(Options{}).Channel(); got == "" {
		t.Fatalf("默认通道名为空")
	}
	fixed := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	c := New(Options{Channel: "某口岸", Now: func() time.Time { return fixed }})
	defer c.Close()
	if c.Channel() != "某口岸" {
		t.Fatalf("Channel = %s", c.Channel())
	}
	r, err := c.Submit(context.Background(), "S-001", 100)
	if err != nil {
		t.Fatalf("Submit 返回错误: %v", err)
	}
	if !r.AcceptedAt.Equal(fixed) {
		t.Fatalf("AcceptedAt = %s", r.AcceptedAt)
	}
}
