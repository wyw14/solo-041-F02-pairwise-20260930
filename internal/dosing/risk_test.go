package dosing

import "testing"

func TestRiskPolicyClassify(t *testing.T) {
	policy := DefaultRiskPolicy()

	cases := []struct {
		name     string
		chemical string
		oldRate  float64
		newRate  float64
		want     RiskLevel
	}{
		{"普通范围内小幅调整", "pac", 60, 70, RiskNormal},
		{"首次投加为高风险", "pac", 0, 60, RiskHigh},
		{"超出便捷上限", "pac", 60, 150, RiskHigh},
		{"低于便捷下限", "pac", 60, 2, RiskHigh},
		{"跳变比例超限", "pac", 60, 100, RiskHigh},
		{"下调超限", "naclo", 40, 20, RiskHigh},
		{"未知药剂适用默认范围", "unknown-chem", 50, 55, RiskNormal},
		{"未知药剂首次投加", "unknown-chem", 0, 10, RiskHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			level, reasons := policy.Classify(tc.chemical, tc.oldRate, tc.newRate)
			if level != tc.want {
				t.Fatalf("Classify(%s,%v,%v)=%s, want %s (reasons=%v)", tc.chemical, tc.oldRate, tc.newRate, level, tc.want, reasons)
			}
			if level == RiskHigh && len(reasons) == 0 {
				t.Fatal("高风险变更必须给出判定理由")
			}
		})
	}
}
