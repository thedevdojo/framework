package render

import (
	"sort"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/expr"
)

// loopSignal is the non-local control state a @break/@continue raises inside a
// loop body. renderNodes stops walking a body as soon as one is set, letting it
// bubble up through nested @if branches to the innermost @foreach, which
// consumes it.
type loopSignal int

const (
	sigNone loopSignal = iota
	sigContinue
	sigBreak
)

// renderForeach evaluates the items expression and renders the body once per
// element, layering a per-iteration child scope (the value var, optional key
// var, and $loop) over the enclosing scope. A non-iterable value diagnoses
// error/foreach and renders nothing. The parent scope is restored afterward, so
// loop bindings do NOT leak past @endforeach — a deliberate v1 divergence from
// PHP's leaking foreach (see the task report).
func (r *renderer) renderForeach(node *ast.Foreach) {
	items, err := expr.Eval(node.ItemsExpr, r.scope)
	if err != nil {
		r.diagExprError(err, node.P)
		return
	}
	pairs, ok := iterablePairs(items)
	if !ok {
		r.diag(node.P, KindError, "foreach", "cannot iterate over a non-array value")
		return
	}

	valName := trimDollar(node.ValVar)
	keyName := trimDollar(node.KeyVar)
	count := int64(len(pairs))

	parent := r.scope
	r.loopDepth++
	for i, p := range pairs {
		iteration := int64(i) + 1
		vars := map[string]expr.Value{
			valName: p.val,
			"loop": map[string]expr.Value{
				"index":     int64(i),
				"iteration": iteration,
				"first":     i == 0,
				"last":      int64(i) == count-1,
				"count":     count,
				"even":      iteration%2 == 0,
				"odd":       iteration%2 == 1,
			},
		}
		if node.KeyVar != "" {
			vars[keyName] = p.key
		}
		r.scope = &loopScope{vars: vars, parent: parent}

		r.renderNodes(node.Body, directiveEatsNewline)

		sig := r.signal
		r.signal = sigNone
		if sig == sigBreak {
			break
		}
		// sigContinue and sigNone both fall through to the next iteration.
	}
	r.loopDepth--
	r.scope = parent
}

// renderLoopJump handles @break/@continue. With no active loop (a parser edge
// case: a dropped malformed @foreach, or a jump inside a component body) it
// diagnoses error/loop-jump and renders nothing. Otherwise an absent or truthy
// condition raises the signal; a condition that fails to evaluate is diagnosed
// and treated FALSE (no jump). Either way the directive eats one newline.
func (r *renderer) renderLoopJump(node *ast.LoopJump) bool {
	if r.loopDepth == 0 {
		r.diag(node.P, KindError, "loop-jump", "@break/@continue outside a @foreach")
		return directiveEatsNewline
	}

	fire := true
	if node.Cond != "" {
		cond, err := expr.Eval(node.Cond, r.scope)
		if err != nil {
			r.diagExprError(err, node.P)
			fire = false // condition error → treated false, no jump
		} else {
			fire = expr.Truthy(cond)
		}
	}
	if fire {
		if node.Continue {
			r.signal = sigContinue
		} else {
			r.signal = sigBreak
		}
	}
	return directiveEatsNewline
}

// pair is one element of an iterated collection: its PHP key and value.
type pair struct {
	key expr.Value
	val expr.Value
}

// iterablePairs flattens an iterable value into ordered key/value pairs, or
// reports ok=false for a non-iterable (scalar/nil) value. Lists key 0..n-1 as
// int64. Maps are UNORDERED in Go (a JSON object decodes to one), so their keys
// iterate in SORTED order — a documented v1 decision; JSON arrays are the
// primary collection shape and keep their natural order.
func iterablePairs(v expr.Value) ([]pair, bool) {
	switch items := v.(type) {
	case []expr.Value:
		pairs := make([]pair, len(items))
		for i, e := range items {
			pairs[i] = pair{key: int64(i), val: e}
		}
		return pairs, true
	case map[string]expr.Value:
		keys := make([]string, 0, len(items))
		for k := range items {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]pair, len(keys))
		for i, k := range keys {
			pairs[i] = pair{key: k, val: items[k]}
		}
		return pairs, true
	default:
		return nil, false
	}
}

// trimDollar drops a leading '$' from a loop variable name (the parser stores
// ValVar/KeyVar with the sigil; Scope.Lookup keys are sigil-less).
func trimDollar(name string) string {
	if len(name) > 0 && name[0] == '$' {
		return name[1:]
	}
	return name
}

// loopScope layers per-iteration loop bindings over a parent scope: a lookup
// resolves against the loop-local vars first (so the value/key vars and $loop
// shadow anything outside), then falls through to the enclosing scope. Nested
// loops therefore see outer variables while $loop always names the innermost
// loop. A fresh loopScope per iteration is what makes body expressions
// re-evaluate against the current item rather than a stale snapshot.
type loopScope struct {
	vars   map[string]expr.Value
	parent expr.Scope
}

func (s *loopScope) Lookup(name string) (expr.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	return s.parent.Lookup(name)
}
