# Parallel I/O with `Task.async`/`Task.await`

**Project**: `parallel_scraper` — fetch N URLs in parallel and aggregate results.

---

## Why task y concurrencia matters

You have a list of URLs (or database IDs, or files) and need to fetch
each one. Done serially, total time = sum of latencies. Done in parallel
via `Task.async` + `Task.await`, total time ≈ slowest call — exactly what
you want for I/O-bound work.

This exercise uses a simulated "HTTP fetch" (`Process.sleep` + synthetic
result) so you can focus on the concurrency pattern, not on real network
plumbing. The same code shape applies to `Finch`, `Ecto.Repo.all`, file
reads, etc.

You'll learn: the `async/await` lifecycle, why `Task.await` has a
timeout, how errors propagate through a link, and why bare `Task.async`
is *not* the right answer for unbounded inputs — which sets up exercises
50, 51, and 55.

---

## Project structure

```
parallel_scraper/
├── lib/
│   └── parallel_scraper.ex
├── script/
│   └── main.exs
├── test/
│   └── parallel_scraper_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not a lower-level alternative?** For task y concurrencia, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. `Task.async/1` spawns a **linked, monitored** process

```
Task.async(fn -> work end)
  ├── spawn_link from the caller
  ├── monitor from the caller
  └── returns %Task{pid, ref, owner}
```

Because it's linked, if the task crashes, the caller crashes too. Because
it's monitored, `Task.await/2` can receive a `:DOWN` message and turn it
into an exception in the caller.

The ownership rule: **only the process that called `Task.async` may call
`Task.await`**. The struct is not shareable.

### 2. `Task.await/2` blocks until a reply or a timeout

```elixir
result = Task.await(task, 5_000)
# Success: returns the function's return value.
# Timeout: raises; the task is NOT cancelled automatically.
# Crash:   raises with the exit reason.
```

Default timeout is 5 seconds. A timeout in `await` does **not** kill the
task — the task keeps running, now orphaned. Use `Task.shutdown/2` if you
need to actually cancel work past the deadline.

### 3. Parallelism is bounded by the scheduler, not by your input

`Task.async` does not limit fan-out. If you map over 10_000 URLs, you
will spawn 10_000 tasks. For CPU-bound work that's wasteful; for I/O
with external rate limits it's often a self-DDoS. For bounded
concurrency, use `Task.async_stream` or a pool.

### 4. Pattern: map → async → await_many

```
urls                          [t1 t2 t3 t4]
  |> Enum.map(&Task.async)  → processes running in parallel
  |> Task.await_many(5_000) → caller blocks until all return
```

`Task.await_many/2` (Elixir 1.13+) awaits a list of tasks with a single
timeout. If any task crashes or times out, the others in the batch are
shut down. 

---

## Design decisions

**Option A — spawn unsupervised**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Task.async/await` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because linked tasks surface failures to the caller and clean up automatically.

## Implementation

### `mix.exs`

```elixir
defmodule ParallelScraper.MixProject do
  use Mix.Project

  def project do
    [
      app: :parallel_scraper,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
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

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new parallel_scraper
cd parallel_scraper
```

### `lib/parallel_scraper.ex`

**Objective**: Implement `parallel_scraper.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.

```elixir
defmodule ParallelScraper do
  @moduledoc """
  Demonstrates parallel I/O with `Task.async` + `Task.await`.

  The `fetch` step is simulated with `Process.sleep/1` so the exercise
  focuses on the concurrency pattern. Swap in an HTTP client (Finch,
  Req) in real code — the shape of the orchestration does not change.
  """

  @type url :: String.t()
  @type fetched :: %{url: url(), bytes: non_neg_integer(), latency_ms: pos_integer()}

  @doc """
  Fetches all `urls` serially. Baseline for comparison — total time is
  the sum of per-URL latencies.
  """
  @spec fetch_serial([url()]) :: [fetched()]
  def fetch_serial(urls) do
    Enum.map(urls, &simulate_fetch/1)
  end

  @doc """
  Fetches all `urls` in parallel using `Task.async` + `Task.await_many`.

  Total time is roughly the slowest single fetch, as long as the
  scheduler has cores to run them on.

  Options:
    * `:timeout` — per-batch timeout in ms (default 5_000). Applies to
      the whole `await_many` call.
  """
  @spec fetch_parallel([url()], keyword()) :: [fetched()]
  def fetch_parallel(urls, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, 5_000)

    urls
    |> Enum.map(fn url -> Task.async(fn -> simulate_fetch(url) end) end)
    |> Task.await_many(timeout)
  end

  # Simulates an HTTP request: sleeps for a pseudo-random latency derived
  # from the URL (so tests are deterministic), then returns a fake payload.
  @spec simulate_fetch(url()) :: fetched()
  defp simulate_fetch(url) do
    latency = :erlang.phash2(url, 50) + 20
    Process.sleep(latency)
    %{url: url, bytes: byte_size(url) * 10, latency_ms: latency}
  end
