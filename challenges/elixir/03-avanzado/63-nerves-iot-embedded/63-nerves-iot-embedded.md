# Nerves — IoT Gateway on Embedded Hardware

**Project**: `api_gateway` — a variant deployed to a Raspberry Pi 4 as an
edge IoT gateway

---

## Project context

The `api_gateway` runs in the cloud, but the infrastructure team needs an edge
variant: a physically deployed Raspberry Pi 4 at each data center rack that reads
environmental sensors (temperature, humidity, power), exposes a local HTTP API, and
forwards metrics to the cloud gateway via MQTT. This exercise builds that device.

Nerves compiles your Elixir application into a complete Linux firmware image that
boots directly into the BEAM VM. The same OTP supervision tree, GenServer patterns,
and ETS stores you use in the cloud gateway work identically on the device.

Project structure:

```
api_gateway/
├── config/
│   ├── config.exs          # shared config
│   └── target.exs          # device-only config (GPIO, WiFi, MQTT)
├── lib/
│   └── api_gateway/
│       ├── application.ex          # target-aware supervision tree
│       ├── sensor/
│       │   ├── bme280.ex           # BME280 sensor over I2C
│       │   └── store.ex            # ETS-backed circular buffer
│       ├── indicator/
│       │   └── led.ex              # status LED controller
│       └── device_api.ex           # local HTTP API
├── test/
│   └── api_gateway/
│       └── sensor_store_test.exs   # given tests — run on host, no hardware needed
└── mix.exs
```

---

## The business problem

Each rack unit needs:

1. **Sensor readings** — BME280 over I2C: temperature, humidity, pressure every 5 seconds
2. **Status LED** — blinks green when healthy, fast-blinks amber on sensor errors
3. **Local HTTP API** — `GET /sensor/current` and `GET /sensor/history` for the local dashboard
4. **ETS-backed circular buffer** — keeps the last 100 readings in RAM for fast queries
5. **OTA updates** — firmware can be updated from the cloud without physical access

---

## Why BEAM on embedded hardware

Most embedded systems run a RTOS or bare metal C. BEAM brings three things those lack:
supervisor trees that restart individual components without rebooting the device,
hot code loading so you can update business logic without touching the OS, and
distributed clustering (multiple Nerves devices can form a cluster with `libcluster`).

The trade-off: BEAM has a higher memory floor (~30 MB for the VM itself) compared to a
C program. For a Raspberry Pi 4 with 4 GB RAM, this is irrelevant. For a microcontroller
with 256 KB, Nerves is the wrong choice.

---

## Why the target-aware `children/1` pattern

Nerves projects must run tests on your development machine (`:host` target) without
GPIO hardware. The `children(target())` pattern keeps the supervisor tree identical in
structure — same OTP design, same restart strategies — but substitutes stub
implementations for hardware-dependent modules when running on the host.

This means: all business logic is testable without a Raspberry Pi. Only the hardware
I/O modules (`Circuits.GPIO`, `Circuits.I2C`) require the actual device.

---

## Implementation

### Step 1: Create the Nerves project

```bash
mix archive.install hex nerves_bootstrap
mix nerves.new api_gateway_edge --target rpi4
cd api_gateway_edge
mkdir -p lib/api_gateway/sensor lib/api_gateway/indicator
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:nerves, "~> 1.10", runtime: false},
    {:nerves_runtime, "~> 0.13"},
    {:circuits_gpio, "~> 2.0"},
    {:circuits_i2c, "~> 2.0"},
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:toolshed, "~> 0.4"},
    {:shoehorn, "~> 0.9"}
  ]
end
```

### Step 3: `lib/api_gateway/sensor/store.ex`

```elixir
defmodule ApiGateway.Sensor.Store do
  @moduledoc """
  ETS-backed circular buffer for sensor readings.

  Uses :ordered_set keyed by monotonic timestamp so history queries
  are O(log n) and eviction always removes the oldest entry.
  """
  use GenServer

  @table    :sensor_readings
  @max_size 100

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Inserts a new reading. Evicts the oldest if over capacity."
  def insert(reading) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(@table, {ts, reading})
    GenServer.cast(__MODULE__, :maybe_evict)
  end

  @doc "Returns the most recent reading, or nil."
  def latest do
    case :ets.last(@table) do
      :"$end_of_table" -> nil
      key -> :ets.lookup(@table, key) |> hd() |> elem(1)
    end
  end

  @doc "Returns the last N readings, newest first."
  def history(n \\ 10) do
    :ets.tab2list(@table)
    |> Enum.sort_by(fn {ts, _} -> ts end, :desc)
    |> Enum.take(n)
    |> Enum.map(&elem(&1, 1))
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:ordered_set, :public, :named_table, read_concurrency: true])
    {:ok, %{}}
  end

  @impl true
  def handle_cast(:maybe_evict, state) do
    if :ets.info(@table, :size) > @max_size do
      oldest = :ets.first(@table)
      :ets.delete(@table, oldest)
    end
    {:noreply, state}
  end
end
```

