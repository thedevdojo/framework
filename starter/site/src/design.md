# Starter — design brief

## Atmosphere

A neutral, light starter: a near-white page, hairlines instead of boxes, soft black for the actions that
matter, and Inter for everything so the sections read as one kit. Eight pages — Home, Features, Pricing with
a plan comparison table, Customers with a page per story, Changelog, a Blog with a page per post, About and
Contact — are compositions of general-purpose sections that share one type scale, one radius scale and one
depth recipe. Nothing shouts; the product screenshots and the copy do the selling.

## Colors

Declared once in `src/css/site.css` (`:root` + `@theme`); markup never writes a hex.

| Role | Token | Value |
|---|---|---|
| Page ground | `--background` | `#fbfbfb` near-white |
| Ink (text, primary buttons, selection) | `--foreground` / `--primary` | `#0a0a0a` soft black, never `#000` |
| Cards, popovers, windows, the pressed toggle pill | `--card` | `#ffffff` |
| Hover fills, wells, stages, the toggle track | `--muted` / `--secondary` | `#f2f2f2` |
| Supporting copy | `--muted-foreground` | `#6b6b6b` |
| The one accent (titles on hover, focus ring, the glow behind the dark band) | `--accent` | `#1d4ed8` blue, budgeted |
| Accent tint (focus halo) | `--color-accent-soft` | accent at 8% |
| Hairlines | `--border` / `--input` | ink at 8%; `--color-border-strong` ink at 16% for outlined buttons and window frames |
| Stars, an upward delta | `--color-star` / `--color-up` | `#fbbf24` / `#059669` |
| The dark counterpart (closing band, terminal) | `--color-shade*` | ink ground, near-white text, 12% hairlines |

Text has four tiers from two variables: `text-foreground`, `text-foreground/78` (body copy, nav labels),
`text-muted-foreground` (supporting copy), `text-muted-foreground/80` (metadata, icons at rest). Two-line
headlines put the second line in the faint tier (`text-muted-foreground/80 first-line:text-foreground`).

## Typography

- `--font-display` and `--font-sans` — **Inter** 400/500/600/700 (Google Fonts, loaded in the layout). Headings `font-semibold`, body 400, labels and buttons `font-medium`.
- `--font-mono` — **JetBrains Mono** 400/500: eyebrows (`font-mono text-[11px] tracking-widest uppercase`), version numbers, the FAQ numbers, step chips, code inside `.prose`.
- Scale: `text-hero` clamp(2.75rem → 4.75rem) / 1.02 / −0.04em; `text-h2` clamp(1.875rem → 2.5rem) / 1.1 / tracking-tight;
  `text-figure` clamp(2.25rem → 3.25rem) / 1 for big numbers; post and story titles `text-[2.5rem]/[1.08]` → `sm:text-[3.25rem]/[1.05]`;
  card titles `text-lg/7`–`text-xl` semibold; ledes `text-lg/8` (`sm:text-xl/8`); body `text-[17px]/7 text-foreground/78`;
  supporting `text-[15px]/6` and `text-[14px]/6 text-muted-foreground`; metadata 12–13px; long-form `.prose` 17px / 1.75.
- Headings `text-balance`, paragraphs `text-pretty`, figures `tabular-nums`; measures capped with `max-w-[46ch]`–`max-w-[62ch]`.

## Spacing & Radius

- Container `mx-auto w-full max-w-6xl px-6 lg:px-8`. Sections `py-16 sm:py-28` (quieter ones `py-20 sm:py-28`), openers `pt-16 sm:pt-24`; `<main>` divides sections with `divide-y divide-border`, never with a background change.
- Card grids `gap-5`; article grids `gap-x-8 gap-y-12`–`gap-y-14`; splits `grid lg:grid-cols-12 gap-8 lg:gap-12` (7/5) or `lg:grid-cols-2 lg:gap-20`.
- Radius: `--radius` 0.75rem (`rounded-xl`: controls, small cards), 1rem (`rounded-2xl`: panels, forms, the FAQ box), 1.5rem (`rounded-3xl`: framed images, quote wells, plan cards); `rounded-control` (= `--radius`) on every text button; chips `rounded-full`.
- One depth recipe: `--shadow-card` (a hairline plus a soft layered shadow) on `.lift` cards; windows `border border-border bg-card shadow-2xl shadow-foreground/5` on a `bg-muted` stage; photographs `outline-1 -outline-offset-1 outline-foreground/10`.

