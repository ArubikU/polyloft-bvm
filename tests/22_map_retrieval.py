import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
s = 0
for i in range(10000):
    s += m["key" + str(i)]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
