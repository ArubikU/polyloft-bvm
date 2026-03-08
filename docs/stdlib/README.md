# Standard Library

The `polyloft-bvm` standard library is embedded under `internal/modules/stdlib/` and compiled through the same parse, sema, compile, and VM pipeline as user code.

Current module pages:

- [polyloft.common](common.md) - wrapper and helper types for numbers, booleans, text, chars, and byte-like arrays
- [polyloft.maps](maps.md) - object facades over raw maps plus hashed map/set helpers
- [polyloft.collections](collections.md) - collection contracts and `HashSet`
- [polyloft.function](function.md) - generic single-abstract-method interfaces for lambda-oriented code

Runtime-backed builtins that are not sourced from `polyloft.*` modules are still documented in [../stdlib.md](../stdlib.md).