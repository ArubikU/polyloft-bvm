# Mejoras stdlib BVM

## Estado

- [x] Fase 1.A de Bytes: validación `0..255`, clonación defensiva, `asHex()`, `fromHex(...)`, `equals(...)`, `concat(...)`, `slice(start)` y `toString()` hexadecimal simple.
- [ ] Fase 1.B de Bytes: historia textual/binaria completa (`asString`, `asBase64`, `fromBase64`, conversiones numéricas) y revisar la integración de `hash(...)` global con `Bytes`.
- [ ] Fase 2: homogeneizar la API numérica entre `Integer`, `Float` y `Double`.
- [ ] Fase 3: introducir contratos compartidos en `polyloft.common`.

## Notas

Bytes era el punto más débil de common. En `index.pf:520` era básicamente un wrapper indexable sobre array, mientras que crypto trabaja solo con texto en `index.pf:1`. La dirección correcta es una historia binaria más completa: `toString` o `asString`, `asHex`, `asBase64`, `fromHex`, `fromBase64`, `equals`, `hash`, `concat`, `slice` más ergonómico y quizá conversión numérica.

La API numérica está algo asimétrica. `Integer` tiene `max` y `min` en `index.pf:53`, pero `Float` y `Double` no; en cambio `Float` y `Double` sí tienen `sqrt` en `index.pf:124` e `index.pf:207`. Conviene homogeneizar `min`, `max`, `clamp`, `floor`, `ceil`, `round` y un `compareTo` más flexible entre wrappers y primitivos.

Common todavía no tiene contratos compartidos, a diferencia de collections y maps. Eso limita la reutilización genérica. Hay espacio claro para introducir interfaces tipo `Comparable`, `Hashable`, `TextLike`, `ByteSequence` o `Sequence`, y hacer que `String`, `CharArray`, `Bytes` e incluso los wrappers numéricos expongan capacidades comunes.