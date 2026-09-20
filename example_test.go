package rulescore_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/parthsarkhelia/rulescore"
)

// Example shows the whole library on one worked problem: you are comparing
// flats to rent, so your criteria become a ruleset and each listing becomes a
// record. Decode the ruleset, evaluate one listing against it, and read both
// the score and the per-rule breakdown that explains it.
//
// The listing has no "pets_allowed" key, so that rule is reported as an
// evaluation error rather than as a non-match: "pets are not allowed" and
// "the listing does not say" are different answers. It contributes nothing to
// the score either way.
func Example() {
	rulesetJSON := []byte(`{
  "version": 1,
  "rules": [
    {"id": "rent",      "field": "rent_pcm",      "op": "lte", "value": 1500,        "weight": 4},
    {"id": "commute",   "field": "km_to_work",    "op": "lte", "value": 5,           "weight": 2},
    {"id": "pets",      "field": "pets_allowed",  "op": "eq",  "value": true,        "weight": 2},
    {"id": "area",      "field": "neighbourhood", "op": "eq",  "value": "Southbank", "weight": 1},
    {"id": "furnished", "field": "furnished",     "op": "eq",  "value": true,        "weight": 1}
  ]
}`)

	ruleset, err := rulescore.Decode(rulesetJSON)
	if err != nil {
		fmt.Println("decode:", err)
		return
	}

	record := map[string]any{
		"rent_pcm":      1400.0,
		"km_to_work":    9.0,
		"neighbourhood": "Southbank",
		"furnished":     true,
	}

	result := ruleset.Evaluate(record)
	fmt.Printf("score: %.2f\n", result.Score)
	for _, rule := range result.Rules {
		switch {
		case rule.Err != nil:
			fmt.Printf("%-9s error   %v\n", rule.ID, rule.Err)
		case rule.Matched:
			fmt.Printf("%-9s matched weight %g\n", rule.ID, rule.Weight)
		default:
			fmt.Printf("%-9s no match\n", rule.ID)
		}
	}

	// Output:
	// score: 0.60
	// rent      matched weight 4
	// commute   no match
	// pets      error   field "pets_allowed": field missing from record
	// area      matched weight 1
	// furnished matched weight 1
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

// ExampleRuleset_Evaluate_nonFiniteRecordValue shows that a NaN or infinite
// record number is a per-rule evaluation error classified as invalid record
// data rather than as a defect in the rule.
func ExampleRuleset_Evaluate_nonFiniteRecordValue() {
	ruleset := rulescore.Ruleset{
		Version: 1,
		Rules: []rulescore.Rule{
			{ID: "high", Field: "score", Op: rulescore.OperatorGreaterThan, Value: json.Number("10"), Weight: 1},
		},
	}

	err := ruleset.Evaluate(map[string]any{"score": math.NaN()}).Rules[0].Err
	fmt.Println(err)
	fmt.Println("is ErrInvalidRecord:", errors.Is(err, rulescore.ErrInvalidRecord))
	fmt.Println("is ErrInvalidRule:  ", errors.Is(err, rulescore.ErrInvalidRule))

	// Output:
	// field "score": record value NaN is not a finite number
	// is ErrInvalidRecord: true
	// is ErrInvalidRule:   false
}

// ExampleRuleset_Evaluate_unusableWeight shows the one exception to "the
// denominator is every rule": a weight that is not a finite, non-negative
// number leaves its rule out of both sums. Decode rejects negative weights and
// JSON cannot express NaN or Inf, so this only arises from a hand-built
// Ruleset.
func ExampleRuleset_Evaluate_unusableWeight() {
	ruleset := rulescore.Ruleset{
		Version: 1,
		Rules: []rulescore.Rule{
			{ID: "pro", Field: "tier", Op: rulescore.OperatorEqual, Value: "pro", Weight: 1},
			{ID: "negative", Field: "tier", Op: rulescore.OperatorEqual, Value: "free", Weight: -1},
		},
	}

	result := ruleset.Evaluate(map[string]any{"tier": "pro"})
	fmt.Printf("score: %.2f\n", result.Score)
	for _, rule := range result.Rules {
		fmt.Printf("%-9s matched=%-5t err=%v\n", rule.ID, rule.Matched, rule.Err)
	}

	// Output:
	// score: 1.00
	// pro       matched=true  err=<nil>
	// negative  matched=false err=weight -1: must be a finite, non-negative number
}
