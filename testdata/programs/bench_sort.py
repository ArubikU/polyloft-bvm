import time

class Item:
    def __init__(self, k: int, v: int):
        self.key = k
        self.val = v

def qsort(arr, lo, hi):
    if lo >= hi:
        return
    pivot = arr[lo + (hi - lo) // 2].key
    i, j = lo, hi
    while i <= j:
        while arr[i].key < pivot:
            i += 1
        while arr[j].key > pivot:
            j -= 1
        if i <= j:
            arr[i], arr[j] = arr[j], arr[i]
            i += 1
            j -= 1
    qsort(arr, lo, j)
    qsort(arr, i, hi)

started = time.perf_counter()

N = 5000
items = [Item(N - i + (i % 17) * 3, i) for i in range(N)]
build_ms = (time.perf_counter() - started) * 1000

t1 = time.perf_counter()
qsort(items, 0, N - 1)
sort_ms = (time.perf_counter() - t1) * 1000

checksum = sum(item.key for item in items)

ended = time.perf_counter()

print("python sort benchmark")
print(f"checksum={checksum}")
print(f"first_key={items[0].key}")
print(f"last_key={items[-1].key}")
print(f"build_ms={build_ms:.3f}")
print(f"sort_ms={sort_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
