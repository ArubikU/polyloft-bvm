# Optimización del intérprete polyloft-bvm: de 3× CPython a paridad

Este documento describe, en orden cronológico y con detalle técnico, cada
optimización aplicada al intérprete de bytecode de polyloft-bvm para igualar
o superar a CPython 3 en el conjunto de benchmarks del proyecto. Se incluyen
la motivación, el mecanismo, las mediciones y —cuando aplica— los intentos
fallidos y sus causas, ya que estos últimos documentan los límites reales del
diseño.

**Entorno de medición:** Windows 11, Intel Core i7-12700, Go 1.x (gc),
CPython 3 del sistema. Cada cifra es la mediana de ≥3 ejecuciones de los
programas `testdata/programs/bench_*.pf` frente a sus equivalentes
`bench_*.py`. La metodología de comparación entre versiones usa binarios
compilados por separado (`bvm_old.exe` desde el commit base, `bvm_bench.exe`
desde el árbol de trabajo) ejecutados de forma intercalada.

## Resultado final

| Benchmark | CPython 3 | polyloft-bvm | Ratio | Estado |
|-----------|-----------|--------------|-------|--------|
| fib (recursión, fib(35)) | 838 ms | ~795 ms | **0.95×** | más rápido |
| float (Leibniz π, Newton, Mandelbrot) | 127 ms | ~122 ms | **0.96×** | más rápido |
| string (concatenación/búsqueda) | — | — | **0.76×** | más rápido |
| sort (quicksort de objetos, N=5000) | 4.1 ms | ~5.0 ms | 1.2× | |
| array (criba 500k, sumas de prefijos) | 42 ms | ~49 ms | 1.17× | |
| poly (despacho virtual, 30000 objetos) | 12.4 ms | ~14.8 ms | 1.19× | |

Punto de partida de la campaña descrita aquí (commit `dbd86d8`):
fib ≈ 1950 ms (2.3×), float ≈ 555 ms (4.4×), sort ≈ 18 ms (4.4×),
array ≈ 146 ms (3.4×), poly ≈ 35 ms (2.8×).

## Arquitectura de referencia

polyloft-bvm es una máquina virtual de pila escrita en Go. El compilador
(`internal/compiler`) traduce el AST a bytecode (`internal/bytecode`); la VM
(`internal/vm/vm.go`) lo ejecuta en un único bucle de despacho
(`executeUntilDepth`) con un `switch` sobre el opcode. Los valores son un
struct `value.Value` de ~64 bytes (Kind, Num float64, Int int64, NumberKind,
Bool, Str string, Object any) que viaja por copia. Cada llamada de función
crea un `frame` con slots de locales; los frames se reciclan en un pool.

Las optimizaciones se agrupan en cuatro familias:

1. **Fusión de opcodes** — colapsar secuencias frecuentes en una instrucción.
2. **Eliminación de overhead por instrucción** — pila, lectores, cachés del
   bucle de despacho.
3. **Derrota del límite de inlining de Go en funciones grandes** — la familia
   con mayor impacto individual.
4. **Memoización y caminos rápidos de tipo** — constantes, igualdad,
   constructores.

---

## 1. Fusión de opcodes (peepholes del compilador)

La fuente de coste dominante en un intérprete de bytecode es el *dispatch*:
cada instrucción paga la lectura del opcode, el salto del `switch` y la
vuelta al inicio del bucle. Fusionar k instrucciones en 1 elimina (k−1)
despachos completos y, de paso, el tráfico de pila intermedio.

### 1.1 `OpJumpIfFalsePop` / `OpJumpIfTruePop` (commit `c9c6707`)

**Antes:** toda condición (`if`, `loop`, `&&`, `||`, `switch`, `try/catch`)
compilaba a `JUMP_IF_FALSE off` seguido de `POP` en ambos destinos — el valor
de condición permanecía en la pila y había que retirarlo en las dos ramas.

**Después:** una sola instrucción que hace pop del tope y salta
condicionalmente. El compilador elimina los `POP` de ambas ramas. Se extendió
además el peephole existente `NOT + JUMP_IF_FALSE → JUMP_IF_TRUE` a las
variantes Pop (`NOT + JUMP_IF_FALSE_POP → JUMP_IF_TRUE_POP`), de modo que
`if !x:` no ejecuta nunca el `NOT`.

