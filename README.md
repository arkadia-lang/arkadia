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
| レコード | 名前付きフィールド、`u.name` / `.name` / `{ u \| age = 1 }` / `User { name, age }` パターン。フィールドは型主導で解決 |
| 代数的エフェクト | `effect` 宣言・`handle ... with` によるディープハンドラ。エフェクトは行多相の型で追跡され、未処理のエフェクトはコンパイルエラー |
| 並列実行・プリエンプション | タスクは全コアで並列に動き、ランタイムがプリエンプションする。純粋関数の決定的な並列化（`Std.Par`）、構造化された並行性（`Std.Task`）、STM（`Std.Stm`）、チャネル（`Std.Channel`）。エフェクト型が「並列にしても安全」なことを保証する |
| GC | Go ランタイムの並行 GC を利用 |
| ネイティブバイナリ | Arkadia → Go → `go build` で単一の静的バイナリを生成 |
| 現代的なエコシステム | `ark` コマンド（new / build / run / check / test / fmt / doc / repl）、Language Server（VS Code 拡張付き）、`ark.toml` による依存管理（path / git）とロックファイル、テストランナー、Go への FFI |
| 再帰とパターンマッチだけの制御 | `if` もループも無い。`match`・ガード・再帰（自己末尾呼び出しはループに最適化）だけ |
| 空白区切りの関数適用 | `f x y` |
| 数値リテラル | 整数リテラルは `Float` の文脈では `Float` になる（`3 * r` で `r : Float`）。それ以外は `Int` |
| 全関数がカリー化 | `add 1` は部分適用。既知アリティの完全適用は直接呼び出しに最適化 |
| インデントによるブロック | `=`, `->`, `where`, `with` の後の字下げがブロックになる |
| ガード節 | 関数定義と `match` の両方で `\| 条件 = 式` / `\| 条件 -> 式` |
| where 節 | 等式の本体とガードにまたがる局所束縛。ローカル関数は相互再帰も可 |
| セルフホスト | `ark` コマンドは Arkadia で書かれたコンパイラ（[`compiler/`](compiler)）で、自分自身をビルドできる（固定点に達する） |

## インストール

