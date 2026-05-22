import time
class ArgTester:
    def test(self, a, b, c, d, e, f, g):
        return a+b+c+d+e+f+g

started = time.time() * 1000
obj = ArgTester()
s = 0
for i in range(1000000):
    s += obj.test(1, 1, 1, 1, 1, 1, 1)
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
