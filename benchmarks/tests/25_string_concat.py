import time
started = time.time() * 1000
s = ""
for i in range(2000):
    s += "a"
ended = time.time() * 1000
print(len(s))
print("TIME_PY")
print(int(ended - started))
