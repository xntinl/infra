# 5. GenServer Hot State Migration & Code Upgrades

**Difficulty**: Avanzado

## Prerequisites
- Mastered: GenServer lifecycle, OTP release concepts
- Mastered: Pattern matching on complex map/tuple structures
- Familiarity with: Erlang hot code loading, `:sys` module, Distillery/Mix Release appups

## Learning Objectives
- Analyze the difference between cold restarts and hot code upgrades in production
- Design versioned state structures that support backward-compatible migration
- Implement `code_change/3` to transform state between software versions
- Evaluate when `:sys.change_code/4` is appropriate vs. a rolling restart

## Concepts

### Hot Code Upgrades in OTP

The BEAM supports replacing a module's code while the system is running — without
stopping processes. This is one of OTP's most powerful features and the basis for
the "nine nines" availability claims in Erlang/OTP systems. The upgrade flow:

1. New module version is compiled and loaded into the VM
2. `:sys.change_code/4` is called on a GenServer pid
3. The GenServer stops processing messages, calls `code_change/3`, then resumes
4. Existing state is transformed by `code_change/3` to match the new code's expectations

```
v1 code running → load v2 module → :sys.change_code(pid, Module, "1", extra)
  └─ GenServer suspends message processing
       └─ code_change("1", old_state, extra) → {:ok, new_state}
            └─ GenServer resumes with new_state and v2 callbacks
```

Without `code_change/3`, a hot upgrade would crash the GenServer the moment any
callback tries to pattern-match on state fields that were added, removed, or renamed.

### code_change/3 Signature

```elixir
def code_change(old_vsn, state, extra) do
  # old_vsn: the version string of the OLD code being replaced
  #           or {:down, vsn} for a downgrade
  # state:   the current process state (in the OLD format)
  # extra:   arbitrary term passed to :sys.change_code/4
  # Returns: {:ok, new_state} | {:error, reason}
end
```

The function receives the state in its OLD format and must return it in the format
expected by the NEW callbacks. This is where data migration happens.

```elixir
# v1 state: %{count: integer()}
# v2 state: %{count: integer(), updated_at: DateTime.t()}

def code_change("1", %{count: count}, _extra) do
  new_state = %{
    count: count,
    updated_at: DateTime.utc_now()
  }
  {:ok, new_state}
end

# Downgrade: v2 → v1
def code_change({:down, "2"}, %{count: count, updated_at: _}, _extra) do
  {:ok, %{count: count}}
end
```

### State Versioning Pattern

In long-running systems, a GenServer may go through multiple versions. Embedding a
version tag in state makes `code_change/3` explicit and avoids brittle pattern
matching on field presence:

```elixir
# v1
%{version: 1, data: %{user_id: id}}

# v2 — added :preferences field
%{version: 2, data: %{user_id: id, preferences: %{}}}

# v3 — renamed :data to :payload, added :metadata
%{version: 3, payload: %{user_id: id, preferences: %{}}, metadata: %{}}
```

`code_change` then becomes a version chain:

```elixir
def code_change(old_vsn, state, extra) do
  migrate(state)
end

defp migrate(%{version: 3} = state), do: {:ok, state}

defp migrate(%{version: 2} = state) do
  migrated = %{
    version: 3,
    payload: state.data,
    metadata: %{migrated_from: 2, migrated_at: DateTime.utc_now()}
  }
  migrate(migrated)
end

defp migrate(%{version: 1} = state) do
  migrated = %{
    version: 2,
    data: Map.put(state.data, :preferences, %{})
  }
  migrate(migrated)
end
```

This chained migration means a v1 state can be upgraded to v3 in one
`code_change/3` call by traversing the migration chain.

### :sys Module — Key Functions

The `:sys` module provides standardized hooks for OTP-compliant processes:

```elixir
# Trigger code_change on a live process
:sys.change_code(pid_or_name, Module, old_vsn_string, extra_term)

# Suspend a GenServer (stops processing messages, keeps mailbox)
:sys.suspend(pid)

# Resume a suspended GenServer
:sys.resume(pid)

# Inspect current state (uses sys:get_status internally)
:sys.get_state(pid)

# Replace state directly (useful for testing)
:sys.replace_state(pid, fn old_state -> new_state end)
```

`:sys.change_code/4` is the function you call manually during a live upgrade
or in tests to simulate what a release upgrade would do.

### Trade-offs

