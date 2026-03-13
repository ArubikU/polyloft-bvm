# Polyloft BVM CLI Reference

Complete reference for the current `polyloft-bvm` command-line interface.

## Commands

### `polyloft-bvm run`

Execute source code, a compiled module, a bundle, or a project directory.

**Usage:**
```sh
go run ./cmd/polyloft-bvm run <path>
go run ./cmd/polyloft-bvm run [--jit] [--jit-threshold <n>] [--jit-log] <path>
```

**JIT options:**
- `--jit`: forces the JIT warmup threshold to `1`
- `--jit-threshold <n>`: sets the JIT warmup threshold explicitly
- `--jit-log`: writes JIT hotness, compilation and execution events to `stderr`

**Accepted inputs:**
- `.pf` source file
- `.pfbc` compiled module
- `.pfx` bundle
- project directory containing `polyloft.toml`
- `polyloft.toml` directly

**Examples:**
```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm run --jit --jit-log ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm run --jit-threshold 8 ./complex_project
go run ./cmd/polyloft-bvm run ./complex_project
go run ./cmd/polyloft-bvm run ./complex_project.pfx
```

### `polyloft-bvm dump`

Compile or load a target and print bytecode disassembly.

**Usage:**
```sh
go run ./cmd/polyloft-bvm dump <path>
go run ./cmd/polyloft-bvm dump -o <file> <path>
```

**Examples:**
```sh
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm dump -o demo.bytecode.txt ./complex_project
```

### `polyloft-bvm compile`

Compile a source file to `.pfbc`, or a project to a `.pfx` bundle.

**Usage:**
```sh
go run ./cmd/polyloft-bvm compile <path>
go run ./cmd/polyloft-bvm compile -o <file> <path>
```

**Examples:**
```sh
go run ./cmd/polyloft-bvm compile ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm compile ./complex_project
go run ./cmd/polyloft-bvm compile -o release/demo.pfbc ./testdata/programs/demo.pf
```

### `polyloft-bvm check`

Parse and type-check a source target without executing it.

**Usage:**
```sh
go run ./cmd/polyloft-bvm check <path>
```

**Accepted inputs:**
- `.pf` source file
- project directory containing `polyloft.toml`
- `polyloft.toml` directly

**Examples:**
```sh
go run ./cmd/polyloft-bvm check ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm check ./complex_project
```

### `polyloft-bvm runline`

Compile and execute inline source without creating a physical `.pf` file first.

**Usage:**
```sh
go run ./cmd/polyloft-bvm runline <inline-script>
go run ./cmd/polyloft-bvm runline --check <inline-script>
go run ./cmd/polyloft-bvm runline --stdin --path <logical-path>
```

**Options:**
- `--check`: parse and type-check the inline script without executing it
- `--stdin`: read the inline script from stdin instead of from the CLI argument
- `--path <logical-path>`: provide a logical path used in diagnostics and import resolution

**Examples:**
```sh
go run ./cmd/polyloft-bvm runline "println(40 + 2)"
go run ./cmd/polyloft-bvm runline --check "let n: int = 1"
Get-Content .\snippet.pf | go run ./cmd/polyloft-bvm runline --stdin --path inline_check.pf --check
```

### `polyloft-bvm types`

Emit manifest JSON for builtin symbols consumed by tooling.

**Usage:**
```sh
go run ./cmd/polyloft-bvm types <primitives|runtime|stdlib>
go run ./cmd/polyloft-bvm types <primitives|runtime|stdlib> -o <file>
```

**Examples:**
```sh
go run ./cmd/polyloft-bvm types stdlib
go run ./cmd/polyloft-bvm types runtime -o runtime.types.json
```

## Pipeline Behavior

For `run`, `check`, `compile`, and `runline`, the CLI performs the relevant slice of the BVM pipeline:

1. parse source
2. resolve imports and embedded stdlib modules
3. run semantic checks
4. compile to bytecode when required
5. execute, dump or write artifacts when required

## Error Reporting

`polyloft-bvm` now reports structured diagnostics for parse, type-check and runtime failures.

That includes:
- error type such as `ParseError`, `TypeError` or `ValueError`
- file and line information when available
- code context for source-based failures
- runtime stack traces

## Notes

- `polyloft-bvm` is still a VM-focused toolchain, not a full replacement for the main Polyloft CLI.
- Package publishing, registry actions and project scaffolding are still documented in the parent repository, not here.