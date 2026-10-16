# Arkadia Lang

**Arkadia** は、代数的エフェクトと型クラスを備えた、正格評価の静的型付き関数型プログラミング言語です。
ソースコードはネイティブバイナリにコンパイルされ、メモリは GC で管理されます。

```haskell
import Std.State

counter : Unit -> <State Int, IO> Unit
counter () =
  let n = get ()
  println "count = ${n}"
  put (n + 1)

classify : Int -> String
classify n
  | n < 0 = "negative"
  | n == 0 = "zero"
  | otherwise = "positive"

main () =
  let (_, final) = runState 0 (\_ -> counter (); counter ())
  println "final = ${final}"
  println (joinWith ", " (map classify [-5, 0, 7]))
```

## 特徴

| ゴール | Arkadia での実現 |
|---|---|
| 正格評価 | 引数は左から右へ評価してから関数を呼ぶ（値呼び） |
| アドホック多相 | 型クラス（スーパークラス・デフォルト実装・`deriving (Eq, Ord, Show)`）を辞書渡しで実装 |
| 高カインド | `Functor f`, `Monad m`, `Foldable t` など、型構築子を抽象化できる。カインドは推論される |
| 代数的エフェクト | `effect` 宣言・`handle ... with` によるディープハンドラ。エフェクトは行多相の型で追跡され、未処理のエフェクトはコンパイルエラー |
| GC | Go ランタイムの並行 GC を利用 |
| ネイティブバイナリ | Arkadia → Go → `go build` で単一の静的バイナリを生成 |
| 現代的なエコシステム | `ark` コマンド（new / build / run / check / test / doc / repl）、Language Server（VS Code 拡張付き）、`ark.toml` による依存管理（path / git）とロックファイル、テストランナー、Go への FFI |
| 再帰とパターンマッチだけの制御 | `if` もループも無い。`match`・ガード・再帰（自己末尾呼び出しはループに最適化）だけ |
| 空白区切りの関数適用 | `f x y` |
| 全関数がカリー化 | `add 1` は部分適用。既知アリティの完全適用は直接呼び出しに最適化 |
| インデントによるブロック | `=`, `->`, `where`, `with` の後の字下げがブロックになる |
| ガード節 | 関数定義と `match` の両方で `\| 条件 = 式` / `\| 条件 -> 式` |

## インストール

Go 1.22 以降が必要です（コンパイラ本体と、生成コードのビルドの両方に使います）。

```sh
git clone <this repository> arkadia && cd arkadia
go build -o ~/.local/bin/ark ./cmd/ark
```

## クイックスタート

```sh
ark new hello      # プロジェクトを作成
cd hello
ark run            # ビルドして実行  → Hello, Arkadia!
ark test           # tests/ 以下と src/ 以下の test 宣言を実行
ark build          # bin/hello を生成
```

単一ファイルも直接実行できます。

```sh
ark run examples/json.ark
```

## 言語ツアー

### 関数・パターンマッチ・ガード

```haskell
-- 複数の等式とパターン
length : List a -> Int
length [] = 0
length (_ :: xs) = 1 + length xs

-- ガードは次の等式へフォールスルーする
fizz : Int -> String
fizz n = match (n % 3, n % 5) with
  (0, 0) -> "FizzBuzz"
  (0, _) -> "Fizz"
  (_, 0) -> "Buzz"
  _ -> show n

-- ラムダ・部分適用・演算子セクション・パイプ
evensSquared = [1 .. 10] |> filter even |> map (\x -> x * x)
inc = (+ 1)
```

制御構造は `match` とガード、再帰だけです。`Bool` も単なるデータ型なので `match b with True -> ... False -> ...` と書きます。
網羅性はコンパイル時に検査され、漏れがあるとエラーになります。

### データ型と型クラス

```haskell
type Tree a = Leaf | Node (Tree a) a (Tree a)
  deriving (Show, Eq)

instance Functor Tree where
  map _ Leaf = Leaf
  map f (Node l x r) = Node (map f l) (f x) (map f r)

-- 高カインドの型クラスに対して多相な関数
doubleAll : Functor f => f Int -> f Int
doubleAll = map (* 2)

class Shape a where
  area : a -> Float
  describe : a -> String
  describe s = "area = ${area s}"     -- デフォルト実装
```

### 代数的エフェクト

エフェクトは関数型の矢印に `<...>` として現れ、推論されます。ハンドラは継続 `k` を受け取ります。