Go 1.22 以降と [just](https://github.com/casey/just) が必要です（Go はブートストラップと、生成コードのビルドの両方に使います）。

```sh
git clone <this repository> arkadia && cd arkadia
just build      # ブートストラップ: bin/ark0 (Go) → bin/ark1 → bin/ark
just install    # $GOBIN/ark から bin/ark へのシンボリックリンクを張る
```

`ark` は標準ライブラリ（`lib/`）と Go ランタイム（`runtime/rt`）を、実行ファイル（シンボリックリンクの先）の親ディレクトリ、
または `$ARKADIA_ROOT` から探します。リポジトリの外に置く場合は `ARKADIA_ROOT` にリポジトリのパスを設定してください。

開発に必要なツール（Go・git・just・Node.js）は Nix flake で管理しています。`nix develop` で開発環境に入り、タスクは `just` で実行します（`just build` / `just check` / `just test` / `just fmt` / `just fixpoint`、一覧は `just`）。

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

### レコード

```haskell
type User = User { name : String, age : Int }
  deriving (Show, Eq)

birthday : User -> User
birthday u = { u | age = u.age + 1 }        -- 更新（新しい値を返す）

main () =
  let ann = User { name = "ann", age = 30 }
  println "${ann.name} is ${ann.age}"       -- フィールド参照
  println (show (map .name [ann]))          -- アクセサ関数 .name
  match birthday ann with
    User { age } | age > 30 -> println "older"
    _ -> println "same"
```

フィールドの参照はレコードの型で解決され、型が決まらない場合はフィールド名から一意に定まる型が選ばれます。

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

### 並行・並列実行

タスクは全てのコアで並列に動き、ランタイムがプリエンプションします（長い計算をするタスクが他を止めることはありません）。
エフェクト型によって、並列化して安全なものだけが並列化されます。

```haskell
import Std.Par as Par
import Std.Task as Task
import Std.Stm

-- 純粋な関数を全コアで。結果は map と常に同じ（型 a -> <> b が純粋性を保証）
scores = Par.map expensive inputs

-- 構造化された並行性: scoped は全タスクの終了を待ち、一つが失敗すると残りをキャンセルしてエラーを伝える
fetchBoth () = Task.scoped (\s ->
  let a = Task.fork s (\_ -> httpGet urlA)
  let b = Task.fork s (\_ -> httpGet urlB)
  (Task.await a, Task.await b))

-- STM: 残高が足りるまで待つ（retry）原子的な送金。トランザクションは <STM> しか持てない
transfer from to n = atomically (\_ ->
  let balance = readTVar from
  check (balance >= n)
  writeTVar from (balance - n)
  modifyTVar to (+ n))

-- 時間制限: ループもエフェクトもない純粋な再帰でも止まる
Task.timeout 1.0 (\_ -> search problem)
```

| | 型が保証すること |
|---|---|
| `Std.Par`（`map`, `filter`, `reduce`, `sortBy` …） | 関数は純粋（`<>`）なので、並列でもデータ競合は起こらず、結果は逐次版と同じ。エラーも逐次版と同じ（最初に失敗する要素のもの） |
| `Std.Task`（`scoped`, `fork`, `await`, `race`, `timeout` …） | タスクは `Unit -> <IO> a`。親のハンドラ（`State` など）を共有しないので、エフェクトの状態が競合しない |
| `Std.Stm`（`atomically`, `TVar`, `retry`, `orElse`） | トランザクションは `<STM>` 以外のエフェクトを持てないので、衝突時の再実行や `retry` での待機が安全 |
| `Std.Channel`（`send`, `receive`, `select`） | タスク間でメッセージを受け渡す |

- キャンセルは協調的です。関数呼び出し・ループの反復・エフェクト操作・待機（`await`, `sleep`, チャネル, `retry`）がキャンセルポイントで、キャンセルは捕捉できません。
- `Std.Ref` の読み書きは不可分で、`atomicModifyRef` / `atomicUpdateRef` で更新を失わずに共有できます。`Std.MArray` / `Std.HashMap` の各操作もタスク間で安全です。
- `Std.Http.serveParallel` は各リクエストを独立したタスクで並列に処理します。

詳しくは [言語ガイド](docs/guide.md#10-並行並列実行) を参照してください。

## ツールチェーン

| コマンド | 説明 |
|---|---|
| `ark new <name>` | プロジェクト雛形を作成 |
| `ark build [file]` | ネイティブバイナリを生成（`-o` で出力先） |
| `ark run [file] [-- args]` | ビルドして実行 |
| `ark check [file]` | 型検査のみ |
| `ark test [file]` | `test "名前" = ...` 宣言を実行（`Fail` エフェクトで失敗を報告）。`-run 文字列` で名前による絞り込み |
| `ark fmt [path...]` | `.ark` ファイルを整形して上書き（既定はカレントディレクトリ以下）。`-check` は未整形のファイルを列挙して終了コード 1、`ark fmt -` は標準入力を整形して標準出力へ |
| `ark doc <Module>` | モジュールの公開 API を、宣言直前の `--` コメント（ドキュメント）付きで表示 |
| `ark repl` | 対話環境（式の評価、定義の追加、`:t` で型表示） |
| `ark emit [file]` | 生成された Go コードを表示 |
| `ark lsp` | Language Server（診断・ホバーで型とドキュメントを表示・定義ジャンプ・フォーマット）。VS Code 拡張は [`editors/vscode`](editors/vscode) |

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

## セルフホスト

`ark` コマンドは Arkadia で書かれたコンパイラ（[`compiler/`](compiler)）です。
字句解析から型推論（型クラス・エフェクト行）、Go コード生成、REPL、Language Server まですべて Arkadia で実装されています。

最初の一段だけは Go で書かれたブートストラップ用コンパイラ（[`bootstrap/`](bootstrap)）でビルドします。

```
bootstrap/ ──go build──▶ bin/ark0 ──build──▶ bin/ark1 ──build──▶ bin/ark ──build──▶ (bin/ark と同一)
```

```sh
just build        # bin/ark0 → bin/ark1 → bin/ark
just fixpoint     # bin/ark で自分自身をビルドし、生成コードとバイナリが一致する（固定点）ことを確認
```

言語の機能追加や修正は `compiler/` に入れます。ブートストラップ用コンパイラは凍結されていて、
`compiler/` のソース（とそれが使う標準ライブラリ）をビルドできる限り変更しません。
詳しくは [compiler/README.md](compiler/README.md) と [bootstrap/README.md](bootstrap/README.md) を参照してください。

## 標準ライブラリ

| モジュール | 内容 |
|---|---|
| `Prelude` | 基本型（`Option`, `Result`, `List` …）、型クラス（`Eq`, `Ord`, `Show`, `Num`, `Semigroup`, `Monoid`, `Functor`, `Applicative`, `Monad`, `Foldable`, `Traversable`）、リスト・文字列関数、入出力、`Fail` エフェクト |
| `Std.State` / `Std.Reader` / `Std.Writer` | 状態・環境・出力のエフェクト |
| `Std.Exn` | 型付き例外 `Exn e` |
| `Std.Task` | 並列に動くタスク。スコープによる構造化された並行性（`scoped` / `fork` / `await`）、キャンセル、`race`, `timeout`, `both`, `mapConcurrently`, `mapPooled` |
| `Std.Par` | 純粋関数の決定的な並列化（`map`, `filter`, `reduce`, `sortBy`, `foldMap`, `generate` …） |
| `Std.Stm` | ソフトウェアトランザクショナルメモリ（`TVar`, `atomically`, `retry`, `orElse`, `check`） |
| `Std.Channel` | タスク間のチャネル（バッファ付き / なし、`close`、タイムアウト付きの `select`） |
| `Std.Gen` | `Yield` エフェクトによるジェネレータ（無限列の `takeGen`、`mapGen` / `filterGen` などの合成） |
| `Std.Parser` | バックトラックするパーサコンビネータ（`alt`, `many`, `sepBy`, `chainl1` …、行・列付きのエラー） |
| `Std.Random` | 乱数エフェクト（決定的シード / 時刻シード） |
| `Std.List` | Prelude にないリスト関数（`span`, `groupBy`, `chunksOf`, `scanl`, `transpose`, `permutations` …）。非修飾 import 可 |
| `Std.Option` / `Std.Result` | `Option` / `Result` の補助関数（`orElse`, `map2`, `fold`, `combineAll` …） |
| `Std.Ord` | `Down`（逆順）、`comparing`, `thenComparing` |
| `Std.Map` / `Std.Set` | 永続的な順序付きマップ・集合（AVL 木）。`lookupLT` などの順序クエリ、集合演算 |
| `Std.Array` | 定数時間でインデックスできる不変配列（`binarySearch`, `mapWithIndex` …） |
| `Std.Queue` / `Std.Heap` | 永続的な両端キュー、優先度付きキュー（leftist heap） |
| `Std.String` / `Std.Char` | 文字列（`splitOnce`, `stripPrefix`, `wrap` …）、文字の分類と変換 |
| `Std.Math` / `Std.Bits` | 三角・指数・対数関数、`powInt`, `isqrt`, `isPrime` など。ビット演算 |
| `Std.Json` | JSON のパース・出力（整形あり）・パスでの参照。`ToJson` / `FromJson` クラスと `deriving (ToJson, FromJson)` によるレコード等との自動変換（失敗箇所付きのエラー） |
| `Std.IO` / `Std.Path` / `Std.Time` / `Std.Ref` | ファイル・ディレクトリ・プロセス、パス操作、時刻（RFC 3339 などの書式化と解析、日付の構築、曜日、日・月の加算）と計測、可変参照（不可分な更新付き） |
| `Std.Http` | HTTP サーバ（ルーティング・パスパラメータ・ミドルウェア・静的ファイル・Cookie、リクエストの並列処理）と HTTP クライアント |
| `Std.Net` / `Std.Url` | TCP ソケット・名前解決、URL のパース・パーセントエンコード・クエリ文字列 |
| `Std.Regex` | 正規表現（RE2 構文）。一致・キャプチャ・名前付きグループ・置換（関数による置換も可）・分割 |
| `Std.MArray` / `Std.HashMap` | 可変配列（push/pop・その場ソート・freeze/thaw）、挿入順を保つ可変ハッシュマップ（構造的なキー比較） |
| `Std.BigInt` | 多倍長整数（`Num`/`Ord`/`Show` のインスタンス、`pow`, `powMod`, `gcd`, `isqrt`, 基数変換） |
| `Std.Encoding` / `Std.Crypto` | Base64（URL セーフ版を含む）・16 進、MD5/SHA-1/SHA-256/SHA-512・HMAC-SHA256、安全な乱数・UUID |
| `Std.Args` | コマンドライン引数の解析（`--name=value`, `-n value`, `-abc`, `--`） |
| `Std.Csv` | CSV の解析・出力（引用符・改行を含むフィールド、区切り文字の指定、ヘッダ付きレコード） |
| `Std.Format` | 数値の書式化（小数点以下の桁数 `fixed 2 x`、指数表記、基数変換、桁区切り、ゼロ埋め、パーセント、バイト数） |

モジュールの公開インターフェースは `ark doc Std.Map` のように表示できます。

### HTTP サーバ

```haskell
import Std.Http
import Std.Json as Json

hello : Handler e
hello req = text "Hello, ${withDefault "world" (query "name" req)}!"

user : Handler e
user req =
  let id = parseInt (param "id" req) |> orHalt (badRequest "id must be a number")
  json (Json.object [("id", Json.int id)])

main () = serve ":8080" (logRequests (router
  [ get "/" hello
  , get "/users/:id" user
  , staticDir "/assets" "public"
  ]))
```

- ハンドラ `Handler e` は `Request -> <IO, Fail, Halt | e> Response`。`halt resp`（`orHalt` / `orHaltWith`）で任意のレスポンスを返して早期終了でき、`fail` や実行時エラーは 500 になります（サーバは止まりません）。
- ルートのパターンは固定文字列・`:name`（1 セグメント）・末尾の `*name`（残り全部）。`scope "/api" [...]` でまとめて前置でき、パスは合うがメソッドが違う場合は 405 を返します。
- リクエストの受信は Go のゴルーチンで並行に行います。`serve` はハンドラを一度に一つずつ実行するので、`State` などのエフェクトをサーバ全体の周りで処理して、ハンドラ間でロックなしに共有できます（[`examples/http/server.ark`](examples/http/server.ark)）。
- `serveParallel` は各リクエストを独立したタスク（`Std.Task`）で並列に処理します。ハンドラは `Handler` 以外のエフェクトを持たず、リクエスト間の共有状態には `Std.Stm` や `Std.Ref` を使います。
- `request "GET" "/users/1" ""` でリクエストを作り `runHandler` に渡せば、サーバを立てずにハンドラやルータをテストできます。
- `bodyAs req` は JSON の本文を `FromJson` の型に変換し、`jsonOf x` は `ToJson` の値を JSON で返します。
- クライアントは `httpGet` / `httpPost` / `postJson` / `getJson` / `getAs` / `fetch`。
- `Std.State` の `get` / `put` と名前が重なるので、両方使うときは片方を `import Std.State as St` のように修飾付きで import してください。

## サンプル

- [`examples/parallel.ark`](examples/parallel.ark) — `Std.Par` による並列のマンデルブロ集合と素数の map-reduce、プリエンプションとタイムアウト
- [`examples/philosophers.ark`](examples/philosophers.ark) — STM による食事する哲学者（デッドロックしない）
- [`examples/pipeline.ark`](examples/pipeline.ark) — チャネルでつないだタスクのパイプライン（ワーカープール、`select`）
- [`examples/scheduler.ark`](examples/scheduler.ark) — エフェクトで実装した協調スレッド
- [`examples/json.ark`](examples/json.ark) — State と Fail エフェクトによる JSON パーサ
- [`examples/calculator.ark`](examples/calculator.ark) — モナディックなパーサコンビネータ（高カインド）
- [`examples/lisp.ark`](examples/lisp.ark) — クロージャ付きの小さな Lisp インタプリタ
- [`examples/http/server.ark`](examples/http/server.ark) — JSON API と静的ページの TODO アプリ（`cd examples/http && ark run server.ark`）

## 実装の構成

```
compiler           コンパイラ ark（Arkadia で記述。モジュール構成は compiler/README.md）
lib                標準ライブラリ（Arkadia で記述）
runtime/rt         生成コードにリンクされるランタイム（値表現・カリー化・エフェクト・タスク・STM）
bootstrap          ブートストラップ用コンパイラ（Go で記述、凍結）
tests              テストスイート（compiler_test.go が bin/ark 相当のコンパイラで実行する）
editors/vscode     VS Code 拡張（シンタックスハイライト + LSP クライアント）
```

## 制限事項

- 継続はワンショット（同じ継続を二度再開すると実行時エラー）。非決定性のような多重再開は未対応です。
- 型クラスは単一パラメータのみ。
- レコードの更新 `{ r | f = v }` は値の型を変えられません（型パラメータを変える更新は構築し直します）。
- キャンセルは協調的です。キャンセルポイントを通らない処理（ファイルやネットワークの読み書き、FFI の呼び出し）の途中では止まらず、それが終わってから止まります。
- `spawn` で起動した（スコープに属さない）タスクのエラーは、`await` したときにだけ報告されます。
- ハンドラが継続を再開せずに捨てた場合、中断された計算（ジェネレータなど）のゴルーチンは GC のときに回収されます。

## ライセンス

MIT
