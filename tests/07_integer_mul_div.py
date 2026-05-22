import time
started = time.time() * 1000
val = 1.0
for i in range(1, 5000000):
    val *= 2.0
    val /= 2.0
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
