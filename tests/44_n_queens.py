import time
started = time.time() * 1000
s = 0
for i in range(100000):
    s = 2
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
