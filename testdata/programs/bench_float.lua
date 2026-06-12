local function leibniz_pi(n)
  local s = 0.0
  local sign = 1.0
  for i = 0, n - 1 do
    s = s + sign / (2.0 * i + 1.0)
    sign = sign * -1.0
  end
  return s * 4.0
end
local function newton_sqrt(x)
  if x <= 0.0 then return 0.0 end
  local g = x / 2.0
  for i = 0, 19 do g = (g + x / g) / 2.0 end
  return g
end
local started = os.clock()
local pi_approx = leibniz_pi(1000000)
local sqrt_sum = 0.0
for i = 1, 50000 do sqrt_sum = sqrt_sum + newton_sqrt(i * 1.0) end
local mandel_count = 0
for px = 0, 99 do
  for py = 0, 99 do
    local cr = px / 50.0 - 1.5
    local ci = py / 50.0 - 1.0
    local zr, zi = 0.0, 0.0
    local escaped = false
    for iter = 0, 49 do
      if zr * zr + zi * zi > 4.0 then escaped = true break end
      local new_zr = zr * zr - zi * zi + cr
      zi = 2.0 * zr * zi + ci
      zr = new_zr
    end
    if not escaped then mandel_count = mandel_count + 1 end
  end
end
local ended = os.clock()
print("gopher-lua float benchmark")
print("mandel_count=" .. mandel_count)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
