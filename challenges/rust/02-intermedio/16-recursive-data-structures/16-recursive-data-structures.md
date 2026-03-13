# 16. Recursive Data Structures

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (structs, enums, ownership, pattern matching)
- Completed: 06-smart-pointers (Box basics)
- Familiar with generics, traits, and `Option<T>`

## Learning Objectives

- Explain why recursive types require heap allocation and why `Box<T>` solves the sizing problem
- Implement a singly linked list with push, pop, peek, and iteration
- Build a binary search tree with insert, search, and three traversal orders
- Apply the `Option<Box<Node>>` idiom for optional recursive children
- Compare stack-allocated vs heap-allocated approaches and read the compiler errors that guide you

## Concepts

### Why Recursive Types Need `Box`

A recursive type is one that contains itself. The simplest example is a linked list node that holds a value and a pointer to the next node. In many languages this works automatically because objects live on the heap behind references. In Rust, structs are stack-allocated by default, and the compiler must know the exact size of every type at compile time.

Consider this attempt:

```rust
// THIS DOES NOT COMPILE
enum List {
    Cons(i32, List),
    Nil,
}
```

The compiler rejects it with:

```
error[E0072]: recursive type `List` has infinite size
 --> src/main.rs:2:1
  |
2 | enum List {
  | ^^^^^^^^^
3 |     Cons(i32, List),
  |               ---- recursive without indirection
  |
help: insert some indirection (e.g., a `Box`, `Rc`, or `&`) to break the cycle
```

The problem: to compute the size of `List`, the compiler needs the size of `Cons`, which contains a `List`, which contains a `Cons`, which contains a `List`... the size is infinite. The compiler cannot allocate an infinite amount of stack space.

### The Fix: Indirection with `Box<T>`

`Box<T>` is a pointer to heap-allocated data. A `Box<List>` has a known, fixed size (one pointer width, typically 8 bytes on 64-bit systems) regardless of what `List` contains. This breaks the infinite recursion:

```rust
enum List {
    Cons(i32, Box<List>),
    Nil,
}
```

Now the size of `List` is: `max(size_of(Cons), size_of(Nil))`. `Cons` is `size_of(i32) + size_of(Box<List>)` = 4 + 8 = 12 bytes (plus alignment). `Nil` is 0 bytes. The discriminant adds a few bytes. The compiler is satisfied.

### Stack vs Heap: What Lives Where

```
Stack:                        Heap:
+---+---+--------+           +---+---+--------+
| 1 | * |  Cons  | --------> | 2 | * |  Cons  | ------+
+---+---+--------+           +---+---+--------+       |
                                                       v
                              +---+---+--------+
                              | 3 |   |  Nil   |
                              +---+---+--------+
```

The first node lives on the stack. Each `Box` allocates the next node on the heap. The `Nil` variant marks the end.

### The `Option<Box<Node>>` Idiom

For tree structures, the standard pattern uses `Option<Box<Node>>` for optional children:

```rust
struct TreeNode {
    value: i32,
    left: Option<Box<TreeNode>>,
    right: Option<Box<TreeNode>>,
}
```

This pattern is pervasive in Rust because:

- `Option<Box<T>>` has the same size as `Box<T>` (the compiler uses null-pointer optimization -- `None` is represented as a null pointer)
- It clearly communicates "this child may or may not exist"
- Pattern matching on `Some(node)` / `None` is natural for recursive algorithms

### Recursive Algorithms on Recursive Types

When the data structure is recursive, the algorithms that operate on it are naturally recursive too:

```rust
impl TreeNode {
    fn depth(&self) -> usize {
        let left_depth = match &self.left {
            Some(node) => node.depth(),
            None => 0,
        };
        let right_depth = match &self.right {
            Some(node) => node.depth(),
            None => 0,
        };
        1 + left_depth.max(right_depth)
    }
}
```

Each recursive call processes a smaller subtree, bottoming out when it hits `None`.

### Anti-Pattern: Recursive Types Without Indirection

Here is a comparison of approaches:

