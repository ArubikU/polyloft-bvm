# Polyloft Ui Library (Preview)

The `polyloft.ui` stdlib module provides a cross-backend UI foundation with:

- Component tree (`node_new`, `node_add_child`, `node_set_prop`)
- App/window lifecycle (`app_new`, `window_new`, `window_show`, `app_run`)
- Permissions (`app_set_permission`, `app_allow_profile`, `app_has_permission`)
- Permission snapshot (`app_permissions`)
- Event-driven model and channels (`app_event_channel`, `app_emit_event`, `window_bind`, `window_trigger`)
- Named app channels (`app_channel_new`, `app_channel_send`, `app_channel_receive`, `app_channel_close`)
- Callback dispatch (`app_dispatch`, `app_dispatch_async`)
- Backend abstraction (`headless`, `tk`)

## Quick Start

```polyloft
import polyloft.ui { UI }

var app = UI.app("go")
app.allowProfile("desktop")

var root = UI.view()
root.set("direction", "column")
root.set("align", "stretch")
root.set("justify", "start")
root.set("gap", 8)

var title = UI.text("Hello Polyloft UI")
title.set("padx", 12)
title.set("pady", 12)
root.add(title)

var slider = UI.nativeComponent("slider")
slider.set("min", 0)
slider.set("max", 100)
slider.set("value", 50)
root.add(slider)

var win = app.window("Demo", 900, 540)
win.root(root)
win.show()
app.run()
```

The lower-level global `Ui` runtime module still exists for interoperability, but user code should prefer `import polyloft.ui`.

## HTML / CSS Mental Model

If you come from web UI, the simplest way to think about Polyloft UI is:

- `UI.view()` behaves like a `<div>`.
- `UI.text(...)` behaves like a text node or `<span>`.
- `UI.button(...)` behaves like a `<button>`.
- `UI.input(type)` behaves like `<input type="...">` and dispatches to native widgets by `type`.
- `UI.nativeComponent("slider")` behaves like `<input type="range">` (still available for explicit native control).

Example:

```polyloft
var card = UI.view()           // like <div class="card">
card.setClass("card")

var title = UI.text("Build Status")   // like <span class="title">...</span>
title.setClass("title")

card.add(title)
```

## Styling Modes

Two styling modes are supported today.

### 1. CSS File / StyleSheet

```polyloft
var ss = UI.stylesheet()
ss.loadFile("example_project/src/styles.css")
app.attachStylesheet(ss)

var title = UI.text("Hello")
title.setClass("title")
```

```css
.title {
    color: #e8e8e8;
    font-size: 24px;
    text-align: center;
}
```

### 2. Inline CSS-Like Style

```polyloft
var title = UI.text("Hello")
title.style("color", "#e8e8e8")
title.style("font-size", "24px")
title.style("text-align", "center")
```

Inline styles win over class styles.

## Currently Supported CSS Subset

Supported now in the native Go renderer:

- `color`
- `background-color`
- `background`
- `font-size`
- `font-weight`
- `font-style`
- `font-family` (monospace detection)
- `text-align`
- `text-transform`
- `letter-spacing`
- `width` (`px`, `%`, `vw`)
- `height` (`px`, `%`, `vh`)
- `min-width`
- `max-width`
- `min-height`
- `max-height`
- `padding`, `padding-top/right/bottom/left`
- `margin`, `margin-top/right/bottom/left`
- `gap`
- `row-gap`
- `column-gap`
- `flex`
- `flex-direction` (alias to `direction`)
- `justify-content` (alias to `justify`)
- `align-items` (alias to `align`)
- `place-items` (grid shorthand)
- `align-self` (item-level override)
- `justify-self` (grid item-level override)
- `display` (`none`)
- `visibility` (`hidden`)
- `opacity`
- `border`
- `border-width`
- `border-color`
- `border-radius`
- `:hover` class selectors (`.btn:hover`)

Supported formats now:

