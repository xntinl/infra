# Timeout Hierarchies and Deadline Propagation

**Project**: `deadline_rpc` — request-scoped deadlines that propagate across GenServer calls and Task boundaries so inner operations can never outlive their caller.

## Project context

A checkout HTTP handler has a 2000ms budget. It calls three downstream operations sequentially: fraud check, inventory reservation, payment authorization. Each is currently hardcoded with `GenServer.call(..., 5_000)`. If any one goes slow the handler exceeds its budget and the API gateway kills the connection — but the inner call keeps running for 5 seconds, holding a DB lock and an upstream connection.

The fix is a deadline: the handler computes `deadline = now + 2000`, stores it in the Logger metadata / process dictionary / explicit parameter, and every inner call's timeout becomes `max(0, deadline - now)`. When the outer budget is 300ms gone, the inner call gets `1700ms`. When it is 2100ms gone, inner calls immediately fail with `{:error, :deadline_exceeded}` without attempting any work.

```
deadline_rpc/
├── lib/
│   └── deadline_rpc/
│       ├── deadline.ex            # pure deadline helpers
│       ├── client.ex              # calls with deadline propagation
│       └── service.ex             # example downstream GenServer
├── test/
│   └── deadline_rpc/
│       ├── deadline_test.exs
│       └── client_test.exs
└── mix.exs
```

## Why a deadline and not a timeout

A timeout is a duration. A deadline is an absolute instant. The difference matters:

- Handler starts at t=0, budget=2000 → deadline=2000.
- Fraud check takes 400ms. Timeout approach: inner uses "5000". Deadline approach: inner receives `remaining = 2000 - 400 = 1600`.
- Inventory takes 1500ms. Timeout approach: inner still uses "5000" and wins (nothing aborts). Deadline approach: inner receives `remaining = 100`, and if it cannot finish in 100ms it fails fast.

Timeouts don't compose. Deadlines do. Once the caller has a deadline, every callee derives its own remaining from it.

## Why not `$callers` or `Logger.metadata`

Both are valid carriers for the deadline. `Logger.metadata` is per-process and survives within the same process. `$callers` is specifically for `Task` trees. Neither crosses a `GenServer.call` boundary because the callee runs in a different process. You must pass the deadline as an explicit argument or read/restore it from process dictionary at the callee using a helper. This exercise uses explicit arguments because they are the most auditable and testable.

## Core concepts

### 1. Deadline as absolute monotonic time
```
Caller:   t=0    set deadline = now + 2000 = 2000
          t=400  call inner with deadline=2000
Inner:    t=400  remaining = 2000 - 400 = 1600
          t=500  recurse with deadline=2000
Deeper:   t=500  remaining = 2000 - 500 = 1500
```

### 2. Clamping and short-circuit
If `remaining <= 0` before doing any work, return `{:error, :deadline_exceeded}` immediately. No network call, no DB query.

### 3. Genserver.call timeout derivation
### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
timeout = Deadline.remaining(deadline)
GenServer.call(server, msg, timeout)
```
The GenServer timeout and the deadline collapse into one invariant.

## Design decisions

- **Option A — Store deadline in process dictionary**: convenient, implicit, but hides data flow.
- **Option B — Pass deadline as argument in every function signature**: verbose, but explicit and easy to grep.
→ Chose **B** with an optional `nil` default meaning "no deadline enforced". Explicit beats implicit for something that determines correctness.

- **Option A — Convert between milliseconds and `System.monotonic_time/1`**: need to pick a unit and stick with it.
- **Option B — Opaque struct**: `%Deadline{at: monotonic_ms}` prevents raw-integer confusion.
→ Chose **B**. Opaque struct stops anyone from passing `5000` (a duration) where a deadline (an instant) was expected.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule DeadlineRpc.MixProject do
  use Mix.Project
  def project, do: [app: :deadline_rpc, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 1: Deadline module (`lib/deadline_rpc/deadline.ex`)

**Objective**: Encode deadlines as absolute monotonic times so child operations inherit min(parent, local), preventing timeouts from extending beyond caller's SLO budget.

```elixir
defmodule DeadlineRpc.Deadline do
  @moduledoc "Absolute deadline in monotonic milliseconds."

  @type t :: %__MODULE__{at: integer()} | nil

  defstruct [:at]

  @spec within(pos_integer()) :: t()
  def within(ms) when is_integer(ms) and ms > 0 do
    %__MODULE__{at: System.monotonic_time(:millisecond) + ms}
  end

  @spec remaining(t()) :: non_neg_integer() | :infinity
  def remaining(nil), do: :infinity

  def remaining(%__MODULE__{at: at}) do
    max(0, at - System.monotonic_time(:millisecond))
  end

  @spec expired?(t()) :: boolean()
  def expired?(nil), do: false
  def expired?(%__MODULE__{} = d), do: remaining(d) == 0

  @spec derive(t(), pos_integer()) :: t()
  def derive(nil, ms), do: within(ms)

  def derive(%__MODULE__{} = parent, ms) do
    inner_at = System.monotonic_time(:millisecond) + ms
    %__MODULE__{at: min(parent.at, inner_at)}
  end
