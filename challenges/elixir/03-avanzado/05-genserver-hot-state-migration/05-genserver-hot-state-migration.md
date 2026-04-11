# GenServer Hot State Migration & Code Upgrades

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The circuit breaker worker (exercise 01) has been
running in production for six months. You need to add per-service SLA tiers
(`:critical`, `:standard`, `:best_effort`) that affect failure thresholds and recovery
windows. This is a state schema change — the existing `:closed | :open | :half_open`
state needs new fields.

You cannot restart the workers: each one holds in-memory failure history that takes
30+ seconds to rebuild. A rolling restart would temporarily blind the gateway's
circuit detection. You need a hot upgrade.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       └── circuit_breaker/
│           ├── worker.ex          # ← add code_change/3 here
│           └── supervisor.ex
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           └── migration_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## How hot code upgrades work in OTP

The BEAM supports replacing a module's code while the system is running — without
stopping processes. The upgrade flow:

```
v1 code running
  → load v2 module (beam file replaced in memory)
  → :sys.change_code(pid, Module, "1", extra)
      └─ GenServer suspends message processing
           └─ code_change("1", v1_state, extra) → {:ok, v2_state}
                └─ GenServer resumes with v2_state and v2 callbacks
```

Without `code_change/3`, a hot upgrade would crash the GenServer the moment any
v2 callback tries to pattern-match on a field that was renamed or added.

---

## `code_change/3` signature

```elixir
@impl true
def code_change(old_vsn, state, extra) do
  # old_vsn: version string of the OLD code being replaced
  #          {:down, vsn} for a downgrade
  # state:   current process state in the OLD format
  # extra:   arbitrary term passed to :sys.change_code/4
  # Returns: {:ok, new_state} | {:error, reason}
end
```

The function receives state in its **old format** and must return it in the format
expected by the **new callbacks**. This is where data migration happens.

---

## State versioning: the right way

Embedding a version tag in state makes migration chains explicit and unambiguous:

```elixir
# v1 state — no version tag
%{service: "payments", status: :closed, failures: 0}

# v2 state — version tag added
%{version: 2, service: "payments", status: :closed, failures: 0, sla_tier: :standard}

# v3 state — renamed field, added metadata
%{version: 3, service: "payments", circuit: :closed, failures: 0,
  sla_tier: :standard, metadata: %{}}
```

The migration chain pattern:

```elixir
defp migrate(%{version: 3} = state), do: {:ok, state}

defp migrate(%{version: 2} = state) do
  migrate(%{version: 3, circuit: state.status, ...})
end

defp migrate(state) when not is_map_key(state, :version) do
  # v1: no version key — add defaults
  migrate(%{version: 2, sla_tier: :standard} |> Map.merge(state))
end
```

This means a v1 state can reach v3 in a single `code_change/3` call by traversing
the chain.

---

## Implementation

### Step 1: Version 1 state (current production state)

The current `CircuitBreaker.Worker` produces this state:

```elixir
# v1 state (no version key)
%{
  service:     "payments",
  status:      :closed,          # :closed | :open | :half_open
  failures:    0,
  opened_at:   nil,
  hibernations: 0,
  timer_ref:   reference()
}
```

### Step 2: Version 2 state (target after upgrade)

```elixir
# v2 state
%{
  version:     2,
  service:     "payments",
  status:      :closed,
  failures:    0,
  opened_at:   nil,
  hibernations: 0,
  timer_ref:   reference(),
  sla_tier:    :standard,        # new field — :critical | :standard | :best_effort
  upgraded_at: integer()         # monotonic ms when migration ran
}
```

### Step 3: Add `code_change/3` to `CircuitBreaker.Worker`

