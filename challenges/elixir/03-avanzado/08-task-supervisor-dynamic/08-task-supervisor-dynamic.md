# Task.Supervisor: Supervised Concurrency in the Gateway

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Three use cases have emerged that require concurrent
work with proper supervision:

1. **Fan-out health checks**: the watchdog (exercise 03) must ping up to 100 circuit
   breaker workers in parallel. Sequential pings with a 1-second timeout each would
   take up to 100 seconds — unacceptable for a 10-second check interval.

2. **Async webhook delivery**: when a circuit breaker opens or closes, the gateway
   fires a webhook to an alerting system. This must not block the circuit breaker
   worker's main loop.

3. **Upstream probe batch**: before routing a request, the gateway sometimes probes
   a list of candidate upstreams and picks the fastest to respond. Partial failures
   (one upstream slow/down) must not abort the entire batch.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       └── circuit_breaker/
│           ├── worker.ex
│           └── watchdog.ex            # ← extend with async_stream health checks
│       └── middleware/
│           └── webhook_notifier.ex    # ← you implement this (async_nolink)
│       └── router/
│           └── upstream_prober.ex     # ← you implement this (yield_many)
├── test/
│   └── api_gateway/
│       ├── circuit_breaker/
│       │   └── watchdog_test.exs      # given tests — must pass
│       └── router/
│           └── upstream_prober_test.exs # given tests — must pass
└── mix.exs
```

---

## `Task`, `Task.Supervisor`, and the link model

**`Task.async/1`** — creates a task linked to the calling process. If the task crashes,
the caller crashes too (unless it has `trap_exit`). Use when the caller depends on the
task's result and a crash should propagate.

**`Task.Supervisor.async/3`** — same link semantics, but the task is also under a
supervisor. The supervisor does not prevent the caller from crashing — the link still
exists. Use when you want supervision AND you own the result.

**`Task.Supervisor.async_nolink/3`** — supervised but **no link to the caller**. A
task crash does NOT kill the caller. The caller receives `{ref, result}` and
`{:DOWN, ref, :process, pid, reason}` messages. Use for fire-and-forget work, or when
you want fault isolation between the task and its launcher.

**`Task.Supervisor.start_child/2`** — truly fire-and-forget. No result message, no
DOWN notification. The task runs and dies silently. Use when you neither need the
result nor want to handle the completion message.

```
                 Link?  Caller survives   Result delivery
                         task crash?
async              Yes    No (propagates)  Task.await/yield
Supervisor.async   Yes    No (propagates)  Task.await/yield
async_nolink       No     Yes              receive pattern
start_child        No     Yes              None
```

---

## `Task.Supervisor.async_stream` back-pressure

`async_stream` processes an enumerable concurrently with a bounded concurrency limit.
It will not launch the next task until a slot frees up — this is true back-pressure.

```elixir
Task.Supervisor.async_stream(
  MyApp.TaskSupervisor,
  items,
  fn item -> work(item) end,
  max_concurrency: 10,        # at most 10 concurrent tasks
  timeout: 5_000,             # each task gets 5 seconds
  on_timeout: :kill_task      # kill timeout tasks (not :ignore — that leaks)
)
# Returns a stream of {:ok, result} | {:exit, reason}
```

`on_timeout: :kill_task` terminates the timed-out task. The result is `{:exit, :timeout}`.
`on_timeout: :ignore` returns `{:exit, :timeout}` but leaves the task alive — this
leaks processes and, for I/O-bound work, leaks connections.

---

## Implementation

### Step 1: Add `Task.Supervisor` to the supervision tree

```elixir
# In lib/api_gateway/application.ex (or CoreSupervisor):
{Task.Supervisor, name: ApiGateway.TaskSupervisor}
```

### Step 2: Extend `CircuitBreaker.Watchdog` with async health checks

```elixir
# In lib/api_gateway/circuit_breaker/watchdog.ex

