# polyloft.function

`polyloft.function` provides generic single-abstract-method interfaces for lambda-oriented code.

```pf
import polyloft.function { Predicate, Consumer, Supplier, Runnable, Function, BiFunction, UnaryOperator, BinaryOperator }
```

## Interfaces

### Predicate<T>

- `test(value: T) -> bool`

### Consumer<T>

- `accept(value: T) -> void`

### Supplier<T>

- `get() -> T`

### Runnable

- `run() -> void`

### Function<T, R>

- `apply(value: T) -> R`

### BiFunction<T, U, R>

- `apply(left: T, right: U) -> R`

### UnaryOperator<T>

Extends `Function<T, T>`.

### BinaryOperator<T>

Extends `BiFunction<T, T, T>`.

## Wildcards

The current checker accepts wildcard arguments in imported annotations such as:

```pf
Consumer<? super string>
Iterable<? extends number>
```

## Example

```pf
import polyloft.function { Predicate, Supplier, Runnable }

let starts: Predicate<string> = (value: string) => value[0] == "p"
let maker: Supplier<string> = () => "polyloft"
let done: Runnable = () => println("done")

println(starts.test("poly"))
println(maker.get())
done.run()
```

## Notes

- These interfaces are intended for contextual lambda typing and single-abstract-method contracts.
- Imported functional interfaces, inherited functional interfaces, and tested generic lambda cases currently work together in the BVM checker.
- `Runnable` is used directly by `polyloft.concurrent` for `finally(...)` callbacks.