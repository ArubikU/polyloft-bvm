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
