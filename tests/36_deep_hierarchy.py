import time
class A:
    def m(self): return 1
class B(A):
    def m(self): return 2
class C(B):
    def m(self): return 3
class D(C):
    def m(self): return 4

started = time.time() * 1000
obj = D()
s = 0
for i in range(250000):
    s += obj.m()
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
