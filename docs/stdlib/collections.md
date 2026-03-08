# polyloft.collections

`polyloft.collections` separates public collection contracts from the lower-level built-in protocol interfaces such as `Iterable`, `Indexable`, and `Collection`.

```pf
import polyloft.collections { List, Deque, Set, HashSet }
```

## Contracts

### List

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: any) -> void`
- `remove(element: any) -> bool`
- `contains(element: any) -> bool`
- `clear() -> void`
- `asArray() -> array`
- `__length() -> number`
- `__get(index: int) -> any`
- `__set(index: int, value: any) -> void`
- `__contains(element: any) -> bool`
- `__slice(start: int, finish: int) -> any`
- `get(index: int) -> any`
- `set(index: int, value: any) -> void`

### Deque

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: any) -> void`
- `remove(element: any) -> bool`
- `contains(element: any) -> bool`
- `clear() -> void`
- `asArray() -> array`
- `addFirst(element: any) -> void`
- `addLast(element: any) -> void`
- `removeFirst() -> any`
- `removeLast() -> any`

### Set

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(element: any) -> void`
- `remove(element: any) -> bool`
- `contains(element: any) -> bool`
- `clear() -> void`
- `asArray() -> array`
- `values() -> array`

## HashSet

`HashSet` is the current concrete implementation exposed by this module.

Constructors and factories:

- `HashSet()`
- `HashSet.from(items: array) -> HashSet`

Methods:

- `size() -> number`
- `isEmpty() -> bool`
- `add(key: any) -> void`
- `remove(key: any) -> bool`
- `contains(key: any) -> bool`
- `clear() -> void`
- `asArray() -> array`
- `values() -> array`

## Notes

- `List` and `Deque` are contracts only in the current BVM.
- `HashSet` is backed by hashed map storage.
- The library contracts intentionally live apart from the built-in protocol interfaces documented in `docs/language/builtin-interfaces.md`.