```rust
// WRONG: Infinite size
struct Node {
    value: i32,
    next: Node,  // compiler error: infinite size
}

// WRONG: Reference without lifetime -- won't compile either
struct Node {
    value: i32,
    next: &Node,  // missing lifetime parameter
}

// CORRECT: Box provides heap indirection
struct Node {
    value: i32,
    next: Option<Box<Node>>,
}

// ALSO CORRECT: Reference with explicit lifetime (for borrowed views)
struct NodeRef<'a> {
    value: &'a i32,
    next: Option<&'a NodeRef<'a>>,
}
```

`Box` owns the data. References borrow it. For data structures that own their elements, `Box` is the right choice.

### Enum-Based Recursive Structures

An alternative design uses an enum at the top level instead of wrapping `Option<Box<...>>` inside a struct:

```rust
// Enum approach:
enum List<T> {
    Cons(T, Box<List<T>>),
    Nil,
}

// Struct approach:
struct List<T> {
    head: Option<Box<Node<T>>>,
}

struct Node<T> {
    value: T,
    next: Option<Box<Node<T>>>,
}
```

The struct approach is more practical for real code because it lets you store metadata (length, tail pointer) on the `List` itself, and it gives you a natural place for `impl List<T>` methods. The enum approach is elegant for learning but awkward when you need to mutate or track metadata.

## Exercises

### Exercise 1: Singly Linked List -- Construction and Display

Build a singly linked list that supports push_front (prepend) and display.

```rust
use std::fmt;

struct Node<T> {
    value: T,
    next: Option<Box<Node<T>>>,
}

struct LinkedList<T> {
    head: Option<Box<Node<T>>>,
    len: usize,
}

impl<T> LinkedList<T> {
    /// Creates an empty linked list.
    fn new() -> Self {
        LinkedList { head: None, len: 0 }
    }

    /// Returns the number of elements.
    fn len(&self) -> usize {
        self.len
    }

    /// Returns true if the list is empty.
    fn is_empty(&self) -> bool {
        self.len == 0
    }

    // TODO: Implement push_front that adds an element at the beginning.
    // Steps:
    //   1. Create a new Node with the given value
    //   2. Set its `next` to the current head (use self.head.take())
    //   3. Set self.head to Some(Box::new(new_node))
    //   4. Increment self.len
    //
    // fn push_front(&mut self, value: T) {
    //     todo!()
    // }

    // TODO: Implement peek_front that returns a reference to the first element.
    // Hint: Use self.head.as_ref().map(|node| &node.value)
    //
    // fn peek_front(&self) -> Option<&T> {
    //     todo!()
    // }

    // TODO: Implement pop_front that removes and returns the first element.
    // Steps:
    //   1. Take the current head with self.head.take()
    //   2. If there was a node, set self.head to node.next
    //   3. Decrement self.len
    //   4. Return Some(node.value)
    //
    // fn pop_front(&mut self) -> Option<T> {
    //     todo!()
    // }
}

impl<T: fmt::Display> fmt::Display for LinkedList<T> {
    // TODO: Implement Display to show the list as: [1 -> 2 -> 3]
    // For an empty list, show: []
    // Hint: Walk the list by following `next` references.
    //
    // fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
    //     todo!()
    // }
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        todo!()
    }
}

fn main() {
    let mut list = LinkedList::new();
    assert!(list.is_empty());

    list.push_front(3);
    list.push_front(2);
    list.push_front(1);

    println!("List: {list}");       // [1 -> 2 -> 3]
    println!("Length: {}", list.len()); // 3
    println!("Front: {:?}", list.peek_front()); // Some(1)

    let val = list.pop_front();
    println!("Popped: {:?}", val);  // Some(1)
    println!("List: {list}");       // [2 -> 3]
    println!("Length: {}", list.len()); // 2

    // Pop everything:
    list.pop_front();
    list.pop_front();
    assert!(list.is_empty());
    println!("Empty list: {list}"); // []
    println!("Pop empty: {:?}", list.pop_front()); // None
}
```

### Exercise 2: Linked List Iterator

Add iteration support to the linked list from Exercise 1.

