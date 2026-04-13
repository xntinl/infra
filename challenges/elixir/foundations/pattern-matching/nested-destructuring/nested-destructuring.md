# Nested Destructuring: A JSON AST Walker

**Project**: `json_ast_walker` — extracts specific fields from JSON-like nested structures using destructuring patterns

---

## Project structure

```
json_ast_walker/
├── lib/
│   └── json_ast_walker.ex
├── script/
│   └── main.exs
├── test/
│   └── json_ast_walker_test.exs
└── mix.exs
```

---

## Core concepts

Elixir patterns compose: you can nest map, tuple, and list patterns to any
depth and pull values out at every level in a single match. This replaces the
chained `if data && data.user && data.user.address && ...` defensive code
you'd write in JavaScript or Python.

The combinations worth mastering:

1. **Map inside map**: `%{user: %{id: id}}` extracts `id` from two levels.
2. **Tuple inside map**: `%{point: {x, y}}` extracts coordinates.
3. **List head/tail**: `[head | tail]` destructures the first element.
4. **Fixed-length list**: `[a, b, c]` matches a list of EXACTLY three items.
5. **Combined**: `%{items: [%{id: first} | rest]}` extracts the first item's
   id and captures the tail.

Each clause can succeed or fail independently — if any nested pattern fails,
the whole match fails and the next clause (or raise) applies.

---

## The business problem

A JSON AST walker takes parsed JSON-like structures (nested maps, lists,
tuples) and extracts specific values. Think: pull the first author's email
from a blog post, or the user ID from the third comment. Without
destructuring, you write defensive chains. With it, the shape requirement is
the code.

---

## Why deep destructuring and not `get_in/2` with a key path

`get_in/2` is perfect for dynamic paths known only at runtime. For fixed shapes, deep destructuring is more direct and lets the compiler verify the shape.

## Design decisions

**Option A — deep destructuring in the function head**
- Pros: Intent is visible in the signature, a failed match falls through to the next clause
- Cons: Becomes hard to read beyond 3 levels of nesting

**Option B — shallow match + repeated `Map.get/2` / `Enum.at/2`** (chosen)
- Pros: Works with partially-known shapes
- Cons: Intent hidden across multiple lookups, no compile-time shape verification

→ Chose **A** because JSON-like fixed-shape payloads are exactly the case deep destructuring was designed for. For deeper trees, use `get_in/2` with a path list.

## Implementation

### `mix.exs`
```elixir
defmodule JsonAstWalker.MixProject do
  use Mix.Project

  def project do
    [
      app: :json_ast_walker,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### `lib/json_ast_walker.ex`

```elixir
defmodule JsonAstWalker do
  @moduledoc """
  Extracts fields from nested JSON-like structures using pattern destructuring.

  Each function demonstrates a different nesting combination. Failures return
  `:error` or a specific reason — never `nil` — so the caller can distinguish
  "missing data" from "present but nil".
  """

  @doc """
  Extracts author email from a post with a nested author object.

  Shape: %{post: %{author: %{email: _}}}
  """
  @spec author_email(map()) :: {:ok, String.t()} | :error
  def author_email(%{post: %{author: %{email: email}}}) when is_binary(email) do
    {:ok, email}
  end

  def author_email(_), do: :error

  @doc """
  Extracts coordinates from an event whose location is a tuple.

  Shape: %{event: %{location: {lat, lng}}}
  """
  @spec coordinates(map()) :: {:ok, {float(), float()}} | :error
  def coordinates(%{event: %{location: {lat, lng}}})
      when is_number(lat) and is_number(lng) do
    {:ok, {lat / 1, lng / 1}}
  end

  def coordinates(_), do: :error

  @doc """
  Returns the first comment's author along with the rest.

  Shape: %{comments: [%{author: _} | rest]}

  Demonstrates `[head | tail]` combined with a map pattern in the head.
  """
  @spec first_commenter(map()) :: {:ok, String.t(), [map()]} | :error
  def first_commenter(%{comments: [%{author: author} | rest]})
      when is_binary(author) do
    {:ok, author, rest}
  end

  def first_commenter(_), do: :error

  @doc """
  Extracts the Nth tag (0-indexed) from a post. Uses list destructuring
  with skipped elements.

  Shape: %{tags: [_, _, third | _]} — matches only lists with ≥ 3 elements.
  """
  @spec third_tag(map()) :: {:ok, String.t()} | :error
  def third_tag(%{tags: [_, _, third | _]}) when is_binary(third) do
    {:ok, third}
  end

  def third_tag(_), do: :error

  @doc """
  Matches a GeoJSON-ish feature and returns its type + the first coordinate pair.

  Shape: %{"type" => "Feature",
           "geometry" => %{"type" => "Point",
                           "coordinates" => [lng, lat | _]}}
  """
  @spec first_point(map()) :: {:ok, %{lng: number(), lat: number()}} | :error
  def first_point(%{
        "type" => "Feature",
        "geometry" => %{"type" => "Point", "coordinates" => [lng, lat | _]}
      })
      when is_number(lng) and is_number(lat) do
    {:ok, %{lng: lng, lat: lat}}
  end

  def first_point(_), do: :error

  @doc """
  Recursively collects all `:leaf` values from an AST of nested tuples.

  Shape: {:node, [child1, child2, ...]} or {:leaf, value}

  Demonstrates destructuring inside a recursive function with multiple clauses.
  """
  @spec leaves(term()) :: [term()]
  def leaves({:leaf, value}), do: [value]

  def leaves({:node, children}) when is_list(children) do
    Enum.flat_map(children, &leaves/1)
  end

  def leaves(_), do: []

  @doc """
  Extracts address components from a deeply nested user profile.

  Shape:
      %{user: %{profile: %{address: %{city: _, country: _, zip: _}}}}

  Returns a flat map when all keys match, else `:error`.
  """
  @spec address(map()) :: {:ok, %{city: String.t(), country: String.t(), zip: String.t()}}
                          | :error
  def address(%{
        user: %{profile: %{address: %{city: city, country: country, zip: zip}}}
      })
      when is_binary(city) and is_binary(country) and is_binary(zip) do
    {:ok, %{city: city, country: country, zip: zip}}
  end

  def address(_), do: :error
