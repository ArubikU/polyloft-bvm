import sys, time
sys.setrecursionlimit(200000)


class Order:
    __slots__ = ("cust", "prod", "qty", "price", "rev")

    def __init__(self, c, p, q, pr):
        self.cust = c; self.prod = p; self.qty = q; self.price = pr; self.rev = q * pr


def qsort(arr, lo, hi):
    if lo >= hi:
        return
    mid = lo + (hi - lo) // 2
    pivot = arr[mid].rev
    i, j = lo, hi
    while i <= j:
        while arr[i].rev > pivot:
            i += 1
        while arr[j].rev < pivot:
            j -= 1
        if i <= j:
            arr[i], arr[j] = arr[j], arr[i]
            i += 1; j -= 1
    qsort(arr, lo, j)
    qsort(arr, i, hi)


started = time.perf_counter()
N = 200000; C = 10000; P = 1000

orders = [None] * N
seed = 1
for i in range(N):
    seed = (seed * 16807) % 2147483647
    cust = seed % C
    seed = (seed * 16807) % 2147483647
    prod = seed % P
    seed = (seed * 16807) % 2147483647
    qty = 1 + (seed % 100)
    seed = (seed * 16807) % 2147483647
    price = 100 + (seed % 900)
    orders[i] = Order(cust, prod, qty, price)

discount = {}
for p in range(P):
    discount["p" + str(p)] = p % 30

rev_by_cust = {}
num_customers = 0
total_revenue = 0
for order in orders:
    k = "c" + str(order.cust)
    if k not in rev_by_cust:
        rev_by_cust[k] = 0
        num_customers += 1
    rev_by_cust[k] = rev_by_cust[k] + order.rev
    total_revenue += order.rev

net_by_prod = {}
max_prod_net = 0
for order in orders:
    disc = discount["p" + str(order.prod)]
    net = order.rev - (order.rev * disc) // 100
    pk = "p" + str(order.prod)
    if pk not in net_by_prod:
        net_by_prod[pk] = 0
    acc = net_by_prod[pk] + net
    net_by_prod[pk] = acc
    if acc > max_prod_net:
        max_prod_net = acc

qsort(orders, 0, N - 1)

report = ""
for i in range(20):
    report = report + "#" + str(i + 1) + ":" + str(orders[i].rev) + ";"

checksum = total_revenue + max_prod_net + orders[0].rev
ended = time.perf_counter()

print("python large macro benchmark")
print("orders=" + str(N))
print("customers=" + str(num_customers))
print("total_revenue=" + str(total_revenue))
print("max_product_net=" + str(max_prod_net))
print("top_order_revenue=" + str(orders[0].rev))
print("report_len=" + str(len(report)))
print("checksum=" + str(checksum))
print("elapsed_ms=" + str((ended - started) * 1000.0))
