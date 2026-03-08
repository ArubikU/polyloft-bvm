# polyloft.function

`polyloft.function` provides generic single-abstract-method interfaces for lambda-oriented code.

```pf
import polyloft.function { Predicate, Consumer, Supplier, Function, BiFunction, UnaryOperator, BinaryOperator }
```

## Interfaces

### Predicate<T>

- `test(value: T) -> bool`

### Consumer<T>

- `accept(value: T) -> void`

### Supplier<T>

- `get() -> T`

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
let starts: Predicate<string> = (value: string) => value[0] == "p"
let maker: Supplier<string> = () => "polyloft"

println(starts.test("poly"))
println(maker.get())
```

## Status

The module imports, inherited functional interfaces, and contextual lambda typing now work together for the tested generic cases.