end
```

### Step 2: Client that propagates deadline (`lib/deadline_rpc/client.ex`)

**Objective**: Short-circuit expired deadlines before RPC dispatch and pack deadline in message so server can abandon work early without caller timeout waiting.

```elixir
defmodule DeadlineRpc.Client do
  alias DeadlineRpc.Deadline

  def call(server, request, deadline) do
    if Deadline.expired?(deadline) do
      {:error, :deadline_exceeded}
    else
      timeout = Deadline.remaining(deadline)

      try do
        GenServer.call(server, {request, deadline}, timeout)
      catch
        :exit, {:timeout, _} -> {:error, :deadline_exceeded}
      end
    end
  end
end
```

### Step 3: Example service that respects inbound deadline (`lib/deadline_rpc/service.ex`)

**Objective**: Check deadline before initiating work so GenServer won't burn cycles on operations doomed to timeout before callee finishes.

```elixir
defmodule DeadlineRpc.Service do
  use GenServer
  alias DeadlineRpc.Deadline

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: opts[:name] || __MODULE__)

  @impl true
  def init(opts), do: {:ok, Map.new(opts)}

  @impl true
  def handle_call({{:work, duration_ms}, deadline}, _from, state) do
    cond do
      Deadline.expired?(deadline) ->
        {:reply, {:error, :deadline_exceeded}, state}

      Deadline.remaining(deadline) < duration_ms ->
        {:reply, {:error, :deadline_exceeded}, state}

      true ->
        Process.sleep(duration_ms)
        {:reply, {:ok, :done}, state}
    end
  end
end
```

## Why this works

- **Monotonic time is monotonic** — `System.monotonic_time(:millisecond)` never goes backwards. Two processes on the same BEAM node share the same monotonic clock, so their deadlines are comparable without clock-skew correction.
- **Early short-circuit avoids wasted work** — `Deadline.expired?` is checked both client-side (before the call) and server-side (after receiving). No scheduler work, no DB query, no socket write.
- **Bounded GenServer timeout** — passing `Deadline.remaining/1` as the `GenServer.call` timeout means the caller never waits past its own budget. The catch on `:exit, {:timeout, _}` converts the BEAM's default timeout exit into a typed `{:error, :deadline_exceeded}`.
- **`derive/2` enforces the min rule** — a child deadline never outlives its parent. This is the invariant that makes composition safe.

## Tests

```elixir
defmodule DeadlineRpc.DeadlineTest do
  use ExUnit.Case, async: true
  alias DeadlineRpc.Deadline

  describe "within/1" do
    test "creates a deadline in the future" do
      d = Deadline.within(100)
      assert Deadline.remaining(d) > 0
      assert Deadline.remaining(d) <= 100
    end
  end

  describe "expired?/1" do
    test "nil deadline is never expired" do
      refute Deadline.expired?(nil)
    end

    test "deadline in the past is expired" do
      d = %Deadline{at: System.monotonic_time(:millisecond) - 10}
      assert Deadline.expired?(d)
    end
  end

  describe "derive/2" do
    test "child never outlives parent" do
      parent = Deadline.within(100)
      child = Deadline.derive(parent, 10_000)
      assert child.at == parent.at
    end

    test "child is tighter when parent has more room" do
      parent = Deadline.within(10_000)
      child = Deadline.derive(parent, 100)
      assert child.at < parent.at
    end

    test "nil parent creates a fresh deadline" do
      child = Deadline.derive(nil, 100)
      assert Deadline.remaining(child) <= 100
    end
  end

  describe "remaining/1" do
    test "nil is :infinity" do
      assert :infinity == Deadline.remaining(nil)
    end

    test "never negative" do
      d = %Deadline{at: System.monotonic_time(:millisecond) - 1_000}
      assert 0 == Deadline.remaining(d)
    end
  end