@impl true
def handle_info(:health_check, state) do
  # TODO: replace sequential checks with Task.Supervisor.async_stream
  #
  # For each {service_name, pid} in state.registry:
  #   1. Call Worker.ping(pid) with @ping_timeout_ms
  #   2. If :pong → healthy
  #   3. If task exits (timeout or crash) → unresponsive:
  #      a. Process.exit(pid, :kill)
  #      b. DynamicSupervisor.start_child(state.supervisor, {Worker, service_name})
  #      c. Update registry with new pid on success, remove entry on failure
  #
  # HINT:
  #   state.registry
  #   |> Task.async_stream(
  #     fn {name, pid} -> {name, pid, check_worker(pid)} end,
  #     supervisor: ApiGateway.TaskSupervisor,
  #     max_concurrency: map_size(state.registry),
  #     timeout: @ping_timeout_ms + 500,
  #     on_timeout: :kill_task
  #   )
  #   |> Enum.reduce(state.registry, fn result, reg ->
  #     handle_check_result(result, reg, state.supervisor)
  #   end)
  {:noreply, state}
end

defp check_worker(pid) do
  # TODO: try GenServer.call(pid, :ping, @ping_timeout_ms)
  # Return :healthy or :unresponsive
  # HINT:
  #   try do
  #     GenServer.call(pid, :ping, @ping_timeout_ms)
  #     :healthy
  #   catch
  #     :exit, _ -> :unresponsive
  #   end
  :healthy
end

defp handle_check_result({:ok, {name, _pid, :healthy}}, registry, _sup) do
  registry
end

defp handle_check_result({:ok, {name, pid, :unresponsive}}, registry, sup) do
  # TODO: kill, restart, update registry
  require Logger
  Logger.warning("Watchdog: #{name} unresponsive — restarting")
  Process.exit(pid, :kill)

  case DynamicSupervisor.start_child(sup, {ApiGateway.CircuitBreaker.Worker, name}) do
    {:ok, new_pid} ->
      Logger.info("Watchdog: #{name} restarted as #{inspect(new_pid)}")
      Map.put(registry, name, new_pid)

    {:error, reason} ->
      Logger.error("Watchdog: failed to restart #{name}: #{inspect(reason)}")
      Map.delete(registry, name)
  end
end

defp handle_check_result({:exit, reason}, registry, _sup) do
  require Logger
  Logger.error("Watchdog: health check task crashed: #{inspect(reason)}")
  registry
end
```

### Step 3: `lib/api_gateway/middleware/webhook_notifier.ex`

```elixir
defmodule ApiGateway.Middleware.WebhookNotifier do
  require Logger

  @timeout_ms 5_000

  @doc """
  Fires a webhook for a circuit breaker state change.
  Returns :ok immediately — delivery happens asynchronously.
  The caller (circuit breaker worker) is not affected if delivery fails.
  """
  @spec notify_async(String.t(), :open | :closed | :half_open) :: :ok
  def notify_async(service_name, new_state) do
    # TODO: use Task.Supervisor.start_child/2 for true fire-and-forget
    # (async_nolink would send result messages to this caller, which we don't want)
    #
    # HINT:
    #   Task.Supervisor.start_child(ApiGateway.TaskSupervisor, fn ->
    #     deliver_webhook(service_name, new_state)
    #   end)
    :ok
  end

  @doc """
  Delivers webhooks to a list of URLs in parallel.
  Returns %{ok: [url], errors: [{url, reason}]}.
  Partial failures do not abort other deliveries.
  """
  @spec deliver_to_all([String.t()], map()) ::
          %{ok: [String.t()], errors: [{String.t(), term()}]}
  def deliver_to_all(urls, payload) do
    results =
      Task.Supervisor.async_stream(
        ApiGateway.TaskSupervisor,
        urls,
        fn url -> {url, deliver_one(url, payload)} end,
        max_concurrency: 10,
        timeout: @timeout_ms,
        on_timeout: :kill_task
      )
      |> Enum.to_list()

    Enum.zip(urls, results)
    |> Enum.reduce(%{ok: [], errors: []}, fn {url, task_result}, acc ->
      case task_result do
        {:ok, {^url, :ok}} ->
          %{acc | ok: [url | acc.ok]}

        {:ok, {^url, {:error, reason}}} ->
          %{acc | errors: [{url, reason} | acc.errors]}

        {:exit, :timeout} ->
          %{acc | errors: [{url, :timeout} | acc.errors]}

        {:exit, reason} ->
          %{acc | errors: [{url, reason} | acc.errors]}
      end
    end)
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp deliver_webhook(service_name, new_state) do
    Logger.info("Webhook: #{service_name} circuit #{new_state}")
    # Simulate HTTP delivery
    Process.sleep(:rand.uniform(200))
    if :rand.uniform(10) > 8, do: raise("SMTP unavailable"), else: :ok
  end

  defp deliver_one(url, payload) do
    Logger.debug("Delivering webhook to #{url}: #{inspect(payload)}")
    Process.sleep(:rand.uniform(100))
    if String.starts_with?(url, "http://"), do: :ok, else: {:error, :invalid_url}
  end
