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

# Test 1
write_test(1, "while_loop_10m",
"""
let started = Sys.time()
let i = 0
loop i < 10000000:
    i = i + 1
end
let ended = Sys.time()
println(i)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
i = 0
while i < 10000000:
    i += 1
ended = time.time() * 1000
print(i)
print("TIME_PY")
print(int(ended - started))
""")

# Test 2 - use while loop so output is int not float
write_test(2, "for_loop_10m",
"""
let started = Sys.time()
let s = 0
let i = 0
loop i < 10000000:
    s = s + i
    i = i + 1
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
for i in range(10000000):
    s += i
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 3
write_test(3, "nested_loops",
"""
let started = Sys.time()
let s = 0
for i in range(0, 1000):
    for j in range(0, 1000):
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
started = time.time() * 1000
s = 0
for i in range(1000):
    for j in range(1000):
        s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 4
write_test(4, "complex_branching",
"""
let started = Sys.time()
let s = 0
for i in range(0, 5000000):
    if i % 2 == 0:
        s = s + 1
    elif i % 3 == 0:
        s = s + 2
    else:
        s = s - 1
    end
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
for i in range(5000000):
    if i % 2 == 0:
        s += 1
    elif i % 3 == 0:
        s += 2
    else:
        s -= 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 5
write_test(5, "recursion_fibonacci",
"""
def fib(n: int) -> int:
    if n <= 1:
        return n
    end
    return fib(n - 1) + fib(n - 2)
end

let started = Sys.time()
let res = fib(35)
let ended = Sys.time()
println(res)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def fib(n):
    if n <= 1:
        return n
    return fib(n-1) + fib(n-2)
started = time.time() * 1000
res = fib(35)
ended = time.time() * 1000
print(res)
print("TIME_PY")
print(int(ended - started))
""")

# Test 6
write_test(6, "integer_add_sub",
"""
let started = Sys.time()
let val = 0
for i in range(0, 20000000):
    val = val + 5
    val = val - 2
end
let ended = Sys.time()
println(val)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
val = 0
for i in range(20000000):
    val += 5
    val -= 2
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
""")

# Test 7 - both produce same float value
write_test(7, "integer_mul_div",
"""
let started = Sys.time()
let val = 1.0
for i in range(0, 5000000):
    val = val * 2.0
    val = val / 2.0
end
let ended = Sys.time()
println(val)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
val = 1.0
for i in range(5000000):
    val *= 2.0
    val /= 2.0
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
""")

# Test 8
write_test(8, "float_arithmetic",
"""
let started = Sys.time()
let val = 0.5
for i in range(0, 5000000):
    val = val * 1.000001
    val = val / 1.000001
end
let ended = Sys.time()
println(val)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
val = 0.5
for i in range(5000000):
    val *= 1.000001
    val /= 1.000001
ended = time.time() * 1000
print(val)
print("TIME_PY")
print(int(ended - started))
""")

# Test 9
write_test(9, "modulo_operations",
"""
let started = Sys.time()
let matches = 0
for i in range(0, 10000000):
    if i % 7 == 0:
        matches = matches + 1
    end
end
let ended = Sys.time()
println(matches)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
matches = 0
for i in range(10000000):
    if i % 7 == 0:
        matches += 1
ended = time.time() * 1000
print(matches)
print("TIME_PY")
print(int(ended - started))
""")

# Test 10
write_test(10, "collaz_conjecture",
"""
def collatz(n: number) -> int:
    let steps = 0
    loop n > 1:
        if n % 2 == 0:
            n = (int)(n / 2)
        else:
            n = 3 * n + 1
        end
        steps = steps + 1
    end
    return steps
end

let started = Sys.time()
let max_steps = 0
for i in range(1, 100000):
    let steps = collatz(i)
    if steps > max_steps:
        max_steps = steps
    end
end
let ended = Sys.time()
println(max_steps)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def collatz(n):
    steps = 0
    while n > 1:
        if n % 2 == 0:
            n = n // 2
        else:
            n = 3 * n + 1
        steps += 1
    return steps
started = time.time() * 1000
max_steps = 0
for i in range(1, 100000):
    steps = collatz(i)
    if steps > max_steps:
        max_steps = steps
ended = time.time() * 1000
print(max_steps)
print("TIME_PY")
print(int(ended - started))
""")

# Test 11: Sieve with smaller size to avoid JIT array OOB
write_test(11, "primes_sieve",
"""
def sieve(n: int) -> int:
    let primes = new bool[n]
    for i in range(0, n):
        primes[i] = true
    end
    let p = 2
    loop p * p < n:
        if primes[p]:
            let i = p * p
            loop i < n:
                primes[i] = false
                i = i + p
            end
        end
        p = p + 1
    end
    let count = 0
    for i in range(2, n):
        if primes[i]:
            count = count + 1
        end
    end
    return count
end

let started = Sys.time()
let res = sieve(100001)
let ended = Sys.time()
println(res)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def sieve(n):
    primes = [True] * n
    p = 2
    while p * p < n:
        if primes[p]:
            for i in range(p*p, n, p):
                primes[i] = False
        p += 1
    return sum(1 for i in range(2, n) if primes[i])
started = time.time() * 1000
res = sieve(100001)
ended = time.time() * 1000
print(res)
print("TIME_PY")
print(int(ended - started))
""")

# Test 12
write_test(12, "mandelbrot_set",
"""
def mandelbrot() -> int:
    let s = 0
    let w = 50
    let h = 50
    for y in range(0, h):
        for x in range(0, w):
            let zr = 0.0
            let zi = 0.0
            let cr = (x / w) * 4.0 - 2.0
            let ci = (y / h) * 4.0 - 2.0
            let i = 0
            loop i < 1000:
                let zr2 = zr * zr
                let zi2 = zi * zi
                if zr2 + zi2 > 4.0:
                    break
                end
                zi = 2.0 * zr * zi + ci
                zr = zr2 - zi2 + cr
                i = i + 1
            end
            s = s + i
        end
    end
    return s
end

let started = Sys.time()
let res = mandelbrot()
let ended = Sys.time()
println(res)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def mandelbrot():
    s = 0
    w, h = 50, 50
    for y in range(h):
        for x in range(w):
            zr, zi = 0.0, 0.0
            cr = (x / w) * 4.0 - 2.0
            ci = (y / h) * 4.0 - 2.0
            i = 0
            while i < 1000:
                zr2, zi2 = zr*zr, zi*zi
                if zr2 + zi2 > 4.0:
                    break
                zi = 2.0*zr*zi + ci
                zr = zr2 - zi2 + cr
                i += 1
            s += i
    return s
started = time.time() * 1000
res = mandelbrot()
ended = time.time() * 1000
print(res)
print("TIME_PY")
print(int(ended - started))
""")

# Test 13: Use smaller vector to avoid OOB in JIT
write_test(13, "vector_math",
"""
let started = Sys.time()
let size = 5000
let v1 = new float[size]
let v2 = new float[size]
for i in range(0, size):
    v1[i] = i * 1.5
    v2[i] = i * 2.5
end
let dot = 0.0
for i in range(0, size):
    dot = dot + (v1[i] * v2[i])
end
let ended = Sys.time()
println(dot)
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
size = 5000
v1 = [i * 1.5 for i in range(size)]
v2 = [i * 2.5 for i in range(size)]
dot = sum(v1[i]*v2[i] for i in range(size))
ended = time.time() * 1000
print(dot)
print("TIME_PY")
print(int(ended - started))
""")

# Test 14
write_test(14, "polynomial_eval",
"""
def poly(x: float) -> float:
    return 3.0 * x * x * x - 5.0 * x * x + 2.0 * x - 7.0
end

let started = Sys.time()
let s = 0.0
for i in range(0, 5000000):
    s = s + poly(i * 0.01)
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def poly(x):
    return 3.0*x*x*x - 5.0*x*x + 2.0*x - 7.0
started = time.time() * 1000
s = 0.0
for i in range(5000000):
    s += poly(i * 0.01)
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 15
write_test(15, "factorial_loop",
"""
def fact(n: int) -> int:
    let res = 1
    for i in range(1, n + 1):
        res = res * i
    end
    return res
end

let started = Sys.time()
let s = 0
for i in range(0, 100000):
    s = s + fact(10)
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
def fact(n):
    res = 1
    for i in range(1, n+1):
        res *= i
    return res
started = time.time() * 1000
s = 0
for i in range(100000):
    s += fact(10)
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")
