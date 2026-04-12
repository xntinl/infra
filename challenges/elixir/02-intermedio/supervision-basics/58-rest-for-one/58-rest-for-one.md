# `:rest_for_one` — directional dependencies

**Project**: `rest_for_one_demo` — a pipeline of three stages where each stage depends on the previous; crashing a middle stage restarts it and everything downstream.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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

## Implementation

### Step 1: Create the project

```bash
mix new rest_for_one_demo
cd rest_for_one_demo
```

### Step 2: `lib/rest_for_one_demo/stage.ex`

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

```bash
mix test
```

---

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

## Resources

- [`Supervisor` strategies — `:rest_for_one`](https://hexdocs.pm/elixir/Supervisor.html#module-strategies)
- [`GenStage`](https://hexdocs.pm/gen_stage/) — production pipelines with backpressure
- [`Registry`](https://hexdocs.pm/elixir/Registry.html) — for dynamic dependency lookup
- [Erlang OTP Design Principles — supervisor](https://www.erlang.org/doc/design_principles/sup_princ.html)
