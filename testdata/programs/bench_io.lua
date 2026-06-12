local started = os.clock()
local N = 40000
local content = ""
local buf = {}
for i = 0, N - 1 do
  buf[#buf + 1] = "user" .. (i % 1000) .. ",action" .. (i % 50) .. ",ok\n"
  if #buf >= 1000 then content = content .. table.concat(buf); buf = {} end
end
content = content .. table.concat(buf)
local path = "iobench_tmp.txt"
local f = io.open(path, "w"); f:write(content); f:close()
local total_read = 0
for r = 1, 20 do
  local fr = io.open(path, "r")
  local data = fr:read("*a")
  fr:close()
  total_read = total_read + string.len(data)
end
local ended = os.clock()
print("gopher-lua io benchmark")
print("content_len=" .. string.len(content))
print("total_read=" .. total_read)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
