import time
started = time.time() * 1000
s = 0
for i in range(1000):
    for j in range(1000):
        s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
