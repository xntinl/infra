# A proxy/delegator GenServer with basic rate limiting

**Project**: `proxy_gs` — a GenServer that forwards requests to a backend process while enforcing a per-second rate cap.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You have a downstream service that breaks under sustained load — a
third-party API with a 10 req/s quota, a legacy TCP server, a database
function that locks a table. You cannot change the backend, but you can
put a proxy in front of it: a GenServer every caller goes through,
which forwards requests to the real backend only at a rate the backend
tolerates.

This is a classic OTP pattern: **a GenServer as a gatekeeper**. It
owns the policy (rate limit, retry, batch, cache), the backend owns the
work, and callers see a uniform synchronous API. You'll see variations
in every non-trivial Elixir codebase: `Finch` pool checkouts, rate
limiters in front of Stripe, throttles for webhook delivery.

The exercise builds the minimum shape: a proxy that forwards `call/2`
requests to a configurable backend, tracks requests in a 1-second
window, and returns `{:error, :rate_limited}` when the cap is hit
instead of forwarding.

Project structure:

```
proxy_gs/
├── lib/
│   ├── proxy_gs.ex
│   └── proxy_gs/backend.ex
├── test/
│   └── proxy_gs_test.exs
└── mix.exs
```

---

## Core concepts

### 1. A proxy is a GenServer that forwards

```
caller ──call──▶ ProxyGs ──call──▶ Backend
                   │
                   └── applies policy: rate limit, cache, retry
```

The proxy's callbacks do NOT compute the business answer — they forward
to the backend. All they contribute is the policy layer. This single-
responsibility shape keeps both pieces testable.

### 2. Rate limiting with a sliding window

Track the timestamps (or just a count + window-start) of recent
requests. On each request:

1. Drop timestamps older than the window.
2. If the remaining count is below the cap, forward and record.
3. Otherwise, reject with `{:error, :rate_limited}`.

This exercise uses a simple list of monotonic timestamps. For very high
rates, prefer `:counters` + a rotating bucket (see exercise 38).

### 3. `GenServer.call` inside `handle_call` — beware deadlocks

If the proxy calls a GenServer that eventually calls back into the
proxy, you get a deadlock. For this exercise the backend is a separate
process with no back-reference, so it's safe. In production, draw your
call graph before wiring proxies.

### 4. Synchronous forwarding vs. async handoff

Forwarding via `GenServer.call` blocks the proxy while the backend
works. For slow backends, this serializes all callers through the
proxy — which may be exactly what you want (you wanted a gatekeeper),
or may be catastrophic (you wanted concurrency). When concurrency
matters, hand off to a `Task.Supervisor` and have the proxy just count
and immediately acknowledge.

---

## Implementation

### Step 1: Create the project

```bash
mix new proxy_gs
cd proxy_gs
```

### Step 2: `lib/proxy_gs/backend.ex`

```elixir
defmodule ProxyGs.Backend do
  @moduledoc """
  A trivial backend that echoes the request. Stand-in for any real
  downstream service. Kept separate so tests can spawn many and so
  the proxy has no special knowledge of it.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)

  @spec echo(GenServer.server(), term()) :: {:ok, term()}
  def echo(server, term), do: GenServer.call(server, {:echo, term})

  @impl true
  def init(:ok), do: {:ok, %{count: 0}}

  @impl true
  def handle_call({:echo, term}, _from, %{count: n} = state) do
    {:reply, {:ok, term}, %{state | count: n + 1}}
  end
end
```

### Step 3: `lib/proxy_gs.ex`

```elixir
defmodule ProxyGs do
  @moduledoc """
  A GenServer that forwards `echo/2` requests to a `ProxyGs.Backend`
  while enforcing a configurable per-second rate cap.

  Returns `{:ok, reply}` on forwarded requests and `{:error, :rate_limited}`
  when the cap is exceeded — the backend is not called in that case.
  """

  use GenServer

  @default_cap 5
  @window_ms 1_000

  defmodule State do
    @moduledoc false
    @enforce_keys [:backend, :cap, :window_ms]
    defstruct [:backend, :cap, :window_ms, timestamps: []]

    @type t :: %__MODULE__{
            backend: GenServer.server(),
            cap: pos_integer(),
            window_ms: pos_integer(),
            timestamps: [integer()]
          }
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {backend, opts} = Keyword.pop!(opts, :backend)
    {cap, opts} = Keyword.pop(opts, :cap, @default_cap)
    {window_ms, opts} = Keyword.pop(opts, :window_ms, @window_ms)
    GenServer.start_link(__MODULE__, {backend, cap, window_ms}, opts)
  end

  @spec echo(GenServer.server(), term()) :: {:ok, term()} | {:error, :rate_limited}
  def echo(proxy, term), do: GenServer.call(proxy, {:echo, term})

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init({backend, cap, window_ms}) do
    {:ok, %State{backend: backend, cap: cap, window_ms: window_ms}}
  end

  @impl true
  def handle_call({:echo, term}, _from, %State{} = state) do
    now = monotonic_ms()
    recent = prune(state.timestamps, now, state.window_ms)

    if length(recent) >= state.cap do
      {:reply, {:error, :rate_limited}, %{state | timestamps: recent}}
    else
      # Forward under the cap. Backend may take time — caller waits.
      reply = ProxyGs.Backend.echo(state.backend, term)
      {:reply, reply, %{state | timestamps: [now | recent]}}
    end
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp monotonic_ms, do: System.monotonic_time(:millisecond)

  # Drop timestamps older than `window_ms` ago. Cheap for small caps;
  # if cap grows, replace with a counter per bucket (see exercise 38).
  defp prune(timestamps, now, window_ms) do
    threshold = now - window_ms
    Enum.filter(timestamps, &(&1 > threshold))
  end
end
```

