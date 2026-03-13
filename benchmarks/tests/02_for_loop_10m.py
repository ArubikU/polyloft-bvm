import time
started = time.time() * 1000
s = 0
for i in range(10000000):
    s += i
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
