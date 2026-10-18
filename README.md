<p align="center">
  <img src="docs/assets/logo.png" alt="Arkadia logo" width="160">
</p>

<h1 align="center">Arkadia Lang</h1>

<p align="center"><strong>English</strong> | <a href="README.ja.md">日本語</a></p>

**Arkadia** is a strict, statically typed functional programming language with algebraic effects and type classes.
It compiles to native binaries, and lightweight structured tasks run in parallel on every processor.

```haskell
import Std.State (..)
import Std.Task (parMap)

-- Effects show up in types and are handled by a handler (runState)
counter : Unit -> <State Int, IO> Unit
counter () =
  let n = get ()
  println "count = ${n}"
  put (n + 1)

-- Control flow is just match, guards and recursion
fib : Int -> Int
fib n
  | n < 2 = n
  | otherwise = fib (n - 1) + fib (n - 2)

main () =
  let (_, final) = runState 0 (\_ -> counter (); counter ())
  println "final = ${final}"
  println "${parMap fib [25, 30, 35]}"   -- each element is computed in parallel in its own task
```

> [!NOTE]
> Arkadia is in the 0.x stage of development. Until 1.0, a minor version update may include breaking changes.

## Features

- **Algebraic effects** — Exceptions, state, generators and more can be defined as libraries. Effects are tracked and inferred with row-polymorphic types, and an unhandled effect is a compile error.
- **Structured parallelism** — Tasks run in parallel on every processor, and cancellation, timeouts and failures propagate along their scopes. Even a pure infinite loop can be cancelled from outside.
- **An expressive type system** — Type classes (higher-kinded, `deriving`), records, and indexed types with GADTs and type families. Exhaustiveness is checked at compile time.
- **A small syntax** — No `if` and no loops: control flow is just `match`, guards and recursion. Indentation-based blocks, whitespace function application, and every function curried.
- **Native binaries** — Produces a single static binary via Go, running on the Go runtime's GC and scheduler. Go functions can be called too (FFI).
- **A complete toolchain** — Build, run, test, format, docs, REPL and dependency management, all from the single `ark` command. There is also a Language Server and a VS Code extension.

## Installation

Linux and macOS (amd64, arm64) are supported. `ark` generates Go code and runs `go build`, so **Go 1.22 or later** is required.

Download the archive for your platform from [Releases](https://github.com/arkadia-lang/arkadia/releases), extract it, and add `bin/` to your `PATH`.

```sh
curl -LO https://github.com/arkadia-lang/arkadia/releases/download/v0.1.0/arkadia-0.1.0-linux-amd64.tar.gz
tar xzf arkadia-0.1.0-linux-amd64.tar.gz
export PATH="$PWD/arkadia-0.1.0-linux-amd64/bin:$PATH"
ark version
```

The archive contains `ark` itself, the standard library (`lib/`) and the Go runtime (`runtime/rt/`). Keep the layout of the extracted directory as it is.

To build from source, see [Development](#development).

## Getting started

```sh
ark new hello      # create a project
cd hello
ark run            # build and run  → Hello, Arkadia!
ark test           # run the tests
ark build          # produce bin/hello
```

A single file can also be run directly (`ark run main.ark`).

Main commands:

| Command | Description |
|---|---|
| `ark new <name>` | Create a project skeleton |
| `ark run [file] [-- args]` | Build and run |
| `ark build [file]` | Produce a native binary |
| `ark check [file]` | Type-check only |
| `ark test [file]` | Run the tests |
| `ark fmt [path...]` | Format sources |
| `ark doc <Module>` | Show a module's public API (e.g. `ark doc Std.Map`) |
| `ark repl` | Interactive environment |
| `ark lsp` | Language Server (the VS Code extension is in [`editors/vscode`](editors/vscode)) |

## Documentation

The guide and the standard library reference can also be read at **[arkadia-lang.github.io](https://arkadia-lang.github.io/)** (the site's source is in the [arkadia-lang.github.io](https://github.com/arkadia-lang/arkadia-lang.github.io) repository; the guide and other content are generated from this repository).

- [Language guide](docs/guide.md) — reference for syntax, types, type classes, effects, tasks, testing and FFI
- Standard library — `ark doc <Module>` shows each module's API and documentation
- [Changelog](CHANGELOG.md)

## Examples

- [`examples/parallel.ark`](examples/parallel.ark) — data parallelism with tasks, channel pipelines, and cancelling a pure loop
- [`examples/scheduler.ark`](examples/scheduler.ark) — cooperative threads implemented with effects
- [`examples/json.ark`](examples/json.ark) — a JSON parser using the State and Fail effects
- [`examples/calculator.ark`](examples/calculator.ark) — a calculator with parser combinators and `Result`
- [`examples/lisp.ark`](examples/lisp.ark) — a small Lisp interpreter with closures
- [`examples/http/server.ark`](examples/http/server.ark) — a TODO app with a JSON API and a static page

## Limitations

- Continuations are one-shot (resuming the same continuation twice is a runtime error). Multiple resumption, as in nondeterminism, is not supported.
- Type classes take a single parameter only.
- Indexed types have no kind polymorphism ([details](docs/guide.md#indexed-types)).
- `Ref`, `MArray` and `HashMap` cannot be shared between tasks. To share data between tasks, use `Std.Channel`, `Std.Atomic` or `Std.Mutex`.

## Development

The compiler ([`compiler/`](compiler)) is written in Arkadia; only its first stage is built with a frozen bootstrap compiler written in Go ([`bootstrap/`](bootstrap)).
Building from source requires Go and [just](https://github.com/casey/just) (`nix develop` enters a development environment).

```sh
git clone https://github.com/arkadia-lang/arkadia.git && cd arkadia
just build      # bin/ark0 (Go) → bin/ark1 → bin/ark
just install    # symlink $GOBIN/ark to bin/ark
just test       # test suite
just fixpoint   # check that building itself reaches a fixed point
```

`ark` looks for the standard library and runtime in the parent directory of the executable (the target of the symlink) or in `$ARKADIA_ROOT`.

| Directory | Contents |
|---|---|
| [`compiler`](compiler) | The compiler `ark` ([structure](compiler/README.md)) |
| [`lib`](lib) | Standard library |
| [`runtime/rt`](runtime/rt) | Go runtime linked into generated code |
| [`bootstrap`](bootstrap) | Bootstrap compiler (Go, [overview](bootstrap/README.md)) |
| [`tests`](tests) | Test suite |
| [`editors/vscode`](editors/vscode) | VS Code extension |

The release procedure is in [docs/releasing.md](docs/releasing.md).

## License

[MIT](LICENSE)
