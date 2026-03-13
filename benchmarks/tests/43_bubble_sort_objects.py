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
