package dosing

import (
	"math"
	"strconv"
	"strings"
)

// RiskLevel 描述剂量变更的风险等级。
type RiskLevel string

const (
	RiskNormal RiskLevel = "normal" // 普通范围：保持便捷操作，直接下发
	RiskHigh   RiskLevel = "high"   // 高风险：必须申请、审核后才能下发
)

// ChemicalLimit 描述单一药剂允许的便捷调整范围，以及相对当前值的最大跳变比例。
type ChemicalLimit struct {
	Chemical     string
	MinRate      float64
	MaxRate      float64
	MaxJumpRatio float64 // 0 表示不限制相对跳变
}

// RiskPolicy 按药剂配置普通变更范围。范围内且跳变温和的变更直接下发，
// 其余一律走高风险审批流程。
type RiskPolicy struct {
	limits       map[string]ChemicalLimit
	defaultLimit ChemicalLimit
}

// DefaultRiskPolicy 覆盖常见水处理药剂的安全便捷范围。
func DefaultRiskPolicy() RiskPolicy {
	return NewRiskPolicy(map[string]ChemicalLimit{
		"pac":       {Chemical: "pac", MinRate: 5, MaxRate: 120, MaxJumpRatio: 0.35},
		"pam":       {Chemical: "pam", MinRate: 1, MaxRate: 20, MaxJumpRatio: 0.35},
		"naclo":     {Chemical: "naclo", MinRate: 2, MaxRate: 60, MaxJumpRatio: 0.25},
		"carbon":    {Chemical: "carbon", MinRate: 5, MaxRate: 150, MaxJumpRatio: 0.5},
		"phosphate": {Chemical: "phosphate", MinRate: 1, MaxRate: 40, MaxJumpRatio: 0.35},
	}, ChemicalLimit{Chemical: "*", MinRate: 1, MaxRate: 100, MaxJumpRatio: 0.25})
}

func NewRiskPolicy(limits map[string]ChemicalLimit, def ChemicalLimit) RiskPolicy {
	indexed := make(map[string]ChemicalLimit, len(limits))
	for _, limit := range limits {
		key := strings.ToLower(strings.TrimSpace(limit.Chemical))
		if key == "" || key == "*" {
			continue
		}
		copied := limit
		copied.Chemical = key
		indexed[key] = copied
	}
	def.Chemical = "*"
	return RiskPolicy{limits: indexed, defaultLimit: def}
}

func (p RiskPolicy) limitFor(chemical string) ChemicalLimit {
	if limit, ok := p.limits[strings.ToLower(strings.TrimSpace(chemical))]; ok {
		return limit
	}
	return p.defaultLimit
}

// Classify 依据旧值与新值判定风险等级，并返回判定理由。
// oldRate<=0 表示该药剂此前没有下发记录（首次投加），首次投加按高风险处理。
func (p RiskPolicy) Classify(chemical string, oldRate, newRate float64) (RiskLevel, []string) {
	reasons := make([]string, 0, 3)
	limit := p.limitFor(chemical)
	if oldRate <= 0 {
		return RiskHigh, []string{"药剂尚无在执行的投加设定，首次投加需要审核"}
	}
	if newRate < limit.MinRate || newRate > limit.MaxRate {
		reasons = append(reasons, reasonOutOfRange(limit, newRate))
	}
	if limit.MaxJumpRatio > 0 {
		deviation := math.Abs(newRate-oldRate) / oldRate
		if deviation > limit.MaxJumpRatio {
			reasons = append(reasons, reasonJump(limit, oldRate, newRate, deviation))
		}
	}
	if len(reasons) > 0 {
		return RiskHigh, reasons
	}
	return RiskNormal, nil
}

func reasonOutOfRange(limit ChemicalLimit, newRate float64) string {
	direction := "低于"
	if newRate > limit.MaxRate {
		direction = "高于"
	}
	return "新设定" + direction + "药剂 " + limit.Chemical + " 的便捷范围 " +
		formatFloat(limit.MinRate) + "~" + formatFloat(limit.MaxRate)
}

func reasonJump(limit ChemicalLimit, oldRate, newRate, deviation float64) string {
	return "相对当前设定 " + formatFloat(oldRate) + " 变化 " +
		formatPercent(deviation) + "，超过允许跳变 " + formatPercent(limit.MaxJumpRatio)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatPercent(value float64) string {
	return strconv.FormatFloat(value*100, 'f', 0, 64) + "%"
}
