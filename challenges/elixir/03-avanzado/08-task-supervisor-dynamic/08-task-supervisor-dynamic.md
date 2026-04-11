# Task.Supervisor: Supervised Concurrency in the Gateway

## Goal

Build three components demonstrating different `Task.Supervisor` patterns: a watchdog that health-checks workers in parallel via `async_stream`, a webhook notifier using fire-and-forget `start_child`, and an upstream prober using `async_nolink` with `Task.yield_many` for fault-isolated partial results.

---

## Task, Task.Supervisor, and the link model

| API | Link? | Caller survives crash? | Result delivery |
|-----|-------|------------------------|-----------------|
| `Task.async/1` | Yes | No (propagates) | `Task.await/yield` |
| `Task.Supervisor.async/3` | Yes | No (propagates) | `Task.await/yield` |
| `Task.Supervisor.async_nolink/3` | No | Yes | `receive` pattern |
| `Task.Supervisor.start_child/2` | No | Yes | None (fire-and-forget) |
| `Task.Supervisor.async_stream` | No | Yes | Stream |

---

## Full implementation

### Circuit breaker worker (needed by watchdog)

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name)
  end

  @impl true
  def init(service_name) do
    {:ok, %{service: service_name, status: :closed, failures: 0}}
  end

  @impl true
  def handle_call(:ping, _from, state) do
    {:reply, :pong, state}
  end

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state}
  end
end
```

### `lib/api_gateway/circuit_breaker/watchdog.ex`

Uses `Task.async_stream` to check all workers in parallel with bounded concurrency.

```elixir
defmodule ApiGateway.CircuitBreaker.Watchdog do
  use GenServer
  require Logger

  @check_interval_ms 10_000
  @ping_timeout_ms   1_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec registry() :: %{String.t() => pid()}
  def registry, do: GenServer.call(__MODULE__, :registry)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    supervisor = Keyword.fetch!(opts, :supervisor)
    registry   = Keyword.get(opts, :registry, %{})

    {:ok, _ref} = :timer.send_interval(@check_interval_ms, :health_check)
    {:ok, %{supervisor: supervisor, registry: registry}}
  end

  @impl true
  def handle_call(:registry, _from, state) do
    {:reply, state.registry, state}
  end

  @impl true
  def handle_info(:health_check, state) do
    updated_registry =
      state.registry
      |> Task.async_stream(
        fn {name, pid} -> {name, pid, check_worker(pid)} end,
        max_concurrency: max(map_size(state.registry), 1),
        timeout: @ping_timeout_ms + 500,
        on_timeout: :kill_task
      )
      |> Enum.reduce(state.registry, fn result, reg ->
        handle_check_result(result, reg, state.supervisor)
      end)

    {:noreply, %{state | registry: updated_registry}}
  end

  defp check_worker(pid) do
    try do
      GenServer.call(pid, :ping, @ping_timeout_ms)
      :healthy
    catch
      :exit, _ -> :unresponsive
    end
  end

  defp handle_check_result({:ok, {_name, _pid, :healthy}}, registry, _sup) do
    registry
  end

  defp handle_check_result({:ok, {name, pid, :unresponsive}}, registry, sup) do
    Logger.warning("Watchdog: #{name} unresponsive -- restarting")
    Process.exit(pid, :kill)

    case DynamicSupervisor.start_child(sup, {ApiGateway.CircuitBreaker.Worker, name}) do
      {:ok, new_pid} ->
        Map.put(registry, name, new_pid)
      {:error, _reason} ->
        Map.delete(registry, name)
    end
  end

  defp handle_check_result({:exit, _reason}, registry, _sup) do
    registry
  end
