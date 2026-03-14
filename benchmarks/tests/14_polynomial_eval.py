import time

started = time.time() * 1000
s = 0.0
x = 0.0
for i in range(5000000):
    # Horner form: equivalent polynomial with fewer operations.
    s += (((3.0 * x - 5.0) * x + 2.0) * x - 7.0)
    x += 0.01
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
