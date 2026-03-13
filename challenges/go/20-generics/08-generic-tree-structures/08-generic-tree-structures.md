# 8. Generic Tree Structures

<!--
difficulty: advanced
concepts: [binary-search-tree, recursive-generics, cmp-ordered, tree-traversal, generic-methods]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [type-parameters, generic-data-structures, comparable-and-ordered, pointers]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Type Inference and Constraint Inference](../07-type-inference-and-constraint-inference/07-type-inference-and-constraint-inference.md)
- Familiarity with pointers and recursive data structures

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a generic binary search tree with `cmp.Ordered` constraint
- **Implement** recursive insert, search, and traversal methods on generic tree nodes
- **Apply** in-order, pre-order, and level-order traversals to generic trees

## Why Generic Tree Structures

Trees are a core data structure but were painful to implement generically in Go before 1.18. You either duplicated the entire implementation for each type or used `interface{}` with runtime type assertions on every comparison. A single misplaced assertion would panic at runtime.

Generic trees solve both problems. A `BST[int]` is type-safe at compile time, and the same code works for `BST[string]` or any ordered type. The `cmp.Ordered` constraint ensures that comparison operators work, so the compiler catches mistakes rather than your production server.

## The Problem

Build a generic binary search tree (BST) that supports insert, search, in-order traversal, min/max, and delete. The tree must work with any `cmp.Ordered` type.

### Requirements

1. Define a `Node[T cmp.Ordered]` struct with `Value T`, `Left *Node[T]`, `Right *Node[T]`
2. Define a `BST[T cmp.Ordered]` struct that holds the root
3. Implement `Insert(value T)` -- standard BST insertion
4. Implement `Contains(value T) bool` -- search for a value
5. Implement `InOrder() []T` -- return values in sorted order
6. Implement `Min() (T, bool)` and `Max() (T, bool)`
7. Implement `Delete(value T) bool` -- remove a value, return whether it existed
8. Write a main function that exercises the tree with both `int` and `string` types

### Hints

<details>
<summary>Hint 1: Node and BST types</summary>

```go
type Node[T cmp.Ordered] struct {
    Value T
    Left  *Node[T]
    Right *Node[T]
}

type BST[T cmp.Ordered] struct {
    root *Node[T]
    size int
}
```
</details>

<details>
<summary>Hint 2: Recursive insert</summary>

```go
func (b *BST[T]) Insert(value T) {
    b.root = b.insert(b.root, value)
    b.size++
}

func (b *BST[T]) insert(node *Node[T], value T) *Node[T] {
    if node == nil {
        return &Node[T]{Value: value}
    }
    if value < node.Value {
        node.Left = b.insert(node.Left, value)
    } else if value > node.Value {
        node.Right = b.insert(node.Right, value)
    }
    // duplicate: do nothing (or b.size-- to correct the count)
    return node
}
```
</details>

<details>
<summary>Hint 3: In-order traversal</summary>

```go
func (b *BST[T]) InOrder() []T {
    var result []T
    b.inOrder(b.root, &result)
    return result
}

func (b *BST[T]) inOrder(node *Node[T], result *[]T) {
    if node == nil {
        return
    }
    b.inOrder(node.Left, result)
    *result = append(*result, node.Value)
    b.inOrder(node.Right, result)
}
```
</details>

<details>
<summary>Hint 4: Delete with three cases</summary>

The three cases for BST deletion:
1. Leaf node: remove it
2. One child: replace node with its child
3. Two children: replace value with in-order successor (min of right subtree), then delete the successor

```go
func (b *BST[T]) delete(node *Node[T], value T) (*Node[T], bool) {
    if node == nil {
        return nil, false
    }
    var found bool
    if value < node.Value {
        node.Left, found = b.delete(node.Left, value)
    } else if value > node.Value {
        node.Right, found = b.delete(node.Right, value)
    } else {
        found = true
        if node.Left == nil {
            return node.Right, true
        }
        if node.Right == nil {
            return node.Left, true
        }
        successor := b.minNode(node.Right)
        node.Value = successor.Value
        node.Right, _ = b.delete(node.Right, successor.Value)
    }
    return node, found
}
```
</details>

## Verification

Your program should produce output similar to:

```
--- Integer BST ---
In-order: [1 3 5 7 9 11 13]
Contains 7: true
Contains 8: false
Min: 1
Max: 13
Size: 7

After deleting 7:
In-order: [1 3 5 9 11 13]
Contains 7: false
Size: 6

--- String BST ---
In-order: [alice bob charlie dave eve]
Contains "charlie": true
Min: alice
Max: eve
```

```bash
go run main.go
```

## What's Next

Continue to [09 - Generic Iterator Patterns](../09-generic-iterator-patterns/09-generic-iterator-patterns.md) to explore iterator patterns with generics.

## Summary

- Generic BSTs use `cmp.Ordered` to enable comparison operators on the value type
- Recursive methods on generic types follow the same pattern as non-generic trees
- The `*Node[T]` pointer type enables recursive tree structures
- Delete requires handling three cases: leaf, one child, two children
- The same implementation works for `int`, `string`, `float64`, or any custom ordered type

## Reference

- [cmp.Ordered](https://pkg.go.dev/cmp#Ordered)
- [Go spec: Comparison operators](https://go.dev/ref/spec#Comparison_operators)
- [Binary Search Tree (Wikipedia)](https://en.wikipedia.org/wiki/Binary_search_tree)