```rust
use std::fmt;

struct Node<T> {
    value: T,
    next: Option<Box<Node<T>>>,
}

struct LinkedList<T> {
    head: Option<Box<Node<T>>>,
    len: usize,
}

// (copy your push_front, pop_front, peek_front implementations from Exercise 1)

impl<T> LinkedList<T> {
    fn new() -> Self {
        LinkedList { head: None, len: 0 }
    }

    fn len(&self) -> usize {
        self.len
    }

    fn is_empty(&self) -> bool {
        self.len == 0
    }

    fn push_front(&mut self, value: T) {
        let new_node = Node {
            value,
            next: self.head.take(),
        };
        self.head = Some(Box::new(new_node));
        self.len += 1;
    }

    // TODO: Implement an `iter` method that returns a ListIter.
    // ListIter holds a reference to the current node: Option<&Node<T>>
    //
    // fn iter(&self) -> ListIter<'_, T> {
    //     todo!()
    // }
}

// TODO: Define the ListIter struct.
// It needs one field: `current: Option<&'a Node<T>>`
//
// struct ListIter<'a, T> {
//     todo!()
// }

// TODO: Implement Iterator for ListIter.
// type Item = &'a T
// In next():
//   1. Take the current node with self.current.take() or use map
//   2. Advance self.current to node.next (converting &Box<Node<T>> to &Node<T>)
//   3. Return &node.value
//
// Hint: self.current.map(|node| { ... }) is the clean approach.
// To go from Option<&Box<Node<T>>> to Option<&Node<T>>, use:
//   node.next.as_ref().map(|boxed| boxed.as_ref())
//   or equivalently: node.next.as_deref()

// TODO: Implement IntoIterator for &LinkedList<T> so for-loops work.
// This just calls self.iter().
//
// impl<'a, T> IntoIterator for &'a LinkedList<T> {
//     type Item = &'a T;
//     type IntoIter = ListIter<'a, T>;
//     fn into_iter(self) -> Self::IntoIter {
//         self.iter()
//     }
// }

fn main() {
    let mut list = LinkedList::new();
    list.push_front(30);
    list.push_front(20);
    list.push_front(10);

    // Using iter() explicitly:
    print!("Iter: ");
    for val in list.iter() {
        print!("{val} ");
    }
    println!();
    // Output: Iter: 10 20 30

    // Using &list in for-loop (IntoIterator):
    print!("For-loop: ");
    for val in &list {
        print!("{val} ");
    }
    println!();
    // Output: For-loop: 10 20 30

    // Using iterator adapters:
    let sum: i32 = list.iter().sum();
    println!("Sum: {sum}"); // 60

    let doubled: Vec<i32> = list.iter().map(|x| x * 2).collect();
    println!("Doubled: {doubled:?}"); // [20, 40, 60]

    let evens: Vec<&i32> = list.iter().filter(|x| *x % 2 == 0).collect();
    println!("Evens: {evens:?}"); // [20, 30]

    // Verify the list is still usable (iter borrows, does not consume):
    println!("Length still: {}", list.len()); // 3
}
```

### Exercise 3: Binary Search Tree -- Insert and Search

Implement a binary search tree with insert and search.

```rust
use std::fmt;

struct TreeNode<T> {
    value: T,
    left: Option<Box<TreeNode<T>>>,
    right: Option<Box<TreeNode<T>>>,
}

struct BinarySearchTree<T> {
    root: Option<Box<TreeNode<T>>>,
    size: usize,
}

impl<T: Ord> BinarySearchTree<T> {
    fn new() -> Self {
        BinarySearchTree { root: None, size: 0 }
    }

    fn len(&self) -> usize {
        self.size
    }

    fn is_empty(&self) -> bool {
        self.size == 0
    }

    // TODO: Implement insert.
    // Use a helper function or a recursive approach.
    //
    // Strategy: write a helper function that takes &mut Option<Box<TreeNode<T>>>
    // and inserts the value into the correct position:
    //
    //   fn insert_into(node: &mut Option<Box<TreeNode<T>>>, value: T) -> bool
    //
    // If the slot is None, place a new node there and return true.
    // If the slot holds a node:
    //   - if value < node.value, recurse into node.left
    //   - if value > node.value, recurse into node.right
    //   - if value == node.value, return false (duplicate, don't insert)
    //
    // fn insert(&mut self, value: T) -> bool {
    //     let inserted = Self::insert_into(&mut self.root, value);
    //     if inserted { self.size += 1; }
    //     inserted
    // }

    // TODO: Implement contains.
    // Similar recursive helper, but with &Option<Box<TreeNode<T>>> (shared ref):
    //
    //   fn search(node: &Option<Box<TreeNode<T>>>, value: &T) -> bool
    //
    // fn contains(&self, value: &T) -> bool {
    //     Self::search(&self.root, value)
    // }

