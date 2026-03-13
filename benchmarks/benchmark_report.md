# Polyloft BVM vs Python Stress Test Benchmark

## Summary
- **Tests Passed:** 37 / 45
- **Total Python Internal Time (passed):** 7825 ms
- **Total Polyloft Internal Time (passed):** 44838 ms
- **Overall Speedup:** 0.17x

## Detailed Results
| Test Name | Python Time (ms) | Polyloft Time (ms) | Speedup | Passed |
|-----------|------------------|--------------------|---------|--------|
| 01_while_loop_10m              |           507 |             997 |   0.51x |   ✅   |
| 02_for_loop_10m                |           649 |            2188 |   0.30x |   ✅   |
| 03_nested_loops                |            63 |              92 |   0.68x |   ✅   |
| 04_complex_branching           |           488 |             850 |   0.57x |   ✅   |
| 05_recursion_fibonacci         |          1200 |            3647 |   0.33x |   ✅   |
| 06_integer_add_sub             |          1778 |            5027 |   0.35x |   ✅   |
| 07_integer_mul_div             |           466 |            1780 |   0.26x |   ❌   |
| 08_float_arithmetic            |           464 |            1776 |   0.26x |   ✅   |
| 09_modulo_operations           |           598 |             940 |   0.64x |   ✅   |
| 10_collaz_conjecture           |           777 |            2592 |   0.30x |   ✅   |
| 11_primes_sieve                |            88 |            1035 |   0.09x |   ✅   |
| 12_mandelbrot_set              |            30 |             249 |   0.12x |   ✅   |
| 13_vector_math                 |             1 |               8 |   0.12x |   ❌   |
| 14_polynomial_eval             |           841 |           38007 |   0.02x |   ❌   |
| 15_factorial_loop              |            41 |             838 |   0.05x |   ❌   |
| 16_array_append                |             5 |              20 |   0.25x |   ✅   |
| 17_array_seq_read              |             7 |              29 |   0.24x |   ✅   |
| 18_array_rand_access           |            17 |              54 |   0.31x |   ✅   |
| 19_array_sorting               |             5 |              38 |   0.13x |   ✅   |
| 20_array_slicing               |             0 |              11 |   0.00x |   ✅   |
| 21_map_insertion               |             2 |               6 |   0.33x |   ✅   |
| 22_map_retrieval               |             2 |               5 |   0.40x |   ✅   |
| 23_map_updates                 |             2 |               6 |   0.33x |   ✅   |
| 24_map_deletion                |             2 |               6 |   0.33x |   ✅   |
| 25_string_concat               |             0 |               0 |   0.00x |   ✅   |
| 26_string_search               |            73 |             194 |   0.38x |   ✅   |
| 27_string_split_join           |            21 |             145 |   0.14x |   ✅   |
| 28_tuple_creation              |           127 |             777 |   0.16x |   ✅   |
| 29_nested_data_structures      |             0 |               0 |   0.00x |   ✅   |
| 30_simulated_sets              |             6 |              11 |   0.55x |   ✅   |
| 31_object_instantiation        |            19 |              69 |   0.28x |   ✅   |
| 32_method_calls_simple         |            79 |             295 |   0.27x |   ✅   |
| 33_polymorphism                |            96 |            6988 |   0.01x |   ✅   |
| 34_property_access             |            93 |             308 |   0.30x |   ✅   |
| 35_static_methods              |           111 |            7282 |   0.02x |   ✅   |
| 36_deep_hierarchy              |            21 |              59 |   0.36x |   ✅   |
| 37_many_arguments              |           145 |            7734 |   0.02x |   ✅   |
| 38_object_comparisons          |           237 |            1022 |   0.23x |   ✅   |
| 39_factory_pattern             |           106 |             354 |   0.30x |   ✅   |
| 40_linked_list                 |             3 |              10 |   0.30x |   ✅   |
| 41_string_builder_simulation   |             1 |               0 |   0.00x |   ❌   |
| 42_dfs_graph                   |             0 |               0 |   0.00x |   ❌   |
| 43_bubble_sort_objects         |             3 |               0 |   0.00x |   ❌   |
| 44_n_queens                    |             4 |              22 |   0.18x |   ✅   |
| 45_monte_carlo_pi              |            31 |              82 |   0.38x |   ❌   |
