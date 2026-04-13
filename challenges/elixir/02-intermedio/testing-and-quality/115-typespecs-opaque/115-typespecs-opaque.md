# `@opaque` vs `@type` for encapsulating representation

**Project**: `opaque_types` — a `UserId` module that exposes an opaque type
so callers can't peek at (or depend on) the internal representation.

---

## Project context

You ship a library that returns `UserId.t()` all over its API. Today it's
a `String.t()`. Tomorrow you want to migrate to a `{String.t(), integer()}`
tuple for tenancy. If users wrote `id <> "_extra"` anywhere in their code,
your refactor just broke them silently — the spec `@type t :: String.t()`
told them "this is a string, go ahead".

`@opaque` is the fix. It exports the *name* of the type (so callers can
reference `UserId.t()` in their own specs) but **hides the structure**.
Dialyzer will flag any caller that tries to pattern-match or manipulate
the underlying representation. It's Elixir's closest equivalent to a
nominal type.

## Why `@opaque` and not X

**Why not just `@type t :: String.t()`?** Because callers then legitimately
treat it as a string — when you refactor the representation, their code
breaks.

**Why not runtime wrappers (classes/objects)?** The BEAM has no classes;
structs are data + a `__struct__` field. `@opaque` gives you the
abstraction boundary at the type-checking layer without runtime overhead.

**Why not just trust callers to use the public API?** In a private
monorepo maybe. At a library boundary or a team boundary,
Dialyzer-enforced opacity is the cheapest enforcement mechanism.

Project structure:

```
opaque_types/
├── lib/
│   ├── user_id.ex
│   └── consumer.ex
├── test/
│   ├── user_id_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `@type` exports name AND structure

```elixir
@type t :: String.t()
```

Callers can write `@spec handle(UserId.t()) :: :ok` AND can treat the
value as a string — concat it, slice it, pass it to `String.upcase/1`.
Your internal choice has leaked into their code.

### 2. `@opaque` exports name, hides structure

```elixir
@opaque t :: String.t()
```

Callers can still write `@spec handle(UserId.t()) :: :ok`. But Dialyzer
will flag `String.upcase(user_id)` in their code: from the outside,
`UserId.t()` is just "a UserId" — no longer a string.

### 3. Opacity is a Dialyzer concept, not runtime

At runtime, `UserId.t()` is whatever it actually is (a string, a tuple).
Nothing stops a caller from `is_binary/1`-matching it. Opacity is
enforced by `mix dialyzer`, not the VM. Still worth it — it catches
dependency leaks before they ship.

### 4. Constructor and accessor pattern

The typical `@opaque` usage: expose a constructor (`UserId.new/1`), an
accessor (`UserId.to_string/1`), and optionally comparisons/equality.
Never let the internal structure escape.

---

## Design decisions

**Option A — Opaque wrapping a `String.t()`**
- Pros: Minimal change from an existing string-based API.
- Cons: No room to add tenant/metadata later without a breaking change.

**Option B — Opaque wrapping a struct `%__MODULE__{value, tenant}`** (chosen)
- Pros: Future-proof — fields can grow; callers unaffected.
- Cons: Slight construction cost vs a bare string.

→ Chose **B** because the whole point of opacity is enabling internal
evolution. Picking a string kneecaps that future.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {dialyxir},
    {exunit},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new opaque_types
cd opaque_types
```

Add Dialyxir:

```elixir
defp deps do
  [{:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}]
end
```

### Step 2: `lib/user_id.ex`

