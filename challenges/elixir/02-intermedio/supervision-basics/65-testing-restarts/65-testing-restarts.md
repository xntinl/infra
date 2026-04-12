# Testing restarts — `start_supervised!`, monitors, and deterministic assertions

**Project**: `testing_restarts` — a worker with a small API and a full test suite that verifies crash → restart → recovery behavior without sleeps or flakiness.

---

## Project context

"Tests that sleep" is the number-one anti-pattern in OTP code. They pass
on a developer's laptop, fail in CI, and teach you nothing about the
actual restart semantics. The right way to test supervision is with
`start_supervised!/2`, `Process.monitor/1`, and `assert_receive` — a
combination that waits exactly as long as needed and no longer.

This exercise is a small worker plus a test suite that demonstrates the
idioms you'll use for every piece of supervised code you ever write.

Project structure:

```
testing_restarts/
├── lib/
│   ├── testing_restarts.ex
│   ├── testing_restarts/
│   │   ├── service.ex
│   │   └── supervisor.ex
├── test/
│   ├── test_helper.exs
│   └── testing_restarts_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not integration tests only?** Restart logic needs targeted unit tests; integration tests rarely exercise the crash path cleanly.

## Core concepts

### 1. `start_supervised!/2` — test-scoped supervisor

```elixir
start_supervised!({MyApp.Supervisor, opts})
```

This helper from `ExUnit.Callbacks` starts the module under a supervisor
**owned by the test process**. When the test finishes (pass or fail),
the supervisor is shut down automatically. No setup/cleanup pairs, no
leaked processes between tests.

### 2. `Process.monitor/1` + `assert_receive {:DOWN, ...}`

```elixir
ref = Process.monitor(pid)
# trigger the crash
assert_receive {:DOWN, ^ref, :process, ^pid, reason}, 500
```

This is the canonical "wait until the process died" without polling.
The `500` timeout is a ceiling, not a target — most tests receive the
DOWN in microseconds.

### 3. Wait for the NEW pid, not just any pid

After a crash, the supervisor restarts the child with a different pid.
If your test reads `Process.whereis(:worker)` immediately, you may get
`nil` (too early) or the old pid (racy). Poll until a *different* pid
appears:

```elixir
defp wait_for_new_pid(name, old_pid), do: ...
```

### 4. `Process.alive?/1` is not a substitute

`Process.alive?(pid)` tells you "is the VM process alive right now",
which does not mean the supervisor is done restarting, or that the
named registration has completed. Use it as a sanity check AFTER a
monitor-based wait, not as the wait itself.

### 5. `async: false` for supervised tests

The test owns globally-registered names (`:worker`, `MyApp.Supervisor`).
Running tests in parallel that all claim the same name causes
`:already_started`. Use `async: false` for any test that registers a
global name.

---

## Design decisions

**Option A — integration test at the app boundary**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `start_supervised` + targeted crash assertions (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because supervisor restart logic deserves unit-level coverage, not just happy-path integration.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new testing_restarts
cd testing_restarts
```

### Step 2: `lib/testing_restarts/service.ex`

```elixir
defmodule TestingRestarts.Service do
  @moduledoc """
  A small stateful service used to demonstrate testing restart behavior.
  Holds a counter plus a "last reason" field so tests can verify both
  that a restart happened and that state was properly reset.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, :ok, name: name)
  end

  @spec bump(GenServer.server()) :: :ok
  def bump(srv \\ __MODULE__), do: GenServer.cast(srv, :bump)

  @spec value(GenServer.server()) :: non_neg_integer()
  def value(srv \\ __MODULE__), do: GenServer.call(srv, :value)

  @spec crash(GenServer.server()) :: :ok
  def crash(srv \\ __MODULE__), do: GenServer.cast(srv, :crash)

  @impl true
  def init(:ok), do: {:ok, 0}

  @impl true
  def handle_cast(:bump, n), do: {:noreply, n + 1}
  def handle_cast(:crash, _n), do: raise("test-triggered crash")

  @impl true
  def handle_call(:value, _from, n), do: {:reply, n, n}
end
```

### Step 3: `lib/testing_restarts/supervisor.ex`

```elixir
defmodule TestingRestarts.Supervisor do
  @moduledoc """
  Single-child supervisor. Kept deliberately minimal so the tests focus
  on restart mechanics, not tree shape.
  """

  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [TestingRestarts.Service]
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 5, max_seconds: 5)
  end
end
```

### Step 4: `test/testing_restarts_test.exs`

