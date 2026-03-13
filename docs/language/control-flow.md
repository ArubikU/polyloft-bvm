# Control Flow

This page summarizes the control flow constructs that are currently supported in `polyloft-bvm`.

## If / else

Multi-line `if` blocks are supported.

```pf
if score > 10:
    println("high")
else:
    println("low")
end
```

Conditions accept primitive booleans and `polyloft.common.Boolean` through unboxing.

## For loops

The current BVM supports `for` iteration over:

- `range`
- arrays
- tuples
- maps

It also supports `where` guards.

```pf
for i in range(0, 6) where i < 5:
    println(i)
end
```

Destructuring is also supported in iteration:

```pf
for key, value in {"a": 1, "b": 2}:
    println(key)
    println(value)
end
```

## Switch

`switch` is supported with:

- value cases
- type-match cases
- default cases

Example:

```pf
switch value:
    case 1:
        println("one")
    case (n: number):
        println("number")
    default:
        println("other")
end
```

## Try / catch / throw

`polyloft-bvm` now supports structured exception handling.

Basic form:

```pf
try:
    throw "boom"
catch err:
    println(err)
end
```

Typed catches are also supported when the runtime value matches the requested class:

```pf
try:
    println(10 / 0)
catch (err: ValueError):
    println(err.message)
end
```

Notes:

- `throw` accepts plain values or instances
- runtime errors such as division by zero are converted into structured exceptions
- typed catches work with the built-in exception classes and user classes
- if no catch matches, the VM reports a formatted runtime diagnostic with stack trace

## Logical operators

The BVM supports:

- `!`
- `&&`
- `||`

Short-circuit behavior is covered by tests.

## Notes

This page only documents constructs that are actively represented in the BVM test suite. Other control flow forms documented in the parent repository should not be assumed to exist here unless they are added and tested in `polyloft-bvm`.