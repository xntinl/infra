# Nerves — Networking and Cloud Integration

**Project**: `api_gateway` — edge variant deployed on Raspberry Pi 4

---

## Project context

You're building the edge variant of `api_gateway` (continued from the previous exercise).
The device reads sensor data but cannot yet send it to the cloud. This exercise adds the
network stack: WiFi management with reconnection backoff, MQTT telemetry publishing with
offline buffering, and a fleet identity system so the cloud can distinguish between devices.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── network/
│       │   ├── wifi_manager.ex         # WiFi with exponential backoff
│       │   └── telemetry_publisher.ex  # MQTT with offline buffer
│       ├── fleet/
│       │   └── identity.ex             # MAC-based device identity
│       └── application.ex              # update supervision tree
├── test/
│   └── api_gateway/
│       └── telemetry_publisher_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The device collects sensor data but all of it stays local. The cloud gateway needs it
for fleet-wide analytics. Three problems to solve:

1. **WiFi is unreliable** — the device must reconnect automatically after drops, with
   exponential backoff to avoid hammering the AP when hundreds of devices restart simultaneously.
2. **Network goes away** — sensor readings must not be lost during connectivity gaps.
   Buffer up to 500 readings in RAM, flush in order when the connection returns.
3. **Fleet identity** — with 200 devices, the cloud must know which physical unit is
   sending data. Use the MAC address of `eth0` as a stable, hardware-bound identifier.

---

## Why exponential backoff with jitter for reconnection

Without jitter, all devices in a fleet that lose connectivity simultaneously will
reconnect at exactly the same time. If there are 200 devices with a 5-second backoff,
200 TLS handshakes hit the MQTT broker at t=5, then again at t=10. This is the
"thundering herd" problem and it can overload the broker faster than the original outage.

Adding +/-25% random jitter spreads reconnection attempts across the backoff window.
200 devices reconnect over a 5-second window instead of all at second 0.

---

## Why `VintageNet` instead of manual `iwconfig`

Nerves devices have no shell to run `iwconfig`. Network configuration is done through
`VintageNet`, which exposes a `PropertyTable` API. You subscribe to connectivity changes
instead of polling. The pattern is: observe state transitions, react to them. This is the
same event-driven philosophy as OTP message passing, applied to network state.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
{:vintage_net, "~> 0.13"},
{:vintage_net_wifi, "~> 0.12"},
{:tortoise311, "~> 0.12"},   # MQTT client
{:jason, "~> 1.4"}
```

### Step 2: `lib/api_gateway/fleet/identity.ex`

```elixir
defmodule ApiGateway.Fleet.Identity do
  @moduledoc """
  Derives a stable device identity from the MAC address of eth0.

  The MAC address is assigned at manufacture and does not change between
  reboots or firmware updates. It is the most reliable identifier available
  on Linux-based embedded devices without a dedicated hardware security module.
  """

  @doc "Returns a string like \"device-b827eb1a2b3c\"."
  def device_id do
    case mac_address("eth0") do
      {:ok, mac} -> "device-" <> String.replace(mac, ":", "")
      {:error, _} -> fallback_id()
    end
  end

  def mac_address(interface) do
    path = "/sys/class/net/#{interface}/address"

    case File.read(path) do
      {:ok, contents} -> {:ok, String.trim(contents)}
      {:error, reason} -> {:error, reason}
    end
  end

  def device_info do
    %{
      device_id:        device_id(),
      firmware_version: firmware_version(),
      uptime_seconds:   Nerves.Runtime.uptime() |> elem(0),
      connected:        wifi_connected?()
    }
  end

  defp firmware_version do
    Nerves.Runtime.firmware_metadata()
    |> Map.get("nerves_fw_version", "unknown")
  end

  defp wifi_connected? do
    VintageNet.get(["interface", "wlan0", "connection"]) in [:internet, :lan]
  end

  defp fallback_id do
    {:ok, hostname} = :inet.gethostname()
    to_string(hostname)
  end
