# Polyloft BVM Quick Reference

Complete cheat sheet for the `polyloft-bvm` feature slice that is currently implemented.

## CLI

```sh
go run ./cmd/polyloft-bvm run ./file.pf
go run ./cmd/polyloft-bvm dump ./file.pf
go test ./...
```

## Scalar Types

Primitive scalars used by the checker today:

- `int`
- `float`
- `number`
- `bool`
- `char`
- `nil`
- `void`

Example:

```pf
let whole: int = 10
let frac: float = 2.5
let mixed: number = whole + frac
let enabled: bool = true
let initial: char = "P"[0]
let nothing: nil = nil
```

## Text and Native Values

```pf
let rawText: string = "hello"
let arr = [1, 2, 3]             // array
let table = {"a": 1, "b": 2} // map
let pair = (1, "x")            // tuple
let seq = range(0, 5)           // Range

println(rawText[0])             // char
println(rawText[1...3])         // string
```

## Type Aliases and Unions

```pf
type Scalar = number | string

let values: array<Scalar> = ["a", 1]  # or Scalar[] (both equivalent)
# new-array examples
let arr: int[] = new int[3]{0,1,2}
println(values[0])
println(values[1])
```

## Built-in Interfaces

```pf
let seq: Iterable = range(0, 5)
let slots: Indexable = [10, 20, 30]
let text: Sliceable = "polyloft"

println(slots[1])
slots[1] = 99
println(text[1...3])
```

Nominal matrix used by BVM today:

- `Range` -> `Iterable`
- `array` -> `Indexable`, `Sliceable`
- `map` -> `Indexable`
- `tuple` -> `Unstructured`, `Sliceable`
- `string` -> `Sliceable`

## Core Builtins

```pf
println("hello")
println(len([1, 2, 3]))
println(delete({"a": 1}, "a"))
println(keys({"a": 1, "b": 2})[0])
println(values({"a": 1, "b": 2})[0])
println(hash("polyloft"))
```

## Embedded Imports

```pf
import polyloft.common { Integer, String, Boolean, CharArray, Bytes }
import polyloft.maps { Map, HashMap, SetMap }
import polyloft.collections { Set, HashSet }
import polyloft.function { Predicate, Supplier }
```

## Wrapper Objects

```pf
let count = Integer(-5)
println(count.abs().negate().intValue())
println(count.signum())

let text = String("Polyloft")
println(text.charAt(0))
println(text.substring(1, 4).concat("!").toString())
println(text.startsWith("Poly"))

let enabled = Boolean(true)
println(enabled.negate().booleanValue())
println(enabled.and(false).booleanValue())
```

## Collections

```pf
let view = Map.from({"a": 1, "b": 2})
println(view.size())
println(view.get("a"))

let store = HashMap.from({"name": "ana"})
store.put(7, "lucky")
store.put(true, "on")
println(store.get(7))
println(store.containsKey(true))

let tags: Set = HashSet.from(["go", "vm", "go"])
println(tags.contains("go"))
println(tags.asArray())
```

## Functional Interfaces

```pf
let starts: Predicate<string> = (value: string) => value[0] == "p"
let maker: Supplier<string> = () => "polyloft"

println(starts.test("poly"))
println(maker.get())
```

## Enums

```pf
enum Color
    RED
    GREEN
    BLUE
end

println(Color.GREEN.name)
println(Color.valueOf("RED").ordinal)
println(Color.size())
```

## Current Rules

```pf
let count = Integer(7)
println(count + 5)        // wrappers unbox in operators
println(count.intValue()) // methods stay on wrappers

let enabled = Boolean(true)
if enabled:
    println("ok")        // Boolean unboxes in conditions
end
```

## Current Limits

- CLI support is limited to `run` and `dump`.
- Embedded stdlib coverage is intentionally small and test-driven.
