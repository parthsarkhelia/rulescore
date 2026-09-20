// Package rulescore defines JSON-backed weighted scoring rules.
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
func Decode(data []byte) (Ruleset, error) {
	var encoded struct {
		Version *int   `json:"version"`
		Rules   []Rule `json:"rules"`
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
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

	seen := make(map[string]struct{}, len(encoded.Rules))
	for i := range encoded.Rules {
		rule := &encoded.Rules[i]
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
		if rule.Weight < 0 {
			return Ruleset{}, ruleError(rule.ID, "weight", "must not be negative")
		}
	}

	return Ruleset{Version: *encoded.Version, Rules: encoded.Rules}, nil
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
