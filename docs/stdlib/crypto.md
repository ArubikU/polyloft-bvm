# polyloft.crypto

`polyloft.crypto` exposes a `Crypto` facade class for hashing and text encoding helpers backed by native runtime functions.

```pf
import polyloft.crypto { Crypto }
```

## Hashing

- `Crypto.md5(data) -> String`
- `Crypto.sha1(data) -> String`
- `Crypto.sha256(data) -> String`
- `Crypto.sha512(data) -> String`

## Encoding

- `Crypto.base64Encode(data) -> String`
- `Crypto.base64Decode(data) -> String`
- `Crypto.hexEncode(data) -> String`
- `Crypto.hexDecode(data) -> String`

## Notes

- All helpers currently operate on text values and return text.
- Decode helpers return runtime errors when the input is not valid Base64 or hexadecimal data.
- The module is import-facing stdlib, but the concrete implementations are provided by the runtime-backed `Crypto` module.