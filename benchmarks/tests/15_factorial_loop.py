import time
def fact(n):
    res = 1
    for i in range(1, n+1):
        res *= i
    return res

started = time.time() * 1000
s = 0
for i in range(100000):
    s += fact(10)
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