The store uses `System.monotonic_time(:millisecond)` as the ETS key — monotonic time
is strictly increasing and never goes backwards (unlike wall-clock time which can
jump due to NTP adjustments). The `:ordered_set` table type keeps entries sorted by
key, so `:ets.last/1` always returns the most recent reading in O(log n).

Eviction is handled asynchronously via `GenServer.cast/2`. This means `insert/1`
returns immediately after the ETS write. The cast runs in the GenServer process,
checking the table size and removing the oldest entry if over capacity. The small
window where the table has 101 entries is acceptable — it is bounded and resolved
on the next message.

### Step 4: `lib/api_gateway/sensor/bme280.ex`

```elixir
defmodule ApiGateway.Sensor.BME280 do
  @moduledoc """
  Reads temperature, humidity, and pressure from a BME280 sensor over I2C.
  Polls every @poll_interval_ms. On read errors, applies exponential backoff.

  The GenServer owns the I2C bus reference. It opens at init and closes at terminate,
  following the same lifecycle pattern as the ETS table owner in RateLimiter.Server.
  """
  use GenServer
  require Logger
  alias Circuits.I2C

  @bme280_addr      0x76
  @poll_interval_ms 5_000
  @max_backoff_ms   60_000

  # Public API

  def start_link(opts) do
    bus     = Keyword.get(opts, :bus, "i2c-1")
    address = Keyword.get(opts, :address, @bme280_addr)
    GenServer.start_link(__MODULE__, {bus, address}, name: __MODULE__)
  end

  def current_reading, do: ApiGateway.Sensor.Store.latest()

  # Callbacks

  @impl true
  def init({bus, address}) do
    {:ok, bus_ref} = I2C.open(bus)

    # Initialize BME280: write 0x27 to ctrl_hum (oversampling x1),
    # then 0x27 to ctrl_meas (temp x1, pressure x1, normal mode)
    I2C.write(bus_ref, address, <<0xF2, 0x01>>)
    I2C.write(bus_ref, address, <<0xF4, 0x27>>)

    schedule_poll(@poll_interval_ms)

    {:ok, %{bus_ref: bus_ref, address: address, error_count: 0}}
  end

  @impl true
  def handle_info(:poll, state) do
    case read_sensor(state.bus_ref, state.address) do
      {:ok, reading} ->
        ApiGateway.Sensor.Store.insert(reading)
        schedule_poll(@poll_interval_ms)
        {:noreply, %{state | error_count: 0}}

      {:error, reason} ->
        Logger.warning("BME280 read failed: #{inspect(reason)}")
        backoff = min(@poll_interval_ms * Integer.pow(2, state.error_count), @max_backoff_ms)
        schedule_poll(backoff)
        {:noreply, %{state | error_count: state.error_count + 1}}
    end
  end

  @impl true
  def terminate(_reason, state) do
    I2C.close(state.bus_ref)
  end

  # Private

  defp schedule_poll(ms), do: Process.send_after(self(), :poll, ms)

  defp read_sensor(bus_ref, address) do
    with {:ok, t_raw}  <- I2C.write_read(bus_ref, address, <<0xFA>>, 3),
         {:ok, h_raw}  <- I2C.write_read(bus_ref, address, <<0xFD>>, 2),
         {:ok, p_raw}  <- I2C.write_read(bus_ref, address, <<0xF7>>, 3) do
      reading = %{
        temperature_c: decode_temp(t_raw),
        humidity_pct:  decode_hum(h_raw),
        pressure_hpa:  decode_press(p_raw),
        timestamp:     DateTime.utc_now()
      }
      {:ok, reading}
    end
  end

  defp decode_temp(<<msb, lsb, xlsb>>) do
    ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 5120.0
  end

  defp decode_hum(<<msb, lsb>>) do
    ((msb <<< 8) ||| lsb) / 1024.0
  end

  defp decode_press(<<msb, lsb, xlsb>>) do
    ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 25_600.0
  end
end
```

The `init/1` callback opens the I2C bus, writes initialization registers to the BME280
sensor, and schedules the first poll. On successful reads, the reading is inserted into
the `Store` and the next poll is scheduled at the normal interval. On failures, the
error count increments and the backoff grows exponentially up to `@max_backoff_ms`.
This prevents the GenServer from hammering a failing sensor bus.