**Medición:** float −17 %, sort −14 %, poly −11 %.

**Nota metodológica:** una primera medición de este cambio pareció una
regresión del 5-10 % porque se comparó contra un estado intermedio del árbol
en lugar del commit base; re-medir contra binarios limpios mostró la mejora
real. Lección: en campañas largas, todo benchmark debe ancorarse a un binario
de referencia inmutable.

### 1.2 Eliminación de saltos muertos en `if` sin `else`

El generador emitía incondicionalmente `JUMP 0` tras el cuerpo de un `if`
para saltar el `else` aunque no existiera, dejando una instrucción de 3 bytes
que salta a la siguiente. El compilador ahora solo emite `jumpOverElse`
cuando hay rama `else` (aplicado en `compileStmt`, `compileFastIfStmt` y
`compileIfInstanceOf`).

### 1.3 `INC_LOCAL` / `DEC_LOCAL` (commit `d2d87b3`)

`i += 1` compilaba a `ADD_CONST_LOCAL_INT slot, const_idx` (4 bytes, con
búsqueda en el pool de constantes para leer el `1`). Los incrementos ±1 son
el caso abrumadoramente común en bucles, así que se les dio un opcode de
2 bytes que incrementa los campos `Int` y `Num` del local **in situ**, sin
checks de tipo (el compilador solo lo emite cuando la inferencia garantiza
un entero) y sin tocar el pool de constantes.

### 1.4 Saltos fusionados local-vs-local (commit `d2d87b3`)

Patrón objetivo (bucles `loop i <= j:` del quicksort, condición de salida de
la criba):

```
GET_LOCAL a ; GET_LOCAL b ; LESS/GREATER_NUM ; JUMP_IF_{TRUE,FALSE}_POP
```

(4 despachos, 2 pushes, 2 pops, 8 bytes) se fusiona vía peephole en
`emitJump` a:

```
JUMP_IF_LOCAL_{LT,GT}_LOCAL_{TRUE,FALSE} a b offset
```

(1 despacho, 0 tráfico de pila, 5 bytes). La condición invertida del salto
"False" se precomputa en el opcode (p. ej. `LT_FALSE` salta cuando a ≥ b).

**Medición:** array −26 % (la criba), sort −11 %.

**Detalle de implementación:** el peephole inspecciona los últimos bytes de
`chunk.Code` usando `len(code)` y no `lastOpcodeOffset`, porque el peephole
de NOT que corre antes trunca el code sin actualizar ese offset.

### 1.5 Saltos fusionados campo-de-array-vs-local (commit `0b55a42`)

Los bucles internos del quicksort —

```
loop arr[i].key < pivot: i += 1 end
loop arr[j].key > pivot: j -= 1 end
```

— ejecutan por iteración `GET_LOCAL_ARRAY_FIELD + GET_LOCAL + CMP +
JUMP_IF_FALSE_POP` (10 bytes, 4 despachos, 3 pushes). El peephole los fusiona
en `JUMP_IF_ARRAY_FIELD_{GTE,LTE}_LOCAL_TRUE arr idx field cmp offset`
(7 bytes, 1 despacho, 0 pila). El handler valida en runtime el tipo del
array, el tipo del elemento, el rango del índice y el rango del slot de
campo, devolviendo errores limpios (originalmente usaba type-assertions sin
check; ver §6).

**Medición:** sort −21 %.

### 1.6 `OP_SET_ARRAY_LOCALS` (commit `f7910ab`)

Generalización de un opcode previo (`SET_LOCAL_ARRAY_BOOL`, restringido a
literales booleanos): `local_arr[local_idx] = <expr>` compilaba a
`GET_LOCAL arr ; GET_LOCAL idx ; <expr> ; SET_INDEX_ARRAY` (5 operaciones de
pila). El nuevo opcode de 3 bytes lee array e índice directamente de los
slots de locales y solo el valor viaja por la pila. A diferencia del opcode
restringido que le sirvió de precedente, incluye fallback por `hasCells`
para locales capturadas por closures.

