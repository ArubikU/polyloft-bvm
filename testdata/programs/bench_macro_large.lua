local function qsort(arr, lo, hi)
  if lo >= hi then return end
  local mid = lo + math.floor((hi - lo) / 2)
  local pivot = arr[mid].rev
  local i, j = lo, hi
  while i <= j do
    while arr[i].rev > pivot do i = i + 1 end
    while arr[j].rev < pivot do j = j - 1 end
    if i <= j then
      local tmp = arr[i]; arr[i] = arr[j]; arr[j] = tmp
      i = i + 1; j = j - 1
    end
  end
  qsort(arr, lo, j)
  qsort(arr, i, hi)
end

local started = os.clock()
local N, C, P = 200000, 10000, 1000

local orders = {}
local seed = 1
for i = 0, N - 1 do
  seed = (seed * 16807) % 2147483647
  local cust = seed % C
  seed = (seed * 16807) % 2147483647
  local prod = seed % P
  seed = (seed * 16807) % 2147483647
  local qty = 1 + (seed % 100)
  seed = (seed * 16807) % 2147483647
  local price = 100 + (seed % 900)
  orders[i] = { cust = cust, prod = prod, qty = qty, price = price, rev = qty * price }
end

local discount = {}
for p = 0, P - 1 do discount["p" .. p] = p % 30 end

local rev_by_cust = {}
local num_customers = 0
local total_revenue = 0
for i = 0, N - 1 do
  local order = orders[i]
  local k = "c" .. order.cust
  if rev_by_cust[k] == nil then
    rev_by_cust[k] = 0
    num_customers = num_customers + 1
  end
  rev_by_cust[k] = rev_by_cust[k] + order.rev
  total_revenue = total_revenue + order.rev
end

local net_by_prod = {}
local max_prod_net = 0
for i = 0, N - 1 do
  local order = orders[i]
  local disc = discount["p" .. order.prod]
  local net = order.rev - math.floor((order.rev * disc) / 100)
  local pk = "p" .. order.prod
  if net_by_prod[pk] == nil then net_by_prod[pk] = 0 end
  local acc = net_by_prod[pk] + net
  net_by_prod[pk] = acc
  if acc > max_prod_net then max_prod_net = acc end
end

qsort(orders, 0, N - 1)

local report = ""
for i = 0, 19 do report = report .. "#" .. (i + 1) .. ":" .. orders[i].rev .. ";" end

local checksum = total_revenue + max_prod_net + orders[0].rev
local ended = os.clock()

print("gopher-lua large macro benchmark")
print("orders=" .. N)
print("customers=" .. num_customers)
print("total_revenue=" .. total_revenue)
print("max_product_net=" .. max_prod_net)
print("top_order_revenue=" .. orders[0].rev)
print("report_len=" .. string.len(report))
print("checksum=" .. checksum)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
