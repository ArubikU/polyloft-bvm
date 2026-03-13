import time
class Box:
    def __init__(self):
        self.inner = 0

started = time.time() * 1000
b = Box()
s = 0
for i in range(1000000):
    b.inner = i
    s += b.inner
ended = time.time() * 1000
print(1000000)
print("TIME_PY")
print(int(ended - started))
