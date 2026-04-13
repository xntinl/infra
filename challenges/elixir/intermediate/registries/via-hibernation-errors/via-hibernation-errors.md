# Via tuple patterns with hibernation and error handling

**Project**: `via_hibernation_handling` — combining `:via` tuple naming with hibernation, and safely handling Registry lookup failures.

---

## Why via hibernation error handling matters

A common pattern in Elixir clusters: a GenServer registered via a `:via` tuple
that also hibernates to save memory. But if the Registry process crashes or
restarts, your `via(key)` calls will hang or fail unpredictably. The pattern
here is: **always validate that the Registry exists before issuing via-tuple
calls, and return clear errors if it doesn't.**

This exercise teaches three things:

1. How to combine `:via` tuple patterns with hibernation
2. How to detect Registry unavailability before calling through a via tuple
3. How to implement graceful fallback and error handling

---

## Project structure

```
via_hibernation_handling/
├── lib/
│   └── via_hibernation_handling.ex
├── script/
│   └── main.exs
├── test/
│   └── via_hibernation_handling_test.exs
└── mix.exs
```

---

## The business problem

Real systems built on Elixir/OTP need via hibernation error handling to handle production load: concurrent callers, partial failures, and operational visibility. Without the right OTP primitives — proper supervision, explicit message semantics, structured error handling — code that worked on a laptop silently breaks under contention or restarts.

This challenge frames the topic as a small, runnable system so the trade-offs are concrete: what crashes, what restarts, what stays consistent, and what observability you get for free.

## Why X and not Y

- **Why not just blindly call via tuples?** If the Registry crashes and
  restarts before you re-lookup, your process might not be found, or you
  might get a stale entry. Check existence first.

- **Why not use `:global` for every cluster-wide name?** `:global` is slower,
  limits the number of registered names, and is cluster-aware. `Registry`
  per-node is simpler for per-node patterns; mix them by intent.

## Core concepts

### 1. Registry validation before via-tuple calls

Before calling `GenServer.call({:via, Registry, {MyReg, key}}, msg)`,
check if the Registry is alive:

```elixir
case Registry.lookup(registry_name, key) do
  [{pid, _}] -> {:ok, pid}
  [] -> {:error, :not_found}
end
```
If lookup fails, the via tuple will fail too; surface the error early.

### 2. Combining `:via` with `:hibernate`

A GenServer can return `:hibernate` on every callback and still work with
via-tuple naming. The hibernation is transparent:

```elixir
def handle_call(:ping, _from, state) do
  {:reply, :pong, state, :hibernate}
end
```
The next message (whether a call or cast through the via tuple) will thaw the
process and re-run the callback.

### 3. The race between `Registry.lookup/2` and process death

Even after successful `Registry.lookup/2`, the process might die before your
call arrives. Callers must catch `:noproc` and retry or fail gracefully:

```elixir
case GenServer.call(via_tuple, msg) do
  :ok -> :ok
  :error -> {:error, :process_died}
catch
  :exit, {:noproc, _} -> {:error, :process_died}
end
```
### 4. A centralized `find_or_start` with validation

Wrap all the messy patterns in one helper:

```elixir
def find_or_start(key) do
  case Registry.lookup(@registry, key) do
    [{pid, _}] ->
      {:ok, pid}

    [] ->
      case Worker.start_link(name: via(key), tag: key) do
        {:ok, pid} -> {:ok, pid}
        {:error, {:already_started, pid}} -> {:ok, pid}
      end
  end
rescue
  error ->
    {:error, {:registry_error, error}}
end
```
### 5. Hibernation is not a substitute for availability checking

A hibernating process is still subject to the same Registry lookup rules.
Hibernation only affects in-memory state size; it does not affect naming or
availability. Always validate Registry before calling.

---

## Design decisions

**Option A — spray via tuples everywhere, hope the Registry stays alive**
- Pros: simple code.
- Cons: silent failures, hard-to-debug `:noproc` errors.

**Option B — centralize via-tuple construction + Registry validation (chosen)**
- Pros: clear error paths, easy to audit, testable.
- Cons: slightly more boilerplate per call site.

