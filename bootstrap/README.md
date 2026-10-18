# bootstrap — ブートストラップ用コンパイラ

`bootstrap/` は Go で書かれた Arkadia コンパイラで、Arkadia で書かれた本体（[`../compiler`](../compiler)）を
最初にビルドするためだけに使います。

```sh
just bootstrap    # bin/ark0 をビルド
just build        # bin/ark0 で compiler/ をビルド（bin/ark1）し、bin/ark1 で再ビルド（bin/ark）
```

本体と同じコマンド（`build` / `run` / `emit` など）を備えていますが、日常の開発には `bin/ark` を使ってください。

## 方針

- **凍結**: 言語の機能追加や修正は `compiler/` に入れ、ここには入れません。
  変更するのは、`compiler/src`（とそれが import する標準ライブラリのモジュール）をビルドできなくなるときだけです。
- **テスト**: ここには独自の統合テストを置きません。`tests/compiler_test.go` が、
  このコンパイラでビルドした本体が自分自身を再ビルドして固定点に達することを検査します。
- 標準ライブラリ（`lib/`）はリポジトリのものを埋め込んで使います。Go ランタイムは、エビデンス渡しに移行する前の
  `runtime/rt` を凍結した複製（[`runtime/rt`](runtime/rt)）を埋め込みます。このコンパイラが生成するコードはその呼び出し規約に従い、
  ビルドしたプログラム（セルフホストコンパイラの第 1 段）は一度に一つのゴルーチンだけで動きます。

## 構成

```
cmd/ark            CLI
internal/syntax    字句解析（レイアウト規則）・構文解析・演算子優先順位・脱糖
internal/types     カインド推論・型推論（HM + 型クラス + エフェクト行）・網羅性検査
internal/core      中間言語 Core への変換（辞書・パターン・ハンドラ）
internal/codegen   Core の最適化と Go コード生成
internal/driver    モジュール読み込み・パッケージ管理・ビルド・テスト
internal/lsp       Language Server Protocol 実装
runtime/rt         生成コードにリンクされるランタイム（凍結）
```
