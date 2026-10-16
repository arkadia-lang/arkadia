# Arkadia 言語ガイド

このドキュメントは Arkadia の文法と意味論をまとめたものです。

- [1. 字句](#1-字句)
- [2. レイアウト規則](#2-レイアウト規則)
- [3. モジュール](#3-モジュール)
- [4. 宣言](#4-宣言)
- [5. 式](#5-式)
- [6. パターン](#6-パターン)
- [7. 型](#7-型)
- [8. 型クラス](#8-型クラス)
- [9. 代数的エフェクト](#9-代数的エフェクト)
- [10. テスト](#10-テスト)
- [11. FFI](#11-ffi)
- [12. 評価と実行時](#12-評価と実行時)

---

## 1. 字句

```haskell
-- 行コメント
{- ブロックコメント {- 入れ子可 -} -}
```

| 種類 | 例 |
|---|---|
| 識別子（値） | `x`, `fooBar`, `x'`, `go_1` |
| 識別子（型・構築子・モジュール） | `Option`, `Some`, `Std.Map` |
| 修飾名 | `Map.insert`, `Std.Map.Map` |
| 整数 | `42`, `-7`, `1_000_000`, `0xff`, `0b1010` |
| 浮動小数 | `3.14`, `1e-9`, `-0.5` |
| 文字 | `'a'`, `'\n'`, `'\u{3042}'` |
| 文字列 | `"hello\n"` |
| 補間文字列 | `"x = ${x}, sum = ${a + b}"`（`Display` クラスで文字列化） |
| 生の複数行文字列 | `"""..."""`（先頭の改行は除去、エスケープなし） |
| 演算子 | `!#$%&*+./<=>?@\^|-~:` の並び |

負の数リテラル: `-` の直後に数字があり、`-` の前が空白か開き括弧ならリテラルになります（`f -1` は `f (-1)`、`x-1` と `x - 1` は減算）。

予約語: `module import as type alias class instance effect where let in match with handle return foreign infixl infixr infix private test deriving`

タブ文字はインデントに使えません（スペースのみ）。

## 2. レイアウト規則

ブロックはインデントで表します。

- 行末が **ブロック開始記号** `=`, `->`, `where`, `with` で終わり、次の行がより深く字下げされていると、新しいブロックが始まります。
- ブロック内で同じ桁から始まる行は新しい文（要素）です。
- 開始記号で終わらない行の後の、より深い行は **継続行** です。
- ブロックより浅い行でブロックが閉じます。
- 括弧 `( [ {` の中では改行は無視されます（ただし開始記号の後の字下げブロックは使えます）。
- `;` で一行に複数の文を書けます: `\_ -> tick (); tick ()`

```haskell
main () =                  -- `=` の後の字下げ → ブロック
  let xs = [1, 2, 3]
  println (show xs)        -- 同じ桁 → 次の文
  let total = sum xs
    + 100                  -- 開始記号で終わっていない → 継続行
  match total with         -- `with` の後 → アームのブロック
    0 -> println "zero"
    n | n > 100 -> println "big"
      | otherwise ->       -- ガードの継続
          println "small"
```

## 3. モジュール

1 ファイル = 1 モジュール。モジュール名はパスと一致します（`src/Data/Queue.ark` → `Data.Queue`）。
エントリファイルでは `module` 宣言を省略できます（`Main` になります）。

```haskell
module Data.Queue

import Std.Map              -- 全ての名前を非修飾で、かつ Map.x として使える
import Std.Map as M         -- 修飾名 M.x のみ
import Std.Map (insert, Map) -- 指定した名前だけを非修飾で
import Std.Map (Map(..))    -- 型とその構築子
```

`Prelude` は暗黙にインポートされます。トップレベルの定義は既定で公開され、`private` を付けると非公開になります。
インスタンスはプログラム全体で共有されます。

## 4. 宣言

### 関数と値

```haskell
-- 型シグネチャ（省略可能。省略時は推論される）
gcd : Int -> Int -> Int
-- 複数の等式（上から順に照合）
gcd a 0 = a
gcd a b = gcd b (a % b)

-- ガード
sign : Int -> Int
sign n
  | n > 0 = 1
  | n < 0 = -1
  | otherwise = 0

-- 演算子の定義と結合性
infixl 6 <+>
(<+>) : Vec -> Vec -> Vec
Vec a b <+> Vec c d = Vec (a + c) (b + d)

-- 値（トップレベルの値は純粋でなければならない）
primes = [2, 3, 5, 7]
```

ガードが全て失敗すると次の等式へフォールスルーします。どの等式にも一致しない可能性があるとコンパイルエラーになります（網羅性検査）。

### データ型

```haskell
type Shape
  = Circle Float
  | Rect Float Float
  deriving (Show, Eq, Ord)

type Tree a = Leaf | Node (Tree a) a (Tree a)

-- 構築子のない型は不透明型（FFI 用）
type Handle

-- 型エイリアス
alias Point = (Float, Float)
```

`deriving` できるのは `Eq`, `Ord`, `Show` です。

## 5. 式

| 構文 | 例 |
|---|---|
| 関数適用（左結合・カリー化） | `f x y` |
| 演算子 | `a + b * c`、関数を中置: ``x `mod` 3`` |
| 演算子を関数として | `(+)`, `foldl (+) 0 xs` |
| セクション | `(+ 1)`, `(10 -)`, ``(`div` 2)`` |
| ラムダ | `\x y -> x + y`, `\(a, b) -> a` |
| let | `let x = 1` (ブロック内の文), `let x = 1 in x + 1` |
| ローカル関数 | `let go acc n = ...`（再帰可） |
| ローカル値 | `let x = x + 1`（値の束縛は非再帰。右辺の `x` は外側の `x`） |
| パターン束縛 | `let (q, r) = quotRem a b` |
| match | `match e with` + アームのブロック |
| handle | `handle e with` + 節のブロック（9 章） |
| タプル / ユニット | `(1, "a")`, `()` |
| リスト / 範囲 | `[1, 2, 3]`, `x :: xs`, `[1 .. 10]` |
| 型注釈 | `(e : List Int)` |
| パイプ・合成 | `xs \|> map f`, `f <\| x`, `f >> g`, `g << f` |
| ブロックの引数 | `forEach xs \x ->` の後に字下げした本体 |

`&&` と `||` は短絡評価されます。それ以外の関数は全て正格です。

ブロックの最後の文がブロックの値です。途中の文の値は `Unit` でなければなりません（捨てたい場合は `let _ = ...`）。

`if` 式はありません。`match` とガードを使います。

```haskell
max a b = match a >= b with
  True -> a
  False -> b
```

## 6. パターン

| パターン | 例 |
|---|---|
| 変数 / ワイルドカード | `x`, `_` |
| リテラル | `0`, `'a'`, `"text"`, `1.5` |
| 構築子 | `None`, `Some x`, `Node l v r` |
| リスト | `[]`, `[x]`, `[x, y]`, `x :: rest` |
| タプル | `(a, b)` |
| as パターン | `all@(x :: _)` |
| 型注釈 | `(x : Int)` |

関数の引数位置では、引数を取る構築子パターンを括弧で囲みます: `f (Some x) = ...`。

## 7. 型

```haskell
Int  Float  String  Char  Bool  Unit
List a      Option a      Result e a      (a, b)
a -> b                  -- 関数
a -> <IO> b             -- IO エフェクトを伴う関数
a -> <State Int | e> b  -- State Int と、その他のエフェクト e
Eq a => a -> a -> Bool  -- 型クラス制約
```

型変数は暗黙に全称量化されます。型推論は Hindley–Milner を基礎とし、ローカルな `let` も（値制限の下で）多相になります。

### エフェクト行の書き方

- 関数型の矢印にはエフェクト行が付きます。`<IO, State Int>` は閉じた行、`<IO | e>` は開いた行です。
- シグネチャで行を書かない矢印は、**暗黙のエフェクト** を持ちます。
  - カリー化された関数の途中の矢印（部分適用）は純粋です（引数の関数型の中でも同様）。
  - 最後の矢印と、引数の関数型の最後の矢印は、シグネチャ全体で共有される暗黙のエフェクト変数を持ちます。
    つまり `map : (a -> b) -> List a -> List b` は「`f` が起こすエフェクトをそのまま起こす」という意味になります。
  - シグネチャ中に行変数がちょうど一つ書かれていれば、それが暗黙のエフェクトとして使われます。
- 明示的な閉じた行の関数（例 `println : String -> <IO> Unit`）は、より多くのエフェクトが許される文脈でも呼べます。
- データ型のフィールドや、型引数の中（`List (Int -> Int)`、タプルの要素など）の矢印で行を書かないものは純粋（`<>`）です。エフェクトを持つ関数を格納するには `List (Int -> <e> Int)` のように明示します。
- 構築子の適用にはエフェクトがありません（部分適用した構築子は純粋な関数として渡せます）。

## 8. 型クラス

```haskell
class Eq a => Ord a where           -- スーパークラス
  compare : a -> a -> Ordering
  (<) : a -> a -> Bool
  x < y = compare x y == LT          -- デフォルト実装

instance Ord Int where
  compare = primIntCompare

instance Eq a => Eq (List a) where   -- 文脈付きインスタンス
  ...
```

- 単一パラメータの型クラスです。パラメータは高カインドでもよく、カインドは推論されます（`class Functor f` の `f : Type -> Type`）。
- インスタンスの頭部は「型構築子 + 相異なる型変数」です（部分適用可: `instance Functor (Result e)`）。
- 曖昧な型変数は `Unit` に既定化されます。
- 実装は辞書渡しで、静的に決まるインスタンスのメソッド呼び出しは直接呼び出し（`Int` の算術は Go の演算）に最適化されます。

### Functor / Applicative / Monad と純粋性

`Functor`, `Applicative`, `Monad` のメソッドは **純粋な関数**（`<>`）を受け取ります。パーサや状態変換子のように、受け取った関数を「後で」実行するインスタンスでも健全にするためです。
エフェクトを起こす関数でリストを写すには `mapList`、走査には `forEach` / `foldl` / `traverse` を使います（`Foldable` と `Traversable` は関数をその場で呼ぶのでエフェクト多相です）。

## 9. 代数的エフェクト

### 宣言と実行

```haskell
effect State s where
  get : Unit -> s
  put : s -> Unit
```

操作 `get`, `put` は普通の関数として呼べます。型は `get : Unit -> <State s> s` です。
引数を取らない操作は `Unit` を受け取るように宣言します。

### ハンドラ

```haskell
runState : s -> (Unit -> <State s | e> a) -> <e> (a, s)
runState init action =
  let run = handle action () with
    return x -> \s -> (x, s)         -- 本体が値を返したとき（省略時は恒等）
    get () k -> \s -> k s s          -- k は継続（ディープハンドラ）
    put s k -> \_ -> k () s
  run init
```

- `handle e with` は `e` を評価し、その中で起きた操作を節で処理します。全ての操作に節が必要です。
- 継続 `k` を呼ぶと中断した計算が再開し、再開後の計算も同じハンドラの下で動きます（ディープハンドラ）。`k` の結果はハンドラ全体の結果です。
- `k` を呼ばなければ計算は破棄されます（例外）。`k` をデータ構造に保存して後で呼ぶこともできます（ジェネレータ、協調スレッド）。
- 継続はワンショットです（二度目の再開は実行時エラー）。

### 型付け

ハンドラは本体のエフェクト行から自分のエフェクトを一つ取り除きます。

```haskell
catch : (Unit -> <Raise | e> a) -> (String -> <e> a) -> <e> a
```

`main` は `Unit -> <IO> Unit` でなければならず、処理されていないエフェクトはコンパイル時に検出されます。

### mask の自動挿入

ハンドラの節の中で別のエフェクトを使うと、引数として受け取った関数の同名のエフェクトが意図せず内側のハンドラに捕まってしまう、という問題があります（スコープ付きラベルの意味論）。
Arkadia の型検査器はこのような箇所を検出し、呼び出しの周りに *mask* を自動で挿入します。mask された呼び出しの中の操作は、内側のハンドラを飛ばして外側のハンドラに届きます。

### 組み込みのエフェクト

| エフェクト | 説明 |
|---|---|
| `IO` | 外界との入出力。ハンドラを書くことはできない |
| `Fail` | `fail : String -> a`。`runFail` で `Result String a` に変換。テストの失敗にも使われる |

`Std.State`, `Std.Reader`, `Std.Writer`, `Std.Exn`, `Std.Random` も参照してください。

### 実装

ハンドラは形に応じて最も安価な方法で実行されます。

| ハンドラの形 | 実行方法 |
|---|---|
| 全ての節が末尾で一度だけ `k` を呼ぶ（Reader、ロガー等） | 呼び出し元でそのまま節を実行 |
| 節が末尾再開か、`k` を使わない（例外） | 呼び出し元で実行し、中断は panic/recover |
| 状態渡し `return x -> \s -> ...` / `op k -> \s -> k v s'` を初期状態に即適用（State、Writer、Random） | 状態をハンドラのフレームに保持し、呼び出し元で実行 |
| それ以外（継続の保存、非末尾の再開） | 本体を専用のゴルーチンで実行し、操作はメッセージ送信（同時に動くゴルーチンは常に一つ） |

## 10. テスト

```haskell
test "reverse is an involution" =
  assertEq (reverse (reverse [1, 2, 3])) [1, 2, 3]

test "parse fails on garbage" =
  assert (isNone (parseInt "x")) "expected None"
```

テスト本体は `Unit` 型で、`<Fail, IO>` のエフェクトを使えます。`ark test` はプロジェクトの `src/` と `tests/` の全モジュールのテストを実行します。

## 11. FFI

```haskell
foreign sqrt : Float -> Float = "rt.FloatSqrt"
```

`foreign` は Go の関数を Arkadia の関数として束縛します。Go 側の関数は引数の数だけ `rt.Value`（= `any`）を受け取り、`rt.Value` を返します。
パッケージの `native/*.go`（`package main`、`import rt "arkprog/rt"`）に関数を置くと、`foreign` から参照できます。

| Arkadia | Go の表現 |
|---|---|
| `Int` / `Float` / `String` / `Char` / `Bool` | `int64` / `float64` / `string` / `rune` / `bool` |
| `Unit` | `rt.Unit` |
| データ構築子 | `*rt.D0` 〜 `*rt.D3`, `*rt.DN`（`T` がタグ） |
| リスト | `rt.Nil`, `rt.Cons(h, t)`, `rt.ListToSlice`, `rt.SliceToList` |
| `Option` / `Result` | `rt.None`, `rt.Some(x)`, `rt.Ok(x)`, `rt.Err(e)` |
| 関数 | `rt.Apply1(f, x)` などで呼び出す |

## 12. 評価と実行時

- 正格（値呼び）。引数は左から右へ評価されます。
- 自己末尾呼び出し（トップレベル関数・ローカル関数）はループにコンパイルされます。それ以外の再帰も Go の伸長可能なスタックにより深い再帰が可能です。
- 値は一様表現（`any`）ですが、`Int`/`Float` の演算は静的に分かる範囲でボックス化せずに行います。
- メモリは Go の GC が管理します。
- 実行時エラー（`error`、0 除算、パターン不一致、未処理エフェクト）はメッセージを表示して終了します。`ARKADIA_TRACE=1` で Go のスタックトレースを表示します。
