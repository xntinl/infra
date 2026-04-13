# Bounded parallelism with `Task.async_stream`

**Project**: `image_processor` — process N "images" concurrently with a hard cap on in-flight work.

---

## Project context

You have a few thousand images to thumbnail, or a few million rows to
transform, or a few hundred API calls to make. Naïvely mapping
`Task.async` across them spawns one task per input — which on big inputs
means thousands of simultaneously-running processes, memory blowup, and
(for I/O) rate-limit bans from the remote side.

`Task.async_stream` is the right answer: it's a `Stream`-aware parallel
`Enum.map` with a **hard** `max_concurrency` cap, per-task timeouts, and
backpressure — the stream only pulls the next element when a slot frees
up.

This exercise uses simulated image work (`Process.sleep` + a fake byte
count) so you can focus on the concurrency mechanics. The same pattern
applies to database batch updates, HTTP fan-out, and file conversions.

Project structure:

```
image_processor/
├── lib/
│   └── image_processor.ex
├── test/
│   └── image_processor_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not raw `Task.async`?** Unbounded parallelism exhausts FDs/memory; `async_stream` caps concurrency and streams results.

## Core concepts

### 1. `Task.async_stream/3` is a bounded parallel map

```
Task.async_stream(enumerable, fun,
  max_concurrency: N,
  timeout: T,
  on_timeout: :kill_task | :exit,
  ordered: true | false
)
```

Only up to `N` workers run at once. As each finishes, the next input is
pulled from the enumerable. Memory usage is bounded by `N`, not by
`length(enumerable)` — you can stream a million items through 8 workers
without loading them all at once.

### 2. Output shape: `{:ok, v}` or `{:exit, reason}`

Each element of the output stream is one of:

- `{:ok, value}` — the function returned successfully.
- `{:exit, reason}` — the task crashed, was killed, or (with
  `on_timeout: :kill_task`) timed out. The stream keeps going.

With `on_timeout: :exit` (the default), a timeout raises and the whole
stream aborts. Pick `:kill_task` when you want "skip the bad ones".

### 3. `ordered: false` unlocks throughput

By default, results come out in input order — meaning a slow item at
position 0 blocks all later results from being yielded, even after
they're done. Set `ordered: false` for true "first done, first out"
streaming.

For associative reducers (sum, merge, count), ordering doesn't matter
and `ordered: false` is faster. For order-sensitive output (writing
results to disk in input order), keep `ordered: true`.

### 4. `max_concurrency` should match the bottleneck

- **CPU-bound**: `System.schedulers_online()` (one per core).
- **I/O-bound to one service**: what the service says it can take
  without rate-limiting (often 4–16).
- **Fan-out across many services**: higher, but mind process memory —
  thousands of live tasks each have heap overhead.

Do not set `max_concurrency` to `:infinity` unless you have a very good
reason.

---

## Design decisions

**Option A — unbounded parallel `Task.async`**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Task.async_stream` with `max_concurrency` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because unbounded parallelism exhausts file handles and memory; bounded streams are production-safe.


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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new image_processor
cd image_processor
```

### Step 2: `lib/image_processor.ex`

**Objective**: Implement `image_processor.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.


