# Stop reasons: `:normal`, `:shutdown`, and custom reasons under a supervisor

**Project**: `stop_reasons_gs` — a GenServer that stops with different reasons, observed under supervision.

---

## Project context

`{:stop, reason, state}` is one of the most frequently returned GenServer
tuples — and one of the most commonly misused. The `reason` atom is not
cosmetic. It drives two important downstream effects:

1. **Supervisor restart decisions**: the supervisor inspects the reason
   and treats `:normal`/`:shutdown`/`{:shutdown, _}` as "clean exit,
   don't count it against restart intensity". Anything else is an abnormal
   crash that counts toward `max_restarts` and may trip the supervisor.
2. **`terminate/2` invocation**: the reason is passed to `terminate/2` so
   cleanup code can branch on "graceful vs. crash".

Getting the reason wrong means a graceful shutdown looks like a crash
(pollutes metrics, trips restart intensity) or a real crash looks
graceful (supervisor happily accepts broken state indefinitely). This
exercise builds a GenServer with multiple stop paths and tests each one
under a `DynamicSupervisor` so you can see the restart behavior directly.

Project structure:

```
stop_reasons_gs/
├── lib/
│   ├── stop_reasons_gs.ex
│   └── stop_reasons_gs/supervisor.ex
├── test/
│   └── stop_reasons_gs_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not always `:normal`?** It silences real failures in SASL logs and disables restart policies.
- **Why not `raise`?** Raising inside a callback becomes `:normal` → `:shutdown` tagging in the supervisor — explicit reasons are clearer.

## Core concepts

### 1. The "normal" class of reasons

OTP defines a small set of reasons as *normal* — they are not crashes.
Supervisors do not increment the restart-intensity counter for these:

- `:normal` — "I chose to stop because my work is done."
- `:shutdown` — "I was asked to stop by my supervisor (or equivalent)."
- `{:shutdown, anything}` — same category, with extra context.

Everything else — atoms, tuples, strings — counts as a crash.

### 2. Why `{:shutdown, reason}` is better than a custom atom

A custom atom like `:account_closed` is classified as a crash because it
isn't in the normal set. Wrapping it — `{:shutdown, :account_closed}` —
keeps the supervisor happy while preserving the business reason for
logs and `terminate/2`. Use this whenever you want "graceful stop with a
specific reason".

### 3. Restart strategies recap

Under `:transient` (the most common for workers with graceful shutdowns):

| Exit reason                | Restart?                |
|----------------------------|-------------------------|
| `:normal`                  | no                      |
| `:shutdown`                | no                      |
| `{:shutdown, _}`           | no                      |
| anything else              | yes (until max_restarts)|

Under `:permanent`: always restart. Under `:temporary`: never restart.

### 4. `terminate/2` sees the exact reason

Whatever you put in `{:stop, reason, state}` shows up in
`terminate(reason, state)`. This lets you branch cleanup: close files on
all paths, but flush a commit only if the reason is not `:shutdown`
(because shutdown means "host is stopping, don't do network I/O").

---

## Design decisions

**Option A — always `:normal` to avoid noisy logs**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — use semantic reasons (`:normal`, `:shutdown`, `{:shutdown, term}`, custom) (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because reason drives supervisor restart policy and SASL logs — silencing it hides real failures.


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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new stop_reasons_gs --sup
cd stop_reasons_gs
```

### Step 2: `lib/stop_reasons_gs.ex`

**Objective**: Implement `stop_reasons_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule StopReasonsGs do
  @moduledoc """
  A tiny worker that can be asked to stop with different reasons so we
  can observe supervisor restart behavior for each.

  Restart strategy: `:transient` — treat `:normal`/`:shutdown`/
  `{:shutdown, _}` as clean, restart on everything else.
  """

  use GenServer, restart: :transient

  require Logger

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)

  @doc """
  Cleanly stop. Supervisor will NOT restart a :transient worker for these reasons.

    * `:normal`                 — the canonical "work done" exit
    * `:shutdown`               — canonical graceful shutdown
    * `{:shutdown, something}`  — graceful with extra context
  """
  @spec stop_clean(GenServer.server(), term()) :: :ok
  def stop_clean(server, reason \\ :normal) do
    GenServer.cast(server, {:stop_clean, reason})
  end

  @doc """
  Stop with an abnormal reason. For a :transient worker this WILL be restarted.
  """
  @spec stop_crashy(GenServer.server(), term()) :: :ok
  def stop_crashy(server, reason \\ :boom) do
    GenServer.cast(server, {:stop_crashy, reason})
  end

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(:ok) do
    Process.flag(:trap_exit, true)
    {:ok, %{started_at: System.monotonic_time()}}
  end

  @impl true
  def handle_cast({:stop_clean, reason}, state) do
    {:stop, reason, state}
  end

  def handle_cast({:stop_crashy, reason}, state) do
    # Abnormal reason — counted as a crash by the supervisor.
    {:stop, reason, state}
  end

  @impl true
  def terminate(reason, _state) do
    # terminate/2 runs for ALL of the above because trap_exit is on and
    # none of them are :kill. Branch cleanup on the reason shape.
    case reason do
      :normal -> Logger.debug("clean exit: :normal")
      :shutdown -> Logger.debug("clean exit: :shutdown")
      {:shutdown, ctx} -> Logger.debug("clean exit: shutdown ctx=#{inspect(ctx)}")
      other -> Logger.warning("crash exit: #{inspect(other)}")
    end

    :ok
  end
end
```

