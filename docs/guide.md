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
- [10. 並行・並列実行](#10-並行並列実行)
- [11. テスト](#11-テスト)
- [12. FFI](#12-ffi)
- [13. 評価と実行時](#13-評価と実行時)

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

整数リテラルは `Int` ですが、`Float` が期待される場所では `Float` になります（`area r = 3 * r * r` で `r : Float` なら `3` は `3.0`、`[1.5, 2]` は `[1.5, 2.0]`）。型が文脈から決まらなければ `Int` です（`half x = x / 2` は `Int -> Int`）。

負の数リテラル: `-` の直後に数字があり、`-` の前が空白か開き括弧ならリテラルになります（`f -1` は `f (-1)`、`x-1` と `x - 1` は減算）。

予約語: `module import as type alias class instance effect where let in match with handle return foreign infixl infixr infix private test deriving`（`as`, `alias`, `test`, `deriving` は文脈によってのみ予約語）

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

### フォーマッタ

`ark fmt` はトークン列を変えずに空白だけを整えます（改行の位置・コメント・リテラルはそのまま）。

- ブロックは開始記号のある行（`with` の場合は対応する `match` / `handle` の行）から 2 桁下げます。ガード `| 条件 =` の本体は `|` から 4 桁下げます。
- 継続行は、同じ要素の中で同じ桁にあった継続行と揃え、前の行の同種のトークン（`|` や引数）に揃えてあった場合はその揃えを保ち、それ以外は要素の行から 2 桁下げます。
- `=`, `->`, `=>`, `|`, `:` と二項演算子の前後、`,` の後に空白を 1 つ入れます。一行で閉じる `(…)` / `[…]` の内側の空白は取り除き、`{…}` の内側には空白を入れます。
- 行末の空白を削除し、連続する空行を 1 行にまとめ、ファイルを改行 1 つで終えます。行末コメントの前の空白（揃え）は保ちます。

整形結果は再度字句解析され、トークン・レイアウト・コメントが元と一致することを確認してから書き込まれます。

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

宣言（またはその型シグネチャ）の直前に続く `--` コメントはドキュメントとして扱われ、`ark doc` やエディタのホバーに表示されます。ファイル先頭のコメントはモジュールの説明になります。

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

### where 節

等式の後に `where` で局所的な束縛を書けます。束縛はその等式の本体と全てのガードから見え、引数も参照できます。

```haskell
classify : Int -> String
classify n
  | n < small = "small"
  | n < big = "medium"
  where
    small = 10
    big = 100
classify _ = "large"            -- ガードが全て失敗するとここへ

collatz : Int -> Int
collatz n = go n 0
  where
    go : Int -> Int -> Int      -- シグネチャを別の行に書ける
    go 1 steps = steps
    go m steps = go (next m) (steps + 1)
      where next k = match k % 2 with   -- 入れ子にできる
        0 -> k / 2
        _ -> 3 * k + 1
```

- `where` は等式の頭より深く字下げします。本体がブロックのときは、本体と同じ桁に書いてブロックを終えることもできます（下の例）。束縛が一つなら同じ行に書けます（`where y = x * 2`、`;` で複数）。
- `where` の束縛から本体の `let` は見えません（束縛は本体より先に評価されます）。

```haskell
report xs =
  println "total ${total}"
  println "mean ${total / count}"
  where
    total = sum xs
    count = length xs
```

- 束縛は `let` 文と同じ規則で、等式に一致した後、本体やガードより先に **書いた順に** 評価されます（正格評価）。
- 関数・値・パターン束縛（`where (q, r) = quotRem a b`）を書けます。

### ローカルな相互再帰

ブロック内で連続する `let` の関数定義（および `where` の関数定義）は、後に定義された関数を参照すると一つの相互再帰グループになります。

```haskell
parity n =
  let isEven k = match k with
    0 -> True
    _ -> isOdd (k - 1)          -- 後で定義される isOdd を参照
  let isOdd k = match k with
    0 -> False
    _ -> isEven (k - 1)
  isEven n
```

グループの関数はまとめて型推論・汎化されます（型クラス制約を持つ多相関数でも可）。値の束縛は再帰しません（`let x = x + 1` の右辺の `x` は外側の `x`）。

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

`deriving` できるのは `Eq`, `Ord`, `Show`、および `Std.Json` をインポートしたモジュールでは `ToJson`, `FromJson` です（`deriving (Json.ToJson)` のように修飾しても書けます）。

`ToJson` / `FromJson` の導出は次の形の JSON と相互に変換します。

| 構築子 | JSON |
|---|---|
| レコード | `{"name": "ann", "age": 30}`（`Option` 型のフィールドは省略・`null` 可） |
| それ以外（構築子が一つの型） | 引数が一つならその値、複数なら配列、無ければ構築子名の文字列 |
| 構築子が複数の型 | 引数の無い構築子は `"Admin"`、レコードは `{"tag": "Circle", "radius": 1}`、それ以外は `{"tag": "Polygon", "values": [...]}` |

```haskell
import Std.Json as Json

type User = User { name : String, age : Int, email : Option String }
  deriving (Show, Json.ToJson, Json.FromJson)

Json.encode (User { name = "ann", age = 30, email = None })
-- {"name":"ann","age":30,"email":null}
(Json.decode "{\"name\": \"bob\", \"age\": \"x\"}" : Result String User)
-- Err "field 'age': expected an integer, got a string"
```

### レコード

構築子のフィールドには名前を付けられます。

```haskell
type User = User { name : String, age : Int, tags : List String }
  deriving (Show, Eq)

-- 型が同じフィールドはまとめて書ける。複数の構築子にもフィールドを付けられる
type Shape
  = Circle { x, y : Float, r : Float }
  | Rect { x, y : Float, w : Float, h : Float }
```

| 構文 | 例 |
|---|---|
| 構築 | `User { name = "ann", age = 30, tags = [] }`（全フィールドが必要。順序は自由） |
| 省略記法 | `User { name, age, tags = [] }`（`name` は `name = name`） |
| 参照 | `u.name`, `pet.owner.name`, `(f x).age` |
| アクセサ関数 | `.name`（`\r -> r.name`）。`map .name users` |
| 更新 | `{ u \| age = u.age + 1, tags = [] }`（新しい値を返す。型は変わらない） |
| パターン | `User { name, age = a }`（書かなかったフィールドは `_`） |

- `.` の前に空白を入れずに書くとフィールド参照、空白の後や行頭ではアクセサ関数になります（`f u.name` は `f (u.name)`、`f .name` は `f (\r -> r.name)`）。
- フィールドの参照と更新は **レコードの型** で解決されます。型がまだ分からない場合（`\u -> u.name` など）は、囲んでいる束縛の型が決まるまで解決を待ち、それでも分からなければ、そのフィールドを全て持つ型がスコープ内に一つだけあるときにその型を使います。候補が複数あると型注釈を求めるエラーになります。
- 参照・更新できるのは、型の **全ての構築子** が持つフィールドです（`Shape` の `x`, `y`）。
- 構築・更新の値はソースに書いた順に評価されます。
- レコード構築子は位置引数でも使えます（`User "ann" 30 []`、`User n a _`）。
- `deriving Show` はレコード構文で表示します（`User { name = "ann", age = 30, tags = [] }`）。

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
| レコード | `User { name = "a", age = 1 }`, `u.name`, `.name`, `{ u \| age = 2 }`（4 章） |
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
| レコード | `User { name, age = a }` |
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

`Std.State`, `Std.Reader`, `Std.Writer`, `Std.Exn`, `Std.Random`, `Std.Gen`（ジェネレータ）, `Std.Parser`（パーサコンビネータ）, `Std.Http`（`Halt` エフェクトによる HTTP ハンドラの早期リターン）も参照してください。各モジュールの API とドキュメントは `ark doc Std.Http` のように表示できます。

### 実装

ハンドラは形に応じて最も安価な方法で実行されます。

| ハンドラの形 | 実行方法 |
|---|---|
| 全ての節が末尾で一度だけ `k` を呼ぶ（Reader、ロガー等） | 呼び出し元でそのまま節を実行 |
| 節が末尾再開か、`k` を使わない（例外） | 呼び出し元で実行し、中断は panic/recover |
| 状態渡し `return x -> \s -> ...` / `op k -> \s -> k v s'` を初期状態に即適用（State、Writer、Random） | 状態をハンドラのフレームに保持し、呼び出し元で実行 |
| それ以外（継続の保存、非末尾の再開） | 本体を専用のゴルーチンで実行し、操作はメッセージ送信（一つの計算の中で同時に動くゴルーチンは常に一つ） |

ハンドラのスタックはゴルーチンごとに持ちます（[10. 並行・並列実行](#10-並行並列実行)）。
継続を再開しないままハンドラがエラーやキャンセルで抜けると、中断された計算はその場で巻き戻されます。継続を再開せずに捨てた場合は、GC が継続を回収したときに巻き戻されます。

## 10. 並行・並列実行

Arkadia のプログラムは複数の **タスク** を全てのコアで並列に実行できます。タスクはランタイム（Go のスケジューラ）にプリエンプションされるので、長い計算をするタスクが他のタスクを止めることはありません。

並行性の道具はどれもエフェクト型と組み合わせて設計されていて、「並列にしても安全」なことを型が保証します。

| モジュール | 用途 | 型による保証 |
|---|---|---|
| `Std.Par` | 純粋な計算の並列化 | 関数は `<>`（純粋）。データ競合は起こらず、結果は逐次版と同じ |
| `Std.Task` | タスク・構造化された並行性・キャンセル | タスクの本体は `Unit -> <IO> a`。親のハンドラを共有しない |
| `Std.Stm` | 共有状態のトランザクション | トランザクションは `<STM>` 以外のエフェクトを持てない |
| `Std.Channel` | タスク間のメッセージ | — |

いずれも `import Std.Task as Task` のように修飾付きで使うことを想定した名前（`Par.map`, `Task.fork`, `Chan.send` …）になっています。

### 純粋な並列化（Std.Par）

```haskell
import Std.Par as Par

Par.map fib [30, 31, 32, 33]                 -- map と同じ結果
Par.reduce (+) 0 (Par.map score items)       -- 結合的な演算と単位元で畳み込む
Par.sortBy (comparing .age) users            -- 安定な並列ソート
let (a, b) = Par.both (\_ -> left ()) (\_ -> right ())
```

`Par.map : (a -> <> b) -> List a -> List b` のように、渡す関数は純粋でなければなりません（IO や外側で処理されるエフェクトを使うとコンパイルエラー）。
そのため並列に評価しても結果は逐次評価と常に同じで、**プログラムの出力はコア数に依存しません**。

- 純粋な関数の中で `runState` のように自分でハンドラを張るのは自由です（外から見て純粋なら良い）。
- 複数の要素がエラーになった場合、送出されるのは **最初に失敗する要素** のエラーです。逐次の `map` と同じで、これも決定的です。
- 作業はコア数（`Task.processors ()`、`GOMAXPROCS`）だけのタスクが要素の塊を順に取って分担します。`Par` の中で `Par` を呼んでも構いません。
- 呼び出し元のタスクがキャンセルされると、作業中のタスクもキャンセルされます。

### タスクとスコープ（Std.Task）

```haskell
import Std.Task as Task

main () =
  let (a, b) = Task.both (\_ -> download "a") (\_ -> download "b")
  let pages = Task.mapPooled 8 download urls      -- 同時に 8 タスクまで
  let total = Task.scoped (\s ->
    let t1 = Task.fork s (\_ -> work 1)
    let t2 = Task.fork s (\_ -> work 2)
    Task.await t1 + Task.await t2)
  ...
```

タスクは **スコープ** の中で起動します（構造化された並行性）。

- `scoped body` は本体と、その中で `fork` した全てのタスクが終わってから戻ります。スコープを抜けた後に動き続けるタスクはありません。
- あるタスクが失敗すると、同じスコープの他のタスクと本体がキャンセルされ、`scoped` がそのエラーを送出します。本体が失敗した（エラーや例外で抜けた）場合もタスクはキャンセルされ、終わるのを待ってからエラーが伝わります。
- スコープの値をタスクに渡せば、タスクから同じスコープにタスクを追加できます。
- `spawn` はスコープに属さないタスクを起動します。誰も待たないので、エラーは `await` したときにしか分かりません。

| 関数 | 説明 |
|---|---|
| `await t` / `awaitResult t` | 結果を待つ（エラーは送出 / `Err メッセージ`） |
| `cancel t` | キャンセルして、止まるまで待つ。キャンセルされたタスクを `await` するとエラー |
| `both`, `concurrently`, `mapConcurrently`, `forEachConcurrently` | 並列に実行して全ての結果を待つ |
| `mapPooled n f xs` | 同時に n タスクまでのワーカープール |
| `race fs` | 最初に終わったものの結果を返し、残りをキャンセル |
| `timeout secs f` | 時間内に終われば `Some 結果`、そうでなければキャンセルして `None` |
| `sleep`, `yield`, `checkCancelled`, `processors` | 待機・他のタスクに譲る・キャンセルポイント・並列度 |

### エフェクトとタスク

タスクの本体は `Unit -> <IO> a` で、起動元のハンドラを引き継ぎません。そのため、例えば `runState` の内側から `fork` したタスクが同じ状態を書き換えるといった競合は、型検査で弾かれます。

```haskell
runState 0 (\_ -> Task.scoped (\s -> Task.fork s (\_ -> put 1)))
-- error: unhandled effect: this performs <State a | e>, but the surrounding context only allows <IO>
```

タスクの中では、自分で張ったハンドラを自由に使えます（ジェネレータ・パーサ・例外 …）。一方、`scoped` と `timeout` の本体は呼び出し元と同じタスクで動くので、`<IO | e>` の任意のエフェクトを使えます。

タスク間でデータを共有するには次の三つを使います。

- **`Std.Stm`**: 複数の値をまとめて不可分に更新する。
- **`Std.Channel`**: 値を受け渡す（生産者と消費者、ワーカープール）。
- **`Std.Ref`**: 単一の値。`readRef` / `writeRef` / `swapRef` は不可分で、`atomicModifyRef` / `atomicUpdateRef` は他のタスクの書き込みを失わずに更新します（`modifyRef` は読んでから書くので不可分ではありません）。

`Std.MArray` / `Std.HashMap` の各操作もロックで保護されていて、タスク間で共有しても壊れません（複数の操作をまとめて不可分にはなりません）。標準出力への一回の `println` も他の出力と混ざりません。

### キャンセル

キャンセルは協調的で、キャンセルされたタスクは次の **キャンセルポイント** で止まります。

- 関数の呼び出しとループ（自己末尾呼び出し）の反復
- エフェクトの操作
- 待機: `await`, `cancel`, `sleep`（`Std.Time.sleep` も）、チャネルの送受信と `select`、STM の `retry`、HTTP サーバの `accept`

関数の呼び出しがキャンセルポイントなので、ループもエフェクトもない純粋な再帰（素朴な `fib` など）も時間制限で止まります。キャンセルが要求されていない間のコストは、関数呼び出しごとに一回のメモリ読み出しと分岐です。

キャンセルは例外のように捕捉することはできず、必ずタスクの根元まで巻き戻ります（途中のスコープは子タスクの終了を待ちます）。ファイルやネットワークの読み書き、FFI の呼び出しの途中では止まらず、それが終わってから止まります。

### ソフトウェアトランザクショナルメモリ（Std.Stm）

```haskell
import Std.Stm

transfer : TVar Int -> TVar Int -> Int -> <STM> Unit
transfer from to amount =
  let balance = readTVar from
  check (balance >= amount)          -- 足りなければ、from が変わるまで待つ
  writeTVar from (balance - amount)
  modifyTVar to (+ amount)

main () =
  let a = newTVarIO 100
  let b = newTVarIO 0
  atomically (\_ -> transfer a b 30)
  println (show (readTVarIO a, readTVarIO b))   -- (70, 30)
```

- `atomically` はトランザクションを不可分に実行します。他のトランザクションと衝突すると自動的にやり直します。
- `retry` はトランザクションを中断し、読んだ `TVar` のどれかが変わるまで待ってからやり直します。`check cond` は条件が成り立つまで待ちます。
- `orElse a b` は `a` が `retry` したら `a` の書き込みを取り消して `b` を実行します。
- `STM` は操作を持たないエフェクトで、ハンドラを書くことはできません。トランザクションは `<STM>` 以外のエフェクトを持てない（`println` などを書くとコンパイルエラー）ので、再実行しても外から見える副作用が重複しません。

`effect STM` のように操作を持たないエフェクトは誰も処理できないので、`foreign` 関数と組み合わせて、このようにランタイムが実装する能力を表すのに使えます。

実装は TL2（グローバルなバージョンクロックによるスナップショットと、コミット時の読み込み集合の検証）です。読み込みだけのトランザクションはロックを取りません。

### チャネル（Std.Channel）

```haskell
import Std.Channel as Chan

let jobs = Chan.new 16          -- 容量 16（0 ならバッファなし）
Chan.send jobs job
Chan.receive jobs               -- Some 値、閉じて空なら None
Chan.close jobs
Chan.forEach jobs process       -- 閉じられるまで受信

match Chan.select [Chan.onReceive jobs Job, Chan.onReceive quit (\_ -> Quit), Chan.after 1.0 Idle] with
  Job (Some j) -> ...
  Job None -> ...               -- jobs が閉じられた
  Quit -> ...
  Idle -> ...                   -- 1 秒何も来なかった
```

`select` の各選択肢は、起きたことを結果の値に写す純粋な関数を持ちます。結果に応じた処理は `select` の後で `match` して行います。

### 実装

- タスクは Go のゴルーチンで、Go のスケジューラが全コアで実行し、非同期プリエンプションで切り替えます。
- エフェクトハンドラのスタック・所属するタスク・実行中のトランザクションはゴルーチンごとの状態（fiber）として持ちます。fiber はランタイムの `g` ポインタ（amd64 / arm64 ではアセンブリで取得、他のアーキテクチャではゴルーチン ID）をキーとするロックフリーな表から引きます。
- キャンセルポイントは、保留中のキャンセルがあるかを示すグローバルなカウンタを読むだけなので、キャンセルがない間はほぼコストがかかりません。

## 11. テスト

```haskell
test "reverse is an involution" =
  assertEq (reverse (reverse [1, 2, 3])) [1, 2, 3]

test "parse fails on garbage" =
  assert (isNone (parseInt "x")) "expected None"
```

テスト本体は `Unit` 型で、`<Fail, IO>` のエフェクトを使えます。`ark test` はプロジェクトの `src/` と `tests/` の全モジュールのテストを実行します。

## 12. FFI

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

Go の関数は複数のタスクから同時に呼ばれることがあるので、共有する状態は Go 側で保護してください。Go のコードが自分で起動したゴルーチンから Arkadia の関数を呼んでも構いません（ハンドラのスタックは空の状態から始まります）。

## 13. 評価と実行時

- 正格（値呼び）。引数は左から右へ評価されます。
- 自己末尾呼び出し（トップレベル関数・ローカル関数）はループにコンパイルされます。それ以外の再帰も Go の伸長可能なスタックにより深い再帰が可能です。
- 値は一様表現（`any`）ですが、`Int`/`Float` の演算は静的に分かる範囲でボックス化せずに行います。
- メモリは Go の GC が管理します。
- `main` はルートのタスクとして実行されます。`main` が戻るとプログラムは終了し、残っているタスク（`spawn` したもの）も止まります。どのタスクで `exit` を呼んでもプログラムが終了します。
- タスクの中の実行時エラーはそのタスクの失敗になり、`await` やスコープを通じて伝わります。
- 実行時エラー（`error`、0 除算、パターン不一致、未処理エフェクト）はメッセージと、エラーが起きた時点で実行中だった Arkadia の関数（内側から順、再帰はまとめて回数表示）を表示して終了します。`ARKADIA_TRACE=1` で Go のスタックトレースも表示します。

```
runtime error: division by zero
  in Main.average
  in Main.loop (4 times)
  in Main.main
```
