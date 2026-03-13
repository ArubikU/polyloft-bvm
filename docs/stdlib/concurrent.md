# polyloft.concurrent

`polyloft.concurrent` provides typed wrappers around the global runtime-backed `Concurrent` module.

```pf
import polyloft.concurrent { Thread, CompletableFuture, Channel, async }
```

## Overview

This module wraps native concurrency primitives behind generic classes that participate in normal import resolution and type checking.

## Thread<R>

Constructors and factories:

- `Thread(task: Supplier<R>)`
- `Thread.startThread(task: Supplier<R>) -> Thread<R>`

Methods:

- `start() -> Thread<R>`
- `join() -> R`
- `isAlive() -> bool`

## CompletableFuture<T>

Constructors and factories:

- `CompletableFuture()`
- `CompletableFuture(nativeFuture: any)`
- `CompletableFuture.supplyAsync(task: Supplier<T>) -> CompletableFuture<T>`

Methods:

- `complete(candidate: T) -> bool`
- `get() -> T`
- `await() -> T`
- `getTimeout(timeoutMillis: int) -> T`
- `isDone() -> bool`
- `cancel() -> bool`
- `then<R>(callback: Function<T, R>) -> CompletableFuture<R>`
- `catch(callback: Function<String, T>) -> CompletableFuture<T>`
- `finally(callback: Runnable) -> CompletableFuture<T>`

## Channel<T>

Constructors:

- `Channel()`
- `Channel(capacity: int)`

Methods:

- `send(candidate: T) -> void`
- `receive() -> T`
- `close() -> bool`
- `isClosed() -> bool`

## Functions

- `async<T>(task: Supplier<T>) -> CompletableFuture<T>`

## Notes

- `Thread`, `CompletableFuture`, and `Channel` all store native runtime handles internally
- `catch(...)` currently receives the failure message as `String`
- `finally(...)` is modeled with `Runnable`

## Example

```pf
import polyloft.concurrent { Thread, async, Channel }

let worker = Thread.startThread(() => 21 * 2)
println(worker.join())

let future = async(() => "ready")
println(future.await())

let channel = Channel<string>()
channel.send("ok")
println(channel.receive())
```