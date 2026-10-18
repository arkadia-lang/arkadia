# bootstrap — the bootstrap compiler

**English** | [日本語](README.ja.md)

`bootstrap/` is an Arkadia compiler written in Go, used only to build the compiler written in Arkadia ([`../compiler`](../compiler))
for the first time.

```sh
just bootstrap    # build bin/ark0
just build        # build compiler/ with bin/ark0 (bin/ark1), then rebuild it with bin/ark1 (bin/ark)
```

It has the same commands as the main compiler (`build` / `run` / `emit`, etc.), but use `bin/ark` for everyday development.

## Policy

- **Frozen**: language features and fixes go into `compiler/`, not here.
  This compiler is changed only when it can no longer build `compiler/src` (and the standard library modules it imports).
- **Tests**: no integration tests of its own live here. `tests/compiler_test.go` checks that
  the main compiler built with this one rebuilds itself and reaches a fixed point.
- It embeds the repository's standard library (`lib/`). For the Go runtime it embeds a frozen copy ([`runtime/rt`](runtime/rt))
  of `runtime/rt` from before the move to evidence passing. The code this compiler generates follows that calling convention,
  and the programs it builds (the first stage of the self-hosted compiler) run on only one goroutine at a time.

## Structure

```
cmd/ark            CLI
internal/syntax    lexing (layout rule), parsing, operator precedence, desugaring
internal/types     kind inference, type inference (HM + type classes + effect rows), exhaustiveness checking
internal/core      translation to the Core intermediate language (dictionaries, patterns, handlers)
internal/codegen   Core optimization and Go code generation
internal/driver    module loading, package management, building, testing
internal/lsp       Language Server Protocol implementation
runtime/rt         runtime linked into generated code (frozen)
```
