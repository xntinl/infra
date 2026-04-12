# Custom `Enumerable` — a binary tree with DFS `reduce/3`

**Project**: `tree_enum` — a binary tree struct implementing `Enumerable.reduce/3` for depth-first traversal, with full support for `:halt` and `:suspend`.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Exercise 34 implemented `Enumerable` for a bag by materializing into a list
and delegating to the standard list reducer. That's a shortcut; the real
contract of `reduce/3` is a continuation-passing state machine that supports
streaming, early termination, and suspension. For a tree, where a list
representation would destroy the whole point, you have to implement
`reduce/3` properly.

This exercise builds a binary tree and its DFS `reduce/3` from scratch.
You'll internalize:

- Why `reduce/3` is a recursion over TWO structures (the data AND the
  accumulator state).
- How `:suspend` turns your recursion into a resumable continuation.
- How to keep the return shapes (`:done` / `:halted` / `:suspended`) aligned
  with what `Enum` expects.

Project structure:

```
tree_enum/
├── lib/
│   └── tree.ex
├── test/
│   └── tree_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Enumerable.reduce/3` is a state machine

The return is one of:

```
{:done, acc}                           # finished the enumerable
{:halted, acc}                         # caller signalled :halt
{:suspended, acc, continuation}        # caller signalled :suspend
```

The accumulator comes in as `{:cont, acc}`, `{:halt, acc}`, or
`{:suspend, acc}`. Your job: dispatch on both the structural state (tree
node or empty) AND the accumulator state.

### 2. Pre-order DFS as CPS

For a tree `{value, left, right}`:

1. Emit `value`.
2. Recurse left.
3. Recurse right.

Between each step, you must check the accumulator state. If the reducer
returned `:halt`, stop. If it returned `:suspend`, return a continuation
that resumes where you left off.

### 3. Suspension requires a CAPTURED closure

When you return `{:suspended, acc, cont}`, `cont` is a function that takes
a fresh accumulator and resumes the traversal. You can't "save progress
into a mutable field" — the continuation itself IS the progress. Each
recursive call becomes a closure parameter.

### 4. Why bother with suspend?

`Stream.zip/2` and `Enum.zip/2` against another stream need to interleave
two enumerables one step at a time. That's impossible if your `reduce/3`
runs to completion unless you implement suspension. Skipping `:suspend`
silently breaks interop with lazy streams.

---

## Implementation

### Step 1: Create the project

```bash
mix new tree_enum
cd tree_enum
```

### Step 2: `lib/tree.ex`

```elixir
defmodule Tree do
  @moduledoc """
  Minimal binary tree with pre-order DFS iteration via `Enumerable`.
  Fully supports `:cont`, `:halt`, and `:suspend`.
  """

  defstruct value: nil, left: nil, right: nil

  @type t :: %__MODULE__{value: term(), left: t | nil, right: t | nil}

  @doc "Leaf node (no children)."
  @spec leaf(term()) :: t
  def leaf(value), do: %__MODULE__{value: value}

  @doc "Node with explicit left/right children (either can be nil)."
  @spec node(term(), t | nil, t | nil) :: t
  def node(value, left, right), do: %__MODULE__{value: value, left: left, right: right}
end

defimpl Enumerable, for: Tree do
  # We don't cheaply know size or membership without traversal.
  # Returning {:error, __MODULE__} delegates to reduce/3.
  def count(_tree), do: {:error, __MODULE__}
  def member?(_tree, _element), do: {:error, __MODULE__}
  def slice(_tree), do: {:error, __MODULE__}

  # ── reduce/3 entry point ────────────────────────────────────────────────
  #
  # We convert the tree walk into walking a list of "pending" sub-trees,
  # pre-order. The pending list is what a continuation needs to carry.
  def reduce(%Tree{} = tree, acc, fun) do
    do_reduce([tree], acc, fun)
  end

  # ── accumulator-state dispatch (the three required clauses) ─────────────
  defp do_reduce(_pending, {:halt, acc}, _fun), do: {:halted, acc}

  defp do_reduce(pending, {:suspend, acc}, fun) do
    # Captured continuation: when the caller resumes with a fresh acc, we
    # pick up with the same pending list and reducer.
    {:suspended, acc, &do_reduce(pending, &1, fun)}
  end

  # Empty pending with :cont — traversal complete.
  defp do_reduce([], {:cont, acc}, _fun), do: {:done, acc}

  # Nil branches are "no tree" — skip them.
  defp do_reduce([nil | rest], {:cont, _} = acc, fun) do
    do_reduce(rest, acc, fun)
  end

  # Core step: emit current node's value, then schedule left then right
  # (the list acts as a stack — prepending left then right means left runs
  # first, which is pre-order DFS).
  defp do_reduce([%Tree{value: v, left: l, right: r} | rest], {:cont, acc}, fun) do
    case fun.(v, acc) do
      {:halt, new_acc} ->
        do_reduce(rest, {:halt, new_acc}, fun)

      {:suspend, new_acc} ->
        do_reduce([l, r | rest], {:suspend, new_acc}, fun)

      {:cont, new_acc} ->
        do_reduce([l, r | rest], {:cont, new_acc}, fun)
    end
  end
end
```

