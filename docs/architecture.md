# Architecture

This page describes the current internal architecture of `polyloft-bvm` for contributors.

## High-level pipeline

The current execution path is:

1. lex source into tokens
2. parse tokens into AST
3. resolve imports and build a runtime registry
4. type-check the AST against the registry
5. compile AST to bytecode
6. execute bytecode on the VM

The CLI entry point is in `cmd/polyloft-bvm/main.go` and currently exposes only `run` and `dump`.

## Package map

Core internal packages:

- `internal/token`: token definitions
- `internal/lexer`: source scanning
- `internal/ast`: syntax tree nodes
- `internal/parser`: parser for declarations, expressions and statements
- `internal/sema`: type checker and semantic validation
- `internal/modules`: import loading and embedded stdlib resolution
- `internal/runtime`: builtins, runtime registry and module registration
- `internal/compiler`: AST to bytecode compiler
- `internal/bytecode`: instruction and chunk definitions
- `internal/value`: runtime value model
- `internal/vm`: bytecode interpreter

## Parser responsibilities

The parser currently handles the tested language slice, including:

- imports
- top-level declarations
- classes, interfaces and records
- methods, constructors and static members
- `if`, `for` and `switch`
- expressions, indexing and slicing
- lambda syntax used in tests

The main parser entry point is `internal/parser/parser.go`.

## Import loading and embedded stdlib

`internal/modules/loader.go` is responsible for:

- parsing imported files
- resolving project roots and package candidates
- loading embedded `polyloft.*` stdlib modules
- type-checking imported modules
- compiling and executing modules to materialize exports
- attaching export specs used later by the checker

An important design point is that embedded stdlib modules are not special-cased at semantic level. They are parsed, checked, compiled and executed through the same pipeline as user modules.

## Registry and specs

`internal/runtime/registry.go` provides two things:

- the runtime globals map used by the VM
- a parallel spec graph used by the checker for imported symbols

Specs describe:

- callable parameter and return types
- module members
- class members and instance members
- abstract, sealed, interface and record metadata

This spec layer is what makes imported members discoverable to the semantic checker.

## Semantic checking

`internal/sema` validates the current program against the registry and imported specs.

Current responsibilities include:

- variable and assignment typing
- method and function call checking
- access control checks
- interface conformance
- abstract and sealed restrictions
- imported type reconstruction for chained method returns

One recent architectural constraint is that imported return-type reconstruction now uses caches to avoid recursive overflow when a method returns its own class type.

## Compilation

`internal/compiler/compiler.go` lowers AST into bytecode.

Notable concerns in the compiler:

- global-slot collection and script-local decisions
- closure capture
- class and method compilation
- static members
- fast paths for arrays, maps and pure numeric methods
- keeping module compilation separate from script compilation when globals cannot be optimized the same way

`CompileWithRegistry` is the usual entry point for top-level execution after imports are prepared.

## VM execution

`internal/vm/vm.go` executes bytecode chunks using stack frames and runtime values from `internal/value`.

Notable runtime responsibilities include:

- global slot initialization
- function, method, constructor and builtin calls
- object field access
- fast paths for specialized operations
- wrapper unboxing for numeric and boolean operations
- VM-level `hash(...)` evaluation for hashed collection wrappers

## Runtime values

`internal/value` defines the value model used by the VM:

- primitive values
- arrays, tuples, maps and ranges
- classes and instances
- builtins and modules
- closures and bound methods

This package is also where structural equality support and object metadata live.

## Where to make changes

Typical change locations:

- new syntax: `internal/token`, `internal/lexer`, `internal/parser`, then `internal/sema` and `internal/compiler`
- new builtin/runtime helper: `internal/runtime`
- new embedded stdlib module: `internal/modules/stdlib/`
- import semantics: `internal/modules/loader.go`
- runtime execution semantics: `internal/vm` and `internal/value`

## Ground truth

For architecture work, keep these aligned:

- implementation under `internal/`
- current docs in this folder
- executable behavior covered by `e2e_test.go`

If these diverge, prefer the tested implementation and update the docs accordingly.