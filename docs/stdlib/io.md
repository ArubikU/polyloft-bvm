# polyloft.io

`polyloft.io` exposes the import-facing `IO` wrapper class around the global runtime-backed `Io` module.

```pf
import polyloft.io { IO }
```

## Overview

Use `polyloft.io` when you want filesystem helpers through a normal embedded import instead of the low-level global `Io` module.

## Class

### IO

Static methods:

- `readFile(path: String) -> String`
- `readFile(path: String, encoding: String) -> String`
- `writeFile(path: String, content: String) -> void`
- `writeFile(path: String, content: String, encoding: String) -> void`
- `appendFile(path: String, content: String) -> void`
- `exists(path: String) -> bool`
- `delete(path: String) -> void`
- `mkdir(path: String) -> void`
- `readDir(path: String) -> array`
- `isDir(path: String) -> bool`
- `isFile(path: String) -> bool`
- `getFileSize(path: String) -> int`
- `getFileInfo(path: String) -> map`

## Notes

- the `encoding` overloads currently ignore the encoding argument and behave like the plain overloads
- this facade currently covers file and directory helpers only; lower-level handle and buffer functions remain on the global `Io` module

## Example

```pf
import polyloft.io { IO }

IO.writeFile("./demo.txt", "hello")
println(IO.readFile("./demo.txt"))
println(IO.exists("./demo.txt"))
```