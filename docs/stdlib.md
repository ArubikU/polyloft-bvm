# Standard Library and Runtime Modules

This document lists the builtins, runtime-backed globals, exception classes, and embedded `polyloft.*` modules currently exposed by `polyloft-bvm`.

The public surface is split in two:

- global native modules registered directly by the runtime such as `Sys`, `Http`, `Json`, `Io`, and `Concurrent`
- embedded modules under `polyloft.*` that compile like normal user code and usually wrap those native globals behind typed classes

In practice, user-facing code should usually import the `polyloft.*` facade when one exists. The raw global module remains useful for low-level interoperability and for understanding the underlying native entry points.

For the object-oriented embedded stdlib, use:

- [Stdlib Index](stdlib/README.md)
- [polyloft.common](stdlib/common.md)
- [polyloft.http](stdlib/http.md)
- [polyloft.json](stdlib/json.md)
- [polyloft.io](stdlib/io.md)
- [polyloft.concurrent](stdlib/concurrent.md)
- [polyloft.maps](stdlib/maps.md)
- [polyloft.collections](stdlib/collections.md)
- [polyloft.vectors](stdlib/vectors.md)
- [polyloft.math](stdlib/math.md)
- [polyloft.crypto](stdlib/crypto.md)
- [polyloft.function](stdlib/function.md)

## Core Builtins

Installed from `internal/runtime/core.go`:

- `print(value)`
- `println(value)`
- `input([prompt])`
- `range(start, end)`
- `len(value)`
- `sqrt(number)`
- `delete(map, key)`
- `keys(map)`
- `values(map)`
- `hash(value)`

Notes:

- `len` supports `string`, `array`, `tuple`, and `map`
- `sqrt` supports non-negative numeric values and returns `float`
- `delete`, `keys`, and `values` operate on raw runtime maps
- `hash` is declared as a builtin but executed by the VM

## Built-in Exception Classes

Runtime exception classes registered globally:

- `Exception`
- `RuntimeError`
- `NameError`
- `TypeError`
- `ValueError`
- `ArityError`
- `IndexError`
- `KeyError`
- `IOException`
- `FileNotFoundException`
- `NetworkError`
- `TimeoutError`

These classes are available for typed `catch` clauses and runtime-generated failures.

## Sys Module

The `Sys` module is runtime-backed and provides:

- `Sys.time()` -> current Unix time in milliseconds
- `Sys.sleep(milliseconds)` -> blocking sleep

## Http Module

The global `Http` module is runtime-backed and provides low-level native bindings used by `polyloft.http`:

- `Http.client_request(method, url, body, headers, timeoutMillis)`
- `Http.server_listen(port, callback)`

`server_listen` expects a callback that receives a request map and returns either a response map or a printable value.

For typed request and response classes, handlers, and a small server facade, see [stdlib/http.md](stdlib/http.md).

## Json Module

The global `Json` module is runtime-backed and provides:

- `Json.stringify(value)`
- `Json.parse(text)`

For the import-facing wrapper class, see [stdlib/json.md](stdlib/json.md).

## Io Module

The global `Io` module is runtime-backed and provides file, directory and buffer helpers:

- `Io.read_file(path)`
- `Io.write_file(path, content)`
- `Io.append_file(path, content)`
- `Io.exists(path)`
- `Io.delete_path(path)`
- `Io.mkdir(path)`
- `Io.read_dir(path)`
- `Io.is_dir(path)`
- `Io.is_file(path)`
- `Io.file_size(path)`
- `Io.file_info(path)`
- `Io.open_file(path)`
- `Io.open_file_mode(path, mode)`
- `Io.file_read(handle)`
- `Io.file_read_size(handle, size)`
- `Io.file_read_line(handle)`
- `Io.file_write(handle, content)`
- `Io.file_close(handle)`
- `Io.buffer_new()`
- `Io.buffer_new_with_string(text)`
- `Io.buffer_write(buffer, text)`
- `Io.buffer_read(buffer, size)`
- `Io.buffer_string(buffer)`
- `Io.buffer_clear(buffer)`