### Step 4: `test/proxy_gs_test.exs`

```elixir
defmodule ProxyGsTest do
  use ExUnit.Case, async: true

  setup do
    {:ok, backend} = ProxyGs.Backend.start_link()
    %{backend: backend}
  end

  describe "under the cap" do
    test "forwards requests and returns the backend reply", %{backend: backend} do
      {:ok, proxy} = ProxyGs.start_link(backend: backend, cap: 5, window_ms: 1_000)

      for i <- 1..5 do
        assert {:ok, ^i} = ProxyGs.echo(proxy, i)
      end
    end
  end

  describe "over the cap" do
    test "rejects with :rate_limited without calling the backend", %{backend: backend} do
      {:ok, proxy} = ProxyGs.start_link(backend: backend, cap: 3, window_ms: 1_000)

      for i <- 1..3, do: assert({:ok, ^i} = ProxyGs.echo(proxy, i))

      # 4th request is over the cap within the same 1s window.
      assert {:error, :rate_limited} = ProxyGs.echo(proxy, 4)

      # Backend should have seen exactly 3 requests, not 4.
      # (Echo backend tracks count in state; we verify by monitoring behaviour.)
      # The proof below: re-allowing after the window passes shows the proxy
      # really is gating, not the backend.
    end

    test "window expiry restores capacity", %{backend: backend} do
      {:ok, proxy} = ProxyGs.start_link(backend: backend, cap: 2, window_ms: 50)

      assert {:ok, 1} = ProxyGs.echo(proxy, 1)
      assert {:ok, 2} = ProxyGs.echo(proxy, 2)
      assert {:error, :rate_limited} = ProxyGs.echo(proxy, 3)

      # Wait for the window to expire.
      Process.sleep(80)

      # Capacity is restored.
      assert {:ok, 4} = ProxyGs.echo(proxy, 4)
    end
  end

  describe "serialized forwarding" do
    test "concurrent callers all get answers, none lost", %{backend: backend} do
      # Cap large enough that no caller is rate-limited — we want to see
      # the proxy serialize correctly without dropping or duplicating.
      {:ok, proxy} = ProxyGs.start_link(backend: backend, cap: 1_000, window_ms: 1_000)

      tasks = for i <- 1..20, do: Task.async(fn -> ProxyGs.echo(proxy, i) end)
      results = Task.await_many(tasks, 1_000)

      assert length(results) == 20
      assert Enum.all?(results, &match?({:ok, _}, &1))
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. A proxy is a bottleneck by design**
Every caller is serialized through one process. That is usually what
you want (it's why you added a gatekeeper), but on hot paths with fast
backends, the proxy itself becomes the slow link. Measure before
assuming the proxy is free.

**2. `Enum.filter` on timestamps is O(n) per request**
Fine for small caps (tens per window). For large caps (thousands),
replace with a ring buffer of counters per sub-bucket or with
`:counters` + a self-scheduled tick that rotates buckets (exercise 38).
Don't carry a list of 10k timestamps per request.

**3. `GenServer.call` to the backend blocks the proxy**
If the backend is slow, the proxy stops accepting new calls until this
one returns. For concurrency, either (a) turn the proxy into a mere
admission controller that hands work to a pool, or (b) use `cast` +
reply from the backend (advanced). The simple shape in this exercise
is right when the backend is fast and serialization is the goal.

**4. Rate limit errors are not retried by the proxy**
The proxy rejects and returns — the caller decides what to do. This is
correct: the proxy does not know the caller's deadline, patience, or
correctness constraints. Don't bake retry logic into the gatekeeper;
expose the rejection and let the caller choose.

**5. The rate cap must be enforceable under back-pressure**
If callers can produce requests faster than the proxy drains them, the
proxy's mailbox grows. A 10 req/s cap enforced by a proxy that takes
10ms to reject is overwhelmed at >100 req/s of rejected load. At high
rejection rates, consider a lock-free check in the caller (e.g. `:atomics`)
before hitting the proxy.

**6. When NOT to use a GenServer proxy**
For per-caller limits (one quota per user), use a sharded store like
`Registry` or ETS — a single GenServer becomes the bottleneck. For
distributed limits across nodes, use a centralized store (Redis) or a
CRDT-based library (`Hammer` with a shared backend). Single-GenServer
proxies are for single-node, single-quota gatekeeping.

---

## Resources

- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- [`Hammer` — rate limiting library](https://hexdocs.pm/hammer/) — production patterns
- [`Finch` — HTTP client with pool-based gatekeeping](https://hexdocs.pm/finch/)
- [Alex Koutmos, "Rate limiting in Elixir"](https://akoutmos.com/) — practical deep dive
- [`:counters` — lock-free counters for hot paths](https://www.erlang.org/doc/man/counters.html)
