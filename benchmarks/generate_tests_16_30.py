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

# Test 16
write_test(16, "array_append",
"""
let started = Sys.time()
let capacity = 100000
let arr = new int[capacity]
for i in range(0, capacity):
    arr[i] = i
end
let ended = Sys.time()
println(arr[99999])
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
capacity = 100000
arr = [0] * capacity
for i in range(capacity):
    arr[i] = i
ended = time.time() * 1000
print(arr[99999])
print("TIME_PY")
print(int(ended - started))
""")

# Test 17
write_test(17, "array_seq_read",
"""
let capacity = 100000
let arr = new int[capacity]
for i in range(0, capacity):
    arr[i] = i
end
let started = Sys.time()
let s = 0
for i in range(0, capacity):
    s = s + arr[i]
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
capacity = 100000
arr = list(range(capacity))
started = time.time() * 1000
s = 0
for i in range(capacity):
    s += arr[i]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 18
write_test(18, "array_rand_access",
"""
let capacity = 100000
let arr = new int[capacity]
for i in range(0, capacity):
    arr[i] = i
end
let started = Sys.time()
let s = 0
for i in range(0, capacity):
    let idx = (i * 137) % capacity
    s = s + arr[idx]
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
capacity = 100000
arr = list(range(capacity))
started = time.time() * 1000
s = 0
for i in range(capacity):
    idx = (i * 137) % capacity
    s += arr[idx]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 19
write_test(19, "array_sorting",
"""
def quicksort(arr: int[], low: int, high: int) -> void:
    if low < high:
        let pi = partition(arr, low, high)
        quicksort(arr, low, pi - 1)
        quicksort(arr, pi + 1, high)
    end
end
def partition(arr: int[], low: int, high: int) -> int:
    let pivot = arr[high]
    let i = low - 1
    for j in range(low, high):
        if arr[j] <= pivot:
            i = i + 1
            let temp = arr[i]
            arr[i] = arr[j]
            arr[j] = temp
        end
    end
    let temp = arr[i + 1]
    arr[i + 1] = arr[high]
    arr[high] = temp
    return i + 1
end

let size = 5000
let arr = new int[size]
for i in range(0, size):
    arr[i] = (size - i) * 31 % size
end
let started = Sys.time()
quicksort(arr, 0, size - 1)
let ended = Sys.time()
println(arr[2500])
println("TIME_PF")
println(ended - started)
""",
"""
import time
def quicksort(arr, low, high):
    if low < high:
        pi = partition(arr, low, high)
        quicksort(arr, low, pi - 1)
        quicksort(arr, pi + 1, high)
def partition(arr, low, high):
    pivot = arr[high]
    i = low - 1
    for j in range(low, high):
        if arr[j] <= pivot:
            i += 1
            arr[i], arr[j] = arr[j], arr[i]
    arr[i+1], arr[high] = arr[high], arr[i+1]
    return i + 1
size = 5000
arr = [(size - i) * 31 % size for i in range(size)]
started = time.time() * 1000
quicksort(arr, 0, size - 1)
ended = time.time() * 1000
print(arr[2500])
print("TIME_PY")
print(int(ended - started))
""")

# Test 20
write_test(20, "array_slicing",
"""
let size = 50000
let original = new int[size]
for i in range(0, size):
    original[i] = i
end

let started = Sys.time()
let half = 25000
let subset = new int[half]
for i in range(0, half):
    subset[i] = original[i]
end
let slice_sum = 0
for i in range(0, half):
    slice_sum = slice_sum + subset[i]
end
let ended = Sys.time()
println(slice_sum)
println("TIME_PF")
println(ended - started)
""",
"""
import time
size = 50000
original = list(range(size))
started = time.time() * 1000
half = 25000
subset = original[:half]
slice_sum = sum(subset)
ended = time.time() * 1000
print(slice_sum)
print("TIME_PY")
print(int(ended - started))
""")

# Test 21
write_test(21, "map_insertion",
"""
let started = Sys.time()
let m = {}
for i in range(0, 10000):
    m["key" + i] = i
end
let ended = Sys.time()
println(m["key9999"])
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
m = {}
for i in range(10000):
    m["key" + str(i)] = i
ended = time.time() * 1000
print(m["key9999"])
print("TIME_PY")
print(int(ended - started))
""")

# Test 22
write_test(22, "map_retrieval",
"""
let m = {}
for i in range(0, 10000):
    m["key" + i] = i
end
let started = Sys.time()
let s = 0
for i in range(0, 10000):
    s = s + m["key" + i]
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
s = 0
for i in range(10000):
    s += m["key" + str(i)]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 23
write_test(23, "map_updates",
"""
let m = {}
for i in range(0, 10000):
    m["key" + i] = i
end
let started = Sys.time()
for i in range(0, 10000):
    m["key" + i] = m["key" + i] + 5
end
let ended = Sys.time()
println(m["key9999"])
println("TIME_PF")
println(ended - started)
""",
"""
import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
for i in range(10000):
    m["key" + str(i)] = m["key" + str(i)] + 5
ended = time.time() * 1000
print(m["key9999"])
print("TIME_PY")
print(int(ended - started))
""")

# Test 24
write_test(24, "map_deletion",
"""
let m = {}
for i in range(0, 10000):
    m["key" + i] = i
end
let started = Sys.time()
for i in range(0, 5000):
    m["key" + i] = 0
end
let s = 0
for i in range(0, 10000):
    s = s + m["key" + i]
end
let ended = Sys.time()
println(s)
println("TIME_PF")
println(ended - started)
""",
"""
import time
m = {}
for i in range(10000):
    m["key" + str(i)] = i
started = time.time() * 1000
for i in range(5000):
    m["key" + str(i)] = 0
s = sum(m["key" + str(i)] for i in range(10000))
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 25
write_test(25, "string_concat",
"""
let started = Sys.time()
let s = ""
for i in range(0, 2000):
    s = s + "a"
end
let ended = Sys.time()
println(len(s))
println("TIME_PF")
println(ended - started)
""",
"""
import time
started = time.time() * 1000
s = ""
for i in range(2000):
    s += "a"
ended = time.time() * 1000
print(len(s))
print("TIME_PY")
print(int(ended - started))
""")

# Test 26
write_test(26, "string_search",
"""
let started = Sys.time()
let count = 0
for i in range(0, 1000000):
    if "hello world" == "hello world":
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
started = time.time() * 1000
count = 0
for i in range(1000000):
    if "hello world" == "hello world":
        count += 1
ended = time.time() * 1000
print(count)
print("TIME_PY")
print(int(ended - started))
""")

# Test 27
write_test(27, "string_split_join",
"""
let started = Sys.time()
let s = 0
for i in range(0, 100000):
    let parts = new string[3]
    parts[0] = "a"
    parts[1] = "b"
    parts[2] = "c"
    let joined = parts[0] + "," + parts[1] + "," + parts[2]
    if joined == "a,b,c":
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
for i in range(100000):
    parts = ["a", "b", "c"]
    joined = ",".join(parts)
    if joined == "a,b,c":
        s += 1
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 28
write_test(28, "tuple_creation",
"""
let started = Sys.time()
let s = 0
for i in range(0, 1000000):
    let t = new int[2]
    t[0] = i
    t[1] = i + 1
    s = s + t[0]
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
for i in range(1000000):
    t = (i, i + 1)
    s += t[0]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 29
write_test(29, "nested_data_structures",
"""
let started = Sys.time()
let s = 0
for i in range(0, 1000):
    let inner = {}
    inner["val"] = i
    s = s + inner["val"]
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
    inner = {"val": i}
    s += inner["val"]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
""")

# Test 30
write_test(30, "simulated_sets",
"""
let started = Sys.time()
let s = {}
for i in range(0, 10000):
    s["k" + i] = true
end
let count = 0
for i in range(0, 20000):
    if s["k" + i]:
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
started = time.time() * 1000
s = {}
for i in range(10000):
    s["k" + str(i)] = True
count = 0
for i in range(20000):
    if s.get("k" + str(i)):
        count += 1
ended = time.time() * 1000
print(count)
print("TIME_PY")
print(int(ended - started))
""")
