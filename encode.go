package rulescore

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"
)

// Encode validates rs and returns its compact JSON representation.
//
// Encode accepts exactly the Ruleset values that Decode can produce. In
// particular, Rule.Value must be a string, bool, or valid json.Number; other
// Go values are rejected rather than coerced. Hand-built rulesets are subject
// to the same version, identifier, field, operator, and weight constraints as
// decoded rulesets. A successful Encode can therefore be passed to Decode to
// recover a Ruleset equal field-for-field to rs. Equality here includes rule
// order, whether Rules is nil or non-nil, each Value's dynamic type, and the
// exact decimal text held by json.Number.
//
// Encode also rejects values Go can represent but JSON cannot round-trip:
// invalid UTF-8 in string fields, malformed json.Number text, and NaN or
// infinite weights. Each error identifies the offending rule and field.
//
// The returned JSON is byte-stable for a given Ruleset across calls and
// processes. Struct fields have a fixed order, rules retain declaration order,
// and Rule.Value cannot contain maps or other values with variable ordering.
func Encode(rs Ruleset) ([]byte, error) {
	if err := validateRulesetForEncode(rs); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(rs)
	if err != nil {
		return nil, fmt.Errorf("encode ruleset: %w", err)
	}
	return encoded, nil
}

func validateRulesetForEncode(rs Ruleset) error {
	if rs.Version != 1 {
		return fmt.Errorf("ruleset field %q: unsupported version %d", "version", rs.Version)
	}

	seen := make(map[string]struct{}, len(rs.Rules))
	for i := range rs.Rules {
		rule := &rs.Rules[i]
		if rule.ID == "" {
			return ruleError("<empty>", "id", "must not be empty")
		}
		if !utf8.ValidString(rule.ID) {
			return ruleError(rule.ID, "id", "must contain valid UTF-8")
		}
		if _, exists := seen[rule.ID]; exists {
			return ruleError(rule.ID, "id", "duplicate id")
		}
		seen[rule.ID] = struct{}{}

		if rule.Field == "" {
			return ruleError(rule.ID, "field", "must not be empty")
		}
		if !utf8.ValidString(rule.Field) {
			return ruleError(rule.ID, "field", "must contain valid UTF-8")
		}
		if !rule.Op.valid() {
			return ruleError(rule.ID, "op", "unknown operator %q", rule.Op)
		}
		if err := validateValueForEncode(rule.Value); err != nil {
			return ruleError(rule.ID, "value", "%v", err)
		}
		if math.IsNaN(rule.Weight) || math.IsInf(rule.Weight, 0) {
			return ruleError(rule.ID, "weight", "must be finite")
		}
		if rule.Weight < 0 {
			return ruleError(rule.ID, "weight", "must not be negative")
		}
	}

	return nil
}

func validateValueForEncode(value any) error {
	switch value := value.(type) {
	case json.Number:
		// encoding/json treats an empty json.Number as zero instead of rejecting
		// it. Reject that lossy special case first; Marshal validates every
		// non-empty value's JSON-number grammar without converting via float64.
		if value == "" {
			return fmt.Errorf("must be a valid JSON number: empty text")
		}
		if _, err := json.Marshal(value); err != nil {
			return fmt.Errorf("must be a valid JSON number: %w", err)
		}
		return nil
	case string:
		// encoding/json replaces invalid UTF-8 with U+FFFD. Reject it so a
		// successful round trip cannot silently change a hand-built value.
		if !utf8.ValidString(value) {
			return fmt.Errorf("must contain valid UTF-8")
		}
		return nil
	case bool:
		return nil
	default:
		return fmt.Errorf("must be a number, string, or bool (got %T)", value)
	}
}
