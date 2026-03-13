import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
for i in range(5000):
    m["key" + str(i)] = 0
s = sum(m["key" + str(i)] for i in range(10000))
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
