# Task.async / Task.await: Building a Parallel URL Fetcher

**Project**: `url_fetch` — a concurrent URL fetcher that downloads N URLs in
parallel with per-request timeouts and bounded concurrency.

---

## Project structure

```
url_fetch/
├── lib/
│   └── url_fetch/
│       ├── http.ex
│       ├── fetcher.ex
│       └── stream_fetcher.ex
├── test/
│   └── url_fetch/
│       ├── fetcher_test.exs
│       └── stream_fetcher_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Core concepts

Two primitives from the `Task` module, which is the idiomatic way to run
short-lived concurrent work without writing receive loops by hand.

**1. `Task.async/1` + `Task.await/2`.** `Task.async/1` spawns a linked process
that runs a function; `Task.await/2` blocks the caller until that process
produces a result or the timeout fires. Under the hood it's exactly the same
ref/mailbox dance that `GenServer.call/3` uses — `Task` just packages it.

**2. `Task.async_stream/3`.** When you have a *collection* to process, never
spawn N tasks in a `for` loop. `Task.async_stream/3` gives you bounded
concurrency (`:max_concurrency`), per-item timeouts, and a lazy stream you can
pipe straight into `Enum`. It also supports `:on_timeout` to decide whether a
stuck item kills the whole pipeline or just the slow item.

A senior-level nuance: `Task.await/2` on a task that crashed raises an exit in
the *caller*. `Task.async_stream/3` yields `{:exit, reason}` instead — much
safer for batch pipelines where partial failure is acceptable.

---

## The business problem

You operate a service that aggregates data from dozens of third-party endpoints.
Serial fetching is O(N × network_latency) — minutes for 50 URLs. You need:

1. Parallel fetching with a concrete concurrency cap (don't DoS yourself).
2. Per-request timeouts so one slow host doesn't stall the batch.
3. Graceful handling of failures (timeouts, connection errors) — one failure
   must not kill the other 49.
4. A simple API: `fetch_all(urls) -> [{url, result}, ...]`.

---

## Why `Task` and not raw `spawn`/`receive`

- A raw `spawn` + `receive` loop works for the sync-reply pattern, but you rewrite the ref/mailbox/timeout dance every time.
- `Task.async/1` links the task to the caller, so a caller crash cleans up its workers automatically — no supervision boilerplate.
- `Task.yield/2 + shutdown/2` converts timeouts into `nil` instead of raising `exit` in the caller, which is what a batch pipeline actually wants.
- For N items with a concurrency cap, `Task.async_stream/3` gives you bounded parallelism without writing a pool.

---

## Design decisions

**Option A — `Task.await/2` for each task**
- Pros: shortest code; one line per await.
- Cons: on timeout, `await` **raises an exit in the caller**. The batch dies on the first slow URL unless you wrap every call in `try/catch`.

**Option B — `Task.yield/2 || Task.shutdown/2`** (chosen for `Fetcher`)
- Pros: timeouts become `nil` instead of exits; slow tasks are actively killed, not leaked; caller returns a partial result set with `{:error, :timeout}` for the slow items.
- Cons: slightly more code; two concepts to teach instead of one.

**Option C — `Task.async_stream/3` with `:max_concurrency`** (chosen for `StreamFetcher`)
- Pros: bounded concurrency for free; lazy stream composes with `Stream.take/2`; `on_timeout: :kill_task` gives the same partial-result semantics per item.
- Cons: the result shape is `{:ok, value}` / `{:exit, reason}` — callers have to normalize.

→ Chose **B** for the fixed-N fetcher (teaches timeouts explicitly) and **C** for the batch fetcher (teaches concurrency caps). Covering both is the lesson.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp deps do
  # No HTTP client — tests use an in-memory fake. stdlib only.
  []
end
```


### Step 1: Create the project

**Objective**: Scaffold the project so HTTP transport, per-task fetcher, and bounded-concurrency fetcher are separate modules with independent tests.

```bash
mix new url_fetch
cd url_fetch
```

### Step 2: `mix.exs`

**Objective**: Declare a behaviour-based HTTP dep surface so tests can swap the transport and stay fully offline while still exercising `Task` semantics.

