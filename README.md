# polyloft-bvm

Polyloft BVM is a fresh bytecode-VM implementation of a reduced Polyloft-like language.

Current slice:
- numbers, booleans, strings and nil
- arithmetic and comparisons
- let bindings, assignment and globals
- if/else
- functions and returns
- for-in over range with optional where guard
- static module access like Sys.time() and Sys.sleep()
- builtin print, println and range

Static module architecture:
- module registration lives in internal/runtime
- core globals are installed from internal/runtime/core.go
- Sys is defined in internal/runtime/sys_module.go
- new modules can use the reusable ModuleBuilder from internal/runtime/registry.go

Current syntax uses `:` and `end` blocks to stay close to Polyloft while keeping the parser small.

Example:

```pf
def add(a, b):
    return a + b
end

let total = 0
for i in range(0, 6) where i < 5:
    total = total + i
end

if add(total, 1) > 10:
    println("ok")
else:
    println("no")
end

let started = Sys.time()
Sys.sleep(1)
println(Sys.time() >= started)
```

Run:

```sh
go run ./cmd/polyloft-bvm run ./testdata/programs/demo.pf
go run ./cmd/polyloft-bvm dump ./testdata/programs/demo.pf
```