# compiler — Arkadia コンパイラ `ark`

[English](README.md) | **日本語**

`compiler/` は Arkadia コンパイラ `ark` の本体で、Arkadia 自身で書かれています。
Go で書かれたブートストラップ用コンパイラ（[`../bootstrap`](../bootstrap)）で最初の一段をビルドし、
以降は自分自身でビルドします。ブートストラップは固定点に達します。

```
bin/ark0 (Go)  ──build──▶  bin/ark1  ──build──▶  bin/ark  ──build──▶  bin/ark2
                               │                     │                    │
                               └──── emit した Go コードはすべて同一、bin/ark と bin/ark2 のバイナリも同一 ────┘
```

## ビルドとテスト

```sh
just build        # bin/ark0 → bin/ark1 → bin/ark
just fixpoint     # bin/ark で自分自身をビルドし、各世代の生成コードとバイナリが一致することを確認
just test         # テストスイート（../tests/compiler_test.go）
```

`go test ./...` に含まれる `tests/compiler_test.go` は、上と同じ手順で自分自身がビルドしたコンパイラを作り、次を検査します。

- 自分自身のソースについて、ブートストラップ用コンパイラがビルドした段と同一のコードを生成する（固定点）
- `tests/run` と `examples` の全プログラムの出力が `.out` と一致する
- `tests/errors` の全プログラムについて、期待されるコンパイルエラーを報告する
- `tests/fmt` の整形例と、リポジトリの全ソース（およびインデントを崩したもの）を安定して整形する
- `tests/unit` の単体テストが通る

## 使い方

```sh
bin/ark run examples/json.ark
bin/ark new hello && cd hello && ../bin/ark run && ../bin/ark test
bin/ark build | run | check | test | fmt | emit | doc | repl | lsp | new | version
```

標準ライブラリ（`lib/`）と Go ランタイム（`runtime/rt`）は `$ARKADIA_ROOT`、
または実行ファイルの親ディレクトリから探します（`bin/ark` ならリポジトリのルート）。

## 構成

| モジュール | 内容 |
|---|---|
| `Ark.Ast` | トークン・構文木（型検査器が埋める `Ref` 付き）・型・カインド・各種 Info |
| `Ark.Lexer` | 字句解析・レイアウト規則 |
| `Ark.Format` | ソースの整形（`ark fmt`） |
| `Ark.Parser` | 再帰下降パーサ（エラーは `Exn` エフェクトで脱出） |
| `Ark.Fixity` | 演算子の結合性解決・負号/セクション/補間の脱糖 |
| `Ark.Types` | カインド・型の操作、型の表示、単一化（エフェクト行）、検査器の状態、名前解決、型式の変換 |
| `Ark.Infer` | 式・パターン・束縛の型推論、制約解決、汎化 |
| `Ark.Check` | 宣言（型・エフェクト・クラス・インスタンス・deriving）とモジュール単位の検査 |
| `Ark.Exhaust` | 網羅性検査 |
| `Ark.Loader` / `Ark.Render` | モジュールの読み込み・エラー表示 |
| `Ark.Core` / `Ark.Lower` | 中間言語 Core とその生成 |
| `Ark.Opt` / `Ark.Codegen` | Core の最適化と Go コード生成（エビデンス渡し、キャンセルポイントの挿入） |
| `Ark.Project` / `Ark.Build` | `ark.toml`（path / git 依存、`ark.lock`）、Go モジュールの生成と `go build` |
| `Ark.Repl` / `Ark.Lsp` / `Ark.Json` | REPL、Language Server |
| `Main` | CLI |

## 開発メモ

- **ブートストラップの制約**: `compiler/src` と、それが import する標準ライブラリのモジュールは、
  ブートストラップ用コンパイラ（凍結）でもビルドできる必要があります。新しい言語機能は `compiler/` に実装しますが、
  それをコンパイラ自身のソースや、コンパイラが import する標準ライブラリのモジュールで使うには、先にブートストラップ用コンパイラを更新する必要があります。
  ブートストラップ用コンパイラは凍結したランタイム（`bootstrap/runtime/rt`）を使うので、`runtime/rt` は自由に変更できます。
  ただし `native/*.go` は両方のランタイムに対してコンパイルされるため、両者に共通の API（値の表現など）だけを使います。
- **可変状態**: 型検査器のメタ変数・カインド変数、辞書の解、検査結果の注釈などは
  `Ref`（`newRef` / `readRef` / `writeRef`、`<IO>` エフェクト付き）で表します。
- **呼び出し規約**: 生成される関数はすべて、呼び出し元のエビデンス `ev *rt.Ev`（ハンドラのフレームと実行中のタスク）を最初の引数に取ります。
  foreign 関数は、高階のもの（関数を引数に取る）とターゲットを `"ev:pkg.Func"` と書いたものだけがエビデンスを受け取ります。
  キャンセルポイント `rt.Poll(ev)` は、ループする関数・未知の呼び出し（関数値・エフェクト操作・高階の foreign）をする関数・大域的な参照グラフで再帰する関数の入口に置きます（`Ark.Codegen` の `pollLine`）。
- **エラー**: パーサの脱出と単一化の失敗は `Std.Exn` の `throw` / `try` で表します
  （どちらも継続を捨てるハンドラなので、ランタイムでは panic/recover の高速パスで動きます）。
- **FFI**（`native/native.go`）は標準ライブラリにない汎用の機能だけに限っています:
  可変参照、Go と同じ数値リテラルの解釈と浮動小数の表示、パス操作、`go/format` による整形、
  外部コマンドの実行、LSP 用の標準入出力。コンパイラのロジックはすべて Arkadia で書かれています。
- `native/go.mod` は native ファイルをリポジトリの Go モジュールから除外するためのものです。
