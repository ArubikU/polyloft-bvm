# polyloft.json

`polyloft.json` exposes the import-facing `Json` wrapper class.

```pf
import polyloft.json { Json }
```

## Overview

This module is intentionally small. It forwards to the global runtime-backed `Json` module while giving user code a normal embedded import path.

## Class

### Json

Static methods:

- `Json.stringify(value: any) -> String`
- `Json.parse(json: String) -> any`

## Notes

- `stringify` and `parse` are native at runtime.
- This wrapper exists so embedded modules such as `polyloft.http` can depend on JSON helpers through normal imports.

## Example

```pf
import polyloft.json { Json }

let text = Json.stringify({ "ok": true })
println(text)
println(Json.parse(text)["ok"])
```