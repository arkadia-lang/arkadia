# Arkadia Language Guide

**English** | [日本語](guide.ja.md)

This document summarizes the syntax and semantics of Arkadia.

- [1. Lexical structure](#1-lexical-structure)
- [2. Layout rule](#2-layout-rule)
- [3. Modules](#3-modules)
- [4. Declarations](#4-declarations)
- [5. Expressions](#5-expressions)
- [6. Patterns](#6-patterns)
- [7. Types](#7-types)
- [8. Type classes](#8-type-classes)
- [9. Algebraic effects](#9-algebraic-effects)
- [10. Tasks and parallelism](#10-tasks-and-parallelism)
- [11. Testing](#11-testing)
- [12. FFI](#12-ffi)
- [13. Evaluation and runtime](#13-evaluation-and-runtime)

---

## 1. Lexical structure

```haskell
-- line comment
{- block comment {- may nest -} -}
```

| Kind | Examples |
|---|---|
| Identifier (value) | `x`, `fooBar`, `x'`, `go_1` |
| Identifier (type, constructor, module) | `Option`, `Some`, `Std.Map` |
| Qualified name | `Map.insert`, `Std.Map.Map` |
| Integer | `42`, `-7`, `1_000_000`, `0xff`, `0b1010` |
| Float | `3.14`, `1e-9`, `-0.5` |
| Character | `'a'`, `'\n'`, `'\u{3042}'` |
| String | `"hello\n"` |
| Interpolated string | `"x = ${x}, sum = ${a + b}"` (converted to text with the `Display` class) |
| Raw multi-line string | `"""..."""` (a leading newline is removed; no escapes) |
| Operator | a sequence of `!#$%&*+./<=>?@\^\|-~:` |

Integer literals are `Int`, but become `Float` where a `Float` is expected (in `area r = 3 * r * r` with `r : Float`, `3` is `3.0`; `[1.5, 2]` is `[1.5, 2.0]`). If the context does not determine the type, it is `Int` (`half x = x / 2` is `Int -> Int`).

Negative number literals: a `-` immediately followed by a digit is a literal when the `-` does not directly follow an expression (it is at the start of a line, or after an opening bracket, an operator, `,`, etc.): `[-1, 2]`, `x = -1`. A `-` directly after an expression is subtraction (`x-1`, `x - 1`). Writing `-1` after an expression with a space before it is an error, because it is ambiguous between subtraction and a negative argument (write `x - 1` or `f (-1)`).

Reserved words: `module import as type alias class instance effect where let in match with handle return foreign infixl infixr infix private test deriving` (`as`, `alias`, `test` and `deriving` are reserved only in certain contexts)

Tab characters cannot be used for indentation (spaces only).

## 2. Layout rule

Blocks are expressed by indentation.

- When a line ends with a **block opener** `=`, `->`, `where` or `with` and the next line is indented further, a new block starts.
- Within a block, a line starting at the same column is a new statement (element).
- A more deeply indented line after a line that does not end with an opener is a **continuation line**.
- A line indented less than the block closes the block.
- Newlines are ignored inside brackets `( [ {` (but indented blocks after an opener can still be used).
- `;` puts several statements on one line: `\_ -> tick (); tick ()`

```haskell
main () =                  -- indented after `=` → block
  let xs = [1, 2, 3]
  println (show xs)        -- same column → next statement
  let total = sum xs
    + 100                  -- previous line has no opener → continuation line
  match total with         -- after `with` → block of arms
    0 -> println "zero"
    n | n > 100 -> println "big"
      | otherwise ->       -- continuation of the guard
          println "small"
```

### Formatter

`ark fmt` adjusts whitespace without changing the token sequence (line breaks, comments and literals stay as they are, except for the `where` rule below).

- A block is indented 2 columns from the line with its opener (for `with`, from the line of the corresponding `match` / `handle`). The body of a guard `| condition =` is indented 4 columns from the `|`.
- A continuation line is aligned with continuation lines that were at the same column within the same element; if it was aligned with a token of the same kind on the previous line (a `|` or an argument), that alignment is kept; otherwise it is indented 2 columns from the element's line.
- One space is put around `=`, `->`, `=>`, `|`, `:` and binary operators, and after `,`. Spaces inside `(…)` / `[…]` closed on the same line are removed, and spaces are put inside `{…}`.
- Bindings written on the same line as `where` are moved to the next line (the only rule that changes line breaks).
- Trailing whitespace is removed, consecutive blank lines are collapsed into one, and the file ends with a single newline. Whitespace before an end-of-line comment (alignment) is kept.

The formatted result is lexed again and is written only after checking that its tokens, layout and comments match the original (after moving the `where` bindings).

## 3. Modules

One file = one module. The module name matches the path (`src/Data/Queue.ark` → `Data.Queue`).
The entry file may omit the `module` declaration (it becomes `Main`).

```haskell
module Data.Queue

import Std.Map               -- only the qualified names Map.x (and Std.Map.x)
import Std.Map as M          -- only the qualified names M.x
import Std.Map (insert, Map) -- qualified names, plus the listed names unqualified
import Std.Map (Map(..))     -- a type and its constructors
import Std.State (..)        -- every name unqualified
```

The qualifier is the last component of the path (`Map` for `Std.Map`), so `as` is written only when you want a different name.
An unqualified name always appears in an import at the top of the file, either as `(..)` or in a list of names.
Effect and DSL modules such as `Std.State`, `Std.Task` and `Std.Http` are designed to be used with `(..)`, and data type modules such as `Std.String` and `Std.Map` to be used with qualified names.

When different modules bring the same unqualified name (such as `get` from `Std.State (..)` and `Std.Http (..)`), it is an ambiguity error **only when the name is used** unqualified.
A module's own top-level definitions take precedence over imported names.
Using a name from an imported module without qualifying it gives an error with a suggestion such as `did you mean String.splitOnce?`.

Type class methods such as `map` and `length` use the same name for every type; there are no per-module functions with the same name ("8. Type classes"). However, `Map.toList` returns a list of key–value pairs and is a different function from `Foldable`'s `toList` (a list of values).

`--` comments directly preceding a declaration (or its type signature) are treated as documentation and shown by `ark doc` and editor hovers. A comment at the top of the file becomes the module's description.

`Prelude` is imported implicitly. Top-level definitions are public by default; `private` makes them private.
Instances are shared across the whole program.

## 4. Declarations

### Functions and values

```haskell
-- type signature (optional; inferred when omitted)
gcd : Int -> Int -> Int
-- multiple equations (matched top to bottom)
gcd a 0 = a
gcd a b = gcd b (a % b)

-- guards
sign : Int -> Int
sign n
  | n > 0 = 1
  | n < 0 = -1
  | otherwise = 0

-- operator definition and fixity
infixl 6 <+>
(<+>) : Vec -> Vec -> Vec
Vec a b <+> Vec c d = Vec (a + c) (b + d)

-- values (top-level values must be pure)
primes = [2, 3, 5, 7]
```

If all guards fail, matching falls through to the next equation. If an argument might match no equation, it is a compile error (exhaustiveness checking).

### where clauses

Local bindings can be written with `where` after an equation. The bindings are visible from the equation's body and all its guards, and can refer to the arguments.

```haskell
classify : Int -> String
classify n
  | n < small = "small"
  | n < big = "medium"
  where
    small = 10
    big = 100
classify _ = "large"            -- reached when all guards fail

collatz : Int -> Int
collatz n = go n 0
  where
    go : Int -> Int -> Int      -- a signature can be written on its own line
    go 1 steps = steps
    go m steps = go (next m) (steps + 1)
      where                     -- can be nested
        next k = match k % 2 with
          0 -> k / 2
          _ -> 3 * k + 1
```

- `where` is indented deeper than the head of the equation. When the body is a block, it can also be written at the same column as the body to end the block (example below). Bindings are written indented on the lines after `where`. A binding written on the same line (`where y = x * 2`) is moved to the next line by `ark fmt`.
- `where` bindings cannot see the body's `let`s (the bindings are evaluated before the body).

```haskell
report xs =
  println "total ${total}"
  println "mean ${total / count}"
  where
    total = sum xs
    count = length xs
```

- The bindings follow the same rules as `let` statements: after the equation matches, they are evaluated **in the order written**, before the body and the guards (strict evaluation).
- Functions, values and pattern bindings (`where (q, r) = (a / b, a % b)`) can be written.

### Local mutual recursion

Consecutive `let` function definitions in a block (and `where` function definitions) form one mutually recursive group when they refer to a function defined later.

```haskell
parity n =
  let isEven k = match k with
    0 -> True
    _ -> isOdd (k - 1)          -- refers to isOdd, defined later
  let isOdd k = match k with
    0 -> False
    _ -> isEven (k - 1)
  isEven n
```

The functions of a group are type-inferred and generalized together (polymorphic functions with type class constraints included). Value bindings are not recursive (the `x` on the right of `let x = x + 1` is the outer `x`).

### Data types

```haskell
type Shape
  = Circle Float
  | Rect Float Float
  deriving (Show, Eq, Ord)

type Tree a = Leaf | Node (Tree a) a (Tree a)

-- a type without constructors is an opaque type (for FFI)
type Handle

-- type alias
alias Point = (Float, Float)
```

The classes that can be derived are `Eq`, `Ord`, `Show`, and `ToJson`, `FromJson` from `Std.Json`. Class names are resolved the same way as in constraints, so with `import Std.Json` you write `deriving (Json.ToJson)`, and with `import Std.Json (ToJson)` you write `deriving (ToJson)`.

Derived `ToJson` / `FromJson` convert to and from JSON of the following shapes.

| Constructor | JSON |
|---|---|
| Record | `{"name": "ann", "age": 30}` (fields of `Option` type may be omitted or `null`) |
| Otherwise (type with a single constructor) | the value if there is one argument, an array if several, the constructor name as a string if none |
| Type with several constructors | `"Admin"` for a constructor without arguments, `{"tag": "Circle", "radius": 1}` for a record, otherwise `{"tag": "Polygon", "values": [...]}` |

```haskell
import Std.Json

type User = User { name : String, age : Int, email : Option String }
  deriving (Show, Json.ToJson, Json.FromJson)

Json.encode (User { name = "ann", age = 30, email = None })
-- {"name":"ann","age":30,"email":null}
(Json.decode "{\"name\": \"bob\", \"age\": \"x\"}" : Result String User)
-- Err "field 'age': expected an integer, got a string"
```

### Records

Constructor fields can be named.

```haskell
type User = User { name : String, age : Int, tags : List String }
  deriving (Show, Eq)

-- fields of the same type can be written together; several constructors can have fields
type Shape
  = Circle { x, y : Float, r : Float }
  | Rect { x, y : Float, w : Float, h : Float }
```

| Syntax | Example |
|---|---|
| Construction | `User { name = "ann", age = 30, tags = [] }` (every field is required; any order) |
| Punning | `User { name, age, tags = [] }` (`name` means `name = name`) |
| Access | `u.name`, `pet.owner.name`, `(f x).age` |
| Accessor function | `.name` (`\r -> r.name`). `map .name users` |
| Update | `{ u \| age = u.age + 1, tags = [] }` (returns a new value; the type does not change) |
| Pattern | `User { name, age = a }` (fields not written are `_`) |

- A `.` written without a space before it is a field access; after a space or at the start of a line it is an accessor function (`f u.name` is `f (u.name)`, `f .name` is `f (\r -> r.name)`).
- Field access and update are resolved by **the record's type**. If the type is not known yet (as in `\u -> u.name`), resolution waits until the type of the enclosing binding is determined; if it is still unknown, it is an error asking for a type annotation (the type is never guessed from the field name).
- Only fields that **every constructor** of the type has can be accessed or updated (`x` and `y` of `Shape`).
- The values of a construction or update are evaluated in the order written in the source.
- Record constructors can be constructed and matched only with names (`User "ann" 30 []` and `User n a _` are errors).
- `deriving Show` displays records in record syntax (`User { name = "ann", age = 30, tags = [] }`).

### Indexed types

Type parameters can carry value-like information (a length, a state, the type of an expression, etc.), which matching can recover.

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

- **Data kinds**: a type without parameters whose fields are only such types (`Nat`, `Bool`, etc.) can be used as a kind, and its constructors as types (`Z : Nat`, `S : Nat -> Nat`). A type with the same name takes precedence.
- **Kind annotations**: a type parameter can be given a kind, as in `(n : Nat)`. Kinds are `Type`, `Row`, `Effect`, data types, and arrows between them (`Type -> Type`). Omitted kinds are inferred.
- **GADTs**: after `type T a where`, list constructor signatures `C : T1 -> .. -> T ..`. The result type is the declared type applied to as many arguments as it has parameters, and type variables are implicitly quantified per constructor. `deriving` can be written at the end of the block (`FromJson`, and classes for types with constructors that have existential type variables, cannot be derived). Constructors can have neither type class constraints nor field names.
- **Refinement**: matching a GADT constructor assumes, within that arm, the type equations the constructor implies (in the `IntE` arm of `eval : Expr a -> a`, `a = Int`). Constructor type variables not determined by the result type become existential and cannot escape the arm. In a refining match, the type of the scrutinee must be known (give a signature). Nothing is assumed in the arm about parts of the scrutinee's type that are still unknown. When the arms have different types, the result type of the match must also be determined, by a signature or otherwise. A `let` pattern cannot match a refining constructor.
- **Exhaustiveness**: constructors that cannot occur given the value's type need no case (`vhead : Vec (S n) a -> a` needs no `VNil` case, and `vzip` needs no `(VNil, VCons ..)` case). Conversely, a pattern of a constructor that cannot occur is an error.
- **Type families**: after `type family F (a : K) .. : K where`, list equations `F p1 .. pn = t`. Patterns consist of type constructors and mutually distinct type variables, and equations are tried top to bottom. A later equation is not used while an earlier one might still apply. To prevent non-terminating reduction, when the right-hand side applies the family itself (or a family that comes back to it), the arguments must not contain type families, the arguments as a whole must be smaller than the patterns, and no type variable may be used more times than in the patterns (`Add (S n) m = S (Add n m)` is fine, and so is using it as an argument of another family, as in `Mul (S n) m = Add m (Mul n m)`). A type family is always applied to all its arguments and cannot be an instance head. The result kind cannot be `Row` or `Effect`. Irreducible applications are considered equal only when their arguments are equal.

## 5. Expressions

| Syntax | Example |
|---|---|
| Function application (left-associative, curried) | `f x y` |
| Operators | `a + b * c` |
| Operator as a function | `(+)`, `foldl (+) 0 xs` |
| Sections | `(+ 1)`, `(10 -)` |
| Lambda | `\x y -> x + y`, `\(a, b) -> a` |
| let | `let x = 1` (statement in a block), `let x = 1 in x + 1` |
| Local function | `let go acc n = ...` (may be recursive) |
| Local value | `let x = x + 1` (value bindings are not recursive; the `x` on the right is the outer `x`) |
| Pattern binding | `let (x, y) = pair` |
| match | `match e with` + a block of arms |
| handle | `handle e with` + a block of clauses (chapter 9) |
| Tuple / unit | `(1, "a")`, `()` |
| List / range | `[1, 2, 3]`, `x :: xs`, `[1 .. 10]` |
| Record | `User { name = "a", age = 1 }`, `u.name`, `.name`, `{ u \| age = 2 }` (chapter 4) |
| Type annotation | `(e : List Int)` |
| Pipe | `xs \|> map f` (same as `map f xs`) |
| Concatenation | `"a" ++ "b"`, `[1] ++ [2]` (`Semigroup`) |
| Block argument | `forEach xs \x ->` followed by an indented body |

Integer `/` truncates toward zero and `%` is its remainder, so `(a / b) * b + a % b == a` holds (`-7 / 2 == -3`, `-7 % 2 == -1`). The remainder with the sign of the divisor is `mod` (`mod (-7) 2 == 1`).

`&&` and `||` short-circuit. All other functions are strict.

The last statement of a block is the block's value. The values of earlier statements must be `Unit` (to discard one, use `let _ = ...`).

There is no `if` expression. Use `match` and guards.

```haskell
max a b = match a >= b with
  True -> a
  False -> b
```

## 6. Patterns

| Pattern | Example |
|---|---|
| Variable / wildcard | `x`, `_` |
| Literal | `0`, `'a'`, `"text"`, `1.5` |
| Constructor | `None`, `Some x`, `Node l v r` |
| List | `[]`, `[x]`, `[x, y]`, `x :: rest` |
| Tuple | `(a, b)` |
| As pattern | `all@(x :: _)` |
| Record | `User { name, age = a }` |
| Type annotation | `(x : Int)` |

In argument position of a function, constructor patterns with arguments are parenthesized: `f (Some x) = ...`.

## 7. Types

```haskell
Int  Float  String  Char  Bool  Unit
List a      Option a      Result e a      (a, b)
a -> b                  -- function
a -> <IO> b             -- function with the IO effect
a -> <State Int | e> b  -- State Int and other effects e
Eq a => a -> a -> Bool  -- type class constraint
```

Type variables are implicitly universally quantified. Type inference is based on Hindley–Milner, and local `let`s are polymorphic too (under the value restriction).
Kinds of types (`Type`, `Type -> Type`, data kinds such as `Nat`) are also inferred ([Indexed types](#indexed-types)).

### Writing effect rows

- Arrows of function types carry an effect row. `<IO, State Int>` is a closed row and `<IO | e>` is an open row.
- An arrow without a written row in a signature has an **implicit effect**.
  - Intermediate arrows of a curried function (partial application) are pure (likewise inside argument function types).
  - The last arrow, and the last arrow of argument function types, carry an implicit effect variable shared across the whole signature.
    So `map : (a -> b) -> List a -> List b` means "performs exactly the effects that `f` performs".
  - If exactly one row variable is written in the signature, it is used as the implicit effect.
- A function with an explicit closed row (e.g. `println : String -> <IO> Unit`) can be called in contexts that allow more effects.
- Arrows without a written row in data type fields or inside type arguments (`List (Int -> Int)`, tuple elements, etc.) are pure (`<>`). To store effectful functions, write it explicitly, as in `List (Int -> <e> Int)`.
- Constructor application has no effects (a partially applied constructor can be passed as a pure function).

## 8. Type classes

```haskell
class Eq a => Ord a where           -- superclass
  compare : a -> a -> Ordering
  (<) : a -> a -> Bool
  x < y = compare x y == LT          -- default implementation

instance Ord Int where
  compare = primIntCompare

instance Eq a => Eq (List a) where   -- instance with a context
  ...
```

- Type classes take a single parameter. The parameter may be higher-kinded, and its kind is inferred (`f : Type -> Type` in `class Functor f`).
- An instance head is "a type constructor + distinct type variables" (partial application allowed: `instance Functor (Result e)`).
- If a constraint's type variable appears in no type and is left undetermined (such as the element type in `show []`), it is an error. Determine it with a type annotation (`show ([] : List Int)`).
- The implementation uses dictionary passing, and method calls on statically known instances are optimized to direct calls (`Int` arithmetic becomes Go operations).

### Functor / Applicative / Monad / Foldable and effects

The methods of `Functor`, `Applicative`, `Monad`, `Foldable` and `Traversable` call the functions they receive **on the spot**, so functions that perform effects can be passed too (`map (\x -> println x; x) xs`).
`List`, `Option`, `Result`, `Map`, `Array`, `Queue` and `Set` (`Foldable` only) all use the same `map` / `andThen` / `foldl` / `length` / `isEmpty` / `toList`.
`Foldable`'s `toList`, `length` and `isEmpty` have default implementations based on folding, and instances can override them with faster ones (`Array`'s `length` is constant time).

Types that store the functions they receive and call them "later" (such as parsers or state transformers wrapping functions) cannot be instances of these classes, because by then the caller's handlers may be gone (the type checker rejects them).
Such computations are expressed with effects. For example, `Std.Parser` is a parser built on the `ParseInput` / `Fail` effects.

## 9. Algebraic effects

### Declaration and use

```haskell
effect State s where
  get : Unit -> s
  put : s -> Unit
```

The operations `get` and `put` can be called like ordinary functions. Their types are `get : Unit -> <State s> s`, etc.
Operations without arguments are declared to take `Unit`.

### Handlers

```haskell
runState : s -> (Unit -> <State s | e> a) -> <e> (a, s)
runState init action =
  let run = handle action () with
    return x -> \s -> (x, s)         -- when the body returns a value (identity when omitted)
    get () k -> \s -> k s s          -- k is the continuation (deep handler)
    put s k -> \_ -> k () s
  run init
```

- `handle e with` evaluates `e` and handles the operations performed in it with the clauses. Every operation needs a clause.
- Calling the continuation `k` resumes the suspended computation, and the resumed computation also runs under the same handler (deep handler). The result of `k` is the result of the whole handler.
- If `k` is not called, the computation is discarded (exceptions). `k` can also be stored in a data structure and called later (generators, cooperative threads).
- Continuations are one-shot (resuming a second time is a runtime error).

### Typing

A handler removes its own effect from the effect row of the body.

```haskell
catch : (Unit -> <Raise | e> a) -> (String -> <e> a) -> <e> a
```

`main` must be `Unit -> <IO> Unit`, and unhandled effects are detected at compile time.

### Automatic mask insertion

When a handler clause uses another effect, an effect of the same name in a function received as an argument can be unintentionally caught by the inner handler (the semantics of scoped labels).
Arkadia's type checker detects such places and automatically inserts a *mask* around the call. Operations inside a masked call skip the inner handler and reach the outer one.

### Built-in effects

| Effect | Description |
|---|---|
| `IO` | Input and output with the outside world. Handlers cannot be written for it |
| `Fail` | `fail : String -> a`. `runFail` converts it to `Result String a`. Also used for test failures |

See also `Std.State`, `Std.Reader`, `Std.Writer`, `Std.Exn`, `Std.Random`, `Std.Gen` (generators), `Std.Parser` (parser combinators) and `Std.Http` (early return from HTTP handlers with the `Halt` effect). Each module's API and documentation can be shown with, e.g., `ark doc Std.Http`.

### Implementation

Effects are implemented with *evidence passing*. Every generated function receives the caller's *evidence* (an immutable list of the handler frames in effect at that point, innermost first) as its first argument.
`handle` calls the body with evidence that has a new frame pushed, and an operation looks up the innermost (unmasked) handler in the evidence it received. Clauses run with the evidence outside the handler (deep handlers).
Since there is no global handler stack, nothing has to be restored when a computation unwinds, and any number of independent computations (tasks) can run in parallel.
Evidence frames also point to the running task and its cancellation scope, which generated code accesses in constant time.

Each handler runs in the cheapest way for its shape.

| Handler shape | How it runs |
|---|---|
| Every clause calls `k` exactly once in tail position (Reader, loggers, etc.) | The clause runs directly at the call site |
| Clauses resume in tail position or do not use `k` (exceptions) | Runs at the call site; aborting uses panic/recover |
| State passing `return x -> \s -> ...` / `op k -> \s -> k v s'` immediately applied to an initial state (State, Writer, Random) | The state is kept in the handler's frame, and it runs at the call site |
| Anything else (storing continuations, non-tail resumption) | The body runs on a dedicated goroutine and operations are sent as messages (only one goroutine within a task runs at a time) |

## 10. Tasks and parallelism

Tasks from `Std.Task` are computations that run in parallel on every processor.

```haskell
import Std.Task (..)

main () = runAsync (\_ ->
  let a = spawn (\_ -> fib 32)        -- start a task and get a Task Int
  let b = spawn (\_ -> fib 33)
  println "${await a + await b}")     -- wait for the results
```

| Function | Description |
|---|---|
| `runAsync : (Unit -> <Async, IO \| e> a) -> <IO \| e> a` | Opens a task scope and waits until the body and every task started in it have finished |
| `spawn : (Unit -> <Async, IO> a) -> <Async> Task a` / `fork` | Starts a task in the innermost scope (`fork` when the result is not used) |
| `await` / `awaitOutcome` | Waits for a task's result (`awaitOutcome` gives `Completed x` or `Cancelled`) |
| `cancel` / `timeout` / `cancelOnInterrupt` | Cancels a task, runs with a time limit (returns an `Option`), cancels on Ctrl-C / SIGTERM |
| `finally` / `uncancellable` | Cleanup that runs however things end, a section that is not cancelled |
| `parallel` / `parMap` / `both` / `race` | Runs in parallel and collects the results, takes whichever finishes first |
| `catchError` | Turns a runtime error into `Err` (for tasks whose failure should not stop the scope) |

**Structure.** A task is always started inside a scope (`runAsync`) and never outlives it. The `Async` effect means "can start tasks" and is handled by `runAsync`. The scope to start in is found from the evidence.

**Effects.** The only effects a task can use are `Async` and `IO`. Effects handled outside the scope (such as `State`) cannot be used inside a task, so handle them inside the task if needed. This way a handler is never called from several threads at once. The body of `runAsync` and the contents of `timeout` and the like run in the calling task, so they can use outer effects as is.

**Failure.** When a task fails with a runtime error, the other tasks in the same scope and the body are cancelled immediately, and once everything has finished, `runAsync` raises that error. `await` also re-raises the error of a failed task.

**Cancellation.** Cancellation is requested asynchronously, and the computation unwinds at the next *cancellation point*. The compiler inserts cancellation points (`rt.Poll`) at the entry of looping functions, recursive functions (strongly connected components of the global reference graph), and functions that call function values, effect operations or higher-order foreign functions. Every cycle of calls passes through one of them, so even a pure infinite loop can be cancelled. Waiting in `await`, `sleep`, channels, locks and the network are cancellation points too. Unwinding runs `finally` cleanups and stops at the outermost cancelled scope within the task (`None` for `timeout`, `Cancelled` for a task).

**Preemption.** Tasks run on Go goroutines; the Go scheduler distributes them across all processors and asynchronously preempts long-running goroutines. A task that keeps computing purely does not stop the other tasks (even with `GOMAXPROCS=1`). A task waiting on I/O does not occupy an OS thread.

**Sharing data.** To share data between tasks, use `Std.Channel` (bounded queues; `closeChannel` signals the end), `Std.Atomic` (references updated atomically with pure functions) and `Std.Mutex` (`withLock`, and `Shared` values whose updates hold the lock). `Ref`, `MArray` and `HashMap` are meant for use within a single task and must not be used from two tasks at once (data races).

**HTTP.** `serve` in `Std.Http` handles each request in its own task, so requests are processed in parallel. Handlers can use `Async` to start tasks as part of a request, and requests whose client disconnects are cancelled.

## 11. Testing

Tests are written as top-level `test "name" = body` declarations. Tests are normally placed right next to what they test, in the same module.
Tests are also executable documentation showing how functions are used; kept alongside the code, they are read together and updated together.

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

- A test body has type `Unit` and can use the `<Fail, IO>` effects. A test fails when it calls `fail` or hits a runtime error, and the other tests keep running.
- The Prelude's `assert : Bool -> String -> <Fail> Unit` fails with the given message when the condition is false, and `assertEq actual expected` fails, `show`ing both, when the two values are not equal. `orFail msg opt` extracts the value from an `Option` and fails on `None`.
- Tests in a module can also use its `private` declarations.
- Tests run only under `ark test` and are never called from programs built by `ark build` / `ark run`.

### Where to put tests

| Location | Purpose |
|---|---|
| Inside each module in `src/` (recommended) | Unit tests, written in the same module as what they test |
| Modules in `tests/` | Integration tests spanning several modules. They `import` what they test and use only the public API |

The `ark new` skeleton also puts the test of `greet` right below it in `src/Greeting.ark`.
Create `tests/` when you need it (modules in `tests/` can `import` modules in `src/`, and are not included in the build).

### Running

```sh
ark test                 # run the tests of every module in src/ and tests/
ark test -run Stack      # run only the tests whose "module › test name" contains this string
ark test src/Stack.ark   # run the tests of one file (and the modules it imports)
```

```
✓ Stack › pop returns the last pushed value
✓ Stack › pop on an empty stack is None

all 2 tests passed (0s)
```

If any test fails, the reasons are shown and the exit code is 1.

## 12. FFI

```haskell
foreign sqrt : Float -> Float = "rt.FloatSqrt"
```

`foreign` binds a Go function as an Arkadia function. The Go function takes as many `rt.Value` (= `any`) arguments as the function has, and returns an `rt.Value`.
Functions put in a package's `native/*.go` (`package main`, `import rt "arkprog/rt"`) can be referred to from `foreign`.
`private foreign` binds without exporting, which is useful for implementing type class methods in Go.

Higher-order foreign functions (those taking functions as arguments) and foreign functions whose target is written `"ev:pkg.Func"` also receive the caller's evidence `ev *rt.Ev` as their first argument.
Arkadia function values are called by passing it, as in `rt.Apply1(ev, f, x)`.
A function that waits for a long time should also wait on `ev.Done()` (a channel closed when the computation is cancelled), and when it is closed, call `rt.Poll(ev)` to unwind the computation.

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

| Arkadia | Go representation |
|---|---|
| `Int` / `Float` / `String` / `Char` / `Bool` | `int64` / `float64` / `string` / `rune` / `bool` |
| `Unit` | `rt.Unit` |
| Data constructors | `*rt.D0` to `*rt.D3`, `*rt.DN` (`T` is the tag) |
| Lists | `rt.Nil`, `rt.Cons(h, t)`, `rt.ListToSlice`, `rt.SliceToList` |
| `Option` / `Result` | `rt.None`, `rt.Some(x)`, `rt.Ok(x)`, `rt.Err(e)` |
| Functions | called with `rt.Apply1(ev, f, x)` and the like |

## 13. Evaluation and runtime

- Strict (call by value). Arguments are evaluated left to right.
- Self tail calls (in top-level and local functions) are compiled to loops. Other recursion can also go deep thanks to Go's growable stacks.
- Values have a uniform representation (`any`), but `Int`/`Float` arithmetic is done unboxed as far as is statically known.
- Memory is managed by Go's GC (a concurrent GC).
- Tasks run in parallel on Go goroutines (the number of processors can be changed with `GOMAXPROCS`). Standard output is shared between tasks, and a single `println` line is never interleaved. Output is buffered and flushed before waiting for input or I/O, or at most 50ms after it is written.
- A runtime error (`error`, division by zero, pattern match failure, unhandled effect) exits after showing the message and the Arkadia functions that were running when the error occurred (innermost first, with recursion collapsed into a count). `ARKADIA_TRACE=1` also shows the Go stack trace.

```
runtime error: division by zero
  in Main.average
  in Main.loop (4 times)
  in Main.main
```
