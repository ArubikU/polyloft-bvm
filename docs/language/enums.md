# Enums

This page documents the enum model implemented by polyloft-bvm. Enums are compiled as singleton instance sets with generated helpers, rather than as a dynamic map of names to values.

## Declaring Enums

Basic enum syntax:

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

## Generated Members

Every enum value exposes:

- `name`
- `ordinal`

Every enum type exposes:

- `valueOf(name)`
- `values()`
- `names()`
- `size()`

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

## Enum Constructors and Methods

Enums may declare fields, a constructor, and instance methods.

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

## Current Constraints

The current implementation is intentionally strict so enum instances can be created once and reused efficiently.

- enum values are singleton instances stored as static members on the generated class object
- constructor arguments must be compile-time constants
- constructor bodies are currently limited to direct `this.field = expr` assignments
- those assigned expressions must also be compile-time evaluable
- enum instances are frozen after construction
- constructing enum values with `new` is not part of the supported surface

## Notes

- Equality between enum values works by singleton identity.
- Helper methods such as `valueOf`, `values`, `names`, and `size` are generated once per enum type.
- `final` aliases that reference enum values may be inlined when the initializer is compile-time resolvable.

## Related Pages

- [basics.md](basics.md)
- [types-and-objects.md](types-and-objects.md)
- [../QUICK_REFERENCE.md](../QUICK_REFERENCE.md)