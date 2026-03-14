# Polyloft BVM vs Python Stress Test Benchmark

## Summary
- **Tests Passed:** 45 / 45
- **Total Python Internal Time (passed):** 33456 ms
- **Total Polyloft Internal Time (passed):** 82181 ms
- **Overall Speedup:** 0.41x

## Detailed Results
| Test Name | Python Time (ms) | Polyloft Time (ms) | Speedup | Passed |
|-----------|------------------|--------------------|---------|--------|
| 01_while_loop_10m              |          1909 |            3566 |   0.54x |   ✅   |
| 02_for_loop_10m                |          2383 |            4477 |   0.53x |   ✅   |
| 03_nested_loops                |           221 |             298 |   0.74x |   ✅   |
| 04_complex_branching           |          2254 |            3566 |   0.63x |   ✅   |
| 05_recursion_fibonacci         |          4884 |           12834 |   0.38x |   ✅   |
| 06_integer_add_sub             |          6636 |           11158 |   0.59x |   ✅   |
| 07_integer_mul_div             |          1410 |            3510 |   0.40x |   ✅   |
| 08_float_arithmetic            |          1629 |            3961 |   0.41x |   ✅   |
| 09_modulo_operations           |          1965 |            2895 |   0.68x |   ✅   |
| 10_collaz_conjecture           |          2643 |            9069 |   0.29x |   ✅   |
| 11_primes_sieve                |           416 |            3124 |   0.13x |   ✅   |
| 12_mandelbrot_set              |           112 |             561 |   0.20x |   ✅   |
| 13_vector_math                 |             6 |              21 |   0.29x |   ✅   |
| 14_polynomial_eval             |          2339 |            8077 |   0.29x |   ✅   |
| 15_factorial_loop              |           160 |             656 |   0.24x |   ✅   |
| 16_array_append                |            16 |              68 |   0.24x |   ✅   |
| 17_array_seq_read              |            17 |              68 |   0.25x |   ✅   |
| 18_array_rand_access           |            65 |             127 |   0.51x |   ✅   |
| 19_array_sorting               |            23 |             123 |   0.19x |   ✅   |
| 20_array_slicing               |             2 |              32 |   0.06x |   ✅   |
| 21_map_insertion               |             6 |              25 |   0.24x |   ✅   |
| 22_map_retrieval               |             9 |              14 |   0.64x |   ✅   |
| 23_map_updates                 |            13 |              23 |   0.57x |   ✅   |
| 24_map_deletion                |             7 |              16 |   0.44x |   ✅   |
| 25_string_concat               |             1 |               1 |   1.00x |   ✅   |
| 26_string_search               |           255 |             658 |   0.39x |   ✅   |
| 27_string_split_join           |            73 |             458 |   0.16x |   ✅   |
| 28_tuple_creation              |           448 |            2424 |   0.18x |   ✅   |
| 29_nested_data_structures      |             0 |               2 |   0.00x |   ✅   |
| 30_simulated_sets              |            26 |              39 |   0.67x |   ✅   |
| 31_object_instantiation        |            75 |             209 |   0.36x |   ✅   |
| 32_method_calls_simple         |           284 |             838 |   0.34x |   ✅   |
| 33_polymorphism                |           300 |             776 |   0.39x |   ✅   |
| 34_property_access             |           365 |             854 |   0.43x |   ✅   |
| 35_static_methods              |           425 |             943 |   0.45x |   ✅   |
| 36_deep_hierarchy              |           104 |             151 |   0.69x |   ✅   |
| 37_many_arguments              |           537 |            2117 |   0.25x |   ✅   |
| 38_object_comparisons          |           886 |            3183 |   0.28x |   ✅   |
| 39_factory_pattern             |           419 |             993 |   0.42x |   ✅   |
| 40_linked_list                 |            11 |              25 |   0.44x |   ✅   |
| 41_string_builder_simulation   |             1 |              10 |   0.10x |   ✅   |
| 42_dfs_graph                   |             0 |               0 |   0.00x |   ✅   |
| 43_bubble_sort_objects         |            12 |              48 |   0.25x |   ✅   |
| 44_n_queens                    |            14 |              26 |   0.54x |   ✅   |
| 45_monte_carlo_pi              |            95 |             157 |   0.61x |   ✅   |
