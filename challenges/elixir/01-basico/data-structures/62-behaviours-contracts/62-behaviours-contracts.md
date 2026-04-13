# Behaviours as Module Contracts

**Project**: `kv_store` — a storage backend behaviour with two implementations (in-memory and file)

---

## Project structure

```
kv_store/
├── lib/
│   └── kv_store/
│       ├── backend.ex         # the behaviour
│       ├── in_memory.ex       # Agent-backed impl
│       └── file.ex            # file-backed impl
├── test/
│   └── kv_store_test.exs
└── mix.exs
```

---

## The business problem

You want a key/value API where the storage is swappable: in-memory for tests, file-based
for dev, Redis for prod. The call site should not know or care which backend is active.

A **behaviour** defines the contract (which functions, what types). Each implementing
module opts in with `@behaviour`. The caller picks the module at runtime.

This is the module-level counterpart to protocol-based polymorphism. Protocols dispatch on
**values**; behaviours are chosen explicitly by **modules** — the caller (often via config)
picks which implementation to use.

---

## Why behaviours and not duck-typed function passing

Passing a module around (`KvStore.get(MyBackend, key)`) works without any formal contract — but then the compiler never warns you when a backend is missing `put/3`, or when someone renames `get/2` to `fetch/2` in one impl and forgets the others. The first failure is a runtime `UndefinedFunctionError` after deployment.

Behaviours encode the contract in `@callback` declarations. `@impl true` on each implementer makes the compiler check that every listed callback exists with the right arity. Protocols don't fit here because the value being passed (`key`) doesn't carry backend information — the caller must explicitly choose a backend, and that's the module-driven case.

---

## Design decisions

**Option A — pass a module by name, no `@behaviour`**
- Pros: simplest; no ceremony; works with any module that happens to have the right functions.
- Cons: typos and missing callbacks become runtime errors; no single place documents the contract; editor tooling can't surface "what must a backend implement?".

**Option B — `@callback` in a dedicated behaviour module + `@behaviour` in each impl** (chosen)
- Pros: compiler verifies all required callbacks are present; `@impl true` flags renames and typos; Dialyzer can use the contract for inter-procedural checks; the behaviour file IS the documentation.
- Cons: more files; adding a callback is a breaking change for every impl (which is also the point).

Chose **B** because the whole reason for swappable backends is enforceable uniformity — losing the compile-time check defeats the purpose.

---

## Core concepts

### `@callback`

Declares a function that implementers must provide. It includes name, arity, argument
types, and return type. The compiler verifies all callbacks are present when a module
declares `@behaviour`.

### `@behaviour ModuleName`

Marks a module as implementing a behaviour. The compiler emits a warning for missing
callbacks. It does NOT check return types — that is Dialyzer's job.

### `@optional_callbacks`

Lists callbacks that are optional. Implementers can skip them. The caller must then
check with `function_exported?/3` before calling — or use `@callback` + default impl
via `defoverridable`.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Separate Backend module contract from InMemory/File impls so @callback enforcement is clear and testable.

```bash
mix new kv_store
cd kv_store
mkdir -p lib/kv_store
```

### Step 2: `lib/kv_store/backend.ex` — the behaviour

**Objective**: Declare @callback + @optional_callbacks so compiler enforces contract and separates required from optional capabilities.

```elixir
defmodule KvStore.Backend do
  @moduledoc """
  Contract for a key/value backend.

  Any module implementing this behaviour must handle `get/2`, `put/3`, `delete/2`
  with the documented semantics.
  """

  @typedoc "Opaque handle returned by the implementation — its shape is private."
  @type handle :: term()

  # The callback signatures ARE the contract.
  # Clients program against these types — not against any concrete module.

  @callback init(keyword()) :: {:ok, handle()} | {:error, term()}

  @callback get(handle(), key :: String.t()) :: {:ok, term()} | :not_found

  @callback put(handle(), key :: String.t(), value :: term()) :: :ok | {:error, term()}

  @callback delete(handle(), key :: String.t()) :: :ok

  # Optional — not every backend can meaningfully list all keys (e.g. a remote
  # KV with millions of entries). Clients must check `function_exported?/3`.
  @callback list_keys(handle()) :: [String.t()]
  @optional_callbacks list_keys: 1
end
```

### Step 3: `lib/kv_store/in_memory.ex`

