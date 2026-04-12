# Fan-out / fan-in aggregation

**Project**: `fanout_aggregator` — dispatch a request to N workers and merge their results.

---

## Project context

You need to answer a query by consulting several sources and combining
their answers: a price comparator hitting 4 upstream APIs, a search
ranker querying 3 shards, a dashboard widget pulling metrics from
several services. The pattern is always the same:

```
         ┌──► worker_1 ──►┐
request ─┼──► worker_2 ──►┼─► aggregate ─► reply
         └──► worker_N ──►┘
```

Fan-out the work, fan-in the results. The interesting bits are:

- How do you combine partial failures? (fail the whole request, or
  return what you have?)
- How do you enforce a deadline? (one slow worker shouldn't punish the
  caller.)
- How do you keep ordering or associate each result with its source?

This exercise builds a generic fan-out aggregator on top of
`Task.async_stream` and a user-supplied reducer, then shows how to handle
partial timeouts.

Project structure:

```
fanout_aggregator/
├── lib/
│   └── fanout_aggregator.ex
├── test/
│   └── fanout_aggregator_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For concurrency patterns fan out, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. Fan-out is a map; fan-in is a reduce

Dispatching N tasks is a map. Combining their results — summing numbers,
picking the minimum, merging lists, choosing the fastest — is a reduce.
Keep the two separate in your code:

```
sources |> map(run/1) |> reduce(combiner/2, initial)
```

The combiner is where your domain logic lives. Everything else is
plumbing.

### 2. `Task.async_stream` for bounded fan-out with per-task timeout

```
Task.async_stream(sources, fun,
  max_concurrency: 8,   # hard cap, not just a hint
  timeout: 500,         # per-task deadline
  on_timeout: :kill_task # slow task is killed, stream yields {:exit, :timeout}
)
```

Each element yields `{:ok, value}` or `{:exit, reason}`. You decide in
the reducer what "exit" means for your query — often "skip and continue",
sometimes "abort".

### 3. Deadline belongs to the request, not the worker

If you need the answer in 500ms, that budget applies to the whole fan-out.
Setting a per-task timeout of 500ms means the slowest accepted worker can
take 500ms and the *total* call can also take 500ms (tasks run in
parallel) — as long as `max_concurrency >= N`. If you throttle, the
effective budget grows.

### 4. Partial results vs total failure

Two canonical strategies:

- **All-or-nothing**: any worker failure fails the request. Use
  `Task.await_many` (exercise 50) or `on_timeout: :kill_task` + reducer
  that raises on `:exit`.
- **Best-effort**: collect whatever succeeded by the deadline, annotate
  the response with which sources were unavailable. Use the reducer to
  skip `:exit` entries.

Pick one explicitly — "it depends on the caller" is how you end up with
inconsistent behavior.

---

## Design decisions

**Option A — sequential processing**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — fan-out via `Task.async_stream` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because embarrassingly-parallel work should saturate cores; `async_stream` also bounds memory.


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
mix new fanout_aggregator
cd fanout_aggregator
```

### Step 2: `lib/fanout_aggregator.ex`

```elixir
defmodule FanoutAggregator do
  @moduledoc """
  Generic fan-out / fan-in helper: runs a function against a list of
  sources concurrently, then reduces the results with a user-supplied
  combiner. Partial failures are surfaced in the return value so the
  caller can decide whether to treat them as fatal.
  """

  @type source :: term()
  @type result :: term()

  @type aggregate_response :: %{
          value: term(),
          ok: non_neg_integer(),
          failed: non_neg_integer(),
          failures: [{source(), term()}]
        }

  @doc """
  Runs `fun` against every element of `sources` concurrently, then
  folds successful results through `combiner` starting from `initial`.

  Options:
    * `:max_concurrency` — default `System.schedulers_online() * 2`.
    * `:timeout` — per-task deadline in ms, default `5_000`.
  """
  @spec aggregate([source()], (source() -> result()), term(), (result(), term() -> term()), keyword()) ::
          aggregate_response()
  def aggregate(sources, fun, initial, combiner, opts \\ [])
      when is_list(sources) and is_function(fun, 1) and is_function(combiner, 2) do
    max_concurrency = Keyword.get(opts, :max_concurrency, System.schedulers_online() * 2)
    timeout = Keyword.get(opts, :timeout, 5_000)

    sources
    |> Task.async_stream(fun,
      max_concurrency: max_concurrency,
      timeout: timeout,
      on_timeout: :kill_task,
      ordered: false
    )
    |> Enum.zip(sources)
    |> Enum.reduce(%{value: initial, ok: 0, failed: 0, failures: []}, fn
      {{:ok, value}, _source}, acc ->
        %{acc | value: combiner.(value, acc.value), ok: acc.ok + 1}

      {{:exit, reason}, source}, acc ->
        %{acc | failed: acc.failed + 1, failures: [{source, reason} | acc.failures]}
    end)
  end
end
```

