# Molejo Testkit branding contract

The Testkit browser interface is a Molejo-branded technical fixture. It uses the
product name `Molejo Testkit` in page metadata and documentation, while the
official Molejo logo is displayed separately from the `Testkit` product suffix.

## Logo

The current dark interface uses:

- `static/brand/molejo-horizontal-on-dark.webp` on desktop.
- `static/brand/molejo-symbol-on-dark.webp` on narrow screens.

These are the official 420×90 horizontal and 180×99 symbol Molejo logos. They are
embedded in the binary with the rest of the browser assets, so the interface must
not load a logo from a remote URL. The source references are the corresponding
Molejo Console assets at `apps/console-web/public/brand/`.

Use the logo on the dark Testkit surface with alternative text `Molejo`. Keep
`Testkit` as the adjacent product descriptor. If the interface gains a light
surface, add and use the corresponding official `on-light` variant rather than
recreating the mark in CSS.

## Color

`static/style.css` keeps the dark layout but consumes these Molejo brand
primitives through semantic roles:

| Primitive | Value | Use |
| --- | --- | --- |
| Petroleum | `#12333f` | Main dark surface |
| Petroleum strong | `#0b252e` | Canvas and deepest surface |
| Graphite | `#172126` | Raised surface and dark text |
| Teal | `#0e8f82` | Supporting accent and active surfaces |
| Teal strong | `#087267` | Strong action or link base |
| Lime | `#b7c83a` | Primary brand accent and active indicator |
| Paper | `#f5f4ee` | Primary text on dark surfaces |

Derived translucent values belong in the `:root` token block. Component rules
must consume semantic variables instead of introducing isolated brand colors.
Keep focus indicators and feedback states distinguishable from the brand accent.

## Typography

The preferred families are Space Grotesk for display text, IBM Plex Sans for body
text, and IBM Plex Mono for protocol and payload details. The current standalone
fixture uses local system fallbacks and must not fetch fonts at runtime.

## Change checklist

When changing the browser interface:

1. Preserve `Molejo Testkit` in localized metadata and footer copy.
2. Use the appropriate official logo variant and keep its alternative text.
3. Add colors to the semantic token block before using them in components.
4. Update the embedded-asset test and run `go test ./...` and
   `npm run test:frontend`.
