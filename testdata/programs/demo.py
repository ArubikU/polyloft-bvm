import time


class Worker:
    def __init__(self, factor: int) -> None:
        self.factor = factor

    def run(self, limit: int) -> int:
        acc = 0
        for i in range(limit):
            acc += i * self.factor
        return acc


def main() -> None:
    started = time.perf_counter()

    total = 0
    worker = Worker(3)

    run_started = time.perf_counter()
    for _ in range(250):
        total += worker.run(400)
    run_ended = time.perf_counter()

    alloc_total = 0
    alloc_started = time.perf_counter()
    for i in range(1, 600):
        temp = Worker(i)
        alloc_total += temp.factor
    alloc_ended = time.perf_counter()

    ended = time.perf_counter()

    print("python benchmark")
    print(f"checksum={total}")
    print(f"run_loop_ms={(run_ended - run_started) * 1000:.3f}")
    print(f"alloc_checksum={alloc_total}")
    print(f"alloc_loop_ms={(alloc_ended - alloc_started) * 1000:.3f}")
    print(f"elapsed_ms={(ended - started) * 1000:.3f}")


if __name__ == "__main__":
    main()