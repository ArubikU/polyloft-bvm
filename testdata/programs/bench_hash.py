import time

started = time.perf_counter()

# Part 1: insert 50k string keys
t1 = time.perf_counter()
m = {}
for i in range(50000):
    key = "key" + str(i)
    m[key] = i * 2
insert_ms = (time.perf_counter() - t1) * 1000

# Part 2: lookup all keys
t2 = time.perf_counter()
total = 0
for i in range(50000):
    key = "key" + str(i)
    total += m[key]
lookup_ms = (time.perf_counter() - t2) * 1000

# Part 3: update (read-modify-write) 20k entries
t3 = time.perf_counter()
for i in range(20000):
    key = "key" + str(i * 2)
    m[key] = m[key] + 1
update_ms = (time.perf_counter() - t3) * 1000

# Part 4: frequency count with string keys
t4 = time.perf_counter()
freq = {}
for i in range(100000):
    k = "w" + str(i % 1000)
    if k not in freq:
        freq[k] = 0
    freq[k] += 1
freq_ms = (time.perf_counter() - t4) * 1000

ended = time.perf_counter()

print("python hash benchmark")
print(f"insert_total_check={total}")
print(f"insert_ms={insert_ms:.3f}")
print(f"lookup_ms={lookup_ms:.3f}")
print(f"update_ms={update_ms:.3f}")
print(f"freq_ms={freq_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
