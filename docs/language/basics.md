# Basics

This page summarizes the core syntax that is available in polyloft-bvm today. The examples here are limited to forms that are parsed, checked, compiled, and executed by the current VM.

## Overview

This page covers:

- variable declarations and mutability
- destructuring
- functions and lambdas
- common expression forms

For control flow such as if, while, for, try, catch, and throw, see [control-flow.md](control-flow.md).

## Variables

polyloft-bvm supports these declaration forms:

- `let`
- `var`
- `const`
- `final`

The current mutability rules are:

- `let` and `var` create mutable bindings
- `const` creates an immutable binding after initialization
- `final` is reserved for compile-time constants
- `final` values are inlined into bytecode when the initializer can be resolved during compilation
- non-constant initializers are rejected for `final`

Type annotations on declarations are supported and may be omitted when inference is sufficient.

```pf
let total: number = 10
var score = 7
const label = "vm"
final enabled = true
final answer = 40 + 2

total += 5
score *= 2

println(answer)
```

### Destructuring

Tuple destructuring with `let` is supported.

```pf
let pair = (1, "hello")
let left, right = pair

println(left)
println(right)
```

## Functions

Top-level functions support:

- positional parameters
- optional parameter annotations
- optional return annotations
- explicit `return`
- lexical capture of outer variables

```pf
def add(a: number, b: number) -> number:
    return a + b
end

println(add(2, 3))
```

### Lambdas

Lambda expressions are supported, including closures over local and top-level bindings.

```pf
let factor = 3
let multiply = (value) => value * factor

final base = 40
let fortyTwo = () => base + 2

println(multiply(4))
println(fortyTwo())
```

## Expressions

The VM supports the expression forms most commonly used by programs and tests in this repository:

- arithmetic and remainder
- equality and comparisons
- logical negation and short-circuit boolean operators
- indexing into arrays, maps, tuples, and strings
- slicing of strings, arrays, and tuples
- function and method calls
- explicit numeric casts such as `(int)` and `(float)`

```pf
println(10 % 3)
println("Polyloft"[0])
println("Polyloft"[1...4])
println([1, 2, 3][1])
println((int) 3.9)
```

## Notes

- The examples on this page document the supported BVM surface, not the full historical Polyloft language.
- If a syntax form appears in the original Polyloft documentation but does not appear in this BVM manual, treat it as unsupported or undocumented until it is validated here.