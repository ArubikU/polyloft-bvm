# Polyloft BVM Documentation

Welcome to the `polyloft-bvm` documentation. This section documents the bytecode VM implementation as it exists today: the supported language slice, the runtime model, the embedded standard library, and the current command-line workflow.

The parent Polyloft repository contains broader language documentation, but not all of it matches the actual implementation status of `polyloft-bvm`. For BVM work, prefer the local docs in this directory.

## Table of Contents

### Getting Started
- [Quick Start Guide](quickstart.md)
- [Project Overview](../README.md)

### Runtime & Language Model
- [Runtime Model](runtime-model.md)
- [Language Overview](language/README.md)
- [Basics](language/basics.md)
- [Types and Objects](language/types-and-objects.md)
- [Built-in Interfaces](language/builtin-interfaces.md)
- [Enums](language/enums.md)
- [Imports](language/imports.md)
- [Control Flow](language/control-flow.md)

### Standard Library
- [Stdlib Overview](stdlib.md)
- [Stdlib Index](stdlib/README.md)
- [polyloft.common](stdlib/common.md)
- [polyloft.maps](stdlib/maps.md)
- [polyloft.collections](stdlib/collections.md)
- [polyloft.function](stdlib/function.md)

### Contributor Reference
- [Architecture](architecture.md)

### Reference
- [Quick Reference](QUICK_REFERENCE.md)

## Scope Note

These docs are intentionally implementation-driven. They describe what the BVM supports now, not the full intended future language surface.

When behavior differs from the parent repository, these local docs win.