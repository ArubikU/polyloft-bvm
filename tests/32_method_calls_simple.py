import time
class Counter:
    def __init__(self):
        self.val = 0
    def inc(self):
        self.val += 1

started = time.time() * 1000
c = Counter()
for i in range(1000000):
    c.inc()
ended = time.time() * 1000
print(c.val)
print("TIME_PY")
print(int(ended - started))
