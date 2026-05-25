# Tree-sitter grammar review: call sites, error recovery, and design flags

Analysis of `tree-sitter-witcherscript/grammar.js` (~866 lines). Two
purposes:

1. Document how call sites and arguments are represented, and how to stop an
   unclosed call from collapsing the whole expression into an `ERROR` node.
2. Flag anything in the current grammar design that is invalid, bad practice, or
   poor design. The grammar is unreleased and used only by this project, so now
   is the time to fix it.

Line numbers refer to `grammar.js` as of this review.

---

## Part 1 — How call arguments are represented today

Three rules cover call sites:

```js
func_call_expr: prec.left(
  PREC.CALL,
  seq(
    field("func", $._expr),
    "(",
    field("args", optional($.func_call_args)),
    ")",
  ),
);
func_call_args: choice($._expr, comma2(optional($._expr))); // comma2 = delim2(rule, ',')
```

Resulting shapes:

- **0 args** — `func_call_expr` with no `func_call_args` child.
- **1 arg** — `func_call_args` wrapping a single `_expr`.
- **2+ args** — `func_call_args` = `optional(_expr)` items with anonymous `,`
  tokens between them.

Because the slots are `optional(_expr)`, an omitted argument (`foo(a, , c)`,
`foo(a,)`) is **grammatically valid** — it produces no node, just a zero-width
gap between the `,` and the next `,`/`)`. A "missing required argument" squiggle
is therefore a **diagnostics concern** (arity check against the resolved
callee), not a grammar one; the range is that gap.

---

## Part 2 — The unclosed-call `ERROR` cascade

### Symptom

`func_call_expr` makes `)` **mandatory**. While a call is being typed
(`foo(a, ` with no `)` yet), tree-sitter cannot complete the rule _or_ form the
node, so it discards the whole expression into a generic `ERROR` node wrapping
`func` + `(` + partial `func_call_args`. The entire call goes red instead of
just the missing tail. Confirmed with `dump_tree`:

```
ERROR
  ident / member_access_expr   <- the callee
  (
  func_call_args               <- partial args, if any
```

### Recommended fix — the grammar already has the pattern

`member_access_expr` (grammar.js:668-684) is a `choice` of the complete rule and
an `incomplete_member_access_expr` (`accessor` + `.`, no member). Its comment
states the intent: a distinct named node so downstream tools can flag it "while
still allowing tree-sitter to close enclosing blocks rather than cascading to a
block-level ERROR." Lines 138-143 add the supporting philosophy: short rules
recover better.

Apply the same shape to calls:

```js
func_call_expr: $ => choice(
  prec.left(PREC.CALL, seq(field('func', $._expr), '(', field('args', optional($.func_call_args)), ')')),
  $.incomplete_func_call_expr,
),
incomplete_func_call_expr: $ => prec.left(PREC.CALL, seq(
  field('func', $._expr), '(', field('args', optional($.func_call_args))
)),
```

`foo(a, ` then parses as a real `incomplete_func_call_expr` with intact
`func`/`args` fields; the error is localized to the missing `)`, and enclosing
blocks stay structured.

### Risks / follow-up

- **Conflicts.** The two `choice` branches share a prefix up to `)`. The
  `incomplete_member_access_expr` precedent works, so this is feasible, but a
  `conflicts: [[$.func_call_expr, $.incomplete_func_call_expr]]` entry may be
  needed; the closed form should win whenever `)` is present.
- **Regenerate + version bump.** Requires `tree-sitter generate`, a new grammar
  tag, and the `tag` bump in this repo's `Cargo.toml`.
- **Downstream consumers.** `symbols.rs`, `resolve/mod.rs`, `semantic_tokens`,
  `diagnostics.rs` should treat `incomplete_func_call_expr` like
  `func_call_expr` where appropriate. The LSP `signature_help` `ERROR`-node
  fallback could then be dropped or kept as belt-and-suspenders.
- **Fixtures.** Add `tests/fixtures/invalid/` cases for unclosed calls to pin
  the localized-error behaviour.

---

## Part 3 — Design flags (most important)

Ordered by severity.

### 3.1 — Bug: `comma_trail` references an undefined variable

```js
function comma_trail(rule) {
  return delim_trail(rule, del); // `del` is not a parameter and not in scope
}
```

`comma_trail` (grammar.js:852) is **dead code** (never called) and would throw a
`ReferenceError` if it ever were. It should be `delim_trail(rule, ',')` or
deleted outright. (`block_delim(rule, del)` at line 863 looks similar but is
fine — there `del` _is_ a parameter.)

### 3.2 — Smell: arithmetic sign baked into numeric literal tokens

```js
literal_int:   token(seq(optional(choice('+', '-')), repeat1(/[0-9]/)))
literal_float: token(seq(optional(choice('+', '-')), ...))
```

