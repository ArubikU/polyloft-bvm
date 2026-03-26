# polyloft.ui

`polyloft.ui` is the object-oriented stdlib facade for the global runtime `Ui` module.

Import it with:

```pf
import polyloft.ui { UI, UIApp, UIWindow, UINode, UIChannel }
import polyloft.concurrent { CompletableFuture }
```

## Main Types

- `UI`: static entry-point helpers.
- `UIApp`: app lifecycle, permissions, channels, dispatch.
- `UIWindow`: window lifecycle and event binding.
- `UINode`: component tree nodes and style/props.
- `UIChannel`: named app-scoped channel wrapper.
- `CompletableFuture<any>`: async dispatch result (from `polyloft.concurrent`).

## Entry Point

```pf
let app = UI.app("headless")
```

## UIApp

- `allowProfile(profile)`
- `setPermission(name, allowed)`
- `hasPermission(name)`
- `permissions()`
- `eventChannel(capacity)`
- `emit(name, payload)`
- `platform()`
- `channel(name, capacity)`
- `dispatch(callback, payload)`
- `dispatchAsync(callback, payload)`
- `post(callback, payload, priority)`
- `cancelTask(taskId)`
- `runOnUiThread(maxTasks)`
- `startUiLoop(timeSliceMs, maxTasksPerTick)`
- `stopUiLoop()`
- `isUiLoopRunning()`
- `window(title, width, height)`
- `run()`

## UIWindow

- `root(node)`
- `on(eventName, callback)`
- `trigger(eventName, payload)`
- `json()`
- `layoutJson()`
- `reconcile(node)`
- `lastPatchJson()`
- `lastTransitionJson()`
- `debug(enabled)`
- `show()`

## UINode

- `set(name, value)`
- `style(name, value)`
- `setClass(className)`
- `addClass(className)`
- `add(child)`
- static builders: `view()`, `text(content)`, `button(label)`, `input()`, `nativeComponent(component)`

## HTML Equivalence

Polyloft UI is intentionally HTML-like. These are the current conceptual mappings:

| Polyloft UI | Rough HTML equivalent | Notes |
| --- | --- | --- |
| `UI.view()` | `<div>` | Generic container/layout node. This is the closest equivalent to a standard block container. |
| `UI.text("...")` | `<span>` / text node | Rendered as text/label. Use classes/styles for typography. |
| `UI.button("...")` | `<button>` | Supports event binding through `event` prop plus `window.on(...)`. |
| `UI.input(type)` | `<input type="...">` | Native input dispatch by type (`text`, `password`, `textarea`, `range`, `checkbox`, `select`). |
| `UI.nativeComponent("slider")` | `<input type="range">` | Native host control. Emits change event payloads. |
| `UI.nativeComponent("progressbar")` | `<progress>` | Native host progress widget. |
| `UI.nativeComponent("checkbox")` | `<input type="checkbox">` | Native host checkbox widget. |
| `UI.nativeComponent("select")` | `<select>` | Native host dropdown/select widget. |

## CSS-Like Styling

Polyloft UI supports two styling paths:

1. Class-based styling through `StyleSheet` and `.css` files.
2. Inline styling through `node.style(name, value)`.

Class names are attached with:

- `node.setClass("card")`
- `node.addClass("selected")`
- `node.set("class", "card selected")`

Inline styles override class styles.

## Supported CSS Properties

Current supported CSS-like properties in the Go/native renderer:

| CSS property | Supported | Notes |
| --- | --- | --- |
| `color` | Yes | Text foreground color. |
| `background-color` | Yes | Background fill for views and button backing. |
| `background` | Yes | Alias/fallback for background-color. |
| `font-size` | Yes | Numeric text size; `px` supported. |
| `font-weight` | Yes | `bold`, `700`, `800`, `900`, `bolder`. |
| `font-style` | Yes | `italic`, `oblique`. |
| `font-family` | Partial | Monospace detection (`mono`, `monospace`). |
| `text-align` | Yes | `left`, `center`, `right`, `end`. |
| `text-transform` | Yes | `uppercase`, `lowercase`, `capitalize`. |
| `letter-spacing` | Partial | Positive spacing currently emulated with spaces. |
| `:hover` (class selector) | Yes | Use `.class:hover { ... }`. |
| `width` | Yes | Supports `px`, `%`, `vw`. |
| `height` | Yes | Supports `px`, `%`, `vh`. |
| `min-width`, `max-width` | Yes | Applied during layout sizing. |
| `min-height`, `max-height` | Yes | Applied during layout sizing. |
| `padding` | Yes | 1-4 value shorthand plus side-specific props. |
| `padding-left/right/top/bottom` | Yes | Side-specific padding supported. |
| `margin` | Yes | 1-4 value shorthand plus side-specific props. |
| `margin-left/right/top/bottom` | Yes | Side-specific margins supported. |
| `gap` | Yes | Spacing between children in flex-like layout. |
| `row-gap` | Yes | Row spacing in grid and wrapped flex lines. |
| `column-gap` | Yes | Column spacing in grid and wrapped flex lines. |
| `flex` | Yes | Numeric flex weight for child distribution. |
| `place-items` | Partial | Grid shorthand for align/justify item placement. |
| `align-self` | Yes | Per-item override for cross-axis alignment. |
| `justify-self` | Partial | Grid item horizontal placement. |
| `display` | Partial | `none` supported. |
| `visibility` | Partial | `hidden` supported. |
| `opacity` | Partial | Applied to text/background alpha. |
| `border`, `border-width`, `border-color` | Partial | Rectangle/background-based rendering. |
| `border-radius` | Partial | Rounded corners on rectangle-backed nodes. |
| `position`, `overflow`, `box-shadow` | Planned | Not implemented yet. |

## Supported Value Formats

Current accepted value formats:

| Property type | Supported formats |
| --- | --- |
| Colors | `#RGB`, `#RGBA`, `#RRGGBB`, `#RRGGBBAA`, `rgb(...)`, `rgba(...)`, basic named colors |
| Font size / lengths | bare number as string, `px`, `%`, `vw`, `vh` |
| Font weight | `bold`, `bolder`, `700`, `800`, `900` |
| Font style | `italic`, `oblique` |
| Text align | `left`, `center`, `right`, `end` |

Notes:

- CSS files currently support class selectors like `.card { color: #fff; }`.
- Comma-separated class selectors are supported.
- Inline style properties should use CSS names, e.g. `style("font-size", "20px")`.
- Layout props are still mainly controlled with node props such as `direction`, `justify`, `align`, `gap`, `flex`, `width`, `height`, `padx`, `pady`.

Layout props supported on containers and children include:

- `direction`: `row` | `column`
- `justify`: `start` | `center` | `end` | `space-between` | `space-around`
- `align`: `start` | `center` | `end` | `stretch`
- `flex`: numeric weight
- `width`, `height`, `gap`, `padx`, `pady`

## UIChannel

- `send(payload)`
- `receive()`
- `close()`

## Notes

- `dispatchAsync` requires `background` permission.
- For `dispatchAsync`, import `polyloft.concurrent` in the consumer module (e.g. `CompletableFuture`).
- For desktop rendering, use backend `go` (or alias `native`).
- `headless` backend is ideal for tests and server-side orchestration.
- `window.reconcile(...)` computes incremental patch sets (`add`, `update`, `remove`) and updates window tree.
- keyed children (`key` / `id`) produce robust reorder/move patches.
- transitions are exported as JSON through `window.lastTransitionJson()` (`fade-in`, `fade-out`, `move`, `morph`).
