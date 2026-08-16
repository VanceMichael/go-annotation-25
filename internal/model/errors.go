package model

import "errors"

// 领域哨兵错误。上层通过 errors.Is 判定错误类别，禁止依赖错误文本。
var (
	// ErrInvalidPickup 回收登记非法。
	ErrInvalidPickup = errors.New("model: 回收登记非法")
	// ErrInvalidAssay 检测结果非法。
	ErrInvalidAssay = errors.New("model: 检测结果非法")
	// ErrInvalidBatch 转化批次非法。
	ErrInvalidBatch = errors.New("model: 转化批次非法")
	// ErrUnknownGrade 品质等级代码不存在。
	ErrUnknownGrade = errors.New("model: 未知品质等级")
	// ErrUnknownSource 来源类型代码不存在。
	ErrUnknownSource = errors.New("model: 未知来源类型")
	// ErrPickupUnknown 回收单不存在。
	ErrPickupUnknown = errors.New("model: 回收单不存在")
	// ErrCollectorUnknown 回收单位不存在。
	ErrCollectorUnknown = errors.New("model: 回收单位不存在")
	// ErrCollectorInactive 回收单位资质已停用。
	ErrCollectorInactive = errors.New("model: 回收单位资质已停用")
	// ErrBatchUnknown 转化批次不存在。
	ErrBatchUnknown = errors.New("model: 转化批次不存在")
	// ErrAssayMissing 缺少检测结果。
	ErrAssayMissing = errors.New("model: 缺少检测结果")
	// ErrGradeRejected 品质不合格，不得进入转化。
	ErrGradeRejected = errors.New("model: 品质不合格")
	// ErrQuotaInsufficient 出口配额不足。
	ErrQuotaInsufficient = errors.New("model: 出口配额不足")
	// ErrMassBalance 质量平衡不闭合。
	ErrMassBalance = errors.New("model: 质量平衡不闭合")
	// ErrTraceBroken 溯源链断裂。
	ErrTraceBroken = errors.New("model: 溯源链断裂")
	// ErrManifestWrite 出口清单写出失败。
	ErrManifestWrite = errors.New("model: 出口清单写出失败")
	// ErrCustomsUnavailable 海关申报通道不可用。
	ErrCustomsUnavailable = errors.New("model: 海关申报通道不可用")
)
