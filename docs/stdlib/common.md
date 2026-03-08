# polyloft.common

`polyloft.common` contains wrapper and helper classes that sit on top of native runtime values.

```pf
import polyloft.common { Integer, Float, Double, String, Boolean, Char, CharArray, Bytes }
```

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

Methods:

- `length() -> int`
- `size() -> int`
- `isEmpty() -> bool`
- `get(index: int) -> int`
- `contains(value: int) -> bool`
- `indexOf(value: int) -> int`
- `asArray() -> array`
- `unwrap() -> array`

## Behavior Notes

- Numeric wrappers unbox in arithmetic operators.
- `Boolean` unboxes in conditions and logical operators.
- Native text indexing still happens on raw `string` values; import `String` when you want object-style methods.