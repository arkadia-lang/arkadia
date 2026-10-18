# Changelog

**English** | [日本語](CHANGELOG.ja.md)

All notable changes to this project are recorded here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and version numbers follow [Semantic Versioning](https://semver.org/).
Until 1.0, a minor version update may include breaking changes.

## [Unreleased]

### Language

- Strict evaluation, static typing, every function curried. Control flow is only `match`, guards and recursion (self tail calls are optimized to loops)
- Indentation-based blocks, `where` clauses, `let`, operator definitions and fixity, string interpolation `"${x}"`
- Algebraic data types, records (field access, accessor functions, update, patterns), exhaustiveness checking
- Type classes (superclasses, default implementations, `deriving (Eq, Ord, Show)`) and higher-kinded types
- Indexed types: data types used as kinds, GADTs, closed type families
- Algebraic effects: row-polymorphic effect types, deep handlers, implementation by evidence passing, a fast path for tail-resumptive handlers
- Structured parallel tasks (`Std.Task`). Propagation of cancellation, timeouts and failures, and cancellation points inserted by the compiler
- FFI to Go (`foreign` declarations and a package's `native/*.go`)

### Toolchain

- The `ark` command: `new` / `build` / `run` / `check` / `test` / `fmt` / `doc` / `repl` / `emit` / `lsp` / `version`
- Dependency management with `ark.toml` (path / git) and `ark.lock`
- Language Server and VS Code extension (diagnostics, hover, go to definition, formatting)
- Self-hosting: the compiler is written in Arkadia and reaches a fixed point when building itself

### Standard library

- `Prelude` and the `Std.*` modules: effects (State / Reader / Writer / Exn / Gen / Random),
  collections (List / Map / Set / Array / Queue / Heap / MArray / HashMap), text (String / Char / Regex / Format / Csv / Json),
  numbers (Math / Bits / BigInt), binary data (Bytes / Encoding / Crypto), I/O (IO / Path / Time / Ref / Args),
  concurrency (Task / Channel / Atomic / Mutex), networking (Net / Url / Http server and client)

[Unreleased]: https://github.com/arkadia-lang/arkadia/commits/main
