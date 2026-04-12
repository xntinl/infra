# Access Behaviour: Custom Lens-Like Access Patterns

## Goal

Build `task_queue` job configuration structs that implement the `Access` behaviour, enabling `get_in/2`, `put_in/3`, and `update_in/3` on nested structs. Learn the three callbacks (`fetch`, `get_and_update`, `pop`), how to expose only safe fields via an allow-list, and why the return value must be re-wrapped into the struct.

---

## What the Access behaviour requires

The `Access` behaviour defines three callbacks:

```elixir
@callback fetch(container, key) :: {:ok, value} | :error
@callback get_and_update(container, key, (value -> {get_value, new_value} | :pop)) :: {get_value, new_container}
@callback pop(container, key) :: {value, new_container}
```

`fetch/2` is used by `get_in/2`. `get_and_update/3` is used by `update_in/3` and `get_and_update_in/3`. `pop/2` is used by `pop_in/2`.

The simplest implementation delegates to `Map.fetch`, `Map.get_and_update`, and `Map.pop` on the struct converted to a map, then wraps the result back into the struct.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/job_config.ex` -- structs with Access behaviour

`RetryPolicy` implements Access by delegating to `Map.fetch/2`, `Map.get_and_update/2`, and `Map.pop/2` on the struct-to-map conversion. The critical detail is re-wrapping the result with `struct(__MODULE__, updated_map)` -- without this, `update_in` returns a plain map instead of the struct, breaking downstream pattern matches.

`JobConfig` uses an explicit allow-list (`@accessible_keys`) to hide internal fields like `_version`. When `fetch/2` receives a key not in the allow-list, it returns `:error`, making `get_in(config, [:_version])` return `nil` instead of the actual value.

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
    _version: 1
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

### Step 3: Tests

```elixir
# test/task_queue/access_test.exs
defmodule TaskQueue.AccessTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobConfig
  alias TaskQueue.JobConfig.RetryPolicy

  describe "RetryPolicy -- Access behaviour" do
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
      assert new_policy.max_attempts == nil or is_integer(new_policy.max_attempts)
    end
  end

  describe "JobConfig -- Access behaviour with path operations" do
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

### Step 4: Run

```bash
mix test test/task_queue/access_test.exs --trace
```

---

## Trade-off analysis

| Approach | Works with `get_in/update_in` | Hides internal fields | Boilerplate | Best for |
|----------|------------------------------|----------------------|-------------|----------|
| `@behaviour Access` custom | yes | yes (explicit allow-list) | medium | domain structs with controlled public interface |
| `@derive [Access]` via macro | yes | no -- exposes all fields | none | simple structs, all fields public |
| Manual `Map.from_struct` | no -- must call explicitly | depends | high | one-off conversions |
| Pattern matching | no | N/A | none | simple, one-level access |

---

## Common production mistakes

**1. Not wrapping `Map.get_and_update` result back into the struct**
Without `struct(__MODULE__, updated_map)`, `update_in` returns a plain map.

**2. Not handling the `:pop` atom from `get_and_update`**
The function in `get_and_update/3` can return `{get_value, new_value}` OR `:pop`. Delegating to `Map.get_and_update` handles this correctly.

**3. Exposing all fields including internal ones**
A field prefixed with `_` signals "internal." Use an explicit allow-list to prevent callers from `update_in`-ing invariants.

**4. Using `Access.key/1` on a struct without `Access` implemented**
`Access.key/1` calls `Access.fetch/2`. If the struct does not implement `Access`, this raises.

**5. Confusing `Access.fetch/2` with `Map.fetch/2`**
`Map.fetch(my_struct, :key)` works (structs are maps) but bypasses your custom `Access` implementation, exposing all fields.

---

## Resources

- [Access behaviour -- official docs](https://hexdocs.pm/elixir/Access.html)
- [get_in/put_in/update_in -- Kernel docs](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [Access.key/2 -- path helpers](https://hexdocs.pm/elixir/Access.html#key/2)
