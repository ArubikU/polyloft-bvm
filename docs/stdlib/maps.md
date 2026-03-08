# polyloft.maps

`polyloft.maps` exposes object-style views over raw runtime maps and hashed storage.

```pf
import polyloft.maps { Map, HashMap, SetMap }
```

## Map

`Map` is a thin object view over a raw runtime map keyed by strings.

Constructors and factories:

- `Map()`
- `Map.from(entries: map) -> Map`

Methods:

- `unwrap() -> map`
- `size() -> int`
- `isEmpty() -> bool`
- `containsKey(key: string) -> bool`
- `containsValue(expected: any) -> bool`
- `get(key: string) -> any`
- `getOrDefault(key: string, fallback: any) -> any`
- `delete(key: string) -> bool`
- `keys() -> array`
- `values() -> array`

## HashMap

`HashMap` accepts arbitrary keys and stores them by VM hash.

Constructors and factories:

- `HashMap()`
- `HashMap.from(entries: map) -> HashMap`

Methods:

- `unwrap() -> map`
- `size() -> int`
- `isEmpty() -> bool`
- `containsHash(hashed: string) -> bool`
- `containsKey(key: any) -> bool`
- `containsValue(expected: any) -> bool`
- `get(key: any) -> any`
- `getOrDefault(key: any, fallback: any) -> any`
- `put(key: any, value: any) -> HashMap`
- `putAll(entries: map) -> HashMap`
- `clear() -> HashMap`
- `delete(key: any) -> bool`
- `keys() -> array`
- `values() -> array`

## SetMap

`SetMap` is a hashed set-like wrapper backed by a raw map.

Constructors and factories:

- `SetMap()`
- `SetMap.from(items: array) -> SetMap`

Methods:

- `unwrap() -> map`
- `size() -> int`
- `isEmpty() -> bool`
- `contains(key: any) -> bool`
- `add(key: any) -> SetMap`
- `addAll(entries: map) -> SetMap`
- `clear() -> SetMap`
- `delete(key: any) -> bool`
- `values() -> array`

## Example

```pf
let view = Map.from({"a": 1, "b": 2})
println(view.size())

let store = HashMap.from({"name": "ana"})
store.put(7, "lucky")
println(store.get(7))

let tags = SetMap.from(["go", "vm"])
tags.add(7)
println(tags.contains(7))
```