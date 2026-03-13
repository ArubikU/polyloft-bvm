import time
def mandelbrot():
    s = 0
    w, h = 50, 50
    for y in range(h):
        for x in range(w):
            zr, zi = 0.0, 0.0
            cr = (x / w) * 4.0 - 2.0
            ci = (y / h) * 4.0 - 2.0
            i = 0
            while i < 1000:
                zr2, zi2 = zr*zr, zi*zi
                if zr2 + zi2 > 4.0:
                    break
                zi = 2.0*zr*zi + ci
                zr = zr2 - zi2 + cr
                i += 1
            s += i
    return s

started = time.time() * 1000
res = mandelbrot()
ended = time.time() * 1000
print(res)
print("TIME_PY")
print(int(ended - started))