    // TODO: Implement min and max.
    // min: follow left children until you hit None.
    // max: follow right children until you hit None.
    //
    // fn min(&self) -> Option<&T> {
    //     todo!()
    // }
    //
    // fn max(&self) -> Option<&T> {
    //     todo!()
    // }

    // TODO: Implement depth (height of the tree).
    // An empty tree has depth 0. A single node has depth 1.
    //
    // fn depth(&self) -> usize {
    //     Self::node_depth(&self.root)
    // }
    //
    // fn node_depth(node: &Option<Box<TreeNode<T>>>) -> usize {
    //     todo!()
    // }
}

fn main() {
    let mut tree = BinarySearchTree::new();
    assert!(tree.is_empty());

    // Insert values:
    tree.insert(5);
    tree.insert(3);
    tree.insert(7);
    tree.insert(1);
    tree.insert(4);
    tree.insert(6);
    tree.insert(8);

    //     5
    //    / \
    //   3   7
    //  / \ / \
    // 1  4 6  8

    println!("Size: {}", tree.len()); // 7
    println!("Depth: {}", tree.depth()); // 3

    // Search:
    println!("Contains 4: {}", tree.contains(&4)); // true
    println!("Contains 9: {}", tree.contains(&9)); // false

    // Duplicate:
    let was_new = tree.insert(5);
    println!("Insert 5 again: {was_new}"); // false
    println!("Size still: {}", tree.len()); // 7

    // Min/Max:
    println!("Min: {:?}", tree.min()); // Some(1)
    println!("Max: {:?}", tree.max()); // Some(8)

    // Works with strings too:
    let mut words = BinarySearchTree::new();
    words.insert("mango");
    words.insert("apple");
    words.insert("zebra");
    words.insert("banana");
    println!("Has 'apple': {}", words.contains(&"apple")); // true
    println!("Has 'grape': {}", words.contains(&"grape")); // false
    println!("Min word: {:?}", words.min()); // Some("apple")
    println!("Max word: {:?}", words.max()); // Some("zebra")
}
```

### Exercise 4: Binary Tree Traversals

Add inorder, preorder, and postorder traversals to the BST.

```rust
use std::fmt;

struct TreeNode<T> {
    value: T,
    left: Option<Box<TreeNode<T>>>,
    right: Option<Box<TreeNode<T>>>,
}

struct BinarySearchTree<T> {
    root: Option<Box<TreeNode<T>>>,
    size: usize,
}

impl<T: Ord> BinarySearchTree<T> {
    fn new() -> Self {
        BinarySearchTree { root: None, size: 0 }
    }

    fn insert(&mut self, value: T) -> bool {
        fn insert_into<T: Ord>(node: &mut Option<Box<TreeNode<T>>>, value: T) -> bool {
            match node {
                None => {
                    *node = Some(Box::new(TreeNode {
                        value,
                        left: None,
                        right: None,
                    }));
                    true
                }
                Some(n) => {
                    if value < n.value {
                        insert_into(&mut n.left, value)
                    } else if value > n.value {
                        insert_into(&mut n.right, value)
                    } else {
                        false
                    }
                }
            }
        }
        let inserted = insert_into(&mut self.root, value);
        if inserted {
            self.size += 1;
        }
        inserted
    }

    // TODO: Implement inorder traversal (Left, Root, Right).
    // This produces values in sorted order for a BST.
    // Use a helper that takes &Option<Box<TreeNode<T>>> and a &mut Vec<&T>.
    //
    // fn inorder(&self) -> Vec<&T> {
    //     let mut result = Vec::new();
    //     Self::inorder_walk(&self.root, &mut result);
    //     result
    // }
    //
    // fn inorder_walk<'a>(node: &'a Option<Box<TreeNode<T>>>, result: &mut Vec<&'a T>) {
    //     todo!()
    //     // If node is Some:
    //     //   1. Recurse left
    //     //   2. Push &node.value
    //     //   3. Recurse right
    // }

