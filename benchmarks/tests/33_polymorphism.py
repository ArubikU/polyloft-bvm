import time
class Animal:
    def speak(self): return "animal"
class Dog(Animal):
    def speak(self): return "woof"

started = time.time() * 1000
a = Animal()
d = Dog()
s = 0
for i in range(500000):
    if a.speak() == "animal":
        s += 1
    if d.speak() == "woof":
        s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
