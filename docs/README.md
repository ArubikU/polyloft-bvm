# Polyloft BVM Documentation

Welcome to the implementation reference for `polyloft-bvm`.

These docs follow the style of the main Polyloft documentation, but they only describe the language slice and runtime behavior that the bytecode VM actually supports today. If something differs from the parent repository, the BVM docs are the source of truth for BVM work.

The standard library currently has two public surfaces:

- global runtime-backed modules such as `Sys`, `Http`, `Json`, `Io`, and `Concurrent`
- embedded import-facing modules under `polyloft.*`, including typed wrappers such as `polyloft.http`, `polyloft.io`, `polyloft.json`, and `polyloft.concurrent`

When both exist, prefer the `polyloft.*` import in user code when you want typed classes and method contracts. The global runtime module is the low-level native surface those wrappers delegate to.

## Table of Contents

### Getting Started
- [Quick Start Guide](quickstart.md)
- [Installation and Project Overview](../README.md)
- [CLI Reference](CLI.md)

### Language Fundamentals

#### Basics and Types
- [Language Overview](language/README.md)
- [Basics](language/basics.md)
- [Types and Objects](language/types-and-objects.md)
- [Built-in Interfaces](language/builtin-interfaces.md)
- [Enums](language/enums.md)

#### Program Structure
- [Imports](language/imports.md)
- [Control Flow](language/control-flow.md)

### Standard Library

#### Runtime-backed Modules
- [Stdlib Overview](stdlib.md)
- [Stdlib Index](stdlib/README.md)
- `Sys` - clocks and sleep helpers
- `Http` - low-level HTTP client and server native bindings
- `Json` - low-level JSON parse and stringify helpers
- `Io` - low-level filesystem helpers
- `Concurrent` - low-level thread, future, and channel helpers

#### Embedded Modules
- [polyloft.common](stdlib/common.md)
- [polyloft.http](stdlib/http.md)
- [polyloft.json](stdlib/json.md)
- [polyloft.io](stdlib/io.md)
- [polyloft.concurrent](stdlib/concurrent.md)
- [polyloft.maps](stdlib/maps.md)
- [polyloft.collections](stdlib/collections.md)
- [polyloft.vectors](stdlib/vectors.md)
- [polyloft.math](stdlib/math.md)
- [polyloft.crypto](stdlib/crypto.md)
- [polyloft.function](stdlib/function.md)

### Reference
- [Quick Reference](QUICK_REFERENCE.md)
- [Runtime Model](runtime-model.md)
- [Architecture](architecture.md)

## Quick Reference

### Hello World
```pf
println("Hello, World!")
```

### Variables
```pf
let name = "Polyloft"
var count = 1
const build = "debug"
final limit = 10
```

### Functions
```pf
def square(value: number) -> number:
	return value * value
end

println(square(4))
```

### Classes and Annotations
```pf
abstract class Worker:
	abstract def run() -> String
end

class DemoWorker extends Worker:
	@Override
	def run() -> String:
		return "ready"
	end
end
```

### Control Flow and Exceptions
```pf
try:
	println(10 / 0)
catch (err: ValueError):
	println(err.message)
end
```

### Imports and Runtime Modules
```pf
import polyloft.common { Integer, String }
import polyloft.maps { HashMap }
import polyloft.json { Json }
import polyloft.io { IO }

println(Sys.time())
println(Json.stringify({"name": "ana"}))
println(Io.exists("./demo.txt"))
println(IO.exists("./demo.txt"))

let count = Integer(-5)
println(count.abs().intValue())

let store = HashMap.from({"lang": "polyloft"})
println(store.get("lang"))
```

## Scope Note

These pages are implementation-driven. They are meant to answer one question clearly: what does `polyloft-bvm` support right now?

Use the parent Polyloft docs for the broader language vision. Use this folder for the behavior that is compiled, executed and tested in the BVM.