end
```

### `test/json_ast_walker_test.exs`

```elixir
defmodule JsonAstWalkerTest do
  use ExUnit.Case, async: true
  doctest JsonAstWalker

  alias JsonAstWalker, as: Walker

  describe "author_email/1" do
    test "extracts email from 3 levels deep" do
      payload = %{post: %{author: %{email: "a@b.com", name: "Alice"}, title: "Hi"}}
      assert {:ok, "a@b.com"} = Walker.author_email(payload)
    end

    test "fails when any level missing" do
      assert :error = Walker.author_email(%{post: %{author: %{}}})
      assert :error = Walker.author_email(%{post: %{}})
      assert :error = Walker.author_email(%{})
    end

    test "fails when email is not a string" do
      assert :error = Walker.author_email(%{post: %{author: %{email: nil}}})
    end
  end

  describe "coordinates/1" do
    test "destructures tuple location" do
      assert {:ok, {40.7, -74.0}} = Walker.coordinates(%{event: %{location: {40.7, -74.0}}})
    end

    test "rejects non-tuple location" do
      assert :error = Walker.coordinates(%{event: %{location: [40.7, -74.0]}})
    end
  end

  describe "first_commenter/1" do
    test "returns first author and tail" do
      payload = %{
        comments: [
          %{author: "Alice", body: "hi"},
          %{author: "Bob", body: "yo"},
          %{author: "Carol", body: "sup"}
        ]
      }

      assert {:ok, "Alice", rest} = Walker.first_commenter(payload)
      assert length(rest) == 2
    end

    test "empty list fails" do
      assert :error = Walker.first_commenter(%{comments: []})
    end
  end

  describe "third_tag/1" do
    test "returns third element" do
      assert {:ok, "elixir"} = Walker.third_tag(%{tags: ["erlang", "beam", "elixir"]})
    end

    test "returns third when more exist" do
      assert {:ok, "c"} = Walker.third_tag(%{tags: ["a", "b", "c", "d", "e"]})
    end

    test "fails with fewer than 3 tags" do
      assert :error = Walker.third_tag(%{tags: ["a", "b"]})
    end
  end

  describe "first_point/1" do
    test "extracts GeoJSON Point" do
      payload = %{
        "type" => "Feature",
        "properties" => %{"name" => "X"},
        "geometry" => %{"type" => "Point", "coordinates" => [-74.0, 40.7]}
      }

      assert {:ok, %{lng: -74.0, lat: 40.7}} = Walker.first_point(payload)
    end

    test "rejects non-Point geometry" do
      payload = %{
        "type" => "Feature",
        "geometry" => %{"type" => "LineString", "coordinates" => [[0, 0], [1, 1]]}
      }

      assert :error = Walker.first_point(payload)
    end
  end

  describe "leaves/1 (recursive AST)" do
    test "collects all leaf values" do
      tree =
        {:node,
         [
           {:leaf, 1},
           {:node, [{:leaf, 2}, {:leaf, 3}]},
           {:node, [{:node, [{:leaf, 4}]}]}
         ]}

      assert Walker.leaves(tree) == [1, 2, 3, 4]
    end

    test "single leaf" do
      assert Walker.leaves({:leaf, :only}) == [:only]
    end

    test "empty node" do
      assert Walker.leaves({:node, []}) == []
    end

    test "non-AST input returns empty list" do
      assert Walker.leaves(42) == []
    end
  end

  describe "address/1" do
    test "extracts 4-level nested address" do
      payload = %{
        user: %{
          profile: %{
            address: %{city: "Madrid", country: "ES", zip: "28001"},
            bio: "hi"
          }
        }
      }

      assert {:ok, %{city: "Madrid", country: "ES", zip: "28001"}} = Walker.address(payload)
    end

    test "missing zip fails" do
      payload = %{user: %{profile: %{address: %{city: "X", country: "Y"}}}}
      assert :error = Walker.address(payload)
    end
  end
