# polyloft.maps

`polyloft.maps` exposes object-style wrappers over raw runtime maps and hashed map or set storage.

```pf
import polyloft.maps { MapLike, SetLike, Map, HashMap, SetMap }
```

## Overview

Use this module when you want:

- object-style helpers over native map values
- arbitrary-key lookup through VM hashing
- set-like behavior backed by hashed storage

Internally, these exports are now split across separate stdlib files under `internal/modules/stdlib/polyloft/maps/`, while `polyloft.maps` remains the stable import-facing facade.

## Contracts

### MapLike

Methods:

- `size() -> int`
- `isEmpty() -> bool`
- `containsKey(key: K) -> bool`
- `__contains(key: K) -> bool`
- `containsValue(expected: V) -> bool`
- `get(key: K) -> V`
- `getOrDefault(key: K, fallback: V) -> V`
- `delete(key: K) -> bool`
- `keys() -> array<K>`
- `values() -> array<V>`

### SetLike

Methods:

- `size() -> int`
- `isEmpty() -> bool`
- `contains(key: T) -> bool`
- `__contains(key: T) -> bool`
- `add(key: T) -> SetLike<T>`
- `addAll(items: array<T>) -> SetLike<T>`
- `clear() -> SetLike<T>`
- `delete(key: T) -> bool`
- `asArray() -> array<T>`
- `values() -> array<T>`

## Map

`Map` is a thin object wrapper over a raw runtime map.

Constructors and factories:

- `Map()`
- `Map.from(entries: map<string, V>) -> Map<V>`

Methods:

- `unwrap() -> map<string, V>`
- `size() -> int`
- `isEmpty() -> bool`
- `containsKey(key: string) -> bool`
- `__contains(key: string) -> bool`
- `containsValue(expected: V) -> bool`
- `get(key: string) -> V`
- `getOrDefault(key: string, fallback: V) -> V`
- `delete(key: any) -> bool`
- `keys() -> array<string>`
- `values() -> array<V>`
- `entries() -> array<Entry<string, V>>`

## HashMap

`HashMap` accepts arbitrary keys and stores them using the VM hash model.

Constructors and factories:

- `HashMap()`
- `HashMap.from(entries: map<K, V>) -> HashMap<K, V>`

Methods:

- `unwrap() -> map<string, V>`
- `size() -> int`
- `isEmpty() -> bool`
- `containsHash(hashed: string) -> bool`
- `containsKey(key: K) -> bool`
- `__contains(key: K) -> bool`
- `containsValue(expected: V) -> bool`
- `get(key: K) -> V`
- `getOrDefault(key: K, fallback: V) -> V`
- `put(key: K, value: V) -> HashMap<K, V>`
- `putAll(entries: map<K, V>) -> HashMap<K, V>`
- `clear() -> HashMap<K, V>`
- `delete(key: K) -> bool`
- `keys() -> array<K>`
- `values() -> array<V>`
- `entries() -> array<Entry<K, V>>`

## SetMap

`SetMap` is a hashed set-like wrapper backed by a raw map.

Constructors and factories:

- `SetMap()`
- `SetMap.from(items: array<T>) -> SetMap<T>`

Methods:

- `unwrap() -> map<string, T>`
- `size() -> int`
- `isEmpty() -> bool`
- `contains(key: T) -> bool`
- `__contains(key: T) -> bool`
- `add(key: T) -> SetMap<T>`
- `addAll(items: array<T>) -> SetMap<T>`
- `clear() -> SetMap<T>`
- `delete(key: T) -> bool`
- `asArray() -> array<T>`
- `values() -> array<T>`

## Example

```pf
import polyloft.maps { Map, HashMap, SetMap }

let view = Map.from({"a": 1, "b": 2})
println(view.size())

let store = HashMap.from({"name": "ana"})
store.put(7, "lucky")
println(store.get(7))

let tags = SetMap.from(["go", "vm"])
tags.add(7)
println(tags.contains(7))
```