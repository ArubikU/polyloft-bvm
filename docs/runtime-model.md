# Runtime model

This document describes the current execution model of `polyloft-bvm`.

## Primitive scalars

The checker treats these as primitive scalar types:

- `int`
- `float`
- `number`
- `bool`
- `char`
- `nil`
- `void`

`number` remains the broad numeric supertype used for compatibility, while `int` and `float` now keep their own literal and cast behavior.

## Text and runtime-native values

The BVM still keeps several values runtime-native because the VM has dedicated behavior for them:

- `string`
- `array`
- `map`
- `tuple`
- `Range`

Text is represented by the runtime string type. In source annotations, both `string` and `String` currently resolve to that text type. Object-style helper methods live in `polyloft.common.String`.

String indexing returns `char`.

## What stays runtime-native

The following values are not expressed as userland classes because the VM needs direct support for them:

- `string`
- `array`
- `map`
- `tuple`
- `Range`

Reasons:

- indexing and indexed assignment need dedicated opcodes and fast paths
- iteration and destructuring need stable runtime behavior
- some operations are easier to validate and optimize when they remain core values

## What lives in stdlib

Reference-style wrappers and collection facades live in embedded stdlib modules.

Current examples:

- `polyloft.common.Integer`
- `polyloft.common.Float`
- `polyloft.common.Double`
- `polyloft.common.String`
- `polyloft.common.Boolean`
- `polyloft.common.Char`
- `polyloft.common.CharArray`
- `polyloft.common.Bytes`
- `polyloft.maps.Map`
- `polyloft.maps.HashMap`
- `polyloft.maps.SetMap`

## Imports and embedded stdlib

Imports under `polyloft.*` resolve from embedded sources in `internal/modules/stdlib/` before user filesystem roots are considered.

That means the BVM can type-check and execute modules like this without external files:

```pf
import polyloft.common { Integer }
import polyloft.maps { HashMap }
```

## Wrapper semantics

The current target behavior is Java-like, not Python-like.

- primitive operators stay on primitives
- raw text indexing and slicing stay on native strings
- wrapper methods stay on wrapper objects
- wrappers can participate in primitive operators through unboxing
- primitives do not automatically gain wrapper methods

Examples:

```pf
import polyloft.common { Integer, Boolean }

let count = Integer(7)
println(count + 5)
println(count.abs().negate().intValue())

let enabled = Boolean(true)
if enabled:
    println("enabled")
end
```

## Imported type metadata

The checker reconstructs imported callable return types from runtime specs.

This matters for chaining across module boundaries, for example:

```pf
import polyloft.common { Integer, String }
import polyloft.maps { HashMap }

println(Integer(-5).abs().negate().intValue())
println(String("Polyloft").substring(1, 4).concat("!").toString())

let store = HashMap.from({"name": "ana"})
store.put(7, "lucky")
println(store.get(7))
```

The current implementation uses cached reconstruction in the checker to avoid recursive overflows when a class method returns its own type.

## Hashing and maps

`HashMap` and `SetMap` currently use VM-level `hash(...)` to map arbitrary user keys onto internal raw `map` storage.

Current behavior:

- primitives can be hashed
- arrays, tuples, maps and instances are hashed structurally
- hashing is deterministic within the current runtime model
- collection wrappers still sit on top of runtime-native raw maps

This is enough for tested usage, but it is still a VM implementation strategy, not a final language guarantee.