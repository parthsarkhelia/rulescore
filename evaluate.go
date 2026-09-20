package rulescore

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
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

	// ErrInvalidRule reports that a hand-built rule has an unusable operator,
	// numeric value, or weight. Decode rejects these defects before evaluation.
	ErrInvalidRule = errors.New("invalid rule")

	// ErrInvalidRecord reports that a record contains a malformed or non-finite
	// numeric value.
	ErrInvalidRecord = errors.New("invalid record")
)

// classifiedError preserves an established error message while adding a
// sentinel to its error chain.
type classifiedError struct {
	error
	class error
}

func (err classifiedError) Unwrap() error {
	return err.class
}

func classify(err, class error) error {
	return classifiedError{error: err, class: class}
}

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
// Every non-nil RuleResult.Err wraps one of ErrFieldMissing, ErrTypeMismatch,
// ErrNotOrdered, ErrInvalidRule, or ErrInvalidRecord. The first three describe
// comparison failures, ErrInvalidRule identifies a defect in a hand-built
// ruleset, and ErrInvalidRecord identifies unusable numeric input.
//
// The six comparison decisions this package settles:
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
//     record authoring bug behind a plausible-looking score. A hand-built rule
//     whose Value is not a json.Number, string, or bool is classified the same
//     way, preserving ErrTypeMismatch for unsupported rule value types.
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
//
//     A float64 record value is compared as the decimal literal it came from,
//     not as the binary float it is stored as: it is taken at its shortest
//     representation that round-trips (strconv.FormatFloat with precision -1),
//     which is the literal json.Unmarshal parsed. A record's 0.1 therefore
//     equals a rule's json.Number("0.1"), even though the nearest float64 to
//     0.1 is slightly above one tenth. The two record representations agree:
//     the same JSON document scores the same whether it was decoded with
//     json.Unmarshal or with UseNumber.
//
//     Precision a float64 record never carried cannot be recovered. A record
//     literal of 9007199254740993 arrives as float64 9007199254740992 and is
//     compared as that value; decode records with UseNumber to preserve
//     integers beyond 2^53.
//
//     A malformed json.Number record value, or a float64 record value that is
//     NaN or ±Inf, cannot be compared and is reported as ErrInvalidRecord.
//
//  5. Hand-built ruleset defects. Decode rejects unknown operators, malformed
//     numeric rule values, and unusable weights. A Ruleset assembled directly
//     can still contain them, so Evaluate reports each as ErrInvalidRule rather
//     than panicking or silently treating it as a non-match.
//
//  6. Score shape. Score is the sum of the weights of the matched rules divided
//     by the sum of the weights of all rules in the ruleset, matched, unmatched
//     and unevaluable alike. The denominator depends only on the ruleset, so
//     the same ruleset scores every record on the same scale, and a record that
//     is missing fields scores lower rather than being quietly rescaled. If the
//     total weight is zero — an empty ruleset, or one whose weights are all
//     zero — no rule can contribute anything and Score is 0, never NaN.
//
//     The weights are summed as exact rationals, so no ruleset of finite
//     weights can overflow the total to ±Inf and turn Score into NaN or 0.
//     A weight that is not itself a finite, non-negative number makes its rule
//     unevaluable: it is reported as ErrInvalidRule in that rule's
//     RuleResult.Err and left out of both sums, since no such weight has a share
//     of a total. Decode rejects negative weights and JSON cannot express NaN
//     or ±Inf, so this can only arise from a hand-built Ruleset. Score is
//     therefore always in [0, 1].
func (rs Ruleset) Evaluate(record map[string]any) Result {
	results := make([]RuleResult, 0, len(rs.Rules))
	totalWeight, matchedWeight := new(big.Rat), new(big.Rat)

	for _, rule := range rs.Rules {
		var matched bool
		weight, err := ruleWeight(rule.Weight)
		if err == nil {
			totalWeight.Add(totalWeight, weight)
			matched, err = evaluateRule(rule, record)
			if matched {
				matchedWeight.Add(matchedWeight, weight)
			}
		}
		results = append(results, RuleResult{
			ID:      rule.ID,
			Weight:  rule.Weight,
			Matched: matched,
			Err:     err,
		})
	}

	score := 0.0
	if totalWeight.Sign() > 0 {
		// matchedWeight <= totalWeight and both are non-negative, so the
		// quotient is in [0, 1] and Float64 cannot report Inf or NaN.
		score, _ = new(big.Rat).Quo(matchedWeight, totalWeight).Float64()
	}
	return Result{Score: score, Rules: results}
}

// ruleWeight converts a declared weight to an exact rational, rejecting the
// weights that have no share of a total (decision 5).
func ruleWeight(weight float64) (*big.Rat, error) {
	// SetFloat64 is exact for every finite float64 and returns nil for NaN and
	// ±Inf.
	rat := new(big.Rat).SetFloat64(weight)
	if rat == nil || rat.Sign() < 0 {
		return nil, classify(
			fmt.Errorf("weight %v: must be a finite, non-negative number", weight),
			ErrInvalidRule,
		)
	}
	return rat, nil
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
		return false, classify(
			fmt.Errorf("field %q: unknown operator %q", rule.Field, rule.Op),
			ErrInvalidRule,
		)
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
			return 0, classify(
				fmt.Errorf("rule value %q is not a valid number", want.String()),
				ErrInvalidRule,
			)
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
			return nil, classify(
				fmt.Errorf("record value %q is not a valid number", got.String()),
				ErrInvalidRecord,
			)
		}
		return rat, nil

	case float64:
		if math.IsNaN(got) || math.IsInf(got, 0) {
			return nil, classify(
				fmt.Errorf("record value %v is not a finite number", got),
				ErrInvalidRecord,
			)
		}
		// Take the float at its shortest round-tripping decimal rather than at
		// its exact binary value, so that a record decoded by json.Unmarshal
		// and one decoded with UseNumber compare identically: the nearest
		// float64 to 0.1 is just above one tenth, and comparing that binary
		// value against a rule's exact json.Number("0.1") would report the
		// record as greater (decision 4).
		return floatRat(got, strconv.FormatFloat(got, 'g', -1, 64))

	default:
		return nil, fmt.Errorf("record value is %T, want number: %w", got, ErrTypeMismatch)
	}
}

// floatRat parses the shortest round-tripping decimal for a finite float64.
// The text parameter keeps the defensive parse-failure branch directly
// testable even though strconv.FormatFloat always returns a valid decimal.
func floatRat(got float64, text string) (*big.Rat, error) {
	rat, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, classify(
			fmt.Errorf("record value %v is not a valid number", got),
			ErrInvalidRecord,
		)
	}
	return rat, nil
}

func typeMismatch(got, want any) error {
	return fmt.Errorf("record value is %T, rule value is %T: %w", got, want, ErrTypeMismatch)
}
