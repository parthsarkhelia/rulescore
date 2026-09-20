package rulescore_test

import (
	"encoding/json"
	"fmt"

	"github.com/parthsarkhelia/rulescore"
)

// Example shows the whole library: decode a ruleset, evaluate a record against
// it, and read both the score and the per-rule breakdown that explains it.
//
// The record has no "region" key, so that rule is reported as an evaluation
// error rather than as a non-match, and contributes nothing to the score.
func Example() {
	rulesetJSON := []byte(`{
  "version": 1,
  "rules": [
    {"id": "adult",    "field": "age",      "op": "gte", "value": 18,   "weight": 2},
    {"id": "verified", "field": "verified", "op": "eq",  "value": true, "weight": 2},
    {"id": "eu",       "field": "region",   "op": "eq",  "value": "eu", "weight": 1}
  ]
}`)

	ruleset, err := rulescore.Decode(rulesetJSON)
	if err != nil {
		fmt.Println("decode:", err)
		return
	}

	record := map[string]any{"age": 31.0, "verified": true}

	result := ruleset.Evaluate(record)
	fmt.Printf("score: %.2f\n", result.Score)
	for _, rule := range result.Rules {
		switch {
		case rule.Err != nil:
			fmt.Printf("%-8s error   %v\n", rule.ID, rule.Err)
		case rule.Matched:
			fmt.Printf("%-8s matched weight %g\n", rule.ID, rule.Weight)
		default:
			fmt.Printf("%-8s no match\n", rule.ID)
		}
	}

	// Output:
	// score: 0.80
	// adult    matched weight 2
	// verified matched weight 2
	// eu       error   field "region": field missing from record
}

// ExampleDecode shows that a ruleset is validated, not merely parsed: Decode
// reports the first rule that is not usable and names the offending field.
func ExampleDecode() {
	_, err := rulescore.Decode([]byte(`{
	  "version": 1,
	  "rules": [{"id": "r1", "field": "age", "op": "between", "value": 18, "weight": 1}]
	}`))
	fmt.Println(err)

	// Output:
	// rule "r1" field "op": unknown operator "between"
}

// ExampleRuleset_Evaluate shows the three ways a rule can fail to contribute:
// a comparison that is performed and does not hold, a field that is missing,
// and a comparison between values of different kinds.
func ExampleRuleset_Evaluate() {
	ruleset := rulescore.Ruleset{
		Version: 1,
		Rules: []rulescore.Rule{
			{ID: "too-young", Field: "age", Op: rulescore.OperatorGreaterThan, Value: json.Number("65"), Weight: 1},
			{ID: "absent", Field: "tier", Op: rulescore.OperatorNotEqual, Value: "free", Weight: 1},
			{ID: "mismatched", Field: "age", Op: rulescore.OperatorEqual, Value: "31", Weight: 1},
		},
	}

	result := ruleset.Evaluate(map[string]any{"age": 31.0})
	fmt.Printf("score: %.2f\n", result.Score)
	for _, rule := range result.Rules {
		fmt.Printf("%-11s matched=%-5t err=%v\n", rule.ID, rule.Matched, rule.Err)
	}

	// Output:
	// score: 0.00
	// too-young   matched=false err=<nil>
	// absent      matched=false err=field "tier": field missing from record
	// mismatched  matched=false err=field "age": record value is float64, rule value is string: type mismatch
}

// ExampleEncode round-trips a hand-built ruleset back to JSON. Rule values must
// be string, bool, or json.Number, which is how a number keeps the exact
// decimal text it was written with.
func ExampleEncode() {
	ruleset := rulescore.Ruleset{
		Version: 1,
		Rules: []rulescore.Rule{
			{ID: "big", Field: "id", Op: rulescore.OperatorGreaterThan, Value: json.Number("9007199254740993"), Weight: 1.5},
		},
	}

	encoded, err := rulescore.Encode(ruleset)
	if err != nil {
		fmt.Println("encode:", err)
		return
	}
	fmt.Println(string(encoded))

	// Output:
	// {"version":1,"rules":[{"id":"big","field":"id","op":"gt","value":9007199254740993,"weight":1.5}]}
}
