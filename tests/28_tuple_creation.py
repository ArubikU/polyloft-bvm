import time
started = time.time() * 1000
s = 0
for i in range(1000000):
    t = (i, i + 1)
    s += t[0]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
