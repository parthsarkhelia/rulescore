package rulescore

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Sentinel errors reported per rule in RuleResult.Err. They are wrapped with
// context, so callers must test them with errors.Is rather than ==.
var (
	// ErrFieldMissing reports that the record has no key for the rule's field.
	ErrFieldMissing = errors.New("field missing from record")

	// ErrTypeMismatch reports that the record value and the rule value are of
	// different kinds, for example a string field compared against a numeric
	// rule value.
	ErrTypeMismatch = errors.New("type mismatch")

	// ErrNotOrdered reports that an ordering operator (gt, gte, lt, lte) was
	// applied to a type rulescore does not order. Only numbers and strings are
	// ordered; see Evaluate.
	ErrNotOrdered = errors.New("type does not support ordering")
)

// RuleResult records how one rule was accounted for in a Result.
type RuleResult struct {
	// ID is the rule's ID, in the ruleset's declaration order.
	ID string

	// Weight is the rule's weight as declared, whether or not it matched.
	Weight float64

	// Matched reports whether the comparison held. It is false whenever Err is
	// non-nil: a rule that could not be evaluated never contributes to the score.
	Matched bool

	// Err is non-nil when the rule could not be evaluated at all, and carries
	// the reason. Matched == false with Err == nil means the comparison was
	// performed and did not hold; Err != nil means no comparison was possible.
	// This is the distinction between "did not match" and "could not be
	// evaluated"; callers that must not silently absorb bad records should
	// reject any Result containing a non-nil Err.
	Err error
}

// Result is the outcome of evaluating a Ruleset against one record.
type Result struct {
	// Score is the matched weight divided by the total weight of every rule in
	// the ruleset, so it always lies in [0, 1] and is comparable across records
	// evaluated by the same ruleset. See Evaluate for the exact definition.
	Score float64

	// Rules holds one entry per rule in the ruleset, in declaration order.
	// Every rule is accounted for, including ones that could not be evaluated.
	Rules []RuleResult
}

// Evaluate scores record against rs and explains the result rule by rule.
//
// Records are the shape json.Unmarshal produces for an object: numbers arrive
// as float64 (plain Unmarshal) or json.Number (Unmarshal with UseNumber), and
// both are accepted. Other Go numeric types (int, float32, ...) are not
// recognised as numbers and compare as a type mismatch.
//
// Evaluate returns no error. Every failure belongs to a single rule and is
// recorded in that rule's RuleResult.Err, so one unevaluable rule neither
// panics nor discards the score of the rules around it.
//
// The five comparison decisions this package settles:
//
//  1. Missing field. A rule whose field is absent from the record is an
//     evaluation error (ErrFieldMissing), not a silent non-match. An absent
//     field is a statement about the record, not about the value, so "ne"
//     against a missing field reports the error rather than reporting true.
//     The rule still appears in Result.Rules with Matched false and contributes
//     zero to the score.
//
//  2. Type mismatch. Comparing across kinds — number against string, bool
//     against string, either against a number — is an evaluation error
//     (ErrTypeMismatch) for every operator, "eq" and "ne" included. Cross-kind
//     values are never equal, but reporting "not equal" would hide a ruleset or
//     record authoring bug behind a plausible-looking score.
//
//  3. Ordering on non-numerics. Strings are ordered lexicographically by byte,
//     which is Go's native string ordering and orders the common case of
//     zero-padded or ISO-8601 values correctly. Bools have no meaningful
//     ordering, so gt, gte, lt and lte on a bool are an evaluation error
//     (ErrNotOrdered). Bools remain comparable with eq and ne.
//
//  4. Numeric comparison. Numbers are compared exactly, as arbitrary-precision
//     rationals (math/big), never by converting through float64. A
//     json.Number rule value of 9007199254740993 is therefore greater than a
//     record value of 9007199254740992, a distinction float64 cannot represent.
//     A record number that is NaN or ±Inf cannot be ordered and is reported as
//     an evaluation error.
//
//  5. Score shape. Score is the sum of the weights of the matched rules divided
//     by the sum of the weights of all rules in the ruleset, matched, unmatched
//     and unevaluable alike. The denominator depends only on the ruleset, so
//     the same ruleset scores every record on the same scale, and a record that
//     is missing fields scores lower rather than being quietly rescaled. If the
//     total weight is zero — an empty ruleset, or one whose weights are all
//     zero — no rule can contribute anything and Score is 0, never NaN.
func (rs Ruleset) Evaluate(record map[string]any) Result {
	results := make([]RuleResult, 0, len(rs.Rules))
	var totalWeight, matchedWeight float64

	for _, rule := range rs.Rules {
		totalWeight += rule.Weight

		matched, err := evaluateRule(rule, record)
		if matched {
			matchedWeight += rule.Weight
		}
		results = append(results, RuleResult{
			ID:      rule.ID,
			Weight:  rule.Weight,
			Matched: matched,
			Err:     err,
		})
	}

	score := 0.0
	if totalWeight > 0 {
		score = matchedWeight / totalWeight
	}
	return Result{Score: score, Rules: results}
}

