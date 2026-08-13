package render

import (
	"sort"
	"strconv"

	"github.com/thedevdojo/pocketknife/blade/ast"
	"github.com/thedevdojo/pocketknife/blade/expr"
	"github.com/thedevdojo/pocketknife/blade/scanner"
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
		if !r.consumeLoopIteration(node.P) {
			break // budget exhausted: the whole render is aborting
		}
		iteration := int64(i) + 1
		vars := map[string]expr.Value{
			valName: p.val,
			"loop":  loopObject(i, iteration, count),
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

// consumeLoopIteration draws one @foreach iteration from the entry's shared
// budget (renderCtx.loopCap, set from Options.MaxLoopIterations). The first
// draw past the cap emits the single error/render-budget diagnostic — at the
// position of the @foreach that tripped it — and latches ctx.exceeded, which
// renderNodes treats as a full-render abort. Returns false once the budget is
// gone. A renderer with no ctx, or a ctx with no cap (unit-test constructions
// outside Compile), runs unbudgeted.
func (r *renderer) consumeLoopIteration(p scanner.Pos) bool {
	if r.ctx == nil || r.ctx.loopCap <= 0 {
		return true
	}
	if r.ctx.exceeded {
		return false
	}
	if r.ctx.loopUsed >= r.ctx.loopCap {
		r.ctx.exceeded = true
		r.diag(p, KindError, "render-budget",
			"render budget exceeded: more than "+strconv.Itoa(r.ctx.loopCap)+
				" @foreach iterations in one entry; render aborted")
		return false
	}
	r.ctx.loopUsed++
	return true
}

// overBudget reports whether this render's entry has exhausted its loop
// budget; renderNodes checks it after every node so the abort unwinds the
// whole entry, not just the loop that tripped it.
func (r *renderer) overBudget() bool {
	return r.ctx != nil && r.ctx.exceeded
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

// loopObject builds the $loop stdClass for the current iteration. It carries the
// seven meta properties v1 supports, in real Blade's relative order, and marks
// remaining/depth/parent as app-tier members (spec §3.2) so a bare access to one
// reports app-tier — fulfilling the spec's promise now that $loop is an object
// (a missing member on a map used to silently yield nil). As an *Object, an
// access to any OTHER absent member (e.g. $loop->typo) is an undefined-property
// error, matching real Blade's fatal.
func loopObject(i int, iteration, count int64) *expr.Object {
	loop := expr.NewObject()
	loop.Set("iteration", iteration)
	loop.Set("index", int64(i))
	loop.Set("count", count)
	loop.Set("first", i == 0)
	loop.Set("last", int64(i) == count-1)
	loop.Set("odd", iteration%2 == 1)
	loop.Set("even", iteration%2 == 0)
	loop.MarkAppTier("remaining", "depth", "parent")
	return loop
}

// pair is one element of an iterated collection: its PHP key and value.
type pair struct {
	key expr.Value
	val expr.Value
}

// iterablePairs flattens an iterable value into ordered key/value pairs, or
// reports ok=false for a non-iterable (scalar/nil) value. Lists key 0..n-1 as
// int64. An *Object (a JSON object / stdClass) iterates its properties in
// INSERTION order — real Blade's foreach-over-stdClass order — which the decoder
// preserved from the JSON document. A bare map[string]Value has no stored order,
// so its keys iterate SORTED (the vestigial path — protocol data objects, whose
// order was lost at Go's json boundary, become sorted-key *Objects instead).
func iterablePairs(v expr.Value) ([]pair, bool) {
	switch items := v.(type) {
	case []expr.Value:
		pairs := make([]pair, len(items))
		for i, e := range items {
			pairs[i] = pair{key: int64(i), val: e}
		}
		return pairs, true
	case *expr.Object:
		keys := items.Keys()
		pairs := make([]pair, len(keys))
		for i, k := range keys {
			v, _ := items.Get(k)
			pairs[i] = pair{key: k, val: v}
		}
		return pairs, true
	case *expr.Arr:
		// An ordered array literal (['a', 'k' => $v]) iterates its entries in
		// insertion order with their PHP keys — exactly PHP's foreach.
		ps := items.Pairs()
		pairs := make([]pair, len(ps))
		for i, p := range ps {
			pairs[i] = pair{key: p.Key, val: p.Val}
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
