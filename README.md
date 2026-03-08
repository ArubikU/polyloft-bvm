# polyloft-bvm

`polyloft-bvm` is the bytecode VM prototype for Polyloft. It focuses on a smaller, testable language slice with explicit runtime rules, embedded stdlib modules, and a straightforward compiler pipeline.

## Current scope

Implemented and exercised in tests:

- primitives: `number`, `bool`, `string`, `nil`
- runtime-native core values: `array`, `map`, `tuple`, `range`
- arithmetic, comparisons, boolean operators and conditionals
- `let` bindings, reassignment, globals and indexing
- functions, methods, classes, static members, records and interfaces
- `for` over `range`, arrays, tuples and maps
- embedded imports such as `polyloft.common` and `polyloft.maps`
- core builtins such as `print`, `println`, `range`, `len`, `delete`, `keys`, `values` and `hash`
- module access like `Sys.time()` and `Sys.sleep()`

## Design model

The BVM deliberately separates primitive syntax from library objects:

- real primitives stay operator-driven: `number`, `bool`, `string`, `nil`
- `array`, `map`, `tuple` and `range` stay runtime-native because the VM needs direct support for indexing, iteration and destructuring
- wrapper/reference classes live in embedded stdlib modules such as `polyloft.common`
- collection objects like `Map`, `HashMap` and `SetMap` live in `polyloft.maps`
- callable members on imported classes and modules are reconstructed from runtime specs and used by the checker for discoverability and chaining

Java-like behavior currently implemented:

- `Integer`, `Float` and `Double` participate in numeric operators through unboxing
- `Boolean` participates in `if`, `where`, `!`, `&&`, `||` and equality through unboxing
- imported wrapper methods and static factories can now preserve return members for chaining
- primitive values are still not treated as instances of wrapper classes

## CLI

The current CLI is intentionally small:

```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
```

- `run`: parses, type-checks, compiles and executes a `.pf` file
- `dump`: emits bytecode disassembly for the compiled file

## Documentation

- [docs/README.md](docs/README.md)
- [docs/quickstart.md](docs/quickstart.md)
- [docs/runtime-model.md](docs/runtime-model.md)
- [docs/language/README.md](docs/language/README.md)
- [docs/stdlib.md](docs/stdlib.md)
- [docs/architecture.md](docs/architecture.md)
- [docs/QUICK_REFERENCE.md](docs/QUICK_REFERENCE.md)

## Example

```pf
import polyloft.common { Integer, String }
import polyloft.maps { HashMap }

let count = Integer(-5)
println(count.abs().negate().intValue())

let text = String("Polyloft")
println(text.substring(1, 4).concat("!").toString())

let store = HashMap.from({"name": "ana"})
store.put(7, "lucky")
println(store.get("name"))
println(store.get(7))
```

## Development

Useful commands while iterating on the VM:

```sh
go test ./...
go test -run TestEmbeddedStdlibWrappersAndMapsExposeObjectApis ./...
```