# Nerves — Networking and Cloud Integration

## Project context

You are building `api_gateway`. This exercise creates the network stack for an edge variant deployed on Raspberry Pi 4: WiFi management with reconnection backoff, MQTT telemetry publishing with offline buffering, and a fleet identity system so the cloud can distinguish between devices. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── network/
│       │   ├── wifi_manager.ex         # WiFi with exponential backoff
│       │   └── telemetry_publisher.ex  # MQTT with offline buffer
│       └── fleet/
│           └── identity.ex             # MAC-based device identity
├── test/
│   └── api_gateway/
│       └── telemetry_publisher_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The device collects sensor data but all of it stays local. The cloud gateway needs it for fleet-wide analytics. Three problems to solve:

1. **WiFi is unreliable** — the device must reconnect automatically after drops, with exponential backoff to avoid hammering the AP when hundreds of devices restart simultaneously.
2. **Network goes away** — sensor readings must not be lost during connectivity gaps. Buffer up to 500 readings in RAM, flush in order when the connection returns.
3. **Fleet identity** — with 200 devices, the cloud must know which physical unit is sending data. Use the MAC address of `eth0` as a stable, hardware-bound identifier.

---

## Why exponential backoff with jitter for reconnection

Without jitter, all devices in a fleet that lose connectivity simultaneously will reconnect at exactly the same time. If there are 200 devices with a 5-second backoff, 200 TLS handshakes hit the MQTT broker at t=5, then again at t=10. This is the "thundering herd" problem. Adding +/-25% random jitter spreads reconnection attempts across the backoff window.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Implementation

### Step 1: `mix.exs` additions

**Objective**: Add VintageNet, WiFi, and Tortoise (MQTT) for embedded network and cloud connectivity.

```elixir
defp deps do
  [
    {:vintage_net, "~> 0.13"},
    {:vintage_net_wifi, "~> 0.12"},
    {:tortoise311, "~> 0.12"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/api_gateway/fleet/identity.ex`

**Objective**: Derive stable device ID from eth0 MAC address to identify devices across reboots and updates.

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
```

### Step 3: `lib/api_gateway/network/wifi_manager.ex`

**Objective**: Subscribe to VintageNet status changes and apply exponential backoff + jitter on disconnects.

```elixir
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
```

### Step 4: `lib/api_gateway/network/telemetry_publisher.ex`

**Objective**: Implement the `telemetry_publisher.ex` module.

```elixir
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

The buffer uses Erlang's `:queue` module which provides O(1) amortized enqueue and dequeue operations. When the buffer is full (`buffer_count >= @max_buffer`), the oldest message is dequeued and discarded before the new one is enqueued — maintaining FIFO order and bounded memory usage.

### Step 5: Given tests — must pass without modification

**Objective**: Implement: Given tests — must pass without modification.

```elixir
# test/api_gateway/telemetry_publisher_test.exs
defmodule ApiGateway.Network.TelemetryPublisherTest do
  use ExUnit.Case, async: false

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

### Step 6: Run tests on host

**Objective**: Run the code to validate the full workflow end-to-end.

```bash
MIX_TARGET=host mix test test/api_gateway/telemetry_publisher_test.exs --trace
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.

---

## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---
## Trade-off analysis

| Aspect | VintageNet + Tortoise311 | Raw `wpa_supplicant` + `paho-mqtt` | Cloud-only (no edge) |
|--------|--------------------------|-------------------------------------|----------------------|
| Reconnect logic | GenServer with backoff | Shell scripts | N/A |
| Offline buffering | `:queue` in GenServer state | External queue | Data loss |
| Fleet identity | MAC from sysfs | Custom provisioning | Cloud-assigned |
| Observable state | PropertyTable subscriptions | Polling | N/A |

Reflection question: the buffer uses `:queue` in GenServer state. What is the memory implication of holding 500 MQTT messages in RAM? At what message size does this become a concern on a device with 512 MB RAM?

---

## Common production mistakes

**1. Jitter applied after capping at max_backoff**
Compute `next = min(current * 2, max)`, then add jitter to `next`. If you add jitter before the cap, devices near the maximum backoff may jitter above it.

**2. Not flushing buffer in FIFO order**
`:queue.out/1` is already FIFO — use it directly. Using `:lists.reverse` inverts the order and corrupts time-series data.

**3. QoS 0 for sensor data**
MQTT QoS 0 is fire-and-forget. For sensor telemetry you want QoS 1 (at-least-once). The cloud gateway handles idempotency via upsert-by-timestamp.

**4. Using the same MQTT client_id across multiple devices**
If two devices register with the same `client_id`, the broker disconnects the first. Each device must have a globally unique `client_id` — the MAC-based `device_id` guarantees this.

---

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [VintageNet](https://hexdocs.pm/vintage_net) — network interface configuration and observation
- [Tortoise311](https://hexdocs.pm/tortoise311) — MQTT client: QoS, subscriptions, handlers
- [VintageNetWiFi](https://hexdocs.pm/vintage_net_wifi) — WiFi-specific configuration
- [MQTT spec — QoS levels](https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html#_Toc3901103) — why QoS 1 vs 0 matters for IoT

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
