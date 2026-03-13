import time
class Node:
    def __init__(self, v):
        self.val = v
        self.next = None

started = time.time() * 1000
head = Node(0)
cur = head
for i in range(1, 10000):
    n = Node(i)
    cur.next = n
    cur = n
ended = time.time() * 1000
print(cur.val)
print("TIME_PY")
print(int(ended - started))
