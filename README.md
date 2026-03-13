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

Experimental execution path now available:

- the VM can JIT-lower a small safe subset of arithmetic bytecode functions into an OS-specific instruction stream after a hotness threshold is reached
- Windows and non-Windows targets currently use distinct backend encodings selected at build time
- unsupported functions still run through the interpreter with full fallback behavior

## CLI

The current CLI covers execution, validation, disassembly, inline snippets, artifact generation and builtin type manifests:

```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm run --jit --jit-log ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm check ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm compile ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm runline "println(40 + 2)"
go run ./cmd/polyloft-bvm types stdlib
```

- `run`: executes a source file, compiled module, bundle or project
- `run --jit`: lowers the JIT warmup threshold to `1`
- `run --jit-threshold <n>`: configures the JIT hotness threshold explicitly
- `run --jit-log`: emits JIT hot/compile/execute events to `stderr`
- `check`: parses and type-checks a source file or project without running it
- `dump`: emits bytecode disassembly for a source file or compiled artifact
- `compile`: writes `.pfbc` modules or `.pfx` project bundles
- `runline`: executes inline Polyloft code, with optional `--check`, `--stdin` and `--path`
- `types`: emits manifest JSON for `primitives`, `runtime` or `stdlib`

## Documentation

- [docs/README.md](docs/README.md)
- [docs/CLI.md](docs/CLI.md)
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