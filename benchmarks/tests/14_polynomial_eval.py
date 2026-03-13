import time
def poly(x):
    return 3.0*x*x*x - 5.0*x*x + 2.0*x - 7.0

started = time.time() * 1000
s = 0.0
for i in range(5000000):
    s += poly(i * 0.01)
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
