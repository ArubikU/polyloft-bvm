# Quick Start Guide

## Prerequisites

- Go installed locally
- a `.pf` source file

## Run a program

From the repository root:

```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
```

This command performs the full pipeline:

1. parse source
2. resolve embedded and user imports
3. type-check the program
4. compile to bytecode
5. execute on the VM

## Dump bytecode

To inspect the generated chunk instead of running it:

```sh
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
```

This is useful when working on:

- instruction selection
- fast paths for arrays and maps
- specialized numeric operations
- import and global-slot behavior

## Run tests

Run the full suite:

```sh
go test ./...
```

Run a focused stdlib/import regression:

```sh
go test -run TestEmbeddedStdlibWrappersAndMapsExposeObjectApis ./...
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
```

Run it with:

```sh
go run ./cmd/polyloft-bvm run ./path/to/file.pf
```

## Current CLI limitations

- only `run` and `dump` are available
- there is no standalone package manager in this repo
- stdlib coverage is intentionally partial and driven by tested features
