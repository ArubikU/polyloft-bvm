import time
started = time.time() * 1000
val = 0
for i in range(20000000):
    val += 5
    val -= 2
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
