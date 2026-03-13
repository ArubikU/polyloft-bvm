import time
started = time.time() * 1000
size = 10000
v1 = [i * 1.5 for i in range(size)]
v2 = [i * 2.5 for i in range(size)]
dot = sum(v1[i]*v2[i] for i in range(size))
ended = time.time() * 1000
print(dot)
print("TIME_PY")
print(int(ended - started))
