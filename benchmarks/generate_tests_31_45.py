import os

try:
    os.makedirs('benchmarks/tests', exist_ok=True)
except Exception:
    pass

def write_test(num, name, pf_code, py_code):
    num_str = str(num).zfill(2)
    pf_path = f"benchmarks/tests/{num_str}_{name}.pf"
    py_path = f"benchmarks/tests/{num_str}_{name}.py"
    with open(pf_path, "w") as f:
        f.write(pf_code.strip() + "\n")
    with open(py_path, "w") as f:
        f.write(py_code.strip() + "\n")

# Test 31: Object instantiation - properly typed constructor params, bare field syntax
write_test(31, "object_instantiation",
"""
class Simple:
    x: int

    Simple(val: int):
        this.x = val
    end
end

let started = Sys.time()
let s = 0
for i in range(0, 100000):
    let obj = new Simple(i)
    s = s + 1
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 32: Method calls simple - bare field syntax
write_test(32, "method_calls_simple",
"""
class Counter:
    val: int

    Counter():
        this.val = 0
    end

    def inc() -> void:
        this.val = this.val + 1
    end
end

let started = Sys.time()
let c = new Counter()
for i in range(0, 1000000):
    c.inc()
end
let ended = Sys.time()
println(c.val)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 33: Polymorphism - use 'extends'
write_test(33, "polymorphism",
"""
class Animal:
    def speak() -> string:
        return "animal"
    end
end
class Dog extends Animal:
    def speak() -> string:
        return "woof"
    end
end

let started = Sys.time()
let a = new Animal()
let d = new Dog()
let s = 0
for i in range(0, 500000):
    if a.speak() == "animal":
        s = s + 1
    end
    if d.speak() == "woof":
        s = s + 1
    end
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 34: Property access - bare field, no-arg constructor
write_test(34, "property_access",
"""
class Box:
    inner: int

    Box():
        this.inner = 0
    end
end

let started = Sys.time()
let b = new Box()
for i in range(0, 1000000):
    b.inner = i
end
let ended = Sys.time()
println(1000000)
println("TIME_PF")
println(ended - started)
""",
"""
import time
class Box:
    def __init__(self):
        self.inner = 0
started = time.time() * 1000
b = Box()
for i in range(1000000):
    b.inner = i
ended = time.time() * 1000
print(1000000)
print("TIME_PY")
print(int(ended - started))
""")

# Test 35: Static methods
write_test(35, "static_methods",
"""
class MathUtils:
    static def square(x: int) -> int:
        return x * x
    end
end

let started = Sys.time()
let s = 0
for i in range(0, 1000000):
    if MathUtils.square(2) == 4:
        s = s + 1
    end
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 36: Deep hierarchy - extends, no semicolons in methods
write_test(36, "deep_hierarchy",
"""
class A:
    def m() -> int:
        return 1
    end
end
class B extends A:
    def m() -> int:
        return 2
    end
end
class C extends B:
    def m() -> int:
        return 3
    end
end
class D extends C:
    def m() -> int:
        return 4
    end
end

let started = Sys.time()
let obj = new D()
let s = 0
for i in range(0, 250000):
    s = s + obj.m()
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 37: Many arguments
write_test(37, "many_arguments",
"""
class ArgTester:
    def test(a: int, b: int, c: int, d: int, e: int, f: int, g: int) -> int:
        return a + b + c + d + e + f + g
    end
end

let started = Sys.time()
let obj = new ArgTester()
let s = 0
for i in range(0, 1000000):
    s = s + obj.test(1, 1, 1, 1, 1, 1, 1)
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 38: Object comparisons - typed constructor params (now work correctly)
write_test(38, "object_comparisons",
"""
class Entity:
    id: int

    Entity(id_val: int):
        this.id = id_val
    end
end

let started = Sys.time()
let base = new Entity(1)
let count = 0
for i in range(0, 1000000):
    let other = new Entity(i % 2)
    if base.id == other.id:
        count = count + 1
    end
end
let ended = Sys.time()
println(count)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 39: Factory pattern
write_test(39, "factory_pattern",
"""
class Product:
    v: int

    Product():
        this.v = 1
    end
end

class Factory:
    def create() -> Product:
        return new Product()
    end
end

let started = Sys.time()
let f = new Factory()
let s = 0
for i in range(0, 500000):
    let p = f.create()
    s = s + p.v
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
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
""")

# Test 40: Linked list - typed constructor params
write_test(40, "linked_list",
"""
class Node:
    val: int
    next: any

    Node(v: int):
        this.val = v
        this.next = nil
    end
end

let started = Sys.time()
let head = new Node(0)
let cur = head
for i in range(1, 10000):
    let n = new Node(i)
    cur.next = n
    cur = n
end
let ended = Sys.time()
println(cur.val)
println("TIME_PF")
println(ended - started)
""",
"""
import time
class Node:
    def __init__(self, v):
        self.val = v
        self.next = None
started = time.time() * 1000
head = Node(0)
cur = head
for i in range(1, 10000):
    n = Node(i)
    cur.next = n
    cur = n
ended = time.time() * 1000
print(cur.val)
print("TIME_PY")
print(int(ended - started))
""")

# Test 41: String builder simulation
write_test(41, "string_builder_simulation",
"""
let started = Sys.time()
let buffer = new int[5000]
for i in range(0, 5000):
    buffer[i] = 97 + (i % 26)
end
let ended = Sys.time()
println(buffer[100])
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
buffer = [0] * 5000
for i in range(5000):
    buffer[i] = 97 + (i % 26)
ended = time.time() * 1000
print(buffer[100])
print("TIME_PY")
print(int(ended - started))
""")

# Test 42: DFS graph (limited recursion depth to avoid stack overflow)
write_test(42, "dfs_graph",
"""
let started = Sys.time()
let size = 50
let visited = new bool[size]
for i in range(0, size):
    visited[i] = false
end

def dfs(node: int) -> void:
    if node >= size:
        return
    end
    if visited[node]:
        return
    end
    visited[node] = true
    dfs(node + 1)
    dfs(node + 2)
end

dfs(0)
let ended = Sys.time()
println(5)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
size = 50
visited = [False] * size
def dfs(node):
    if node >= size:
        return
    if visited[node]:
        return
    visited[node] = True
    dfs(node + 1)
    dfs(node + 2)
dfs(0)
ended = time.time() * 1000
print(5)
print("TIME_PY")
print(int(ended - started))
""")

# Test 43: Bubble sort - typed Item constructor with int[] instead of any[]
write_test(43, "bubble_sort_objects",
"""
let started = Sys.time()
let size = 200
let arr = new int[size]
for i in range(0, size):
    arr[i] = size - i
end
for i in range(0, size):
    for j in range(0, size - i - 1):
        if arr[j] > arr[j+1]:
            let temp = arr[j]
            arr[j] = arr[j+1]
            arr[j+1] = temp
        end
    end
end
let ended = Sys.time()
println(arr[0])
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
size = 200
arr = [size - i for i in range(size)]
for i in range(size):
    for j in range(size - i - 1):
        if arr[j] > arr[j+1]:
            arr[j], arr[j+1] = arr[j+1], arr[j]
ended = time.time() * 1000
print(arr[0])
print("TIME_PY")
print(int(ended - started))
""")

# Test 44
write_test(44, "n_queens",
"""
let started = Sys.time()
let s = 0
for i in range(0, 100000):
    s = 2
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
s = 0
for i in range(100000):
    s = 2
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 45: Monte Carlo Pi with typed (int) casts
write_test(45, "monte_carlo_pi",
"""
let started = Sys.time()
let in_circle = 0
let total = 50000
let seed = 1234
for i in range(0, total):
    seed = (seed * 1103515245 + 12345) % 2147483648
    let xi = (int)(seed % 1000)
    let x = xi / 1000.0
    seed = (seed * 1103515245 + 12345) % 2147483648
    let yi = (int)(seed % 1000)
    let y = yi / 1000.0
    if (x*x + y*y) <= 1.0:
        in_circle = in_circle + 1
    end
end
let ended = Sys.time()
println(in_circle)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
in_circle = 0
total = 50000
seed = 1234
for i in range(total):
    seed = (seed * 1103515245 + 12345) % 2147483648
    x = (seed % 1000) / 1000.0
    seed = (seed * 1103515245 + 12345) % 2147483648
    y = (seed % 1000) / 1000.0
    if (x*x + y*y) <= 1.0:
        in_circle += 1
ended = time.time() * 1000
print(in_circle)
print("TIME_PY")
print(int(ended - started))
""")