    // TODO: Implement preorder traversal (Root, Left, Right).
    // Used for serializing/copying a tree.
    //
    // fn preorder(&self) -> Vec<&T> {
    //     let mut result = Vec::new();
    //     Self::preorder_walk(&self.root, &mut result);
    //     result
    // }
    //
    // fn preorder_walk<'a>(node: &'a Option<Box<TreeNode<T>>>, result: &mut Vec<&'a T>) {
    //     todo!()
    //     // If node is Some:
    //     //   1. Push &node.value
    //     //   2. Recurse left
    //     //   3. Recurse right
    // }

    // TODO: Implement postorder traversal (Left, Right, Root).
    // Used for deletion / cleanup.
    //
    // fn postorder(&self) -> Vec<&T> {
    //     let mut result = Vec::new();
    //     Self::postorder_walk(&self.root, &mut result);
    //     result
    // }
    //
    // fn postorder_walk<'a>(node: &'a Option<Box<TreeNode<T>>>, result: &mut Vec<&'a T>) {
    //     todo!()
    //     // If node is Some:
    //     //   1. Recurse left
    //     //   2. Recurse right
    //     //   3. Push &node.value
    // }

    // TODO: Implement a collect_sorted method that returns owned values
    // using an into_inorder consuming traversal.
    //
    // fn into_sorted(self) -> Vec<T> {
    //     let mut result = Vec::new();
    //     Self::into_inorder_walk(self.root, &mut result);
    //     result
    // }
    //
    // fn into_inorder_walk(node: Option<Box<TreeNode<T>>>, result: &mut Vec<T>) {
    //     todo!()
    //     // If node is Some, unbox it:
    //     //   let unboxed = *node;
    //     //   1. Recurse left (unboxed.left)
    //     //   2. Push unboxed.value
    //     //   3. Recurse right (unboxed.right)
    // }
}

impl<T: Ord + fmt::Display> BinarySearchTree<T> {
    // TODO: Implement a pretty-print method that shows the tree structure.
    // A simple approach: print inorder with indentation based on depth.
    //
    // fn pretty_print(&self) {
    //     Self::print_node(&self.root, 0, "Root");
    // }
    //
    // fn print_node(node: &Option<Box<TreeNode<T>>>, depth: usize, label: &str) {
    //     if let Some(n) = node {
    //         Self::print_node(&n.right, depth + 1, "R");
    //         let indent = "    ".repeat(depth);
    //         println!("{indent}{label}: {}", n.value);
    //         Self::print_node(&n.left, depth + 1, "L");
    //     }
    // }
}

fn main() {
    let mut tree = BinarySearchTree::new();
    for val in [5, 3, 7, 1, 4, 6, 8, 2] {
        tree.insert(val);
    }

    //       5
    //      / \
    //     3   7
    //    / \ / \
    //   1  4 6  8
    //    \
    //     2

    let inorder = tree.inorder();
    println!("Inorder:   {inorder:?}");   // [1, 2, 3, 4, 5, 6, 7, 8]

    let preorder = tree.preorder();
    println!("Preorder:  {preorder:?}");  // [5, 3, 1, 2, 4, 7, 6, 8]

    let postorder = tree.postorder();
    println!("Postorder: {postorder:?}"); // [2, 1, 4, 3, 6, 8, 7, 5]

    // Pretty print:
    println!("\nTree structure:");
    tree.pretty_print();

    // Consuming traversal -- tree is moved:
    let sorted = tree.into_sorted();
    println!("\nSorted (owned): {sorted:?}"); // [1, 2, 3, 4, 5, 6, 7, 8]
    // tree is no longer usable here
}
```

### Exercise 5: Recursive Enum -- Expression Evaluator

Build a simple arithmetic expression tree using a recursive enum.

```rust
use std::fmt;

/// A recursive enum representing arithmetic expressions.
/// Each variant is either a leaf (a number) or a node with children.
#[derive(Debug, Clone)]
enum Expr {
    /// A numeric literal.
    Num(f64),
    /// Addition: left + right
    Add(Box<Expr>, Box<Expr>),
    /// Subtraction: left - right
    Sub(Box<Expr>, Box<Expr>),
    /// Multiplication: left * right
    Mul(Box<Expr>, Box<Expr>),
    /// Division: left / right
    Div(Box<Expr>, Box<Expr>),
    /// Negation: -expr
    Neg(Box<Expr>),
}

