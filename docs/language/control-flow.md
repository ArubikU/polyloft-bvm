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

## Logical operators

The BVM supports:

- `!`
- `&&`
- `||`

Short-circuit behavior is covered by tests.

## Notes

This page only documents constructs that are actively represented in the BVM test suite. Other control flow forms documented in the parent repository should not be assumed to exist here unless they are added and tested in `polyloft-bvm`.