**Objective**: Implement `user_id.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule UserId do
  @moduledoc """
  An opaque user identifier. Callers MUST use `new/1` to construct and
  `to_string/1` to render. The underlying representation is private and
  may change without notice.
  """

  # Internally a struct. Externally, callers see only `UserId.t()`.
  defstruct [:value, :tenant]

  @opaque t :: %__MODULE__{value: String.t(), tenant: atom()}

  @doc "Builds a UserId. Raises on empty input."
  @spec new(String.t(), atom()) :: t()
  def new(value, tenant \\ :default)
      when is_binary(value) and value != "" and is_atom(tenant) do
    %__MODULE__{value: value, tenant: tenant}
  end

  @doc "Renders a UserId to its canonical string form."
  @spec to_string(t()) :: String.t()
  def to_string(%__MODULE__{value: v, tenant: :default}), do: v
  def to_string(%__MODULE__{value: v, tenant: t}), do: "#{t}:#{v}"

  @doc "Opaque equality — safe, doesn't expose structure."
  @spec equal?(t(), t()) :: boolean()
  def equal?(%__MODULE__{} = a, %__MODULE__{} = b), do: a == b

  @doc "Returns the tenant. The only approved way to read it."
  @spec tenant(t()) :: atom()
  def tenant(%__MODULE__{tenant: t}), do: t
end
```

### Step 3: `lib/consumer.ex`

**Objective**: Implement `consumer.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule Consumer do
  @moduledoc """
  Demonstrates a caller using `UserId.t()` WITHOUT reaching into its guts.
  Everything here is what Dialyzer would call "clean opaque usage".
  """

  @spec greet(UserId.t()) :: String.t()
  def greet(user_id) do
    # We use the public API only — no struct pattern-matching, no field access.
    "hello, " <> UserId.to_string(user_id)
  end

  @spec same_user?(UserId.t(), UserId.t()) :: boolean()
  def same_user?(a, b), do: UserId.equal?(a, b)

  # COMMENTED-OUT EXAMPLES of what Dialyzer would flag:
  #
  # def bad(user_id), do: user_id.value
  # #=> dialyzer: "The call Map.get(user_id, :value) breaks the opacity"
  #
  # def bad2(%UserId{value: v}), do: v
  # #=> dialyzer: "matching against the internal structure"
end
```

### Step 4: `test/user_id_test.exs`

**Objective**: Write `user_id_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule UserIdTest do
  use ExUnit.Case, async: true

  describe "new/2" do
    test "builds with default tenant" do
      id = UserId.new("alice")
      assert UserId.to_string(id) == "alice"
      assert UserId.tenant(id) == :default
    end

    test "builds with explicit tenant" do
      id = UserId.new("bob", :acme)
      assert UserId.to_string(id) == "acme:bob"
      assert UserId.tenant(id) == :acme
    end

    test "rejects empty value" do
      assert_raise FunctionClauseError, fn -> UserId.new("") end
    end
  end

  describe "equal?/2" do
    test "equal when value and tenant match" do
      assert UserId.equal?(UserId.new("a"), UserId.new("a"))
    end

    test "unequal when tenant differs" do
      refute UserId.equal?(UserId.new("a", :x), UserId.new("a", :y))
    end
  end

  describe "Consumer as an opacity-respecting caller" do
    test "greet uses the public API only" do
      assert Consumer.greet(UserId.new("ada")) == "hello, ada"
    end

    test "same_user? delegates to UserId.equal?/2" do
      assert Consumer.same_user?(UserId.new("x"), UserId.new("x"))
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
mix dialyzer
```

Dialyzer should report zero issues. Now try uncommenting the `bad/1`
function in `consumer.ex` and re-run — you'll see an opacity warning
identifying the offending line.

### Why this works

`@opaque t :: %__MODULE__{...}` exports the type name to callers while
telling Dialyzer "treat anything produced inside this module as an
abstract token". When a caller writes `user_id.value` or
`%UserId{value: v}`, Dialyzer sees the attempt to unpack a value that
should be opaque and emits an opacity warning with the exact file:line.
The VM isn't involved — the struct is still a struct at runtime — but
the typing discipline catches accidental leaks before they merge.

---

## Benchmark

<!-- benchmark N/A: el costo de construir un struct vs una string es
sub-nanosegundo; no es el criterio de decisión. -->

---

## Trade-offs and production gotchas

