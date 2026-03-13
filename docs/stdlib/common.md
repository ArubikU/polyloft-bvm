# polyloft.common

`polyloft.common` provides wrapper and helper classes on top of native runtime values.

```pf
import polyloft.common { Integer, Float, Double, String, Boolean, Char, CharArray, Bytes }
```

## Overview

This module is useful when you want:

- object-style methods on numbers, booleans, and strings
- explicit wrappers around `char`, character arrays, or byte-like arrays
- fluent helper operations that return wrapper instances for chaining

## Integer

Constructor:

- `Integer(value: int)`

Methods:

- `intValue() -> int`
- `floatValue() -> float`
- `doubleValue() -> float`
- `abs() -> Integer`
- `negate() -> Integer`
- `isZero() -> bool`
- `isPositive() -> bool`
- `isNegative() -> bool`
- `signum() -> int`
- `max(other: int) -> Integer`
- `min(other: int) -> Integer`
- `compareTo(other: int) -> int`
- `equals(other: any) -> bool`
- `toString() -> string`
- `unwrap() -> int`

## Float

Constructor:

- `Float(value: float)`

Methods:

- `floatValue() -> float`
- `intValue() -> int`
- `doubleValue() -> float`
- `abs() -> Float`
- `negate() -> Float`
- `sqrt() -> Float`
- `isZero() -> bool`
- `isPositive() -> bool`
- `isNegative() -> bool`
- `signum() -> int`
- `compareTo(other: float) -> int`
- `equals(other: any) -> bool`
- `toString() -> string`
- `unwrap() -> float`

## Double

Constructor:

- `Double(value: float)`

Methods:

- `doubleValue() -> float`
- `floatValue() -> float`
- `intValue() -> int`
- `abs() -> Double`
- `negate() -> Double`
- `sqrt() -> Double`
- `isZero() -> bool`
- `isPositive() -> bool`
- `isNegative() -> bool`
- `signum() -> int`
- `compareTo(other: float) -> int`
- `equals(other: any) -> bool`
- `toString() -> string`
- `unwrap() -> float`

## String

Constructor:

- `String(value: string)`

Methods:

- `length() -> int`
- `isEmpty() -> bool`
- `charAt(index: int) -> char`
- `substring(start: int, finish: int) -> String`
- `concat(other: any) -> String`
- `startsWith(prefix: string) -> bool`
- `endsWith(suffix: string) -> bool`
- `contains(fragment: string) -> bool`
- `indexOf(fragment: string) -> int`
- `repeat(times: int) -> String`
- `equals(other: string) -> bool`
- `toString() -> string`
- `unwrap() -> string`

## Boolean

Constructor:

- `Boolean(value: bool)`

Methods:

- `booleanValue() -> bool`
- `negate() -> Boolean`
- `and(other: bool) -> Boolean`
- `or(other: bool) -> Boolean`
- `isTrue() -> bool`
- `isFalse() -> bool`
- `equals(other: any) -> bool`
- `toString() -> string`
- `unwrap() -> bool`

## Char

Constructor:

- `Char(value: char)`

Methods:

- `charValue() -> char`
- `equals(other: char) -> bool`
- `toString() -> string`
- `unwrap() -> char`

## CharArray

Constructor:

- `CharArray(data: array)`

Methods:

- `length() -> int`
- `isEmpty() -> bool`
- `get(index: int) -> char`
- `contains(value: char) -> bool`
- `indexOf(value: char) -> int`
- `toString() -> string`
- `unwrap() -> array`

## Bytes

Constructor:

- `Bytes(data: array)`

Static helpers:

- `Bytes.fromHex(value: string) -> Bytes`

Methods:

- `length() -> int`
- `size() -> int`
- `isEmpty() -> bool`
- `get(index: int) -> int`
- `contains(value: int) -> bool`
- `indexOf(value: int) -> int`
- `slice(start: int) -> Bytes`
- `concat(other: Bytes) -> Bytes`
- `equals(other: any) -> bool`
- `asHex() -> string`
- `toString() -> string`
- `asArray() -> array`
- `unwrap() -> array`

Notes:

- `Bytes(...)` validates that every element is in the range `0..255`.
- `asArray()` returns a normalized copy, so mutating that array does not mutate the original `Bytes` instance.
- `toString()` currently returns the same hexadecimal text as `asHex()`.

## Example

```pf
import polyloft.common { Integer, Boolean, String }

println(Integer(-5).abs().intValue())
println(Boolean(true).negate().booleanValue())
println(String("polyloft").substring(0, 4).toString())
```

## Notes

- Numeric wrappers unbox in arithmetic operators.
- `Boolean` unboxes in conditions and logical operators.
- Native string indexing still happens on raw `string` values; use `String` when you want object-style methods.