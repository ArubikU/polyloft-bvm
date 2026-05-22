import time
size = 50000
original = list(range(size))
started = time.time() * 1000
half = size // 2
subset = original[:half]
slice_sum = sum(subset)
ended = time.time() * 1000
print(slice_sum)
print("TIME_PY")
print(int(ended - started))