| Approach | Downtime | Risk | Complexity |
|---|---|---|---|
| Rolling restart | Brief (per-node) | Low | Low |
| Hot code upgrade without `code_change` | None | High (crash on state mismatch) | Medium |
| Hot code upgrade with `code_change` | None | Medium (migration bugs) | High |
| Blue/green deployment | None | Low | Infrastructure-heavy |
| Versioned state + migration chain | None | Low-medium | Medium |

Hot upgrades are most valuable in stateful systems where a restart loses work
(in-progress transactions, large in-memory caches) and the state is well-defined
enough to migrate deterministically. For stateless GenServers, rolling restarts
are simpler and safer.

### Async Migration With handle_continue

For large state migrations, synchronous `code_change/3` blocks the process during
transformation. An alternative: do minimal transformation in `code_change/3` (just
tag the state as needing migration), then complete the migration lazily in
`handle_continue` triggered on the first callback:

```elixir
def code_change(old_vsn, state, _extra) do
  # Mark as needing migration — do not do expensive work here
  {:ok, %{migration_pending: true, raw_state: state, version: old_vsn}}
end

def handle_call(msg, from, %{migration_pending: true} = state) do
  {:ok, migrated} = do_full_migration(state.raw_state)
  # Retry the call with migrated state
  handle_call(msg, from, migrated)
end
```

This is a niche pattern — only warranted when migration takes seconds on large states.
Most migrations should be synchronous in `code_change/3`.

---

## Exercises

### Exercise 1: Basic Field Addition Migration

**Problem**: Your `UserSession` GenServer tracks active sessions. Version 1 state is
`%{user_id: id, token: t, created_at: ts}`. Your new version 2 adds `last_active_at`
(so you can implement session timeout) and `metadata` (for audit logging). You need to
upgrade all running `UserSession` processes without restarting them — users must not
be logged out.

**Requirements**:
- v1 state: `%{user_id: string, token: string, created_at: DateTime.t}`
- v2 state: `%{user_id: string, token: string, created_at: DateTime.t, last_active_at: DateTime.t, metadata: map}`
- `code_change("1", v1_state, _extra)` migrates v1 → v2
- Downgrade `{:down, "2"}`: strip added fields, return v1 state
- Test: start a process with v1 state using `:sys.replace_state`, then call
  `:sys.change_code`, then verify the process has v2 state via `:sys.get_state`

**Hints**:
- You cannot literally run two different Elixir module versions in a single-node test —
  simulate it: start the GenServer normally, use `:sys.replace_state/2` to inject a v1
  state map, then call `:sys.change_code/4` to trigger `code_change/3`
- `code_change/3` must be public (it is called via the `:sys` mechanism)
- For `last_active_at` in migration: use `created_at` as a reasonable default —
  we do not know when the session was last active, so assume it was on creation
- Verify the downgrade path explicitly — downgrade bugs in production are the worst
  kind because they manifest under pressure

**One possible solution**:
```elixir
defmodule UserSession do
  use GenServer

  # Current (v2) version string — used in .appup files
  @vsn "2"

  def start_link(user_id) do
    GenServer.start_link(__MODULE__, user_id)
  end

  def touch(pid), do: GenServer.cast(pid, :touch)
  def info(pid), do: GenServer.call(pid, :info)

  def init(user_id) do
    state = %{
      user_id: user_id,
      token: generate_token(),
      created_at: DateTime.utc_now(),
      last_active_at: DateTime.utc_now(),
      metadata: %{}
    }
    {:ok, state}
  end

  def handle_cast(:touch, state) do
    {:noreply, %{state | last_active_at: DateTime.utc_now()}}
  end

  def handle_call(:info, _from, state) do
    {:reply, state, state}
  end

  # Upgrade: v1 → v2
  def code_change("1", %{user_id: _, token: _, created_at: _} = old_state, _extra) do
    new_state = Map.merge(old_state, %{
      last_active_at: old_state.created_at,
      metadata: %{}
    })
    {:ok, new_state}
  end

  # Downgrade: v2 → v1
  def code_change({:down, "2"}, state, _extra) do
    v1_state = Map.take(state, [:user_id, :token, :created_at])
    {:ok, v1_state}
  end

  defp generate_token do
    :crypto.strong_rand_bytes(16) |> Base.encode64()
  end
end
```

---

### Exercise 2: Multi-Version Migration Chain

**Problem**: Your `CounterService` GenServer has existed for three years and has gone
through three state shapes:
- v1: `%{count: integer}`
- v2: `%{count: integer, updated_at: integer}` (monotonic ms)
- v3: `%{version: 3, count: integer, updated_at: integer, tags: list}`

