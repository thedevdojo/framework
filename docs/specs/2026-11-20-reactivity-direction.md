# DevDojo Framework: HTML-first components and optional reactivity

**Status:** Direction, written down so we know where we are going. **Nothing
here is built, and none of it is scheduled yet.** It is not a specification.
**Date:** November 20, 2026

Sections 1 to 11 are the direction as Tony wrote it, brought in line with the
decisions below. §12 sets it against the engine as it stands today and names
what is still open before a spec is written.

## Decided (Tony, 2026-11-20)

1. **A value is printed with single braces in an `.html` file:**
   `{post.title}`. Today the engine reads `{{ post.title }}`; that keeps
   working for a while, with a notice (§12.2.1 has the rules).
2. **A live value is `@{count}` and an event is `@click`.** In an `.html`
   file, `@` means "this happens in the browser". Plain `{title}` is filled
   in when the page is built.
3. **A literal brace is written `\{`.** `\{title}` prints `{title}`.
   (Proposed by the reviewer, since `@{` is taken by 2.)
4. **Two syntaxes stay.** `.html` is the one people write: `<Foreach>`,
   `<If>`, `{post.title}`. Blade (`.blade.php`: `@foreach`, `@if`,
   `{{ $post->title }}`) stays in the engine as something a site can always
   be turned into, and as the proof that the engine is right.
   `devdojo convert --to blade` and `--to studio` already do both directions.

Not decided: props in YAML (§12.2.3), reactivity in a Blade file (§12.2.5),
and the single-brace spelling of raw HTML, which is `{!! x !!}` today.

Related: [`2026-11-20-framework-export-design.md`](2026-11-20-framework-export-design.md)
(export to React, Vue, Svelte, Alpine and Blade, and the `<Script>` tag). Its
§12 says declared state is "later, in its own spec". This page is the start of
that.

---

## 1. Vision

DevDojo is an HTML-first component framework and compiler focused initially on
marketing websites. The authoring format should remain simple, while exports
should generate idiomatic components for React, Vue, Svelte, Alpine where
appropriate, and Blade.

- **YAML:** Public component props, types, defaults, and metadata.
- **DevDojo HTML:** Markup, component composition, compile-time template loops
  and conditionals, prop interpolation, and lightweight interaction bindings.
- **Optional `<Script>` block:** Component-local reactive state and advanced
  JavaScript behavior.
- **Compiler:** Produces HTML/CSS/JS and framework-specific source; bundles
  scripts automatically.

The DevDojo app is the editor experience; DevDojo Framework is the
component/rendering foundation. The reactive layer is an optional enhancement,
not a second full application framework.

## 2. Key decisions and current proposals

| Area | Proposed direction |
| --- | --- |
| Component props | Defined once in the component's associated `.yaml` file |
| Prop defaults | Defined in YAML, not duplicated in HTML |
| `<Props>` tag | Remove; no duplicate prop declarations |
| Passing props | Parent component passes attributes; explicit values override YAML defaults |
| Compile-time interpolation | Single braces, e.g. `{title}` or `{site.name}`. Decided; the engine reads `{{ title }}` today (§12.2.1) |
| Compile-time structure | Keep the existing loops and conditions (`<Foreach>` / `<If>` in an `.html` file, `@foreach` / `@if` in Blade) and preserve their structure for exports |
| Local reactive state | Declare inside an optional `<Script>` block using a compiler-recognized `state()` primitive |
| Reactive text | `@{count}` |
| Events | `@click="count += 1"` |
| State scope | One independent state instance per component instance |
| Browser runtime | Generate only the necessary JavaScript; no virtual DOM required |
| Shared/global state | Out of scope for v1 |
| Reactive loops | Out of scope for v1; use existing template loops |
| Complex interactions | Keep optional component scripts as an escape hatch |
| A literal brace | `\{` |

**Important:** These are design preferences discussed in conversation, not
confirmation that the current DevDojo parser supports them. §12 says which
ones it does not.

## 3. Minimal counter example