```elixir
# In lib/api_gateway/circuit_breaker/worker.ex
# Add after the existing callbacks

@vsn "2"

@impl true
def code_change("1", state, _extra) do
  # TODO: migrate v1 → v2
  # 1. Call migrate/1 to transform the state
  # 2. Return {:ok, v2_state}
  #
  # HINT: use a private migrate/1 that pattern-matches on :version key presence
end

@impl true
def code_change({:down, "2"}, state, _extra) do
  # TODO: downgrade v2 → v1
  # Strip :version, :sla_tier, :upgraded_at
  # HINT: Map.drop(state, [:version, :sla_tier, :upgraded_at])
end

@impl true
def code_change(unknown, _state, _extra) do
  {:error, {:unknown_version, unknown}}
end

# ---------------------------------------------------------------------------
# Private migration chain
# ---------------------------------------------------------------------------

defp migrate(%{version: 2} = state), do: {:ok, state}

defp migrate(v1_state) when not is_map_key(v1_state, :version) do
  # TODO: build v2 state from v1
  # Add version: 2, sla_tier: :standard, upgraded_at: now
  # HINT: Map.merge(v1_state, %{version: 2, sla_tier: :standard,
  #                              upgraded_at: System.monotonic_time(:millisecond)})
  #       then call migrate/1 recursively
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/migration_test.exs
defmodule ApiGateway.CircuitBreaker.MigrationTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Worker

  describe "code_change/3 — v1 to v2 upgrade" do
    test "adds sla_tier and version tag to v1 state" do
      {:ok, pid} = Worker.start_link("test-svc")

      # Inject v1 state (no version key, no sla_tier)
      v1_state = %{
        service:      "test-svc",
        status:       :closed,
        failures:     3,
        opened_at:    nil,
        hibernations: 1,
        timer_ref:    make_ref()
      }
      :sys.replace_state(pid, fn _ -> v1_state end)

      # Simulate hot upgrade
      :ok = :sys.change_code(pid, Worker, "1", [])

      v2_state = :sys.get_state(pid)

      assert v2_state.version == 2
      assert v2_state.sla_tier == :standard
      assert is_integer(v2_state.upgraded_at)
      # Existing fields must be preserved
      assert v2_state.failures == 3
      assert v2_state.service == "test-svc"
      assert v2_state.status == :closed
    end

    test "worker remains functional after upgrade" do
      {:ok, pid} = Worker.start_link("post-upgrade-svc")
      :sys.replace_state(pid, fn s -> Map.delete(s, :version) end)
      :ok = :sys.change_code(pid, Worker, "1", [])

      # Worker must still handle normal calls
      assert Worker.status(pid) == :closed
      Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :closed
    end

    test "pid is unchanged after upgrade" do
      {:ok, pid} = Worker.start_link("same-pid-svc")
      :sys.replace_state(pid, fn s -> Map.delete(s, :version) end)
      :ok = :sys.change_code(pid, Worker, "1", [])
      assert Process.alive?(pid)
    end
  end

  describe "code_change/3 — v2 downgrade" do
    test "removes version tag and sla_tier on downgrade" do
      {:ok, pid} = Worker.start_link("downgrade-svc")

      # Start with v2 state
      v2_state = %{
        version:      2,
        service:      "downgrade-svc",
        status:       :open,
        failures:     5,
        opened_at:    System.monotonic_time(:millisecond),
        hibernations: 0,
        sla_tier:     :critical,
        upgraded_at:  System.monotonic_time(:millisecond),
        timer_ref:    make_ref()
      }
      :sys.replace_state(pid, fn _ -> v2_state end)

      # Simulate downgrade
      :ok = :sys.change_code(pid, Worker, {:down, "2"}, [])

      v1_state = :sys.get_state(pid)

      refute Map.has_key?(v1_state, :version)
      refute Map.has_key?(v1_state, :sla_tier)
      refute Map.has_key?(v1_state, :upgraded_at)
      # Critical fields must survive
      assert v1_state.failures == 5
      assert v1_state.status == :open
    end
  end

  describe "code_change/3 — unknown version" do
    test "returns error for unknown old_vsn" do
      {:ok, pid} = Worker.start_link("unknown-svc")
      assert {:error, {:unknown_version, "99"}} =
        :sys.change_code(pid, Worker, "99", [])
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/migration_test.exs --trace
```