Acelera todo almacenamiento indexado en bucles: llenado de arrays, swaps del
quicksort, sumas de prefijos, construcción de arrays de objetos.

**Medición:** test de llenado de array 10.3 → 7.1 ms (y véase §3.5: este
opcode convirtió una regresión del +10 % en una mejora neta del −21 %).

### 1.7 `OP_ARRAY_PUSH` (commit `e91d914`)

Introducido para las comprensiones de arrays (`[expr for x in iter]`): hace
pop del valor y lo anexa al array que queda en el tope de la pila. El array
resultado permanece en la pila durante todo el bucle de la comprensión, de
modo que la comprensión completa es una expresión balanceada que reutiliza
la maquinaria `ITER_INIT`/`ITER_NEXT` existente sin variables temporales.

---

## 2. Overhead por instrucción: pila, lectores, cachés

### 2.1 Pila con puntero explícito `sp` (commit `232628f`)

**Antes:** la pila de valores era un slice de Go manipulado con `append` /
re-slicing: cada push actualizaba el header del slice (puntero, len, cap) y
pagaba el check de crecimiento de `append`; cada pop reconstruía el header.

**Después:** array preasignado (4096 slots) + entero `vm.sp`:

```go
func (vm *VM) push(v value.Value) { vm.stack[vm.sp] = v; vm.sp++ }
func (vm *VM) pop() value.Value   { vm.sp--; return vm.stack[vm.sp] }
```

Todos los usos de `len(vm.stack)` pasaron a `vm.sp` y los truncamientos
(`vm.stack = vm.stack[:n]`) a asignaciones (`vm.sp = n`).

**Medición:** fib −24 %, float −55 %, sort −43 %, array −31 %, poly −26 % —
la mayor mejora de una sola transformación en toda la campaña, porque
push/pop se ejecutan una o más veces por opcode.

**Seguridad de desbordamiento (commit `ef79541`):** la versión inicial no
tenía check de límites (recursión profunda → panic por índice). Tres intentos
de arreglo, medidos:

1. *Check en cada push* (`if vm.sp >= len(vm.stack)`): −9 % global. El check
   lee el header del slice en cada push.
2. *Check contra un campo cacheado* (`vm.stackCap`): sin mejora apreciable;
   la rama extra en el camino más caliente sigue costando.
3. **Solución adoptada — headroom por frame:** el uso de pila de un frame
   está acotado por su bytecode (profundidad de anidamiento de expresiones,
   ≤255 argumentos pendientes). `ensureStackHeadroom()` en `acquireFrame`
   garantiza 1024 slots libres al crear cada frame, creciendo la pila
   geométricamente si hace falta. `push` vuelve a ser de 2 instrucciones.
   El coste se paga una vez por *llamada* (miles) en lugar de una vez por
   *push* (millones). Test de regresión: recursión de 20 000 niveles.

### 2.2 Aritmética in situ sobre la pila (commit `232628f`)

`OpAddNum`, `OpSubNum`, `OpMulNum`, `OpDivNum`, `OpLessNum`, `OpGreaterNum`
pasaron del patrón pop-pop-compute-push a modificar `vm.stack[sp-2]` por
campos y decrementar `sp`:

```go
case bytecode.OpAddNum:
    s1, s2 := vm.sp-1, vm.sp-2
    if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
        if ambos int { vm.stack[s2].Int += vm.stack[s1].Int; vm.stack[s2].Num = float64(...) }
        else        { vm.stack[s2].Num += vm.stack[s1].Num; ... NumberKind = Float }
        vm.sp--
        continue
    }
    // camino genérico (strings, instancias boxeadas) → binaryNumberOp
```

Con `value.Value` de 64 bytes, cada pop+push evitado son dos copias de
struct completas; el compilador de Go emite loads/stores de campo dirigidos
en lugar de `memmove`s. Las comparaciones escriben `Kind = Bool` y `Bool`
sobre el slot del operando izquierdo.

### 2.3 Caching del frame y del code slice en el bucle de despacho