### Step 3: `test/tree_test.exs`

```elixir
defmodule TreeTest do
  use ExUnit.Case, async: true

  #       1
  #      / \
  #     2   3
  #    / \
  #   4   5
  setup do
    tree =
      Tree.node(
        1,
        Tree.node(2, Tree.leaf(4), Tree.leaf(5)),
        Tree.leaf(3)
      )

    %{tree: tree}
  end

  describe "pre-order DFS via Enum" do
    test "Enum.to_list yields values in pre-order", %{tree: tree} do
      assert Enum.to_list(tree) == [1, 2, 4, 5, 3]
    end

    test "Enum.sum over the tree", %{tree: tree} do
      assert Enum.sum(tree) == 15
    end

    test "Enum.map/2 over the tree", %{tree: tree} do
      assert Enum.map(tree, &(&1 * 10)) == [10, 20, 40, 50, 30]
    end
  end

  describe "halt propagation (early termination)" do
    test "Enum.take/2 halts after N elements", %{tree: tree} do
      # If :halt were ignored, this would traverse the whole tree.
      assert Enum.take(tree, 3) == [1, 2, 4]
    end

    test "Enum.find/2 stops at the first match", %{tree: tree} do
      assert Enum.find(tree, &(&1 == 4)) == 4
    end
  end

  describe "suspend propagation (stream interop)" do
    test "Stream.zip/2 interleaves tree and list", %{tree: tree} do
      # zip requires :suspend to be honored — the two sides advance lock-step.
      zipped = Stream.zip(tree, [:a, :b, :c]) |> Enum.to_list()
      assert zipped == [{1, :a}, {2, :b}, {4, :c}]
    end
  end

  describe "edge cases" do
    test "single-leaf tree" do
      assert Enum.to_list(Tree.leaf(42)) == [42]
    end

    test "tree with only left children" do
      linear = Tree.node(1, Tree.node(2, Tree.leaf(3), nil), nil)
      assert Enum.to_list(linear) == [1, 2, 3]
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Using a list as a pending-stack is the simplest shape**
It's tempting to recurse directly and handle `:suspend` with continuations
by hand. That gets confusing fast. Representing the traversal as "pending
list + cont/halt/suspend" reduces it to three clauses and keeps the code
auditable.

**2. Order of scheduling determines traversal kind**
`[l, r | rest]` gives pre-order DFS. Swap to `[r, l | rest]` for reverse
pre-order. To get in-order DFS you must emit the value BETWEEN scheduling
left and right, which means splitting the node into a "visit" step (emit
the value) distinct from an "expand" step. That's a good follow-up
exercise.

**3. Deep trees can blow the stack / build huge pending lists**
BEAM stacks are per-process and generous, but a pathological linear tree
has O(N) pending list depth. For very large trees, consider a truly lazy
representation (produce each node via `Stream.resource/3`) instead of
implementing `Enumerable` directly.

**4. Opting out of `count/1` is important**
If you claim O(1) count, `Enum.count/1` trusts you and skips the
traversal. Returning `{:error, __MODULE__}` makes `Enum.count/1` fall
back to `reduce/3`, which is correct but O(N) — an honest answer.

**5. When NOT to implement Enumerable for a tree**
If you need multiple traversal orders (BFS, in-order, post-order), expose
them as named functions (`Tree.bfs/1`, `Tree.in_order/1`) returning lists
or streams. `Enumerable` can only represent ONE order per type, and
picking one arbitrarily misleads callers.

---

## Resources

- [`Enumerable` — Elixir stdlib](https://hexdocs.pm/elixir/Enumerable.html)
- [`Stream.resource/3`](https://hexdocs.pm/elixir/Stream.html#resource/3) — the canonical way to build a lazy enumerable from scratch
- ["Continuations in Elixir" — ElixirForum thread](https://elixirforum.com/)
- [Erlang `:queue`](https://www.erlang.org/doc/man/queue.html) — if you need O(1) enqueue/dequeue for BFS
