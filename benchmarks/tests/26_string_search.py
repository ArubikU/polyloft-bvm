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
