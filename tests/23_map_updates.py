import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
for i in range(10000):
    m["key" + str(i)] = m["key" + str(i)] + 5
ended = time.time() * 1000
print(m["key9999"])
print("TIME_PY")
print(int(ended - started))