```haskell
effect Raise where
  raise : String -> a

safeDiv : Int -> Int -> <Raise> Int
safeDiv _ 0 = raise "division by zero"
safeDiv a b = a / b

catch : (Unit -> <Raise | e> a) -> (String -> <e> a) -> <e> a
catch action onError =
  handle action () with
    raise msg _ -> onError msg        -- 継続を捨てる = 例外

effect Yield a where
  yield : a -> Unit                   -- 継続を保存して後で再開 = ジェネレータ
```

- `main` の型は `Unit -> <IO> Unit`。処理されていないユーザー定義エフェクトが残っているとコンパイルエラーになります。
- 引数の関数をハンドラの内側で呼ぶ場合、コンパイラが自動的に *mask* を挿入し、意図しないハンドラに捕まらないようにします。
- 全ての操作節が「末尾で一度だけ再開する」ハンドラ（State の get/put 風、ロガー等）は、コンテキスト切り替えなしの高速パスで実行されます。

詳しくは [言語ガイド](docs/guide.md) を参照してください。

## ツールチェーン

| コマンド | 説明 |
|---|---|
| `ark new <name>` | プロジェクト雛形を作成 |
| `ark build [file]` | ネイティブバイナリを生成（`-o` で出力先） |
| `ark run [file] [-- args]` | ビルドして実行 |
| `ark check [file]` | 型検査のみ |
| `ark test [file]` | `test "名前" = ...` 宣言を実行（`Fail` エフェクトで失敗を報告） |
| `ark doc <Module>` | モジュールの公開 API を表示 |
| `ark repl` | 対話環境（式の評価、定義の追加、`:t` で型表示） |
| `ark emit [file]` | 生成された Go コードを表示 |
| `ark lsp` | Language Server（診断・ホバーで型表示・定義ジャンプ）。VS Code 拡張は [`editors/vscode`](editors/vscode) |

`ark.toml`:

```toml
[package]
name = "myapp"
version = "0.1.0"

[dependencies]
mylib = { path = "../mylib" }
json  = { git = "https://example.com/arkadia-json.git", tag = "v0.1.0" }
```

git 依存は解決したコミットが `ark.lock` に記録されます。パッケージの `native/*.go` はビルドに含まれ、`foreign` 宣言から Go の関数を呼び出せます。

## 標準ライブラリ

| モジュール | 内容 |
|---|---|
| `Prelude` | 基本型（`Option`, `Result`, `List` …）、型クラス（`Eq`, `Ord`, `Show`, `Num`, `Semigroup`, `Monoid`, `Functor`, `Applicative`, `Monad`, `Foldable`, `Traversable`）、リスト・文字列関数、入出力、`Fail` エフェクト |
| `Std.State` / `Std.Reader` / `Std.Writer` | 状態・環境・出力のエフェクト |
| `Std.Exn` | 型付き例外 `Exn e` |
| `Std.Random` | 乱数エフェクト（決定的シード / 時刻シード） |
| `Std.Map` / `Std.Set` | 永続的な順序付きマップ・集合（AVL 木） |
| `Std.Array` | 定数時間でインデックスできる不変配列 |
| `Std.String` / `Std.Bits` / `Std.IO` | 文字列、ビット演算、ファイル・プロセス操作 |

## サンプル

- [`examples/scheduler.ark`](examples/scheduler.ark) — エフェクトで実装した協調スレッド
- [`examples/json.ark`](examples/json.ark) — State と Fail エフェクトによる JSON パーサ
- [`examples/calculator.ark`](examples/calculator.ark) — モナディックなパーサコンビネータ（高カインド）

## 実装の構成

```
cmd/ark            CLI
internal/syntax    字句解析（レイアウト規則）・構文解析・演算子優先順位・脱糖
internal/types     カインド推論・型推論（HM + 型クラス + エフェクト行）・網羅性検査
internal/core      中間言語 Core への変換（辞書・パターン・ハンドラ）
internal/codegen   Core の最適化と Go コード生成
internal/driver    モジュール読み込み・パッケージ管理・ビルド・テスト
internal/lsp       Language Server Protocol 実装
editors/vscode     VS Code 拡張（シンタックスハイライト + LSP クライアント）
runtime/rt         生成コードにリンクされるランタイム（値表現・カリー化・エフェクト）
lib                標準ライブラリ（Arkadia で記述）
```

## 制限事項

- 継続はワンショット（同じ継続を二度再開すると実行時エラー）。非決定性のような多重再開は未対応です。
- ローカルな相互再帰関数は未対応（トップレベルでは可能）。
- 型クラスは単一パラメータのみ。

## ライセンス

MIT
