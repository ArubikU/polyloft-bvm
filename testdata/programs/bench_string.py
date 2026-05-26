import time

started = time.perf_counter()

# Part 1: repeated string concatenation
s = ""
for i in range(3000):
    s = s + "x"
build_ms = (time.perf_counter() - started) * 1000

# Part 2: numeric-to-string conversion
t1 = time.perf_counter()
nums = ""
for i in range(5000):
    nums = nums + str(i) + ","
conv_ms = (time.perf_counter() - t1) * 1000

# Part 3: string contains check
t2 = time.perf_counter()
haystack = "the quick brown fox jumps over the lazy dog"
hits = 0
for i in range(50000):
    if "fox" in haystack:
        hits += 1
search_ms = (time.perf_counter() - t2) * 1000

ended = time.perf_counter()

print("python string benchmark")
print(f"build_len={len(s)}")
print(f"conv_len={len(nums)}")
print(f"search_hits={hits}")
print(f"build_ms={build_ms:.3f}")
print(f"conv_ms={conv_ms:.3f}")
print(f"search_ms={search_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