end
```

The `mac_address/1` function reads the MAC address from the Linux sysfs filesystem.
Every network interface exposes its hardware address at `/sys/class/net/<iface>/address`.
`File.read/1` returns `{:ok, contents}` on success or `{:error, reason}` on failure
(e.g., the interface does not exist). `String.trim/1` removes the trailing newline
that Linux appends to sysfs files.

When the MAC address is unavailable (e.g., on the host target during development),
the fallback uses the system hostname.

### Step 3: `lib/api_gateway/network/wifi_manager.ex`

```elixir
defmodule ApiGateway.Network.WiFiManager do
  @moduledoc """
  Manages WiFi connectivity with exponential backoff and jitter.

  Subscribes to VintageNet property changes instead of polling.
  On :disconnected -> schedules reconnect with exponential backoff.
  On :internet/:lan -> resets backoff counter.

  Backoff formula: min(base * 2^attempt, max_backoff) + rand(0, backoff/4)
  The jitter prevents thundering herd when the entire fleet loses connectivity.
  """
  use GenServer
  require Logger

  @interface       "wlan0"
  @base_backoff_ms  5_000
  @max_backoff_ms   300_000   # 5 minutes
  @connected_states [:internet, :lan]

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def connected? do
    VintageNet.get(["interface", @interface, "connection"]) in @connected_states
  end

  def current_ip do
    case VintageNet.get(["interface", @interface, "addresses"]) do
      [%{address: addr} | _] -> addr |> :inet.ntoa() |> to_string()
      _                      -> nil
    end
  end

  # Callbacks

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

  # Private

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

The `:disconnected` handler implements the core backoff logic:

1. Cancel any existing reconnect timer to avoid duplicate reconnection attempts.
2. Compute `next_backoff = min(current * 2, max)` — doubles each time, capped at 5 minutes.
3. Add jitter: a random value between 0 and 25% of the backoff. `max(div(...), 1)` prevents
   `:rand.uniform(0)` which would raise.
4. Schedule the `:reconnect` message after the computed delay.
5. Store the timer reference so it can be cancelled if another status change arrives
   before the timer fires.

The `:internet` / `:lan` handler resets the attempt counter and backoff to their
initial values — the device is connected, so the next disconnection starts fresh.

### Step 4: `lib/api_gateway/network/telemetry_publisher.ex`

```elixir
defmodule ApiGateway.Network.TelemetryPublisher do
  @moduledoc """
  Publishes sensor readings to the cloud gateway via MQTT.

  When the network is unavailable, readings are buffered in a :queue
  (FIFO) up to @max_buffer entries. On reconnect, the buffer is flushed
  in order. Oldest entries are dropped if the buffer is full.

  QoS 1 ensures at-least-once delivery. The cloud gateway must be
  idempotent on duplicate messages (e.g., upsert by timestamp).
  """
  use GenServer
  require Logger

  @max_buffer      500
  @flush_interval  1_000
  @topic_prefix    "devices"

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def publish(payload) when is_map(payload) do
    GenServer.cast(__MODULE__, {:publish, payload})
  end

  # Callbacks

  @impl true
  def init(opts) do
    host      = Keyword.fetch!(opts, :host)
    device_id = Keyword.fetch!(opts, :device_id)

    VintageNet.subscribe(["interface", "wlan0", "connection"])

    state = %{
      host:          host,
      device_id:     device_id,
      mqtt_pid:      nil,
      connected:     false,
      buffer:        :queue.new(),
      buffer_count:  0
    }

    {:ok, state, {:continue, :connect_mqtt}}
  end

  @impl true
  def handle_continue(:connect_mqtt, state) do
    case connect_mqtt(state) do
      {:ok, pid} ->
        Logger.info("MQTT connected to #{state.host}")
        {:noreply, %{state | mqtt_pid: pid, connected: true}}

      {:error, reason} ->
        Logger.warning("MQTT connect failed: #{inspect(reason)} — buffering")
        schedule_retry()
        {:noreply, state}
    end
  end

  @impl true
  def handle_cast({:publish, payload}, state) do
    message = encode(state.device_id, payload)

    if state.connected do
      case mqtt_publish(state.mqtt_pid, topic(state.device_id), message) do
        :ok            -> {:noreply, state}
        {:error, _}    -> {:noreply, buffer_message(state, message)}
      end
    else
      {:noreply, buffer_message(state, message)}
    end
  end

  @impl true
  def handle_info(
    {VintageNet, ["interface", "wlan0", "connection"], _old, status, _meta},
    state
  ) do
    case status do
      s when s in [:internet, :lan] ->
        case connect_mqtt(state) do
          {:ok, pid} ->
            new_state = %{state | mqtt_pid: pid, connected: true}
            {:noreply, flush_buffer(new_state)}
          {:error, _} ->
            {:noreply, state}
        end

      :disconnected ->
        {:noreply, %{state | connected: false, mqtt_pid: nil}}

      _ ->
        {:noreply, state}
    end
  end

  def handle_info(:retry_connect, state) do
    handle_continue(:connect_mqtt, state)
  end

  # Private

  defp connect_mqtt(state) do
    Tortoise311.Supervisor.start_child(
      client_id: state.device_id,
      handler:   {ApiGateway.Network.MQTTHandler, []},
      server:    {Tortoise311.Transport.TCP, host: state.host, port: 1883}
    )
  end

  defp mqtt_publish(pid, topic, message) do
    Tortoise311.publish(pid, topic, message, qos: 1)
  end

  defp topic(device_id), do: "#{@topic_prefix}/#{device_id}/telemetry"

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

  defp flush_buffer(%{buffer_count: 0} = state), do: state

  defp flush_buffer(state) do
    Logger.info("Flushing #{state.buffer_count} buffered MQTT messages")
    do_flush(state)
  end

  defp do_flush(%{buffer_count: 0} = state), do: state

  defp do_flush(state) do
    case :queue.out(state.buffer) do
      {:empty, _} ->
        %{state | buffer_count: 0}

      {{:value, msg}, rest} ->
        case mqtt_publish(state.mqtt_pid, topic(state.device_id), msg) do
          :ok ->
            do_flush(%{state | buffer: rest, buffer_count: state.buffer_count - 1})

          {:error, _} ->
            %{state | buffer: :queue.in_r(msg, rest)}
        end
    end
  end

  defp schedule_retry do
    Process.send_after(self(), :retry_connect, 5_000)
  end
end
```

