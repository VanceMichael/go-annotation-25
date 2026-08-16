// Package quota 管理出口配额的预留与释放。
//
// 出口配额是强约束额度：任何时刻已预留总量都不得超过额度上限，
// 剩余额度不得为负；并发预留下成功预留的次数最多为
// 「额度上限 ÷ 单次预留量」。
package quota

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"wasteoil/internal/model"
)

// VoucherRounds 是预留凭证校验码的迭代轮数。
//
// 凭证需要防伪，因此采用多轮迭代摘要，单次计算耗时可观。
const VoucherRounds = 2000

// Ledger 是出口配额台账。
type Ledger struct {
	mu sync.RWMutex
	// capacityKG 是额度上限。
	capacityKG float64
	// reservedKG 是已预留总量。
	reservedKG float64
	// byShipment 记录每票货物的预留量。
	byShipment map[string]float64
	// vouchers 记录每票货物的预留凭证校验码。
	vouchers map[string]string
	// seq 是凭证序号。
	seq int
	// grants 记录成功预留的次数。
	grants int
	// rejects 记录被拒绝的次数。
	rejects int
	year    int
}

// New 构造某年度的配额台账。
func New(year int, capacityKG float64) (*Ledger, error) {
	if capacityKG <= 0 {
		return nil, fmt.Errorf("quota: 额度上限必须为正, 收到 %.3f", capacityKG)
	}
	return &Ledger{
		capacityKG: capacityKG,
		byShipment: make(map[string]float64),
		vouchers:   make(map[string]string),
		year:       year,
	}, nil
}

// voucherFor 生成预留凭证校验码。
func voucherFor(shipmentID string, massKG float64, seq int) string {
	seed := []byte(fmt.Sprintf("%s|%.3f|%d", shipmentID, massKG, seq))
	sum := sha256.Sum256(seed)
	for i := 0; i < VoucherRounds; i++ {
		sum = sha256.Sum256(sum[:])
	}
	return hex.EncodeToString(sum[:8])
}

// Voucher 返回某票货物的预留凭证校验码。
func (l *Ledger) Voucher(shipmentID string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.vouchers[shipmentID]
}

// Year 返回年度。
func (l *Ledger) Year() int {
	return l.year
}

// CapacityKG 返回额度上限。
func (l *Ledger) CapacityKG() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.capacityKG
}

// ReservedKG 返回已预留总量。
func (l *Ledger) ReservedKG() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.reservedKG
}

// RemainingKG 返回剩余额度。
func (l *Ledger) RemainingKG() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.capacityKG - l.reservedKG
}

// Grants 返回成功预留次数。
func (l *Ledger) Grants() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.grants
}

// Rejects 返回被拒绝次数。
func (l *Ledger) Rejects() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.rejects
}

// Reserve 为一票出口货物预留配额。
//
// 并发调用下累计预留量不得超过额度上限，剩余额度不得为负；
// 额度不足时返回 model.ErrQuotaInsufficient 并计入 rejects。
func (l *Ledger) Reserve(shipmentID string, massKG float64) error {
	if shipmentID == "" {
		return fmt.Errorf("quota: 缺少出口货物编号")
	}
	if massKG <= 0 {
		return fmt.Errorf("quota: 预留量必须为正, 收到 %.3f", massKG)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.capacityKG-l.reservedKG < massKG {
		l.rejects++
		return fmt.Errorf("%w: 需要 %.3fkg, 剩余 %.3fkg", model.ErrQuotaInsufficient,
			massKG, l.capacityKG-l.reservedKG)
	}
	voucher := voucherFor(shipmentID, massKG, l.seq+1)
	l.seq++
	l.reservedKG += massKG
	l.byShipment[shipmentID] += massKG
	l.vouchers[shipmentID] = voucher
	l.grants++
	return nil
}

// Release 释放一票出口货物已预留的配额。
func (l *Ledger) Release(shipmentID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	held, ok := l.byShipment[shipmentID]
	if !ok {
		return fmt.Errorf("quota: 出口货物 %s 没有已预留的配额", shipmentID)
	}
	l.reservedKG -= held
	delete(l.byShipment, shipmentID)
	return nil
}

// Held 返回某票货物已预留的配额。
func (l *Ledger) Held(shipmentID string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.byShipment[shipmentID]
}

// Shipments 返回已预留配额的货物编号，按字典序排序。
func (l *Ledger) Shipments() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.byShipment))
	for id := range l.byShipment {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Snapshot 是台账快照。
type Snapshot struct {
	Year        int     `json:"year"`
	CapacityKG  float64 `json:"capacity_kg"`
	ReservedKG  float64 `json:"reserved_kg"`
	RemainingKG float64 `json:"remaining_kg"`
	Shipments   int     `json:"shipments"`
	Grants      int     `json:"grants"`
	Rejects     int     `json:"rejects"`
	// Oversold 报告是否发生超卖（剩余额度为负）。
	Oversold bool `json:"oversold"`
}

// Snapshot 返回台账快照。
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	remaining := l.capacityKG - l.reservedKG
	return Snapshot{
		Year:        l.year,
		CapacityKG:  l.capacityKG,
		ReservedKG:  l.reservedKG,
		RemainingKG: remaining,
		Shipments:   len(l.byShipment),
		Grants:      l.grants,
		Rejects:     l.rejects,
		Oversold:    remaining < 0 || l.reservedKG > l.capacityKG,
	}
}