end
```

### Step 4: `lib/api_gateway/router/upstream_prober.ex`

```elixir
defmodule ApiGateway.Router.UpstreamProber do
  require Logger

  @probe_timeout_ms 2_000

  @doc """
  Probes a list of upstream URLs and returns results categorized by outcome.
  {:ok, results} where results = %{responsive: [url], slow: [url], down: [url]}
  """
  @spec probe_all([String.t()]) ::
          {:ok, %{responsive: [String.t()], slow: [String.t()], down: [String.t()]}}
  def probe_all(urls) when is_list(urls) do
    tasks =
      Enum.map(urls, fn url ->
        Task.Supervisor.async_nolink(ApiGateway.TaskSupervisor, fn ->
          {url, probe_one(url)}
        end)
      end)

    # TODO: use Task.yield_many/2 to collect results within @probe_timeout_ms
    # Then cancel any tasks that did not finish.
    #
    # HINT:
    #   raw_results = Task.yield_many(tasks, @probe_timeout_ms)
    #
    #   # Cancel timed-out tasks
    #   Enum.each(raw_results, fn
    #     {task, nil} -> Task.shutdown(task, :brutal_kill)
    #     _ -> :ok
    #   end)
    #
    # Categorize results:
    #   {:ok, {url, :ok}}         → responsive
    #   nil (did not finish)       → slow
    #   {:exit, reason}            → down

    {:ok, %{responsive: [], slow: [], down: []}}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp probe_one(url) do
    # Simulate upstream probe — some are fast, some slow, some fail
    delay = :rand.uniform(3_000)
    Process.sleep(delay)
    if delay > 2_500, do: raise("upstream timeout"), else: :ok
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/watchdog_test.exs
defmodule ApiGateway.CircuitBreaker.WatchdogTest do
  use ExUnit.Case, async: false

  alias ApiGateway.CircuitBreaker.{Worker, Watchdog}

  setup do
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)
    workers =
      for name <- ["w1", "w2", "w3", "w4", "w5"] do
        {:ok, pid} = DynamicSupervisor.start_child(sup, {Worker, name})
        {name, pid}
      end
      |> Map.new()
    {:ok, _} = Watchdog.start_link(supervisor: sup, registry: workers)
    on_exit(fn -> DynamicSupervisor.stop(sup) end)
    %{supervisor: sup, workers: workers}
  end

  test "health check completes within 2x single-ping timeout for 5 workers" do
    # With parallel checks, 5 workers × 1s should finish in ~1s, not ~5s
    {elapsed_us, _} = :timer.tc(fn ->
      send(Process.whereis(Watchdog), :health_check)
      Process.sleep(1_500)   # give it time to complete
    end)
    # Check that it didn't take 5+ seconds (serial would)
    assert elapsed_us < 4_000_000
  end

  test "dead worker is replaced and registry is updated", %{workers: workers} do
    {name, old_pid} = Enum.at(workers, 0)
    Process.exit(old_pid, :kill)
    Process.sleep(50)

    send(Process.whereis(Watchdog), :health_check)
    Process.sleep(500)

    registry = Watchdog.registry()
    new_pid = Map.get(registry, name)
    assert is_pid(new_pid)
    assert new_pid != old_pid
    assert Process.alive?(new_pid)
  end