end
```

```elixir
defmodule DeadlineRpc.ClientTest do
  use ExUnit.Case, async: false
  alias DeadlineRpc.{Client, Deadline, Service}

  setup do
    name = :"svc_#{System.unique_integer([:positive])}"
    {:ok, _} = Service.start_link(name: name)
    {:ok, svc: name}
  end

  describe "deadline propagation" do
    test "succeeds when duration fits in deadline", %{svc: svc} do
      d = Deadline.within(500)
      assert {:ok, :done} = Client.call(svc, {:work, 50}, d)
    end

    test "rejects immediately when server sees insufficient remaining", %{svc: svc} do
      d = Deadline.within(50)
      assert {:error, :deadline_exceeded} = Client.call(svc, {:work, 500}, d)
    end

    test "rejects at client before sending when already expired", %{svc: svc} do
      d = %Deadline{at: System.monotonic_time(:millisecond) - 1}
      assert {:error, :deadline_exceeded} = Client.call(svc, {:work, 10}, d)
    end
  end
end
```

## Benchmark

```elixir
# Cost of Deadline.remaining must be negligible — it runs on every hop.
{t, _} = :timer.tc(fn ->
  d = DeadlineRpc.Deadline.within(1_000)
  for _ <- 1..1_000_000, do: DeadlineRpc.Deadline.remaining(d)
end)
IO.puts("avg: #{t / 1_000_000} µs")
```

Expected: < 0.1µs. It's a single `System.monotonic_time` call and a subtraction.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Monotonic time is per-node** — deadlines do not transfer across the wire. When crossing an RPC or HTTP boundary, convert to a duration (`remaining/1`), serialize, and reconstruct on the other side with `Deadline.within/1`.

**2. `Process.sleep` inside handle_call blocks the GenServer** — the service example uses it for clarity. In production, offload slow work to a `Task` and monitor it with a timeout.

**3. Deadline of zero is a valid deadline** — not the same as `nil`. Zero means "do nothing, fail immediately". `nil` means "no deadline, block forever".

**4. `:infinity` in GenServer.call** — if you pass `:infinity` (from `remaining(nil)`) to `GenServer.call` you revert to no-deadline behaviour. Explicit is safer: reject `nil` at the edge of deadline-aware code.

**5. GenServer timeout exits crash the caller by default** — the `try/catch :exit` pattern converts the crash into a typed error. Without it, a deadline miss propagates as a linked exit.

**6. When NOT to use this** — background jobs (Oban workers, message consumers) already have their own deadline semantics (job timeout, visibility timeout). Deadline propagation is for request-scoped work.

## Reflection

The client computes `timeout = Deadline.remaining(deadline)` and passes it to `GenServer.call`. The server then does its own `Deadline.expired?` check. Why do we need both checks instead of trusting one side?

## Resources

- [gRPC deadlines vs timeouts](https://grpc.io/blog/deadlines/) — the canonical explanation
- [`System.monotonic_time/1` — Elixir docs](https://hexdocs.pm/elixir/System.html#monotonic_time/1)
- [Go context package](https://pkg.go.dev/context) — the same pattern in another language
- [Finch deadline option](https://hexdocs.pm/finch)
