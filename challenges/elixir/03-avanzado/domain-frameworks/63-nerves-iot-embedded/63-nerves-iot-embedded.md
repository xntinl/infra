# Nerves — IoT Gateway on Embedded Hardware

## Project context

You are building `api_gateway`. This exercise creates an edge variant: a physically deployed Raspberry Pi 4 at each data center rack that reads environmental sensors (temperature, humidity, power), exposes a local HTTP API, and stores readings in an ETS-backed circular buffer. All modules are defined from scratch.

Nerves compiles your Elixir application into a complete Linux firmware image that boots directly into the BEAM VM. The same OTP supervision tree, GenServer patterns, and ETS stores you use in the cloud gateway work identically on the device.

Project structure:

```
api_gateway/
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

Most embedded systems run a RTOS or bare metal C. BEAM brings three things those lack: supervisor trees that restart individual components without rebooting the device, hot code loading so you can update business logic without touching the OS, and distributed clustering (multiple Nerves devices can form a cluster with `libcluster`).

The trade-off: BEAM has a higher memory floor (~30 MB for the VM itself). For a Raspberry Pi 4 with 4 GB RAM, this is irrelevant. For a microcontroller with 256 KB, Nerves is the wrong choice.

---

## Why the target-aware `children/1` pattern

Nerves projects must run tests on your development machine (`:host` target) without GPIO hardware. The `children(target())` pattern keeps the supervisor tree identical in structure but substitutes stub implementations for hardware-dependent modules when running on the host. All business logic is testable without a Raspberry Pi.

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

### Step 1: `mix.exs`

**Objective**: Pin `nerves`, `circuits_gpio`, and `circuits_i2c` — these bind the BEAM to hardware; `shoehorn` adds boot-time fault tolerance on-device.

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

### Step 2: `lib/api_gateway/sensor/store.ex`

**Objective**: Back the ring buffer with an `:ordered_set` keyed by monotonic time — history queries stay O(log n) and eviction always drops the oldest entry.

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

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Inserts a new reading. Evicts the oldest if over capacity."
  @spec insert(map()) :: :ok
  def insert(reading) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(@table, {ts, reading})
    GenServer.cast(__MODULE__, :maybe_evict)
  end

  @doc "Returns the most recent reading, or nil."
  @spec latest() :: map() | nil
  def latest do
    case :ets.last(@table) do
      :"$end_of_table" -> nil
      key -> :ets.lookup(@table, key) |> hd() |> elem(1)
    end
  end

  @doc "Returns the last N readings, newest first."
  @spec history(pos_integer()) :: [map()]
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

The store uses `System.monotonic_time(:millisecond)` as the ETS key — monotonic time is strictly increasing and never goes backwards (unlike wall-clock time which can jump due to NTP adjustments). The `:ordered_set` table type keeps entries sorted by key, so `:ets.last/1` always returns the most recent reading in O(log n).

Eviction is handled asynchronously via `GenServer.cast/2`. This means `insert/1` returns immediately after the ETS write. The small window where the table has 101 entries is acceptable — it is bounded and resolved on the next message.

### Step 3: `lib/api_gateway/sensor/bme280.ex`

**Objective**: Own the I2C bus ref inside a GenServer and back off on read errors — the process is the unit of failure for a flaky sensor.

```elixir
defmodule ApiGateway.Sensor.BME280 do
  @moduledoc """
  Reads temperature, humidity, and pressure from a BME280 sensor over I2C.
  Polls every @poll_interval_ms. On read errors, applies exponential backoff.

  The GenServer owns the I2C bus reference. It opens at init and closes at terminate.
  """
  use GenServer
  require Logger
  alias Circuits.I2C

  @bme280_addr      0x76
  @poll_interval_ms 5_000
  @max_backoff_ms   60_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    bus     = Keyword.get(opts, :bus, "i2c-1")
    address = Keyword.get(opts, :address, @bme280_addr)
    GenServer.start_link(__MODULE__, {bus, address}, name: __MODULE__)
  end

  @spec current_reading() :: map() | nil
  def current_reading, do: ApiGateway.Sensor.Store.latest()

  @impl true
  def init({bus, address}) do
    {:ok, bus_ref} = I2C.open(bus)

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

  defp decode_temp(<<msb, lsb, xlsb>>),
    do: ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 5120.0

  defp decode_hum(<<msb, lsb>>),
    do: ((msb <<< 8) ||| lsb) / 1024.0

  defp decode_press(<<msb, lsb, xlsb>>),
    do: ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 25_600.0
