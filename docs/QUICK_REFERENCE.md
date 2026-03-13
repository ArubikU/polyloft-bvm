# Polyloft BVM Quick Reference

Complete cheat sheet for the `polyloft-bvm` language and runtime surface that is currently implemented.

## CLI

```sh
go run ./cmd/polyloft-bvm run ./file.pf
go run ./cmd/polyloft-bvm check ./file.pf
go run ./cmd/polyloft-bvm run ./project-dir
go run ./cmd/polyloft-bvm runline "println(1 + 1)"
go run ./cmd/polyloft-bvm dump ./file.pf
go run ./cmd/polyloft-bvm compile ./file.pf
go run ./cmd/polyloft-bvm types stdlib
go test ./...
```

## Scalar Types

Primitive scalar types used by the checker today:

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
let arr = [1, 2, 3]           // array
let table = {"a": 1, "b": 2} // map
let pair = (1, "x")          // tuple
let seq = range(0, 5)         // Range

println(rawText[0])           // char
println(rawText[1...3])       // string
```

## Type Aliases and Unions

```pf
type Scalar = number | string

let values: array<Scalar> = ["a", 1]  # or Scalar[] (both equivalent)
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
println(sqrt(81))
println(delete({"a": 1}, "a"))
println(keys({"a": 1, "b": 2})[0])
println(values({"a": 1, "b": 2})[0])
println(hash("polyloft"))
```

## Runtime Modules

```pf
println(Sys.time())
Io.write_file("./demo.txt", "hello")
println(Io.read_file("./demo.txt"))
println(Json.stringify({"ok": true}))
```

Available runtime-backed modules:

- `Sys`
- `Http`
- `Json`
- `Io`
- `Concurrent`

These are the low-level global modules. When you want typed wrapper classes, import the embedded facades instead:

```pf
import polyloft.json { Json }
import polyloft.io { IO }
import polyloft.concurrent { Thread, CompletableFuture, Channel }
```

## Embedded Imports

```pf
import polyloft.common { Integer, String, Boolean, CharArray, Bytes }
import polyloft.http { Http, HttpServer }
import polyloft.json { Json }
import polyloft.io { IO }
import polyloft.concurrent { Thread, CompletableFuture, Channel }
import polyloft.maps { Map, HashMap, SetMap }
import polyloft.collections { List, Deque, Set, ArrayList, ArrayDeque, HashSet }
import polyloft.function { Predicate, Supplier, Runnable }
```

## Wrapper Objects

```pf
let count = Integer(-5)
println(count.abs().negate().intValue())
println(count.signum())
println(Float(9.0).sqrt().unwrap())

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

let queue: Deque<string> = ArrayDeque()
queue.addLast("a")
queue.addFirst("b")
println(queue.removeFirst())
```

## Functional Interfaces

```pf
let starts: Predicate<string> = (value: string) => value[0] == "p"
let maker: Supplier<string> = () => "polyloft"
let done: Runnable = () => println("done")

println(starts.test("poly"))
println(maker.get())
done.run()
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

## Exceptions

```pf
try:
    throw "boom"
catch err:
    println(err)
end

try:
    println(10 / 0)
catch (err: ValueError):
    println(err.message)
end
```

Built-in exception classes available in the runtime:

- `Exception`
- `RuntimeError`
- `NameError`
- `TypeError`
- `ValueError`
- `ArityError`
- `IndexError`
- `KeyError`
- `IOException`
- `FileNotFoundException`
- `NetworkError`
- `TimeoutError`

## Annotations

```pf
abstract class Shape:
    abstract def area() -> number
end

class Square extends Shape:
    @Override
    def area() -> number:
        return 4
    end
end
```

Current method annotations recognized by the checker/compiler:

- `@Override`
- `@Equals`
- `@Hash`

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

- Package-manager and registry commands are not part of `polyloft-bvm`.
- Embedded stdlib coverage is intentionally small and test-driven.