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
- `show()`

## UINode

- `set(name, value)`
- `style(name, value)`
- `add(child)`
- static builders: `view()`, `text(content)`, `button(label)`, `input()`, `nativeComponent(component)`

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
