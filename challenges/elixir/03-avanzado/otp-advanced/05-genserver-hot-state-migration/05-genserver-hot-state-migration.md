# GenServer Hot State Migration & Code Upgrades

## Goal

Add `code_change/3` to a circuit breaker worker so its state can be upgraded from v1 to v2 (adding SLA tier fields) without restarting the process. This preserves in-memory failure history that takes 30+ seconds to rebuild. The implementation includes both the upgrade path (v1 -> v2) and the downgrade path (v2 -> v1) for safe rollbacks.

---

## How hot code upgrades work in OTP

The BEAM supports replacing a module's code while the system is running. The upgrade flow:

```
v1 code running
  -> load v2 module
  -> :sys.change_code(pid, Module, "1", extra)
      -> GenServer suspends message processing
           -> code_change("1", v1_state, extra) -> {:ok, v2_state}
                -> GenServer resumes with v2_state and v2 callbacks
```

Without `code_change/3`, a hot upgrade would crash the GenServer the moment any v2 callback tries to pattern-match on a field that was renamed or added.

---

## State versioning

Embedding a version tag in state makes migration chains explicit:

```elixir
# v1 state -- no version tag
%{service: "payments", status: :closed, failures: 0}

# v2 state -- version tag added
%{version: 2, service: "payments", status: :closed, failures: 0, sla_tier: :standard}
```

The migration chain pattern allows a v1 state to reach any future version in a single `code_change/3` call by traversing the chain.

---

## Full implementation

### `lib/api_gateway/circuit_breaker/worker.ex`

The worker implements a circuit breaker state machine with `code_change/3` for hot upgrades. It includes the full state machine (`:closed`, `:open`, `:half_open`), heartbeat via `:timer.send_interval`, and migration logic.

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer
  require Logger

  @vsn "2"
  @failure_threshold   5
  @recovery_window_ms  30_000
  @heartbeat_ms        30_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name)
  end

  @spec record_success(pid()) :: :ok
  def record_success(pid), do: GenServer.cast(pid, :success)

  @spec record_failure(pid()) :: :ok
  def record_failure(pid), do: GenServer.cast(pid, :failure)

  @spec status(pid()) :: :closed | :open | :half_open
  def status(pid), do: GenServer.call(pid, :status)

  @spec ping(pid()) :: :pong
  def ping(pid), do: GenServer.call(pid, :ping, 1_000)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(service_name) do
    {:ok, timer_ref} = :timer.send_interval(@heartbeat_ms, :heartbeat)

    state = %{
      version: 2,
      service: service_name,
      status: :closed,
      failures: 0,
      opened_at: nil,
      hibernations: 0,
      timer_ref: timer_ref,
      sla_tier: :standard,
      upgraded_at: nil
    }

    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state}
  end

  @impl true
  def handle_call(:ping, _from, state) do
    {:reply, :pong, state}
  end

  @impl true
  def handle_cast(:success, state) do
    new_status =
      case state.status do
        :half_open -> :closed
        other -> other
      end

    {:noreply, %{state | failures: 0, status: new_status}}
  end

  @impl true
  def handle_cast(:failure, state) do
    new_failures = state.failures + 1

    new_state =
      case state.status do
        :closed when new_failures >= @failure_threshold ->
          %{state | failures: new_failures, status: :open, opened_at: System.monotonic_time(:millisecond)}

        :half_open ->
          %{state | failures: new_failures, status: :open, opened_at: System.monotonic_time(:millisecond)}

        _ ->
          %{state | failures: new_failures}
      end

    {:noreply, new_state}
  end

  @impl true
  def handle_info(:heartbeat, %{status: :open} = state) do
    elapsed = System.monotonic_time(:millisecond) - state.opened_at

    if elapsed >= @recovery_window_ms do
      {:noreply, %{state | status: :half_open}}
    else
      {:noreply, state}
    end
  end

  @impl true
  def handle_info(:heartbeat, state) do
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    if Map.has_key?(state, :timer_ref) and state.timer_ref != nil do
      :timer.cancel(state.timer_ref)
    end
    :ok
  end

  # ---------------------------------------------------------------------------
  # Hot code upgrade -- code_change/3
  # ---------------------------------------------------------------------------

  @impl true
  def code_change("1", state, _extra) do
    migrate(state)
  end

  @impl true
  def code_change({:down, "2"}, state, _extra) do
    # Downgrade v2 -> v1: strip fields that v1 does not expect.
    v1_state = Map.drop(state, [:version, :sla_tier, :upgraded_at])
    {:ok, v1_state}
  end

  @impl true
  def code_change(unknown, _state, _extra) do
    {:error, {:unknown_version, unknown}}
  end

  # ---------------------------------------------------------------------------
  # Private migration chain
  # ---------------------------------------------------------------------------

  # Terminal case: state is already at target version.
  defp migrate(%{version: 2} = state), do: {:ok, state}

  # v1 -> v2: add version tag, SLA tier, and upgrade timestamp.
  # v1 state has no :version key.
  defp migrate(v1_state) when not is_map_key(v1_state, :version) do
    v2_state = Map.merge(v1_state, %{
      version: 2,
      sla_tier: :standard,
      upgraded_at: System.monotonic_time(:millisecond)
    })

    migrate(v2_state)
  end
