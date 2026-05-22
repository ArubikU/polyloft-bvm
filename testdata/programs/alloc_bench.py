import time

class ObjectBox:
    def __init__(self, value: int) -> None:
        self.value = value

def main() -> None:
    started = time.perf_counter()
    total = 0
    for i in range(100000):
        temp = ObjectBox(i)
        total += temp.value
    ended = time.perf_counter()
    print("python object instantiation benchmark")
    print(f"checksum={total}")
    print(f"elapsed_ms={(ended - started) * 1000:.3f}")

if __name__ == "__main__":
    main()