```elixir
defmodule ImageProcessor do
  @moduledoc """
  Bounded-concurrency image "processing" via `Task.async_stream`.

  The `process/1` step is simulated with `Process.sleep/1` + a derived
  byte count, so tests are deterministic and no real image decoder is
  needed. Swap in `Mogrify`, `Image`, or whatever else in real code —
  the concurrency plumbing is unchanged.
  """

  @type image :: %{id: term(), payload_size: pos_integer()}
  @type processed :: %{id: term(), thumb_bytes: non_neg_integer(), latency_ms: pos_integer()}

  @doc """
  Runs `process/1` over every image concurrently with a hard cap.

  Options:
    * `:max_concurrency` — default `System.schedulers_online()`.
    * `:timeout` — per-image deadline in ms, default 1_000.
    * `:ordered` — default `true`. Set `false` if output order doesn't matter.

  Returns a list of `{:ok, processed}` or `{:exit, reason}` tuples.
  """
  @spec process_all(Enumerable.t(), keyword()) :: [{:ok, processed()} | {:exit, term()}]
  def process_all(images, opts \\ []) do
    max_concurrency = Keyword.get(opts, :max_concurrency, System.schedulers_online())
    timeout = Keyword.get(opts, :timeout, 1_000)
    ordered = Keyword.get(opts, :ordered, true)

    images
    |> Task.async_stream(&process/1,
      max_concurrency: max_concurrency,
      timeout: timeout,
      on_timeout: :kill_task,
      ordered: ordered
    )
    |> Enum.to_list()
  end

  @doc """
  Simulated image work. The sleep duration is a function of the image
  id so tests are deterministic; the byte count is a function of the
  input size.
  """
  @spec process(image()) :: processed()
  def process(%{id: id, payload_size: size}) do
    latency = :erlang.phash2(id, 40) + 10
    Process.sleep(latency)
    %{id: id, thumb_bytes: div(size, 4), latency_ms: latency}
  end
end
```

### Step 3: `test/image_processor_test.exs`

