# Recursion and Tail Call Optimization: Building a File System Tree Walker

**Project**: `tree` — a recursive directory lister with depth limits, filtering, and size calculation

---

## Why recursion replaces loops in Elixir

Elixir has no `for` loops, no `while` loops, no mutable loop counters. All iteration
is either recursion or higher-order functions (`Enum.map/2`, `Enum.reduce/3`) which
are built on recursion internally.

For a senior developer from Java or Go, this seems limiting. It is not — it is a
guarantee. Without mutable state, every recursive function is:

1. **Predictable**: the only inputs are the function arguments
2. **Concurrent-safe**: no shared mutable counter to synchronize
3. **Testable**: call with inputs, check outputs

The catch: naive recursion can overflow the stack. Elixir solves this with Tail Call
Optimization (TCO). When the recursive call is the **last** operation in the function
(not wrapped in another operation), the BEAM reuses the current stack frame. This
turns recursion into a constant-space loop.

```elixir
# NOT tail-recursive — multiplication happens AFTER the recursive call
def factorial(0), do: 1
def factorial(n), do: n * factorial(n - 1)  # stack grows with each call

# Tail-recursive — recursive call IS the last operation
def factorial(n), do: factorial(n, 1)
defp factorial(0, acc), do: acc
defp factorial(n, acc), do: factorial(n - 1, n * acc)  # constant stack
```

---

## The business problem

Build a file system tree walker that:

1. Recursively lists directory contents
2. Respects a configurable depth limit
3. Filters files by extension
4. Calculates total size of matching files
5. Uses tail-recursive accumulation for large directory trees

---

## Project structure

```
tree/
├── lib/
│   └── tree.ex
├── test/
│   └── tree_test.exs
└── mix.exs
```

---

## Implementation

### `lib/tree.ex`

