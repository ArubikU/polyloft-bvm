# Types and Objects

This page covers the types, classes and object model that `polyloft-bvm` supports today.

## Primitive and built-in type names

Primitive scalar names used by the checker:

- `int`
- `float`
- `number`
- `bool`
- `char`
- `nil`
- `void`

Runtime-native built-in value kinds:

- `string`
- `array`
- `map`
- `tuple`
- `Range`
- `any`

Current source compatibility notes:

- `string` and `String` both resolve to the native text type in annotations
- object-style helper methods live on `polyloft.common.String`
- string indexing returns `char`

## Numeric types

`int` and `float` are now distinct primitive numeric types in the BVM.

Current numeric rules:

- integer literals like `1` infer as `int`
- decimal literals like `1.5` infer as `float`
- `number` remains the broad numeric supertype for compatibility
- `int` is assignable to `float`
- `float` is not assignable to `int` without an explicit cast
- casts currently support `(int)` and `(float)`
- `+`, `-`, `*`, `%` keep `int` only when both operands are `int`
- `/` produces `float`

Example:

```pf
let whole: int = 1
let frac: float = 1.5
let widened: float = whole
let narrowed: int = (int) frac
let mixed: float = whole + frac
```

## Text and char model

The current BVM text model distinguishes `char` from text values.

Current behavior:

- `char` is its own scalar type
- indexing a string returns `char`
- slicing a string returns string text
- `char` and string values are comparable in the current checker and VM rules
- the imported `polyloft.common.Char` and `polyloft.common.String` classes are wrappers, not replacements for the native value kinds

Example:

```pf
let text: string = "Polyloft"
let first: char = text[0]

println(first == "P")
println(text[1...3])
println(String(text).charAt(0))
```

## Union types and aliases

The checker now supports unions and static type aliases.

Current behavior:

- unions use `|`
- aliases use `type Name = ...`
- aliases are compile-time only and do not produce runtime values
- array and map literals preserve mixed element types as unions instead of collapsing straight to `any`
- function and method annotations carry the structural type string into bytecode metadata so the VM can enforce common cases such as `array<number | string>`
- arrays may also be written with Java‑style `T[]` syntax; both forms are equivalent and nest (e.g. `int[][]` ↔ `array<array<int>>`)

Example:

```pf
type Scalar = number | string

let values: array<Scalar> = ["a", 1]

def sizeOf(items: array<Scalar>) -> number:
    return len(items)
end

println(sizeOf(values))
```

Invalid assignments are rejected:

```pf
let broken: array<number> = ["a"]
```

## Classes

Supported class features include:

- fields
- constructors
- instance methods
- static methods and static fields
- access modifiers: `public`, `private`, `protected`
- inheritance via `<`
- interface implementation via `implements`
- abstract classes
- sealed classes
- enums with singleton values and static helpers

Example:

```pf
class Counter:
    value: number

    Counter(start: number):
        this.value = start
    end

    def inc() -> number:
        this.value += 1
        return this.value
    end

    static def square(x: number) -> number:
        return x * x
    end
end
```

## Records

Records are supported as a compact immutable form.

Current BVM behavior:

- fields are generated from the parameter list
- a constructor is generated automatically
- fields are immutable
- custom constructors are rejected

Example:

```pf
record Point(x: number, y: number)
    def sum() -> number:
        return this.x + this.y
    end
end

let point = Point(2, 5)
println(point.sum())
```

## Enums

Enums are supported as optimized singleton object sets.

Current BVM behavior:

- enum values are created once and stored as static singleton members
- every enum value exposes `name` and `ordinal`
- `valueOf`, `values`, `names` and `size` are generated automatically
- enum instances are frozen after construction
- enum constructors currently require compile-time constant arguments
- enum constructor bodies are currently limited to direct `this.field = expr` assignments with compile-time evaluable expressions
- `final enum` syntax is accepted

Example:

```pf
enum Color
    RED
    GREEN
    BLUE
end

println(Color.GREEN.name)
println(Color.valueOf("RED").ordinal)
println(Color.size())
```

Example with constructor:

```pf
enum Planet
    MERCURY(3.7)
    EARTH(9.8)

    var gravity: number

    Planet(g: number):
        this.gravity = g
    end

    def weight(mass: number) -> number:
        return mass * this.gravity
    end
end

println(Planet.EARTH.weight(75))
```

## Interfaces

Interfaces are supported, including declarative methods and functional dispatch scenarios used in tests.

Built-in protocol interfaces are also available:

- `Iterable`
- `Unstructured`
- `Sliceable`
- `Indexable`
- `Collection`

See [builtin-interfaces.md](builtin-interfaces.md) for the exact BVM matrix and protocol method names.

Example:

```pf
interface Worker:
    run(task: string) -> string
end
```

## Wrapper objects and unboxing

`polyloft-bvm` distinguishes between primitive syntax and wrapper objects from `polyloft.common`.

Current supported behavior:

- wrappers expose object-style methods
- numeric wrappers unbox in numeric operators
- `Boolean` unboxes in conditions and logical operators
- native string indexing and slicing remain on raw text values
- imported methods and static factories preserve return members for chaining
- common wrappers now expose extra helper methods such as string search/repeat, boolean combinators, numeric sign helpers, and array-like searches on `CharArray` and `Bytes`

Example:

```pf
import polyloft.common { Integer, Boolean, String }

println(Integer(-5).abs().negate().intValue())
println(Boolean(true).negate().booleanValue())
println(String("Polyloft").substring(1, 4).concat("!").toString())
```

## Collections

Runtime-native collections:

- `array`
- `map`
- `tuple`
- `Range`

Nominal interface membership is intentionally narrower than native syntax support.

Current tested behavior:

- `Range` is nominal `Iterable`
- `array` is nominal `Indexable` and `Sliceable`
- `map` is nominal `Indexable`
- `tuple` is nominal `Unstructured` and `Sliceable`
- `string` is nominal `Sliceable`

Native syntax that stays available even without nominal interface membership:

- `for item in array`
- `for key in map`
- `text[index]`
- `tuple[index]`

Library object facades:

- `polyloft.maps.Map`
- `polyloft.maps.HashMap`
- `polyloft.maps.SetMap`

These facades are documented further in [../stdlib.md](../stdlib.md).