- colors: `#RGB`, `#RGBA`, `#RRGGBB`, `#RRGGBBAA`, `rgb(...)`, `rgba(...)`, named colors (`white`, `black`, `red`, `green`, `blue`, `yellow`, `gray`, `transparent`)
- lengths: number string, `px`, `%`, `vw`, `vh`
- font-weight: `bold`, `bolder`, `700`, `800`, `900`
- font-style: `italic`, `oblique`
- text-align: `left`, `center`, `right`, `end`

Notes:

- `display:none` removes rendering for the node.
- `visibility:hidden` keeps layout but hides drawing.
- `border` shorthand supports patterns like `1px solid #30363d`.
- `margin` and `padding` support 1-4 value shorthand.

## Input Type Dispatch

`UI.input(type)` maps to native controls:

- `UI.input("text")` -> single-line entry
- `UI.input("password")` -> password entry
- `UI.input("textarea")` / `UI.input("multiline")` -> multiline entry
- `UI.input("range")` / `UI.input("slider")` -> slider
- `UI.input("checkbox")` -> checkbox
- `UI.input("select")` -> dropdown

If no type is provided, `UI.input()` defaults to `text`.

Still primarily prop-based for layout:

- `direction`
- `justify`
- `align`
- `gap`
- `flex`
- `width`
- `height`
- `padx`
- `pady`

That means the current model is:

- typography/colors: CSS-like
- layout: mixed CSS-like intent, but mostly explicit node props

This is intentional for now and can be expanded gradually.

## Permissions

Permissions are explicit and can be adjusted per app.

- `window`
- `process_exec`
- `clipboard`
- `file_dialog`
- `network`
- `background`

Profiles:

- `desktop`
- `mobile`
- `sandbox`

Example:

```polyloft
Ui.app_allow_profile(app, "sandbox")
Ui.app_set_permission(app, "network", false)
```

## Channels + Events

Use `Concurrent.channel_*` together with Ui events.

```polyloft
var eventCh = Ui.app_event_channel(app, 64)
Ui.window_bind(win, "click", fn(payload) {
    Ui.app_emit_event(app, "click", payload)
})

var worker = Concurrent.thread_new(fn() {
    while true:
        var e = Concurrent.channel_receive(eventCh)
        if e == nil:
            break
        println("UI event: " + e)
    end
})
Concurrent.thread_start(worker)
```

### Named App Channels

```polyloft
Ui.app_channel_new(app, "jobs", 64)
Ui.app_channel_send(app, "jobs", {"task": "render", "id": 10})
var job = Ui.app_channel_receive(app, "jobs")
println(job)
Ui.app_channel_close(app, "jobs")
```

### Dispatch (sync + async)

```polyloft
import polyloft.concurrent { CompletableFuture }

def on_payload(payload: any) -> any:
    return "handled => " + payload
end

var result = Ui.app_dispatch(app, on_payload, "sync")
println(result)

var future = Ui.app_dispatch_async(app, on_payload, "async")
println(Concurrent.future_get(future))
```

### Scheduler (UI Thread)

```polyloft
def job(payload: any) -> any:
    return "job => " + payload
end

var idA = app.post(job, "A", 10)
var idB = app.post(job, "B", 1)

println(app.runOnUiThread(1))
println(app.cancelTask(idB))
println(app.runOnUiThread(64))
println(app.startUiLoop(16, 8))
println(app.isUiLoopRunning())
println(app.stopUiLoop())
```

### Reconciliation (Diff/Patch)

```polyloft
var nextRoot = UI.view()
nextRoot.set("direction", "column")
nextRoot.add(UI.text("Updated"))

var patch = win.reconcile(nextRoot)
println(patch)
println(win.lastPatchJson())
println(win.lastTransitionJson())
println(win.layoutJson())
```

## Backends

- `headless`: no real OS window, ideal for tests and servers.
- `go` / `native`: Go-native renderer (no Python dependency).

Notes:

- `window_show` marks windows as visible; `app.run()` opens the native window.
- `app_dispatch_async` requires `background` permission.

## Design Direction

The module is designed to evolve toward richer backends (native desktop/mobile/web) while keeping a stable declarative component model and event channel semantics.
