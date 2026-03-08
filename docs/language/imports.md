# Imports

The `import` statement is fully supported in the BVM and is part of the tested language slice.

## Syntax

Import an entire module namespace:

```pf
import lib.math
```

Import specific exported symbols:

```pf
import polyloft.common { Integer, String }
import polyloft.maps { HashMap }
```

## Resolution behavior

The loader resolves imports in this order:

1. embedded stdlib modules under `polyloft.*`
2. package-style candidates based on project roots such as `src`, `lib`, `libs`
3. local filesystem candidates near the importing file

The BVM also honors `polyloft.toml` project root detection when resolving package roots.

## Visibility rules

Imported symbols respect the current visibility model:

- `public`: accessible across modules
- `protected`: accessible from the same module directory
- `private`: only accessible inside the defining file/module

These rules are validated in cross-file tests.

## Namespace imports

Namespace imports build nested runtime modules so dotted access works as expected.

Example:

```pf
import lib.math
println(lib.math.twice(5))
```

## Embedded stdlib imports

The following style is supported without needing real user files on disk:

```pf
import polyloft.common { Integer, Boolean }
import polyloft.maps { Map, HashMap, SetMap }
```

This works because the stdlib sources are embedded and compiled through the same parse, sema, compile and VM pipeline.