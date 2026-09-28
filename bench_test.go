package rulescore

import (
	"encoding/json"
	"fmt"
	"testing"
)

var benchSizes = []int{10, 100, 1000}

// benchRuleset builds n rules that cycle through every operator and value
// kind, and a record in which roughly half of them match.
func benchRuleset(n int) (Ruleset, map[string]any) {
	ops := []Operator{
		OperatorEqual, OperatorNotEqual, OperatorGreaterThan,
		OperatorGreaterThanOrEqual, OperatorLessThan, OperatorLessThanOrEqual,
	}
	rs := Ruleset{Version: 1, Rules: make([]Rule, n)}
	record := make(map[string]any, n)
	for i := range n {
		field := fmt.Sprintf("f%d", i)
		op := ops[i%len(ops)]
		switch i % 3 {
		case 0:
			rs.Rules[i] = Rule{ID: field, Field: field, Op: op, Value: json.Number("100.5"), Weight: 1.5}
			record[field] = float64(i % 200)
		case 1:
			rs.Rules[i] = Rule{ID: field, Field: field, Op: OperatorEqual, Value: "active", Weight: 2}
			record[field] = []string{"active", "inactive"}[i%2]
		default:
			rs.Rules[i] = Rule{ID: field, Field: field, Op: OperatorNotEqual, Value: true, Weight: 0.25}
			record[field] = i%2 == 0
		}
	}
	return rs, record
}

func BenchmarkDecode(b *testing.B) {
	for _, n := range benchSizes {
		rs, _ := benchRuleset(n)
		data, err := Encode(rs)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Decode(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEvaluate(b *testing.B) {
	for _, n := range benchSizes {
		rs, record := benchRuleset(n)
		if res := rs.Evaluate(record); res.Score == 0 || res.Score == 1 {
			b.Fatalf("fixture should match some but not all rules, score %v", res.Score)
		}
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rs.Evaluate(record)
			}
		})
	}
}