end
```

### Tests

```elixir
# test/api_gateway/circuit_breaker/migration_test.exs
defmodule ApiGateway.CircuitBreaker.MigrationTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Worker

  describe "code_change/3 -- v1 to v2 upgrade" do
    test "adds sla_tier and version tag to v1 state" do
      {:ok, pid} = Worker.start_link("test-svc")

      v1_state = %{
        service:      "test-svc",
        status:       :closed,
        failures:     3,
        opened_at:    nil,
        hibernations: 1,
        timer_ref:    make_ref()
      }
      :sys.replace_state(pid, fn _ -> v1_state end)

      :ok = :sys.change_code(pid, Worker, "1", [])

      v2_state = :sys.get_state(pid)

      assert v2_state.version == 2
      assert v2_state.sla_tier == :standard
      assert is_integer(v2_state.upgraded_at)
      assert v2_state.failures == 3
      assert v2_state.service == "test-svc"
      assert v2_state.status == :closed
    end

    test "worker remains functional after upgrade" do
      {:ok, pid} = Worker.start_link("post-upgrade-svc")
      :sys.replace_state(pid, fn s -> Map.delete(s, :version) end)
      :ok = :sys.change_code(pid, Worker, "1", [])

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

  describe "code_change/3 -- v2 downgrade" do
    test "removes version tag and sla_tier on downgrade" do
      {:ok, pid} = Worker.start_link("downgrade-svc")

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

      :ok = :sys.change_code(pid, Worker, {:down, "2"}, [])

      v1_state = :sys.get_state(pid)

      refute Map.has_key?(v1_state, :version)
      refute Map.has_key?(v1_state, :sla_tier)
      refute Map.has_key?(v1_state, :upgraded_at)
      assert v1_state.failures == 5
      assert v1_state.status == :open
    end
  end

  describe "code_change/3 -- unknown version" do
    test "returns error for unknown old_vsn" do
      {:ok, pid} = Worker.start_link("unknown-svc")
      assert {:error, {:unknown_version, "99"}} =
        :sys.change_code(pid, Worker, "99", [])
    end
  end
end
```

---

## How it works

1. **`@vsn "2"`**: declares the current module version. OTP uses this to determine the `old_vsn` argument passed to `code_change/3`.

2. **Migration chain**: `migrate/1` is recursive. Each step advances the state by one version. v1 (no `:version` key) -> v2 (adds `:version`, `:sla_tier`, `:upgraded_at`). Future v3 would extend the chain naturally.

3. **Downgrade path**: `code_change({:down, "2"}, state, _extra)` strips v2-specific fields to restore v1 compatibility. Critical for safe rollbacks under production pressure.

4. **`:sys.change_code/4`**: suspends the GenServer, calls `code_change/3`, and resumes with the new state. The process keeps its PID, mailbox, and all linked/monitored relationships.

---

## Common production mistakes

**1. Pattern-matching on state shape instead of version tag**
Using `%{count: c, updated_at: ts}` to detect "v2 state" breaks when unrelated changes add the same fields. Explicit version tags make migration unambiguous.

**2. Doing expensive work in `code_change/3`**
`code_change/3` runs synchronously and suspends the GenServer. If migration transforms 1 million entries, you may block the process for seconds. Use the lazy pattern: tag state as `migration_pending: true`, complete migration in `handle_continue`.

**3. Not testing the downgrade path**
Downgrade is triggered when a deployment is rolled back under production pressure. Teams routinely discover that `code_change({:down, vsn}, ...)` was never implemented.

**4. Assuming `:sys.change_code` updates all processes**
`:sys.change_code/4` affects a single process. In a cluster with 50,000 GenServer instances, you must call it on each one.

---

## Resources

- [OTP docs -- `gen_server:code_change/3`](https://www.erlang.org/doc/man/gen_server.html#Module:code_change-3)
- [Erlang -- `:sys` module](https://www.erlang.org/doc/man/sys.html)
- [OTP Design Principles -- Release Handling](https://www.erlang.org/doc/design_principles/release_handling.html)
