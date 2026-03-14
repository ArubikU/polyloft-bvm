# Standard Library

The polyloft-bvm standard library is divided into two layers:

- embedded `polyloft.*` modules that are parsed, checked, compiled, and executed through the same pipeline as user code
- runtime-backed global modules such as `Sys`, `Http`, `Json`, `Io`, and `Concurrent`, documented in [../stdlib.md](../stdlib.md)

This section covers the embedded `polyloft.*` modules.

Some embedded modules are thin typed facades over those global runtime modules. That means both of these can be true at the same time:

- `Json` exists as a global runtime module
- `polyloft.json { Json }` exists as an embedded import-facing wrapper class

The same pattern is used for `polyloft.http`, `polyloft.io`, `polyloft.concurrent`, `polyloft.math`, and `polyloft.crypto`.

## Modules

- [common.md](common.md): wrapper and helper types for numbers, booleans, text, chars, and byte-like arrays
- [http.md](http.md): typed HTTP client and server facade classes built on top of the global `Http` module
- [json.md](json.md): import-facing `Json` wrapper around the global `Json` runtime module
- [io.md](io.md): import-facing `IO` wrapper around the global `Io` runtime module
- [concurrent.md](concurrent.md): typed thread, future, and channel wrappers around the global `Concurrent` module
- [maps.md](maps.md): object facades over raw maps and hashed map or set storage
- [collections.md](collections.md): generic collection contracts plus concrete `ArrayList`, `ArrayDeque`, `LinkedList`, and `HashSet`, reexported from `polyloft.collections`
- [vectors.md](vectors.md): `Vec2` and `Vec3` value types with arithmetic operators, hashing, and geometric helpers
- [ui.md](ui.md): object-oriented UI facade (`UIApp`, `UIWindow`, `UINode`, `UIChannel`) backed by the global `Ui` runtime module
- [math.md](math.md): `Math` constants and numeric helpers backed by the runtime module
- [crypto.md](crypto.md): `Crypto` hashing and encoding helpers backed by the runtime module
- [function.md](function.md): generic functional interfaces used by lambdas and single-abstract-method APIs, including `Runnable`

## Notes

- These modules are shipped with the BVM and do not need to exist on disk in a user project.
- Imports such as `import polyloft.common { Integer }` are resolved through the same loader used for user modules.
- Runtime-backed globals remain documented separately in [../stdlib.md](../stdlib.md).