# Editor integration

Kryndel includes an LSP server in the `kry` executable. Configure an editor to
start `kry lsp` over standard input/output for `.kry` files and use the language
identifier `kryndel`.

```text
command: kry
arguments: [lsp]
transport: stdio
file extension: .kry
language identifier: kryndel
```

The executable must be available on the editor's `PATH`; otherwise configure
its absolute path. For example, Helix can register the server in
`languages.toml`:

```toml
[language-server.kryndel]
command = "kry"
args = ["lsp"]

[[language]]
name = "kryndel"
language-id = "kryndel"
scope = "source.kryndel"
file-types = ["kry"]
roots = ["kry.toml", ".git"]
language-servers = ["kryndel"]
```

## Supported requests

- Diagnostics are published after opening, editing, or saving a document.
  They come from the same lexer, module resolver, type checker, and IR
  validation used by `kry check`. Open buffers override files on disk, so
  unsaved imports are checked with the text currently shown in the editor.
- Go to definition resolves function overloads to the selected function and
  follows local bindings, struct fields, struct literals, enum variants, and
  imported declarations where the checked AST records the reference.
- Hover shows checked expression types and user function or builtin
  signatures.
- Completion offers language keywords, primitive/container types, builtins,
  visible top-level functions, structs, and enums, along with locals in scope
  and accessible fields after `.` when the receiver is a struct-valued identifier.
- Formatting returns a whole-document edit from the Kryndel formatter.

The server uses full-document synchronization. It converts LSP positions using
UTF-16 code units, including non-BMP characters. Closing a document clears its
published diagnostics. The client must send `shutdown` followed by `exit` for a
clean process status.

The compiler reports its first diagnostic per analysis, so the server
currently publishes at most one compiler diagnostic for each open file.