A production cluster has some nodes on v1, most on v2, and you are deploying v3.
During a rolling upgrade, a v3 node may receive `code_change` for a process that was
migrated from v1 to v2 and never upgraded to v3. Implement a migration chain that
handles all three starting versions.

**Requirements**:
- `code_change("1", v1_state, _)` → v3 state (via chain through v2)
- `code_change("2", v2_state, _)` → v3 state
- Any other `old_vsn` → `{:error, {:unknown_version, old_vsn}}`
- Downgrade: `{:down, "3"}` → v2 state (strip `version` tag and `tags`)
- Test all three migration paths independently

**Hints**:
- The migration chain pattern: `code_change` calls a private `migrate/1` function
  that pattern-matches on the state shape (or version tag) and calls itself
  recursively until it reaches the target version
- In v3, the version tag is INSIDE the state (`%{version: 3, ...}`), not in a wrapper
  tuple. v1 and v2 states do NOT have the `:version` key — use `Map.has_key?/2` or
  match on struct shape to distinguish them
- For `updated_at` in v1→v2 migration: use `System.monotonic_time(:millisecond)` as
  the best available approximation of "when was this state last updated"
- `tags` in v3 defaults to `[]` on upgrade — new feature, no historical data

**One possible solution**:
```elixir
defmodule CounterService do
  use GenServer

  @vsn "3"

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def increment, do: GenServer.cast(__MODULE__, :increment)
  def get, do: GenServer.call(__MODULE__, :get)
  def tag(label), do: GenServer.cast(__MODULE__, {:tag, label})

  def init(_) do
    {:ok, %{version: 3, count: 0, updated_at: now(), tags: []}}
  end

  def handle_cast(:increment, state) do
    {:noreply, %{state | count: state.count + 1, updated_at: now()}}
  end

  def handle_cast({:tag, label}, state) do
    {:noreply, %{state | tags: [label | state.tags]}}
  end

  def handle_call(:get, _from, state) do
    {:reply, state, state}
  end

  def code_change(old_vsn, state, _extra) when old_vsn in ["1", "2"] do
    {:ok, migrate(state)}
  end

  def code_change({:down, "3"}, state, _extra) do
    v2 = %{count: state.count, updated_at: state.updated_at}
    {:ok, v2}
  end

  def code_change(unknown, _state, _extra) do
    {:error, {:unknown_version, unknown}}
  end

  # Migration chain — idempotent, terminates at version 3
  defp migrate(%{version: 3} = state), do: state

  # v2 → v3: add version tag and tags list
  defp migrate(%{count: c, updated_at: ts} = state) when not is_map_key(state, :version) do
    migrate(%{version: 3, count: c, updated_at: ts, tags: []})
  end

  # v1 → v2: add updated_at, then continue chain
  defp migrate(%{count: c} = state) when not is_map_key(state, :updated_at) do
    migrate(%{count: c, updated_at: now()})
  end

  defp now, do: System.monotonic_time(:millisecond)
end
```

---

### Exercise 3: Live Migration on a Running Node

**Problem**: Simulate a production hot upgrade using `:sys.change_code/4`. You have
`CounterService` from Exercise 2 running on v2. Without stopping the process, migrate
it to v3, verify the state transformation, perform 10 increments on v3, then downgrade
back to v2 and verify backward compatibility. This exercise simulates what Erlang
release tools (relup, appup) do during a rolling upgrade.

**Requirements**:
- Start `CounterService` in v2 state (inject via `:sys.replace_state`)
- Call `:sys.change_code/4` to trigger the v2 → v3 migration
- Verify state has `:version`, `:tags`, `:updated_at` after migration
- Perform 10 `increment` calls — verify count is correct
- Trigger downgrade: `:sys.change_code(pid, CounterService, {:down, "3"}, [])`
- Verify state is back to v2 shape (no `:version` key, no `:tags`)

**Hints**:
- `:sys.replace_state(pid, fun)` calls `fun.(current_state)` and replaces state
  with the return value — use it to inject the v2 state map
- `:sys.change_code(pid, Module, old_vsn, extra)` calls `Module.code_change(old_vsn, state, extra)`
  — the `old_vsn` argument is passed directly to `code_change/3`
- After `:sys.change_code`, immediately call `:sys.get_state(pid)` to inspect
  the transformed state
- The process does NOT restart during this operation — its pid remains the same.
  This is the core promise of hot code upgrades
- Wrap the whole sequence in a test module with descriptive assertions

