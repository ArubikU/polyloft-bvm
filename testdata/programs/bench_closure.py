import time

def make_adder(n):
    return lambda x: x + n

def make_accumulator(start):
    total = [start]
    def acc(x):
        total[0] += x
        return total[0]
    return acc

def apply_n(f, n, x):
    result = x
    for _ in range(n):
        result = f(result)
    return result

started = time.perf_counter()

# Part 1: create and call many closures
t1 = time.perf_counter()
s = 0
for i in range(5000):
    adder = make_adder(i)
    s += adder(10)
make_ms = (time.perf_counter() - t1) * 1000

# Part 2: stateful closure (accumulator)
t2 = time.perf_counter()
acc = make_accumulator(0)
for i in range(100000):
    acc(i)
final_acc = acc(0)
accum_ms = (time.perf_counter() - t2) * 1000

# Part 3: function composition chain
t3 = time.perf_counter()
inc = lambda x: x + 1
dbl = lambda x: x * 2
result = apply_n(inc, 10000, 0)
result2 = apply_n(dbl, 20, 1)
hof_ms = (time.perf_counter() - t3) * 1000

ended = time.perf_counter()

print("python closure benchmark")
print(f"make_sum={s}")
print(f"accum_final={final_acc}")
print(f"hof_inc={result}")
print(f"hof_dbl={result2}")
print(f"make_ms={make_ms:.3f}")
print(f"accum_ms={accum_ms:.3f}")
print(f"hof_ms={hof_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