```elixir
defmodule UrlFetch.MixProject do
  use Mix.Project

  def project do
    [
      app: :url_fetch,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

No external HTTP library — we inject a fake HTTP client so tests are
deterministic and the exercise stays focused on `Task`, not on networking.

### Step 3: `.formatter.exs`

**Objective**: Pin formatter rules so pipelines of `Task.async_stream/3` options stay readable across reviewers, where indentation carries meaning.

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### Step 4: `lib/url_fetch/http.ex`

**Objective**: Define a behaviour the transport must honour, so tests inject a deterministic impl and real deployments plug in a live HTTP client.

```elixir
defmodule UrlFetch.Http do
  @moduledoc """
  HTTP client behaviour.

  A behaviour (not a concrete client) lets tests inject a deterministic
  in-memory fake. This is the standard way to unit-test concurrent Elixir
  code without real sockets.
  """

  @callback get(url :: String.t(), timeout_ms :: non_neg_integer()) ::
              {:ok, status :: pos_integer(), body :: binary()}
              | {:error, term()}
end

defmodule UrlFetch.Http.Fake do
  @moduledoc """
  In-memory HTTP fake. The map `responses` maps URL -> response directive.

  Directives:
    * `{:ok, status, body}`           — return immediately
    * `{:sleep, ms, {:ok, s, body}}`  — sleep `ms` then return
    * `{:error, reason}`              — return an error tuple
  """

  @behaviour UrlFetch.Http

  @doc "Starts an Agent that stores the response map. Returns its pid."
  @spec start(map()) :: pid()
  def start(responses) when is_map(responses) do
    {:ok, pid} = Agent.start_link(fn -> responses end)
    Process.put(:url_fetch_fake_pid, pid)
    pid
  end

  @impl true
  def get(url, _timeout_ms) do
    # We look the fake pid up from the process dictionary so `get/2` keeps the
    # required 2-arity signature. In real code you'd pass it via options.
    pid = Process.get(:url_fetch_fake_pid) || raise "fake not started"

    case Agent.get(pid, &Map.get(&1, url, {:error, :not_configured})) do
      {:sleep, ms, response} ->
        Process.sleep(ms)
        response

      other ->
        other
    end
  end
end
```

### Step 5: `lib/url_fetch/fetcher.ex`

**Objective**: Use `Task.async`/`await` so each fetch runs in a linked process — the caller crashes if any task crashes, making failures loud, not silent.

```elixir
defmodule UrlFetch.Fetcher do
  @moduledoc """
  Parallel URL fetcher using `Task.async` + `Task.await`.

  This is the classic "spawn N tasks, await N results" pattern. It's fine when
  N is bounded and small (say, < 100). For large or unbounded inputs use
  `UrlFetch.StreamFetcher` instead — that version caps concurrency.
  """

  @default_timeout 5_000

  @doc """
  Fetches every URL in parallel and returns a list of `{url, result}` tuples in
  the same order as the input.

  `result` is `{:ok, status, body}` on success, or `{:error, reason}` on any
  failure — including `:timeout` when the per-request deadline fires.
  """
  @spec fetch_all([String.t()], module(), timeout()) :: [{String.t(), term()}]
  def fetch_all(urls, http_mod \\ UrlFetch.Http.Fake, timeout \\ @default_timeout) do
    tasks =
      Enum.map(urls, fn url ->
        # `Task.async` links the task to the caller. If the caller dies, the
        # task dies too — that's the desired lifecycle for a request handler.
        {url, Task.async(fn -> http_mod.get(url, timeout) end)}
      end)

    Enum.map(tasks, fn {url, task} ->
      # `Task.yield/2` + `Task.shutdown/2` is the RIGHT way to timeout.
      # Plain `Task.await/2` raises an `exit`, which kills the caller unless
      # you rescue — almost never what you want in a batch pipeline.
      result =
        case Task.yield(task, timeout) || Task.shutdown(task, :brutal_kill) do
          {:ok, value} -> value
          nil -> {:error, :timeout}
          {:exit, reason} -> {:error, {:exit, reason}}
        end

      {url, result}
    end)
  end
