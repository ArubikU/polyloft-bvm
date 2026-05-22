import time
started = time.time() * 1000
val = 0.5
for i in range(5000000):
    val *= 1.000001
    val /= 1.000001
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