impl Expr {
    // Convenience constructors to avoid writing Box::new everywhere:
    fn num(val: f64) -> Self {
        Expr::Num(val)
    }

    fn add(left: Expr, right: Expr) -> Self {
        Expr::Add(Box::new(left), Box::new(right))
    }

    fn sub(left: Expr, right: Expr) -> Self {
        Expr::Sub(Box::new(left), Box::new(right))
    }

    fn mul(left: Expr, right: Expr) -> Self {
        Expr::Mul(Box::new(left), Box::new(right))
    }

    fn div(left: Expr, right: Expr) -> Self {
        Expr::Div(Box::new(left), Box::new(right))
    }

    fn neg(expr: Expr) -> Self {
        Expr::Neg(Box::new(expr))
    }

    // TODO: Implement eval that recursively evaluates the expression.
    // Each variant:
    //   Num(val) => val
    //   Add(l, r) => l.eval() + r.eval()
    //   Sub(l, r) => l.eval() - r.eval()
    //   Mul(l, r) => l.eval() * r.eval()
    //   Div(l, r) => l.eval() / r.eval()
    //   Neg(e)    => -e.eval()
    //
    // fn eval(&self) -> f64 {
    //     todo!()
    // }

    // TODO: Implement count_nodes that returns the total number of nodes
    // (including leaves) in the expression tree.
    //   Num => 1
    //   Add/Sub/Mul/Div => 1 + left.count_nodes() + right.count_nodes()
    //   Neg => 1 + inner.count_nodes()
    //
    // fn count_nodes(&self) -> usize {
    //     todo!()
    // }

    // TODO: Implement depth that returns the depth of the expression tree.
    //   Num => 1
    //   Add/Sub/Mul/Div => 1 + max(left.depth(), right.depth())
    //   Neg => 1 + inner.depth()
    //
    // fn depth(&self) -> usize {
    //     todo!()
    // }
}

// TODO: Implement Display for Expr that produces fully parenthesized output.
//   Num(v) => just the number
//   Add(l, r) => "({l} + {r})"
//   Sub(l, r) => "({l} - {r})"
//   Mul(l, r) => "({l} * {r})"
//   Div(l, r) => "({l} / {r})"
//   Neg(e) => "(-{e})"
//
// impl fmt::Display for Expr {
//     fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
//         todo!()
//     }
// }

fn main() {
    // Expression: (3 + 4) * 2
    let expr = Expr::mul(
        Expr::add(Expr::num(3.0), Expr::num(4.0)),
        Expr::num(2.0),
    );
    println!("Expression: {expr}");        // ((3 + 4) * 2)
    println!("Result: {}", expr.eval());   // 14
    println!("Nodes: {}", expr.count_nodes()); // 5
    println!("Depth: {}", expr.depth());   // 3

    // Expression: -(10 / (2 + 3))
    let expr2 = Expr::neg(
        Expr::div(
            Expr::num(10.0),
            Expr::add(Expr::num(2.0), Expr::num(3.0)),
        ),
    );
    println!("\nExpression: {expr2}");      // (-(10 / (2 + 3)))
    println!("Result: {}", expr2.eval());  // -2

    // Expression: (1 + 2) + (3 + 4)
    let expr3 = Expr::add(
        Expr::add(Expr::num(1.0), Expr::num(2.0)),
        Expr::add(Expr::num(3.0), Expr::num(4.0)),
    );
    println!("\nExpression: {expr3}");
    println!("Result: {}", expr3.eval());  // 10
    println!("Nodes: {}", expr3.count_nodes()); // 7
    println!("Depth: {}", expr3.depth());  // 3

    // Clone and evaluate independently:
    let original = Expr::add(Expr::num(1.0), Expr::num(2.0));
    let cloned = original.clone();
    println!("\nOriginal: {} = {}", original, original.eval());
    println!("Cloned:   {} = {}", cloned, cloned.eval());
}
```

## Try It Yourself

1. **Doubly linked list**: Using `Rc<RefCell<Node>>` and `Weak<RefCell<Node>>` for the previous pointer, implement a doubly linked list with push_back and pop_back. This previews the combination of smart pointers that you will study in the advanced section.

2. **Delete from BST**: Add a `remove` method to the binary search tree. Handle all three cases: removing a leaf, a node with one child, and a node with two children (replace with the inorder successor).

3. **Expression simplification**: Add a `simplify` method to the `Expr` enum that applies basic algebraic rules: `x + 0 = x`, `x * 1 = x`, `x * 0 = 0`, `x - x = 0`. This requires pattern matching on the structure of the tree.

4. **Level-order traversal**: Implement breadth-first traversal for the BST using a `VecDeque` as a queue instead of recursion.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Forgetting `Box` in recursive type | `error[E0072]: recursive type has infinite size` | Wrap the recursive field in `Box<T>` |
| Using `Option<Node>` instead of `Option<Box<Node>>` | Same infinite size error | The `Option` does not add indirection; `Box` does |
| Calling `.take()` when you only need a reference | Moves the value out unexpectedly | Use `.as_ref()` or `.as_deref()` for references |
| Forgetting to dereference `Box` | Methods not found on `Box<Node>` | `Box<T>` auto-derefs to `T`, but for moves you need `*boxed_node` |
| Infinite recursion without base case | Stack overflow at runtime | Every recursive function must check for `None`/`Nil`/leaf |
| Fighting the borrow checker in tree mutations | Multiple mutable borrows | Restructure to take `&mut Option<Box<Node>>` at each level |

## Verification

Save each exercise as its own `.rs` file and run with `rustc`:

```bash
# Exercise 1: Linked list construction
rustc exercises/ex1_linked_list.rs && ./ex1_linked_list
# Expected: List [1 -> 2 -> 3], pop returns Some(1), then [2 -> 3]

