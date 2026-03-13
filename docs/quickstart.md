# Quick Start Guide

## Prerequisites

- Go installed locally
- a `.pf` source file or a Polyloft project directory

## Run Your First Program

From the `polyloft-bvm` repository root:

```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
```

This command performs the full source pipeline:

1. parse source
2. resolve user imports and embedded stdlib modules
3. type-check the program
4. compile to bytecode
5. execute on the VM

## Compile Artifacts

Compile a single file to a `.pfbc` module:

```sh
go run ./cmd/polyloft-bvm compile ./testdata/programs/demo.pf
```

Compile a project directory to a `.pfx` bundle:

```sh
go run ./cmd/polyloft-bvm compile ./complex_project
```

## Dump Bytecode

To inspect disassembly instead of executing the program:

```sh
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
```

This is useful when working on:

- instruction selection
- fast numeric paths
- global-slot allocation
- imports and bundled modules
- exception handlers and control-flow lowering

## Run Tests

Run the full suite:

```sh
go test ./...
```

Run a focused regression:

```sh
go test -run TestExceptionsAndAnnotationsExample ./...
```

## Your First Program

```pf
import polyloft.common { Integer, Boolean, String }
import polyloft.maps { HashMap }

let count = Integer(10)
println(count.negate().abs().intValue())

let enabled = Boolean(true)
if enabled:
    println("ok")
end

let label = String("vm")
println(label.repeat(2).toString())

let store = HashMap.from({"lang": "polyloft"})
println(store.get("lang"))

try:
    println(10 / 0)
catch (err: ValueError):
    println(err.message)
end
```

Run it with:

```sh
go run ./cmd/polyloft-bvm run ./path/to/file.pf
```

## Current Scope

- the CLI supports `run`, `dump` and `compile`
- bundles and compiled modules are supported
- diagnostics are implementation-focused and runtime-aware
- stdlib coverage is partial by design and driven by tests
