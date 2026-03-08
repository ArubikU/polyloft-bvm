# Built-in Interfaces

`polyloft-bvm` now exposes the core protocol-style interfaces used by the language surface:

- `Iterable`
- `Unstructured`
- `Sliceable`
- `Indexable`
- `Collection`

These interfaces matter in two different ways:

- nominal typing: you can annotate parameters and variables with them
- protocol dispatch: some of them also drive syntax such as `for`, destructuring, `[]` and slices on user-defined classes

## Protocol methods

### Iterable

Required methods:

```pf
interface Iterable:
    __length() -> number
    __get(index: int) -> any
end
```

If a class provides these methods, BVM uses them for `for item in value:`.

### Unstructured

Required methods:

```pf
interface Unstructured:
    __pieces() -> number
    __get_piece(index: int) -> any
end
```

If a class provides these methods, BVM uses them for destructuring.

### Indexable

Required methods:

```pf
interface Indexable:
    __get(key) -> any
    __set(key, value) -> void
    __contains(key) -> bool
end
```

If a class provides `__get` and `__set`, BVM uses them for `value[index]` and `value[index] = other`.

If a class also provides `__contains`, BVM uses it for `needle in value`.

### Sliceable

Required methods:

```pf
interface Sliceable:
    __slice(start: int, finish: int) -> any
end
```

If a class provides `__slice`, BVM uses it for `value[start...finish]`.

### Collection

Required methods:

```pf
interface Collection:
    size() -> number
    isEmpty() -> bool
    add(element) -> void
    remove(element) -> bool
    contains(element) -> bool
    clear() -> void
    asArray() -> array
end
```

`Collection` is currently a nominal API contract. It does not add VM syntax by itself, but it is available for typing and interface validation.

## Native type matrix

The BVM intentionally separates native syntax support from nominal interface membership.

Current tested matrix:

- `Range`: implements `Iterable`
- `array`: implements `Indexable` and `Sliceable`
- `map`: implements `Indexable`
- `tuple`: implements `Unstructured` and `Sliceable`
- `string`: implements `Sliceable`

Containment support:

- `array`, `map` and `string` support `needle in value` natively
- user-defined classes can support `in` by implementing `__contains`

Important non-equivalences:

- `array` can still be used in `for` loops, but it is not assignable to `Iterable`
- `string` supports native indexing syntax, but it is not modeled as nominal `Indexable`
- `tuple` supports native indexing syntax, but it is not modeled as nominal `Indexable`
- `map` can still be iterated natively in `for`, but that native loop support is separate from the nominal `Iterable` interface

This mirrors the design goal that syntax convenience and interface contracts are related but not identical.

## Example

```pf
class Buffer implements Iterable, Unstructured, Indexable, Sliceable:
    var data: array

    Buffer(items: array):
        this.data = items
    end

    def __length() -> number:
        return len(this.data)
    end

    def __get(index: int) -> any:
        return this.data[index]
    end

    def __set(index: int, value: any) -> void:
        this.data[index] = value
    end

    def __contains(value: any) -> bool:
        return false
    end

    def __slice(start: int, finish: int) -> Buffer:
        let cut = Buffer([])
        cut.data = this.data[start...finish]
        return cut
    end

    def __pieces() -> number:
        return 2
    end

    def __get_piece(index: int) -> any:
        if index == 0:
            return this.data[0]
        end
        return this.data[1]
    end
end
```

## Import-facing collection routes

Core protocol names such as `Iterable` and `Collection` are builtins, so they do not require imports.

Collection-family contracts intended for library imports live under `polyloft.collections`:

```pf
import polyloft.collections { List, Deque, Set, HashSet }
```

This keeps protocol syntax and library surface separate:

- builtins define what the checker and VM understand directly
- `polyloft.collections` defines the public library namespace for collection-oriented APIs