**Frame (sesión previa a `dbd86d8` y extendido después):** `frame` se carga
una vez antes del bucle y se refresca solo donde puede cambiar: tras cada
opcode de llamada exitoso, tras `OpReturn` que no sale, y tras cada
`handleRaised` que devuelve `handled=true`. Esto elimina
`vm.frames[len(vm.frames)-1]` + comparación de profundidad por instrucción.

**Code slice (commit `97ed07f`):** `code := frame.fn.Chunk.Code` se cachea
junto al frame y se refresca en los mismos puntos. El fetch del opcode y
las lecturas de operandos dejan de perseguir tres punteros
(frame→fn→Chunk→Code) por byte.

**Invariante de mantenimiento:** todo punto que reasigna `frame` debe
reasignar `code` en la línea siguiente. Una auditoría automatizada (awk
sobre el rango de la función) verifica que ninguno de los ~27 puntos de
refresco omita la segunda asignación.

### 2.4 Limpieza única de locales por llamada (commit `97ed07f`)

Los frames reciclados se limpiaban dos veces: `releaseFrame` hacía
`clear(locals)` al devolver el frame al pool y `acquireFrame` volvía a
limpiar al reutilizarlo. `memclr` era ~10 % del perfil de fib.

Se estableció el invariante: *el backing array de locales de un frame en el
pool está completamente a cero hasta su capacidad*. Inducción: los arrays
nuevos nacen a cero (`make`); `releaseFrame` re-cero el prefijo usado antes
de encolar; `acquireFrame` re-slicea exponiendo solo slots a cero, sin
limpiar. La limpieza queda en exactamente un punto (release), que además
cumple la función de soltar referencias para el GC.

### 2.5 `readUint16` directo

El lector de operandos de 16 bits hacía dos llamadas a `readByte` (cada una
con su bounds-check y su `ip++`). Se reescribió como una lectura directa de
dos bytes con un único avance de `ip`. (La historia completa de por qué esto
no bastó está en §3.2.)

---

## 3. El límite de inlining de Go en funciones grandes

Esta familia produjo la mayor ganancia puntual de la campaña (−41 % fib,
−34 % float en un solo commit) y explica varios fallos previos.

### 3.1 El problema

El compilador gc de Go tiene dos presupuestos de inlining:

- Presupuesto normal: un callee con coste ≤ 80 nodos IR es inlineable.
- **Límite de función grande:** si el *caller* supera ~5000 nodos IR
  (`inlineBigFunctionNodes`), solo se inlinean en él callees con coste
  ≤ 20 (`inlineBigFunctionMaxCost`).

`executeUntilDepth` —el switch de despacho con ~150 opcodes— supera con
holgura ese umbral. Consecuencia silenciosa: `go build -gcflags='-m'`
reporta "can inline" para `readUint16` (coste 32), `localGet` (71) y
`localSet` (60), pero **ninguna de sus llamadas dentro del bucle de despacho
se inlinea**. Cada una compila como llamada real, lo que además fuerza el
*spill* a memoria de los registros calientes (`frame`, `sp`, `code`)
alrededor de cada llamada, en cada instrucción.

El diagnóstico requirió `-gcflags='-m -m'` y grep por "inlining call to" en
las líneas concretas del bucle: la ausencia de mensaje en un call-site cuyo
callee "can inline" es la firma del límite de función grande.

### 3.2 Lectores por debajo del umbral (commit `97ed07f`)

Ninguna variante de `readUint16` como método (que necesita la cadena
`frame.fn.Chunk.Code`) baja de coste 20: la cadena de selectores ya consume
el presupuesto. La solución fue cambiar la forma de los lectores:

```go
// coste 14 — inlinea incluso en la función grande
func readB(code []byte, f *frame) byte { b := code[f.ip]; f.ip++; return b }

// coste 14 — sin estado: el caller avanza ip
func readU16At(code []byte, i int) uint16 {
    return uint16(code[i])<<8 | uint16(code[i+1])
}
```

`readU16At` es deliberadamente *stateless* porque la variante que también
actualizaba `f.ip` costaba 26 (> 20). Los ~33 call-sites del bucle se
transformaron mecánicamente (`x := readU16At(code, frame.ip); frame.ip += 2`),
con cuidado en los casos donde el `ip` ya avanzado participa en el cálculo
(p. ej. `catchIP = frame.ip + offset` en `PushHandler`, y el `frame.ip -=
offset` de `OpLoop`).

