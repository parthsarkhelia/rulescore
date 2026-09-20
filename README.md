# rulescore

A small Go library for evaluating weighted scoring rules declared in JSON.

You write a ruleset as JSON — a list of independent comparisons, each with a
weight. `rulescore` decodes and validates it, evaluates it against a record,
and returns a score in `[0, 1]` plus a per-rule breakdown explaining how that
score was reached. No DSL, no code generation, no dependencies outside the
standard library.

```
go get github.com/parthsarkhelia/rulescore
```

Requires Go 1.25 or later.

## Concepts

If you have not used a scoring engine before, read this before the quickstart.
The library is one small idea, and the idea is easier to meet than the API.

### A choice you already know how to make

You are looking for a flat to rent, and three are on offer. The cheap one is a
long way from work. The one around the corner is over budget. The third would
suit you, but its listing says nothing about pets — and you have a cat.

Nobody needs a program for three flats. But you already know how you would
settle it: write down what you want, admit that some of it matters more to you
than the rest, check each flat against the list, and prefer the flat that ticks
more of what matters. Everything below is that habit, written down so a program
can apply it to three flats or three thousand.

### The words for it

**One flat, written down.** A flat is a handful of facts you can look up: the
rent, the distance to work, the neighbourhood, whether pets are allowed,
whether it comes furnished. Written as data, that is a **record** — a plain bag
of named facts, where each name is a **field**:

```
rent_pcm       1400
km_to_work     9
neighbourhood  "Southbank"
furnished      true
```

Each field holds a number, a piece of text, or a yes/no. Nothing nested,
nothing computed.

**One thing you want.** "Rent of at most 1500 a month" is a claim about one
field that is either true or false of a given flat. Writing it down takes three
parts: the **field** to look at (`rent_pcm`), the **operator** — which kind of
comparison to make (at most) — and the **value** to compare against (`1500`).
That is a **rule**. There are six operators and they are the obvious ones:
equal, not equal, greater than, less than, and the two or-equal forms.

**Your whole list.** Your rules together are a **ruleset**: your criteria in
one object, ready to apply to any flat.

**What matters more.** Rent matters more to you than furnishing does — you
would buy a sofa before you would pay an extra 300 a month. So each rule
carries a **weight**, a non-negative number saying how much that rule counts.
Only the ratios matter: rent at `4` and furnished at `1` says rent is worth
four furnishings to you.

**Checking one flat.** Apply the ruleset to a record and each rule either holds
or it does not. A rule that holds is a **match** and its weight counts towards
that flat; a rule that does not hold contributes nothing.

**The verdict.** The **score** is the weight you matched divided by the weight
you were offering — the sum of every rule's weight. It therefore lands between
`0` and `1`, and because the divisor comes from the ruleset alone, every flat
is measured on the same scale:

| Flat | `rent_pcm` | `km_to_work` | `pets_allowed` | `neighbourhood` | `furnished` | Score |
|------|-----------|-------------|---------------|----------------|------------|-------|
| Rosewood Court | 1400 | 9 | *not stated* | Southbank | true | `0.60` |
| Harbour View | 1750 | 2 | true | Southbank | false | `0.50` |
| Elm Row | 1450 | 4 | true | Elmfield | false | `0.80` |

**Why, not just how much.** `0.60` on its own tells you nothing you can act on.
So alongside the score you get a **breakdown**: one entry per rule, in the
order you wrote them, saying whether that rule matched and what its weight was.
For Rosewood Court it reads: rent matched (`4`), commute did not (`2`),
neighbourhood matched (`1`), furnished matched (`1`) — and pets could not be
checked at all, because the listing has no `pets_allowed` field. That last
distinction is the point of the breakdown. "This flat does not take pets" and
"nobody said" are different answers, and a bare `0.60` hides both behind the
same missing weight.

### The same list as JSON

```json
{
  "version": 1,
  "rules": [
    {"id": "rent",      "field": "rent_pcm",      "op": "lte", "value": 1500,        "weight": 4},
    {"id": "commute",   "field": "km_to_work",    "op": "lte", "value": 5,           "weight": 2},
    {"id": "pets",      "field": "pets_allowed",  "op": "eq",  "value": true,        "weight": 2},
    {"id": "area",      "field": "neighbourhood", "op": "eq",  "value": "Southbank", "weight": 1},
    {"id": "furnished", "field": "furnished",     "op": "eq",  "value": true,        "weight": 1}
  ]
}
```

Two things in there are the library's own bookkeeping rather than part of the
idea. `version` says which ruleset schema the document follows, and today the
only accepted value is `1`. Each rule's `id` is the name that rule is reported
under in the breakdown, so it has to be unique within the ruleset.

