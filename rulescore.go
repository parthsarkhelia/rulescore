package rulescore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Operator identifies the comparison a rule performs.
type Operator string

const (
	// OperatorEqual compares values for equality.
	OperatorEqual Operator = "eq"
	// OperatorNotEqual compares values for inequality.
	OperatorNotEqual Operator = "ne"
	// OperatorGreaterThan compares whether a value is greater than another.
	OperatorGreaterThan Operator = "gt"
	// OperatorGreaterThanOrEqual compares whether a value is greater than or equal to another.
	OperatorGreaterThanOrEqual Operator = "gte"
	// OperatorLessThan compares whether a value is less than another.
	OperatorLessThan Operator = "lt"
	// OperatorLessThanOrEqual compares whether a value is less than or equal to another.
	OperatorLessThanOrEqual Operator = "lte"
)

// Rule describes one weighted comparison in a ruleset.
type Rule struct {
	// ID uniquely identifies the rule within its ruleset.
	ID string `json:"id"`
	// Field names the input field the rule will inspect.
	Field string `json:"field"`
	// Op is the comparison operator.
	Op Operator `json:"op"`
	// Value uses string, bool, or json.Number so JSON numbers round-trip without float64 precision loss.
	Value any `json:"value"`
	// Weight is the rule's non-negative contribution.
	Weight float64 `json:"weight"`
}

// Ruleset is a versioned collection of rules.
type Ruleset struct {
	// Version identifies the ruleset schema version.
	Version int `json:"version"`
	// Rules contains the rules in declaration order.
	Rules []Rule `json:"rules"`
}

// Decode parses and validates one JSON ruleset.
//
// Decode is schema-strict: a ruleset that does not say what it meant is an
// error, never a quietly defaulted value.
//
//   - Unknown keys are rejected, on the ruleset object and on every rule, so a
//     misspelled "weigth" is reported instead of dropped. The error names the
//     offending key; the standard library does not report which rule carried
//     it.
//   - "version" and "rules" are both required. A missing "rules" key is a
//     typo, so it is rejected; an explicit [] or null is an empty ruleset,
//     which is legitimate and scores every record 0.
//   - Every rule must carry every key: "id", "field", "op", "value" and
//     "weight". "weight" has no default, because a rule that contributes
//     nothing to any score is not a rule anyone meant to write. An explicit
//     "weight": 0 remains valid.
//
// Decode accepts everything Encode produces, so a ruleset still round-trips.
func Decode(data []byte) (Ruleset, error) {
	var encoded struct {
		Version *int            `json:"version"`
		Rules   json.RawMessage `json:"rules"`
	}

	decoder := strictDecoder(data)
	if err := decoder.Decode(&encoded); err != nil {
		return Ruleset{}, fmt.Errorf("decode ruleset: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Ruleset{}, err
	}

	if encoded.Version == nil {
		return Ruleset{}, fmt.Errorf("ruleset field %q: is required", "version")
	}
	if *encoded.Version != 1 {
		return Ruleset{}, fmt.Errorf("ruleset field %q: unsupported version %d", "version", *encoded.Version)
	}
	// An absent key leaves the raw message empty, while an explicit null keeps
	// its four bytes and decodes to a nil rule slice, as Encode emits for one.
	if len(encoded.Rules) == 0 {
		return Ruleset{}, fmt.Errorf("ruleset field %q: is required", "rules")
	}

	// The outer decoder captured the rules verbatim, so they need a strict pass
	// of their own rather than inheriting the outer one's.
	var decoded []encodedRule
	if err := strictDecoder(encoded.Rules).Decode(&decoded); err != nil {
		return Ruleset{}, fmt.Errorf("decode ruleset: %w", err)
	}

	var rules []Rule
	if decoded != nil {
		rules = make([]Rule, 0, len(decoded))
	}
	seen := make(map[string]struct{}, len(decoded))
	for i := range decoded {
		rule := &decoded[i]
		if rule.ID == "" {
			return Ruleset{}, ruleError("<empty>", "id", "must not be empty")
		}
		if _, exists := seen[rule.ID]; exists {
			return Ruleset{}, ruleError(rule.ID, "id", "duplicate id")
		}
		seen[rule.ID] = struct{}{}

		if rule.Field == "" {
			return Ruleset{}, ruleError(rule.ID, "field", "must not be empty")
		}
		if !rule.Op.valid() {
			return Ruleset{}, ruleError(rule.ID, "op", "unknown operator %q", rule.Op)
		}
		if !validValue(rule.Value) {
			return Ruleset{}, ruleError(rule.ID, "value", "must be a number, string, or bool")
		}
		if rule.Weight == nil {
			return Ruleset{}, ruleError(rule.ID, "weight", "is required")
		}
		if *rule.Weight < 0 {
			return Ruleset{}, ruleError(rule.ID, "weight", "must not be negative")
		}

		rules = append(rules, Rule{
			ID:     rule.ID,
			Field:  rule.Field,
			Op:     rule.Op,
			Value:  rule.Value,
			Weight: *rule.Weight,
		})
	}

	return Ruleset{Version: *encoded.Version, Rules: rules}, nil
}

// encodedRule mirrors Rule with a pointer weight, so that an omitted "weight"
// stays distinguishable from an explicit 0.
type encodedRule struct {
	ID     string   `json:"id"`
	Field  string   `json:"field"`
	Op     Operator `json:"op"`
	Value  any      `json:"value"`
	Weight *float64 `json:"weight"`
}

func strictDecoder(data []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder
}

func (op Operator) valid() bool {
	switch op {
	case OperatorEqual, OperatorNotEqual, OperatorGreaterThan, OperatorGreaterThanOrEqual,
		OperatorLessThan, OperatorLessThanOrEqual:
		return true
	default:
		return false
	}
}

func validValue(value any) bool {
	switch value.(type) {
	case json.Number, string, bool:
		return true
	default:
		return false
	}
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode ruleset: multiple JSON values")
		}
		return fmt.Errorf("decode ruleset: %w", err)
	}
	return nil
}

func ruleError(id, field, format string, args ...any) error {
	return fmt.Errorf("rule %q field %q: %s", id, field, fmt.Sprintf(format, args...))
}