## Components

- **Buttons:** primary `rounded-control bg-primary px-6 py-3.5 text-[15px] font-medium text-primary-foreground hover:opacity-85 active:scale-[.98]` (the hero's adds a lift and `shadow-lg shadow-black/10`); secondary `border border-border bg-card hover:border-border-strong hover:bg-muted` or `border border-border-strong hover:bg-muted`; `.arrow-link` nudges its arrow 3px.
- **Nav:** sticky `bg-background/85 backdrop-blur-md` bar, hover-intent dropdowns (`[data-dropdown]`), a mobile sheet under the bar, `aria-current` set by `main.js`; the mark launches up on load.
- **Windows and stages:** a screenshot in `rounded-[20px] border border-border bg-card shadow-2xl` on a `rounded-3xl bg-muted` stage; feature rows run the stage off the outer edge on desktop.
- **Cards:** quote and outcome cards `rounded-3xl bg-muted/70 p-8`; plan cards `rounded-3xl border border-border bg-card p-8`, the featured one `border-primary ring-1 ring-primary` with an uppercase badge; article cards are a framed image, a chip row, and a title that turns `text-accent` on hover.
- **Chips:** `rounded-full border border-border bg-card px-2.5 py-0.5 text-[12px] font-medium`; the solid chip `bg-primary text-primary-foreground`; the savings pill `bg-up/10 text-up`.
- **Lists and rails:** FAQ `<details class="faq-item">` rows in one bordered box; the changelog's two-column rail with a sticky date and version; the comparison table (sticky feature column, tinted featured column, more rows revealed in place); `divide-y divide-border` link rows; stat rows `sm:divide-x sm:divide-border`.
- **Forms:** `.field` inputs, `rounded-lg`/`rounded-control border border-input`, accent-soft focus halo; forms are static markup to wire up.
- **The dark band:** `rounded-[2.5rem] bg-shade text-shade-foreground` with a dot grid and one accent glow.
- **Motion:** `data-reveal` + `.reveal-1…6` (blur-and-rise on `--ease-spring`), `.lift`, the pricing toggle (`data-billing`), `<details>` interpolate-size, view-transition fades; reduced motion shows everything at rest. Pages swap in place (`data-instant-navigation`); every per-page behaviour runs inside `setUp(root)` in `public/js/main.js`, re-run on `instant:navigated`.

## Voice

Plain and specific, written by a six-person company in Columbus for developers and the editors they work with.
Short declaratives, concrete numbers (2,140 sites, 91% fewer tickets, 38 ms), named customers and places.
Headlines state the thing ("Edit the page you are looking at"); supporting lines explain the mechanism. No
exclamation marks, no "seamless"/"powerful"/"unlock", no fake urgency. Posts and stories speak in the first
person plural.

## Anti-patterns

- A second accent, gradients, pure `#000`, coloured section backgrounds (the dark band and `bg-muted` wells are the only breaks from the page).
- Hex values or font names in markup; a shadow other than `shadow-card` or the window recipe; heavy borders (hairlines only).
- Uppercase display headings, centred body copy outside centred sections, decorative icons in coloured circles.
- Screenshots not framed as windows; content hidden behind JavaScript; a second navigation runtime (no htmx, Alpine or Turbo).
- Anything outside what DevDojo Framework reads in a view: no ternaries, function calls, string concatenation or arithmetic — derived values live in the data (`anchor`, `groupStart`, plain lists for `features` and `points`) or in `main.js`.

## Tokens

```css
@theme inline {
    --color-background: var(--background);
    --color-foreground: var(--foreground);
    --color-card: var(--card);
    --color-card-foreground: var(--card-foreground);
    --color-popover: var(--popover);
    --color-popover-foreground: var(--popover-foreground);
    --color-primary: var(--primary);
    --color-primary-foreground: var(--primary-foreground);
    --color-secondary: var(--secondary);
    --color-secondary-foreground: var(--secondary-foreground);
    --color-muted: var(--muted);
    --color-muted-foreground: var(--muted-foreground);
    --color-accent: var(--accent);
    --color-accent-foreground: var(--accent-foreground);
    --color-destructive: var(--destructive);
    --color-border: var(--border);
    --color-input: var(--input);
    --color-ring: var(--ring);

    /* Tailwind's own rounded-* scale, derived from the three radius variables */
    --radius-sm: calc(var(--radius) - 0.5rem);
    --radius-md: calc(var(--radius) - 0.375rem);
    --radius-lg: calc(var(--radius) - 0.25rem);
    --radius-xl: var(--radius);
    --radius-2xl: var(--radius-medium);
    --radius-3xl: var(--radius-large);
    --radius-medium: var(--radius-medium);
    --radius-large: var(--radius-large);
    --radius-control: var(--radius-control);
}

:root {
    /* Surfaces: a near-white page, true white for cards, a toasted step for hover fills and wells */
    --background: #fbfbfb;
    --foreground: #0a0a0a;
    --card: #ffffff;
    --card-foreground: #0a0a0a;
    --popover: #ffffff;
    --popover-foreground: #0a0a0a;

    /* Actions: primary buttons are soft black; the accent is the one blue, budgeted */
    --primary: #0a0a0a;
    --primary-foreground: #fbfbfb;
    --secondary: #f2f2f2;
    --secondary-foreground: #0a0a0a;
    --muted: #f2f2f2;
    --muted-foreground: #6b6b6b;
    --accent: #1d4ed8;
    --accent-foreground: #fbfbfb;
    --destructive: #e7000b;

    /* Hairlines: low-alpha ink, not a grey. The focus ring is the accent */
    --border: rgba(10, 10, 10, 0.08);
    --input: rgba(10, 10, 10, 0.08);
    --ring: #1d4ed8;

    /* Corners: controls and cards (rounded-xl), panels (rounded-2xl), framed images (rounded-3xl);
       rounded-control is every text button, so one variable can square or pill them all */
    --radius: 0.75rem;
    --radius-medium: 1rem;
    --radius-large: 1.5rem;
    --radius-control: var(--radius);
}

/* ============================================================================
   Starter — this template's own tokens
   ========================================================================== */
@theme {
    --font-display: 'Inter', ui-sans-serif, system-ui, sans-serif;
    --font-sans: 'Inter', ui-sans-serif, system-ui, sans-serif;
    --font-mono: 'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace;

    /* Type scale: the hero, section headings, and the big figures */
    --text-hero: clamp(2.75rem, 1.5rem + 4.4vw, 4.75rem);
    --text-hero--line-height: 1.02;
    --text-h2: clamp(1.875rem, 1.35rem + 1.9vw, 2.5rem);
    --text-h2--line-height: 1.1;
    --text-figure: clamp(2.25rem, 1.75rem + 1.8vw, 3.25rem);
    --text-figure--line-height: 1;

    /* A stronger hairline for outlined buttons and window frames; a tinted well behind accent content */
    --color-border-strong: color-mix(in srgb, var(--foreground) 16%, transparent);
    --color-accent-soft: color-mix(in srgb, var(--accent) 8%, transparent);

    /* Two named colours the drawn UI needs: star ratings and an upward delta */
    --color-star: #fbbf24;
    --color-up: #059669;

    /* The dark counterpart of the page: the closing panel, the terminal, one dark band */
    --color-shade: var(--foreground);
    --color-shade-foreground: var(--background);
    --color-shade-muted-foreground: color-mix(in srgb, var(--background) 63%, var(--foreground));
    --color-shade-border: color-mix(in srgb, var(--background) 12%, transparent);

    /* One depth recipe page-wide: a hairline plus this soft layered shadow */
    --shadow-card: 0 1px 2px rgba(10, 10, 10, 0.05), 0 12px 32px -16px rgba(10, 10, 10, 0.18);

    /* One easing family for everything interactive: fast in, long settle. */
    --ease-out-quart: cubic-bezier(0.25, 1, 0.5, 1);
    --ease-spring: cubic-bezier(0.16, 1, 0.3, 1);
}
```