**Objective**: Use @impl true on all callbacks so compiler flags missing/renamed functions instead of silent runtime UndefinedFunctionError.

```elixir
defmodule KvStore.InMemory do
  @moduledoc "In-memory backend backed by an Agent. Good for tests."

  @behaviour KvStore.Backend

  @impl true
  def init(_opts) do
    # The handle is the Agent pid — opaque to the client.
    Agent.start_link(fn -> %{} end)
  end

  @impl true
  def get(agent, key) do
    Agent.get(agent, fn state ->
      case Map.fetch(state, key) do
        {:ok, v} -> {:ok, v}
        :error -> :not_found
      end
    end)
  end

  @impl true
  def put(agent, key, value) do
    Agent.update(agent, &Map.put(&1, key, value))
  end

  @impl true
  def delete(agent, key) do
    Agent.update(agent, &Map.delete(&1, key))
  end

  # We DO implement the optional callback.
  @impl true
  def list_keys(agent) do
    Agent.get(agent, &Map.keys/1)
  end
end
```

### Step 4: `lib/kv_store/file.ex`

**Objective**: Skip `list_keys/1` deliberately so `function_exported?/3` is exercised as the proper gate for optional callbacks.

```elixir
defmodule KvStore.File do
  @moduledoc """
  File-backed backend. Each key is a file under `dir`.

  Not concurrency-safe — for this exercise we assume a single caller.
  A real impl would wrap writes in a GenServer or use `:dets`.
  """

  @behaviour KvStore.Backend

  @impl true
  def init(opts) do
    dir = Keyword.fetch!(opts, :dir)
    File.mkdir_p!(dir)
    # The handle carries the directory — no process needed.
    {:ok, %{dir: dir}}
  end

  @impl true
  def get(%{dir: dir}, key) do
    path = Path.join(dir, key)

    case File.read(path) do
      {:ok, bin} -> {:ok, :erlang.binary_to_term(bin)}
      {:error, :enoent} -> :not_found
    end
  end

  @impl true
  def put(%{dir: dir}, key, value) do
    # We serialize with :erlang.term_to_binary so arbitrary terms round-trip.
    # In production, JSON is safer across language boundaries.
    File.write(Path.join(dir, key), :erlang.term_to_binary(value))
  end

  @impl true
  def delete(%{dir: dir}, key) do
    _ = File.rm(Path.join(dir, key))
    :ok
  end

  # Note: we intentionally do NOT implement list_keys/1.
  # Clients must check `function_exported?(KvStore.File, :list_keys, 1)`.
end
```

### Step 5: `test/kv_store_test.exs`

**Objective**: Run the same test shape against both backends so any contract drift between impls shows up as a red test, not a runtime crash.

```elixir
defmodule KvStoreTest do
  use ExUnit.Case, async: true

  # We run the same test suite against both backends.
  # If the contract is respected, both pass identically.

  describe "InMemory backend" do
    setup do
      {:ok, h} = KvStore.InMemory.init([])
      %{h: h, mod: KvStore.InMemory}
    end

    test "put then get", %{h: h, mod: mod} do
      :ok = mod.put(h, "a", 1)
      assert {:ok, 1} = mod.get(h, "a")
    end

    test "get on missing key returns :not_found", %{h: h, mod: mod} do
      assert :not_found = mod.get(h, "nope")
    end

    test "delete is idempotent", %{h: h, mod: mod} do
      assert :ok = mod.delete(h, "nope")
    end

    test "list_keys is available", %{h: h, mod: mod} do
      :ok = mod.put(h, "a", 1)
      :ok = mod.put(h, "b", 2)
      assert Enum.sort(mod.list_keys(h)) == ["a", "b"]
    end
  end

  describe "File backend" do
    setup do
      dir = Path.join(System.tmp_dir!(), "kv_#{System.unique_integer([:positive])}")
      {:ok, h} = KvStore.File.init(dir: dir)
      on_exit(fn -> File.rm_rf!(dir) end)
      %{h: h, mod: KvStore.File}
    end

    test "put then get", %{h: h, mod: mod} do
      :ok = mod.put(h, "a", %{complex: [1, 2, 3]})
      assert {:ok, %{complex: [1, 2, 3]}} = mod.get(h, "a")
    end

    test "optional callback is absent — caller must detect it", %{mod: mod} do
      # This is how a generic client safely uses optional callbacks.
      refute function_exported?(mod, :list_keys, 1)
    end
  end
end
```

