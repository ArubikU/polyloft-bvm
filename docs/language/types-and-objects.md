# Types and Objects

This page documents the type system and object model implemented by polyloft-bvm. It focuses on the current checker and runtime behavior, including the places where native runtime values and nominal interfaces intentionally differ.

## Type Names

Primitive scalar type names accepted by the checker include:

- `int`
- `float`
- `number`
- `bool`
- `char`
- `nil`
- `void`

Native runtime value families include:

- `string`
- `array`
- `map`
- `tuple`
- `Range`
- `any`

Compatibility notes:

- `string` and `String` both resolve to the native text type in annotations
- object-style text helpers live in `polyloft.common.String`
- indexing native strings returns `char`

## Numeric Types

polyloft-bvm distinguishes `int` and `float`.

Current rules:

- integer literals infer as `int`
- decimal literals infer as `float`
- `number` remains the broad numeric compatibility type
- `int` is assignable to `float`
- `float` requires an explicit cast before assignment to `int`
- `(int)` and `(float)` casts are supported
- `+`, `-`, `*`, and `%` remain `int` only when both operands are `int`
- `/` produces `float`

```pf
let whole: int = 1
let frac: float = 1.5
let widened: float = whole
let narrowed: int = (int) frac
let mixed: float = whole + frac
```

## Text and Char

The BVM text model distinguishes scalar characters from string values.

- `char` is a separate scalar type
- string indexing returns `char`
- string slicing returns `string`
- `char` and one-character strings compare successfully in common checker and VM cases
- `polyloft.common.Char` and `polyloft.common.String` are wrappers over native values

```pf
let text: string = "Polyloft"
let first: char = text[0]

println(first == "P")
println(text[1...3])
println(String(text).charAt(0))
```

## Union Types and Aliases

Unions and static aliases are supported.

- unions use `|`
- aliases use `type Name = ...`
- aliases are compile-time only and do not create runtime values
- mixed array and map literals preserve union element types when possible
- structural type strings are preserved in bytecode metadata for common runtime checks
- arrays may be written as `array<T>` or `T[]`

```pf
type Scalar = number | string

let values: array<Scalar> = ["a", 1]

def sizeOf(items: array<Scalar>) -> number:
    return len(items)
end

println(sizeOf(values))
```

Invalid assignments remain type errors.

```pf
let broken: array<number> = ["a"]
```

## Classes

The current class model supports:

- fields
- constructors
- instance methods
- static fields and methods
- `public`, `private`, and `protected`
- inheritance with `<`
- interface implementation with `implements`
- abstract classes
- sealed classes

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

Records are supported as compact immutable data types.

- fields are generated from the parameter list
- a constructor is generated automatically
- fields are immutable
- custom constructors are not currently supported

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

Enums are implemented as singleton instance sets with generated helpers.

- each enum value exposes `name` and `ordinal`
- each enum type exposes `valueOf`, `values`, `names`, and `size`
- enum instances are frozen after construction
- constructor arguments must currently be compile-time constants
- constructor bodies are limited to direct `this.field = expr` assignments with compile-time-evaluable expressions
- `final enum` syntax is accepted

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

For more detail, see [enums.md](enums.md).

## Interfaces

Interfaces are supported for declarations, implementation checks, and the functional-dispatch scenarios exercised in the test suite.

Built-in protocol interfaces include:

- `Iterable`
- `Unstructured`
- `Sliceable`
- `Indexable`
- `Collection`

```pf
interface Worker:
    run(task: string) -> string
end
```

See [builtin-interfaces.md](builtin-interfaces.md) for the protocol method matrix and native type behavior.

## Wrappers and Unboxing

polyloft-bvm keeps primitive syntax and wrapper objects separate.

- wrappers from `polyloft.common` expose object-style methods
- numeric wrappers unbox in arithmetic
- `Boolean` unboxes in conditionals and logical operators
- native string indexing and slicing remain available on raw strings
- wrapper methods and factories preserve chainable return values

```pf
import polyloft.common { Integer, Boolean, String }

println(Integer(-5).abs().negate().intValue())
println(Boolean(true).negate().booleanValue())
println(String("Polyloft").substring(1, 4).concat("!").toString())
```

## Native Collections and Facades

Native collection kinds include:

- `array`
- `map`
- `tuple`
- `Range`

Nominal interface membership is intentionally narrower than the native syntax surface.

Current tested behavior:

- `Range` is nominal `Iterable`
- `array` is nominal `Indexable` and `Sliceable`
- `map` is nominal `Indexable`
- `tuple` is nominal `Unstructured` and `Sliceable`
- `string` is nominal `Sliceable`

Native syntax that remains available even without nominal interface membership includes:

- `for item in array`
- `for key in map`
- `text[index]`
- `tuple[index]`

For object-style facades over maps and sets, see [../stdlib/maps.md](../stdlib/maps.md).