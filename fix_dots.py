import glob
import os

for path in glob.glob('benchmarks/tests/*.pf'):
    with open(path, 'r') as f:
        content = f.read()
    
    content = content.replace('..', '...')
    
    with open(path, 'w') as f:
        f.write(content)

print(f"Fixed {len(glob.glob('benchmarks/tests/*.pf'))} files.")
