# `:rest_for_one` — directional dependencies

**Project**: `rest_for_one_demo` — a pipeline of three stages where each stage depends on the previous; crashing a middle stage restarts it and everything downstream.

---

## Project context

`:one_for_one` restarts only the crashed child. `:one_for_all` restarts
everyone. `:rest_for_one` is the Goldilocks middle: **restart the crashed
child and every child started AFTER it in the list**. Children listed
BEFORE the crashed one are untouched.

This is the perfect fit for pipelines: Stage1 → Stage2 → Stage3. If Stage2
dies, Stage1 is still fine (it doesn't depend on Stage2), but Stage3 holds
a stale reference to the dead Stage2 and must be restarted too.

Project structure:

```
rest_for_one_demo/
├── lib/
│   ├── rest_for_one_demo.ex
│   ├── rest_for_one_demo/
│   │   ├── stage.ex
│   │   └── supervisor.ex
├── test/
│   └── rest_for_one_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not the other strategies?** Each encodes a different coupling assumption; picking the wrong one either over-restarts or under-restarts.

## Core concepts

### 1. Order in the children list encodes the dependency graph

```
  children = [
    Stage1,   # depends on nothing
    Stage2,   # depends on Stage1
    Stage3    # depends on Stage1 and Stage2
  ]
```

Under `:rest_for_one`:

```
  Stage1 crashes  → restart Stage1, Stage2, Stage3
  Stage2 crashes  → restart Stage2, Stage3    (Stage1 untouched)
  Stage3 crashes  → restart Stage3            (Stage1, Stage2 untouched)
```

### 2. "Started after" is about the supervisor's list, not timestamps

The supervisor uses the position in the children list, not wall-clock
startup time, to decide who restarts. Reorder the list = change the
semantics.

### 3. Stages look up their dependency by name in `init/1`

For the restart to actually fix anything, downstream stages must re-read
upstream pid/name in `init/1`. If they capture it once at compile time,
restart doesn't help.

### 4. `:rest_for_one` + `Registry` scales this pattern

For complex dependency graphs inside one pipeline, pair `:rest_for_one`
with a `Registry` so stages look up dependencies by a logical name rather
than a globally registered pid.

---

## Design decisions

**Option A — `:one_for_all`**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `:rest_for_one` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because children form a startup-order dependency chain; only downstream children need to restart.


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
mix new rest_for_one_demo
cd rest_for_one_demo
```

### Step 2: `lib/rest_for_one_demo/stage.ex`

**Objective**: Implement `stage.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule RestForOneDemo.Stage do
  @moduledoc """
  A pipeline stage that, at init time, reads the pid of its upstream stage
  and stores it. If upstream restarts, this stage is now holding a stale
  pid — which is exactly why :rest_for_one also restarts downstream stages
  when an upstream one dies.
  """

  use GenServer

  @type name :: atom()

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    upstream = Keyword.get(opts, :upstream)
    GenServer.start_link(__MODULE__, {name, upstream}, name: name)
  end

  @spec upstream_pid(name()) :: pid() | nil
  def upstream_pid(name), do: GenServer.call(name, :upstream_pid)

  @spec crash(name()) :: :ok
  def crash(name), do: GenServer.cast(name, :crash)

  @impl true
  def init({_name, nil}), do: {:ok, %{upstream: nil}}

  def init({_name, upstream_name}) do
    # Resolve the upstream pid at init time. A restart re-runs this,
    # ensuring we pick up the NEW upstream pid after it's restarted.
    case Process.whereis(upstream_name) do
      nil -> {:stop, {:missing_upstream, upstream_name}}
      pid -> {:ok, %{upstream: pid}}
    end
  end

  @impl true
  def handle_call(:upstream_pid, _from, %{upstream: pid} = s), do: {:reply, pid, s}

  @impl true
  def handle_cast(:crash, _s), do: raise("stage boom")
end
```

### Step 3: `lib/rest_for_one_demo/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule RestForOneDemo.Supervisor do
  @moduledoc """
  Pipeline supervisor with :rest_for_one semantics. The children list
  encodes the dependency graph:

      :s1 (source) ──▶ :s2 (middle) ──▶ :s3 (sink)
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [
      Supervisor.child_spec({RestForOneDemo.Stage, [name: :s1]}, id: :s1),
      Supervisor.child_spec({RestForOneDemo.Stage, [name: :s2, upstream: :s1]}, id: :s2),
      Supervisor.child_spec({RestForOneDemo.Stage, [name: :s3, upstream: :s2]}, id: :s3)
    ]

    Supervisor.init(children, strategy: :rest_for_one)
  end
end
```

### Step 4: `test/rest_for_one_demo_test.exs`

**Objective**: Write `rest_for_one_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule RestForOneDemoTest do
  use ExUnit.Case, async: false

  alias RestForOneDemo.Stage

  setup do
    start_supervised!(RestForOneDemo.Supervisor)
    :ok
  end

  test "crashing the middle stage restarts it AND the sink; source untouched" do
    s1_old = Process.whereis(:s1)
    s2_old = Process.whereis(:s2)
    s3_old = Process.whereis(:s3)

    ref2 = Process.monitor(s2_old)
    ref3 = Process.monitor(s3_old)

    Stage.crash(:s2)

    assert_receive {:DOWN, ^ref2, :process, ^s2_old, _}, 500
    assert_receive {:DOWN, ^ref3, :process, ^s3_old, _}, 500

    wait_until_new([:s2, :s3], %{s2: s2_old, s3: s3_old})

    # Source was never touched.
    assert Process.whereis(:s1) == s1_old
    # Middle and sink got new pids.
    assert Process.whereis(:s2) != s2_old
    assert Process.whereis(:s3) != s3_old
    # Sink now points at the NEW middle, not the old stale one.
    assert Stage.upstream_pid(:s3) == Process.whereis(:s2)
  end

  test "crashing the sink restarts only the sink" do
    s1_old = Process.whereis(:s1)
    s2_old = Process.whereis(:s2)
    s3_old = Process.whereis(:s3)
    ref = Process.monitor(s3_old)

    Stage.crash(:s3)
    assert_receive {:DOWN, ^ref, :process, ^s3_old, _}, 500

    wait_until_new([:s3], %{s3: s3_old})

    assert Process.whereis(:s1) == s1_old
    assert Process.whereis(:s2) == s2_old
    assert Process.whereis(:s3) != s3_old
  end

  test "crashing the source restarts ALL three stages" do
    s1_old = Process.whereis(:s1)
    s2_old = Process.whereis(:s2)
    s3_old = Process.whereis(:s3)

    ref1 = Process.monitor(s1_old)
    ref2 = Process.monitor(s2_old)
    ref3 = Process.monitor(s3_old)

    Stage.crash(:s1)

    assert_receive {:DOWN, ^ref1, :process, ^s1_old, _}, 500
    assert_receive {:DOWN, ^ref2, :process, ^s2_old, _}, 500
    assert_receive {:DOWN, ^ref3, :process, ^s3_old, _}, 500
  end

  defp wait_until_new(names, olds, timeout \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(names, olds, deadline)
  end

  defp do_wait(names, olds, deadline) do
    all_new? =
      Enum.all?(names, fn n ->
        case Process.whereis(n) do
          nil -> false
          pid -> pid != Map.get(olds, n)
        end
      end)

    cond do
      all_new? -> :ok
      System.monotonic_time(:millisecond) > deadline -> flunk("restart timeout")
      true -> Process.sleep(10); do_wait(names, olds, deadline)
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


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Order in the children list is now a semantic contract**
Under `:rest_for_one`, rearranging the list silently changes behavior.
Add a comment next to the list explaining the dependency it encodes, or
tests will break mysteriously when someone "cleans up" the order.

**2. Stale pids captured in state defeat the whole point**
If Stage3 saves Stage2's pid in state and keeps using it after Stage2
restarts, `:rest_for_one` *did* restart Stage3 — but Stage3's NEW init
needs to look up Stage2 again. Always resolve upstream by name (or via
`Registry`) inside `init/1`, never at module-load time.

**3. Startup is sequential and synchronous**
The supervisor waits for Stage1's `init/1` to return before starting
Stage2. If your source stage takes 30s to warm up, the whole tree takes
30s to come up. For parallel-ish init, split the subtree (two
supervisors) or use `:async_start`-style patterns.

**4. Cross-pipeline dependencies need `:one_for_all`, not `:rest_for_one`**
If Stage1 ALSO depends on Stage3 (e.g., Stage3 feeds back signals), the
graph is cyclic and ordering can't encode it. That's a `:one_for_all`
group, or a redesign to break the cycle.

**5. When NOT to use `:rest_for_one`**
When children are independent (use `:one_for_one`). When ALL children
share mutable state bidirectionally (use `:one_for_all`). When the
"pipeline" is really just a handful of queues — that's a Broadway or
GenStage job, not a supervision strategy.

---


## Reflection

- Si agregás un child al medio del orden, ¿qué pasa con la semántica de `:rest_for_one`? Describí los riesgos operacionales.

## Resources

- [`Supervisor` strategies — `:rest_for_one`](https://hexdocs.pm/elixir/Supervisor.html#module-strategies)
- [`GenStage`](https://hexdocs.pm/gen_stage/) — production pipelines with backpressure
- [`Registry`](https://hexdocs.pm/elixir/Registry.html) — for dynamic dependency lookup
- [Erlang OTP Design Principles — supervisor](https://www.erlang.org/doc/design_principles/sup_princ.html)


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