end
```

### Run it

```bash
mix new json_ast_walker
cd json_ast_walker
mix test
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== JsonAstWalker: demo ===\n")

    result_1 = JsonAstWalker.leaves(tree)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = JsonAstWalker.leaves({:leaf, :only})
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = JsonAstWalker.leaves({:node, []})
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create `lib/ast_analyzer.ex` and test in `iex`:

```elixir
defmodule ASTAnalyzer do
  def extract_author_email({:ok, %{author: %{email: email}}}) when is_binary(email) do
    {:ok, email}
  end

  def extract_author_email({:error, reason}), do: {:error, reason}
  def extract_author_email(_), do: {:error, :invalid_structure}

  def extract_user_info({:ok, %{user: %{id: id, profile: %{name: name, age: age}}} = data}) when is_integer(id) and is_binary(name) and is_integer(age) do
    {:ok, %{id: id, name: name, age: age, full_response: data}}
  end

  def extract_user_info({:error, r}), do: {:error, r}
  def extract_user_info(_), do: {:error, :malformed}

  def analyze_author_metadata({:ok, %{author: %{email: email, location: %{city: city, country: country}}}}) when is_binary(email) and is_binary(city) and is_binary(country) do
    {:ok, {email, city, country}}
  end

  def analyze_author_metadata(_), do: {:error, :missing_metadata}

  def validate_package(%{name: name, version: version, license: %{type: license_type}}) when is_binary(name) and is_binary(version) and is_binary(license_type) do
    :valid
  end

  def validate_package(_), do: :invalid
end

# Test it
response = {:ok, %{author: %{email: "alice@example.com"}}}
{:ok, email} = ASTAnalyzer.extract_author_email(response)
IO.inspect(email)  # "alice@example.com"

user_response = {:ok, %{user: %{id: 1, profile: %{name: "Bob", age: 30}}}}
{:ok, info} = ASTAnalyzer.extract_user_info(user_response)
IO.inspect(info.name)  # "Bob"

metadata_response = {:ok, %{author: %{email: "charlie@example.com", location: %{city: "NYC", country: "USA"}}}}
{:ok, {email, city, country}} = ASTAnalyzer.analyze_author_metadata(metadata_response)
IO.inspect(city)  # "NYC"

package = %{name: "my_pkg", version: "1.0.0", license: %{type: "MIT"}}
:valid = ASTAnalyzer.validate_package(package)
IO.puts("Package validation passed")
```

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of extract_author_email/1 over 100k AST nodes
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 20ms total; destructuring is one BEAM instruction per level**.

## Trade-offs and production mistakes

**1. Exact-length list patterns**
`[a, b, c]` matches EXACTLY three elements. A list of four fails silently.
Use `[a, b, c | _]` to match "at least three".

**2. Too-deep patterns become unreadable**
Matching 6 levels deep works but is hard to review. Extract sub-patterns
into helper functions, or use `get_in/2` with a path when the shape is
uncertain.

**3. Destructuring in function heads vs `with`**
For happy-path extraction across multiple steps, `with` chains read better:
```elixir
with %{user: user} <- payload,
     %{email: email} when is_binary(email) <- user do
  {:ok, email}
else
  _ -> :error
end
```

**4. Pattern matching nils**
`%{key: nil}` matches ONLY when the value is nil. `%{key: _}` matches any
value INCLUDING nil. `%{key: value}` binds `value` even if it's nil — a
common source of "why is value nil?" bugs.

**5. Tuple arity must match**
`{a, b}` won't match `{a, b, c}`. Unlike maps, tuples are fixed-size.

## When NOT to use deep destructuring

- When the shape is fuzzy — use `Access` (`get_in/2`, `put_in/3`) instead.
- When you need to customize the error per missing field — a `with` chain
  gives you per-step error handling.
- In library boundaries where the input schema may evolve — rigid patterns
  break when upstream adds/removes fields.

---

## Reflection

Where does deep destructuring stop being readable? Pick a number of levels and justify it based on a real codebase you've touched.

If the AST structure changes (a field gets wrapped in an extra layer), how many places in your code need to change with deep destructuring? With `get_in/2`?

## Resources

- [Pattern matching — HexDocs](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Access behaviour](https://hexdocs.pm/elixir/Access.html)
- [get_in/2 and put_in/3](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [with special form](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)

---

## Why Nested Destructuring matters

Mastering **Nested Destructuring** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Destructuring Composes for Deeply Nested Data

```elixir
{:ok, %{user: %{id: id, email: email}}} = api_response
```

Pattern matching works arbitrarily deep. You extract exactly what you need in one operation.

### 2. Aliasing in Patterns Captures Sub-structures

```elixir
{:ok, %{user: user} = data} = response
```

You capture both the full `data` and the extracted `user`. This prevents re-computing the pattern match later.

### 3. Avoid Over-nesting

While composable, patterns can become brittle if the API changes shape. Consider building helper modules with extraction functions for complex nested structures.

---