`terminate/2` closes the I2C bus reference, releasing the OS file descriptor. Without
this, a crashed-and-restarted process would fail with `{:error, :resource_busy}` when
trying to reopen the same bus.

### Step 5: `lib/api_gateway/indicator/led.ex`

```elixir
defmodule ApiGateway.Indicator.LED do
  @moduledoc """
  Controls the status LED on GPIO pin 18.

  Blink patterns:
    :healthy  — 500ms on/off (1 Hz)
    :warning  — 100ms on/off (5 Hz)
    :off      — solid off
  """
  use GenServer
  require Logger
  alias Circuits.GPIO

  @pin 18

  # Public API

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def set_mode(mode) when mode in [:healthy, :warning, :off] do
    GenServer.cast(__MODULE__, {:set_mode, mode})
  end

  def status, do: GenServer.call(__MODULE__, :status)

  # Callbacks

  @impl true
  def init(_opts) do
    {:ok, gpio} = GPIO.open(@pin, :output)
    GPIO.write(gpio, 0)
    state = %{gpio: gpio, mode: :healthy, level: 0, timer: nil}
    {:ok, schedule_blink(state)}
  end

  @impl true
  def handle_info(:blink, %{mode: :off} = state) do
    GPIO.write(state.gpio, 0)
    {:noreply, %{state | level: 0}}
  end

  def handle_info(:blink, state) do
    new_level = 1 - state.level
    GPIO.write(state.gpio, new_level)
    {:noreply, schedule_blink(%{state | level: new_level})}
  end

  @impl true
  def handle_cast({:set_mode, mode}, state) do
    cancel_timer(state.timer)
    GPIO.write(state.gpio, 0)
    {:noreply, schedule_blink(%{state | mode: mode, level: 0, timer: nil})}
  end

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, %{mode: state.mode, level: state.level, pin: @pin}, state}
  end

  @impl true
  def terminate(_reason, state) do
    cancel_timer(state.timer)
    GPIO.write(state.gpio, 0)
    GPIO.close(state.gpio)
  end

  # Private

  defp interval(:healthy), do: 500
  defp interval(:warning),  do: 100
  defp interval(:off),      do: 60_000

  defp schedule_blink(state) do
    timer = Process.send_after(self(), :blink, interval(state.mode))
    %{state | timer: timer}
  end

  defp cancel_timer(nil), do: :ok
  defp cancel_timer(ref),  do: Process.cancel_timer(ref)
end
```

### Step 6: `lib/api_gateway/device_api.ex`

```elixir
defmodule ApiGateway.DeviceAPI do
  use Plug.Router
  import Plug.Conn

  plug :match
  plug Plug.Parsers, parsers: [:json], json_decoder: Jason
  plug :dispatch

  get "/sensor/current" do
    case ApiGateway.Sensor.Store.latest() do
      nil     -> json(conn, 503, %{error: "no readings yet"})
      reading -> json(conn, 200, reading)
    end
  end

  get "/sensor/history" do
    conn = fetch_query_params(conn)
    n = conn.query_params["n"] |> parse_int(10)
    json(conn, 200, ApiGateway.Sensor.Store.history(n))
  end

  get "/system" do
    info = %{
      firmware_version: Nerves.Runtime.firmware_metadata()["nerves_fw_version"] || "unknown",
      uptime_seconds: Nerves.Runtime.uptime() |> elem(0),
      memory_mb: :erlang.memory(:total) |> div(1_048_576)
    }

    json(conn, 200, info)
  end

  match _ do
    json(conn, 404, %{error: "not found"})
  end

  defp json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(reading_to_json(body)))
  end

  defp reading_to_json(%DateTime{} = dt), do: DateTime.to_iso8601(dt)
  defp reading_to_json(%{timestamp: ts} = r), do: Map.put(r, :timestamp, DateTime.to_iso8601(ts))
  defp reading_to_json(other), do: other

  defp parse_int(nil, default), do: default
  defp parse_int(s, default) do
    case Integer.parse(s) do
      {n, ""} when n > 0 -> min(n, 100)
      _                  -> default
    end
  end
end
```

The `/system` endpoint returns firmware metadata, uptime, and memory usage. These
values come from Nerves runtime functions that read from the Linux procfs and firmware
metadata stored in the firmware image. On the host target these functions may not be
available, which is why this endpoint is only included in the device supervision tree.

### Step 7: `lib/api_gateway/application.ex` — target-aware tree

