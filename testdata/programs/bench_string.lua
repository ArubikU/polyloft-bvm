local started = os.clock()
local s = ""
for i = 0, 2999 do s = s .. "x" end
local nums = ""
for i = 0, 4999 do nums = nums .. i .. "," end
local haystack = "the quick brown fox jumps over the lazy dog"
local hits = 0
for i = 0, 49999 do if string.find(haystack, "fox", 1, true) then hits = hits + 1 end end
local ended = os.clock()
print("gopher-lua string benchmark")
print("build_len=" .. string.len(s))
print("conv_len=" .. string.len(nums))
print("search_hits=" .. hits)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
