import time
class Entity:
    def __init__(self, id):
        self.id = id

started = time.time() * 1000
base = Entity(1)
count = 0
for i in range(1000000):
    other = Entity(i % 2)
    if base.id == other.id:
        count += 1
ended = time.time() * 1000
print(count)
print("TIME_PY")
print(int(ended - started))
