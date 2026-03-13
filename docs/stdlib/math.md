# polyloft.math

`polyloft.math` exposes a `Math` facade class backed by native runtime functions.

```pf
import polyloft.math { Math }
```

## Constants

- `Math.PI`
- `Math.E`

## Functions

- `Math.abs(x) -> number`
- `Math.floor(x) -> number`
- `Math.ceil(x) -> number`
- `Math.round(x) -> number`
- `Math.sqrt(x) -> number`
- `Math.pow(base, exponent) -> number`
- `Math.sin(x) -> number`
- `Math.cos(x) -> number`
- `Math.tan(x) -> number`
- `Math.min(a, b) -> number`
- `Math.max(a, b) -> number`
- `Math.clamp(value, minValue, maxValue) -> number`
- `Math.random() -> number`

## Notes

- `Math.random()` returns a value in `[0, 1)`.
- `Math.sqrt(...)` rejects negative input.
- The module is import-facing stdlib, but the actual implementations are provided by the runtime-backed `Math` module.