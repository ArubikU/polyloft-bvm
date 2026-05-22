import time
started = time.time() * 1000
s = 0
for i in range(1000):
    inner = {"val": i}
    s += inner["val"]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
