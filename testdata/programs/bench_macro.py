import time

# Macro benchmark: a small in-memory sales-analytics pipeline.
# Line-for-line counterpart of bench_macro.pf: same algorithm and same
# integer arithmetic so the non-timing output is identical.


class Order:
    def __init__(self, c: int, p: int, q: int, pr: int):
        self.cust = c
        self.prod = p
        self.qty = q
        self.price = pr
        self.rev = q * pr


def qsort(arr, lo, hi):
    if lo >= hi:
        return
    pivot = arr[lo + (hi - lo) // 2].rev
    i, j = lo, hi
    while i <= j:
        while arr[i].rev > pivot:
            i += 1
        while arr[j].rev < pivot:
            j -= 1
        if i <= j:
            arr[i], arr[j] = arr[j], arr[i]
            i += 1
            j -= 1
    qsort(arr, lo, j)
    qsort(arr, i, hi)


started = time.perf_counter()

N = 20000
C = 200
P = 50

# Phase 1: generate orders with a deterministic LCG, building objects.
t1 = time.perf_counter()
orders = [None] * N
seed = 12345
for i in range(N):
    seed = (seed * 1103515245 + 12345) % 2147483648
    cust = seed % C
    seed = (seed * 1103515245 + 12345) % 2147483648
    prod = seed % P
    seed = (seed * 1103515245 + 12345) % 2147483648
    qty = 1 + (seed % 10)
    seed = (seed * 1103515245 + 12345) % 2147483648
    price = 100 + (seed % 900)
    orders[i] = Order(cust, prod, qty, price)
gen_ms = (time.perf_counter() - t1) * 1000

# Phase 2: aggregate revenue per customer in a hashed map, count distinct.
t2 = time.perf_counter()
rev_by_cust = {}
num_customers = 0
total_revenue = 0
for order in orders:
    k = "c" + str(order.cust)
    if rev_by_cust.get(k) is None:
        rev_by_cust[k] = 0
        num_customers += 1
    rev_by_cust[k] = rev_by_cust[k] + order.rev
    total_revenue += order.rev
agg_ms = (time.perf_counter() - t2) * 1000

# Phase 3: scan the map for the top customer revenue.
t3 = time.perf_counter()
max_cust_rev = 0
for k in rev_by_cust.keys():
    v = rev_by_cust[k]
    if v > max_cust_rev:
        max_cust_rev = v
scan_ms = (time.perf_counter() - t3) * 1000

# Phase 4: sort all orders by revenue (descending).
t4 = time.perf_counter()
qsort(orders, 0, N - 1)
sort_ms = (time.perf_counter() - t4) * 1000

# Phase 5: build a string report from the top 20 orders.
t5 = time.perf_counter()
report = ""
for i in range(20):
    report = report + "#" + str(i + 1) + ":" + str(orders[i].rev) + ";"
report_ms = (time.perf_counter() - t5) * 1000

checksum = total_revenue + max_cust_rev + orders[0].rev

ended = time.perf_counter()

print("python macro benchmark")
print(f"orders={N}")
print(f"customers={num_customers}")
print(f"total_revenue={total_revenue}")
print(f"max_customer_revenue={max_cust_rev}")
print(f"top_order_revenue={orders[0].rev}")
print(f"report_len={len(report)}")
print(f"checksum={checksum}")
print(f"gen_ms={gen_ms:.3f}")
print(f"agg_ms={agg_ms:.3f}")
print(f"scan_ms={scan_ms:.3f}")
print(f"sort_ms={sort_ms:.3f}")
print(f"report_ms={report_ms:.3f}")
print(f"elapsed_ms={(ended - started) * 1000:.3f}")