end
```

### Step 4: `lib/api_gateway/indicator/led.ex`

**Objective**: Encode device state as a blink pattern on GPIO 18 — the LED is the only observable surface when there is no console attached.

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
  alias Circuits.GPIO

  @pin 18

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec set_mode(:healthy | :warning | :off) :: :ok
  def set_mode(mode) when mode in [:healthy, :warning, :off] do
    GenServer.cast(__MODULE__, {:set_mode, mode})
  end

  @spec status() :: map()
  def status, do: GenServer.call(__MODULE__, :status)

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

  defp interval(:healthy), do: 500
  defp interval(:warning), do: 100
  defp interval(:off),     do: 60_000

  defp schedule_blink(state) do
    timer = Process.send_after(self(), :blink, interval(state.mode))
    %{state | timer: timer}
  end

  defp cancel_timer(nil), do: :ok
  defp cancel_timer(ref), do: Process.cancel_timer(ref)
end
```

### Step 5: `lib/api_gateway/device_api.ex`

**Objective**: Expose readings and system info over a local Plug router — the edge device is queryable on the LAN without cloud dependencies.

```elixir
defmodule ApiGateway.DeviceAPI do
  @moduledoc """
  Local HTTP API for the edge gateway device.
  Exposes sensor readings and device system info.
  """
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

  match _ do
    json(conn, 404, %{error: "not found"})
  end

  defp json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(normalize_for_json(body)))
  end

  defp normalize_for_json(%DateTime{} = dt), do: DateTime.to_iso8601(dt)
  defp normalize_for_json(%{timestamp: %DateTime{} = ts} = r),
    do: Map.put(r, :timestamp, DateTime.to_iso8601(ts))
  defp normalize_for_json(list) when is_list(list), do: Enum.map(list, &normalize_for_json/1)
  defp normalize_for_json(other), do: other

  defp parse_int(nil, default), do: default
  defp parse_int(s, default) do
    case Integer.parse(s) do
      {n, ""} when n > 0 -> min(n, 100)
      _                  -> default
    end
  end
end
```

### Step 6: `lib/api_gateway/application.ex` — target-aware tree

**Objective**: Branch children on `MIX_TARGET` — hardware-owning processes only start on-device; host keeps the test loop fast and crash-free.

```elixir
defmodule ApiGateway.Application do
  @moduledoc """
  Target-aware application that starts hardware-dependent children
  only when running on a Nerves device, not during host-based tests.
  """
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    children = children(target())
    Supervisor.start_link(children, strategy: :one_for_one, name: ApiGateway.Supervisor)
  end

  # On the host (dev/test): no GPIO, no I2C — business logic only
  defp children(:host) do
    [ApiGateway.Sensor.Store]
  end

  defp children(_target) do
    Logger.info("Starting edge gateway on #{target()}")
    [
      ApiGateway.Sensor.Store,
      ApiGateway.Indicator.LED,
      {ApiGateway.Sensor.BME280, [bus: "i2c-1", address: 0x76]},
      {Plug.Cowboy, scheme: :http, plug: ApiGateway.DeviceAPI, options: [port: 4000]}
    ]
  end

  defp target do
    Application.get_env(:api_gateway, :target, Mix.target())
  end
end
```