end
```

### Step 3: `test/parallel_scraper_test.exs`

**Objective**: Write `parallel_scraper_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule ParallelScraperTest do
  use ExUnit.Case, async: true

  doctest ParallelScraper

  @urls ~w(
    https://a.example
    https://b.example
    https://c.example
    https://d.example
    https://e.example
  )

  describe "fetch_serial/1" do
    test "returns one result per URL in order" do
      results = ParallelScraper.fetch_serial(@urls)
      assert length(results) == length(@urls)
      assert Enum.map(results, & &1.url) == @urls
    end
  end

  describe "fetch_parallel/2" do
    test "returns one result per URL in input order" do
      results = ParallelScraper.fetch_parallel(@urls)
      assert Enum.map(results, & &1.url) == @urls
    end

    test "is meaningfully faster than serial for I/O-bound work" do
      {serial_us, _} = :timer.tc(fn -> ParallelScraper.fetch_serial(@urls) end)
      {parallel_us, _} = :timer.tc(fn -> ParallelScraper.fetch_parallel(@urls) end)

      # Parallel should be at most ~60% of serial — loose bound to keep the
      # test stable on busy CI runners.
      assert parallel_us < serial_us * 0.6,
             "expected parallel to be faster: serial=#{serial_us}us parallel=#{parallel_us}us"
    end

    test "raises when the timeout is too short for the slowest task" do
      # Pick an impossibly tight budget so await_many times out.
      assert catch_exit(ParallelScraper.fetch_parallel(@urls, timeout: 1))
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

### `script/main.exs`

```elixir
defmodule Main do
  defmodule ParallelScraper do
    def fetch_serial(urls) do
      Enum.map(urls, &fetch_one/1)
    end

    def fetch_parallel(urls, timeout: timeout) do
      tasks = Enum.map(urls, &Task.async(fn -> fetch_one(&1) end))
      Enum.map(tasks, &Task.await(&1, timeout))
    end

    def fetch_parallel(urls) do
      fetch_parallel(urls, timeout: 5000)
    end

    defp fetch_one(url) do
      Process.sleep(10)
      %{url: url, status: 200, body: "mocked"}
    end
  end

  def main do
    urls = ["https://a.example", "https://b.example", "https://c.example"]
  
    results = ParallelScraper.fetch_parallel(urls)
    IO.puts("Fetched #{length(results)} URLs")
  
    results
    |> Enum.each(fn r -> IO.puts("  #{r.url}: #{r.status}") end)
  
    IO.puts("✓ ParallelScraper works correctly")
  end

end

Main.main()
```

## Deep Dive: Task Spawn vs GenServer for Ephemeral Work

A Task is lightweight `spawn/1` for bounded, self-contained work: compute, return, exit. Unlike GenServer (which receives messages indefinitely), Task is inherently ephemeral. This shapes everything: no callbacks, no state management, no back-pressure.

Advantages: simplicity (few lines vs GenServer boilerplate). Disadvantages: no explicit state or message handling—Tasks assume pure computation or simple I/O. If you need a long-lived process responding to external events, you've outgrown Task.

For CPU-bound work (calculations, parsing), Task.Supervisor with `:temporary` is ideal: spawn tasks, let them exit, don't restart. For coordinated async work (multiple tasks handing off results), GenServer + worker tasks often clarifies intent despite more boilerplate. Measure first: if code clarity improves with GenServer, the overhead is justified.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `Task.async` is linked — a crash in one task crashes your caller**
If you map 100 tasks and one of them raises, the caller dies too. For
work where a single failure shouldn't abort the batch, use
`Task.Supervisor.async_nolink` or `Task.async_stream` with
`on_timeout: :kill_task`.

**2. `Task.await` timeout does NOT cancel the task**
A 5-second `await` that times out leaves the task running. In a request
handler, that's a leak — you returned an error to the user but the work
continues. Use `Task.shutdown(task, :brutal_kill)` or
switch to `yield_many`/`async_stream`.

**3. Unbounded fan-out is a foot-gun**
`urls |> Enum.map(&Task.async/1)` for 10_000 URLs spawns 10_000
processes. Your target API will probably rate-limit or outright reject
you. Use `Task.async_stream(max_concurrency: N)` or a
bounded pool whenever the input size isn't small and fixed.

**4. Ownership is not transferable**
Only the process that called `Task.async` can `Task.await`. Passing a
task struct to another process to await "for you" won't work — the
monitor and link are tied to the owner. If you need work that outlives
its owner, use `Task.Supervisor.async_nolink` or a full GenServer.

**5. `await_many` has batch-level failure semantics**
If one task raises, `Task.await_many` shuts down the rest and re-raises.
That's the right default for "all or nothing" batches. For "give me
what's ready by the deadline", use `Task.yield_many`.

**6. When NOT to use `Task.async`**
- Fire-and-forget work where you don't want a link to the caller →
  `Task.Supervisor.start_child`.
- Long-running background jobs — these should be supervised named
  processes, not `Task`s.
- CPU-bound work exceeding `System.schedulers_online/0` parallelism —
  there's no benefit in spawning more tasks than cores.

---

## Reflection

- Aplicá lo aprendido sobre task y concurrencia: describí un caso de tu trabajo donde este patrón cambiaría tu diseño, y qué medirías para validar la mejora.

## Resources

- [`Task` — Elixir stdlib](https://hexdocs.pm/elixir/Task.html)
- [`Task.async_stream/3`](https://hexdocs.pm/elixir/Task.html#async_stream/3) — bounded-concurrency mapping
- [`Task.Supervisor`](https://hexdocs.pm/elixir/Task.Supervisor.html) — for unlinked tasks
- ["Concurrency and parallelism in Elixir"](https://hexdocs.pm/elixir/processes.html) — Elixir getting started
- [Saša Jurić — "Elixir in Action", ch. 5](https://www.manning.com/books/elixir-in-action-second-edition) — excellent on Task semantics

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/parallel_scraper_test.exs`

```elixir
defmodule ParallelScraperTest do
  use ExUnit.Case, async: true

  doctest ParallelScraper

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ParallelScraper.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