Supported file modes include `r`, `w`, `a`, `rw`, `r+`, and `w+`.

For the import-facing `IO` class with method-style wrappers, see [stdlib/io.md](stdlib/io.md).

## Concurrent Module

The global `Concurrent` module is runtime-backed and currently provides thread, future and channel primitives:

### Threads
- `Concurrent.thread_new(task)`
- `Concurrent.thread_start(thread)`
- `Concurrent.thread_join(thread)`
- `Concurrent.thread_is_alive(thread)`

### Futures
- `Concurrent.future_new()`
- `Concurrent.future_run_async(task)`
- `Concurrent.future_complete(future, value)`
- `Concurrent.future_get(future)`
- `Concurrent.future_get_timeout(future, timeoutMillis)`
- `Concurrent.future_is_done(future)`
- `Concurrent.future_cancel(future)`
- `Concurrent.future_then(future, callback)`
- `Concurrent.future_catch(future, callback)`
- `Concurrent.future_finally(future, callback)`

### Channels
- `Concurrent.channel_new(capacity)`
- `Concurrent.channel_send(channel, value)`
- `Concurrent.channel_receive(channel)`
- `Concurrent.channel_close(channel)`
- `Concurrent.channel_is_closed(channel)`

For typed wrapper classes such as `Thread<R>`, `CompletableFuture<T>`, and `Channel<T>`, see [stdlib/concurrent.md](stdlib/concurrent.md).

## Embedded Modules

### polyloft.common

Wrapper and facade classes:

- `Integer`
- `Float`
- `Double`
- `String`
- `Boolean`
- `Char`
- `CharArray`
- `Bytes`

See [stdlib/common.md](stdlib/common.md) for the full method reference.

### polyloft.http

Typed HTTP facade classes and interfaces:

- `Http`
- `HttpHeaders`
- `HttpClientResponse`
- `HttpServerRequest`
- `HttpServerResponse`
- `HttpServer`
- `HttpHandler`
- `HttpErrorHandler`

See [stdlib/http.md](stdlib/http.md) for the request and server API.

### polyloft.json

Typed import-facing wrapper class:

- `Json`

See [stdlib/json.md](stdlib/json.md).

### polyloft.io

Typed import-facing wrapper class:

- `IO`

See [stdlib/io.md](stdlib/io.md).

### polyloft.concurrent

Typed concurrency helpers:

- `Thread<R>`
- `CompletableFuture<T>`
- `Channel<T>`
- `async<T>(task)`

See [stdlib/concurrent.md](stdlib/concurrent.md).

### polyloft.maps

Current collection facade classes:

- `Map`
- `HashMap`
- `SetMap`

See [stdlib/maps.md](stdlib/maps.md) for the full method reference.

### polyloft.collections

Import-facing collection contracts:

- `List`
- `Deque`
- `Set`

Current concrete implementations:

- `ArrayList`
- `ArrayDeque`
- `LinkedList`
- `HashSet`

See [stdlib/collections.md](stdlib/collections.md) for the contract and method reference.

### polyloft.vectors

Embedded vector value types:

- `Vec2`
- `Vec3`

See [stdlib/vectors.md](stdlib/vectors.md) for operators and helper methods.

### polyloft.math

Embedded math facade:

- `Math`

See [stdlib/math.md](stdlib/math.md) for constants and numeric helpers.

### polyloft.crypto

Embedded crypto facade:

- `Crypto`

See [stdlib/crypto.md](stdlib/crypto.md) for hashing and encoding helpers.

### polyloft.function

Java-style functional interfaces available as embedded imports:

- `Predicate<T>`
- `Consumer<T>`
- `Supplier<T>`
- `Runnable`
- `Function<T, R>`
- `BiFunction<T, U, R>`
- `UnaryOperator<T>`
- `BinaryOperator<T>`

See [stdlib/function.md](stdlib/function.md) for the exact contracts and current limitations.

## Status Note

These modules evolve with the VM and its tests. For the broader language-wide vision, use the parent repository. For the behavior that `polyloft-bvm` actually compiles and runs, prefer this document and the module pages under `docs/stdlib/`.