```elixir
defmodule ApiGateway.Application do
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    children = children(target())
    Supervisor.start_link(children, strategy: :one_for_one, name: ApiGateway.Supervisor)
  end

  # On the host (dev/test): no GPIO, no I2C — business logic only
  def children(:host) do
    [ApiGateway.Sensor.Store]
  end

  def children(_target) do
    Logger.info("Starting edge gateway on #{target()}")
    [
      ApiGateway.Sensor.Store,
      ApiGateway.Indicator.LED,
      {ApiGateway.Sensor.BME280, [bus: "i2c-1", address: 0x76]},
      {Plug.Cowboy, scheme: :http, plug: ApiGateway.DeviceAPI, options: [port: 4000]}
    ]
  end

  def target do
    Application.get_env(:api_gateway, :target, Mix.target())
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/api_gateway/sensor_store_test.exs
defmodule ApiGateway.Sensor.StoreTest do
  use ExUnit.Case, async: false

  setup do
    start_supervised!(ApiGateway.Sensor.Store)
    :ok
  end

  test "latest returns nil with empty store" do
    assert ApiGateway.Sensor.Store.latest() == nil
  end

  test "insert and latest" do
    reading = %{temperature_c: 22.5, humidity_pct: 60.0, pressure_hpa: 1013.2}
    ApiGateway.Sensor.Store.insert(reading)
    assert ApiGateway.Sensor.Store.latest() == reading
  end

  test "history returns newest first" do
    for i <- 1..5 do
      ApiGateway.Sensor.Store.insert(%{temperature_c: i * 1.0})
      Process.sleep(1)
    end

    history = ApiGateway.Sensor.Store.history(3)
    assert length(history) == 3
    assert hd(history).temperature_c == 5.0
  end

  test "eviction keeps store at max 100 entries" do
    for i <- 1..110 do
      ApiGateway.Sensor.Store.insert(%{temperature_c: i * 1.0})
    end

    Process.sleep(20)
    assert length(ApiGateway.Sensor.Store.history(200)) <= 100
  end
end
```

### Step 9: Run tests on host (no hardware needed)

```bash
MIX_TARGET=host mix test test/api_gateway/sensor_store_test.exs --trace
```

### Step 10: Build and flash (with Raspberry Pi 4 connected)

```bash
MIX_TARGET=rpi4 mix deps.get
MIX_TARGET=rpi4 mix firmware
MIX_TARGET=rpi4 mix burn
```

---

## Trade-off analysis

| Aspect | Nerves (BEAM on Linux) | RTOS (FreeRTOS) | Bare metal C |
|--------|------------------------|-----------------|--------------|
| Supervision / restart | Built-in OTP | Manual | Manual |
| OTA updates | NervesHub (atomic, signed) | Vendor toolchain | Manual flash |
| Memory floor | ~30 MB | <1 MB | <1 KB |
| Clustering | Distributed Erlang | No | No |
| Language | Elixir/Erlang | C | C |
| Boot time | ~2 seconds | <100ms | <10ms |
| When to choose | Raspberry Pi, BeagleBone | Low-power MCU | Bare MCU |

Reflection question: the `children(:host)` clause omits `LED` and `BME280`. What
would happen to the supervision strategy if you included them with stub implementations
that always return `{:ok, nil}`? Is there a case where you want stubs in the tree?

---

## Common production mistakes

**1. Not closing GPIO and I2C in `terminate/2`**
If the process crashes and restarts, `GPIO.open/2` on the same pin fails with
`{:error, :resource_busy}`. Always release hardware resources in `terminate/2`.

**2. Not using `System.monotonic_time` as ETS key**
Using `DateTime.utc_now()` as a key causes collisions if two inserts happen within
the same microsecond (NTP step can also cause duplicates). Monotonic time is unique
and never goes backwards.

**3. OTA update applied during a sensor write**
If the device reboots mid-write, the sensor store (ETS, RAM) is lost. This is expected —
the store is a cache, not a source of truth. The cloud gateway holds the durable record.
If durability is needed, use DETS (disk-backed ETS) before applying OTA.

**4. Hardcoding GPIO pin numbers**
Pin numbers change between Raspberry Pi hardware revisions. Store pin assignments in
`config/target.exs` and read them with `Application.get_env/3` in `start_link/1`.

**5. Testing with `async: true` when using a named ETS table**
`sensor_store_test.exs` uses `async: false` because the `:sensor_readings` table is
global. Parallel tests that insert different data to the same table will interfere.

---

## Resources

- [Nerves Project](https://nerves-project.org/) — getting started, hardware targets
- [Circuits.GPIO](https://hexdocs.pm/circuits_gpio) — digital GPIO read/write, interrupts
- [Circuits.I2C](https://hexdocs.pm/circuits_i2c) — I2C bus communication
- [NervesHub](https://www.nerves-hub.org/) — OTA firmware distribution
- [Toolshed](https://hexdocs.pm/toolshed) — IEx utilities for debugging on-device
