# polyloft.collections

`polyloft.collections` defines collection-facing library contracts separately from the lower-level built-in interfaces such as `Iterable`, `Indexable`, and `Collection`.

```pf
import polyloft.collections { List, Deque, Set, ArrayList, ArrayDeque, LinkedList, HashSet }
```

## Overview

This module contains:

- generic collection contracts intended for library APIs
- concrete implementations `ArrayList<T>`, `ArrayDeque<T>`, `LinkedList<T>`, and `HashSet<T>`

Internally, the embedded stdlib stores these contracts and implementations in separate module files under `internal/modules/stdlib/polyloft/collections/`, while `polyloft.collections` remains the stable import-facing facade that reexports them from its `index.pf`.

The built-in protocol interfaces used directly by the VM are documented in [../language/builtin-interfaces.md](../language/builtin-interfaces.md).

## Contracts

### List<T>

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: T) -> void`
- `remove(element: T) -> bool`
- `contains(element: T) -> bool`
- `clear() -> void`
- `asArray() -> array<T>`
- `__length() -> number`
- `__get(index: int) -> T`
- `__set(index: int, value: T) -> void`
- `__contains(element: T) -> bool`
- `__slice(start: int, finish: int) -> array<T>`
- `get(index: int) -> T`
- `set(index: int, value: T) -> void`

### Deque<T>

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: T) -> void`
- `remove(element: T) -> bool`
- `contains(element: T) -> bool`
- `clear() -> void`
- `asArray() -> array<T>`
- `addFirst(element: T) -> void`
- `addLast(element: T) -> void`
- `removeFirst() -> T`
- `removeLast() -> T`

### Set<T>

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: T) -> void`
- `remove(element: T) -> bool`
- `contains(element: T) -> bool`
- `__contains(element: T) -> bool`
- `clear() -> void`
- `asArray() -> array<T>`
- `values() -> array<T>`

## Concrete Implementations

### ArrayList<T>

`ArrayList<T>` is a list wrapper backed by the stdlib native list handle layer.

Constructors:

- `ArrayList()`

Notes:

- implements `List<T>`
- supports indexing through `__get`, `__set`, and slicing through `__slice`
- use it when you want list semantics with append, indexed access, and conversion back to `array<T>`

### ArrayDeque<T>

`ArrayDeque<T>` is the deque implementation currently exported by this module.

Constructors:

- `ArrayDeque()`

Notes:

- implements `Deque<T>`
- uses the same native list handle family as list implementations
- `add(element)` appends to the tail
- `addFirst`, `addLast`, `removeFirst`, and `removeLast` provide queue and stack style operations without manual array rebuilding

### LinkedList<T>

`LinkedList<T>` currently shares the same native list-backed behavior as `ArrayList<T>`, but is exposed as a separate concrete type for list-oriented APIs.

Constructors:

- `LinkedList()`

Notes:

- implements `List<T>`
- supports the same list contract methods as `ArrayList<T>`
- remains a distinct import-facing type even though the current runtime storage path is shared

### HashSet<T>

`HashSet<T>` is the current concrete set implementation exposed by this module.

Constructors and factories:

- `HashSet()`
- `HashSet.from(items: array<T>) -> HashSet<T>`

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(key: T) -> void`
- `remove(key: T) -> bool`
- `contains(key: T) -> bool`
- `__contains(key: T) -> bool`
- `clear() -> void`
- `asArray() -> array<T>`
- `values() -> array<T>`

## Notes

- `List` remains a contract plus concrete list implementations.
- `Deque` now has a concrete stdlib implementation via `ArrayDeque`.
- `HashSet` is backed by hashed map storage using the VM `hash(...)` model.
- The library contracts in this module intentionally remain separate from the VM protocol interfaces documented in [../language/builtin-interfaces.md](../language/builtin-interfaces.md).