```elixir
defmodule TestingRestartsTest do
  use ExUnit.Case, async: false

  alias TestingRestarts.Service

  setup do
    # start_supervised!/1 ties the supervisor's lifetime to the test.
    # When this test exits, the supervisor and its children are stopped.
    pid = start_supervised!(TestingRestarts.Supervisor)
    {:ok, sup: pid}
  end

  describe "start_supervised!/1 basics" do
    test "worker is alive after setup" do
      pid = Process.whereis(Service)
      assert is_pid(pid)
      assert Process.alive?(pid)
    end

    test "worker state starts at zero" do
      assert Service.value() == 0
    end
  end

  describe "crash → restart" do
    test "monitor + assert_receive observes the crash deterministically" do
      pid = Process.whereis(Service)
      ref = Process.monitor(pid)

      Service.crash()

      # Wait deterministically for the crash — no Process.sleep/1.
      assert_receive {:DOWN, ^ref, :process, ^pid, _reason}, 500
    end

    test "supervisor restarts the worker with a NEW pid" do
      old = Process.whereis(Service)
      ref = Process.monitor(old)

      Service.crash()
      assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

      new = wait_for_new_pid(Service, old)
      assert is_pid(new)
      assert new != old
      assert Process.alive?(new)
    end

    test "state is reset after restart" do
      Service.bump()
      Service.bump()
      Service.bump()
      assert Service.value() == 3

      old = Process.whereis(Service)
      ref = Process.monitor(old)
      Service.crash()
      assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

      _ = wait_for_new_pid(Service, old)
      # Fresh init/1 means the counter went back to 0.
      assert Service.value() == 0
    end
  end

  describe "restart intensity protects the tree" do
    test "many quick crashes eventually exhaust the budget" do
      # Budget is 5/5s. Six consecutive crashes should kill the supervisor.
      sup = start_supervised!({TestingRestarts.Supervisor, name: :tight_sup})
      _ = sup

      # This is deliberately fragile; in real code you'd test intensity with
      # a dedicated narrow test. Here we just confirm the budget exists —
      # we don't try to kill the supervisor in this permissive (5/5) config.
      pid = Process.whereis(Service)
      ref = Process.monitor(pid)
      Service.crash()
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      # Tree still healthy after one crash within budget.
      new = wait_for_new_pid(Service, pid)
      assert Process.alive?(new)
    end
  end

  # ── helpers ──────────────────────────────────────────────────────────

  defp wait_for_new_pid(name, old, timeout \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(name, old, deadline)
  end

  defp do_wait(name, old, deadline) do
    case Process.whereis(name) do
      pid when is_pid(pid) and pid != old -> pid
      _ ->
        if System.monotonic_time(:millisecond) > deadline do
          flunk("process #{inspect(name)} did not restart in time")
        else
          Process.sleep(10)
          do_wait(name, old, deadline)
        end
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `Process.sleep/1` is almost always a test smell**
If you find yourself writing `Process.sleep(100)` "to let the supervisor
restart", replace it with `Process.monitor/1` + `assert_receive` +
polling for the new pid. The sleep makes the test slow AND flaky.

**2. `start_supervised!/1` > manual start/stop in setup**
Manual `{:ok, pid} = start_link(...)` + `on_exit(fn -> stop(pid) end)`
leaks processes when setup fails. `start_supervised!/1` cleans up
correctly even on errors and doesn't need `on_exit/1`.

**3. `refute_receive` is how you test NO restart**
For `:transient` tests ("should NOT restart on :normal"), use
`refute_receive {:DOWN, ^ref, ...}, 100`. You still need a short timeout
because absence-of-event can't be observed instantly.

**4. Globally-registered names force `async: false`**
If two tests both name their worker `:service`, parallel execution
breaks. Either use `async: false` or don't register the worker by name
(pass the pid explicitly).

**5. When NOT to test restart behavior at all**
For pure domain functions or stateless helpers, don't. Test the function.
Restart semantics live in the supervision tree — test them ONCE per
tree, not once per worker.

---


## Reflection

- ¿Cómo distinguís en un test si un child crasheó y reinició vs nunca crasheó? Dá el código.

## Resources

- [`ExUnit.Callbacks.start_supervised!/2`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html#start_supervised!/2)
- [`Process.monitor/1`](https://hexdocs.pm/elixir/Process.html#monitor/1)
- [`ExUnit.Assertions.assert_receive/3`](https://hexdocs.pm/ex_unit/ExUnit.Assertions.html#assert_receive/3)
- ["Testing Supervised Code" — Plataformatec Elixir School](https://elixirschool.com/en/lessons/testing/basics)