```elixir
defmodule Tree do
  @moduledoc """
  Recursive file system tree walker.

  Demonstrates tail recursion with accumulators, depth-limited
  recursion, and the difference between body recursion and
  tail recursion for stack safety.
  """

  @type entry :: %{
          path: String.t(),
          name: String.t(),
          type: :file | :directory,
          size: non_neg_integer(),
          depth: non_neg_integer()
        }

  @doc """
  Lists all files and directories under the given path recursively.

  Options:
    - `:max_depth` — maximum recursion depth (default: infinity)
    - `:extensions` — list of file extensions to include, e.g. `[".ex", ".exs"]`
    - `:include_dirs` — whether to include directories in output (default: true)

  ## Examples

      iex> entries = Tree.list("/tmp/tree_test_dir", max_depth: 1)
      iex> is_list(entries)
      true

  """
  @spec list(String.t(), keyword()) :: [entry()]
  def list(root_path, opts \\ []) do
    max_depth = Keyword.get(opts, :max_depth, :infinity)
    extensions = Keyword.get(opts, :extensions, nil)
    include_dirs = Keyword.get(opts, :include_dirs, true)

    walk([{root_path, 0}], [], max_depth, extensions, include_dirs)
  end

  @doc """
  Calculates the total size of all files matching the given criteria.

  Uses a tail-recursive accumulator to avoid stack overflow on
  deeply nested directories with millions of files.

  ## Examples

      iex> Tree.total_size("/tmp/tree_test_dir")
      size when is_integer(size) and size >= 0

  """
  @spec total_size(String.t(), keyword()) :: non_neg_integer()
  def total_size(root_path, opts \\ []) do
    root_path
    |> list(Keyword.put(opts, :include_dirs, false))
    |> sum_sizes(0)
  end

  @doc """
  Formats a tree listing as an indented string, similar to the Unix `tree` command.

  ## Examples

      iex> entries = [
      ...>   %{path: "/a", name: "a", type: :directory, size: 0, depth: 0},
      ...>   %{path: "/a/b.ex", name: "b.ex", type: :file, size: 100, depth: 1}
      ...> ]
      iex> Tree.format(entries)
      "a/\\n  b.ex (100 B)"

  """
  @spec format([entry()]) :: String.t()
  def format(entries) do
    entries
    |> Enum.map(&format_entry/1)
    |> Enum.join("\n")
  end

  @doc """
  Counts files grouped by extension.

  Returns a map of extension => count.

  ## Examples

      iex> entries = [
      ...>   %{path: "/a.ex", name: "a.ex", type: :file, size: 0, depth: 0},
      ...>   %{path: "/b.ex", name: "b.ex", type: :file, size: 0, depth: 0},
      ...>   %{path: "/c.md", name: "c.md", type: :file, size: 0, depth: 0}
      ...> ]
      iex> Tree.count_by_extension(entries)
      %{".ex" => 2, ".md" => 1}

  """
  @spec count_by_extension([entry()]) :: %{String.t() => non_neg_integer()}
  def count_by_extension(entries) do
    entries
    |> Enum.filter(&(&1.type == :file))
    |> Enum.group_by(&Path.extname(&1.name))
    |> Enum.map(fn {ext, files} -> {ext, length(files)} end)
    |> Map.new()
  end

  # --- Private: tail-recursive tree walker ---

  # The work list pattern: instead of recursive function calls that build up
  # the stack, we maintain an explicit work list (stack) of paths to visit.
  # This is tail-recursive because `walk` is the last call in every branch.

  @spec walk(
          [{String.t(), non_neg_integer()}],
          [entry()],
          non_neg_integer() | :infinity,
          [String.t()] | nil,
          boolean()
        ) :: [entry()]
  defp walk([], acc, _max_depth, _extensions, _include_dirs) do
    Enum.reverse(acc)
  end

  defp walk([{path, depth} | rest], acc, max_depth, extensions, include_dirs) do
    case File.stat(path) do
      {:ok, %File.Stat{type: :directory, size: size}} ->
        entry = %{
          path: path,
          name: Path.basename(path),
          type: :directory,
          size: size,
          depth: depth
        }

        new_acc = if include_dirs, do: [entry | acc], else: acc

        children =
          if depth_allowed?(depth, max_depth) do
            list_children(path, depth + 1)
          else
            []
          end

        walk(children ++ rest, new_acc, max_depth, extensions, include_dirs)

      {:ok, %File.Stat{type: :regular, size: size}} ->
        name = Path.basename(path)

        if extension_matches?(name, extensions) do
          entry = %{
            path: path,
            name: name,
            type: :file,
            size: size,
            depth: depth
          }

          walk(rest, [entry | acc], max_depth, extensions, include_dirs)
        else
          walk(rest, acc, max_depth, extensions, include_dirs)
        end

      _ ->
        walk(rest, acc, max_depth, extensions, include_dirs)
    end
  end

  @spec depth_allowed?(non_neg_integer(), non_neg_integer() | :infinity) :: boolean()
  defp depth_allowed?(_depth, :infinity), do: true
  defp depth_allowed?(depth, max_depth), do: depth < max_depth

  @spec list_children(String.t(), non_neg_integer()) ::
          [{String.t(), non_neg_integer()}]
  defp list_children(dir_path, child_depth) do
    case File.ls(dir_path) do
      {:ok, names} ->
        names
        |> Enum.sort()
        |> Enum.map(fn name -> {Path.join(dir_path, name), child_depth} end)

      {:error, _} ->
        []
    end
  end

  @spec extension_matches?(String.t(), [String.t()] | nil) :: boolean()
  defp extension_matches?(_name, nil), do: true

  defp extension_matches?(name, extensions) do
    Path.extname(name) in extensions
  end

  # Tail-recursive size accumulator
  @spec sum_sizes([entry()], non_neg_integer()) :: non_neg_integer()
  defp sum_sizes([], acc), do: acc
  defp sum_sizes([%{size: size} | rest], acc), do: sum_sizes(rest, acc + size)

  @spec format_entry(entry()) :: String.t()
  defp format_entry(%{name: name, type: :directory, depth: depth}) do
    indent = String.duplicate("  ", depth)
    "#{indent}#{name}/"
  end

  defp format_entry(%{name: name, type: :file, size: size, depth: depth}) do
    indent = String.duplicate("  ", depth)
    "#{indent}#{name} (#{format_size(size)})"
  end

  @spec format_size(non_neg_integer()) :: String.t()
  defp format_size(bytes) when bytes < 1024, do: "#{bytes} B"
  defp format_size(bytes) when bytes < 1_048_576, do: "#{div(bytes, 1024)} KB"
  defp format_size(bytes), do: "#{div(bytes, 1_048_576)} MB"
end
```

**Why this works:**

- `walk/5` uses the **work list pattern** instead of direct recursion. The first
  argument is a list of `{path, depth}` tuples to process. When we encounter a
  directory, we prepend its children to the work list instead of making a recursive
  call. This keeps the recursion tail-recursive because `walk` is always the last
  call.
- `sum_sizes/2` is a classic tail-recursive accumulator. The sum is passed as the
  second argument, updated with each element. No stack frames accumulate.
- `File.stat/1` returns `{:ok, %File.Stat{}}` or `{:error, reason}`. We pattern
  match on the `type` field (`:directory`, `:regular`) to decide how to handle
  each path. Errors (permission denied, broken symlinks) are silently skipped.

### Tests