### `counter.yaml`

```yaml
props:
  title:
    type: string
    default: Counter
```

### `counter.html` (proposed syntax)

```html
<div>
    <h2>{title}</h2>
    <p>@{count}</p>
    <button @click="count += 1">Increment</button>
</div>

<Script>
    let count = state(0);
</Script>
```

Expected behavior:

1. `{title}` is rendered by the DevDojo template compiler.
2. `state(0)` declares local reactive state.
3. `@{count}` renders the current count and updates when it changes.
4. `@click` handles the event and updates state.
5. Two copies of the counter on one page must not share the same `count`.

The `<Script>` element is intended as a **DevDojo compiler directive**, not a
browser-executed HTML script element. The compiler transforms/bundles its
contents.

## 4. Why keep state in `<Script>` instead of YAML?

An earlier option was to declare `state` in YAML, or via a `<State>` tag. The
more recent preference is:

- YAML defines the **public contract**: props, defaults, metadata.
- `<Script>` defines **private mutable state and behavior**.
- HTML consumes props and binds to reactive state.

This keeps state and advanced logic close to the markup, avoids competing
declarations, and makes complex state values/functions easier than HTML
attributes or YAML configuration.

For simple components without interactivity, omit `<Script>` entirely.

## 5. Compile-time vs runtime behavior

**Compile-time:**

- Existing DevDojo template loops and conditions: `<Foreach>` and `<If>` in an
  `.html` file, `@foreach` and `@if` in Blade.
- Prop expressions such as `{title}`.
- Component composition and template expansion.

**Runtime:**

- `@{count}` and other reactive expressions.
- `@click` and other events.
- Reactive text, form values, visibility, and dynamic classes/attributes, if
  implemented.

**`@` means the browser.** An `.html` file has no compile-time directives:
its loops and conditions are tags. So in an `.html` file `@` can mean one
thing only, "this happens in the browser" (`@{count}`, `@click`). Blade is
the syntax where `@` is compile-time (`@if`, `@foreach`); how a Blade file
spells a runtime binding is open (§12.2.5).

**Parser consideration:** `@{...}` must be parsed as its own form before any
general single-brace interpolation handling. The compiler must not leave the
expression as literal browser text.

## 6. Export behavior

Preserve a structured representation of the original template and its
expressions. Do **not** first flatten loops into HTML and then attempt to
reconstruct them for framework exports.

| DevDojo concept | React | Vue | Svelte |
| --- | --- | --- | --- |
| Props | Function props / typed interface | `defineProps` | `$props()` (Svelte 5) |
| Local state | `useState` | `ref` / `reactive` | `$state` |
| Template loop | `.map()` | `v-for` | `{#each}` |
| Template condition | JSX conditional | `v-if` | `{#if}` |
| Event | `onClick` and state setter | `@click` | `onclick` (Svelte 5) |
| Reactive output | JSX expression | Interpolation | Expression |

Example React target for the counter:

```jsx
import { useState } from 'react';

export default function Counter({ title = 'Counter' }) {
    const [count, setCount] = useState(0);

    return (
        <div>
            <h2>{title}</h2>
            <p>{count}</p>
            <button onClick={() => setCount(c => c + 1)}>
                Increment
            </button>
        </div>
    );
}
```

The compiler must understand `count += 1` as a state update, not blindly copy
it into React, where mutating a local variable does not update state.