## Quickstart

The same list in Go, scored against the Rosewood Court listing. This is the
body of the `Example` function in
[`example_test.go`](example_test.go), so CI verifies its output on every push to `main` and on every pull request.

<!-- rulescore-example:quickstart -->
```go
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
```

<!-- rulescore-example:output -->
```
score: 0.60
rent      matched weight 4
commute   no match
pets      error   field "pets_allowed": field missing from record
area      matched weight 1
furnished matched weight 1
```

The record is the shape `json.Unmarshal` produces for a JSON object. Numbers
may arrive as `float64` (plain `Unmarshal`) or `json.Number` (`Unmarshal` with
`UseNumber`); both are accepted and both score identically. Other Go numeric
types — `int`, `float32` — are not recognised as numbers.

## What the library adds to the bare idea

Four things, each of them an answer to a question the flat hunt raises on its
own.

- **A typo is not a criterion.** Write `"weigth": 4` and you have not said how
  much rent matters — you have said nothing. `Decode` fails with
  `unknown field "weigth"` rather than scoring every flat against a rule that
  could never contribute; leaving `weight` out entirely is an error too, not a
  default. See [The ruleset](#the-ruleset).
- **A broken rule and an incomplete listing are different faults.** Rosewood
  Court's missing `pets_allowed` is the listing's problem; `"op": "under"` is
  yours. Each per-rule failure is classified, so you can reject records you do
  not trust without also swallowing your own mistakes. See
  [What counts as an error](#what-counts-as-an-error).
- **1500 means 1500.** Numbers are compared as exact decimals, not through
  `float64`, so a rent of exactly `1500` is at most `1500` and a rule written
  `0.1` matches a record's `0.1`. See [Operators](#operators).
- **The number is not the answer.** `Evaluate` returns the breakdown next to
  the score, accounting for every rule including the ones it could not
  evaluate, so a `0.60` can always be explained. See
  [What the score means](#what-the-score-means).

## The ruleset

`version` and `rules` are both required, and `version` must be `1`. Every rule
must carry all five of `id`, `field`, `op`, `value` and `weight`, and `Decode`
rejects an empty or duplicate `id`, an empty `field`, an unknown `op`, a
`value` that is not a number, string or bool, and a negative `weight`, naming
the rule and field at fault. It also rejects trailing content after the ruleset
object.

`Decode` is schema-strict: a ruleset that does not say what it meant is an
error, never a quietly defaulted value.

- **Unknown keys are rejected**, on the ruleset object and on every rule, so
  `"weigth": 0.5` is reported instead of dropped. The error names the offending
  key; the standard library does not report which rule carried it.
- **`weight` has no default.** A rule that contributes nothing to any score is
  not a rule anyone meant to write, so an omitted `weight` is an error. An
  explicit `"weight": 0` is still valid.
- **A missing `rules` key is an error**, while an explicit `[]` or `null` is an
  empty ruleset — legitimate, and scoring every record `0`. A missing key is a
  typo; an empty list is a statement.

This strictness arrived in v0.3.0 and is a breaking change: rulesets with an
unknown key at either level, with a rule that omits `weight`, or with no
`rules` key at all used to decode and now fail. `Encode` is unaffected —
everything it emits still decodes, unchanged.

## Operators

Six, and only these six:

| Op    | Meaning               | Applies to             |
|-------|-----------------------|------------------------|
| `eq`  | equal                 | numbers, strings, bools |
| `ne`  | not equal             | numbers, strings, bools |
| `gt`  | greater than          | numbers, strings        |
| `gte` | greater than or equal | numbers, strings        |
| `lt`  | less than             | numbers, strings        |
| `lte` | less than or equal    | numbers, strings        |

Strings are ordered lexicographically by byte, which orders zero-padded and
ISO-8601 values correctly. Bools have no ordering, so `gt`/`gte`/`lt`/`lte` on
a bool is an error.

Numbers are compared exactly, as arbitrary-precision rationals, never through
`float64`. A rule value of `9007199254740993` is therefore genuinely greater
than a record value of `9007199254740992`. A `float64` record value is compared
as the decimal literal it was parsed from, so a record's `0.1` equals a rule's
`0.1`. Precision a `float64` never carried cannot be recovered: decode records
with `UseNumber` if you need integers beyond 2^53.

## What counts as an error

`Evaluate` returns no error value. Every failure belongs to one rule and is
recorded in that rule's `RuleResult.Err`, so a single bad rule neither panics
nor discards the rest of the score.

| Situation | `errors.Is` classification |
|-----------|----------------------------|
| Comparison performed, did not hold | `Err == nil` |
| Field absent from the record | `ErrFieldMissing` |
| Record and rule values are different kinds | `ErrTypeMismatch` |
| Hand-built rule has an unsupported value type | `ErrTypeMismatch` |
| Ordering operator applied to a bool | `ErrNotOrdered` |
| Hand-built rule has an unknown operator | `ErrInvalidRule` |
| Hand-built rule has a malformed `json.Number` value | `ErrInvalidRule` |
| Rule weight is negative, NaN, or ±Inf | `ErrInvalidRule` |
| Record has a malformed `json.Number` value | `ErrInvalidRecord` |
| Record number is NaN or ±Inf | `ErrInvalidRecord` |
| A finite `float64` cannot be parsed from its formatted decimal | `ErrInvalidRecord` |

The last row is a defensive path: the standard library's `FormatFloat`
currently guarantees a valid decimal for every finite `float64`, but the path
is still classified if that invariant ever changes.

Two choices here are deliberate and worth knowing before you write rules:

- **A missing field is an error, not a non-match.** `ne` against a missing
  field reports `ErrFieldMissing` rather than reporting `true`. An absent field
  is a statement about the record, not about the value.
- **A type mismatch is an error for every operator, `eq` and `ne` included.**
  Cross-kind values are never equal, but answering "not equal" would hide a
  ruleset or record authoring bug behind a plausible-looking score.

Every non-nil `RuleResult.Err` wraps exactly one of the five sentinels above,
so test it with `errors.Is`, not `==`. `ErrInvalidRule` groups defects in a
hand-built ruleset that `Decode` would reject; `ErrInvalidRecord` groups
malformed or non-finite numeric input. For example, a NaN record number still
reports `field "score": record value NaN is not a finite number`, and
`errors.Is(err, ErrInvalidRecord)` is true.

A rule that could not be evaluated is always `Matched: false`. If your caller
must not absorb bad records silently, reject any `Result` containing a non-nil
`Err`.

## What the score means

`Score` is the sum of the weights of the matched rules divided by the sum of
the weights of every rule whose weight is usable — matched, unmatched, and
unevaluable alike.

The denominator depends only on the ruleset, so the same ruleset scores every
record on the same scale, and a record missing half its fields scores lower
rather than being quietly rescaled against the rules it did answer. Weights are
summed as exact rationals, so the total cannot overflow to ±Inf. If the total
weight is zero, `Score` is `0`, never NaN. `Score` is always in `[0, 1]`.

The one exception: a weight that is not a finite, non-negative number is not a
share of any total, so that rule is left out of **both** sums, not just the
numerator. A ruleset of one matched `weight: 1` rule and one unmatched
`weight: -1` rule scores `1.00`, not `0.50`. `Decode` rejects negative weights
and JSON cannot express NaN or ±Inf, so this only reaches `Evaluate` from a
hand-built `Ruleset`. It is shown in `ExampleRuleset_Evaluate_unusableWeight`.

## Writing rulesets back out

`Encode` is the inverse of `Decode`. It validates the ruleset and returns
compact, byte-stable JSON; a successful `Encode` fed back to `Decode` yields a
`Ruleset` equal field-for-field to the original, including rule order, each
value's dynamic type, and the exact decimal text of a `json.Number`.

Hand-built rulesets are held to the same rules as decoded ones: `Rule.Value`
must be a `string`, `bool`, or valid `json.Number`, and is rejected rather than
coerced if it is anything else.

## Limitations

These are real and unlikely to change soon. If any of them is a problem for
you, this is not the library you want.

- **`field` is a top-level key.** Nested paths such as `user.address.city` are not
  supported. The field name is looked up directly in the record map.
- **Six comparison operators only.** There is no `in`, `contains`, `matches`,
  or `exists`.
- **Rules are independent and additively weighted.** There is no AND/OR
  grouping and no nesting. You cannot express "A and either B or C".
- **No custom functions.** Comparisons are the six operators against a literal
  value; you cannot call your own code from a rule.
- **The API is not stable yet.** Expect breaking changes before v1.

There are no benchmarks in this repository, so it makes no performance claims.

## Documentation

`go doc -all github.com/parthsarkhelia/rulescore`, or
[pkg.go.dev](https://pkg.go.dev/github.com/parthsarkhelia/rulescore). The doc
comments on `Evaluate` and `Encode` are the authoritative statement of the
semantics summarised above.

## License

MIT — see [LICENSE](LICENSE).