The buffer uses Erlang's `:queue` module which provides O(1) amortized enqueue and
dequeue operations. When the buffer is full (`buffer_count >= @max_buffer`), the
oldest message is dequeued and discarded before the new one is enqueued — maintaining
FIFO order and bounded memory usage.

The `flush_buffer/1` function drains the queue in order, publishing each message via
MQTT. If a publish fails mid-flush, the undelivered message is pushed back to the
front of the queue with `:queue.in_r/2` (insert at the rear of the "reverse" list,
which is the front of the logical queue), preserving order for the next flush attempt.

### Step 5: Given tests — must pass without modification

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
```

### Step 6: Run tests on host

```bash
MIX_TARGET=host mix test test/api_gateway/telemetry_publisher_test.exs --trace
```

---

## Trade-off analysis

| Aspect | VintageNet + Tortoise311 | Raw `wpa_supplicant` + `paho-mqtt` | Cloud-only (no edge) |
|--------|--------------------------|-------------------------------------|----------------------|
| Reconnect logic | GenServer with backoff | Shell scripts | N/A |
| Offline buffering | `:queue` in GenServer state | External queue | Data loss |
| Fleet identity | MAC from sysfs | Custom provisioning | Cloud-assigned |
| Observable state | PropertyTable subscriptions | Polling | N/A |
| OTA of network config | `VintageNet.configure/2` | Reboot required | N/A |

Reflection question: the buffer uses `:queue` (Erlang's functional queue) in GenServer
state. What is the memory implication of holding 500 MQTT messages in RAM? At what
message size does this become a concern on a device with 512 MB RAM?

---

## Common production mistakes

**1. Jitter applied after capping at max_backoff**
Compute `next = min(current * 2, max)`, then add jitter to `next`. If you add jitter
before the cap, devices near the maximum backoff may jitter above it — or below it,
reducing the cap's effectiveness.

**2. Not flushing buffer in FIFO order**
Using `:lists.reverse` on the queue or collecting into a list before publishing
inverts the order. Sensor readings delivered out of order confuse time-series databases.
`:queue.out/1` is already FIFO — use it directly.

**3. QoS 0 for sensor data**
MQTT QoS 0 is fire-and-forget. If the broker drops the message (network hiccup, broker
overload), no retry happens. For sensor telemetry you want QoS 1 (at-least-once). The
cloud gateway handles idempotency via upsert-by-timestamp.

**4. Using the same MQTT client_id across multiple devices**
If two devices register with the same `client_id`, the broker disconnects the first when
the second connects. Each device must have a globally unique `client_id` — the MAC-based
`device_id` guarantees this.

**5. Not closing the MQTT connection on `terminate/2`**
Tortoise311 connections survive process death (they are supervised separately). If the
`TelemetryPublisher` crashes and restarts without stopping the old connection, you will
have two MQTT sessions for the same `client_id`. The broker will disconnect the first
when the second connects.

---

## Resources

- [VintageNet](https://hexdocs.pm/vintage_net) — network interface configuration and observation
- [Tortoise311](https://hexdocs.pm/tortoise311) — MQTT client: QoS, subscriptions, handlers
- [VintageNetWiFi](https://hexdocs.pm/vintage_net_wifi) — WiFi-specific configuration
- [MQTT spec — QoS levels](https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html#_Toc3901103) — why QoS 1 vs 0 matters for IoT
