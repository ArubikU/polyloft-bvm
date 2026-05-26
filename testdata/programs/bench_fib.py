import time

def fib(n: int) -> int:
    if n <= 1:
        return n
    return fib(n - 1) + fib(n - 2)

started = time.perf_counter()
result = fib(35)
ended = time.perf_counter()

print("python recursion benchmark")
print(f"fib(35)={result}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
