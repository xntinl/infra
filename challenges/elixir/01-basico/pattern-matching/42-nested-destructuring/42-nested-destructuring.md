# Nested Destructuring: A JSON AST Walker

**Project**: `json_ast_walker` — extracts specific fields from JSON-like nested structures using destructuring patterns

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Project structure

```
json_ast_walker/
├── lib/
│   └── json_ast_walker.ex
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

## Implementation

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

## Resources

- [Pattern matching — HexDocs](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Access behaviour](https://hexdocs.pm/elixir/Access.html)
- [get_in/2 and put_in/3](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [with special form](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