### Step 7: Given tests — must pass without modification

**Objective**: Implement: Given tests — must pass without modification.

```elixir
# test/api_gateway/sensor_store_test.exs
defmodule ApiGateway.Sensor.StoreTest do
  use ExUnit.Case, async: false

  setup do
    start_supervised!(ApiGateway.Sensor.Store)
    :ok
  end

  describe "ApiGateway.Sensor.Store" do
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
end
```

### Step 8: Run tests on host (no hardware needed)

**Objective**: Run the code to validate the full workflow end-to-end.

```bash
MIX_TARGET=host mix test test/api_gateway/sensor_store_test.exs --trace
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

| Aspect | Nerves (BEAM on Linux) | RTOS (FreeRTOS) | Bare metal C |
|--------|------------------------|-----------------|--------------|
| Supervision / restart | Built-in OTP | Manual | Manual |
| OTA updates | NervesHub (atomic, signed) | Vendor toolchain | Manual flash |
| Memory floor | ~30 MB | <1 MB | <1 KB |
| Clustering | Distributed Erlang | No | No |
| Boot time | ~2 seconds | <100ms | <10ms |
| When to choose | Raspberry Pi, BeagleBone | Low-power MCU | Bare MCU |

Reflection question: the `children(:host)` clause omits `LED` and `BME280`. What would happen to the supervision strategy if you included them with stub implementations that always return `{:ok, nil}`?

---

## Common production mistakes

**1. Not closing GPIO and I2C in `terminate/2`**
If the process crashes and restarts, `GPIO.open/2` on the same pin fails with `{:error, :resource_busy}`. Always release hardware resources in `terminate/2`.

**2. Not using `System.monotonic_time` as ETS key**
Using `DateTime.utc_now()` as a key causes collisions if two inserts happen within the same microsecond. Monotonic time is unique and never goes backwards.

**3. Hardcoding GPIO pin numbers**
Pin numbers change between hardware revisions. Store pin assignments in `config/target.exs` and read them with `Application.get_env/3`.

**4. Testing with `async: true` when using a named ETS table**
The `:sensor_readings` table is global. Parallel tests that insert different data to the same table will interfere.

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

## Executable Example

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

defmodule ApiGateway.Sensor.Store do
  @moduledoc """
  ETS-backed circular buffer for sensor readings.

  Uses :ordered_set keyed by monotonic timestamp so history queries
  are O(log n) and eviction always removes the oldest entry.
  """
  use GenServer

  @table    :sensor_readings
  @max_size 100

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Inserts a new reading. Evicts the oldest if over capacity."
  @spec insert(map()) :: :ok
  def insert(reading) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(@table, {ts, reading})
    GenServer.cast(__MODULE__, :maybe_evict)
  end

  @doc "Returns the most recent reading, or nil."
  @spec latest() :: map() | nil
  def latest do
    case :ets.last(@table) do
      :"$end_of_table" -> nil
      key -> :ets.lookup(@table, key) |> hd() |> elem(1)
    end
  end

  @doc "Returns the last N readings, newest first."
  @spec history(pos_integer()) :: [map()]
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

defmodule ApiGateway.Sensor.BME280 do
  end
  @moduledoc """
  Reads temperature, humidity, and pressure from a BME280 sensor over I2C.
  Polls every @poll_interval_ms. On read errors, applies exponential backoff.

  The GenServer owns the I2C bus reference. It opens at init and closes at terminate.
  """
  use GenServer
  require Logger
  alias Circuits.I2C

  @bme280_addr      0x76
  @poll_interval_ms 5_000
  @max_backoff_ms   60_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    bus     = Keyword.get(opts, :bus, "i2c-1")
    address = Keyword.get(opts, :address, @bme280_addr)
    GenServer.start_link(__MODULE__, {bus, address}, name: __MODULE__)
  end

  @spec current_reading() :: map() | nil
  def current_reading, do: ApiGateway.Sensor.Store.latest()

  @impl true
  def init({bus, address}) do
    {:ok, bus_ref} = I2C.open(bus)

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

  defp decode_temp(<<msb, lsb, xlsb>>),
    do: ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 5120.0

  defp decode_hum(<<msb, lsb>>),
    do: ((msb <<< 8) ||| lsb) / 1024.0

  defp decode_press(<<msb, lsb, xlsb>>),
    do: ((msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)) / 25_600.0
end

defmodule ApiGateway.Indicator.LED do
  end
  @moduledoc """
  Controls the status LED on GPIO pin 18.

  Blink patterns:
    :healthy  — 500ms on/off (1 Hz)
    :warning  — 100ms on/off (5 Hz)
    :off      — solid off
  """
  use GenServer
  alias Circuits.GPIO

  @pin 18

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec set_mode(:healthy | :warning | :off) :: :ok
  def set_mode(mode) when mode in [:healthy, :warning, :off] do
    GenServer.cast(__MODULE__, {:set_mode, mode})
  end

  @spec status() :: map()
  def status, do: GenServer.call(__MODULE__, :status)

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

  defp interval(:healthy), do: 500
  defp interval(:warning), do: 100
  defp interval(:off),     do: 60_000

  defp schedule_blink(state) do
    timer = Process.send_after(self(), :blink, interval(state.mode))
    %{state | timer: timer}
  end

  defp cancel_timer(nil), do: :ok
  defp cancel_timer(ref), do: Process.cancel_timer(ref)
end

defmodule ApiGateway.DeviceAPI do
  end
  @moduledoc """
  Local HTTP API for the edge gateway device.
  Exposes sensor readings and device system info.
  """
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

  match _ do
    json(conn, 404, %{error: "not found"})
  end

  defp json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(normalize_for_json(body)))
  end

  defp normalize_for_json(%DateTime{} = dt), do: DateTime.to_iso8601(dt)
  defp normalize_for_json(%{timestamp: %DateTime{} = ts} = r),
    do: Map.put(r, :timestamp, DateTime.to_iso8601(ts))
  defp normalize_for_json(list) when is_list(list), do: Enum.map(list, &normalize_for_json/1)
  defp normalize_for_json(other), do: other

  defp parse_int(nil, default), do: default
  defp parse_int(s, default) do
    case Integer.parse(s) do
      {n, ""} when n > 0 -> min(n, 100)
      _                  -> default
    end
  end
end

defmodule ApiGateway.Application do
  @moduledoc """
  Target-aware application that starts hardware-dependent children
  only when running on a Nerves device, not during host-based tests.
  """
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    children = children(target())
    Supervisor.start_link(children, strategy: :one_for_one, name: ApiGateway.Supervisor)
  end

  # On the host (dev/test): no GPIO, no I2C — business logic only
  defp children(:host) do
    [ApiGateway.Sensor.Store]
  end

  defp children(_target) do
    Logger.info("Starting edge gateway on #{target()}")
    [
      ApiGateway.Sensor.Store,
      ApiGateway.Indicator.LED,
      {ApiGateway.Sensor.BME280, [bus: "i2c-1", address: 0x76]},
      {Plug.Cowboy, scheme: :http, plug: ApiGateway.DeviceAPI, options: [port: 4000]}
    ]
  end

  defp target do
    Application.get_env(:api_gateway, :target, Mix.target())
  end
end

# test/api_gateway/sensor_store_test.exs
defmodule ApiGateway.Sensor.StoreTest do
  use ExUnit.Case, async: false

  setup do
    start_supervised!(ApiGateway.Sensor.Store)
    :ok
  end

  describe "ApiGateway.Sensor.Store" do
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
end

defmodule Main do
  def main do
      # Demonstrating 63-nerves-iot-embedded
      :ok
  end
end

Main.main()
end
end
end
end
end
end
end
end
end
end
end
end
end
end
```