### Step 6: Simulate a full upgrade cycle in IEx

```elixir
# In iex -S mix

alias ApiGateway.CircuitBreaker.Worker

# 1. Start a worker, record some state
{:ok, pid} = Worker.start_link("payments")
Worker.record_failure(pid)
Worker.record_failure(pid)
Worker.record_failure(pid)
:sys.get_state(pid)    # inspect current state

# 2. Inject v1 state (no version tag)
:sys.replace_state(pid, fn s -> Map.drop(s, [:version, :sla_tier, :upgraded_at]) end)
:sys.get_state(pid)    # confirm v1 shape

# 3. Hot upgrade
:ok = :sys.change_code(pid, Worker, "1", [])
:sys.get_state(pid)    # confirm v2 shape, failures preserved

# 4. Use the worker normally
Worker.record_failure(pid)
Worker.record_failure(pid)
Worker.status(pid)     # should be :open (5 failures total)

# 5. Downgrade
:ok = :sys.change_code(pid, Worker, {:down, "2"}, [])
:sys.get_state(pid)    # confirm v1 shape, failures still 5
```

---

## Trade-off analysis

| Approach | Downtime | Risk | Complexity |
|----------|----------|------|------------|
| Rolling restart | Brief per pod | Low | Low |
| Hot upgrade without `code_change` | None | High — crash on state mismatch | Medium |
| Hot upgrade with `code_change` | None | Medium — migration bugs | High |
| Blue/green deployment | None | Low | Infrastructure-heavy |
| Versioned state + migration chain | None | Low-medium | Medium |

Reflection question: `code_change/3` runs synchronously inside the suspended
GenServer. What happens if the migration takes 2 seconds (e.g., transforming a
100,000-entry request log)? How would you use the lazy migration pattern to avoid
blocking the process during the upgrade window?

---

## Common production mistakes

**1. Pattern-matching on state shape instead of version tag**
Matching `%{count: c, updated_at: ts}` to detect "v2 state" works until someone adds
`updated_at` to v1 for an unrelated reason, or until v3 also has those fields. Explicit
version tags make migration unambiguous. If legacy state lacks a version field, add one
in the very first migration and carry it forward in all subsequent versions.

**2. Doing expensive work in `code_change/3`**
`code_change/3` runs synchronously and suspends the GenServer. If your state has 1 million
entries and migration transforms each one, you may block the process for seconds — during
which all callers wait with their timeouts counting down. Measure migration time in
staging. If it exceeds ~100 ms, use the lazy pattern: tag state as `migration_pending: true`,
complete migration in `handle_continue` on the first call after upgrade.

**3. Not testing the downgrade path**
Downgrade is triggered when a deployment is rolled back under production pressure.
Teams routinely discover that `code_change({:down, vsn}, ...)` was never implemented
or returns incorrect state, making the rollback worse than the original problem.
Always implement AND test both directions. Include downgrade tests in your normal
test suite, not just integration tests.

**4. Assuming `:sys.change_code` updates all processes**
`:sys.change_code/4` affects a single process. In a cluster with 50,000 GenServer
instances, you need to call it on each one. OTP release tools (via `.appup` files)
automate this for supervised processes. For manually managed processes, you must
iterate and call `:sys.change_code` yourself, or design your supervisor to restart
workers when new code is loaded.

---

## Resources

- [OTP docs — `gen_server:code_change/3`](https://www.erlang.org/doc/man/gen_server.html#Module:code_change-3)
- [Erlang — `:sys` module](https://www.erlang.org/doc/man/sys.html)
- [OTP Design Principles — Release Handling](https://www.erlang.org/doc/design_principles/release_handling.html)
- [Saša Jurić — Elixir in Action, 2nd ed.](https://www.manning.com/books/elixir-in-action-second-edition) — ch. 13, running a system
- [Mix.Release — HexDocs](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
