local function qsort(arr, lo, hi)
  if lo >= hi then return end
  local mid = lo + math.floor((hi - lo) / 2)
  local pivot = arr[mid].key
  local i, j = lo, hi
  while i <= j do
    while arr[i].key < pivot do i = i + 1 end
    while arr[j].key > pivot do j = j - 1 end
    if i <= j then
      local tmp = arr[i]; arr[i] = arr[j]; arr[j] = tmp
      i = i + 1; j = j - 1
    end
  end
  qsort(arr, lo, j)
  qsort(arr, i, hi)
end
local started = os.clock()
local N = 5000
local items = {}
for i = 0, N - 1 do items[i] = { key = N - i + (i % 17) * 3, val = i } end
local t1 = os.clock()
qsort(items, 0, N - 1)
local checksum = 0
for i = 0, N - 1 do checksum = checksum + items[i].key end
local ended = os.clock()
print("gopher-lua sort benchmark")
print("checksum=" .. checksum)
print("first_key=" .. items[0].key)
print("last_key=" .. items[N - 1].key)
print("elapsed_ms=" .. ((ended - started) * 1000.0))
