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
