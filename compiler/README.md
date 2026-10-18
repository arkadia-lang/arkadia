# compiler — the Arkadia compiler `ark`

**English** | [日本語](README.ja.md)

`compiler/` is the Arkadia compiler `ark` itself, written in Arkadia.
Its first stage is built with the bootstrap compiler written in Go ([`../bootstrap`](../bootstrap)),
and from then on it builds itself. The bootstrap reaches a fixed point.

```
bin/ark0 (Go)  ──build──▶  bin/ark1  ──build──▶  bin/ark  ──build──▶  bin/ark2
                               │                     │                    │
                               └──── all emitted Go code is identical; bin/ark and bin/ark2 are identical binaries ────┘
```

## Building and testing

```sh
just build        # bin/ark0 → bin/ark1 → bin/ark
just fixpoint     # build itself with bin/ark and check that each generation's generated code and binary match
just test         # test suite (../tests/compiler_test.go)
```

`tests/compiler_test.go`, which is part of `go test ./...`, builds a compiler that was built by itself, following the same steps as above, and checks that:

- for its own sources, it generates the same code as the stage built by the bootstrap compiler (fixed point)
- the output of every program in `tests/run` and `examples` matches its `.out`
- it reports the expected compile error for every program in `tests/errors`
- it formats the examples in `tests/fmt` and every source in the repository (and versions of them with broken indentation) stably
- the unit tests in `tests/unit` pass

## Usage

```sh
bin/ark run examples/json.ark
bin/ark new hello && cd hello && ../bin/ark run && ../bin/ark test
bin/ark build | run | check | test | fmt | emit | doc | repl | lsp | new | version
```

The standard library (`lib/`) and the Go runtime (`runtime/rt`) are looked up in `$ARKADIA_ROOT`,
or in the parent directory of the executable (the repository root for `bin/ark`).

## Structure

| Module | Contents |
|---|---|
| `Ark.Ast` | Tokens, syntax tree (with `Ref`s filled in by the type checker), types, kinds, and various Info |
| `Ark.Lexer` | Lexing and the layout rule |
| `Ark.Format` | Source formatting (`ark fmt`) |
| `Ark.Parser` | Recursive-descent parser (errors escape with the `Exn` effect) |
| `Ark.Fixity` | Operator fixity resolution, desugaring of negation, sections and interpolation |
| `Ark.Types` | Operations on kinds and types, type display, unification (effect rows), checker state, name resolution, conversion of type expressions |
| `Ark.Infer` | Type inference for expressions, patterns and bindings, constraint solving, generalization |
| `Ark.Check` | Checking declarations (types, effects, classes, instances, deriving) and whole modules |
| `Ark.Exhaust` | Exhaustiveness checking |
| `Ark.Loader` / `Ark.Render` | Module loading and error display |
| `Ark.Core` / `Ark.Lower` | The Core intermediate language and its generation |
| `Ark.Opt` / `Ark.Codegen` | Core optimization and Go code generation (evidence passing, insertion of cancellation points) |
| `Ark.Project` / `Ark.Build` | `ark.toml` (path / git dependencies, `ark.lock`), Go module generation and `go build` |
| `Ark.Repl` / `Ark.Lsp` / `Ark.Json` | REPL, Language Server |
| `Main` | CLI |

## Development notes

- **Bootstrap constraint**: `compiler/src` and the standard library modules it imports must also be buildable
  by the (frozen) bootstrap compiler. New language features are implemented in `compiler/`, but to use one in the compiler's own sources,
  or in standard library modules the compiler imports, the bootstrap compiler has to be updated first.
  The bootstrap compiler uses a frozen runtime (`bootstrap/runtime/rt`), so `runtime/rt` can be changed freely.
  However, `native/*.go` is compiled against both runtimes, so it uses only the API common to both (value representation, etc.).
- **Mutable state**: the type checker's meta variables and kind variables, dictionary solutions, annotations from checking, etc.
  are represented with `Ref` (`newRef` / `readRef` / `writeRef`, with the `<IO>` effect).
- **Calling convention**: every generated function takes the caller's evidence `ev *rt.Ev` (the handler frames and the running task) as its first argument.
  Among foreign functions, only higher-order ones (taking functions as arguments) and those whose target is written `"ev:pkg.Func"` receive the evidence.
  The cancellation point `rt.Poll(ev)` is placed at the entry of functions that loop, make unknown calls (function values, effect operations, higher-order foreign functions), or recurse in the global reference graph (`pollLine` in `Ark.Codegen`).
- **Errors**: parser escapes and unification failures are expressed with `throw` / `try` from `Std.Exn`
  (both are handlers that discard the continuation, so at runtime they take the panic/recover fast path).
- **FFI** (`native/native.go`) is limited to general-purpose functionality missing from the standard library:
  mutable references, parsing numeric literals and printing floats the same way as Go, path operations, formatting with `go/format`,
  running external commands, and standard I/O for the LSP. All compiler logic is written in Arkadia.
- `native/go.mod` exists to exclude the native files from the repository's Go module.
