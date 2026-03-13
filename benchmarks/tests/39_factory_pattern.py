import time
class Product:
    def __init__(self):
        self.v = 1
class Factory:
    def create(self):
        return Product()

started = time.time() * 1000
f = Factory()
s = 0
for i in range(500000):
    p = f.create()
    s += p.v
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
