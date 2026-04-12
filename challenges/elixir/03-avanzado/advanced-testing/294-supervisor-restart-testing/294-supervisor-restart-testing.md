# Testing Supervisor Restart Strategies

**Project**: `payment_gateway_supervisor` — a supervisor whose restart strategy and intensity are verified by deterministic tests using `Process.monitor/1`.

## Project context

`payment_gateway` has three workers under a single supervisor: `PaymentClient`, `LedgerWriter`,
and `FraudCheck`. The SRE team demands the following guarantees, written into the
architecture doc:

1. If `PaymentClient` crashes, only `PaymentClient` restarts — `LedgerWriter` keeps running.
   (Strategy must be `:one_for_one`.)
2. If three crashes occur within 5 seconds, the supervisor gives up and escalates to its
   parent. (`max_restarts: 3, max_seconds: 5`.)
3. `LedgerWriter` is `restart: :transient` — normal exit does not cause a restart.

These guarantees are load-bearing; they affect correctness and escalation. A supervisor
whose strategy is wrongly set to `:one_for_all` would restart `LedgerWriter` on every
`PaymentClient` failure, losing in-flight batches. Tests must verify the strategy.

```
payment_gateway/
├── lib/
│   └── payment_gateway/
│       ├── supervisor.ex
│       ├── payment_client.ex
│       ├── ledger_writer.ex
│       └── fraud_check.ex
├── test/
│   ├── payment_gateway/
│   │   └── supervisor_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why test the supervisor itself

Most codebases test workers in isolation and trust that "Elixir's supervisor just works".
The supervisor's configuration (strategy, intensity, restart type) is production code
with bugs like any other. An `:one_for_all` vs `:one_for_one` typo is invisible to unit
tests but catastrophic in prod.

## Why `Process.monitor` and not `Process.exit(pid, :kill)` + `sleep`

- **sleep + `Process.whereis`**: flaky; a restart can happen in microseconds or milliseconds.
- **`Process.monitor/1` + `assert_receive {:DOWN, ...}`**: deterministic; the test blocks
  exactly until the restart completes, no longer.

## Core concepts

### 1. Restart strategies
- `:one_for_one` — only the crashed child restarts.
- `:one_for_all` — all children restart when any one crashes.
- `:rest_for_one` — crashed child and all children defined after it restart.

### 2. Restart intensity
`max_restarts` crashes within `max_seconds` cause the supervisor to die and escalate.

### 3. Child restart type
- `:permanent` — always restart.
- `:transient` — restart only on abnormal exit.
- `:temporary` — never restart.

## Design decisions

- **Option A — integration test against the real app supervisor**: realistic but couples
  unrelated concerns; the real app's supervisor holds many children.
- **Option B — standalone test supervisor started per test**: isolated, parameterized,
  fast. Requires an explicit `start_supervised!/1`.

Chosen: **Option B**. Each test gets its own supervisor with `start_supervised!`, which
ExUnit shuts down automatically.

## Implementation

### Dependencies (`mix.exs`)

```elixir
# Uses only stdlib
```

### Step 1: workers (deliberately simple)

```elixir
# lib/payment_gateway/payment_client.ex
defmodule PaymentGateway.PaymentClient do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def crash, do: GenServer.cast(__MODULE__, :crash)
  def stop_normally, do: GenServer.cast(__MODULE__, :stop)

  @impl true
  def init(_), do: {:ok, %{started_at: System.monotonic_time()}}

  @impl true
  def handle_cast(:crash, _), do: raise("boom")
  def handle_cast(:stop, state), do: {:stop, :normal, state}
end
```

```elixir
# lib/payment_gateway/ledger_writer.ex
defmodule PaymentGateway.LedgerWriter do
  use GenServer, restart: :transient

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def stop_normally, do: GenServer.cast(__MODULE__, :stop)
  def crash, do: GenServer.cast(__MODULE__, :crash)

  @impl true
  def init(_), do: {:ok, %{}}
  @impl true
  def handle_cast(:stop, state), do: {:stop, :normal, state}
  def handle_cast(:crash, _), do: raise("ledger down")
end
```

```elixir
# lib/payment_gateway/fraud_check.ex
defmodule PaymentGateway.FraudCheck do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  @impl true
  def init(_), do: {:ok, %{}}
end
```

### Step 2: the supervisor

```elixir
# lib/payment_gateway/supervisor.ex
defmodule PaymentGateway.Supervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, :ok,
      name: Keyword.get(opts, :name, __MODULE__)
    )
  end

  @impl true
  def init(:ok) do
    children = [
      PaymentGateway.PaymentClient,
      PaymentGateway.LedgerWriter,
      PaymentGateway.FraudCheck
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 5
    )
  end