**One possible solution**:
```elixir
defmodule HotUpgradeSimulation do
  @doc """
  Simulates a live v2 → v3 → v2 upgrade cycle on a running CounterService.
  Run this as: HotUpgradeSimulation.run()
  """
  def run do
    {:ok, pid} = CounterService.start_link([])

    # Step 1: Inject v2 state (as if the process was never upgraded beyond v2)
    v2_state = %{count: 42, updated_at: System.monotonic_time(:millisecond)}
    :sys.replace_state(pid, fn _current -> v2_state end)

    assert_state(pid, fn state ->
      assert state == v2_state
      IO.puts("v2 state confirmed: #{inspect(state)}")
    end)

    # Step 2: Simulate v2 → v3 hot upgrade
    :ok = :sys.change_code(pid, CounterService, "2", [])

    assert_state(pid, fn state ->
      assert state.version == 3
      assert state.count == 42
      assert state.tags == []
      assert Map.has_key?(state, :updated_at)
      IO.puts("v3 state confirmed after upgrade: #{inspect(state)}")
    end)

    # Step 3: Use the process normally under v3
    for _ <- 1..10, do: CounterService.increment()

    assert_state(pid, fn state ->
      assert state.count == 52
      IO.puts("v3 post-increment state: count=#{state.count}")
    end)

    # Step 4: Simulate downgrade v3 → v2
    :ok = :sys.change_code(pid, CounterService, {:down, "3"}, [])

    assert_state(pid, fn state ->
      refute Map.has_key?(state, :version)
      refute Map.has_key?(state, :tags)
      assert state.count == 52
      IO.puts("v2 state confirmed after downgrade: #{inspect(state)}")
    end)

    IO.puts("\nHot upgrade simulation complete. Process pid: #{inspect(pid)}")
    :ok
  end

  defp assert_state(pid, fun) do
    state = :sys.get_state(pid)
    fun.(state)
  end
end
```

---

## Common Mistakes

### Mistake: Pattern Matching on State Shape Instead of Version Tag

Matching `%{count: c, updated_at: ts}` to detect v2 state works — until someone adds
`updated_at` to v1 for unrelated reasons, or v3 also has those fields. Explicit version
tags (`%{version: 2, ...}`) make migration unambiguous. If your legacy state lacks a
version field, add one in the first migration and include it in all future versions.

### Mistake: Doing Expensive Work in code_change/3

`code_change/3` runs synchronously and blocks the GenServer. If your state is a map
with 1 million entries and migration requires transforming each entry, you may block
the process for seconds — during which all callers wait. Measure migration time in
a staging environment. If it exceeds ~100ms, consider the lazy migration pattern
(tag state as pending, migrate on first access).

### Mistake: Not Testing the Downgrade Path

Downgrade is triggered when a deployment is rolled back. Under production stress,
teams often discover that `code_change({:down, vsn}, ...)` was never implemented
or returns incorrect state. This makes the rollback worse than the original problem.
Always implement AND test both directions. Test them as part of your normal test suite,
not just in integration tests.

### Mistake: Assuming :sys.change_code Updates All Processes

`:sys.change_code/4` affects a single process identified by pid or name. In a cluster
with 10,000 GenServer instances, you need to call it on each one. OTP release tools
(via `.appup` files) automate this for supervised processes. For manually managed
processes (e.g., ETS-registered workers), you must iterate and call `:sys.change_code`
yourself, or design your supervisor to restart workers when the new code is loaded.

---

## Summary
- `code_change/3` receives the OLD state and must return the NEW state; the callback
  itself is compiled with the new module version — do not confuse which code runs
- Version tags in state make migration chains explicit and safe — always embed `version`
- Test both upgrade AND downgrade paths before deploying to production
- `:sys.change_code/4` is for single-process upgrades; release tools manage fleets
- Expensive migrations should be flagged and deferred to `handle_continue` to avoid
  blocking the process during the upgrade window

## What's Next
This completes the advanced GenServer series. Suggested next area: OTP Supervisors —
`DynamicSupervisor`, `PartitionSupervisor`, supervision tree design, and restart
strategies for the patterns you built in these five exercises.

## Resources
- OTP docs — `gen_server:code_change/3`: https://www.erlang.org/doc/man/gen_server.html#Module:code_change-3
- Erlang — `:sys` module: https://www.erlang.org/doc/man/sys.html
- OTP Design Principles — Release Handling: https://www.erlang.org/doc/design_principles/release_handling.html
- Saša Jurić — "Elixir in Action" 2nd ed., ch. 13 (running a system)
- Erlang in Production — Hot code upgrades (Erlang Solutions blog)