end
```

### Step 6: `lib/url_fetch/stream_fetcher.ex`

**Objective**: Bound concurrency with `Task.async_stream/3` so fetching 10k URLs never spawns 10k sockets, trading peak parallelism for stable resource use.

```elixir
defmodule UrlFetch.StreamFetcher do
  @moduledoc """
  Bounded-concurrency fetcher using `Task.async_stream/3`.

  Prefer this over `Fetcher.fetch_all/3` when:
    * The input list is large or unbounded.
    * You must respect a concurrency limit (rate limits, connection pools).
    * You want to stream results lazily instead of materialising a list.
  """

  @default_timeout 5_000
  @default_concurrency 10

  @doc """
  Returns a lazy Stream of `{url, result}` tuples.

  Options:
    * `:max_concurrency` — cap on in-flight requests. Defaults to 10.
    * `:timeout`         — per-URL timeout in ms. Defaults to 5_000.
    * `:http`            — module implementing `UrlFetch.Http`.
  """
  @spec stream([String.t()], keyword()) :: Enumerable.t()
  def stream(urls, opts \\ []) do
    http_mod = Keyword.get(opts, :http, UrlFetch.Http.Fake)
    timeout = Keyword.get(opts, :timeout, @default_timeout)
    max_conc = Keyword.get(opts, :max_concurrency, @default_concurrency)

    urls
    |> Task.async_stream(
      fn url -> {url, http_mod.get(url, timeout)} end,
      max_concurrency: max_conc,
      timeout: timeout,
      # `:kill_task` converts a per-item timeout into `{:exit, :timeout}`
      # on that item only — the stream keeps flowing. Without this the whole
      # pipeline would raise on the first slow URL.
      on_timeout: :kill_task,
      ordered: true
    )
    |> Stream.map(&normalize/1)
  end

  @doc """
  Eagerly collects the stream into a list. Convenience wrapper around `stream/2`.
  """
  @spec fetch_all([String.t()], keyword()) :: [{String.t(), term()}]
  def fetch_all(urls, opts \\ []) do
    urls |> stream(opts) |> Enum.to_list()
  end

  # `Task.async_stream/3` wraps every result in `{:ok, _}` or `{:exit, _}`.
  # We flatten that into our `{url, result}` contract.
  defp normalize({:ok, {url, {:ok, status, body}}}), do: {url, {:ok, status, body}}
  defp normalize({:ok, {url, {:error, reason}}}), do: {url, {:error, reason}}
  defp normalize({:exit, :timeout}), do: {:unknown, {:error, :timeout}}
  defp normalize({:exit, reason}), do: {:unknown, {:error, {:exit, reason}}}
end
```

### Step 7: Tests

**Objective**: Cover timeouts, task crashes, and on-timeout options to prove the fetcher fails loudly or degrades predictably, never mid-way silently.

```elixir
# test/url_fetch/fetcher_test.exs
defmodule UrlFetch.FetcherTest do
  use ExUnit.Case, async: true

  alias UrlFetch.{Fetcher, Http}

  setup do
    Http.Fake.start(%{
      "https://a" => {:ok, 200, "A"},
      "https://b" => {:ok, 200, "B"},
      "https://c" => {:error, :nxdomain},
      "https://slow" => {:sleep, 200, {:ok, 200, "S"}}
    })

    :ok
  end

  test "fetches multiple URLs in parallel and preserves order" do
    urls = ["https://a", "https://b"]
    results = Fetcher.fetch_all(urls, Http.Fake, 1_000)

    assert [{"https://a", {:ok, 200, "A"}}, {"https://b", {:ok, 200, "B"}}] = results
  end

  test "parallel execution is actually parallel" do
    # Two 200ms sleeps run concurrently should finish in ~200ms, not 400ms.
    Http.Fake.start(%{
      "u1" => {:sleep, 200, {:ok, 200, "1"}},
      "u2" => {:sleep, 200, {:ok, 200, "2"}}
    })

    {micros, results} =
      :timer.tc(fn -> Fetcher.fetch_all(["u1", "u2"], Http.Fake, 1_000) end)

    assert length(results) == 2
    # Generous bound (350ms) to avoid CI flakiness, but well below 400ms serial.
    assert micros < 350_000
  end

  test "returns errors from individual URLs without failing the batch" do
    urls = ["https://a", "https://c"]
    results = Fetcher.fetch_all(urls, Http.Fake, 1_000)

    assert {"https://a", {:ok, 200, "A"}} in results
    assert {"https://c", {:error, :nxdomain}} in results
  end

  test "applies per-URL timeout and marks the slow URL as :timeout" do
    results = Fetcher.fetch_all(["https://slow", "https://a"], Http.Fake, 50)

    assert {"https://slow", {:error, :timeout}} in results
    assert {"https://a", {:ok, 200, "A"}} in results
  end