The sign is part of the literal _token_. Unary operators (`unary_op_neg: '-'`,
`unary_op_plus: '+'`) also exist. The result is an **inconsistent AST for
equivalent code**: `return -5;` lexes `-5` as a single `literal_int`, while
`return - 5;` and `return -x;` produce `unary_op_expr(neg, ...)`. Every
consumer (symbols, resolve, semantic tokens, formatter) then has to handle two
shapes for "negated value."

Recommendation: drop the sign from the literal tokens and let `unary_op_expr`
own it. This is the standard approach and removes a whole class of edge cases.
Context-aware lexing currently masks the worst ambiguities (e.g. `5-3`), but the
`return -5` inconsistency is real and reachable.

### 3.3 — Inconsistency: error recovery is a one-off, not a strategy

`incomplete_member_access_expr` is the _only_ explicit recovery rule, yet the
mandatory-closer cascade affects roughly a dozen constructs:

- `func_call_expr` `( )`, `func_params` `( )`, `_annotation_arg` `( )`
- `for_stmt` / `while_stmt` / `if_stmt` / `switch_stmt` / `do_while_stmt` `( )`
- `array_expr` `[ ]`, `nested_expr` / `_paren_ident` `( )`
- `block` / `block_delim` `{ }`
- `type_annot` generic `< >`

Each of these collapses to an `ERROR` node when its closer is missing —
exactly the problem Part 2 describes, repeated. The `incomplete_*` pattern is
the right tree-sitter idiom, but applying it to one rule and nothing else means
error-recovery quality is wildly uneven across the language. Decide on a
**systematic** approach: either add `incomplete_*` variants to every bracketed
construct, or adopt a consistent convention and document it. Half-applied
recovery is worse than none because it is unpredictable.

### 3.4 — Fragility: `prec.dynamic` cast disambiguation

```js
cast_expr: prec.dynamic(
  1,
  prec.right(
    PREC.CAST,
    seq(field("type", $._paren_ident), field("value", $._expr)),
  ),
);
nested_expr: choice(seq("(", $._expr, ")"), $._paren_ident);
```

`(foo)` is genuinely ambiguous between a parenthesised expression and the start
of a cast, hence `prec.dynamic` and the lone declared conflict
`[$._expr, $._paren_ident]`. `prec.dynamic` is tree-sitter's last-resort tool —
it works, but it is the kind of thing that produces surprising parses later.
Also `_paren_ident` is reused for two semantically unrelated things (cast type
and parenthesised ident). At minimum, add a grammar comment explaining _why_
`prec.dynamic` is required here so it is not "cleaned up" by a future editor. If
WitcherScript's real grammar has a cleaner cast syntax, prefer that.

### 3.5 — Minor: `func_call_args` shape and trailing-comma inconsistency

- `func_call_args: choice($._expr, comma2(optional($._expr)))` splits the 1-arg
  and 2+-arg cases needlessly. `func_call_expr` already wraps it in
  `optional(...)` for the 0-arg case, so a single uniform rule (e.g.
  `delim(optional($._expr), ',')`) would cover 0/1/many without the `choice`.
- `func_params` uses `comma(...)` (no trailing comma allowed) while
  `func_call_args` tolerates trailing/empty slots. Pick one tolerance policy and
  apply it consistently.

### 3.6 — Minor: equal precedence for `MEMBER` / `CALL` / `ARRAY`

`PREC.MEMBER`, `PREC.CALL`, `PREC.ARRAY` are all `13`. It generates cleanly
because the rules are token-distinguishable (`.` vs `(` vs `[`), so this is not
currently a bug — but equal precedences among sibling `choice` alternatives are
worth a comment, since any future rule sharing that precedence could surface a
conflict that is hard to trace.

---

## Part 4 — What is good (keep)

- The hidden `_intro` rules (`_enum_decl_intro`, `_func_decl_intro`, etc.) and
  the documented philosophy at lines 138-143 — chopping rules into short
  sequences genuinely improves tree-sitter error recovery. Good, deliberate
  design.
- `nop: ';'` as a named node tolerated in every body — stray semicolons stay in
  the tree instead of erroring. Good.
- The `incomplete_member_access_expr` _pattern itself_ is correct and idiomatic.
  The only issue is that it is not applied consistently (see 3.3).

---

## Suggested order of work

1. Delete or fix `comma_trail` (3.1) — trivial, no parser impact.
2. Add `incomplete_func_call_expr` (Part 2) — highest user-visible payoff.
3. Decide the systematic error-recovery convention (3.3) and roll
   `incomplete_*` variants out across the bracketed constructs.
4. Move literal signs to `unary_op_expr` (3.2) — do this before release; it
   changes tree shapes and every consumer.
5. Tidy `func_call_args` and trailing-comma policy (3.5); add explanatory
   comments for `prec.dynamic` and the shared precedences (3.4, 3.6).

Each grammar change requires `tree-sitter generate`, updated
`tests/fixtures/`, a new grammar tag, and a `Cargo.toml` `tag` bump in this
repo.
