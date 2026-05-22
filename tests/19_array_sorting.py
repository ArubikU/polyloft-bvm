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
