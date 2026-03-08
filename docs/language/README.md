# Polyloft BVM Language

This section documents the language surface that `polyloft-bvm` supports today.

It is intentionally narrower than the full Polyloft language documentation in the parent repository. The goal here is precision: if a construct is described in this folder, it should correspond to behavior that is already implemented and exercised in the BVM.

## Table of Contents

### Fundamentals
- [Basics](basics.md)
- [Types and Objects](types-and-objects.md)
- [Built-in Interfaces](builtin-interfaces.md)
- [Enums](enums.md)

### Program Structure
- [Imports](imports.md)
- [Control Flow](control-flow.md)

## Scope note

The best source of truth for current support is still the implementation and `e2e_test.go`, but these pages should stay synchronized with that tested surface.