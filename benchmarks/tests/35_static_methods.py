import time
class MathUtils:
    @staticmethod
    def square(x):
        return x * x

started = time.time() * 1000
s = 0
for i in range(1000000):
    if MathUtils.square(2) == 4:
        s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
