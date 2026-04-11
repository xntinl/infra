# Access Behaviour: Custom Lens-Like Access Patterns

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` stores job configuration in a deeply nested map. The scheduler needs to read and update individual fields several levels deep — retry policies, timeout values, handler options. With the `Access` behaviour, structs become composable with `get_in/2`, `update_in/3`, and `put_in/3`, enabling path-based access just like plain maps.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── job_config.ex           # ← you implement Access here
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── access_test.exs         # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Job configurations are nested structs:

```elixir
%JobConfig{
  handler: "TaskQueue.Handlers.Email",
  retry: %RetryPolicy{max_attempts: 3, backoff_ms: 1_000},
  timeout: %TimeoutPolicy{execute_ms: 5_000, connect_ms: 1_000}
}
```

The scheduler needs to:
1. Read `config.retry.max_attempts` without chained struct field access
2. Update `config.retry.backoff_ms` after a transient failure using `update_in`
3. Pass config paths to generic functions that work on both maps and structs

Without `Access`, path-based operations on structs raise `UndefinedFunctionError`. With `Access`, the struct participates in the same `get_in/update_in/put_in` ecosystem as maps.

---

## What the Access behaviour requires

The `Access` behaviour defines three callbacks:

```elixir
@callback fetch(container, key) :: {:ok, value} | :error
@callback get_and_update(container, key, (value -> {get_value, new_value} | :pop)) :: {get_value, new_container}
@callback pop(container, key) :: {value, new_container}
```

`fetch/2` is used by `get_in/2`. `get_and_update/3` is used by `update_in/3` and `get_and_update_in/3`. `pop/2` is used by `pop_in/2`.

The simplest implementation delegates to `Map.fetch`, `Map.get_and_update`, and `Map.pop`:

```elixir
@behaviour Access

def fetch(struct, key), do: Map.fetch(Map.from_struct(struct), key)
```

But this exposes ALL struct fields. A deliberate implementation exposes only the fields that make semantic sense as public paths.

---

## Implementation

### Step 1: `lib/task_queue/job_config.ex` — structs with Access behaviour

```elixir
defmodule TaskQueue.JobConfig.RetryPolicy do
  @moduledoc "Retry configuration for a job."

  @behaviour Access

  defstruct [
    max_attempts: 3,
    backoff_ms: 1_000,
    max_backoff_ms: 30_000
  ]

  @type t :: %__MODULE__{
    max_attempts:  pos_integer(),
    backoff_ms:    pos_integer(),
    max_backoff_ms: pos_integer()
  }

  @impl Access
  def fetch(policy, key) do
    Map.fetch(Map.from_struct(policy), key)
  end

  @impl Access
  def get_and_update(policy, key, fun) do
    {get, updated_map} = Map.get_and_update(Map.from_struct(policy), key, fun)
    {get, struct(__MODULE__, updated_map)}
  end

  @impl Access
  def pop(policy, key) do
    {value, map} = Map.pop(Map.from_struct(policy), key)
    {value, struct(__MODULE__, map)}
  end
end