### 3.3 Expansión manual de `localGet`/`localSet` (commit `97ed07f`)

`localGet` cuesta 71 porque contiene una llamada al camino lento
(`localGetSlow`, no inlineable), y una llamada a función no inlineable
cuesta 57 nodos por sí sola — es imposible bajarlo de 20. La solución fue
expandir a mano el camino rápido en los opcodes más calientes:

```go
case bytecode.OpGetLocal:
    slot := readB(code, frame)
    if !frame.hasCells {
        vm.push(frame.locals[slot])
    } else {
        vm.push(vm.localGetSlow(frame, slot))
    }
```

Aplicado a `OpGetLocal`, `OpSetLocal`, `OpAddConstLocalInt`,
`OpJumpIfLocalLessEqualIntConstFalse` (la condición base de fib),
`OpCallSelfLocalSubInt` (la llamada recursiva de fib) y posteriormente
`OpIterNext` (commit `c06333e`).

**Medición conjunta de §3.2+§3.3+§2.3+§2.4:** fib 1478 → 781 ms (−41 %),
float 242 → 161 ms (−34 %), sort −22 %, array −22 %, poly −15 %. La magnitud
excede la suma de los tiempos planos de las funciones eliminadas porque el
beneficio principal es indirecto: sin llamadas en el camino por instrucción,
el asignador de registros mantiene el estado caliente del bucle en
registros de forma estable.

### 3.4 La tensión DRY ↔ despacho, medida (commits `ef79541` y `c06333e`)

Una revisión de código SOLID/DRY introdujo helpers compartidos para los
handlers duplicados. Dos de ellos resultaron regresiones medibles y fueron
revertidos a duplicación deliberada con comentario *keep-in-sync*:

- `jumpIfLocalCmp(frame, func(a,b float64) bool)` — unificaba los 4 saltos
  local-vs-local. Coste > 20 → llamada real **más** llamada indirecta del
  closure comparador por cada salto fusionado: 9.4 % del perfil de
  bench_array. Re-expandido.
- `evalCondition(v)` — unificaba la truthiness de los 4 saltos
  condicionales: +8-13 % en fib/float. Re-expandido; posteriormente se les
  añadió a los cuatro un fast path `Kind == Bool` inline (dos lecturas de
  campo) que evita la llamada a `booleanOperand` en el caso dominante.

En cambio, los helpers compartidos que **no** están en el camino por
instrucción se conservaron como única fuente de verdad:
`readArrayFieldCmpArgs` (decodificación validada de los saltos
campo-de-array, §1.5), `applyToLocalSlow` (camino con closures de los
opcodes `*_TO_LOCAL`), `adjustIntLocal` (INC/DEC con células),
`resolvedConsts` (§4.1) y `valuesEqual` como fallback genérico (§4.3).

**Regla operativa adoptada:** un helper llamado desde el bucle de despacho
debe demostrar coste ≤ 20 con `-gcflags='-m -m'`; si no puede, el camino
rápido se duplica en los `case` con comentario de sincronización, y solo el
camino lento se factoriza.

### 3.5 Caso de estudio: la regresión de `array_append`

Cachear `code` (§2.3) produjo una regresión aislada del +10 % en el microtest
de llenado de arrays (10.3 vs 9.0 ms del binario base) mientras los otros
44 programas mejoraban 2-3×. Causa plausible: el local extra `code` aumenta
la presión de registros en `SET_INDEX_ARRAY`, un opcode con muchos valores
vivos (3 pops + validaciones). En lugar de revertir, se atacó el patrón a
nivel de bytecode con `OP_SET_ARRAY_LOCALS` (§1.6), que elimina los valores
vivos en lugar de pelear por registros: 10.3 → 7.1 ms, neto −21 % respecto
al binario base.

---

## 4. Memoización y caminos rápidos de tipo

### 4.1 Memoización del pool de constantes (commit `dfff5e8`)

