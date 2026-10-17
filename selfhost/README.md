# arkc — Arkadia で書かれた Arkadia コンパイラ

`selfhost/` は Arkadia コンパイラを Arkadia 自身で実装した **セルフホスト版** です。
Go 実装（`cmd/ark`, `internal/`）を忠実に移植したもので、同じ入力に対して
**バイト単位で同一の Go コード** を生成します。ブートストラップは固定点に達します。

```
ark (Go 実装)  ──build──▶  arkc (stage 1)  ──build──▶  arkc (stage 2)  ──build──▶  arkc (stage 3)
                               │                          │                          │
                               └──── emit した Go コードはすべて同一、stage 2 と 3 のバイナリも同一 ────┘
```

## ビルドとブートストラップ

```sh
make selfhost     # Go 実装で selfhost/ をビルド → bin/arkc
make bootstrap    # arkc で自分自身を 2 回ビルドし、各世代の生成コードとバイナリが一致することを確認
```

`go test ./...` に含まれる `TestSelfHost`（`internal/driver/selfhost_test.go`）は次を検査します。

- `tests/run` と `examples` の全プログラムについて、arkc が Go 実装と同一の Go コードを生成する
- `tests/errors` の全プログラムについて、期待されるコンパイルエラーを報告する
- arkc 自身のソースについて同一のコードを生成し、arkc がビルドした arkc（stage 2）も同一のコードを生成する

## 使い方

`ark` と同じコマンドを備えています。

```sh
bin/arkc run examples/json.ark
bin/arkc new hello && cd hello && ../bin/arkc run && ../bin/arkc test
bin/arkc build | run | check | test | emit | doc | repl | lsp | new | version
```

標準ライブラリ（`lib/`）と Go ランタイム（`runtime/rt`）は `$ARKADIA_ROOT`、
または arkc の実行ファイルの親ディレクトリから探します（`bin/arkc` ならリポジトリのルート）。
VS Code 拡張の LSP として使う場合も `ark lsp` と同じ応答を返します。

## 構成

各モジュールは Go 実装のパッケージ・ファイルにほぼ一対一で対応しています。

| モジュール | 内容 | Go 実装 |
|---|---|---|
| `Ark.Ast` | トークン・構文木（型検査器が埋める `Ref` 付き）・型・カインド・各種 Info | `syntax/ast.go`, `syntax/token.go`, `types/types.go`, `types/env.go` |
| `Ark.Lexer` | 字句解析・レイアウト規則 | `syntax/lexer.go`, `syntax/layout.go` |
| `Ark.Parser` | 再帰下降パーサ（エラーは `Exn` エフェクトで脱出） | `syntax/parser.go` |
| `Ark.Fixity` | 演算子の結合性解決・負号/セクション/補間の脱糖 | `syntax/fixity.go` |
| `Ark.Types` | カインド・型の操作、型の表示、単一化（エフェクト行）、検査器の状態、名前解決、型式の変換 | `types/types.go`, `types/unify.go`, `types/check.go` |
| `Ark.Infer` | 式・パターン・束縛の型推論、制約解決、汎化 | `types/infer.go`, `types/binds.go` |
| `Ark.Check` | 宣言（型・エフェクト・クラス・インスタンス・deriving）とモジュール単位の検査 | `types/decls.go`, `types/binds.go` |
| `Ark.Exhaust` | 網羅性検査 | `types/exhaust.go` |
| `Ark.Loader` / `Ark.Render` | モジュールの読み込み・エラー表示 | `driver/load.go`, `diag/diag.go` |
| `Ark.Core` / `Ark.Lower` | 中間言語 Core とその生成 | `core/` |
| `Ark.Opt` / `Ark.Codegen` | Core の最適化と Go コード生成 | `codegen/` |
| `Ark.Project` / `Ark.Build` | `ark.toml`（path / git 依存、`ark.lock`）、Go モジュールの生成と `go build` | `driver/project.go`, `driver/build.go` |
| `Ark.Repl` / `Ark.Lsp` / `Ark.Json` | REPL、Language Server | `driver/repl.go`, `lsp/` |
| `Main` | CLI | `driver/cli.go` |

## 実装メモ

- **可変状態**: 型検査器のメタ変数・カインド変数、辞書の解、検査結果の注釈などは
  `Ref`（`newRef` / `readRef` / `writeRef`、`<IO>` エフェクト付き）で表します。
  Go 実装がポインタで共有している状態を、構文木やレコードに `Ref` を持たせて再現しています。
- **エラー**: パーサの脱出と単一化の失敗は `Std.Exn` の `throw` / `try` で表します
  （どちらも継続を捨てるハンドラなので、ランタイムでは panic/recover の高速パスで動きます）。
- **生成コードの一致**: 一意な名前の採番（`x_12`, `arg$3`, `t45` など）や走査順序まで
  Go 実装と同じになるように移植しています。Go 実装を変更するときは arkc にも同じ変更を入れてください
  （`TestSelfHost` が差分を検出します）。
- **FFI**（`native/native.go`）は標準ライブラリにない汎用の機能だけに限っています:
  可変参照、Go と同じ数値リテラルの解釈と浮動小数の表示、パス操作、`go/format` による整形、
  外部コマンドの実行、LSP 用の標準入出力。コンパイラのロジックはすべて Arkadia で書かれています。
- `native/go.mod` は native ファイルをリポジトリの Go モジュールから除外するためのものです。