end
```

### Step 3: the tests

```elixir
# test/payment_gateway/supervisor_test.exs
defmodule PaymentGateway.SupervisorTest do
  # async: false because workers use globally-registered names (:name, __MODULE__)
  use ExUnit.Case, async: false

  alias PaymentGateway.{Supervisor, PaymentClient, LedgerWriter, FraudCheck}

  setup do
    start_supervised!(Supervisor)
    :ok
  end

  describe "strategy :one_for_one" do
    test "crashed child is restarted in isolation" do
      original = Process.whereis(PaymentClient)
      ledger_before = Process.whereis(LedgerWriter)

      ref = Process.monitor(original)
      PaymentClient.crash()

      # Wait for the crash deterministically
      assert_receive {:DOWN, ^ref, :process, ^original, _}, 500

      # Small bounded wait for the supervisor to re-register the name
      Process.sleep(20)

      new_pid = Process.whereis(PaymentClient)
      assert new_pid != nil
      assert new_pid != original

      # Critical: siblings MUST not have restarted
      assert Process.whereis(LedgerWriter) == ledger_before
    end

    test "crashing one child does not kill unrelated siblings" do
      fraud_before = Process.whereis(FraudCheck)

      ref = Process.monitor(Process.whereis(PaymentClient))
      PaymentClient.crash()
      assert_receive {:DOWN, ^ref, :process, _, _}, 500

      Process.sleep(20)
      assert Process.whereis(FraudCheck) == fraud_before
    end
  end

  describe "restart type :transient for LedgerWriter" do
    test "normal stop does NOT trigger a restart" do
      original = Process.whereis(LedgerWriter)
      ref = Process.monitor(original)

      LedgerWriter.stop_normally()
      assert_receive {:DOWN, ^ref, :process, ^original, :normal}, 500

      # Supervisor must NOT have restarted on :normal
      Process.sleep(50)
      assert Process.whereis(LedgerWriter) == nil
    end

    test "abnormal crash DOES trigger a restart" do
      original = Process.whereis(LedgerWriter)
      ref = Process.monitor(original)

      LedgerWriter.crash()
      assert_receive {:DOWN, ^ref, :process, ^original, _}, 500

      Process.sleep(20)
      new_pid = Process.whereis(LedgerWriter)
      assert new_pid != nil
      assert new_pid != original
    end
  end

  describe "restart intensity" do
    test "supervisor dies after exceeding max_restarts within max_seconds" do
      sup = Process.whereis(Supervisor)
      sup_ref = Process.monitor(sup)

      # Cause 4 crashes in quick succession (limit is 3)
      for _ <- 1..4 do
        case Process.whereis(PaymentClient) do
          nil ->
            Process.sleep(5)

          pid ->
            ref = Process.monitor(pid)
            PaymentClient.crash()
            receive do
              {:DOWN, ^ref, :process, _, _} -> :ok
            after
              500 -> :ok
            end
        end
      end

      # The supervisor itself must have crashed
      assert_receive {:DOWN, ^sup_ref, :process, ^sup, _}, 1_000
    end
  end
end
```

## Why this works

- `start_supervised!/1` starts the supervisor and links its lifetime to the test's. When
  the test exits, ExUnit stops the supervisor, releasing the globally registered names.
- `Process.monitor/1` gives us a `:DOWN` message as soon as the target exits. Pairing it
  with `assert_receive` removes all guesswork about "has the crash happened yet?".
- Comparing pids before and after is the canonical way to prove a restart happened (the
  name re-registers to a new pid).
- The `Process.sleep(20)` after `:DOWN` is bounded and covers only the tiny window
  between the child's termination and the supervisor's `register_name/2` on the new child.

## Tests

See Step 3. Covers `:one_for_one` isolation, transient restart semantics, and intensity.

## Benchmark

Per-test wall clock should be well under 100ms dominated by supervisor startup:

```elixir
{t, _} = :timer.tc(fn -> ExUnit.run() end)
IO.puts("supervisor suite #{t / 1000}ms")
```

Target: 5 tests in under 500ms.

## Trade-offs and production gotchas

**1. `async: true` with globally registered names**
Two tests starting the same named supervisor collide. Either avoid named registration in
tests (pass `name: nil`) or set `async: false`.

**2. Asserting on `Process.whereis/1 == nil` immediately after `:DOWN`**
There's a small race: the name might still be registered to the dying pid for a few micro-
seconds. Wait for `:DOWN` first, then (optionally) `Process.sleep(20)` before checking
`whereis`.

**3. Forgetting `Process.monitor/1` before triggering the crash**
If you monitor AFTER the crash, the `:DOWN` message is never delivered and `assert_receive`
times out. Always monitor first, trigger second.

**4. `raise` vs `exit(:reason)`**
A `raise` in a GenServer callback causes the process to exit with `{error, ...}` which
supervisor treats as abnormal. `GenServer.stop(pid, :shutdown)` is treated as normal
termination (no restart for `:transient`). Choose the kill signal that reflects the
production scenario you are reproducing.

**5. Testing intensity without `async: false`**
Intensity tests crash the supervisor itself; if the supervisor is shared across tests
running in parallel, the side effects are chaotic. Always `async: false` for intensity.

**6. When NOT to use this**
For dynamic supervisors (`DynamicSupervisor`) the test strategy is slightly different —
you start children on demand and assert on the count of active children, not on named
pids. The principles (monitor + assert_receive) still apply.

## Reflection

The intensity test crashed the same child 4 times. If the four crashes came from four
DIFFERENT children within the same 5-second window, would the supervisor still die at
the 4th crash? Read the OTP docs on restart intensity to confirm your answer.

## Resources

- [`Supervisor` — hexdocs](https://hexdocs.pm/elixir/Supervisor.html)
- [Restart strategy and intensity — Erlang docs](https://www.erlang.org/doc/system/sup_princ.html#supervision-principles)
- [`Process.monitor/1`](https://hexdocs.pm/elixir/Process.html#monitor/1)
- [`start_supervised!/1`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html#start_supervised!/1)