end
```

```elixir
# test/url_fetch/stream_fetcher_test.exs
defmodule UrlFetch.StreamFetcherTest do
  use ExUnit.Case, async: true

  alias UrlFetch.{Http, StreamFetcher}

  setup do
    Http.Fake.start(%{
      "u1" => {:ok, 200, "1"},
      "u2" => {:ok, 200, "2"},
      "u3" => {:ok, 200, "3"},
      "u4" => {:ok, 200, "4"},
      "slow" => {:sleep, 300, {:ok, 200, "S"}}
    })

    :ok
  end

  test "caps concurrency at max_concurrency" do
    # With max_concurrency: 2, four 100ms requests should take ~200ms, not ~100.
    Http.Fake.start(
      Map.new(1..4, fn i -> {"u#{i}", {:sleep, 100, {:ok, 200, "#{i}"}}} end)
    )

    urls = ["u1", "u2", "u3", "u4"]

    {micros, results} =
      :timer.tc(fn ->
        StreamFetcher.fetch_all(urls, max_concurrency: 2, timeout: 1_000)
      end)

    assert length(results) == 4
    # 2 concurrent x 2 batches x 100ms = ~200ms. Give CI headroom up to 350ms,
    # but assert we didn't serialise (> 400ms) and didn't go wider (< 150ms).
    assert micros in 150_000..350_000
  end

  test "preserves input order with ordered: true" do
    urls = ["u1", "u2", "u3", "u4"]
    results = StreamFetcher.fetch_all(urls, max_concurrency: 4, timeout: 1_000)

    assert Enum.map(results, fn {u, _} -> u end) == urls
  end

  test "slow URL is killed by on_timeout: :kill_task and the rest succeed" do
    results =
      StreamFetcher.fetch_all(
        ["slow", "u1", "u2"],
        max_concurrency: 3,
        timeout: 50
      )

    assert length(results) == 3
    # The slow one comes back as a timeout; the others are fine.
    assert Enum.any?(results, fn {_, r} -> match?({:error, :timeout}, r) end)
    assert Enum.any?(results, fn {_, r} -> match?({:ok, 200, "1"}, r) end)
  end

  test "stream is lazy — wrapping in Stream.take/2 limits work" do
    urls = ["u1", "u2", "u3", "u4"]

    results =
      urls
      |> StreamFetcher.stream(max_concurrency: 4, timeout: 1_000)
      |> Stream.take(2)
      |> Enum.to_list()

    assert length(results) == 2
  end
end
```

### Step 8: Run

**Objective**: Run the full suite to validate that both fetchers meet their contract under load and degrade predictably on per-task failures.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test
mix format
```

### Why this works

`Task.async/1` is just `spawn_link/1` plus a monitor and a reply channel. The linked lifecycle means a caller crash takes its tasks with it — no orphaned processes. `Task.yield/2` is non-raising: it either returns `{:ok, value}` once the task replied, or `nil` once the deadline passed. Pairing it with `Task.shutdown/2, :brutal_kill` guarantees the task is dead before the caller moves on. `Task.async_stream/3` with `:max_concurrency` uses a sliding window internally — at most N tasks run concurrently, the rest queue lazily — so the pipeline stays bounded in memory regardless of input size.

---

## Executable Example

Create `lib/parallel_fetcher.ex` and test in `iex`:

```elixir
defmodule ParallelFetcher do
  def fetch_user_and_posts(user_id) do
    user_task = Task.async(fn -> fetch_user(user_id) end)
    posts_task = Task.async(fn -> fetch_posts(user_id) end)

    user = Task.await(user_task, 5000)
    posts = Task.await(posts_task, 5000)

    {:ok, user, posts}
  end

  defp fetch_user(user_id) do
    Process.sleep(100)
    %{id: user_id, name: "User #{user_id}"}
  end

  defp fetch_posts(user_id) do
    Process.sleep(150)
    [%{id: 1, title: "Post 1"}, %{id: 2, title: "Post 2"}]
  end

  def fetch_all_users(ids) do
    ids
    |> Enum.map(&Task.async(fn -> fetch_user(&1) end))
    |> Enum.map(&Task.await(&1, 5000))
  end
end

# Test it
{:ok, user, posts} = ParallelFetcher.fetch_user_and_posts(123)
IO.inspect(user)
IO.inspect(posts)

users = ParallelFetcher.fetch_all_users([1, 2, 3])
IO.inspect(length(users))
```

---
## Key Concepts

### 1. Tasks Are Lightweight Wrappers Around Spawned Functions

```elixir
task = Task.async(fn -> expensive_computation() end)
result = Task.await(task)  # Blocks until the task finishes
```

`Task.async` spawns a process and returns a task reference. `Task.await` blocks until the task finishes, returning its result or raising if it failed.

### 2. Tasks Integrate with Supervision

Tasks started with `Task.start_link` are linked to the parent process. If the task crashes, the parent gets an exit signal. If the parent crashes, the task terminates. This integration makes fault tolerance automatic.

### 3. Timeouts Prevent Hanging

`Task.await(task, 5000)` waits up to 5 seconds. If the task doesn't finish in time, it raises `Task.timeout`. Always set timeouts for tasks that communicate with external systems.

---
## Benchmark

