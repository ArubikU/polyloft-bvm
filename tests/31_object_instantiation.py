import time
class Simple:
    def __init__(self, val):
        self.x = val

started = time.time() * 1000
s = 0
for i in range(100000):
    obj = Simple(i)
    s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
