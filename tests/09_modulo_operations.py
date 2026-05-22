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
