import time
started = time.time() * 1000
size = 50
visited = [False] * size

def dfs(node):
    if node >= size:
        return
    if visited[node]:
        return
    visited[node] = True
    dfs(node + 1)
    dfs(node + 2)

dfs(0)
ended = time.time() * 1000
print(5)
print("TIME_PY")
print(int(ended - started))