### Step 3: `test/fanout_aggregator_test.exs`

```elixir
defmodule FanoutAggregatorTest do
  use ExUnit.Case, async: true

  describe "aggregate/5 — happy path" do
    test "sums integers from all sources" do
      sources = [1, 2, 3, 4, 5]

      result =
        FanoutAggregator.aggregate(sources, &(&1 * 10), 0, &Kernel.+/2)

      assert result.value == 150
      assert result.ok == 5
      assert result.failed == 0
      assert result.failures == []
    end

    test "merges maps from all sources" do
      sources = [:a, :b, :c]

      result =
        FanoutAggregator.aggregate(
          sources,
          fn s -> %{s => to_string(s)} end,
          %{},
          &Map.merge/2
        )

      assert result.value == %{a: "a", b: "b", c: "c"}
      assert result.ok == 3
    end

    test "runs in parallel — slowest source dominates, not the sum" do
      sources = 1..5 |> Enum.to_list()

      {elapsed_us, _result} =
        :timer.tc(fn ->
          FanoutAggregator.aggregate(
            sources,
            fn _ -> Process.sleep(80); :ok end,
            [],
            fn v, acc -> [v | acc] end,
            max_concurrency: 5
          )
        end)

      # Serial would be ~400ms. Parallel should be ~80ms plus overhead.
      assert elapsed_us < 200_000
    end
  end

  describe "aggregate/5 — partial failure" do
    test "records timeouts as failures and keeps the good results" do
      sources = [:fast_a, :slow, :fast_b]

      result =
        FanoutAggregator.aggregate(
          sources,
          fn
            :slow -> Process.sleep(500); :late
            other -> other
          end,
          [],
          fn v, acc -> [v | acc] end,
          timeout: 50
        )

      assert result.ok == 2
      assert result.failed == 1
      assert [{:slow, _reason}] = result.failures
      assert Enum.sort(result.value) == [:fast_a, :fast_b]
    end

    test "records raised errors as failures" do
      sources = [:ok1, :boom, :ok2]

      result =
        FanoutAggregator.aggregate(
          sources,
          fn
            :boom -> raise "nope"
            other -> other
          end,
          [],
          fn v, acc -> [v | acc] end
        )

      assert result.ok == 2
      assert result.failed == 1
      assert [{:boom, _}] = result.failures
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

```elixir
{us, _} = :timer.tc(fn -> fan_out_compute(1000) end)
```

Target esperado: ~1/N del tiempo secuencial con N cores.

## Trade-offs and production gotchas

**1. `ordered: false` gives you throughput, not determinism**
With `ordered: false`, the first result returned is the first to finish,
not the first source in the input. For associative combiners (sum, union,
max) this is fine. For order-sensitive reducers, use `ordered: true` and
accept the cost of waiting for earlier-in-input slow tasks.

**2. `on_timeout: :kill_task` brutalizes the worker**
A killed task cannot clean up. If it holds external resources (an open
transaction, a file handle, a bookshop reservation), those leak. For
work with external side effects, prefer `Task.yield_many` + explicit
`Task.shutdown` (exercise 53) or an in-worker deadline.

**3. `max_concurrency` bounds in-flight, not total time**
With 100 sources, `max_concurrency: 10`, and 500ms per task, the total
wall time is ~5s — not 500ms. The deadline is per-task, not per-batch.
If you need a request-level deadline, wrap the whole call in a separate
`Task` + `Task.yield` + `Task.shutdown`.

**4. Partial-result APIs need explicit contract**
A function that returns "2 of 3 sources succeeded" forces every caller
to handle partial data. Either document clearly which sources may be
missing, or decide the endpoint is all-or-nothing. Don't leave it
ambiguous — callers will assume completeness and be wrong.

**5. The reducer runs in the caller, in result order**
If the reducer is slow, it bottlenecks the whole fan-in. Keep combiners
cheap (sums, merges, list-cons) and do heavy post-processing after
`aggregate` returns.

**6. When NOT to fan out**
- Single-source queries — obvious.
- Sources that share a rate limit: parallel requests get throttled or
  banned. Serialize or use a dedicated pool.
- Sources that must see requests in a strict order (event sourcing,
  certain caches). Fan-out breaks the sequence.

---


## Reflection

- Si una de las tareas en el fan-out falla, ¿cancelás todas o juntás los resultados parciales? Justificá según el dominio.

## Resources

- [`Task.async_stream/3`](https://hexdocs.pm/elixir/Task.html#async_stream/3)
- [`Task.yield_many/2`](https://hexdocs.pm/elixir/Task.html#yield_many/2) — for deadline-oriented collection
- ["Scatter-gather" — Enterprise Integration Patterns](https://www.enterpriseintegrationpatterns.com/patterns/messaging/BroadcastAggregate.html)
- [Fred Hebert — "Stuff Goes Bad: Erlang in Anger"](https://www.erlang-in-anger.com/) — chapters on timeouts and partial failure
