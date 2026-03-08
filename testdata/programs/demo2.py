import time


class Worker:
    def __init__(self, factor: int, bias: int) -> None:
        self.factor = factor
        self.bias = bias

    def run(self, limit: int) -> int:
        acc = 0
        for i in range(limit):
            acc += i * self.factor + self.bias
        return acc

    def run_with_list(self, items: list[int]) -> int:
        total = 0
        for item in items:
            total += item * self.factor
        return total


class PairBox:
    def __init__(self, left: int, right: int) -> None:
        self.left = left
        self.right = right

    def mix(self) -> int:
        return self.left * 3 + self.right * 5


def reduce_boxes(boxes: list[PairBox]) -> int:
    total = 0
    for box in boxes:
        total += box.mix()
    return total


def make_scaler(factor: int):
    def scale(value: int) -> int:
        return value * factor

    return scale


def update_table(values: list[int]) -> int:
    table: dict[str, int] = {}
    total = 0
    for value in values:
        key = f"k{value % 11}"
        current = table.get(key, 0)
        next_value = current + value
        table[key] = next_value
        total += next_value
    return total + len(table)


def slice_and_sum(values: list[int]) -> int:
    middle = values[10:90]
    total = 0
    for i in range(0, len(middle), 3):
        total += middle[i]
    return total


def indexed_update(values: list[int]) -> int:
    total = 0
    for i in range(len(values)):
        values[i] = values[i] + i
        total += values[i]
    return total


def main() -> None:
    started = time.perf_counter()

    worker = Worker(7, 3)
    values = [i for i in range(120)]
    boxes = [PairBox(i, i + 1) for i in range(80)]
    scaler = make_scaler(9)

    method_started = time.perf_counter()
    method_total = 0
    for _ in range(180):
        method_total += worker.run(220)
    method_ended = time.perf_counter()

    list_started = time.perf_counter()
    list_total = 0
    for _ in range(220):
        list_total += worker.run_with_list(values)
    list_ended = time.perf_counter()

    object_started = time.perf_counter()
    object_total = 0
    for _ in range(200):
        object_total += reduce_boxes(boxes)
    object_ended = time.perf_counter()

    closure_started = time.perf_counter()
    closure_total = 0
    for i in range(5000):
        closure_total += scaler(i)
    closure_ended = time.perf_counter()

    container_started = time.perf_counter()
    table_total = update_table(values)
    slice_total = slice_and_sum(values)
    index_total = indexed_update(values[:])
    container_ended = time.perf_counter()

    ended = time.perf_counter()

    print("python benchmark demo2")
    print(f"method_total={method_total}")
    print(f"method_ms={(method_ended - method_started) * 1000:.3f}")
    print(f"list_total={list_total}")
    print(f"list_ms={(list_ended - list_started) * 1000:.3f}")
    print(f"object_total={object_total}")
    print(f"object_ms={(object_ended - object_started) * 1000:.3f}")
    print(f"closure_total={closure_total}")
    print(f"closure_ms={(closure_ended - closure_started) * 1000:.3f}")
    print(f"table_total={table_total}")
    print(f"slice_total={slice_total}")
    print(f"index_total={index_total}")
    print(f"container_ms={(container_ended - container_started) * 1000:.3f}")
    print(f"elapsed_ms={(ended - started) * 1000:.3f}")


if __name__ == "__main__":
    main()