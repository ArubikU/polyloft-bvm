import time
started = time.time() * 1000
buffer = [0] * 5000
for i in range(5000):
    buffer[i] = 97 + (i % 26)
ended = time.time() * 1000
print(buffer[100])
print("TIME_PY")
print(int(ended - started))
