local function fib(n)
  if n <= 1 then return n end
  return fib(n - 1) + fib(n - 2)
end
local t = os.clock()
local r = fib(35)
local e = os.clock()
print("gopher-lua recursion benchmark")
print("fib(35)=" .. r)
print("elapsed_ms=" .. ((e - t) * 1000.0))
