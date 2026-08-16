// Package gateway 封装对海关单一窗口申报通道的调用。
//
// 申报是外部同步调用，响应时间不可控。调用方通过 context 控制单次申报的
// 生命周期：设定的超时到达或调用被取消时，必须立即返回对应错误。
// Client 另外保存一个基础 context，用于后台保活与整体关闭。
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wasteoil/internal/model"
)

// Receipt 是海关申报回执。
type Receipt struct {
	ShipmentID string    `json:"shipment_id"`
	Channel    string    `json:"channel"`
	SerialNo   string    `json:"serial_no"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// Client 是海关申报通道客户端。
type Client struct {
	channel string
	latency time.Duration
	now     func() time.Time

	// baseCtx 是客户端自身的生命周期 context，仅用于后台保活与 Close。
	baseCtx context.Context
	cancel  context.CancelFunc

	mu     sync.Mutex
	closed bool
	// calls 记录已发起的申报次数。
	calls int
}

// Options 是构造客户端的参数。
type Options struct {
	Channel string
	// Latency 模拟海关通道的响应耗时。
	Latency time.Duration
	Now     func() time.Time
}

// New 构造海关申报通道客户端。
func New(opts Options) *Client {
	channel := opts.Channel
	if channel == "" {
		channel = "海关单一窗口"
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	return &Client{channel: channel, latency: opts.Latency, now: now, baseCtx: baseCtx, cancel: cancel}
}

// Channel 返回申报通道名称。
func (c *Client) Channel() string {
	return c.channel
}

// Calls 返回已发起的申报次数。
func (c *Client) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// Close 关闭客户端，终止后台保活。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.cancel()
}

// Alive 报告客户端是否仍可用。
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// call 执行一次外部请求：等待响应耗时，或在 ctx 结束时立即返回。
func (c *Client) call(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.Alive() {
		return fmt.Errorf("%w: 客户端已关闭", model.ErrCustomsUnavailable)
	}
	if c.latency <= 0 {
		return nil
	}
	timer := time.NewTimer(c.latency)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Submit 提交一票出口申报。
//
// ctx 控制本次申报的生命周期：ctx 超时或被取消时必须立即返回对应错误。
func (c *Client) Submit(ctx context.Context, shipmentID string, massKG float64) (Receipt, error) {
	if shipmentID == "" {
		return Receipt{}, fmt.Errorf("gateway: 缺少出口货物编号")
	}
	if massKG <= 0 {
		return Receipt{}, fmt.Errorf("gateway: 出口质量必须为正, 收到 %.3f", massKG)
	}

	c.mu.Lock()
	c.calls++
	seq := c.calls
	c.mu.Unlock()

	if err := c.call(ctx); err != nil {
		return Receipt{}, fmt.Errorf("gateway: 出口货物 %s 申报未完成: %w", shipmentID, err)
	}
	return Receipt{
		ShipmentID: shipmentID,
		Channel:    c.channel,
		SerialNo:   fmt.Sprintf("CN-%06d", seq),
		AcceptedAt: c.now(),
	}, nil
}

// Query 查询一票申报的回执。
func (c *Client) Query(ctx context.Context, shipmentID string) (Receipt, error) {
	if shipmentID == "" {
		return Receipt{}, fmt.Errorf("gateway: 缺少出口货物编号")
	}
	if err := c.call(ctx); err != nil {
		return Receipt{}, fmt.Errorf("gateway: 出口货物 %s 回执查询未完成: %w", shipmentID, err)
	}
	return Receipt{ShipmentID: shipmentID, Channel: c.channel, AcceptedAt: c.now()}, nil
}

// Probe 探测申报通道可用性。
func (c *Client) Probe(ctx context.Context) error {
	if err := c.call(ctx); err != nil {
		return fmt.Errorf("%w: %s", model.ErrCustomsUnavailable, err.Error())
	}
	return nil
}
