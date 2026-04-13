# Agent State Basics: Building an In-Memory Counter Service

**Project**: `count_agent` — an in-memory visit counter and a simple
per-key rate limiter, both built on `Agent`.

---

## Project structure

```
count_agent/
├── lib/
│   └── count_agent/
│       ├── visits.ex
│       └── rate_limit.ex
├── script/
│   └── main.exs
├── test/
│   └── count_agent/
│       ├── visits_test.exs
│       └── rate_limit_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Core concepts

`Agent` is the smallest abstraction the standard library offers for
**mutable state held inside a process**. It's a thin wrapper around
`GenServer` whose only job is to hold a value and let callers read or
update it with an anonymous function.

Mental model:

- The agent's process owns the state.
- `Agent.get/3` runs `fun.(state)` inside the agent's process and returns the
  result. The state is NOT modified.
- `Agent.update/3` runs `fun.(state)` inside the agent's process and replaces
  the state with the return value.
- `Agent.get_and_update/3` does both atomically: returns a value AND updates
  the state in a single message round-trip.

Two senior-level points that separate correct usage from buggy usage:

**1. Code runs in the agent, not in the caller.** `Agent.update(pid, &heavy_work/1)`
serialises *all* callers through the agent's message queue. If `heavy_work/1`
takes 50ms, your agent handles 20 req/s — max. Keep the function O(1).

**2. Agent is for SIMPLE state only.** Anything with side-effects, timers,
supervised children, or custom receive patterns belongs in a `GenServer`.
The official docs explicitly call this out.

---

## The business problem

Two common services that map perfectly to Agent:

**Visits**: an in-memory counter per page. Increment on each visit, read
totals on a dashboard endpoint. State is a `%{page => count}` map. Resets
on process restart — acceptable for approximate, non-critical metrics.

**Rate limit**: a per-key in-memory bucket. "Allow at most N requests per
second per API key." State is `%{key => {count, window_started_at}}`.
Fixed-window algorithm — simplest correct implementation.

Both are read-heavy, small state, no persistence — textbook Agent territory.

---

## Why `Agent` and not `GenServer` or ETS

- A full `GenServer` is strictly more powerful, but requires callbacks and boilerplate for what is really just `get / update / get_and_update` over a map.
- ETS gives truly parallel reads but has no built-in atomic read-modify-write without careful `:ets.update_counter/3` or match-specs — wrong shape for a per-key fixed-window decision.
- A plain module-level process dict or a global variable doesn't exist in Elixir for a reason: it would hide who owns the state.

`Agent` is the minimum viable process-based state container — exactly when "hold a value, mutate it under a function" is the whole requirement.

---

## Design decisions

**Option A — separate `get` + `update` call for rate-limit decision**
- Pros: two simple functions; easier to unit-test each piece.
- Cons: another caller can slip between the two messages and both reach "count = limit - 1" → the limit is violated. Classic check-then-act race.

**Option B — single `get_and_update/3` atomically deciding and mutating** (chosen)
- Pros: decision + state transition happen inside one agent message, so every caller sees a consistent view; no race possible under any concurrency.
- Cons: slightly more complex `decide/4` helper; the agent's mailbox is the serialisation point (one process = one bottleneck).

→ Chose **B** because the rate-limit contract is "at most N per window" — correctness under concurrency is non-negotiable.

---

## Implementation

### Step 1: Create the project

**Objective**: Establish a standard Mix layout so both services live as siblings under one OTP app, making later supervision and shared tests trivial.

```bash
mix new count_agent
cd count_agent
```

### `mix.exs`
**Objective**: Pin the Elixir version and declare zero external deps, proving Agent is a stdlib primitive that needs no third-party library to build a real service.

```elixir
defmodule CountAgent.MixProject do
  use Mix.Project

  def project do
    [
      app: :count_agent,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
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
### Step 3: `.formatter.exs`

**Objective**: Lock formatting rules up front so style drift never becomes a reviewable concern and diffs stay focused on semantics.

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```
### `lib/count_agent.ex`

```elixir
defmodule CountAgent do
  @moduledoc """
  Agent State Basics: Building an In-Memory Counter Service.

  - A full `GenServer` is strictly more powerful, but requires callbacks and boilerplate for what is really just `get / update / get_and_update` over a map.
  """
end
```
### `lib/count_agent/visits.ex`

**Objective**: Prove Agent's serialisation guarantee by letting concurrent callers hit the same counter without locks, losses, or torn updates.

```elixir
defmodule CountAgent.Visits do
  @moduledoc """
  In-memory per-page visit counter backed by `Agent`.

  State shape: `%{page_id => count}` (a plain map).
  Values reset whenever the process restarts — fine for approximate
  dashboards, never for billing or audit counters.
  """

  @doc """
  Starts an agent holding an empty counter map. Returns `{:ok, pid}`.

  The initial-state function runs inside the agent on start — keep it cheap.
  """
  @spec start_link(keyword()) :: {:ok, pid()}
  def start_link(opts \\ []) do
    Agent.start_link(fn -> %{} end, opts)
  end

  @doc """
  Increments the counter for `page` by 1.

  Uses `Agent.update/3` because we don't need the new value — that avoids a
  round-trip waiting for a reply. `Agent.update/3` is fire-and-forget from the
  caller's perspective but still serialised in the agent's mailbox.
  """
  @spec hit(pid() | atom(), String.t()) :: :ok
  def hit(agent, page) when is_binary(page) do
    Agent.update(agent, fn state -> Map.update(state, page, 1, &(&1 + 1)) end)
  end

  @doc """
  Returns the current count for `page`. Reads are cheap but still synchronous:
  they block the caller until the agent picks the message.
  """
  @spec get(pid() | atom(), String.t()) :: non_neg_integer()
  def get(agent, page) when is_binary(page) do
    Agent.get(agent, fn state -> Map.get(state, page, 0) end)
  end

  @doc """
  Returns the full snapshot. Note: this COPIES the whole map from the agent's
  heap into the caller's heap. For large maps, prefer `get/2` per key.
  """
  @spec snapshot(pid() | atom()) :: %{optional(String.t()) => non_neg_integer()}
  def snapshot(agent) do
    Agent.get(agent, & &1)
  end

  @doc """
  Atomically resets the counter for `page` and returns its previous value.

  `get_and_update/3` is the ONLY way to "read then write" without a race —
  doing a `get` followed by an `update` from the caller would let another
  caller slip between the two.
  """
  @spec reset(pid() | atom(), String.t()) :: non_neg_integer()
  def reset(agent, page) when is_binary(page) do
    Agent.get_and_update(agent, fn state ->
      previous = Map.get(state, page, 0)
      {previous, Map.delete(state, page)}
    end)
  end
end
```
### `lib/count_agent/rate_limit.ex`

**Objective**: Collapse the check-then-act race into a single `get_and_update/3` message so "at most N per window" holds even under contended, concurrent access.

```elixir
defmodule CountAgent.RateLimit do
  @moduledoc """
  Fixed-window per-key rate limiter backed by `Agent`.

  Contract: `check(agent, key)` returns `:ok` if the request is allowed, or
  `{:error, :rate_limited, retry_after_ms}` if the caller should back off.

  State shape: `%{key => {count, window_start_ms}}`.

  LIMITATIONS (read before shipping):
    * In-memory only — resets on restart. Per-node only, not cluster-wide.
    * Fixed window has the "burst at boundary" problem: 2N requests can slip
      through at window edges. Sliding-window / token-bucket are more accurate
      but more complex. For many practical use cases, fixed-window is enough.
    * Under heavy load, the single Agent process becomes the bottleneck.
  """

  @type config :: %{limit: pos_integer(), window_ms: pos_integer()}

  @doc """
  Starts a limiter with `limit` requests per `window_ms` per key.
  """
  @spec start_link(pos_integer(), pos_integer(), keyword()) :: {:ok, pid()}
  def start_link(limit, window_ms, opts \\ [])
      when limit > 0 and window_ms > 0 do
    initial = %{config: %{limit: limit, window_ms: window_ms}, buckets: %{}}
    Agent.start_link(fn -> initial end, opts)
  end

  @doc """
  Records an attempt for `key` and returns whether it is allowed.

  Uses `get_and_update/3` because the decision (allow/deny) and the state
  mutation (increment/reset window) must be atomic. Two concurrent callers
  must never both see "count = limit - 1" and both be allowed through.
  """
  @spec check(pid() | atom(), String.t()) ::
          :ok | {:error, :rate_limited, non_neg_integer()}
  def check(agent, key) when is_binary(key) do
    now = System.monotonic_time(:millisecond)

    Agent.get_and_update(agent, fn state ->
      %{config: %{limit: limit, window_ms: window_ms}, buckets: buckets} = state
      {decision, new_bucket} = decide(Map.get(buckets, key), now, limit, window_ms)
      {decision, %{state | buckets: Map.put(buckets, key, new_bucket)}}
    end)
  end

  # First request for this key: start a new window.
  defp decide(nil, now, _limit, _window_ms), do: {:ok, {1, now}}

  # Current window expired: reset count to 1 and open a new window.
  defp decide({_count, started_at}, now, _limit, window_ms)
       when now - started_at >= window_ms do
    {:ok, {1, now}}
  end

  # Still inside the window and under the limit: count++.
  defp decide({count, started_at}, _now, limit, _window_ms) when count < limit do
    {:ok, {count + 1, started_at}}
  end

  # Inside the window and at/over the limit: deny and hint the retry delay.
  defp decide({count, started_at}, now, _limit, window_ms) do
    retry_after = max(0, window_ms - (now - started_at))
    {{:error, :rate_limited, retry_after}, {count, started_at}}
  end
end
```
### `test/count_agent_test.exs`

**Objective**: Exercise the services under 200-way concurrent load to demonstrate that no increments are lost and no request ever slips past the configured limit.

```elixir
defmodule CountAgent.VisitsTest do
  use ExUnit.Case, async: true
  doctest CountAgent.RateLimit

  alias CountAgent.Visits

  setup do
    {:ok, pid} = Visits.start_link()
    %{agent: pid}
  end

  describe "core functionality" do
    test "get/2 returns 0 for unseen pages", %{agent: a} do
      assert Visits.get(a, "/home") == 0
    end

    test "hit/2 increments the counter", %{agent: a} do
      Visits.hit(a, "/home")
      Visits.hit(a, "/home")
      Visits.hit(a, "/about")

      assert Visits.get(a, "/home") == 2
      assert Visits.get(a, "/about") == 1
    end

    test "snapshot/1 returns the full map", %{agent: a} do
      Visits.hit(a, "/a")
      Visits.hit(a, "/b")

      assert %{"/a" => 1, "/b" => 1} = Visits.snapshot(a)
    end

    test "reset/2 returns the previous count and clears the entry", %{agent: a} do
      Visits.hit(a, "/home")
      Visits.hit(a, "/home")

      assert Visits.reset(a, "/home") == 2
      assert Visits.get(a, "/home") == 0
      refute Map.has_key?(Visits.snapshot(a), "/home")
    end

    test "concurrent hits are serialised and not lost", %{agent: a} do
      # 200 hits across many processes. The agent's mailbox is the serialisation
      # point, so no increments may be dropped even under contention.
      1..200
      |> Task.async_stream(fn _ -> Visits.hit(a, "/home") end,
        max_concurrency: 50,
        ordered: false
      )
      |> Stream.run()

      assert Visits.get(a, "/home") == 200
    end
  end
end
```
```elixir
defmodule CountAgent.RateLimitTest do
  use ExUnit.Case, async: true
  doctest CountAgent.RateLimit

  alias CountAgent.RateLimit

  describe "core functionality" do
    test "allows up to `limit` requests within the window" do
      {:ok, a} = RateLimit.start_link(3, 1_000)

      assert :ok = RateLimit.check(a, "k")
      assert :ok = RateLimit.check(a, "k")
      assert :ok = RateLimit.check(a, "k")
      assert {:error, :rate_limited, retry} = RateLimit.check(a, "k")
      assert retry > 0 and retry <= 1_000
    end

    test "independent keys have independent buckets" do
      {:ok, a} = RateLimit.start_link(1, 1_000)

      assert :ok = RateLimit.check(a, "alice")
      assert :ok = RateLimit.check(a, "bob")
      assert {:error, :rate_limited, _} = RateLimit.check(a, "alice")
    end

    test "opens a new window after the previous one expires" do
      {:ok, a} = RateLimit.start_link(1, 50)

      assert :ok = RateLimit.check(a, "k")
      assert {:error, :rate_limited, _} = RateLimit.check(a, "k")

      # Wait just past the window.
      Process.sleep(80)
      assert :ok = RateLimit.check(a, "k")
    end

    test "is safe under concurrent access — never exceeds the limit" do
      {:ok, a} = RateLimit.start_link(10, 1_000)

      results =
        1..100
        |> Task.async_stream(fn _ -> RateLimit.check(a, "shared") end,
          max_concurrency: 25,
          ordered: false
        )
        |> Enum.map(fn {:ok, r} -> r end)

      allowed = Enum.count(results, &(&1 == :ok))
      assert allowed == 10
    end
  end
end
```
### Step 7: Run

**Objective**: Treat warnings as errors on compile so subtle Agent misuse (unused state, dead clauses) fails the build before it reaches review.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test
mix format
```

### Why this works

The Agent's process is a single mailbox — every `Agent.get/2`, `update/2`, and `get_and_update/2` is one message to that mailbox, handled to completion before the next. That serialisation IS the concurrency control: no mutex, no retry, just FIFO. `get_and_update/3` collapses read-modify-write into a single message, which is the only primitive strong enough to implement a correct fixed-window rate limiter. `Map.update/4` with a default avoids a separate "initialise" branch for first-time keys.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== CountAgent: demo ===\n")

    result_1 = CountAgent.Visits.get(a, "/home")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = CountAgent.Visits.get(a, "/about")
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = CountAgent.Visits.reset(a, "/home")
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

Create `lib/counter_service.ex` and test in `iex`:

```elixir
defmodule CounterService do
  def start(initial \\ 0) do
    Agent.start_link(fn -> initial end)
  end

  def increment(agent_pid) do
    Agent.update(agent_pid, &(&1 + 1))
  end

  def decrement(agent_pid) do
    Agent.update(agent_pid, &(&1 - 1))
  end

  def get(agent_pid) do
    Agent.get(agent_pid, & &1)
  end

  def reset(agent_pid) do
    Agent.update(agent_pid, fn _state -> 0 end)
  end
end

# Test it
{:ok, counter} = CounterService.start(0)

CounterService.increment(counter)
IO.inspect(CounterService.get(counter))  # 1

CounterService.increment(counter)
CounterService.increment(counter)
IO.inspect(CounterService.get(counter))  # 3

CounterService.decrement(counter)
IO.inspect(CounterService.get(counter))  # 2

CounterService.reset(counter)
IO.inspect(CounterService.get(counter))  # 0
```
## Benchmark

```elixir
# bench/agent.exs
{:ok, a} = CountAgent.Visits.start_link()

{t_hit, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn _ -> CountAgent.Visits.hit(a, "/home") end)
end)

{t_get, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn _ -> CountAgent.Visits.get(a, "/home") end)
end)

IO.puts("hit: #{t_hit / 100_000} µs/call   get: #{t_get / 100_000} µs/call")
```
Target: < 5 µs per call on modern hardware (message send + map op + reply). Under concurrent load, the agent saturates around 100–200k messages/s — past that, shard by key or move to ETS.

---

## Trade-off analysis

| Aspect | `Agent` | `GenServer` | ETS |
|--------|---------|-------------|-----|
| Code to write | Smallest | Medium | Small |
| State in process | Yes | Yes | No — shared table |
| Handles timers / side-effects | No | Yes | N/A |
| Concurrent reads | Serialised through one process | Serialised | Truly parallel |
| Hot-path latency | O(1) message, serialised | O(1) message, serialised | Lock-free per row |
| Best for | Small, simple, read-modify-write state | State + behaviour | High-throughput shared reads |

**Rule of thumb.** If your `fn state -> ... end` ever needs to schedule a
timer, call another process, or react to a `:DOWN` message — stop using
Agent and write a GenServer. If you need thousands of concurrent readers
with minimal contention, use ETS.

### When Agent vs GenServer

| Situation | Pick |
|-----------|------|
| Hold a counter / small map, small number of callers | Agent |
| Need to handle arbitrary messages, timers, links | GenServer |
| Reset the state periodically without an external trigger | GenServer |
| Want to expose an API that hides the process entirely | Either — both can be wrapped |

---

## Common production mistakes

**1. Doing expensive work inside the agent function.**
`Agent.update(pid, fn state -> something_slow(state) end)` blocks every other
caller. Do the slow work *outside*, then `Agent.update/3` with the cheap
transformation.

**2. Using two calls instead of `get_and_update/3`.**
```elixir
value = Agent.get(a, & &1)            # racy!
Agent.update(a, fn _ -> derive(value) end)
```
Between the two messages, another caller can mutate the state. Always use
`get_and_update/3` for read-modify-write.

**3. Treating Agent state as durable.**
An Agent lives in memory only. Process crash, node restart, deployment —
state is gone. If that matters, persist on write (database, ETS dump, disk).

**4. Single Agent for high-throughput writes.**
One process = one mailbox = one serialisation point. At ~100k messages/s
you'll saturate it. Shard by key across N agents, or switch to ETS.

**5. Not naming the agent or passing the pid everywhere.**
For a service-level Agent, use `name: __MODULE__` at `start_link/1` and
refer to it by the module atom. Otherwise every caller must know the pid —
an awkward dependency to thread through code.

---

## When NOT to use Agent

- State is durable or shared across nodes — use a database.
- You need high-throughput concurrent reads — use ETS.
- You need behaviour (timers, child processes, custom messages) —
  use `GenServer`.
- You need pub/sub — use `Registry` or Phoenix.PubSub.
- State is per-request — don't use a process at all; use a plain map.

---

## Reflection

- At 100k rate-limit checks per second the single Agent saturates. You shard into 32 agents keyed by `:erlang.phash2(key, 32)`. What becomes harder (snapshots? global reset?) and what becomes trivial (throughput)? Walk the trade.
- Your product wants rate limits that survive process restart. Which part of the `check/2` implementation needs to change, and which stays untouched if you swap the in-memory state for ETS or Redis?

---

## Resources

- [`Agent` — HexDocs](https://hexdocs.pm/elixir/Agent.html)
- [Elixir guide — Agent](https://hexdocs.pm/elixir/agents.html)
- [Saša Jurić — "Elixir in Action", ch. 6 (Generic server processes)](https://www.manning.com/books/elixir-in-action-third-edition)
- [Fixed-window rate limiting — Cloudflare blog](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/)

---

## Why Agent State Basics matters

Mastering **Agent State Basics** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Agents Wrap Stateful Functions in Processes
An Agent maintains state across calls. It's a lightweight wrapper for process-based state management, simpler than `GenServer` for read/update operations.

### 2. Agent Operations Are Synchronous
When you call `Agent.update`, the call blocks until the Agent completes. Agents are suitable for configuration state or shared caches.

### 3. Agents Die with the Process
If you don't supervise the Agent, it's not restarted on crash. Always start Agents under a supervisor in production.

---
