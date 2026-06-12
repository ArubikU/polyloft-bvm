local started = os.clock()
local m = {}
for i = 0, 49999 do m["key" .. i] = i * 2 end
local total = 0
for i = 0, 49999 do total = total + m["key" .. i] end
for i = 0, 19999 do local k = "key" .. (i * 2); m[k] = m[k] + 1 end
local freq = {}
for i = 0, 99999 do
  local k = "w" .. (i % 1000)
  if freq[k] == nil then freq[k] = 0 end
  freq[k] = freq[k] + 1
end
local ended = os.clock()
print("gopher-lua hash benchmark")
print("insert_total_check=" .. total)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