**1. Opacity is compile-time advice, not runtime enforcement**
A determined caller can always bypass it. Treat `@opaque` as a **contract
with Dialyzer-using callers** — still valuable, especially in libraries.

**2. `inspect` leaks the structure**
`IO.inspect(user_id)` prints the full struct, which makes the internal
representation discoverable. If the representation is sensitive, implement
the `Inspect` protocol to render only the opaque form.

**3. Accessors can accidentally break opacity**
A function like `def raw(user_id), do: user_id.value` returns
`String.t()` — which Dialyzer flags as breaking opacity. Either:
a) Don't expose raw access (preferred).
b) Spec the accessor as `:: String.t()` explicitly — Dialyzer then
   treats it as an approved "break".

**4. `@opaque` inside a struct is fine; using the struct name isn't**
If you name the opaque type `t()` (not `user_id()`), callers write
`%UserId{} = id` out of habit — which Dialyzer flags. Document clearly
that construction must go through `new/*`.

**5. When NOT to use `@opaque`**
For internal modules your team fully controls. The ceremony of
constructors + accessors isn't worth it. `@opaque` shines at **library
boundaries** and at **bounded-context edges** where you don't control the
caller.

---

## Reflection

- You ship version 1 of a library with `@type t :: String.t()`. Six months
  later you want tenant support. Compare the upgrade path from `@type`
  vs `@opaque` — what changes, and what breaks for whom?
- A caller needs to log `UserId.t()` values for debugging. Design the
  `Inspect` implementation that balances opacity with operational
  usefulness.

---

```elixir
defmodule Main do
  import ExUnit.Assertions

    @moduledoc """
    An opaque user identifier. Callers MUST use `new/1` to construct and
    `to_string/1` to render. The underlying representation is private and
    may change without notice.
    """

    # Internally a struct. Externally, callers see only `UserId.t()`.
    defstruct [:value, :tenant]

    @opaque t :: %__MODULE__{value: String.t(), tenant: atom()}

    @doc "Builds a UserId. Raises on empty input."
    @spec new(String.t(), atom()) :: t()
    def new(value, tenant \\ :default)
        when is_binary(value) and value != "" and is_atom(tenant) do
      %__MODULE__{value: value, tenant: tenant}
    end

    @doc "Renders a UserId to its canonical string form."
    @spec to_string(t()) :: String.t()
    def to_string(%__MODULE__{value: v, tenant: :default}), do: v
    def to_string(%__MODULE__{value: v, tenant: t}), do: "#{t}:#{v}"

    @doc "Opaque equality — safe, doesn't expose structure."
    @spec equal?(t(), t()) :: boolean()
    def equal?(%__MODULE__{} = a, %__MODULE__{} = b), do: a == b

    @doc "Returns the tenant. The only approved way to read it."
    @spec tenant(t()) :: atom()
    def tenant(%__MODULE__{tenant: t}), do: t
  end

  def main do
    IO.puts("=== User Demo ===
  ")
  
    # Demo: Create and inspect a user
  user = User.new("alice", "alice@example.com", 30)
  IO.puts("1. User created: name='#{User.name(user)}', email='#{User.email(user)}', age=#{User.age(user)}")
  assert User.is_adult(user)

  user_child = User.new("bob", "bob@example.com", 10)
  IO.puts("2. User child: name='#{User.name(user_child)}', age=#{User.age(user_child)}")
  assert not User.is_adult(user_child)

  IO.puts("
  ✓ Typespecs demo completed!")
  end

end

Main.main()
```


## Resources

- [Typespecs — `@opaque`](https://hexdocs.pm/elixir/typespecs.html#user-defined-types)
- [Dialyzer opacity warnings](https://www.erlang.org/doc/man/dialyzer.html)
- ["Opaque types in Erlang/Elixir" — Erlang docs](https://www.erlang.org/doc/reference_manual/typespec.html#type-declarations-of-user-defined-types)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
