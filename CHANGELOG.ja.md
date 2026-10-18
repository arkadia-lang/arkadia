# 変更履歴

[English](CHANGELOG.md) | **日本語**

このプロジェクトの主な変更を記録します。書式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。
1.0 になるまでは、マイナーバージョンの更新で互換性のない変更が入ることがあります。

## [Unreleased]

### 言語

- 正格評価・静的型付け・全関数カリー化。制御は `match`・ガード・再帰のみ（自己末尾呼び出しはループに最適化）
- インデントによるブロック、`where` 節、`let`、演算子の定義と結合性、文字列補間 `"${x}"`
- 代数的データ型、レコード（フィールド参照・アクセサ関数・更新・パターン）、網羅性検査
- 型クラス（スーパークラス・デフォルト実装・`deriving (Eq, Ord, Show)`）と高カインド型
- インデックス型: データ型のカインドとしての利用、GADT、閉じた型族
- 代数的エフェクト: 行多相のエフェクト型、ディープハンドラ、エビデンス渡しによる実装、末尾再開ハンドラの高速パス
- 構造化された並列タスク（`Std.Task`）。キャンセル・タイムアウト・失敗の伝播と、コンパイラが挿入するキャンセルポイント
- Go への FFI（`foreign` 宣言とパッケージの `native/*.go`）

### ツールチェーン

- `ark` コマンド: `new` / `build` / `run` / `check` / `test` / `fmt` / `doc` / `repl` / `emit` / `lsp` / `version`
- `ark.toml` による依存管理（path / git）と `ark.lock`
- Language Server と VS Code 拡張（診断・ホバー・定義ジャンプ・フォーマット）
- セルフホスト: コンパイラは Arkadia で書かれ、自分自身のビルドで固定点に達する

### 標準ライブラリ

- `Prelude` と `Std.*` の各モジュール: エフェクト（State / Reader / Writer / Exn / Gen / Random）、
  コレクション（List / Map / Set / Array / Queue / Heap / MArray / HashMap）、テキスト（String / Char / Regex / Format / Csv / Json）、
  数値（Math / Bits / BigInt）、バイナリ（Bytes / Encoding / Crypto）、入出力（IO / Path / Time / Ref / Args）、
  並行性（Task / Channel / Atomic / Mutex）、ネットワーク（Net / Url / Http のサーバとクライアント）

[Unreleased]: https://github.com/arkadia-lang/arkadia/commits/main