`OpConstant` convertía la entrada `[]any` del chunk a `value.Value` mediante
el type-switch de `constantToValue` **en cada ejecución** — 4.3 % del perfil
de float, que carga constantes (`2.0`, `1.0`, `4`) dentro de bucles de un
millón de iteraciones.

Restricción de diseño: `value` importa `bytecode`, así que el slice
convertido no puede vivir en `bytecode.Chunk` (ciclo de imports). Solución:

- `VM.constCache map[*bytecode.Chunk][]value.Value` — memoización por chunk.
  Es seguro compartir el slice entre frames porque las constantes son
  inmutables tras la compilación y la conversión es determinista; es seguro
  por VM porque el caché no se comparte entre VMs.
- `frame.consts []value.Value` — obtenido perezosamente en el primer acceso
  a constantes del frame (`if frame.consts == nil { frame.consts =
  vm.resolvedConsts(...) }`). La pereza importa: los frames calientes de fib
  usan opcodes especializados que no tocan `OpConstant`, y solo pagan un
  reset a `nil` en `acquireFrame` en vez de un lookup de map por llamada.

Comparten el caché `OpConstant`, `OpCallConst` y `OpCallConstLocalSubInt`.

**Medición:** float 162 → 121 ms (−25 %; cruza por debajo de CPython),
poly −9 %, fib +1 % (coste del reset, aceptado).

### 4.2 `FastConstructor` para subclases (commit `dfdea41`)

El compilador detecta constructores triviales (cuerpo compuesto únicamente
de `this.campo = parámetro`) y los compila a un plan (`FastConstructorPlan`)
que la VM ejecuta sin crear frame: copia los argumentos de la pila a los
slots de campos directamente.

La detección rechazaba cualquier clase con superclase. En bench_poly las
tres figuras extienden `Shape`, así que **ninguna** calificaba y cada `new`
pagaba frame completo (acquire, despacho del cuerpo, return). La restricción
se relajó a: *rechazar solo si algún ancestro declara constructor*.
Justificación de seguridad, verificada empíricamente:

- El escaneo de sentencias ya garantiza que el cuerpo no contiene
  `super(...)`, por lo que no se omite ninguna llamada explícita.
- `NewInstance` aplica los defaults de campos de toda la cadena, igual que
  el camino lento (verificado con un caso de default heredado).
- El camino lento **tampoco** invoca constructores ancestrales
  implícitamente (verificado: ambos binarios dejan `n=nil` cuando el
  ancestro tiene constructor y la subclase no lo llama), de modo que el
  guard es estrictamente más conservador que el comportamiento existente.
- Las superclases deben declararse antes que las subclases (el compilador
  falla con "unknown superclass" si no), así que el `Constructor` del
  ancestro es definitivo en el momento de la detección.

**Medición:** microbench de construcción de subclases 69 → 23 ms (3×);
bench_poly −5 % (su bucle de construcción está dominado por otros opcodes).

### 4.3 Fast path numérico en `OpEqual` (commit `59d0f24`)

`valuesEqual` es genérico (sondea operandos textuales, numéricos y booleanos
en orden, con unwrapping de instancias boxeadas) y es llamada real desde el
bucle. El caso número-vs-número —guardas de bucle como `r == 0`— se maneja
ahora inline con comparación exacta de `Int` cuando ambos lados son enteros,
y de `Num` en caso contrario; `valuesEqual` queda como única fuente de
verdad para los casos complejos. 5.6 % del perfil de poly.

### 4.4 Experimento negativo: almacenamiento inline de campos

`value.Instance` tiene `Inline [1]Value` que respalda `Fields` para clases
de un campo, evitando la segunda allocación. Se midió crecerlo:

- `[4]Value`: bench_poly **+13 % (peor)**. Cada instancia crece 192 bytes;
  el coste extra de escaneo del GC y la pérdida de localidad superan la
  allocación ahorrada en clases de 2-3 campos.
- `[2]Value`: neutro.

Se mantuvo `[1]Value` con la medición documentada en el comentario del campo
y `NewInstance` simplificado a una rama dirigida por bounds, de modo que
retunear el tamaño sea editar una constante.

---

## 5. Cambios de lenguaje con efecto en benchmarks

### Comprensiones de arrays (commit `e91d914`)