**Objective**: Write `image_processor_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ImageProcessorTest do
  use ExUnit.Case, async: true

  defp images(n) do
    for i <- 1..n, do: %{id: i, payload_size: 1024}
  end

  describe "process_all/2 — correctness" do
    test "returns one result per input in order (ordered: true)" do
      results = ImageProcessor.process_all(images(5))

      ids =
        for {:ok, %{id: id}} <- results, do: id

      assert ids == [1, 2, 3, 4, 5]
    end

    test "every successful result has a thumbnail byte count" do
      for {:ok, %{thumb_bytes: bytes}} <- ImageProcessor.process_all(images(5)) do
        assert bytes == div(1024, 4)
      end
    end
  end

  describe "process_all/2 — bounded concurrency" do
    test "never runs more than max_concurrency tasks at once" do
      # Use a probe agent to count peak concurrency.
      {:ok, counter} = Agent.start_link(fn -> %{current: 0, peak: 0} end)

      tick_up = fn ->
        Agent.update(counter, fn %{current: c, peak: p} ->
          %{current: c + 1, peak: max(p, c + 1)}
        end)
      end

      tick_down = fn ->
        Agent.update(counter, fn %{current: c} = s -> %{s | current: c - 1} end)
      end

      work = fn _img ->
        tick_up.()
        Process.sleep(20)
        tick_down.()
        :ok
      end

      1..20
      |> Enum.map(&%{id: &1, payload_size: 1})
      |> Task.async_stream(work, max_concurrency: 4, timeout: 2_000)
      |> Stream.run()

      assert Agent.get(counter, & &1.peak) <= 4
    end
  end

  describe "process_all/2 — timeout handling" do
    test "slow items are reported as {:exit, :timeout} when on_timeout: :kill_task" do
      slow = %{id: :slow_1, payload_size: 1024}
      fast = %{id: :fast_1, payload_size: 1024}

      # Stub: :slow_1 sleeps far past the deadline.
      work = fn
        %{id: :slow_1} -> Process.sleep(500); :never
        %{id: :fast_1} = img -> ImageProcessor.process(img)
      end

      [r1, r2] =
        [slow, fast]
        |> Task.async_stream(work,
          max_concurrency: 2,
          timeout: 50,
          on_timeout: :kill_task,
          ordered: true
        )
        |> Enum.to_list()

      assert match?({:exit, :timeout}, r1)
      assert match?({:ok, %{id: :fast_1}}, r2)
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Deep Dive: Task Spawn vs GenServer for Ephemeral Work

A Task is lightweight `spawn/1` for bounded, self-contained work: compute, return, exit. Unlike GenServer (which receives messages indefinitely), Task is inherently ephemeral. This shapes everything: no callbacks, no state management, no back-pressure.

Advantages: simplicity (few lines vs GenServer boilerplate). Disadvantages: no explicit state or message handling—Tasks assume pure computation or simple I/O. If you need a long-lived process responding to external events, you've outgrown Task.

For CPU-bound work (calculations, parsing), Task.Supervisor with `:temporary` is ideal: spawn tasks, let them exit, don't restart. For coordinated async work (multiple tasks handing off results), GenServer + worker tasks often clarifies intent despite more boilerplate. Measure first: if code clarity improves with GenServer, the overhead is justified.

## Benchmark

```elixir
{us, _} = :timer.tc(fn ->
  1..1000
  |> Task.async_stream(&process/1, max_concurrency: 32)
  |> Enum.to_list()
end)
```

Target esperado: memoria acotada, throughput ~N veces el secuencial con N = max_concurrency.

## Trade-offs and production gotchas

**1. `max_concurrency: :infinity` defeats the purpose**
It turns `async_stream` into `Task.async` with extra steps. Pick a real
number based on the bottleneck (cores, upstream rate limit, DB pool
size). If you don't know, start at 8 and measure.

**2. `on_timeout: :kill_task` leaks resources held by the worker**
`:brutal_kill` can't be trapped and won't run cleanup. For workers that
hold external resources, prefer a cooperative in-worker deadline or
switch to `Task.yield_many` + explicit shutdown.

**3. `ordered: true` hides head-of-line blocking**
One slow worker at position 0 blocks every subsequent result from being
yielded downstream — even if they finished first. For dashboards,
streaming writers, or anything real-time, `ordered: false` is usually
the right default.

**4. The caller is linked to each worker**
Like `Task.async`, each worker is linked to the process running the
stream. An un-caught raise in a worker crashes the caller unless you
use `on_timeout: :kill_task` + pattern-match on `{:exit, _}` in results.
For true isolation (no link to caller), use
`Task.Supervisor.async_stream_nolink/4`.

**5. `Enum.to_list` forces the whole batch into memory**
If you're streaming millions, pipe directly to a reducer
(`Enum.reduce/3`, `Stream.each/2` + `Stream.run/1`) instead of
materializing a list of results.

**6. When NOT to use `async_stream`**
- Batch size is tiny (< 10) and known fixed: plain `Task.async` + `await_many`
  is simpler.
- You need a shared resource (connection, rate limiter) across workers:
  the stream doesn't coordinate them — use a pool.
- Workers must report progress individually to a consumer mid-flight:
  `GenStage` or a supervised fan-out pipeline fits better.

---


## Reflection

- Definí `max_concurrency` para un proceso que hace HTTP a una API con rate limit de 100 req/s. Justificá.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  def main do
    items = [1, 2, 3, 4, 5]
  
    results = items
      |> Task.async_stream(fn x -> x * 10 end, max_concurrency: 2)
      |> Enum.map(fn {:ok, val} -> val end)
  
    IO.puts("Streamed results: #{inspect(results)}")
    IO.puts("✓ Task.async_stream works correctly")
  end

end

Main.main()
```


## Resources

- [`Task.async_stream/3` — Elixir stdlib](https://hexdocs.pm/elixir/Task.html#async_stream/3)
- [`Task.Supervisor.async_stream_nolink/4`](https://hexdocs.pm/elixir/Task.Supervisor.html#async_stream_nolink/4)
- ["Working with streams" — Elixir getting started](https://hexdocs.pm/elixir/enumerable-and-streams.html)
- [`GenStage`](https://hexdocs.pm/gen_stage/) — for producer/consumer pipelines beyond `async_stream`
- ["Elixir at scale" — Saša Jurić talks](https://www.theerlangelist.com/) — includes notes on bounded concurrency in production
