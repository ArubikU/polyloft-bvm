# Embedded Stdlib

This document lists the embedded modules and core builtins currently exposed by `polyloft-bvm`.

For a module-by-module reference, use:

- [Stdlib Index](stdlib/README.md)
- [polyloft.common](stdlib/common.md)
- [polyloft.maps](stdlib/maps.md)
- [polyloft.collections](stdlib/collections.md)
- [polyloft.function](stdlib/function.md)

## Core builtins

Installed from `internal/runtime/core.go`:

- `print(value)`
- `println(value)`
- `input([prompt])`
- `range(start, end)`
- `len(value)`
- `delete(map, key)`
- `keys(map)`
- `values(map)`
- `hash(value)`

Notes:

- `len` currently supports `string`, `array`, `tuple` and `map`
- `delete`, `keys` and `values` operate on raw runtime maps
- `hash` is declared as a builtin but resolved by the VM

## Sys module

The `Sys` module is part of the runtime and is used in existing tests and demos for time-based helpers such as:

- `Sys.time()`
- `Sys.sleep(seconds)`

This module is runtime-backed, not loaded from `polyloft.*` embedded source.

## polyloft.common

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

## polyloft.maps

Current collection facade classes:

- `Map`
- `HashMap`
- `SetMap`

See [stdlib/maps.md](stdlib/maps.md) for the full `Map`, `HashMap`, and `SetMap` reference.

## polyloft.collections

Import-facing collection contracts:

- `List`
- `Deque`
- `Set`

Current concrete implementation:

- `HashSet`

See [stdlib/collections.md](stdlib/collections.md) for the contract and method reference.

## polyloft.function

Java-style functional interfaces available as embedded imports:

- `Predicate<T>`
- `Consumer<T>`
- `Supplier<T>`
- `Function<T, R>`
- `BiFunction<T, U, R>`
- `UnaryOperator<T>`
- `BinaryOperator<T>`

See [stdlib/function.md](stdlib/function.md) for the exact contracts and current limitations.

## Status note

These modules are intentionally small and evolve alongside tests. If you need canonical language-wide docs, use the parent repository. If you need the behavior that `polyloft-bvm` implements today, prefer this document and the module pages under `docs/stdlib/`.