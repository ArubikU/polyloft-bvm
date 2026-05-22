import time
started = time.time() * 1000
in_circle = 0
total = 50000
seed = 1234
for i in range(total):
    seed = (seed * 1103515245 + 12345) % 2147483648
    x = (seed % 1000) / 1000.0
    seed = (seed * 1103515245 + 12345) % 2147483648
    y = (seed % 1000) / 1000.0
    if (x*x + y*y) <= 1.0:
        in_circle += 1
ended = time.time() * 1000
print(in_circle)
print("TIME_PY")
print(int(ended - started))
