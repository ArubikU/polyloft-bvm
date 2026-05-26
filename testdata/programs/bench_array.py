import time

def sieve(limit):
    is_prime = [True] * (limit + 1)
    is_prime[0] = is_prime[1] = False
    i = 2
    while i * i <= limit:
        if is_prime[i]:
            j = i * i
            while j <= limit:
                is_prime[j] = False
                j += i
        i += 1
    return sum(1 for p in is_prime if p)

started = time.perf_counter()

t1 = time.perf_counter()
prime_count = sieve(500000)
sieve_ms = (time.perf_counter() - t1) * 1000

t2 = time.perf_counter()
squares = [i * i for i in range(10000)]
sq_sum = sum(squares)
comp_ms = (time.perf_counter() - t2) * 1000

t3 = time.perf_counter()
N = 100000
data = [i % 997 for i in range(N)]
prefix = [0] * N
prefix[0] = data[0]
for i in range(1, N):
    prefix[i] = prefix[i-1] + data[i]
prefix_ms = (time.perf_counter() - t3) * 1000

ended = time.perf_counter()

print("python array benchmark")
print(f"prime_count={prime_count}")
print(f"sq_sum={sq_sum}")
print(f"prefix_last={prefix[-1]}")
print(f"sieve_ms={sieve_ms:.3f}")
print(f"comp_ms={comp_ms:.3f}")
print(f"prefix_ms={prefix_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
