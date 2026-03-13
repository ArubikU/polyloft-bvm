import time
started = time.time() * 1000
s = {}
for i in range(10000):
    s["k" + str(i)] = True
count = 0
for i in range(20000):
    if s.get("k" + str(i)):
        count += 1
ended = time.time() * 1000
print(count)
print("TIME_PY")
print(int(ended - started))