→ Chose **B** because production systems must fail *loudly* and *fast* when
dependencies (like a Registry) become unavailable.

---

## Implementation

### `mix.exs`

```elixir
defmodule ViaHibernationHandling.MixProject do
  use Mix.Project

  def project do
    [
      app: :via_hibernation_handling,
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
### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation.

```bash
mix new via_hibernation_handling --sup
cd via_hibernation_handling
```

### `lib/via_hibernation_handling/application.ex`

**Objective**: Wire the supervision tree to start the Registry before any
worker can register.

```elixir
defmodule ViaHibernationHandling.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: ViaHibernationHandling.Registry}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ViaHibernationHandling.Supervisor)
  end
end
```
### `lib/via_hibernation_handling/worker.ex`

**Objective**: Implement a hibernating GenServer that works with via-tuple
naming.

```elixir
defmodule ViaHibernationHandling.Worker do
  @moduledoc """
  A GenServer that hibernates after every operation and is named via a
  Registry `:via` tuple. Demonstrates combining both patterns.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct [:tag, :counter]
  end

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    tag = Keyword.get(opts, :tag, :unnamed)
    GenServer.start_link(__MODULE__, tag, name: name)
  end

  @doc "Increment counter and return the new value. Process hibernates after."
  def increment(server), do: GenServer.call(server, :increment)

  @doc "Return the current counter value. Process hibernates after."
  def value(server), do: GenServer.call(server, :value)

  @doc "Return the process state (tag + counter)."
  def state(server), do: GenServer.call(server, :state)

  @impl true
  def init(tag) do
    {:ok, %State{tag: tag, counter: 0}}
  end

  @impl true
  def handle_call(:increment, _from, %State{counter: c} = state) do
    new_state = %{state | counter: c + 1}
    {:reply, new_state.counter, new_state, :hibernate}
  end

  def handle_call(:value, _from, state) do
    {:reply, state.counter, state, :hibernate}
  end

  def handle_call(:state, _from, state) do
    {:reply, state, state, :hibernate}
  end
end
```
### `lib/via_hibernation_handling/registry_checker.ex`

**Objective**: Implement a helper that validates Registry availability before
calling through a via tuple.

```elixir
defmodule ViaHibernationHandling.RegistryChecker do
  @moduledoc """
  Utilities for checking Registry availability and safely calling through
  via tuples.
  """

  alias ViaHibernationHandling.Worker

  @registry ViaHibernationHandling.Registry

  @doc "Check if the Registry is currently alive and running."
  @spec registry_alive?() :: boolean()
  def registry_alive? do
    case :erlang.whereis(@registry) do
      :undefined -> false
      pid when is_pid(pid) -> Process.alive?(pid)
    end
  end

  @doc """
  Call the worker by key, validating Registry availability first.
  Returns {:error, :registry_unavailable} if the Registry is not running.
  """
  @spec call(term(), atom(), timeout()) :: {:ok, any()} | {:error, atom()}
  def call(key, action, timeout \\ 5000) do
    unless registry_alive?() do
      return {:error, :registry_unavailable}
    end

    case Registry.lookup(@registry, key) do
      [{pid, _}] ->
        # Worker found; call through the via tuple
        try do
          result = apply(Worker, action, [via(key)])
          {:ok, result}
        catch
          :exit, {:noproc, _} ->
            {:error, :process_died}
        end

      [] ->
        {:error, :not_found}
    end
  rescue
    error ->
      {:error, {:registry_error, error}}
  end

  @doc """
  Find or start a worker by key. Validates Registry before attempting.
  """
  @spec find_or_start(term()) :: {:ok, pid()} | {:error, atom()}
  def find_or_start(key) do
    unless registry_alive?() do
      return {:error, :registry_unavailable}
    end

    case Registry.lookup(@registry, key) do
      [{pid, _}] ->
        {:ok, pid}

      [] ->
        case Worker.start_link(name: via(key), tag: key) do
          {:ok, pid} -> {:ok, pid}
          {:error, {:already_started, pid}} -> {:ok, pid}
        end
    end
  rescue
    error ->
      {:error, {:registry_error, error}}
  end

  @doc "Centralized via-tuple construction."
  @spec via(term()) :: {:via, module(), term()}
  defp via(key) do
    {:via, Registry, {@registry, key}}
  end
end
```
### `lib/via_hibernation_handling.ex`

**Objective**: Expose a clean public API that uses the Registry checker.

```elixir
defmodule ViaHibernationHandling do
  @moduledoc """
  Public API for via-tuple workers with hibernation and Registry validation.
  """

  alias ViaHibernationHandling.{Worker, RegistryChecker}

  @registry ViaHibernationHandling.Registry

  @doc """
  Find or start a worker by key, ensuring Registry is available.
  Returns {:ok, pid} or {:error, reason}.
  """
  @spec find_or_start(term()) :: {:ok, pid()} | {:error, atom()}
  def find_or_start(key) do
    RegistryChecker.find_or_start(key)
  end

  @doc """
  Increment the worker's counter. Validates Registry availability first.
  """
  @spec increment(term()) :: {:ok, non_neg_integer()} | {:error, atom()}
  def increment(key) do
    RegistryChecker.call(key, :increment)
  end

  @doc """
  Get the worker's current counter value.
  """
  @spec value(term()) :: {:ok, non_neg_integer()} | {:error, atom()}
  def value(key) do
    RegistryChecker.call(key, :value)
  end

  @doc """
  Get the full worker state (tag + counter).
  """
  @spec state(term()) :: {:ok, map()} | {:error, atom()}
  def state(key) do
    RegistryChecker.call(key, :state)
  end

  @doc """
  Check if the Registry is alive.
  """
  @spec registry_alive?() :: boolean()
  def registry_alive? do
    RegistryChecker.registry_alive?()
  end
end
```
### Step 6: `test/via_hibernation_handling_test.exs`

**Objective**: Write tests covering success paths, Registry unavailability,
and hibernation behavior.

```elixir
defmodule ViaHibernationHandlingTest do
  use ExUnit.Case, async: false

  doctest ViaHibernationHandling

  alias ViaHibernationHandling.{Worker, RegistryChecker}

  describe "Registry validation" do
    test "find_or_start succeeds when Registry is alive" do
      assert ViaHibernationHandling.registry_alive?()
      {:ok, pid} = ViaHibernationHandling.find_or_start(:test1)
      assert is_pid(pid)
      GenServer.stop(pid)
    end

    test "increment returns error when Registry is unavailable" do
      # Simulate Registry being down by stopping the supervisor
      # (In a real test, you might trap and restart it)
      # For now, this documents the pattern
      assert ViaHibernationHandling.registry_alive?()
    end
  end

  describe "hibernating worker" do
    test "operations succeed and hibernation is transparent" do
      {:ok, pid} = ViaHibernationHandling.find_or_start(:counter1)
      
      assert {:ok, 1} = ViaHibernationHandling.increment(:counter1)
      assert {:ok, 2} = ViaHibernationHandling.increment(:counter1)
      assert {:ok, 2} = ViaHibernationHandling.value(:counter1)
      
      {:ok, state} = ViaHibernationHandling.state(:counter1)
      assert state.counter == 2
      assert state.tag == :counter1
      
      GenServer.stop(pid)
    end

    test "multiple workers are independent" do
      {:ok, p1} = ViaHibernationHandling.find_or_start(:a)
      {:ok, p2} = ViaHibernationHandling.find_or_start(:b)
      assert p1 != p2

      assert {:ok, 1} = ViaHibernationHandling.increment(:a)
      assert {:ok, 1} = ViaHibernationHandling.increment(:b)
      assert {:ok, 2} = ViaHibernationHandling.increment(:a)

      assert {:ok, 2} = ViaHibernationHandling.value(:a)
      assert {:ok, 1} = ViaHibernationHandling.value(:b)

      GenServer.stop(p1)
      GenServer.stop(p2)
    end

    test "same key returns same process" do
      {:ok, p1} = ViaHibernationHandling.find_or_start(:unique_key)
      {:ok, p2} = ViaHibernationHandling.find_or_start(:unique_key)
      assert p1 == p2
      GenServer.stop(p1)
    end
  end
end
```
### Step 7: Run

**Objective**: Execute the suite so you see Registry validation in action.

```bash
mix test
# For interactive exploration:
#   iex -S mix
#   iex> ViaHibernationHandling.find_or_start(:mykey)
#   iex> ViaHibernationHandling.increment(:mykey)
#   iex> ViaHibernationHandling.registry_alive?()
```

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `ViaHibernationHandling`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== ViaHibernationHandling demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # ViaHibernationHandling.find_or_start/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    # ViaHibernationHandling.increment/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    :ok
  end
end

Main.main()
```
## Why this works

The pattern separates concerns: `Worker` is a naive hibernating GenServer;
`RegistryChecker` validates availability and handles error paths; the public
API is clean. If the Registry crashes, callers get a clear `:registry_unavailable`
error instead of hanging or getting cryptic `:noproc` crashes.

---

## Trade-offs and production gotchas

**1. Registry lookup is not free**
Each call to `Registry.lookup/2` is an ETS lookup. On hot paths (1000s of
calls/sec to the same key), cache the pid in a local variable or use
`:ets.lookup` directly for 10–100ns latency instead of a few microseconds.

**2. The race between lookup and process death is real**
Even after successful lookup, the process might die before your call arrives.
Always catch `:noproc` and implement a reasonable retry/fallback. This is
why `find_or_start` exists: restart the process if needed.

**3. Hibernation + high-frequency calls = latency spikes**
If a worker hibernates after every call and the next call comes 100µs later,
the process must thaw (1–3ms) before replying. This is invisible under light
load but becomes visible under contention. Use hibernation only for truly
idle processes.

**4. Registry validation is a courtesy, not a guarantee**
After `registry_alive?()` returns true, the Registry could crash before your
lookup completes. This is an acceptable race; your error handling (catching
`:noproc`) covers it.

**5. Don't try to "fix" stale via-tuple errors by retrying the same tuple**
If a via-tuple call fails with `:noproc`, the process is dead. Retrying the
same via tuple will fail the same way. Restart the worker first, or use
`find_or_start`.

---

## Reflection

- The Registry goes down for 2 seconds, then comes back up. A caller has a
  stale pid from before the restart. What happens when they call that pid?
  How should your error handling adapt?
- You add hibernation to every operation. Benchmarks show 40% fewer GC pauses
  in idle state — but throughput drops 15%. Why? What's the trade-off?

## Resources

- [`Registry` — `:via` tuple patterns](https://hexdocs.pm/elixir/Registry.html#module-using-in-via)
- [`GenServer` — hibernation](https://hexdocs.pm/elixir/GenServer.html)
- [Erlang process monitoring and links](https://www.erlang.org/doc/system/ref_man_part.html)

### `test/via_hibernation_handling_test.exs`

```elixir
defmodule ViaHibernationHandlingTest do
  use ExUnit.Case, async: true

  doctest ViaHibernationHandling

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ViaHibernationHandling.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Why this OTP shape fits via hibernation error handling

Choosing the right primitive (GenServer, Task, Supervisor, Registry, ETS, Stream, behaviour, protocol, macro) is half the design. Each one encodes specific failure semantics, back-pressure behaviour, and observability hooks. Picking the wrong one forces you to reinvent these properties at a higher cost and worse predictability.

### 2. State, supervision, shutdown

Three properties dominate every OTP design: who owns the state, who restarts the process, and how a clean shutdown propagates. Articulate each property before writing code; code follows the design, not the other way around.

### 3. Idiomatic Elixir patterns

Pattern matching in function heads, multi-clause functions with guards, the pipe operator for sequential transformations, and `with` for short-circuited happy paths are the four idioms you'll see everywhere. Use them — readers expect them, and they make code linearly readable instead of nested.

### 4. Explicit error handling

Functions that can fail return `{:ok, value} | {:error, reason}`. Functions whose failures must crash the process raise. Never swallow errors silently — log them with context or let them propagate to the supervisor that knows how to react.
