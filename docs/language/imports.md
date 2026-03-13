# Imports

The `import` statement is fully supported in polyloft-bvm and is part of the language slice exercised by the repository tests.

## Syntax

Import an entire module namespace:

```pf
import lib.math
```

Import selected exported symbols:

```pf
import polyloft.common { Integer, String }
import polyloft.maps { HashMap }
```

## Resolution Order

The loader resolves imports in this order:

1. embedded standard-library modules under `polyloft.*`
2. package-style candidates rooted in project folders such as `src`, `lib`, and `libs`
3. local filesystem candidates near the importing file

When a project contains `polyloft.toml`, the loader also uses it to detect project roots for package-style resolution.

## Visibility

Imported symbols obey the current module visibility rules:

- `public` allows access across modules
- `protected` allows access within the same module directory
- `private` restricts access to the defining file or module

These rules are validated by the BVM cross-file test suite.

## Namespace Imports

Namespace imports build nested runtime module objects so dotted access works as expected.

```pf
import lib.math
println(lib.math.twice(5))
```

## Embedded Standard Library Imports

Embedded `polyloft.*` modules can be imported without corresponding user files on disk.

```pf
import polyloft.common { Integer, Boolean }
import polyloft.maps { Map, HashMap, SetMap }
```

This works because the standard-library sources are embedded and compiled through the same parse, semantic, compiler, and VM pipeline as user modules.