# Changelog

All notable changes to Kryndel are recorded here.

The format follows the principles of Keep a Changelog, while the project remains in alpha and may still change language rules between minor releases.

## [0.1.0] - 2026-08-25

### Added

- Hand-written lexer with nested block comments, string escapes, numeric literals, operators, and source spans.
- Recursive-descent parser with precedence-aware expressions, functions, blocks, conditions, loops, returns, `break`, and `continue`.
- Static type checker for `Int`, `Float`, `Bool`, `String`, `UiNode`, `Void`, and nominal user-defined structs.
- Immutable bindings by default and explicit `let mut` reassignment.
- Stack-based bytecode compiler and virtual machine.
- User functions, recursion, arithmetic, comparisons, conversions, short-circuit logic, runtime call traces, and typed struct construction and field access.
- Deterministic declarative UI tree with windows, containers, labels, buttons, callbacks, and textual rendering.
- Checksummed KEXE portable artifact format.
- `check`, `run`, `dump`, `build`, `inspect`, and `--version` CLI commands.
- Standard-library-only test suite and runnable examples, including `examples/structs.kry`.
- English language, architecture, and testing documentation.

### Known limitations

- KEXE is a Kryndel package, not a native PE or ELF executable.
- The UI runtime renders a deterministic tree and does not open native windows yet.
- Enums, generics, closures, modules, ownership, borrowing, lifetimes, async tasks, and a language server are not implemented. Struct field mutation is intentionally not specified yet.
## Unreleased

- Added end-to-end unit enums with nominal types, deterministic `MAKE_ENUM`, `EnumValue` printing, equality, and stable diagnostics. Payloads and `match` remain unsupported.