end
```

### `lib/api_gateway/middleware/webhook_notifier.ex`

Demonstrates `start_child` for fire-and-forget and `async_stream` for fan-out with result collection.

```elixir
defmodule ApiGateway.Middleware.WebhookNotifier do
  require Logger

  @timeout_ms 5_000

  @doc """
  Fires a webhook for a circuit breaker state change.
  Returns :ok immediately -- delivery happens asynchronously.
  """
  @spec notify_async(String.t(), :open | :closed | :half_open) :: :ok
  def notify_async(service_name, new_state) do
    Task.Supervisor.start_child(ApiGateway.TaskSupervisor, fn ->
      Logger.info("Webhook: #{service_name} circuit #{new_state}")
      Process.sleep(:rand.uniform(200))
    end)
    :ok
  end

  @doc """
  Delivers webhooks to a list of URLs in parallel.
  Returns %{ok: [url], errors: [{url, reason}]}.
  """
  @spec deliver_to_all([String.t()], map()) :: %{ok: [String.t()], errors: [{String.t(), term()}]}
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

  defp deliver_one(url, _payload) do
    Process.sleep(:rand.uniform(100))
    if String.starts_with?(url, "http://"), do: :ok, else: {:error, :invalid_url}
  end
end
```

### `lib/api_gateway/router/upstream_prober.ex`

Uses `async_nolink` with `Task.yield_many` to probe multiple upstreams concurrently with fault isolation.

```elixir
defmodule ApiGateway.Router.UpstreamProber do
  require Logger

  @probe_timeout_ms 2_000

  @doc """
  Probes a list of upstream URLs and returns results categorized by outcome.
  {:ok, %{responsive: [url], slow: [url], down: [url]}}
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

    raw_results = Task.yield_many(tasks, @probe_timeout_ms)

    # Shut down tasks that did not finish within the timeout
    Enum.each(raw_results, fn
      {task, nil} -> Task.shutdown(task, :brutal_kill)
      _ -> :ok
    end)

    categorized =
      Enum.zip(urls, raw_results)
      |> Enum.reduce(%{responsive: [], slow: [], down: []}, fn {url, {_task, result}}, acc ->
        case result do
          {:ok, {^url, :ok}} ->
            %{acc | responsive: [url | acc.responsive]}
          nil ->
            %{acc | slow: [url | acc.slow]}
          {:exit, _reason} ->
            %{acc | down: [url | acc.down]}
          _ ->
            %{acc | down: [url | acc.down]}
        end
      end)

    {:ok, categorized}
  end

  defp probe_one(_url) do
    delay = :rand.uniform(3_000)
    Process.sleep(delay)
    if delay > 2_500, do: raise("upstream timeout"), else: :ok
  end
end
```

### Tests

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
    {elapsed_us, _} = :timer.tc(fn ->
      send(Process.whereis(Watchdog), :health_check)
      Process.sleep(1_500)
    end)
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

---

## How it works

1. **`async_stream` for health checks**: bounded concurrency ensures at most N tasks run simultaneously. Total time is `1 * timeout` regardless of worker count, not `N * timeout`.

2. **`start_child` for fire-and-forget**: webhook delivery does not block the circuit breaker worker. No result message, no DOWN notification.

3. **`async_nolink` for fault isolation**: a probe crash does NOT kill the router process. Results come as `{ref, result}` and `{:DOWN, ref, ...}` messages. `Task.yield_many` collects them with a timeout.

4. **Shutdown of orphan tasks**: after `yield_many`, any task that returned `nil` (timed out) is explicitly shut down with `Task.shutdown(task, :brutal_kill)` to prevent leaked connections.

---

## Common production mistakes

**1. Not cancelling orphan tasks**
If you launch tasks with `async_nolink` and never call `yield` or `shutdown`, the task result accumulates in the mailbox forever.

**2. `async_stream` without `max_concurrency`**
Launching 100,000 tasks simultaneously exhausts OS file descriptors.

**3. `on_timeout: :ignore` in `async_stream`**
Timed-out tasks continue running in the background, leaking connections. Always use `:kill_task`.

---

## Resources

- [HexDocs -- Task.Supervisor](https://hexdocs.pm/elixir/Task.Supervisor.html)
- [HexDocs -- Task.yield_many/2](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [Concurrent Data Processing in Elixir -- Sasa Juric](https://pragprog.com/titles/sgdpelixir/)
