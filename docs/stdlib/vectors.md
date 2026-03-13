# polyloft.vectors

`polyloft.vectors` provides small immutable vector value types intended to exercise the object-operator model in normal user code.

```pf
import polyloft.vectors { Vec2, Vec3 }
```

## Available Types

- `Vec2`
- `Vec3`

## Operators

Both types support:

- `+` and `-` between vectors of the same dimension
- scalar `*` and reflected scalar `*`
- scalar `/`
- unary `-`
- structural equality through `==`
- stable hashing through `hash(...)`

## Vec2

Constructors and factories:

- `Vec2(x: number, y: number)`
- `Vec2.zero() -> Vec2`

Methods:

- `xValue() -> number`
- `yValue() -> number`
- `dot(other: Vec2Like) -> number`
- `lengthSquared() -> number`
- `length() -> number`
- `normalized() -> Vec2`
- `toString() -> string`

## Vec3

Constructors and factories:

- `Vec3(x: number, y: number, z: number)`
- `Vec3.zero() -> Vec3`

Methods:

- `xValue() -> number`
- `yValue() -> number`
- `zValue() -> number`
- `dot(other: Vec3Like) -> number`
- `cross(other: Vec3Like) -> Vec3`
- `lengthSquared() -> number`
- `length() -> number`
- `normalized() -> Vec3`
- `toString() -> string`

## Notes

- These vectors are stdlib reference types, not core primitives.
- The module intentionally relies on operator overloading hooks such as `__add`, `__mul`, `__eq`, and `__hash` instead of VM-only special cases.