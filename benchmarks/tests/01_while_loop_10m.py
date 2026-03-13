import time
started = time.time() * 1000
i = 0
while i < 10000000:
    i += 1
ended = time.time() * 1000
print(i)
print("TIME_PY")
print(int(ended - started))
