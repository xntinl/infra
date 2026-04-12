# `Access` behaviour — bracket syntax for a `User` struct

**Project**: `user_access` — a `User` struct that supports `user[:profile][:name]` and `put_in/get_in/update_in` by implementing `Access`.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Elixir's bracket syntax — `user[:profile]` — and the `Kernel.get_in/2`,
`put_in/2`, and `update_in/2` family are all powered by the `Access`
behaviour. Maps and keyword lists implement it out of the box. Structs do
NOT — by default `user[:field]` raises.

This matters the moment your domain model is more than a flat map. A `User`
that nests a `Profile` is awkward to traverse with pattern matching alone:

```elixir
%{user | profile: %{user.profile | name: "new"}}  # painful
put_in(user[:profile][:name], "new")              # with Access
```

Implementing `Access` on your own struct unlocks the same ergonomics users
already expect from maps. This exercise does exactly that, with care taken
around the callbacks' exact semantics — `get_and_update` and `pop` are
easy to get subtly wrong.

Project structure:

```
user_access/
├── lib/
│   └── user.ex
├── test/
│   └── user_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Access` has three callbacks

```elixir
@callback fetch(t, key) :: {:ok, value} | :error
@callback get_and_update(t, key, (value | nil -> {get, value} | :pop)) ::
            {get, t}
@callback pop(t, key) :: {value, t}
```

`fetch/2` drives `user[:k]`. `get_and_update/3` drives `update_in`, `put_in`,
and `Kernel.update_in`. `pop/2` drives `pop_in`.

### 2. `get_in/2` composes `Access` dispatch

`get_in(user, [:profile, :name])` calls `Access.get(user, :profile)`, then
`Access.get(result, :name)`. Implementing `Access` once on your struct makes
it work seamlessly with nested structures.

### 3. Structs do NOT get `Access` for free

This is an intentional design choice. Structs have a fixed shape; throwing
arbitrary keys at `user[:typo]` should fail loudly. You implement `Access`
only when you've decided bracket access makes sense for your type.

### 4. `Kernel.get_in/put_in` also accept functions

If your struct shouldn't implement `Access` but you still want nested traversal,
you can pass an accessor function: `get_in(user, [Access.key(:profile), :name])`.
See `Access.key/2` and `Access.all/0` for built-in accessors.

---

## Implementation

### Step 1: Create the project

```bash
mix new user_access
cd user_access
```

### Step 2: `lib/user.ex`

```elixir
defmodule User do
  @moduledoc """
  A small user struct that supports bracket-access (`user[:profile]`) and
  works with `get_in`, `put_in`, and `update_in`. Implements the `Access`
  behaviour.
  """

  @behaviour Access

  @enforce_keys [:id, :profile]
  defstruct [:id, :profile]

  @type t :: %__MODULE__{id: term(), profile: map()}

  @doc "Build a new user with id and a profile map."
  @spec new(term(), map()) :: t
  def new(id, profile) when is_map(profile), do: %__MODULE__{id: id, profile: profile}

  # ── Access callbacks ───────────────────────────────────────────────────
  #
  # We delegate to the underlying map for storage, but restrict which keys
  # are accessible — only declared struct fields. Attempting to fetch an
  # unknown key returns :error, which is the documented "not present" signal.

  @impl Access
  def fetch(%__MODULE__{} = user, key) when key in [:id, :profile] do
    {:ok, Map.fetch!(user, key)}
  end

  def fetch(%__MODULE__{}, _key), do: :error

  @impl Access
  def get_and_update(%__MODULE__{} = user, key, fun)
      when key in [:id, :profile] and is_function(fun, 1) do
    current = Map.fetch!(user, key)

    case fun.(current) do
      {get, new_value} ->
        {get, Map.put(user, key, new_value)}

      :pop ->
        # Pop is allowed only for optional fields; id/profile are enforced.
        raise ArgumentError, "cannot pop required key #{inspect(key)} from User"
    end
  end

  @impl Access
  def pop(%__MODULE__{}, key) do
    raise ArgumentError, "cannot pop key #{inspect(key)} from User (all keys required)"
  end
end
```

### Step 3: `test/user_test.exs`

```elixir
defmodule UserTest do
  use ExUnit.Case, async: true

  setup do
    %{user: User.new(1, %{name: "Jane", email: "j@x.io"})}
  end

  describe "bracket access" do
    test "top-level field via brackets", %{user: user} do
      assert user[:id] == 1
      assert user[:profile] == %{name: "Jane", email: "j@x.io"}
    end

    test "unknown struct key returns nil (Access.get default)", %{user: user} do
      # fetch/2 returns :error → get/2 returns nil, matching Map semantics.
      assert user[:nope] == nil
    end

    test "nested access via brackets", %{user: user} do
      # profile is a plain map; Map already implements Access, so this chains.
      assert user[:profile][:name] == "Jane"
    end
  end

  describe "get_in / put_in / update_in" do
    test "get_in/2 traverses into the profile map", %{user: user} do
      assert get_in(user, [:profile, :name]) == "Jane"
    end

    test "put_in/2 replaces a nested value", %{user: user} do
      updated = put_in(user, [:profile, :name], "Janet")
      assert updated.profile.name == "Janet"
      # Original is untouched — immutable, as always.
      assert user.profile.name == "Jane"
    end

    test "update_in/2 transforms a nested value", %{user: user} do
      updated = update_in(user, [:profile, :name], &String.upcase/1)
      assert updated.profile.name == "JANE"
    end
  end

  describe "pop semantics" do
    test "pop on a required key raises", %{user: user} do
      assert_raise ArgumentError, fn -> pop_in(user, [:profile]) end
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

**1. Implementing `Access` is a public API decision**
Once callers use `user[:field]`, renaming a field is a breaking change — the
bracket form happily returns `nil` for the old key without any warning. Pin
field names via `@enforce_keys` and restrict `fetch/2` to the known set to
fail loudly on typos at the server boundary.

**2. `get_and_update/3` must handle `:pop` correctly**
`update_in` callbacks may return `:pop` to remove the key. If your struct
can't represent "key absent", you must either raise (as above) or coerce to
a default. Silently ignoring `:pop` corrupts state.

**3. `Kernel.put_in/3` is NOT the same as `Map.put/3`**
`put_in(user, [:profile, :name], "x")` requires `Access` on `user`. If you
forget to implement it, the error message is about the struct, not the key —
expect confused issue reports.

**4. Bracket access is `nil`-lenient; dot access is strict**
`user[:missing]` returns `nil`. `user.missing` raises `KeyError`. Don't let
code drift between the two styles — pick one per abstraction layer.

**5. When NOT to implement `Access`**
If the struct's shape is opaque (an encoded blob, an internal cache), don't.
Force callers to go through functions you control. `Access` is for value
objects, not for stateful or invariant-heavy types.

---

## Resources

- [`Access` behaviour — Elixir stdlib](https://hexdocs.pm/elixir/Access.html)
- [`Kernel.get_in/2`, `put_in/3`, `update_in/3`](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [`Access.key/2` — accessor for missing-or-present fields](https://hexdocs.pm/elixir/Access.html#key/2)
- ["The Access behaviour and you" — ElixirSchool](https://elixirschool.com/en/lessons/advanced/protocols/)
