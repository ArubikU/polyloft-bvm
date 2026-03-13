import time
def fib(n):
    if n <= 1:
        return n
    return fib(n-1) + fib(n-2)

started = time.time() * 1000
res = fib(35)
ended = time.time() * 1000
print(res)
print("TIME_PY")
print(int(ended - started))