```elixir
# test/tree_test.exs
defmodule TreeTest do
  use ExUnit.Case, async: true

  @test_dir Path.join(System.tmp_dir!(), "tree_test_#{:erlang.unique_integer([:positive])}")

  setup_all do
    File.rm_rf!(@test_dir)
    File.mkdir_p!(Path.join(@test_dir, "src/lib"))
    File.mkdir_p!(Path.join(@test_dir, "test"))
    File.write!(Path.join(@test_dir, "mix.exs"), "# mix file")
    File.write!(Path.join(@test_dir, "src/app.ex"), String.duplicate("x", 100))
    File.write!(Path.join(@test_dir, "src/lib/helper.ex"), String.duplicate("y", 50))
    File.write!(Path.join(@test_dir, "src/lib/utils.ex"), String.duplicate("z", 75))
    File.write!(Path.join(@test_dir, "test/app_test.exs"), String.duplicate("t", 200))
    File.write!(Path.join(@test_dir, "README.md"), "# Readme")

    on_exit(fn -> File.rm_rf!(@test_dir) end)
    :ok
  end

  describe "list/2" do
    test "lists all files and directories" do
      entries = Tree.list(@test_dir)
      names = Enum.map(entries, & &1.name)
      assert "src" in names
      assert "test" in names
      assert "mix.exs" in names
      assert "app.ex" in names
    end

    test "respects max_depth" do
      entries = Tree.list(@test_dir, max_depth: 1)
      depths = Enum.map(entries, & &1.depth)
      assert Enum.all?(depths, &(&1 <= 1))
      names = Enum.map(entries, & &1.name)
      refute "helper.ex" in names
    end

    test "filters by extension" do
      entries = Tree.list(@test_dir, extensions: [".ex"])
      names = Enum.map(entries, & &1.name)
      assert "app.ex" in names
      assert "helper.ex" in names
      refute "app_test.exs" in names
      refute "README.md" in names
    end

    test "excludes directories when include_dirs: false" do
      entries = Tree.list(@test_dir, include_dirs: false)
      types = Enum.map(entries, & &1.type)
      assert Enum.all?(types, &(&1 == :file))
    end

    test "handles non-existent directory" do
      entries = Tree.list("/tmp/definitely_does_not_exist_#{:rand.uniform(999999)}")
      assert entries == []
    end
  end

  describe "total_size/2" do
    test "sums file sizes" do
      size = Tree.total_size(@test_dir)
      # mix.exs(10) + app.ex(100) + helper.ex(50) + utils.ex(75) + app_test.exs(200) + README.md(8)
      assert size > 0
    end

    test "filters by extension in size calculation" do
      size_ex = Tree.total_size(@test_dir, extensions: [".ex"])
      size_all = Tree.total_size(@test_dir)
      assert size_ex < size_all
      assert size_ex > 0
    end
  end

  describe "format/1" do
    test "indents by depth" do
      entries = [
        %{path: "/a", name: "a", type: :directory, size: 0, depth: 0},
        %{path: "/a/b.ex", name: "b.ex", type: :file, size: 100, depth: 1},
        %{path: "/a/c/d.ex", name: "d.ex", type: :file, size: 50, depth: 2}
      ]

      output = Tree.format(entries)
      lines = String.split(output, "\n")
      assert Enum.at(lines, 0) == "a/"
      assert Enum.at(lines, 1) == "  b.ex (100 B)"
      assert Enum.at(lines, 2) == "    d.ex (50 B)"
    end
  end

  describe "count_by_extension/1" do
    test "groups files by extension" do
      entries = Tree.list(@test_dir, include_dirs: false)
      counts = Tree.count_by_extension(entries)
      assert counts[".ex"] >= 3
      assert counts[".exs"] >= 1
      assert counts[".md"] >= 1
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## Tail recursion vs body recursion

```elixir
# Body recursion — operation AFTER recursive call
# Stack: [sum(4), sum(3), sum(2), sum(1), sum(0)] — grows with input
def sum([]), do: 0
def sum([head | tail]), do: head + sum(tail)

# Tail recursion — recursive call IS the last operation
# Stack: constant size, reuses the same frame
def sum(list), do: sum(list, 0)
defp sum([], acc), do: acc
defp sum([head | tail], acc), do: sum(tail, head + acc)
```

The BEAM detects when the recursive call is in tail position and reuses the
current stack frame. This means tail-recursive functions run in constant space,
no matter the input size. Body-recursive functions grow the stack linearly.

For small inputs (<10,000 elements), the difference is negligible. For large
inputs (millions of files in a directory tree), body recursion overflows the stack.

---

## Common production mistakes

**1. Thinking `Enum` functions are "not recursive"**
`Enum.map/2`, `Enum.reduce/3`, etc. are all built on recursion internally. They
are tail-recursive and handle the accumulator for you. Prefer `Enum` functions
over hand-written recursion unless you need work-list patterns or multi-value
accumulation.

**2. Forgetting to reverse the accumulator**
Prepending to a list (`[element | acc]`) builds it in reverse. Always reverse at
the end: `Enum.reverse(acc)`. This is O(n) total, which is optimal.

**3. Using `++` to "append" in recursive functions**
`list ++ [element]` is O(n) per call. In a recursive function over n elements,
that is O(n^2) total. Always prepend and reverse.

**4. Not handling the empty case**
Every recursive function needs a base case. For lists, it is the empty list `[]`.
For numbers, it is usually 0 or 1. Forgetting the base case causes infinite recursion.

**5. Assuming TCO works through `try/rescue`**
`try` blocks break tail call optimization. If you wrap a recursive call in `try`,
the BEAM cannot reuse the stack frame. Move error handling outside the recursive
loop.

---

## Resources

- [Recursion — Elixir Getting Started](https://elixir-lang.org/getting-started/recursion.html)
- [Tail calls — Erlang efficiency guide](https://www.erlang.org/doc/efficiency_guide/functions.html)
- [File — HexDocs](https://hexdocs.pm/elixir/File.html)
- [Path — HexDocs](https://hexdocs.pm/elixir/Path.html)