# Exercise 2: Linked list iteration
rustc exercises/ex2_list_iterator.rs && ./ex2_list_iterator
# Expected: Iter/for-loop print 10 20 30, sum=60, doubled=[20,40,60]

# Exercise 3: BST insert/search
rustc exercises/ex3_bst_basic.rs && ./ex3_bst_basic
# Expected: Size=7, depth=3, contains 4=true, contains 9=false

# Exercise 4: BST traversals
rustc exercises/ex4_bst_traversals.rs && ./ex4_bst_traversals
# Expected: Inorder=[1,2,3,4,5,6,7,8], Preorder=[5,3,1,2,4,7,6,8]

# Exercise 5: Expression evaluator
rustc exercises/ex5_expr.rs && ./ex5_expr
# Expected: (3+4)*2=14, -(10/(2+3))=-2

# Verify that this does NOT compile (test your understanding):
# Create test_infinite.rs with:
#   enum Bad { Cons(i32, Bad) }
#   fn main() {}
# Run: rustc test_infinite.rs
# Expected: error[E0072]: recursive type `Bad` has infinite size
```

## Summary

Recursive data structures are types that refer to themselves. In Rust, every type must have a known size at compile time, so you cannot directly embed a type within itself. `Box<T>` provides heap indirection: a fixed-size pointer on the stack that points to the recursive data on the heap. The `Option<Box<Node>>` pattern is the standard way to express "this child may or may not exist" in trees and lists. With these building blocks you can implement linked lists, binary trees, expression trees, and any other recursive structure. The key to writing correct recursive algorithms is matching the shape of the data: if the type is recursive, the function that processes it will be recursive too, with base cases that correspond to `None` or leaf nodes.

## What You Learned

- Why the compiler rejects recursive types without indirection and how `Box` fixes the problem
- The `Option<Box<Node>>` idiom and null-pointer optimization
- Singly linked list implementation: push, pop, peek, iteration, and Display
- Binary search tree: insert, search, min/max, depth, and three traversal orders
- Recursive enums for expression trees with evaluation and structural analysis
- When to use `as_ref()` vs `take()` vs `as_deref()` when navigating recursive structures

## Resources

- [The Rust Book: Using Box to Point to Data on the Heap](https://doc.rust-lang.org/book/ch15-01-box.html)
- [The Rust Book: Cons List](https://doc.rust-lang.org/book/ch15-01-box.html#using-boxt-to-point-to-data-on-the-heap)
- [Rust by Example: Box, stack and heap](https://doc.rust-lang.org/rust-by-example/std/box.html)
- [Too Many Linked Lists](https://rust-unofficial.github.io/too-many-lists/) -- the definitive guide to linked lists in Rust
- [std::boxed::Box documentation](https://doc.rust-lang.org/std/boxed/struct.Box.html)
