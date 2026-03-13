# Built-in Interfaces

polyloft-bvm exposes a small set of built-in protocol interfaces that are understood directly by the checker and, in some cases, by the VM syntax layer.

The built-in interfaces are:

- `Iterable`
- `Unstructured`
- `Sliceable`
- `Indexable`
- `Collection`

These interfaces are used in two different ways:

- nominal typing, where variables and parameters are annotated with an interface name
- protocol dispatch, where special method names enable syntax such as `for`, destructuring, indexing, and slicing on user-defined classes

## Protocol Methods

### Iterable

```pf
interface Iterable:
    __length() -> number
    __get(index: int) -> any
end
```

When a class provides this protocol, the VM can use it for `for item in value:` iteration.

### Unstructured

```pf
interface Unstructured:
    __pieces() -> number
    __get_piece(index: int) -> any
end
```

When a class provides this protocol, the VM can use it for destructuring.

### Indexable

```pf
interface Indexable:
    __get(key) -> any
    __set(key, value) -> void
    __contains(key) -> bool
end
```

When a class provides `__get` and `__set`, the VM can route `value[index]` and `value[index] = other` through those methods. If it also provides `__contains`, the VM can route `needle in value` through that method.

### Sliceable

```pf
interface Sliceable:
    __slice(start: int, finish: int) -> any
end
```

When a class provides `__slice`, the VM can route `value[start...finish]` through that method.

### Collection

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

`Collection` is a nominal API contract in the current BVM. It does not by itself add special VM syntax.

## Native Type Matrix

Native syntax support and nominal interface membership are intentionally not identical.

Current tested behavior:

- `Range` implements `Iterable`
- `array` implements `Indexable` and `Sliceable`
- `map` implements `Indexable`
- `tuple` implements `Unstructured` and `Sliceable`
- `string` implements `Sliceable`

Containment support:

- `array`, `map`, and `string` support `needle in value` natively
- user-defined classes can support `in` through `__contains`

Important differences:

- `array` can be iterated natively in `for`, but it is not modeled as nominal `Iterable`
- `string` supports native indexing, but it is not modeled as nominal `Indexable`
- `tuple` supports native indexing, but it is not modeled as nominal `Indexable`
- `map` supports native `for` iteration, but that loop behavior is separate from nominal `Iterable`

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

## Related Library Contracts

Built-in protocol names such as `Iterable` and `Collection` do not require imports.

Library-facing collection contracts live under `polyloft.collections`:

```pf
import polyloft.collections { List, Deque, Set, HashSet }
```

This separation is intentional:

- built-ins define the interfaces that the checker and VM understand directly
- `polyloft.collections` defines the public library namespace for collection-oriented APIs