### Step 3: `lib/stop_reasons_gs/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule StopReasonsGs.Supervisor do
  @moduledoc """
  A `DynamicSupervisor` that hosts `StopReasonsGs` workers. Exists to
  let tests observe restart behavior by reason.
  """

  use DynamicSupervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    DynamicSupervisor.start_link(__MODULE__, :ok, opts)
  end

  @impl true
  def init(:ok) do
    # max_restarts 5 so tests can intentionally exceed it.
    DynamicSupervisor.init(strategy: :one_for_one, max_restarts: 5, max_seconds: 5)
  end

  @spec start_worker(pid()) :: DynamicSupervisor.on_start_child()
  def start_worker(sup) do
    DynamicSupervisor.start_child(sup, {StopReasonsGs, []})
  end
end
```

### Step 4: `test/stop_reasons_gs_test.exs`

**Objective**: Write `stop_reasons_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule StopReasonsGsTest do
  use ExUnit.Case, async: true

  setup do
    {:ok, sup} = StopReasonsGs.Supervisor.start_link()
    %{sup: sup}
  end

  defp children_count(sup), do: DynamicSupervisor.count_children(sup).active

  describe "clean reasons under a :transient worker" do
    test "`:normal` is not restarted", %{sup: sup} do
      {:ok, pid} = StopReasonsGs.Supervisor.start_worker(sup)
      ref = Process.monitor(pid)
      StopReasonsGs.stop_clean(pid, :normal)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500

      # Transient + :normal → no restart.
      Process.sleep(50)
      assert 0 = children_count(sup)
    end

    test "`:shutdown` is not restarted", %{sup: sup} do
      {:ok, pid} = StopReasonsGs.Supervisor.start_worker(sup)
      ref = Process.monitor(pid)
      StopReasonsGs.stop_clean(pid, :shutdown)
      assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 500
      Process.sleep(50)
      assert 0 = children_count(sup)
    end

    test "`{:shutdown, context}` is not restarted and preserves the reason", %{sup: sup} do
      {:ok, pid} = StopReasonsGs.Supervisor.start_worker(sup)
      ref = Process.monitor(pid)
      StopReasonsGs.stop_clean(pid, {:shutdown, :user_closed_account})
      assert_receive {:DOWN, ^ref, :process, ^pid, {:shutdown, :user_closed_account}}, 500
      Process.sleep(50)
      assert 0 = children_count(sup)
    end
  end

  describe "crash reasons under a :transient worker" do
    test "a custom atom reason IS treated as a crash and restarted", %{sup: sup} do
      {:ok, pid} = StopReasonsGs.Supervisor.start_worker(sup)
      ref = Process.monitor(pid)

      # `:account_closed` is NOT in {:normal, :shutdown, {:shutdown, _}} — crash.
      StopReasonsGs.stop_crashy(pid, :account_closed)
      assert_receive {:DOWN, ^ref, :process, ^pid, :account_closed}, 500

      # Supervisor should restart. Poll until we see a new child.
      wait_until(fn -> children_count(sup) == 1 end)
    end
  end

  defp wait_until(fun, remaining \\ 50) do
    cond do
      fun.() -> :ok
      remaining == 0 -> flunk("condition never became true")
      true ->
        Process.sleep(10)
        wait_until(fun, remaining - 1)
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `:normal` vs. `:shutdown` — a subtle convention**
Both are clean. Convention: `:normal` means "I finished my work"
(a task-like server that completed). `:shutdown` means "I was asked to
stop from outside" (a supervisor-driven stop, a user-initiated close).
Being consistent helps log readers and telemetry classify correctly.

**2. Custom "business" reasons: wrap in `{:shutdown, _}`**
A reason like `:session_expired` or `:leader_lost` is a crash under OTP's
classification. If it's actually graceful, wrap it: `{:shutdown, :session_expired}`.
You keep the readable log *and* the supervisor stays happy.

**3. `:transient` restart strategy is almost always what you want**
`:permanent` restarts on ANY reason — including `:normal`, which usually
makes no sense. `:temporary` never restarts, which makes it fragile
under bugs. `:transient` is the usual choice: "I mean my `:normal`/
`:shutdown` exits, but please catch my crashes".

**4. `max_restarts` and exit reasons interact**
`:transient` + `:normal` does NOT count against `max_restarts`. But
`:transient` + `:some_crash` DOES. If you have a loop of repeated
`{:stop, :some_crash, state}`, you will trip the supervisor's
intensity and bring the whole subtree down. That is the point of
intensity — know when it applies and when it doesn't.

**5. `terminate/2` sees the reason but can't change it**
The reason in `terminate(reason, state)` is the one the supervisor sees.
You cannot rewrite it from inside `terminate/2`. To log-with-context,
log from the cast handler before the `{:stop, ...}` return, or include
the context in the reason tuple.

**6. When NOT to use `{:stop, reason, state}`**
For per-request errors the caller can recover from, return
`{:reply, {:error, :bad_input}, state}` and stay alive. Stopping on
bad input means every bad input restarts the process — a trivial DOS.
Reserve `{:stop, ...}` for "this process cannot continue".

---


## Reflection

- Diseñá una taxonomía de `stop reasons` para un sistema de pagos. Cuáles son `:normal`, cuáles `:shutdown`, cuáles fallan ruidoso.

## Resources

- [`GenServer` callback return values](https://hexdocs.pm/elixir/GenServer.html#callbacks)
- [`Supervisor` — restart strategies and intensity](https://hexdocs.pm/elixir/Supervisor.html#module-exit-reasons-and-restarts)
- [Erlang `supervisor` manual — child specs and restart types](https://www.erlang.org/doc/man/supervisor.html)
- Saša Jurić, *Elixir in Action* — ch. 9 "Isolating error effects"
