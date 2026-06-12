local Circle = {}; Circle.__index = Circle
function Circle.new(r) return setmetatable({ r = r }, Circle) end
function Circle:area() return 3.14159265 * self.r * self.r end
function Circle:perimeter() return 2.0 * 3.14159265 * self.r end
local Rectangle = {}; Rectangle.__index = Rectangle
function Rectangle.new(w, h) return setmetatable({ w = w, h = h }, Rectangle) end
function Rectangle:area() return self.w * self.h end
function Rectangle:perimeter() return 2.0 * (self.w + self.h) end
local Triangle = {}; Triangle.__index = Triangle
function Triangle.new(a, b, c) return setmetatable({ a = a, b = b, c = c }, Triangle) end
function Triangle:area()
  local s = (self.a + self.b + self.c) / 2.0
  return (s * (s - self.a) * (s - self.b) * (s - self.c)) ^ 0.5
end
function Triangle:perimeter() return self.a + self.b + self.c end

local started = os.clock()
local N = 30000
local shapes = {}
for i = 0, N - 1 do
  local r = i % 3
  if r == 0 then shapes[i] = Circle.new(i + 1.0)
  elseif r == 1 then shapes[i] = Rectangle.new(i + 1.0, i + 2.0)
  else shapes[i] = Triangle.new(3.0, 4.0, 5.0) end
end
local total_area = 0.0
for i = 0, N - 1 do total_area = total_area + shapes[i]:area() end
local total_perim = 0.0
for i = 0, N - 1 do total_perim = total_perim + shapes[i]:perimeter() end
local ended = os.clock()
print("gopher-lua polymorphism benchmark")
print("shapes=" .. N)
print("area_check=" .. tostring(total_area > 0))
print("perim_check=" .. tostring(total_perim > 0))
print("elapsed_ms=" .. ((ended - started) * 1000.0))