`[expr for var in iterable]` no existía en el parser (tres programas de test
fallaban con error de parseo desde su creación). Se implementó de extremo a
extremo: parser (tras el primer elemento de un literal de array, `for`
conmuta a modo comprensión), nodo AST `ArrayComprehensionExpr`, chequeo en
sema (el iterable debe soportar for-in; la variable se liga al tipo de
elemento en un scope propio; el resultado es `array<T>`), y compilación que
mantiene el array resultado en la pila durante el bucle anexando con
`OP_ARRAY_PUSH` (§1.7). No requirió cambios en la maquinaria de iteración.

---

## 6. Correcciones de robustez surgidas de la campaña

Optimizar agresivamente sin red de seguridad produce bugs; estos se
detectaron por revisión sistemática y se corrigieron sin coste de
rendimiento medible (commit `ef79541`):

1. **Desbordamiento de pila** — resuelto con headroom por frame (§2.1).
2. **Compound assignment sobre locales capturadas** — `OpAddToLocal`/
   `SubToLocal`/`MulToLocal` escribían `frame.locals` directamente ignorando
   `hasCells`; un closure que leyera la local capturada veía valores
   obsoletos (programa de regresión: devolvía 8 en lugar de 20 por mezcla de
   escrituras a célula y a slot según qué opcode eligiera el compilador para
   cada asignación). El camino rápido sigue inline; el caso `hasCells` se
   factorizó a `applyToLocalSlow`.
3. **Type-assertions sin check** en los saltos campo-de-array — panics ante
   bytecode/datos inesperados; reemplazadas por validación con errores de
   runtime (`readArrayFieldCmpArgs`).

Cada corrección quedó cubierta por un test e2e
(`TestDeepRecursionGrowsValueStack`, `TestClosureSeesCompoundAssignOnCapturedLocal`,
`TestArrayComprehension`).

---

## 7. Metodología

- **Perfilado:** `profile_bench_test.go` define `BenchmarkFib/Float/Poly/
  Array/Sort` que compilan y ejecutan los programas `.pf` reales bajo
  `go test -bench X -cpuprofile`, analizados con `go tool pprof -top`. Todas
  las optimizaciones de §3 y §4 partieron de una entrada concreta del perfil,
  no de intuición.
- **Verificación de inlining:** `go build -gcflags='-m -m'` y grep por
  "inlining call to" en las líneas del bucle; "can inline" del callee **no**
  es evidencia suficiente (§3.1).
- **Correctitud:** además de la suite e2e de Go, cada cambio se validó
  ejecutando los 45 programas de `tests/` con el binario nuevo y el binario
  base y comparando la salida funcional byte a byte (excluyendo la línea de
  tiempo). Tres incidentes se detectaron así y no por la suite.
- **Anclaje de mediciones:** binario de referencia compilado del commit base
  y conservado durante toda la campaña; medianas de ≥3 ejecuciones
  intercaladas; los benchmarks con allocación grande (array) presentan
  varianza por GC de ±10 % que se reporta como rango.

## 8. Trabajo futuro

- **Reducir `value.Value` (64 bytes):** la palanca global restante. Cada
  push/pop/local copia el struct entero; un *NaN-boxing* o la eliminación
  del par redundante `Num`/`Int` (mantenido por exactitud de int64 fuera del
  rango de float64) aceleraría todo un 10-30 % estimado, a cambio de un
  refactor semántico profundo.
- **Criba (bench_array):** el gap restante (39 vs 30 ms) está en un bucle
  interno ya reducido a 3 opcodes/iteración más presión de GC del array de
  500k; las fusiones adicionales serían específicas del benchmark.
- **poly:** el gap se reparte entre la maquinaria de llamadas a métodos
  (~16 %) y GC de 30 000 instancias (~15 %); requeriría inline caching de
  métodos o un allocador de instancias por arena.


---

## 9. Reduccion de `value.Value` y arrays primitivos densos

Esta seccion cierra dos items de Trabajo futuro (S8): reducir el struct
`value.Value` y la criba de `bench_array`. El resultado matiza la prediccion
original de que reducir `Value` aceleraria todo un 10-30 %: la palanca real
no fue el tamano del struct sino la representacion de los arrays.

