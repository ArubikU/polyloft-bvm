local function makeAdder(n) return function(x) return x + n end end
local function makeAccumulator(start)
  local total = start
  return function(x) total = total + x; return total end
end
local function applyN(f, n, x)
  local result = x
  for i = 0, n - 1 do result = f(result) end
  return result
end
local started = os.clock()
local sum = 0
for i = 0, 4999 do local adder = makeAdder(i); sum = sum + adder(10) end
local acc = makeAccumulator(0)
for i = 0, 99999 do acc(i) end
local final_acc = acc(0)
local inc = function(x) return x + 1 end
local dbl = function(x) return x * 2 end
local result = applyN(inc, 10000, 0)
local result2 = applyN(dbl, 20, 1)
local ended = os.clock()
print("gopher-lua closure benchmark")
print("make_sum=" .. sum)
print("accum_final=" .. final_acc)
print("hof_inc=" .. result)
print("hof_dbl=" .. result2)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
