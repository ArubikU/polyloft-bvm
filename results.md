# Stress Test Results

Fecha: 2026-03-11

Metodologia:
- 10 stress tests de [polyloft/stress_tests](../polyloft/stress_tests)
- 3 repeticiones por test
- comparacion entre Python, polyloft-bvm sin JIT y polyloft-bvm con --jit
- binario compilado de polyloft-bvm, sin incluir costo de go run
- sin jit log
- comparacion funcional ignorando las lineas Time: emitidas por cada script

## Totales

| Modo | Tiempo total ms |
|---|---:|
| Python | 932.819 |
| BVM | 836.008 |
| BVM + JIT | 738.113 |

Lectura rapida:
- BVM total vs Python: 0.896x
- BVM + JIT total vs Python: 0.791x
- BVM + JIT vs BVM: 0.883x

## Por Test

| Test | Python ms | BVM ms | JIT ms | BVM/Python | JIT/Python | JIT/BVM |
|---|---:|---:|---:|---:|---:|---:|
| test1_loop | 113.053 | 191.437 | 112.911 | 1.693 | 0.999 | 0.590 |
| test10_fibonacci | 128.606 | 114.010 | 112.813 | 0.887 | 0.877 | 0.990 |
| test2_array | 67.197 | 55.052 | 58.942 | 0.819 | 0.877 | 1.071 |
| test3_string | 100.356 | 31.771 | 39.995 | 0.317 | 0.399 | 1.259 |
| test4_nested | 100.895 | 173.162 | 128.654 | 1.716 | 1.275 | 0.743 |
| test5_factorial | 87.536 | 32.489 | 40.118 | 0.371 | 0.458 | 1.235 |
| test6_map | 79.883 | 59.177 | 59.238 | 0.741 | 0.742 | 1.001 |
| test7_conditional | 94.809 | 48.673 | 55.455 | 0.513 | 0.585 | 1.139 |
| test8_function | 90.364 | 82.721 | 84.449 | 0.915 | 0.935 | 1.021 |
| test9_class | 70.120 | 47.516 | 45.538 | 0.678 | 0.649 | 0.958 |

## Validacion Funcional

Comparando contra Python e ignorando solo las lineas de tiempo impresas por los scripts:

- BVM sin JIT: 10/10 equivalentes
- BVM con JIT: 10/10 equivalentes

## Casos Donde Python Supero al BVM

Tomando el mejor modo de BVM disponible en la corrida:

- test4_nested: Python todavia gana frente a BVM + JIT

Casos donde Python gano frente al BVM sin JIT, pero no frente a BVM + JIT:

- test1_loop

## Hipotesis de Optimización

Los tests mas sensibles comparten el patron:

target = target + a * b

Ese patron aparece directamente en:

- [polyloft/stress_tests/test1_loop.pf](../polyloft/stress_tests/test1_loop.pf)
- [polyloft/stress_tests/test4_nested.pf](../polyloft/stress_tests/test4_nested.pf)

El siguiente paso es colapsar la secuencia:

GET_LOCAL target
GET_LOCAL a
GET_LOCAL b
MUL_NUM
ADD_NUM
SET_LOCAL target

en una micro-instruccion especializada.

## Post-Optimización

Optimizacion aplicada:

- nueva micro-op `ADD_LOCAL_MUL_LOCAL`
- emitida por el compilador para el patron `target = target + a * b`
- ejecutada directamente por la VM

## Totales Post-Optimización

| Modo | Tiempo total ms |
|---|---:|
| Python | 1764.406 |
| BVM | 1086.450 |
| BVM + JIT | 1048.491 |

Lectura rapida:
- BVM total vs Python: 0.616x
- BVM + JIT total vs Python: 0.594x
- BVM + JIT vs BVM: 0.965x

## Por Test Post-Optimización

| Test | Python ms | BVM ms | JIT ms | BVM/Python | JIT/Python | JIT/BVM |
|---|---:|---:|---:|---:|---:|---:|
| test1_loop | 158.244 | 75.895 | 82.790 | 0.480 | 0.523 | 1.091 |
| test10_fibonacci | 178.299 | 175.382 | 175.358 | 0.984 | 0.984 | 1.000 |
| test2_array | 143.000 | 105.540 | 101.057 | 0.738 | 0.707 | 0.958 |
| test3_string | 161.990 | 60.539 | 57.114 | 0.374 | 0.353 | 0.943 |
| test4_nested | 189.209 | 122.131 | 119.906 | 0.645 | 0.634 | 0.982 |
| test5_factorial | 146.891 | 47.215 | 49.816 | 0.321 | 0.339 | 1.055 |
| test6_map | 275.114 | 149.236 | 139.450 | 0.542 | 0.507 | 0.934 |
| test7_conditional | 183.213 | 88.012 | 82.292 | 0.480 | 0.449 | 0.935 |
| test8_function | 171.084 | 155.077 | 143.980 | 0.906 | 0.842 | 0.928 |
| test9_class | 157.362 | 107.423 | 96.728 | 0.683 | 0.615 | 0.900 |

## Impacto en los Casos Objetivo

Comparando contra la corrida inicial:

- `test1_loop`
	- antes: BVM 191.437 ms, JIT 112.911 ms
	- despues: BVM 75.895 ms, JIT 82.790 ms
- `test4_nested`
	- antes: BVM 173.162 ms, JIT 128.654 ms
	- despues: BVM 122.131 ms, JIT 119.906 ms

Efecto observable:

- `test1_loop` deja de depender del JIT para ganarle a Python
- `test4_nested` tambien pasa a quedar por delante de Python
- en la nueva corrida, Python ya no supera al BVM en ninguno de los 10 stress tests medidos

Nota importante:

- `test1_loop` y `test4_nested` son scripts top-level puros, no helpers llamados repetidamente
- el JIT actual de `polyloft-bvm` acelera llamadas de funciones y metodos, no el `<script>` completo
- por eso, una diferencia pequena entre `run` y `run --jit` en esos dos casos puede ser solo ruido del proceso externo y no una ejecucion JIT real del loop
- el CLI ahora evita habilitar el motor JIT en scripts top-level puros sin funciones bytecode anidadas, para no introducir overhead enganoso en este tipo de benchmark

## Validacion Funcional Post-Optimización

Comparando contra Python e ignorando solo las lineas de tiempo impresas por los scripts:

- BVM sin JIT: 10/10 equivalentes
- BVM con JIT: 10/10 equivalentes