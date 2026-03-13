import time
started = time.time() * 1000
m = {}
for i in range(10000):
    m["key" + str(i)] = i
ended = time.time() * 1000
print(m["key9999"])
print("TIME_PY")
print(int(ended - started))
