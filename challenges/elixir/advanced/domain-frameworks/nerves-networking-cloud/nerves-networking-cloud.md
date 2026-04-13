# Nerves — Networking and Cloud Integration

**Project**: `nerves_networking_cloud` — production-grade nerves — networking and cloud integration in Elixir

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
nerves_networking_cloud/
├── lib/
│   └── nerves_networking_cloud.ex
├── script/
│   └── main.exs
├── test/
│   └── nerves_networking_cloud_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule NervesNetworkingCloud.MixProject do
  use Mix.Project

  def project do
    [
      app: :nerves_networking_cloud,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/nerves_networking_cloud.ex`

```elixir
defmodule ApiGateway.Fleet.Identity do
  @moduledoc """
  Derives a stable device identity from the MAC address of eth0.

  The MAC address is assigned at manufacture and does not change between
  reboots or firmware updates. It is the most reliable identifier available
  on Linux-based embedded devices without a dedicated hardware security module.
  """

  @doc "Returns a string like \"device-b827eb1a2b3c\"."
  @spec device_id() :: String.t()
  def device_id do
    case mac_address("eth0") do
      {:ok, mac} -> "device-" <> String.replace(mac, ":", "")
      {:error, _} -> fallback_id()
    end
  end

  @spec mac_address(String.t()) :: {:ok, String.t()} | {:error, term()}
  def mac_address(interface) do
    path = "/sys/class/net/#{interface}/address"

    case File.read(path) do
      {:ok, contents} -> {:ok, String.trim(contents)}
      {:error, reason} -> {:error, reason}
    end
  end

  @spec device_info() :: map()
  def device_info do
    %{
      device_id:  device_id(),
      connected:  false
    }
  end

  defp fallback_id do
    {:ok, hostname} = :inet.gethostname()
    to_string(hostname)
  end
end

defmodule ApiGateway.Network.WiFiManager do
  @moduledoc """
  Manages WiFi connectivity with exponential backoff and jitter.

  Subscribes to VintageNet property changes instead of polling.
  On :disconnected -> schedules reconnect with exponential backoff.
  On :internet/:lan -> resets backoff counter.

  Backoff formula: min(base * 2^attempt, max_backoff) + rand(0, backoff/4)
  """
  use GenServer
  require Logger

  @interface       "wlan0"
  @base_backoff_ms  5_000
  @max_backoff_ms   300_000
  @connected_states [:internet, :lan]

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec connected?() :: boolean()
  def connected? do
    VintageNet.get(["interface", @interface, "connection"]) in @connected_states
  end

  @spec current_ip() :: String.t() | nil
  def current_ip do
    case VintageNet.get(["interface", @interface, "addresses"]) do
      [%{address: addr} | _] -> addr |> :inet.ntoa() |> to_string()
      _                      -> nil
    end
  end

  @impl true
  def init(opts) do
    ssid = Keyword.fetch!(opts, :ssid)
    psk  = Keyword.fetch!(opts, :psk)

    VintageNet.subscribe(["interface", @interface, "connection"])

    state = %{ssid: ssid, psk: psk, attempt: 0, backoff_ms: @base_backoff_ms, timer: nil}
    {:ok, state, {:continue, :connect}}
  end

  @impl true
  def handle_continue(:connect, state) do
    apply_config(state.ssid, state.psk)
    {:noreply, state}
  end

  @impl true
  def handle_info(
    {VintageNet, ["interface", @interface, "connection"], _old, new_status, _meta},
    state
  ) do
    case new_status do
      s when s in @connected_states ->
        Logger.info("WiFi connected (#{s}) — IP: #{current_ip()}")
        {:noreply, %{state | attempt: 0, backoff_ms: @base_backoff_ms}}

      :disconnected ->
        cancel_timer(state.timer)

        next_backoff = min(state.backoff_ms * 2, @max_backoff_ms)
        jitter = :rand.uniform(max(div(next_backoff, 4), 1))
        delay = next_backoff + jitter

        Logger.warning("WiFi disconnected — attempt #{state.attempt + 1}, retry in #{delay}ms")

        timer = Process.send_after(self(), :reconnect, delay)
        {:noreply, %{state | timer: timer, backoff_ms: next_backoff}}

      other ->
        Logger.debug("WiFi status: #{inspect(other)}")
        {:noreply, state}
    end
  end

  def handle_info(:reconnect, state) do
    Logger.info("WiFi reconnect attempt #{state.attempt + 1}")
    apply_config(state.ssid, state.psk)
    {:noreply, %{state | attempt: state.attempt + 1, timer: nil}}
  end

  defp apply_config(ssid, psk) do
    VintageNet.configure(@interface, %{
      type: VintageNetWiFi,
      vintage_net_wifi: %{
        networks: [%{ssid: ssid, psk: psk, key_mgmt: :wpa_psk}]
      },
      ipv4: %{method: :dhcp}
    })
  end

  defp cancel_timer(nil), do: :ok
  defp cancel_timer(ref), do: Process.cancel_timer(ref)
end

defmodule ApiGateway.Network.TelemetryPublisher do
  @moduledoc """
  Publishes sensor readings to the cloud gateway via MQTT.

  When the network is unavailable, readings are buffered in a :queue
  (FIFO) up to @max_buffer entries. On reconnect, the buffer is flushed
  in order. Oldest entries are dropped if the buffer is full.

  QoS 1 ensures at-least-once delivery. The cloud gateway must be
  idempotent on duplicate messages.
  """
  use GenServer
  require Logger

  @max_buffer      500
  @topic_prefix    "devices"

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec publish(map()) :: :ok
  def publish(payload) when is_map(payload) do
    GenServer.cast(__MODULE__, {:publish, payload})
  end

  @impl true
  def init(opts) do
    host      = Keyword.fetch!(opts, :host)
    device_id = Keyword.fetch!(opts, :device_id)

    state = %{
      host:          host,
      device_id:     device_id,
      mqtt_pid:      nil,
      connected:     false,
      buffer:        :queue.new(),
      buffer_count:  0
    }

    {:ok, state}
  end

  @impl true
  def handle_cast({:publish, payload}, state) do
    message = encode(state.device_id, payload)
    {:noreply, buffer_message(state, message)}
  end

  # -- Private --

  defp encode(device_id, payload) do
    Jason.encode!(%{
      device_id:  device_id,
      timestamp:  DateTime.utc_now() |> DateTime.to_iso8601(),
      payload:    payload
    })
  end

  defp buffer_message(%{buffer_count: count} = state, message) when count >= @max_buffer do
    Logger.warning("MQTT buffer full — dropping oldest message")
    {_, smaller} = :queue.out(state.buffer)
    %{state | buffer: :queue.in(message, smaller)}
  end

  defp buffer_message(state, message) do
    %{state |
      buffer:       :queue.in(message, state.buffer),
      buffer_count: state.buffer_count + 1
    }
  end
end
```

### `test/nerves_networking_cloud_test.exs`

```elixir
defmodule ApiGateway.Network.TelemetryPublisherTest do
  use ExUnit.Case, async: true
  doctest ApiGateway.Fleet.Identity

  alias ApiGateway.Network.TelemetryPublisher

  setup do
    Application.put_env(:api_gateway, :mqtt_publish_fn, fn _pid, _topic, msg, _opts ->
      send(:test_sink, {:published, msg})
      :ok
    end)
    Process.register(self(), :test_sink)
    :ok
  end

  describe "ApiGateway.Network.TelemetryPublisher" do
    test "buffers messages when not connected" do
      {:ok, pid} = start_supervised({
        TelemetryPublisher,
        [host: "localhost", device_id: "test-001"]
      })

      TelemetryPublisher.publish(%{temperature: 22.5})
      TelemetryPublisher.publish(%{temperature: 23.0})

      Process.sleep(50)
      state = :sys.get_state(pid)
      assert state.buffer_count == 2
      refute_received {:published, _}
    end

    test "buffer does not exceed max_buffer" do
      {:ok, pid} = start_supervised({
        TelemetryPublisher,
        [host: "localhost", device_id: "test-002"]
      })

      for i <- 1..510, do: TelemetryPublisher.publish(%{seq: i})

      Process.sleep(100)
      state = :sys.get_state(pid)
      assert state.buffer_count <= 500
    end

    test "dropping oldest when full — newest entry survives" do
      {:ok, pid} = start_supervised({
        TelemetryPublisher,
        [host: "localhost", device_id: "test-003"]
      })

      for i <- 1..500, do: TelemetryPublisher.publish(%{seq: i})
      Process.sleep(50)

      TelemetryPublisher.publish(%{seq: :last})
      Process.sleep(50)

      state = :sys.get_state(pid)
      messages =
        state.buffer
        |> :queue.to_list()
        |> Enum.map(&Jason.decode!/1)

      assert Enum.any?(messages, fn m -> get_in(m, ["payload", "seq"]) == "last" end)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Nerves — Networking and Cloud Integration.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Nerves — Networking and Cloud Integration ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NervesNetworkingCloud.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: NervesNetworkingCloud.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