**Other targets:** Vue and Svelte should receive idiomatic state/event syntax.
Blade can render server-side structure, but browser interactions still require
client JavaScript (possibly DevDojo's generated runtime or Alpine). Arbitrary
advanced scripts cannot be guaranteed to convert automatically into idiomatic
code.

## 7. V1 scope: solve real marketing-site needs

Initially, prioritize native HTML and CSS whenever practical:

- Responsive navigation can sometimes use `<details>` or CSS.
- Accordions can often use `<details>`.
- Hover effects and many animations need no reactive runtime.

Use reactive state only where it adds real value:

- Pricing-period toggles
- Tabs and modals
- Form input feedback and conditional fields
- Active carousel index
- Simple calculators

Potential v1 primitives:

1. Component-local `state(initialValue)`.
2. Event bindings such as `@click`.
3. Reactive text via `@{expression}`.
4. Reactive input/value binding.
5. Reactive visibility and dynamic class/attribute binding.

**Avoid initially:** global stores, context/reducers, reactive loops, virtual
DOM, and broad React-style lifecycle/effect systems.

A reasonable implementation strategy is to build real components first and add
the smallest repeated primitives, rather than committing to a large reactive
API prematurely.

## 8. Optional advanced JavaScript

Keep `<Script>` available for behavior beyond simple inline state updates:

- Form submission and API requests
- Payment SDK integrations
- Map/library initialization
- Timers and automatic carousels
- Event listeners and cleanup

These require a defined lifecycle and cleanup contract. They may need
framework-specific adapters or explicit non-portable fallbacks on export.

## 9. Technical issues to resolve

1. **Interpolation syntax.** Settled: single braces for compile-time output,
   `@{value}` for runtime output, `\{` for a literal brace. The engine reads
   `{{ value }}` today; §12.2.1 says how it gets from one to the other.
2. **Define valid reactive expressions.** Decide which
   assignments/operators/functions are supported and parsed for framework
   export. Avoid promising arbitrary JS-to-React conversion.
3. **Define scope and mounting.** State must be isolated per component
   instance, even when scripts share one output bundle.
4. **Define event parsing.** `@click` is a source-level directive processed by
   DevDojo; the compiler must produce valid browser event wiring.
5. **Define dynamic binding syntax.** Need a clear approach for `class`,
   `disabled`, `value`, input two-way binding, and conditional display.
6. **Define lifecycle and cleanup.** Especially for timers, subscriptions, and
   third-party SDKs.
7. **Define security boundaries.** Do not evaluate untrusted expressions or
   interpolate unsanitized values into HTML; escape outputs and apply
   appropriate content security rules.
8. **Define compilation/export IR.** Preserve AST-level loops, conditions,
   props, state, events, and expressions.
9. **Define error behavior.** Missing props/state, invalid expressions, and
   unsupported exports should produce actionable compiler diagnostics.
10. **Define optimization.** Static components should not load the reactive
    runtime; shared helpers should be included only when needed.

## 10. Recommended next prototype

Implement three proof-of-concept components end-to-end:

1. **Counter:** `state(0)`, `@{count}`, `@click`.
2. **Pricing toggle:** local boolean state, reactive prices, and dynamic
   classes.
3. **Mobile menu or modal:** visibility binding, accessible focus/keyboard
   behavior, and cleanup.

For each, generate:

- Standalone HTML + minimal JavaScript
- React
- Vue
- Svelte

Test repeated component instances, initial rendering, state updates, and
equivalent behavior across exports.

## 11. Guiding principle

**DevDojo Framework defines HTML-first components; YAML defines public props;
optional `<Script>` defines local behavior; the compiler generates minimal
browser code or idiomatic framework exports.**

Keep the simplest marketing component entirely static, while allowing
incremental interactivity without requiring a full frontend framework.

---

## 12. Set against the engine as it is today (review, 2026-11-20)

Written by reading the engine (1.1.0), `docs/html-syntax.md` and the export
design. Each point is either "this already agrees" or "this has to be decided
before a spec", with a recommendation. None of them is decided.

### 12.1 What already agrees

- **Exports read the tree, never the rendered HTML.** The export design's §5.4
  maps every construct (`<Foreach>` to `.map()` / `v-for` / `{#each}`, and so
  on) from the tree the engine already builds. §6 here asks for the same.
- **One copy, one state.** The export design's `<Script>` (§7) already runs
  once for each copy of a block, with that copy's `root` and a `signal` that
  fires when the copy goes away. Per-copy state sits on top of that with no
  new idea.
- **`<Script>` is a compiler tag.** It is lifted out when the file is read and
  never printed where it is written. A lowercase `<script>` keeps meaning what
  it means today.
- **Declared state was already the plan.** The export design's §12: to print
  `useState`, `ref` and `$state`, state must be declared where the compiler
  can read it, and the direction is "DevDojo's own small layer on top of
  `<Script>`, not Alpine". `state()` is that layer.
- **Native HTML and CSS first.** The catalog agrees: of the 40 library
  sections that use Alpine, 54 uses are `{ hover: false }`, which is plain
  CSS. What is left is on/off toggles, three pricing switches, one tab set and
  one pager. §10's three prototypes are the right three.
- **Right, or refused.** Export already refuses what it cannot print
  correctly, with a `file:line:col` message. §9.2 and §9.9 are that rule.
- **A static block loads nothing.** Already true, and it stays true.

### 12.2 What does not match today, and has to be decided

**1. Single braces: decided, and what it takes.**
The engine reads `{{ title }}` today. Single braces were turned down when the
HTML syntax was designed (`docs/html-syntax.md`, "Not built yet") because
they collide with CSS, JavaScript and Alpine in the same file. The decision
now is to take them anyway and say exactly where a brace is read. Svelte and
JSX work this way. It is cheap **now**: the HTML syntax is days old, the 79
templates reach it through the converter (so its printer changes them all at
once), and 222 files are written in it by hand (the component library).

The rules:

- A `{` is read in the text and the attribute values of an `.html` view:
  `<h2>{title}</h2>`, `href="{link.url}"`, `class="btn btn-{variant}"`,
  `style="height: {height}px"`.
- It is **never** read inside `<style>`, `<script>`, `<Script>` or an HTML
  comment, nor in an attribute that holds code: `on…` handlers and, in a file
  that declares no state, Alpine's and Vue's (`x-…`, `:name`, `@name`).
- It counts only when what is inside is an expression of the engine's
  grammar. `{post.title}` prints. `{ open: false }` and `{}` are text, left
  as written, with no error. A lint says so when text looks like a near miss.
- `\{` prints a brace: `\{title}` is the text `{title}`. (`&#123;` does too,
  and needs nothing from the engine.)
- `{{ title }}` is still read, with a notice (as `resources/` is after
  `src/`), and `devdojo migrate` rewrites it. `@{{ x }}`, the old escape,
  goes with it.
- Raw HTML stays `{!! x !!}` until its spelling is decided. It has to be
  recognised before `{!x}`, which is "not x".
- A Blade file is untouched: `{{ $post->title }}`. The converter maps one to
  the other, and a literal `{title}` in a Blade file becomes `\{title}` in
  `.html`.
- Exports get simpler: Svelte and React print `{post.title}` as written.

Where it has to be learned: the Go scanner; its two mirrors (PHP in the
platform, JavaScript in the editor); how the platform writes literal markup
into an `.html` file; the converter's printer; what the AI is taught; the 222
library files. And it needs an engine version of its own, because an older
engine prints `{title}` as text and says nothing, which the platform must
refuse (as it does for `.html` views and an engine older than 1.0.0).

**2. Loops and conditions: nothing to change.**
In an `.html` file they are `<Foreach>` and `<If>`; `@foreach` and `@if` are
Blade. Both stay (decision 4). `devdojo convert --to blade` already turns
`<Foreach posts="post">` into `@foreach`, and the platform's checks keep
treating `@if (` inside an `.html` file as a mistake a model made.

**3. The engine does not read the `.yml` file. Props live in `<Props>`.**
Today `<Props title="Untitled" />` (Blade: `@props`) is the only place the
engine learns a prop and its default. The `.yml` beside a component is read
by the platform's editor, for the inspector: field types, labels, options. Its
shape is `fields:`, not `props:`. The catalog holds about 1,600 component
files written this way, and customers' sites more.

Making YAML the contract means:

- the Go engine **and** the Laravel package both parse YAML and agree on every
  default, byte for byte;
- real Laravel only knows `@props`, so a Blade file still needs them (written
  by hand, or generated from the YAML);
- `props: { title: { type, default } }` would be a second schema beside
  `fields:`, unless `fields:` is the one that grows;
- every existing component keeps working, so `<Props>` stays readable for a
  long time (the way `resources/` is still read after `src/`).

*Recommendation:* this is its own decision and its own phase, and reactivity
does not need it. If one home for props is wanted, grow the `fields:` file
the editor already reads, keep `<Props>` as the fallback with a notice, and
do not introduce a second YAML shape.

**4. `@click` and `@{…}`: decided, with one guard.**
In an `.html` file `@` is plain text today, on purpose, so that Alpine's
`@click`, CSS `@media` and e-mail addresses need no escaping. So
`@click="open = true"` reaches the browser as written, and Alpine acts on
it. Forty library sections and any Alpine a customer pasted depend on that,
and the export design lists "any change to what an existing site renders" as
a failure.

*The guard:* `@{…}` and `@name="…"` mean something **only in a file whose
`<Script>` declares state**. A file with no `state()` is read exactly as
today. In a file that does declare state, `@{count}` for a name that is not
state is an error with a `file:line:col`, never silent text. Before release,
the engine's corpus check runs over real sites, as the export design does
for `<Script>`.

`@{{ x }}` (today's escape for a literal `{{ x }}`) is read while `{{ }}` is,
and is told apart by its second brace.

**5. Every construct needs a Blade twin, or a rule that it has none.**
The engine's promise is that the Go tier and real Laravel print the same
bytes. `<Script>` already has its Blade spelling (`@blockscript`). In Blade
`@click` is written `@@click`, and real Laravel would print `@{count}` as
text.
*Recommendation:* decide early whether v1 reactivity is `.html` only (and
`devdojo convert --to blade` refuses a file that uses it) or whether the
Laravel package learns it too. `.html` only is much less work.

**6. `state()` has to fit the `<Script>` that is already designed.**
That design gives a script two names (`root`, `signal`), keeps state in the
DOM, and hands it no props. `state()` is a third name. To be decided:

- State is declared at the top of the script as `let name = state(<literal>)`,
  so the compiler can read it without running JavaScript.
- The first value is a literal, so both tiers print the first render into the
  HTML (`<p>0</p>`). No flash, and a crawler sees the page.
- Whether a prop may seed state (`state(period)`), since scripts are not
  handed props today.
- How a hand-written listener in the same script reads and sets state.

**7. Expressions: the engine's own grammar, plus assignment.**
The engine already has a small expression language (`post.title`,
`a ?? 'b'`, `!x`, `&&`, `||`, comparisons), evaluated by PHP's rules.
*Recommendation:* an event may hold that grammar plus `=`, `+=`, `-=` and a
call to a function the `<Script>` declares. Anything else is refused. No
JavaScript parser goes into the engine, and nothing is evaluated from a
string in the browser (§9.7).

**8. "Bundles scripts automatically."**
The engine prints each block's script once per page and does not minify,
because real Laravel does not, and the two tiers must agree. Bundling and
minifying, if wanted, are a step at publish (export design §7.5).

**9. The cost that is easy to miss.**
Any new spelling is learned in more than one place: the Go scanner, the two
mirrors of it (PHP in the platform, JavaScript in the editor), the converter
between syntaxes, the editor's element numbering, and what the AI is taught
about the syntax. Fewer new spellings is cheaper everywhere.

### 12.3 Order

1. Export of plain blocks (engine 1.2.0, export design).
2. `<Script>` with `root` and `signal` (engine 1.3.0, export design).
3. Declared state: this page, turned into a spec, built on 2. The three
   prototypes of §10 come first and decide the primitives.
4. Props in YAML (12.2.3), if wanted, whenever it suits. It depends on none
   of the above.

Single braces (12.2.1) depend on none of it either, and are cheapest done
soon, before more `.html` files exist. They fit beside the move to `src/`,
which already has a `devdojo migrate` that rewrites a site.
