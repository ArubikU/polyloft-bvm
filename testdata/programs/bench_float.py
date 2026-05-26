import time
import math

def leibniz_pi(n):
    s = 0.0
    sign = 1.0
    for i in range(n):
        s += sign / (2.0 * i + 1.0)
        sign *= -1.0
    return s * 4.0

def newton_sqrt(x):
    if x <= 0.0:
        return 0.0
    g = x / 2.0
    for _ in range(20):
        g = (g + x / g) / 2.0
    return g

started = time.perf_counter()

t1 = time.perf_counter()
pi_approx = leibniz_pi(1_000_000)
leibniz_ms = (time.perf_counter() - t1) * 1000

t2 = time.perf_counter()
sqrt_sum = sum(newton_sqrt(float(i)) for i in range(1, 50001))
newton_ms = (time.perf_counter() - t2) * 1000

t3 = time.perf_counter()
mandel_count = 0
for px in range(100):
    for py in range(100):
        cr = px / 50.0 - 1.5
        ci = py / 50.0 - 1.0
        zr, zi = 0.0, 0.0
        escaped = False
        for _ in range(50):
            if zr*zr + zi*zi > 4.0:
                escaped = True
                break
            zr, zi = zr*zr - zi*zi + cr, 2.0*zr*zi + ci
        if not escaped:
            mandel_count += 1
mandel_ms = (time.perf_counter() - t3) * 1000

ended = time.perf_counter()

print("python float benchmark")
print(f"pi_approx={pi_approx}")
print(f"sqrt_sum_check={sqrt_sum > 0}")
print(f"mandel_count={mandel_count}")
print(f"leibniz_ms={leibniz_ms:.3f}")
print(f"newton_ms={newton_ms:.3f}")
print(f"mandel_ms={mandel_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
