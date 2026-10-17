package syntax

import "testing"

const sample = `module Main

import Std.List as L
import Std.Option (map, Option(..))

infixl 1 |>

type Shape
  = Circle Float
  | Rect Float Float
  deriving (Show, Eq)

type Tree a =
  | Leaf
  | Node (Tree a) a (Tree a)

class Functor f where
  map : (a -> b) -> f a -> f b

class Eq a => Ord a where
  compare : a -> a -> Ordering
  lt : a -> a -> Bool
  lt x y = compare x y == LT

instance Show a => Show (Option a) where
  show None = "None"
  show (Some x) = "Some " ++ show x

effect State s where
  get : Unit -> s
  put : s -> Unit

foreign println : String -> <IO> Unit = "rt.Println"

x |> f = f x

fact : Int -> Int
fact 0 = 1
fact n = n * fact (n - 1)

abs n
  | n < 0 = -n
  | otherwise = n

runState : s -> (Unit -> <State s | e> a) -> <e> (a, s)
runState init action =
  let f = handle action () with
    return x -> \s -> (x, s)
    get () k -> \s -> k s s
    put s k -> \_ -> k () s
  f init

main () =
  let xs = [1, 2, 3]
  println "sum: ${show (sum xs)}"
  xs |> L.map (\x -> x * 2) |> print
  match xs with
    [] -> println "empty"
    x :: rest | x > 0 -> println "pos"
              | otherwise ->
                  println "neg"
                  println "!"
  map (\x ->
         let y = x + 1
         y) xs
  xs |> map \x ->
    x + 1
  print [1..10]
  print (+ 1)
  print (x -)

test "fact" =
  assertEq (fact 5) 120
`

func TestParseSample(t *testing.T) {
	m, err := ParseModule("sample.ark", sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolveFixity(m, DefaultFixities()); err != nil {
		t.Fatal(err)
	}
	if len(m.Decls) < 10 {
		t.Fatalf("too few decls: %d", len(m.Decls))
	}
	for _, d := range m.Decls {
		t.Logf("%T", d)
	}
}
