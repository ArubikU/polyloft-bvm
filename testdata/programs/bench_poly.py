import time
import math

class Shape:
    def area(self): return 0.0
    def perimeter(self): return 0.0

class Circle(Shape):
    def __init__(self, r): self.r = r
    def area(self): return math.pi * self.r * self.r
    def perimeter(self): return 2.0 * math.pi * self.r

class Rectangle(Shape):
    def __init__(self, w, h): self.w = w; self.h = h
    def area(self): return self.w * self.h
    def perimeter(self): return 2.0 * (self.w + self.h)

class Triangle(Shape):
    def __init__(self, a, b, c): self.a = a; self.b = b; self.c = c
    def area(self):
        s = (self.a + self.b + self.c) / 2.0
        return math.sqrt(s * (s-self.a) * (s-self.b) * (s-self.c))
    def perimeter(self): return self.a + self.b + self.c

started = time.perf_counter()

N = 30000
shapes = []
for i in range(N):
    r = i % 3
    if r == 0:   shapes.append(Circle(i + 1.0))
    elif r == 1: shapes.append(Rectangle(i + 1.0, i + 2.0))
    else:        shapes.append(Triangle(3.0, 4.0, 5.0))

t1 = time.perf_counter()
total_area = sum(s.area() for s in shapes)
area_ms = (time.perf_counter() - t1) * 1000

t2 = time.perf_counter()
total_perim = sum(s.perimeter() for s in shapes)
perim_ms = (time.perf_counter() - t2) * 1000

ended = time.perf_counter()

print("python polymorphism benchmark")
print(f"shapes={N}")
print(f"area_check={total_area > 0}")
print(f"perim_check={total_perim > 0}")
print(f"area_ms={area_ms:.3f}")
print(f"perim_ms={perim_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