```elixir
# bench/fetch.exs
UrlFetch.Http.Fake.start(
  Map.new(1..100, fn i -> {"u#{i}", {:sleep, 50, {:ok, 200, "#{i}"}}} end)
)

urls = Enum.map(1..100, &"u#{&1}")

{t_serial, _} = :timer.tc(fn -> Enum.map(urls, &UrlFetch.Http.Fake.get(&1, 1_000)) end)
{t_unbounded, _} = :timer.tc(fn -> UrlFetch.Fetcher.fetch_all(urls, UrlFetch.Http.Fake, 2_000) end)
{t_bounded, _} = :timer.tc(fn ->
  UrlFetch.StreamFetcher.fetch_all(urls, max_concurrency: 10, timeout: 2_000)
end)

IO.puts("serial: #{t_serial} µs   unbounded: #{t_unbounded} µs   bounded(10): #{t_bounded} µs")
```

Target: serial ≈ 5s (100 × 50ms), unbounded ≈ 50–80ms (all in parallel), bounded(10) ≈ 500ms (10 batches of 10). The bounded number should be ~N/concurrency × per-request latency — if it's far from that, either the fake is serializing or you hit the scheduler cap.

---

## Trade-off analysis

| Aspect | `Task.async` + `await` | `Task.async_stream` |
|--------|------------------------|----------------------|
| Concurrency cap | None — spawns N tasks | `:max_concurrency` |
| Memory | All tasks alive at once | Bounded in-flight set |
| Ordering | Preserved by construction | `ordered: true/false` |
| Timeout handling | Manual `yield` + `shutdown` | `on_timeout: :kill_task` |
| Best for | Small, fixed fan-out (< 100) | Large or streaming inputs |

| Aspect | `Task.await/2` | `Task.yield/2` + `shutdown/2` |
|--------|----------------|-------------------------------|
| Timeout signal | Raises `exit` in caller | Returns `nil` |
| Cleanup | None — task may outlive caller | `shutdown` kills the task |
| Use | Fire-and-forget happy path | Production batch pipelines |

---

## Common production mistakes

**1. Using `Task.await/2` in a batch pipeline.**
One slow task raises in the caller and bubbles up, killing everything. Use
`Task.yield/2` + `Task.shutdown/2` (as `Fetcher.fetch_all/3` does) or
`Task.async_stream/3` with `on_timeout: :kill_task`.

**2. Spawning unbounded tasks from a list of unknown size.**
`urls |> Enum.map(&Task.async(...))` with 100_000 URLs creates 100_000
processes, 100_000 sockets, and a predictable OOM. Always use
`Task.async_stream/3` when N is not tiny and fixed.

**3. Leaking tasks after the caller returns.**
A `Task.async/1` is linked to the caller. If the caller catches an exit and
continues, the tasks may still be alive. Always pair an async with an
`await`, `yield`+`shutdown`, or use `Task.Supervisor` for fire-and-forget.

**4. Treating `:infinity` timeouts as "safe".**
`Task.async_stream/3` defaults to a 5s timeout. Overriding to `:infinity`
hides real bugs (a wedged process) until production traffic exposes them.

**5. Forgetting that `async_stream` returns `{:ok, value}` / `{:exit, reason}`.**
Pattern-matching `{:ok, body}` instead of `{:ok, {:ok, body}}` silently
discards successful results as "not matching".

---

## When NOT to use `Task`

- You need long-lived state — use `Agent` or `GenServer`.
- You need custom request/response with correlation — build the ref/`receive` loop directly.
- The work is CPU-bound and you want parallelism across cores — `Task` works,
  but check `:erlang.system_info(:schedulers_online)` and size
  `max_concurrency` accordingly.
- You need a named, discoverable process — use `Registry`.

---

## Reflection

- Your pipeline occasionally leaks processes on timeout. The audit shows `Task.await/2` is used in one path and `Task.yield + shutdown` in another. Walk through what the BEAM does in each case when the task runs 1ms past the deadline. Which code path does the leaking?
- With 10k URLs and `max_concurrency: 100`, you hit the HTTP client's connection pool cap (say 50). Where does backpressure happen — in `async_stream`, in the HTTP client, or in the OS? What signal tells you which is the bottleneck?

---

## Resources

- [`Task` — HexDocs](https://hexdocs.pm/elixir/Task.html)
- [`Task.async_stream/3` — HexDocs](https://hexdocs.pm/elixir/Task.html#async_stream/3)
- [`Task.yield/2` — HexDocs](https://hexdocs.pm/elixir/Task.html#yield/2)
- [Elixir School — Concurrency](https://elixirschool.com/en/lessons/intermediate/concurrency)
- [Saša Jurić — "The Zen of Erlang"](https://ferd.ca/the-zen-of-erlang.html)