### Step 6: Run the tests

**Objective**: Confirm both backends produce identical results, which is the only proof the behaviour is doing its job.

```bash
mix test
```

### Why this works

The `KvStore.Backend` module contains only `@callback` declarations — zero executable code — so it acts as a pure contract. Each impl declares `@behaviour KvStore.Backend` and marks every callback with `@impl true`, which makes the compiler check that the set of `def` matches the set of `@callback`. At the call site, the caller passes the implementing module as an argument or reads it from config; dispatch is a plain `Module.function/arity` call with no protocol lookup. `@optional_callbacks` plus `function_exported?/3` cleanly encodes "capability some backends have, others don't" without forcing every impl to stub the function.

---


## Key Concepts

### 1. Behaviours Define Required Callbacks

A behaviour lists required functions (`@callback`). Modules implementing it must define those functions. This is compile-time contract enforcement.

### 2. Behaviours vs Protocols

Behaviours work with modules; protocols work with types. Use behaviours for plugin systems. Use protocols for polymorphism across types.

### 3. Common Behaviours in Elixir

`GenServer`, `Supervisor`, `Agent` are all behaviours. When you `use GenServer`, you agree to implement `handle_call`, `handle_cast`, etc. This contract ensures your code integrates correctly.

---
## Benchmark

<!-- benchmark N/A: behaviours compile to plain function calls — there is no dispatch overhead to measure. Performance depends entirely on the chosen backend, not on the behaviour mechanism itself. -->

---

## Trade-offs

| Mechanism | Key property | Example |
|-----------|-------------|---------|
| Behaviour | Contract between modules, caller picks impl | Storage backend, HTTP adapter |
| Protocol | Dispatch on value's type, open extension | `String.Chars`, `Enumerable` |
| Plain function passing | Ad-hoc, no compile check | `Enum.map(list, fn)` |
| GenServer | Stateful contract via message protocol | OTP workers |

A behaviour gives you **compile-time warnings** when a callback is missing (`@impl true`
catches typos). Plain function passing has no such guardrail.

---

## Common production mistakes

**1. Forgetting `@impl true`**
Without `@impl true`, a typo in the function name compiles silently. The caller gets a
runtime `UndefinedFunctionError` months later. Make `@impl true` mandatory in your style guide.

**2. Hardcoding the backend module**
```elixir
def save(key, value), do: KvStore.File.put(@handle, key, value)  # coupled
```
Instead, inject the module via config or function argument. That is the whole point of
the behaviour.

**3. Leaking impl-specific types in the callback signature**
If `@callback get(...) :: {:ok, Ecto.Schema.t()}`, you tie every backend to Ecto. Use
generic types (`term()`, `map()`) at the contract boundary.

**4. Treating `@optional_callbacks` as "implement if convenient"**
Optional callbacks are for capabilities some backends cannot provide (e.g. `list_keys`
in a distributed KV). If every reasonable backend implements it, make it mandatory.

---

## When NOT to use

- **One impl, no plans for another**: YAGNI. A plain module is fine. Add the behaviour when the second impl appears.
- **Value-driven polymorphism**: use a protocol. Behaviours require the caller to know which module to call.
- **Cross-language contracts**: behaviours are BEAM-only. Use an explicit wire protocol (JSON, protobuf) at the boundary.

---

## Reflection

1. Your app ships with `KvStore.InMemory` for tests and `KvStore.File` for dev. A new requirement arrives: persist to Postgres in prod. Do you add `KvStore.Postgres` as a new `@behaviour` impl, or is this the moment the abstraction leaks (transactions, connection pooling) and a different design is warranted? Where is the line?
2. `@optional_callbacks` lets one backend skip `list_keys/1`. When you deploy a new feature that needs `list_keys`, how do you discover which backends break? Is runtime `function_exported?/3` enough, or would you promote it to mandatory and force the compile error?

---

## Resources

- [Elixir docs — Behaviours guide](https://hexdocs.pm/elixir/typespecs.html#behaviours)
- [GenServer source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/gen_server.ex) — biggest real-world behaviour in the standard library
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/article/spawn_or_not) — where behaviours fit vs processes