// evaluateRule applies one rule to a record, reporting whether it matched or
// why it could not be evaluated. A non-nil error always accompanies false.
func evaluateRule(rule Rule, record map[string]any) (bool, error) {
	got, ok := record[rule.Field]
	if !ok {
		return false, fmt.Errorf("field %q: %w", rule.Field, ErrFieldMissing)
	}

	switch rule.Op {
	case OperatorEqual, OperatorNotEqual:
		equal, err := valuesEqual(got, rule.Value)
		if err != nil {
			return false, fmt.Errorf("field %q: %w", rule.Field, err)
		}
		return equal == (rule.Op == OperatorEqual), nil

	case OperatorGreaterThan, OperatorGreaterThanOrEqual, OperatorLessThan, OperatorLessThanOrEqual:
		cmp, err := compareValues(got, rule.Value)
		if err != nil {
			return false, fmt.Errorf("field %q: %w", rule.Field, err)
		}
		switch rule.Op {
		case OperatorGreaterThan:
			return cmp > 0, nil
		case OperatorGreaterThanOrEqual:
			return cmp >= 0, nil
		case OperatorLessThan:
			return cmp < 0, nil
		default:
			return cmp <= 0, nil
		}

	default:
		// Decode rejects unknown operators, but a hand-built Ruleset can carry
		// one — including the empty Operator. Report it instead of comparing.
		return false, fmt.Errorf("field %q: unknown operator %q", rule.Field, rule.Op)
	}
}

// valuesEqual reports whether a record value equals a rule value of the same
// kind, and reports ErrTypeMismatch across kinds (decision 2).
func valuesEqual(got, want any) (bool, error) {
	if wantBool, ok := want.(bool); ok {
		gotBool, ok := got.(bool)
		if !ok {
			return false, typeMismatch(got, want)
		}
		return gotBool == wantBool, nil
	}

	cmp, err := compareValues(got, want)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

// compareValues orders a record value against a rule value, returning a
// negative number, zero or a positive number as got is less than, equal to or
// greater than want. Only numbers and strings are ordered (decision 3).
func compareValues(got, want any) (int, error) {
	switch want := want.(type) {
	case json.Number:
		gotNum, err := toRat(got)
		if err != nil {
			return 0, err
		}
		wantNum, ok := new(big.Rat).SetString(want.String())
		if !ok {
			return 0, fmt.Errorf("rule value %q is not a valid number", want.String())
		}
		return gotNum.Cmp(wantNum), nil

	case string:
		gotStr, ok := got.(string)
		if !ok {
			return 0, typeMismatch(got, want)
		}
		return strings.Compare(gotStr, want), nil

	case bool:
		if _, ok := got.(bool); !ok {
			return 0, typeMismatch(got, want)
		}
		return 0, fmt.Errorf("bool: %w", ErrNotOrdered)

	default:
		// Decode constrains Rule.Value to json.Number, string or bool; a
		// hand-built Rule may not.
		return 0, fmt.Errorf("unsupported rule value type %T: %w", want, ErrTypeMismatch)
	}
}

// toRat converts a record number to an exact rational, so that comparison
// never round-trips through float64 (decision 4).
func toRat(got any) (*big.Rat, error) {
	switch got := got.(type) {
	case json.Number:
		rat, ok := new(big.Rat).SetString(got.String())
		if !ok {
			return nil, fmt.Errorf("record value %q is not a valid number", got.String())
		}
		return rat, nil

	case float64:
		// SetFloat64 is exact for every finite float64 and returns nil for NaN
		// and ±Inf, which have no place in an ordering.
		rat := new(big.Rat).SetFloat64(got)
		if rat == nil {
			return nil, fmt.Errorf("record value %v is not a finite number", got)
		}
		return rat, nil

	default:
		return nil, fmt.Errorf("record value is %T, want number: %w", got, ErrTypeMismatch)
	}
}

func typeMismatch(got, want any) error {
	return fmt.Errorf("record value is %T, rule value is %T: %w", got, want, ErrTypeMismatch)
}