end
```

```elixir
# test/api_gateway/router/upstream_prober_test.exs
defmodule ApiGateway.Router.UpstreamProberTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Router.UpstreamProber

  test "probe_all returns categorized results" do
    urls = ["http://svc-a:8080", "http://svc-b:8080", "http://svc-c:8080"]
    {:ok, results} = UpstreamProber.probe_all(urls)

    assert Map.has_key?(results, :responsive)
    assert Map.has_key?(results, :slow)
    assert Map.has_key?(results, :down)

    all_urls = results.responsive ++ results.slow ++ results.down
    assert Enum.sort(all_urls) == Enum.sort(urls)
  end

  test "probe_all with empty list returns empty categories" do
    assert {:ok, %{responsive: [], slow: [], down: []}} = UpstreamProber.probe_all([])
  end
end
```

### Step 6: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/watchdog_test.exs \
         test/api_gateway/router/upstream_prober_test.exs --trace
```

---

## Trade-off analysis

| API | Link? | Result delivery | Use case |
|-----|-------|-----------------|----------|
| `Task.async` | Yes | `Task.await/yield` | Caller must have result; crash propagates |
| `Task.Supervisor.async` | Yes | `Task.await/yield` | Supervised; crash still propagates |
| `Task.Supervisor.async_nolink` | No | `receive` pattern | Fault-isolated; partial failure OK |
| `Task.Supervisor.start_child` | No | None | True fire-and-forget |
| `Task.Supervisor.async_stream` | No | Stream | Bounded fan-out over collections |

Reflection question: `async_nolink` sends `{ref, result}` and `{:DOWN, ref, ...}`
messages to the calling process even if you never read them. In a GenServer that calls
`async_nolink` frequently but never handles those messages, what happens to the mailbox
over time? What is the correct API to use instead?

---

## Common production mistakes

**1. Not cancelling orphan tasks**
If you launch tasks with `Task.Supervisor.async/3` or `async_nolink/3` and never call
`Task.await`, `Task.yield`, or `Task.shutdown`, the task runs to completion and sends
its result into your process mailbox where it accumulates forever. Always match what
you start: `async` → `await`; `async_nolink` → `receive` or `Task.shutdown`.

**2. `async_stream` without `max_concurrency`**
`Task.Supervisor.async_stream(sup, 100_000_urls, &fetch/1)` launches 100,000 tasks
simultaneously. Each opens a connection, exhausts the OS file descriptor limit, and
the node crashes. Always set `max_concurrency` based on the limiting resource.

**3. `on_timeout: :ignore` in `async_stream`**
With `:ignore`, a timed-out task continues running in the background after `async_stream`
moves on. If it holds an HTTP connection, that connection is never returned to the pool.
Always use `:kill_task` for I/O-bound work.

**4. Using `Task.Supervisor` for long-running processes**
`Task` is designed for bounded computation. Starting a GenServer-like process via
`Task.Supervisor.start_child` creates a process that runs indefinitely without proper
lifecycle management. Long-running processes belong under a `Supervisor`, not a
`Task.Supervisor`.

---

## Resources

- [HexDocs — Task.Supervisor](https://hexdocs.pm/elixir/Task.Supervisor.html)
- [HexDocs — Task](https://hexdocs.pm/elixir/Task.html) — `yield_many/2`, `shutdown/2`
- [Concurrent Data Processing in Elixir — Saša Jurić](https://pragprog.com/titles/sgdpelixir/)
- [HexDocs — Task.yield_many/2](https://hexdocs.pm/elixir/Task.html#yield_many/2)
