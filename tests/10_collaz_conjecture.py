import time
def collatz(n):
    steps = 0
    while n > 1:
        if n % 2 == 0:
            n = n // 2
        else:
            n = 3 * n + 1
        steps += 1
    return steps

started = time.time() * 1000
max_steps = 0
for i in range(1, 100000):
    steps = collatz(i)
    if steps > max_steps:
        max_steps = steps
ended = time.time() * 1000
print(max_steps)
print("TIME_PY")
print(int(ended - started))
