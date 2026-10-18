# Arkadia 言語ガイド

[English](guide.md) | **日本語**

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
- [10. タスクと並列実行](#10-タスクと並列実行)
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
| 演算子 | `!#$%&*+./<=>?@\^\|-~:` の並び |

整数リテラルは `Int` ですが、`Float` が期待される場所では `Float` になります（`area r = 3 * r * r` で `r : Float` なら `3` は `3.0`、`[1.5, 2]` は `[1.5, 2.0]`）。型が文脈から決まらなければ `Int` です（`half x = x / 2` は `Int -> Int`）。

負の数リテラル: `-` の直後に数字があり、`-` が式の直後に無い（行頭・開き括弧・演算子・`,` などの後にある）ならリテラルになります（`[-1, 2]`、`x = -1`）。式の直後の `-` は減算です（`x-1`、`x - 1`）。式の直後に空白を空けて `-1` と書くと、減算か負の数の引数かが紛らわしいのでエラーになります（`x - 1` か `f (-1)` と書きます）。

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

`ark fmt` はトークン列を変えずに空白を整えます（改行の位置・コメント・リテラルは、下の `where` の規則を除いてそのまま）。

- ブロックは開始記号のある行（`with` の場合は対応する `match` / `handle` の行）から 2 桁下げます。ガード `| 条件 =` の本体は `|` から 4 桁下げます。
- 継続行は、同じ要素の中で同じ桁にあった継続行と揃え、前の行の同種のトークン（`|` や引数）に揃えてあった場合はその揃えを保ち、それ以外は要素の行から 2 桁下げます。
- `=`, `->`, `=>`, `|`, `:` と二項演算子の前後、`,` の後に空白を 1 つ入れます。一行で閉じる `(…)` / `[…]` の内側の空白は取り除き、`{…}` の内側には空白を入れます。
- `where` と同じ行に書いた束縛を次の行へ移します（改行を変える唯一の規則です）。
- 行末の空白を削除し、連続する空行を 1 行にまとめ、ファイルを改行 1 つで終えます。行末コメントの前の空白（揃え）は保ちます。

整形結果は再度字句解析され、トークン・レイアウト・コメントが（`where` の束縛を移した後の）元と一致することを確認してから書き込まれます。

## 3. モジュール

1 ファイル = 1 モジュール。モジュール名はパスと一致します（`src/Data/Queue.ark` → `Data.Queue`）。
エントリファイルでは `module` 宣言を省略できます（`Main` になります）。

```haskell
module Data.Queue

import Std.Map               -- 修飾名 Map.x（と Std.Map.x）のみ
import Std.Map as M          -- 修飾名 M.x のみ
import Std.Map (insert, Map) -- 修飾名に加えて、指定した名前を非修飾で
import Std.Map (Map(..))     -- 型とその構築子
import Std.State (..)        -- 全ての名前を非修飾で
```

修飾名はパスの最後の要素（`Std.Map` なら `Map`）なので、`as` は別の名前を付けたいときだけ書きます。
非修飾の名前は、ファイル先頭の import に `(..)` か名前の一覧として必ず現れます。
`Std.State`・`Std.Task`・`Std.Http` などエフェクトや DSL のモジュールは `(..)` で、`Std.String`・`Std.Map` などデータ型のモジュールは修飾名で使う前提で設計されています。

別々のモジュールが同じ名前を非修飾で持ち込むと（`Std.State (..)` と `Std.Http (..)` の `get` など）、その名前を非修飾で**使ったときだけ**曖昧さのエラーになります。
モジュール自身のトップレベル定義は import した名前より優先されます。
import したモジュールにある名前を修飾せずに使うと、エラーに `did you mean String.splitOnce?` のような候補が出ます。

`map` や `length` のような型クラスのメソッドはどの型でも同じ名前を使い、モジュールごとの同名の関数はありません（「8. 型クラス」）。ただし `Map.toList` はキーと値の組のリストを返し、`Foldable` の `toList`（値のリスト）とは別の関数です。

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
      where                     -- 入れ子にできる
        next k = match k % 2 with
          0 -> k / 2
          _ -> 3 * k + 1
```

- `where` は等式の頭より深く字下げします。本体がブロックのときは、本体と同じ桁に書いてブロックを終えることもできます（下の例）。束縛は `where` の次の行から字下げして書きます。同じ行に書いた束縛（`where y = x * 2`）は `ark fmt` が次の行へ移します。
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
- 関数・値・パターン束縛（`where (q, r) = (a / b, a % b)`）を書けます。

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

`deriving` できるのは `Eq`, `Ord`, `Show` と、`Std.Json` の `ToJson`, `FromJson` です。クラス名は制約に書くときと同じように解決されるので、`import Std.Json` なら `deriving (Json.ToJson)`、`import Std.Json (ToJson)` なら `deriving (ToJson)` と書きます。

`ToJson` / `FromJson` の導出は次の形の JSON と相互に変換します。

| 構築子 | JSON |
|---|---|
| レコード | `{"name": "ann", "age": 30}`（`Option` 型のフィールドは省略・`null` 可） |
| それ以外（構築子が一つの型） | 引数が一つならその値、複数なら配列、無ければ構築子名の文字列 |
| 構築子が複数の型 | 引数の無い構築子は `"Admin"`、レコードは `{"tag": "Circle", "radius": 1}`、それ以外は `{"tag": "Polygon", "values": [...]}` |

```haskell
import Std.Json

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
- フィールドの参照と更新は **レコードの型** で解決されます。型がまだ分からない場合（`\u -> u.name` など）は、囲んでいる束縛の型が決まるまで解決を待ち、それでも分からなければ型注釈を求めるエラーになります（フィールド名から型を推測することはありません）。
- 参照・更新できるのは、型の **全ての構築子** が持つフィールドです（`Shape` の `x`, `y`）。
- 構築・更新の値はソースに書いた順に評価されます。
- レコード構築子は名前付きでだけ構築・マッチできます（`User "ann" 30 []` や `User n a _` はエラー）。
- `deriving Show` はレコード構文で表示します（`User { name = "ann", age = 30, tags = [] }`）。

### インデックス型

型のパラメータに値のような情報（長さ・状態・式の型など）を持たせ、マッチでそれを取り出せます。

```haskell
type Nat = Z | S Nat

type Vec (n : Nat) a where
  VNil : Vec Z a
  VCons : a -> Vec n a -> Vec (S n) a
  deriving (Eq, Show)

type family Add (n : Nat) (m : Nat) : Nat where
  Add Z m = m
  Add (S n) m = S (Add n m)

append : Vec n a -> Vec m a -> Vec (Add n m) a
append VNil ys = ys
append (VCons x xs) ys = VCons x (append xs ys)

vzip : Vec n a -> Vec n b -> Vec n (a, b)
vzip VNil VNil = VNil
vzip (VCons x xs) (VCons y ys) = VCons (x, y) (vzip xs ys)
```

- **データカインド**: パラメータを持たず、フィールドもそのような型だけの型（`Nat`, `Bool` など）はカインドとして使え、その構築子は型として使えます（`Z : Nat`, `S : Nat -> Nat`）。同じ名前の型があればそちらが優先されます。
- **カインド注釈**: 型パラメータには `(n : Nat)` のようにカインドを書けます。カインドは `Type`・`Row`・`Effect`・データ型と、その間の矢印（`Type -> Type`）です。書かなければ推論されます。
- **GADT**: `type T a where` の後に構築子のシグネチャ `C : T1 -> .. -> T ..` を並べます。結果の型は宣言する型をパラメータの数だけ適用したもので、型変数は構築子ごとに暗黙に量化されます。`deriving` はブロックの最後に書けます（`FromJson` と、存在型の型変数を持つ構築子がある型のクラスは導出できません）。構築子には型クラス制約もフィールド名も付けられません。
- **精密化**: GADT の構築子にマッチすると、その腕の中では構築子が意味する型の等式（`eval : Expr a -> a` の `IntE` の腕なら `a = Int`）が仮定されます。結果の型が決めない構築子の型変数は存在型になり、腕の外へ出せません。精密化するマッチでは、マッチする値の型が分かっている必要があります（シグネチャを付けてください）。値の型のうちまだ分かっていない部分については、腕の中でも何も仮定されません。腕ごとに型が異なるときは、マッチの結果の型もシグネチャなどで決まっている必要があります。`let` のパターンでは精密化する構築子にマッチできません。
- **網羅性**: 値の型から起こりえない構築子は場合分けが不要です（`vhead : Vec (S n) a -> a` に `VNil` の場合は要らず、`vzip` に `(VNil, VCons ..)` の場合は要りません）。逆に、起こりえない構築子のパターンはエラーです。
- **型族**: `type family F (a : K) .. : K where` の後に等式 `F p1 .. pn = t` を並べます。パターンは型構築子と互いに異なる型変数からなり、等式は上から順に試されます。前の等式が当てはまる可能性が残っている間は、後の等式は使われません。停止しない簡約を防ぐため、右辺で自分自身（または自分に戻ってくる型族）を適用するときは、引数に型族を含めず、引数全体をパターンより小さくし、どの型変数もパターンより多く使ってはいけません（`Add (S n) m = S (Add n m)` は良く、`Mul (S n) m = Add m (Mul n m)` のように別の型族の引数にするのも良い）。型族は常にすべての引数に適用して使い、インスタンスの頭部にはできません。結果のカインドを `Row` や `Effect` にはできません。簡約できない適用どうしは、引数が等しいときにだけ等しいとみなされます。

## 5. 式

| 構文 | 例 |
|---|---|
| 関数適用（左結合・カリー化） | `f x y` |
| 演算子 | `a + b * c` |
| 演算子を関数として | `(+)`, `foldl (+) 0 xs` |
| セクション | `(+ 1)`, `(10 -)` |
| ラムダ | `\x y -> x + y`, `\(a, b) -> a` |
| let | `let x = 1` (ブロック内の文), `let x = 1 in x + 1` |
| ローカル関数 | `let go acc n = ...`（再帰可） |
| ローカル値 | `let x = x + 1`（値の束縛は非再帰。右辺の `x` は外側の `x`） |
| パターン束縛 | `let (x, y) = pair` |
| match | `match e with` + アームのブロック |
| handle | `handle e with` + 節のブロック（9 章） |
| タプル / ユニット | `(1, "a")`, `()` |
| リスト / 範囲 | `[1, 2, 3]`, `x :: xs`, `[1 .. 10]` |
| レコード | `User { name = "a", age = 1 }`, `u.name`, `.name`, `{ u \| age = 2 }`（4 章） |
| 型注釈 | `(e : List Int)` |
| パイプ | `xs \|> map f`（`map f xs` と同じ） |
| 連結 | `"a" ++ "b"`, `[1] ++ [2]`（`Semigroup`） |
| ブロックの引数 | `forEach xs \x ->` の後に字下げした本体 |

整数の `/` は 0 の方向へ切り捨て、`%` はその剰余で、`(a / b) * b + a % b == a` が成り立ちます（`-7 / 2 == -3`、`-7 % 2 == -1`）。除数と同じ符号の剰余は `mod` です（`mod (-7) 2 == 1`）。

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
型のカインド（`Type`, `Type -> Type`, データカインドの `Nat` など）も推論されます（[インデックス型](#インデックス型)）。

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
- 制約の型変数がどの型にも現れず決まらない場合（`show []` の要素の型など）はエラーになります。型注釈で決めてください（`show ([] : List Int)`）。
- 実装は辞書渡しで、静的に決まるインスタンスのメソッド呼び出しは直接呼び出し（`Int` の算術は Go の演算）に最適化されます。

### Functor / Applicative / Monad / Foldable とエフェクト

`Functor`・`Applicative`・`Monad`・`Foldable`・`Traversable` のメソッドは受け取った関数を**その場で**呼ぶので、エフェクトを起こす関数も渡せます（`map (\x -> println x; x) xs`）。
`List`・`Option`・`Result`・`Map`・`Array`・`Queue`・`Set`（`Foldable` のみ）はどれも同じ `map` / `andThen` / `foldl` / `length` / `isEmpty` / `toList` を使います。
`Foldable` の `toList`・`length`・`isEmpty` は畳み込みによる既定の実装を持ち、インスタンスはより速い実装で上書きできます（`Array` の `length` は定数時間）。

受け取った関数を保存して「後で」呼ぶ型（関数を包んだパーサや状態変換子など）は、その時点で呼び出し元のハンドラが無くなっているかもしれないので、これらのインスタンスにはできません（型検査で弾かれます）。
そうした計算はエフェクトで表します。たとえば `Std.Parser` は `ParseInput` / `Fail` エフェクトによるパーサです。

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

エフェクトは *エビデンス渡し* で実装されています。生成されるすべての関数は、呼び出し元の *エビデンス*（その時点で有効なハンドラのフレームを内側から並べた不変のリスト）を最初の引数として受け取ります。
`handle` は本体を新しいフレームを積んだエビデンスで呼び、操作は受け取ったエビデンスから最も内側の（mask されていない）ハンドラを探します。節はハンドラの外側のエビデンスで実行されます（ディープハンドラ）。
大域的なハンドラスタックが無いので、計算が巻き戻されても何も復元する必要がなく、独立した計算（タスク）をいくつでも並列に実行できます。
エビデンスのフレームは実行中のタスクとキャンセルのスコープも指していて、生成コードはそれらに定数時間でアクセスします。

ハンドラは形に応じて最も安価な方法で実行されます。

| ハンドラの形 | 実行方法 |
|---|---|
| 全ての節が末尾で一度だけ `k` を呼ぶ（Reader、ロガー等） | 呼び出し元でそのまま節を実行 |
| 節が末尾再開か、`k` を使わない（例外） | 呼び出し元で実行し、中断は panic/recover |
| 状態渡し `return x -> \s -> ...` / `op k -> \s -> k v s'` を初期状態に即適用（State、Writer、Random） | 状態をハンドラのフレームに保持し、呼び出し元で実行 |
| それ以外（継続の保存、非末尾の再開） | 本体を専用のゴルーチンで実行し、操作はメッセージ送信（一つのタスクの中で同時に動くゴルーチンは常に一つ） |

## 10. タスクと並列実行

`Std.Task` のタスクは、すべてのプロセッサで並列に動く計算です。

```haskell
import Std.Task (..)

main () = runAsync (\_ ->
  let a = spawn (\_ -> fib 32)        -- タスクを開始して Task Int を得る
  let b = spawn (\_ -> fib 33)
  println "${await a + await b}")     -- 結果を待つ
```

| 関数 | 説明 |
|---|---|
| `runAsync : (Unit -> <Async, IO \| e> a) -> <IO \| e> a` | タスクのスコープを開き、本体と、その中で開始したすべてのタスクが終わるまで待つ |
| `spawn : (Unit -> <Async, IO> a) -> <Async> Task a` / `fork` | 最も内側のスコープでタスクを開始する（`fork` は結果を使わないとき） |
| `await` / `awaitOutcome` | タスクの結果を待つ（`awaitOutcome` は `Completed x` か `Cancelled`） |
| `cancel` / `timeout` / `cancelOnInterrupt` | タスクのキャンセル、制限時間付きの実行（`Option` を返す）、Ctrl-C / SIGTERM によるキャンセル |
| `finally` / `uncancellable` | どう終わっても実行される後始末、キャンセルされない区間 |
| `parallel` / `parMap` / `both` / `race` | 並列に実行して結果を集める、最初に終わったものを採る |
| `catchError` | 実行時エラーを `Err` に変える（失敗してもスコープを止めないタスクに） |

**構造化。** タスクは必ずスコープ（`runAsync`）の中で開始され、スコープより長く生き残ることはありません。`Async` エフェクトは「タスクを開始できる」ことを表し、`runAsync` が処理します。開始先のスコープはエビデンスから見つけます。

**エフェクト。** タスクが使えるエフェクトは `Async` と `IO` だけです。スコープの外側で処理されるエフェクト（`State` など）はタスクの中では使えないので、必要ならタスクの中で処理します。こうしてハンドラが複数のスレッドから同時に呼ばれることはありません。`runAsync` の本体と `timeout` などの中身は呼び出したタスクで動くので、外側のエフェクトをそのまま使えます。

**失敗。** タスクが実行時エラーで失敗すると、同じスコープの他のタスクと本体がすぐにキャンセルされ、すべてが終わってから `runAsync` がそのエラーを送出します。`await` も失敗したタスクのエラーを送出し直します。

**キャンセル。** キャンセルは非同期に要求され、計算は次の *キャンセルポイント* で巻き戻されます。コンパイラは、ループする関数、再帰する関数（大域的な参照グラフの強連結成分）、関数値・エフェクト操作・高階の foreign 関数を呼ぶ関数の入口にキャンセルポイント（`rt.Poll`）を挿入します。呼び出しの循環は必ずどれかを通るので、純粋な無限ループもキャンセルできます。`await`・`sleep`・チャネル・ロック・ネットワークの待ちもキャンセルポイントです。巻き戻しは `finally` の後始末を実行し、タスクの中で最も外側のキャンセルされたスコープで止まります（`timeout` なら `None`、タスクなら `Cancelled`）。

**プリエンプション。** タスクは Go のゴルーチンで動き、Go のスケジューラが全プロセッサに分散し、長く走るゴルーチンを非同期にプリエンプトします。純粋な計算を続けるタスクがあっても他のタスクは止まりません（`GOMAXPROCS=1` でも）。I/O 待ちのタスクは OS スレッドを占有しません。

**データの共有。** タスク間でのデータの共有には `Std.Channel`（容量付きのキュー、`closeChannel` で終わりを伝える）、`Std.Atomic`（純粋な関数で原子的に更新する参照）、`Std.Mutex`（`withLock` と、更新がロックを保持する `Shared` 値）を使います。`Ref`・`MArray`・`HashMap` は一つのタスクの中で使うもので、二つのタスクから同時に使ってはいけません（データ競合）。

**HTTP。** `Std.Http` の `serve` は各リクエストをそれぞれのタスクで処理するので、リクエストは並列に処理されます。ハンドラは `Async` を使ってリクエストの一部としてタスクを開始でき、クライアントが切断したリクエストはキャンセルされます。

## 11. テスト

テストはトップレベルの `test "名前" = 本体` 宣言で書きます。テストはテスト対象のすぐ近く、同じモジュールの中に置くのが基本です。
テストは関数の使い方を示す実行可能なドキュメントでもあり、コードと並べておくと一緒に読まれ、一緒に更新されます。

```haskell
module Stack

type Stack a = Stack (List a)

empty : Stack a
empty = Stack []

push : a -> Stack a -> Stack a
push x (Stack xs) = Stack (x :: xs)

pop : Stack a -> Option (a, Stack a)
pop (Stack []) = None
pop (Stack (x :: xs)) = Some (x, Stack xs)

test "pop returns the last pushed value" =
  match pop (push 2 (push 1 empty)) with
    Some (x, _) -> assertEq x 2
    None -> fail "expected a value"

test "pop on an empty stack is None" =
  assert (isNone (pop empty)) "expected None"
```

- テスト本体は `Unit` 型で、`<Fail, IO>` のエフェクトを使えます。`fail` するか実行時エラーになるとテストは失敗し、他のテストは続けて実行されます。
- Prelude の `assert : Bool -> String -> <Fail> Unit` は条件が偽なら与えたメッセージで失敗し、`assertEq actual expected` は二つの値が等しくなければ両方を `show` して失敗します。`orFail msg opt` は `Option` から値を取り出し、`None` なら失敗します。
- 同じモジュールのテストは `private` な宣言も使えます。
- テストは `ark test` のときだけ実行され、`ark build` / `ark run` のプログラムからは呼ばれません。

### 置き場所

| 置き場所 | 用途 |
|---|---|
| `src/` の各モジュールの中（推奨） | 単体テスト。テスト対象と同じモジュールに書く |
| `tests/` のモジュール | 複数のモジュールにまたがる結合テスト。テスト対象を `import` し、公開 API だけを使う |

`ark new` の雛形も、`src/Greeting.ark` の `greet` のすぐ下にそのテストを置いています。
`tests/` は必要になったら作ってください（`tests/` のモジュールは `src/` のモジュールを `import` でき、ビルドには含まれません）。

### 実行

```sh
ark test                 # src/ と tests/ の全モジュールのテストを実行
ark test -run Stack      # 「モジュール名 › テスト名」がこの文字列を含むテストだけを実行
ark test src/Stack.ark   # 一つのファイル（とそれが import するモジュール）のテストを実行
```

```
✓ Stack › pop returns the last pushed value
✓ Stack › pop on an empty stack is None

all 2 tests passed (0s)
```

失敗したテストがあると、失敗の理由を表示して終了コード 1 で終わります。

## 12. FFI

```haskell
foreign sqrt : Float -> Float = "rt.FloatSqrt"
```

`foreign` は Go の関数を Arkadia の関数として束縛します。Go 側の関数は引数の数だけ `rt.Value`（= `any`）を受け取り、`rt.Value` を返します。
パッケージの `native/*.go`（`package main`、`import rt "arkprog/rt"`）に関数を置くと、`foreign` から参照できます。
`private foreign` とすると公開せずに束縛でき、型クラスのメソッドを Go で実装するときなどに使えます。

高階の foreign 関数（関数を引数に取るもの）と、ターゲットを `"ev:pkg.Func"` と書いた foreign 関数は、最初の引数に呼び出し元のエビデンス `ev *rt.Ev` も受け取ります。
Arkadia の関数値はこれを渡して `rt.Apply1(ev, f, x)` のように呼び出します。
長く待つ関数は `ev.Done()`（計算がキャンセルされると閉じるチャネル）も待ち、閉じたら `rt.Poll(ev)` を呼んで計算を巻き戻してください。

```go
// foreign receiveFrom : Source -> <IO> String = "ev:ReceiveFrom"
func ReceiveFrom(ev *rt.Ev, src rt.Value) rt.Value {
	select {
	case msg := <-src.(*Source).C:
		return msg
	case <-ev.Done():
		rt.Poll(ev)
		return rt.Unit
	}
}
```

| Arkadia | Go の表現 |
|---|---|
| `Int` / `Float` / `String` / `Char` / `Bool` | `int64` / `float64` / `string` / `rune` / `bool` |
| `Unit` | `rt.Unit` |
| データ構築子 | `*rt.D0` 〜 `*rt.D3`, `*rt.DN`（`T` がタグ） |
| リスト | `rt.Nil`, `rt.Cons(h, t)`, `rt.ListToSlice`, `rt.SliceToList` |
| `Option` / `Result` | `rt.None`, `rt.Some(x)`, `rt.Ok(x)`, `rt.Err(e)` |
| 関数 | `rt.Apply1(ev, f, x)` などで呼び出す |

## 13. 評価と実行時

- 正格（値呼び）。引数は左から右へ評価されます。
- 自己末尾呼び出し（トップレベル関数・ローカル関数）はループにコンパイルされます。それ以外の再帰も Go の伸長可能なスタックにより深い再帰が可能です。
- 値は一様表現（`any`）ですが、`Int`/`Float` の演算は静的に分かる範囲でボックス化せずに行います。
- メモリは Go の GC（並行 GC）が管理します。
- タスクは Go のゴルーチンで並列に動きます（プロセッサ数は `GOMAXPROCS` で変更できます）。標準出力はタスク間で共有され、`println` の 1 行は混ざりません。出力はバッファされ、入力や I/O を待つ前か、書いてから遅くとも 50ms 後に書き出されます。
- 実行時エラー（`error`、0 除算、パターン不一致、未処理エフェクト）はメッセージと、エラーが起きた時点で実行中だった Arkadia の関数（内側から順、再帰はまとめて回数表示）を表示して終了します。`ARKADIA_TRACE=1` で Go のスタックトレースも表示します。

```
runtime error: division by zero
  in Main.average
  in Main.loop (4 times)
  in Main.main
```
