# Enums

This page documents the enum support that exists today in `polyloft-bvm`.

The implementation is intentionally optimized around singleton enum values, similar to a compact class-based model rather than a dynamic map of names.

## Supported Syntax

Basic enum:

```pf
enum Color
    RED
    GREEN
    BLUE
end
```

`final enum` is also accepted:

```pf
final enum Mode
    OFF
    ON
end
```

## Built-in Enum Members

Every enum value exposes:

- `name`
- `ordinal`

Every enum type exposes:

- `valueOf(name)`
- `values()`
- `names()`
- `size()`

Example:

```pf
enum Status
    PENDING
    ACTIVE
    DONE
end

let current = Status.ACTIVE
println(current.name)
println(current.ordinal)
println(Status.size())
println(Status.valueOf("DONE").name)
```

## Enum Constructors

Enums can declare fields, a constructor, and instance methods.

Example:

```pf
enum Planet
    MERCURY(3.7)
    EARTH(9.8)
    MARS(3.71)

    var gravity: number

    Planet(g: number):
        this.gravity = g
    end

    def weight(mass: number) -> number:
        return mass * this.gravity
    end
end

println(Planet.MARS.gravity)
println(Planet.EARTH.weight(75))
```

## Current BVM Constraints

The current implementation is intentionally strict so enum instances can be built ahead of execution and reused efficiently.

- enum values are singleton instances created during compilation/runtime setup of the class object
- enum constructor arguments must be compile-time constants
- enum constructor bodies are currently limited to direct assignments to `this.field`
- those assigned expressions must also be compile-time evaluable
- enum instances are frozen after construction
- enum construction through `new` is not part of the supported surface

## Notes on Optimization

Current optimization-oriented behavior:

- enum constants are stored as static singleton members on the generated class object
- equality between enum values works by singleton identity
- helper methods such as `valueOf`, `values`, `names`, and `size` are attached once per enum type
- `final` aliases that reference enum values can also be inlined when the initializer is compile-time resolvable

## Related Pages

- [Basics](basics.md)
- [Types and Objects](types-and-objects.md)
- [../QUICK_REFERENCE.md](../QUICK_REFERENCE.md)