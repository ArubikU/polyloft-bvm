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
