local function sieve(limit)
  local is_prime = {}
  for i = 0, limit do is_prime[i] = true end
  is_prime[0] = false; is_prime[1] = false
  local i = 2
  while i * i <= limit do
    if is_prime[i] then
      local j = i * i
      while j <= limit do is_prime[j] = false; j = j + i end
    end
    i = i + 1
  end
  local count = 0
  for p = 0, limit do if is_prime[p] then count = count + 1 end end
  return count
end
local started = os.clock()
local prime_count = sieve(500000)
local sq_sum = 0
for i = 0, 9999 do sq_sum = sq_sum + i * i end
local N = 100000
local data = {}
for i = 0, N - 1 do data[i] = i % 997 end
local prefix = {}
prefix[0] = data[0]
for i = 1, N - 1 do prefix[i] = prefix[i - 1] + data[i] end
local ended = os.clock()
print("gopher-lua array benchmark")
print("prime_count=" .. prime_count)
print("prefix_last=" .. prefix[N - 1])
print("elapsed_ms=" .. ((ended - started) * 1000.0))
