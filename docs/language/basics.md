# Basics

This page covers the core syntax that is already supported in `polyloft-bvm`.

## Variables

The BVM parser and checker currently support:

- `let`
- `var`
- `const`
- `final`
- destructuring with `let`
- explicit type annotations on declarations
- compound assignments such as `+=`, `-=`, `*=`, `/=`

Current mutability semantics:

- `let` and `var` are mutable
- `const` is immutable after initialization
- `final` is treated as a compile-time constant when its initializer can be fully resolved during compilation
- reads of `final` values are inlined into bytecode instead of loaded from a variable slot when possible
- `final` currently rejects non-constant initializers

Example:

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

Destructuring example:

```pf
let pair = (1, "hello")
let left, right = pair
println(left)
println(right)
```

## Functions

Top-level functions are supported with:

- positional parameters
- optional parameter type annotations
- optional return type annotations
- explicit `return`
- closures and lambda capture

Example:

```pf
def add(a: number, b: number) -> number:
    return a + b
end

println(add(2, 3))
```

Lambda example:

```pf
let factor = 3
let multiply = (value) => value * factor
println(multiply(4))

final base = 40
let fortyTwo = () => base + 2
println(fortyTwo())
```

## Expressions

The current BVM supports expressions commonly exercised in tests:

- arithmetic
- comparisons
- equality
- boolean negation and short-circuit logic
- indexing into arrays, maps and text values
- slicing for text values, arrays and tuples
- function calls and method calls
- numeric casts such as `(int)` and `(float)`

Example:

```pf
println(10 % 3)
println("Polyloft"[0])
println("Polyloft"[1...4])
println([1, 2, 3][1])
println((int) 3.9)
```

## What this page does not claim

If a syntax form is documented in the main Polyloft repo but not here, treat it as unsupported or at least undocumented for the BVM until it is validated in this repo.