### 9.1 Reordenar `value.Value`: 64 -> 56 bytes

`Value` mantenia tres campos de un byte (`Kind`, `NumberKind`, `Bool`)
intercalados entre los dos campos numericos de 8 bytes (`Num`, `Int`),
desperdiciando 13 bytes de relleno por alineacion. Agrupar los tres tags en
una sola palabra de 8 bytes baja el struct a 56 bytes (-12.5 %) sin cambiar
ningun acceso a campo ni la serializacion gob. Reduce la copia por slot en
cada push/pop/move de local. Medido neutro-a-positivo (2-8 %) en los
benchmarks escalares.

### 9.2 Resultado negativo: fusionar `Num`+`Int` en una palabra (48 bytes)

Reinterpretar el payload numerico bit a bit en un unico `uint64` (con
`math.Float64bits`/`frombits`) lleva el struct a 48 bytes. Medido **neto
negativo**: los kernels numericos regresan 9-11 % (fib, float) porque cada
lectura flotante paga un `Float64frombits` y, sobre todo, porque los
accessors `Num()`/`Int()` no se inlinean dentro del switch gigante de
despacho (el mismo limite de presupuesto de inline de S3.1), convirtiendo
cada lectura en una llamada real. Se descarto.

### 9.3 El muro de la linea de cache

`Value` de 64 bytes equivale exactamente a una linea de cache. Un array
contiguo `[]Value` queda con cada elemento alineado a linea. Cualquier tamano
intermedio (33-63 B) es buen tradeoff para los escalares en la pila (acceso
LIFO, caliente en L1) pero **rompe la alineacion de los arrays**: con stride
de 56 B el elemento N empieza en 56*N y cruza fronteras de 64 B, anadiendo
cache-line splits en el recorrido. Medido: `bench_array` (criba de 500k +
prefix sums sobre 100k) regresa ~13 % solo por el cambio 64 -> 56. El
siguiente divisor limpio es 32 B, inalcanzable sin pagar las dos tasas
(fusion numerica + boxing de strings). 64 B es por tanto un optimo local de
alineacion para el almacenamiento `[]Value`.

### 9.4 La palanca real: almacenamiento nativo denso para arrays primitivos

La conclusion es que el problema no era `Value` sino representar arrays de
primitivos como `[]Value`. Los arrays homogeneos de `int`/`float`/`bool`
ahora respaldan sus datos con `[]int64` / `[]float64` / `[]bool` en lugar de
un `Value` de 56 B por elemento. Esto elimina el straddle (stride de 8 o 1
byte, alineado) y la sobrecarga del struct por elemento.

- `Array` lleva un backing tipado seleccionado por `AKind`; objetos, tipos
  custom y arrays heterogeneos/mixtos conservan el almacenamiento `[]Value`
  sin cambios.
- Todo acceso se canaliza por los accessors `Len`/`At`/`SetAt`/`Append`/
  `Values`. `SetAt`/`Append` promueven (materializan a `[]Value`) ante un
  tipo discrepante, preservando la semantica exacta. Los arrays de objetos
  indexan por un camino rapido `Raw()` para no pagar el coste del accessor.
- `new T[N]` con T primitivo emite un fill tipado de ceros (compilador);
  `OpArrayFill` elige el almacenamiento denso a partir del Kind del fill.
  `for-in` itera arrays densos via `Iterator.Arr` sin materializar un
  snapshot `[]Value`. `GobEncode`/`Decode` serializan por la vista `[]Value`,
  asi que el formato `.pfbc` no cambia.

Medido (intercalado, n=15, mismo binario de referencia): `bench_array`
**-25 % a -30 %** respecto a la base de 64 B (de regresion +13 % a mejora
neta); el resto de la suite (macro, sort, string, hash, float, poly, fib) en
paridad dentro del ruido de la maquina. Salida byte-identica en los ocho
benchmarks. Nota metodologica: en este equipo los benchmarks pequenos
(sort ~5 ms, string ~6 ms, poly ~17 ms) oscilan en ambas direcciones entre
corridas (ruido >5 %); solo `array` da senal estable, consistente con la
varianza por GC ya reportada en S7.