defmodule TaskQueue.JobConfig do
  @moduledoc """
  Top-level job configuration.

  Implements `Access` to allow path-based reads and updates:

      get_in(config, [:retry, :max_attempts])
      update_in(config, [:retry, :backoff_ms], &(&1 * 2))

  Only `:handler`, `:retry`, and `:timeout_ms` are accessible via Access.
  Internal fields like `:_version` are hidden.
  """

  @behaviour Access

  defstruct [
    :handler,
    retry: %TaskQueue.JobConfig.RetryPolicy{},
    timeout_ms: 5_000,
    _version: 1    # internal — not accessible via Access
  ]

  @type t :: %__MODULE__{
    handler:    String.t() | nil,
    retry:      TaskQueue.JobConfig.RetryPolicy.t(),
    timeout_ms: pos_integer()
  }

  @accessible_keys ~w(handler retry timeout_ms)a

  @impl Access
  def fetch(config, key) when key in @accessible_keys do
    Map.fetch(Map.from_struct(config), key)
  end

  def fetch(_config, _key), do: :error

  @impl Access
  def get_and_update(config, key, fun) when key in @accessible_keys do
    {get, updated_map} = Map.get_and_update(Map.from_struct(config), key, fun)
    {get, struct(__MODULE__, updated_map)}
  end

  def get_and_update(config, _key, fun) do
    {get, _} = fun.(nil)
    {get, config}
  end

  @impl Access
  def pop(config, key) when key in @accessible_keys do
    {value, map} = Map.pop(Map.from_struct(config), key)
    {value, struct(__MODULE__, map)}
  end

  def pop(config, _key), do: {nil, config}
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/access_test.exs
defmodule TaskQueue.AccessTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobConfig
  alias TaskQueue.JobConfig.RetryPolicy

  describe "RetryPolicy — Access behaviour" do
    test "fetch returns value for existing key" do
      policy = %RetryPolicy{max_attempts: 5}
      assert {:ok, 5} = Access.fetch(policy, :max_attempts)
    end

    test "fetch returns :error for missing key" do
      policy = %RetryPolicy{}
      assert :error = Access.fetch(policy, :nonexistent)
    end

    test "get_and_update modifies a field" do
      policy = %RetryPolicy{backoff_ms: 1_000}
      {old, new_policy} = Access.get_and_update(policy, :backoff_ms, fn v -> {v, v * 2} end)
      assert old == 1_000
      assert new_policy.backoff_ms == 2_000
    end

    test "pop removes and returns a field" do
      policy = %RetryPolicy{max_attempts: 3}
      {value, new_policy} = Access.pop(policy, :max_attempts)
      assert value == 3
      # After pop, field reverts to default (nil or struct default)
      assert new_policy.max_attempts == nil or is_integer(new_policy.max_attempts)
    end
  end

  describe "JobConfig — Access behaviour with path operations" do
    test "get_in reads top-level field" do
      config = %JobConfig{handler: "TaskQueue.Handlers.Email"}
      assert get_in(config, [:handler]) == "TaskQueue.Handlers.Email"
    end

    test "get_in reads nested struct field" do
      config = %JobConfig{retry: %RetryPolicy{max_attempts: 5}}
      assert get_in(config, [:retry, :max_attempts]) == 5
    end

    test "update_in modifies nested struct field" do
      config = %JobConfig{retry: %RetryPolicy{backoff_ms: 1_000}}
      new_config = update_in(config, [:retry, :backoff_ms], &(&1 * 2))
      assert new_config.retry.backoff_ms == 2_000
    end

    test "put_in sets nested field" do
      config = %JobConfig{retry: %RetryPolicy{max_attempts: 3}}
      new_config = put_in(config, [:retry, :max_attempts], 10)
      assert new_config.retry.max_attempts == 10
    end

    test "internal fields are not accessible via Access" do
      config = %JobConfig{}
      assert get_in(config, [:_version]) == nil
    end

    test "update_in returns same struct type" do
      config = %JobConfig{timeout_ms: 5_000}
      new_config = update_in(config, [:timeout_ms], &(&1 + 1_000))
      assert %JobConfig{} = new_config
      assert new_config.timeout_ms == 6_000
    end

    test "update_in on nested struct returns nested struct type" do
      config = %JobConfig{}
      new_config = update_in(config, [:retry, :max_attempts], &(&1 + 1))
      assert %RetryPolicy{} = new_config.retry
    end
  end

  describe "Access behaviour in get_in with Access helpers" do
    test "Access.key/1 works with structs implementing Access" do
      config = %JobConfig{retry: %RetryPolicy{max_attempts: 7}}
      assert get_in(config, [Access.key(:retry), Access.key(:max_attempts)]) == 7
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/access_test.exs --trace
```

---

## Trade-off analysis

| Approach | Works with `get_in/update_in` | Hides internal fields | Boilerplate | Best for |
|----------|------------------------------|----------------------|-------------|----------|
| `@behaviour Access` custom | yes | yes (explicit allow-list) | medium | domain structs with controlled public interface |
| `@derive [Access]` via macro | yes | no — exposes all fields | none | simple structs, all fields are public |
| Manual `Map.from_struct` | no — must call explicitly | depends | high | one-off conversions |
| Pattern matching | no | N/A | none | simple, one-level access |

Reflection question: `Access.all()` works on lists, not maps. If the job registry is `%{job_id => %JobConfig{}}`, how would you use `update_in` with a custom `Access` function to update the `retry.max_attempts` for every job in the registry simultaneously?

Answer: You would define a custom access function that iterates over map values, similar to how `Access.all()` works for lists:

```elixir
def all_values do
  fn :get, data, next ->
    Enum.map(data, fn {_k, v} -> next.(v) end)
  :get_and_update, data, next ->
    Map.new(data, fn {k, v} ->
      {get, updated} = next.(v)
      {k, updated}
    end)
    |> then(fn new_map -> {Map.values(data), new_map} end)
  end
end

update_in(registry, [all_values(), :retry, :max_attempts], &(&1 + 1))
```

---

## Common production mistakes

**1. Not wrapping `Map.get_and_update` result back into the struct**

```elixir
# Wrong — returns a plain map, not the struct
def get_and_update(policy, key, fun) do
  Map.get_and_update(Map.from_struct(policy), key, fun)
end

# Right — re-wrap in the struct
def get_and_update(policy, key, fun) do
  {get, map} = Map.get_and_update(Map.from_struct(policy), key, fun)
  {get, struct(__MODULE__, map)}
end
```

**2. Implementing `Access` but not handling the `:pop` tuple from `get_and_update`**

`fun` in `get_and_update/3` can return `{get_value, new_value}` OR `:pop`. If your implementation does not handle the `:pop` atom, `pop_in/2` will crash. Delegating entirely to `Map.get_and_update` handles this correctly.

**3. Exposing all fields including internal ones**

A field prefixed with `_` signals "internal implementation detail." If `Access.fetch/2` returns it, callers can `update_in` it, breaking invariants. Use an explicit allow-list:

```elixir
@accessible_keys ~w(handler retry timeout_ms)a
def fetch(config, key) when key in @accessible_keys, do: Map.fetch(Map.from_struct(config), key)
def fetch(_config, _key), do: :error
```

**4. Using `Access.key/1` on a struct without `Access` implemented**

`Access.key/1` calls `Access.fetch/2`. If the struct does not implement `Access`, this raises `Protocol.UndefinedError`. Always implement all three callbacks before using `get_in/update_in` with your struct.

**5. Confusing `Access.fetch/2` with `Map.fetch/2`**

`Access.fetch/2` is the protocol function — it dispatches to the struct's implementation. `Map.fetch/2` is a concrete function only for maps. Calling `Map.fetch(my_struct, :key)` works because all structs are maps under the hood, but it bypasses your custom `Access` implementation and exposes ALL fields.

---

## Resources

- [Access behaviour — official docs](https://hexdocs.pm/elixir/Access.html)
- [get_in/put_in/update_in — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [Access.key/2 — path helpers](https://hexdocs.pm/elixir/Access.html#key/2)
- [Implementing Access — Jose Valim blog](https://dashbit.co/blog/access-and-struct-updates)
