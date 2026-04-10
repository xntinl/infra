# 63 — Nerves: IoT y Sistemas Embebidos

**Difficulty**: Avanzado  
**Tiempo estimado**: 6-8 horas  
**Área**: Nerves · Embedded · GPIO · Circuits · OTA

---

## Contexto

Nerves es el framework oficial de Elixir para sistemas embebidos. Permite compilar una imagen
Linux mínima que arranca directamente en una BEAM VM, eliminando capas innecesarias. El resultado
es un sistema con arranque en ~2 segundos, actualizaciones atómicas OTA, y toda la potencia de
OTP (supervisión, GenServer, PubSub) corriendo en hardware real.

Este ejercicio cubre el stack completo: GPIO con `Circuits.GPIO`, lectura de sensores I2C,
exposición de datos via HTTP y actualizaciones OTA con NervesHub.

---

## Setup del proyecto

```bash
# Instalar Nerves bootstrap
mix archive.install hex nerves_bootstrap

# Crear proyecto para Raspberry Pi 4
mix nerves.new my_device --target rpi4
cd my_device

# Dependencias en mix.exs
# {:circuits_gpio, "~> 2.0"},
# {:circuits_i2c, "~> 2.0"},
# {:nerves_runtime, "~> 0.13"},
# {:toolshed, "~> 0.3"},
# {:plug_cowboy, "~> 2.6"},
# {:jason, "~> 1.4"},
# {:nerves_hub_link, "~> 2.0"}  # OTA

# Compilar y flashear (con SD card)
MIX_TARGET=rpi4 mix deps.get
MIX_TARGET=rpi4 mix firmware
MIX_TARGET=rpi4 mix burn
```

### Estructura del proyecto Nerves

```
my_device/
├── config/
│   ├── config.exs          # config compartida
│   └── target.exs          # config SOLO para el dispositivo (GPIO, WiFi, etc.)
├── lib/
│   └── my_device/
│       ├── application.ex  # Application con árbol de supervisión
│       ├── blinker.ex      # GenServer de LED
│       ├── sensor.ex       # Lectura de sensor I2C
│       ├── sensor_store.ex # ETS store de readings
│       └── sensor_api.ex   # HTTP endpoint
├── rootfs_overlay/         # Archivos copiados al rootfs del dispositivo
│   └── etc/
│       └── erlinit.config  # Config del init system de BEAM
└── mix.exs
```

---

## Ejercicio 1 — LED Blinker con GenServer y GPIO

Control de LED físico en un pin GPIO. El GenServer encapsula el ciclo de vida del GPIO
(open al init, close al terminate) y maneja el patrón de blink de forma configurable.

### Implementación

```elixir
# lib/my_device/blinker.ex
defmodule MyDevice.Blinker do
  use GenServer
  require Logger
  alias Circuits.GPIO

  # API pública

  def start_link(opts) do
    pin      = Keyword.fetch!(opts, :pin)
    interval = Keyword.get(opts, :interval_ms, 500)
    GenServer.start_link(__MODULE__, {pin, interval}, name: __MODULE__)
  end

  def set_interval(ms) when is_integer(ms) and ms > 0 do
    GenServer.cast(__MODULE__, {:set_interval, ms})
  end

  def stop_blink, do: GenServer.cast(__MODULE__, :stop)
  def start_blink, do: GenServer.cast(__MODULE__, :start)
  def status, do: GenServer.call(__MODULE__, :status)

  # Callbacks

  @impl true
  def init({pin, interval}) do
    {:ok, gpio} = GPIO.open(pin, :output)
    GPIO.write(gpio, 0)

    state = %{
      gpio:     gpio,
      pin:      pin,
      level:    0,
      interval: interval,
      active:   true,
      timer:    nil
    }

    {:ok, schedule_blink(state)}
  end

  @impl true
  def handle_info(:blink, %{active: false} = state) do
    {:noreply, state}
  end

  def handle_info(:blink, state) do
    new_level = 1 - state.level
    GPIO.write(state.gpio, new_level)
    {:noreply, schedule_blink(%{state | level: new_level})}
  end

  @impl true
  def handle_cast({:set_interval, ms}, state) do
    cancel_timer(state.timer)
    {:noreply, schedule_blink(%{state | interval: ms})}
  end

  def handle_cast(:stop, state) do
    cancel_timer(state.timer)
    GPIO.write(state.gpio, 0)
    {:noreply, %{state | active: false, level: 0, timer: nil}}
  end

  def handle_cast(:start, state) do
    {:noreply, schedule_blink(%{state | active: true})}
  end

  @impl true
  def handle_call(:status, _from, state) do
    info = %{
      pin:      state.pin,
      active:   state.active,
      level:    state.level,
      interval: state.interval
    }
    {:reply, info, state}
  end

  @impl true
  def terminate(_reason, state) do
    cancel_timer(state.timer)
    GPIO.write(state.gpio, 0)
    GPIO.close(state.gpio)
    Logger.info("Blinker GPIO #{state.pin} released")
  end

  # Helpers privados

  defp schedule_blink(%{active: false} = state), do: state

  defp schedule_blink(state) do
    timer = Process.send_after(self(), :blink, state.interval)
    %{state | timer: timer}
  end

  defp cancel_timer(nil), do: :ok
  defp cancel_timer(ref), do: Process.cancel_timer(ref)
end
```

### Supervisor y Application

```elixir
# lib/my_device/application.ex
defmodule MyDevice.Application do
  use Application

  @impl true
  def start(_type, _args) do
    # target/0 retorna :host cuando compilas para tu PC, :rpi4 en el dispositivo
    children = children(target())

    opts = [strategy: :one_for_one, name: MyDevice.Supervisor]
    Supervisor.start_link(children, opts)
  end

  # En el host (dev/test) no iniciamos GPIO
  def children(:host), do: []

  def children(_target) do
    [
      # LED en pin 18 (GPIO header del RPi4)
      {MyDevice.Blinker, [pin: 18, interval_ms: 500]},
      {MyDevice.SensorStore, []},
      {MyDevice.Sensor, [bus: "i2c-1", address: 0x76]},
      {Plug.Cowboy, scheme: :http, plug: MyDevice.SensorAPI, port: 4000}
    ]
  end

  def target do
    Application.get_env(:my_device, :target, Mix.target())
  end
end
```

### config/target.exs

```elixir
# config/target.exs — solo se carga cuando MIX_TARGET != :host
import Config

# El pin GPIO depende del hardware físico conectado
config :my_device, :blinker, pin: 18, interval_ms: 500

# Configurar VintageNet para WiFi (ver ejercicio 64)
config :vintage_net,
  config: [
    {"eth0", %{type: VintageNetEthernet, ipv4: %{method: :dhcp}}},
    {"wlan0", %{
      type: VintageNetWiFi,
      vintage_net_wifi: %{
        networks: [%{ssid: System.get_env("WIFI_SSID"), psk: System.get_env("WIFI_PSK")}]
      },
      ipv4: %{method: :dhcp}
    }}
  ]
```

### Tests

```elixir
# test/my_device/blinker_test.exs
defmodule MyDevice.BlinkerTest do
  use ExUnit.Case, async: false

  # En tests usamos un mock de GPIO para no necesitar hardware real
  setup do
    # Circuits.GPIO en modo mock retorna un proceso Erlang simulado
    Application.put_env(:circuits_gpio, :gpio_backend, Circuits.GPIO.Backend.Stub)
    :ok
  end

  test "inicia en estado activo con nivel 0" do
    {:ok, _pid} = start_supervised({MyDevice.Blinker, [pin: 18, interval_ms: 50]})
    status = MyDevice.Blinker.status()
    assert status.active == true
    assert status.level == 0
  end

  test "cambia de nivel tras interval_ms" do
    {:ok, _pid} = start_supervised({MyDevice.Blinker, [pin: 18, interval_ms: 50]})
    :timer.sleep(75)
    status = MyDevice.Blinker.status()
    assert status.level == 1
  end

  test "stop_blink deja el LED apagado" do
    {:ok, _pid} = start_supervised({MyDevice.Blinker, [pin: 18, interval_ms: 50]})
    :timer.sleep(75)
    MyDevice.Blinker.stop_blink()
    :timer.sleep(100)
    status = MyDevice.Blinker.status()
    assert status.active == false
    assert status.level == 0
  end

  test "set_interval cambia la velocidad" do
    {:ok, _pid} = start_supervised({MyDevice.Blinker, [pin: 18, interval_ms: 500]})
    MyDevice.Blinker.set_interval(50)
    :timer.sleep(75)
    assert MyDevice.Blinker.status().level == 1
  end
end
```

---

## Ejercicio 2 — Sensor I2C: BME280 → ETS → HTTP

Lectura de temperatura/humedad/presión desde un sensor BME280 via I2C cada 5 segundos.
Los readings se almacenan en ETS (memoria RAM, acceso O(1)) y se exponen via HTTP con Plug.

### ETS Store — historial circular de readings

```elixir
# lib/my_device/sensor_store.ex
defmodule MyDevice.SensorStore do
  use GenServer

  @table     :sensor_readings
  @max_size  100   # últimas 100 lecturas

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def insert(reading) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(@table, {ts, reading})
    GenServer.cast(__MODULE__, :maybe_evict)
  end

  def latest do
    case :ets.last(@table) do
      :"$end_of_table" -> nil
      key -> :ets.lookup(@table, key) |> hd() |> elem(1)
    end
  end

  def history(n \\ 10) do
    :ets.tab2list(@table)
    |> Enum.sort_by(fn {ts, _} -> ts end, :desc)
    |> Enum.take(n)
    |> Enum.map(fn {_ts, reading} -> reading end)
  end

  # Callbacks

  @impl true
  def init(_) do
    :ets.new(@table, [:ordered_set, :public, :named_table, read_concurrency: true])
    {:ok, %{}}
  end

  @impl true
  def handle_cast(:maybe_evict, state) do
    size = :ets.info(@table, :size)

    if size > @max_size do
      # Eliminar el reading más antiguo (ETS ordered_set: first = más antiguo)
      oldest_key = :ets.first(@table)
      :ets.delete(@table, oldest_key)
    end

    {:noreply, state}
  end
end
```

### Sensor GenServer — polling cada 5 segundos

```elixir
# lib/my_device/sensor.ex
defmodule MyDevice.Sensor do
  use GenServer
  require Logger
  alias Circuits.I2C

  @bme280_addr    0x76
  @poll_interval  5_000

  # Registros del BME280 (simplificado — en producción usar biblioteca oficial)
  @reg_temp_msb   0xFA
  @reg_hum_msb    0xFD
  @reg_press_msb  0xF7

  def start_link(opts) do
    bus     = Keyword.get(opts, :bus, "i2c-1")
    address = Keyword.get(opts, :address, @bme280_addr)
    GenServer.start_link(__MODULE__, {bus, address}, name: __MODULE__)
  end

  def current_reading, do: MyDevice.SensorStore.latest()

  @impl true
  def init({bus, address}) do
    {:ok, ref} = I2C.open(bus)
    :ok = init_sensor(ref, address)
    schedule_poll()

    {:ok, %{bus_ref: ref, address: address, error_count: 0}}
  end

  @impl true
  def handle_info(:poll, state) do
    case read_sensor(state.bus_ref, state.address) do
      {:ok, reading} ->
        MyDevice.SensorStore.insert(reading)
        Logger.debug("Sensor: #{inspect(reading)}")
        schedule_poll()
        {:noreply, %{state | error_count: 0}}

      {:error, reason} ->
        count = state.error_count + 1
        Logger.warning("Sensor read error #{count}: #{inspect(reason)}")
        # Backoff exponencial hasta 60 segundos
        delay = min(@poll_interval * count, 60_000)
        Process.send_after(self(), :poll, delay)
        {:noreply, %{state | error_count: count}}
    end
  end

  @impl true
  def terminate(_reason, state) do
    I2C.close(state.bus_ref)
  end

  # Helpers privados

  defp schedule_poll do
    Process.send_after(self(), :poll, @poll_interval)
  end

  defp init_sensor(ref, address) do
    # BME280: modo normal, oversampling x1 para temp/hum/press
    I2C.write(ref, address, <<0xF4, 0b00100111>>)  # ctrl_meas
    I2C.write(ref, address, <<0xF2, 0b00000001>>)  # ctrl_hum
  end

  defp read_sensor(ref, address) do
    with {:ok, temp_raw}  <- I2C.write_read(ref, address, <<@reg_temp_msb>>, 3),
         {:ok, hum_raw}   <- I2C.write_read(ref, address, <<@reg_hum_msb>>, 2),
         {:ok, press_raw} <- I2C.write_read(ref, address, <<@reg_press_msb>>, 3) do
      reading = %{
        temperature_c: decode_temperature(temp_raw),
        humidity_pct:  decode_humidity(hum_raw),
        pressure_hpa:  decode_pressure(press_raw),
        timestamp:     DateTime.utc_now()
      }
      {:ok, reading}
    end
  end

  # Conversión de bytes crudos a valores físicos (compensación simplificada)
  # En producción: usar la compensación oficial de Bosch con registros de calibración
  defp decode_temperature(<<msb, lsb, xlsb>>) do
    raw = (msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)
    # Aproximación lineal — la compensación real usa polinomio de calibración
    raw / 5120.0
  end

  defp decode_humidity(<<msb, lsb>>) do
    raw = (msb <<< 8) ||| lsb
    raw / 1024.0
  end

  defp decode_pressure(<<msb, lsb, xlsb>>) do
    raw = (msb <<< 12) ||| (lsb <<< 4) ||| (xlsb >>> 4)
    raw / 25600.0
  end
end
```

### HTTP API con Plug

```elixir
# lib/my_device/sensor_api.ex
defmodule MyDevice.SensorAPI do
  use Plug.Router

  plug :match
  plug Plug.Parsers, parsers: [:json], json_decoder: Jason
  plug :dispatch

  get "/api/sensor/current" do
    case MyDevice.SensorStore.latest() do
      nil ->
        send_resp(conn, 503, Jason.encode!(%{error: "no readings yet"}))

      reading ->
        send_resp(conn, 200, Jason.encode!(reading))
    end
  end

  get "/api/sensor/history" do
    n = conn.params["n"] |> to_integer(10)
    readings = MyDevice.SensorStore.history(n)
    send_resp(conn, 200, Jason.encode!(readings))
  end

  get "/api/system" do
    info = %{
      uptime_seconds:   Nerves.Runtime.uptime() |> elem(0),
      firmware_version: Nerves.Runtime.firmware_metadata()["nerves_fw_version"],
      kernel_version:   Nerves.Runtime.uname().sysname,
      memory_mb:        :erlang.memory(:total) |> div(1_048_576)
    }
    send_resp(conn, 200, Jason.encode!(info))
  end

  match _ do
    send_resp(conn, 404, Jason.encode!(%{error: "not found"}))
  end

  defp to_integer(nil, default), do: default
  defp to_integer(s, default) do
    case Integer.parse(s) do
      {n, ""} when n > 0 -> min(n, 100)
      _                  -> default
    end
  end
end
```

### Tests del sensor store

```elixir
# test/my_device/sensor_store_test.exs
defmodule MyDevice.SensorStoreTest do
  use ExUnit.Case, async: false

  setup do
    # Iniciar store limpio para cada test
    start_supervised!(MyDevice.SensorStore)
    :ok
  end

  test "latest retorna nil cuando no hay readings" do
    assert MyDevice.SensorStore.latest() == nil
  end

  test "insert y latest retornan el último reading" do
    reading = %{temperature_c: 22.5, humidity_pct: 60.0, pressure_hpa: 1013.2}
    MyDevice.SensorStore.insert(reading)
    assert MyDevice.SensorStore.latest() == reading
  end

  test "history retorna los últimos N readings en orden DESC" do
    for i <- 1..5 do
      MyDevice.SensorStore.insert(%{temperature_c: i * 1.0})
      :timer.sleep(1)   # asegurar timestamps distintos
    end

    history = MyDevice.SensorStore.history(3)
    assert length(history) == 3
    assert hd(history).temperature_c == 5.0
  end

  test "evicción mantiene máximo de 100 readings" do
    for i <- 1..110 do
      MyDevice.SensorStore.insert(%{temperature_c: i * 1.0})
    end

    # Dar tiempo al cast de evicción
    :timer.sleep(10)
    history = MyDevice.SensorStore.history(200)
    assert length(history) <= 100
  end
end
```

---

## Ejercicio 3 — OTA Updates con NervesHub

NervesHub es la plataforma oficial de actualizaciones OTA para Nerves. Cada firmware tiene
un número de versión y una firma digital. El dispositivo verifica la firma antes de aplicar.

### Setup NervesHub

```elixir
# mix.exs — añadir dependencia
{:nerves_hub_link, "~> 2.0"}

# config/target.exs
config :nerves_hub_link,
  host: "devices.nerveshub.org",
  # El certificado del dispositivo se genera con: mix nerves_hub.device.cert
  # y se embebe en el firmware durante el build
  device_api_host: "device.nerveshub.org"

# nerves_hub.exs — credenciales de la organización
config :nerves_hub_link,
  org: System.get_env("NERVES_HUB_ORG"),
  product: "my_device"
```

### Workflow de publicación de firmware

```bash
# 1. Autenticarse en NervesHub
mix nerves_hub.user auth

# 2. Crear organización y producto (una sola vez)
mix nerves_hub.org create my_org
mix nerves_hub.product create my_device

# 3. Generar certificado del dispositivo (por dispositivo físico)
mix nerves_hub.device create SN001
mix nerves_hub.device.cert create SN001

# 4. Compilar firmware con versión bumpeada (mix.exs: version: "1.1.0")
MIX_TARGET=rpi4 mix firmware

# 5. Firmar y publicar firmware
MIX_TARGET=rpi4 mix nerves_hub.firmware publish \
  --product my_device \
  --description "Fix sensor polling backoff"

# 6. Crear deployment (qué dispositivos reciben la actualización)
mix nerves_hub.deployment create \
  --product my_device \
  --firmware 1.1.0 \
  --name production \
  --tag production
```

### Módulo de gestión OTA en el dispositivo

```elixir
# lib/my_device/ota_manager.ex
defmodule MyDevice.OTAManager do
  use GenServer
  require Logger

  @check_interval 30 * 60 * 1000   # verificar cada 30 minutos

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    # NervesHub.Client se activa automáticamente; aquí solo monitoreamos
    :ok = :net_kernel.monitor_nodes(true)
    schedule_check()
    {:ok, %{update_pending: false, last_check: nil}}
  end

  # NervesHubLink llama a este callback cuando hay un firmware disponible
  # Retornar :apply aplica inmediatamente; :ignore pospone
  def update_available(data) do
    Logger.info("OTA update available: #{inspect(data)}")

    # Verificar si es seguro actualizar (no hay operaciones críticas activas)
    if safe_to_update?() do
      Logger.info("Applying OTA update")
      :apply
    else
      Logger.warning("Postponing OTA: critical operation in progress")
      :ignore
    end
  end

  # NervesHubLink llama a esto si la actualización falla
  def handle_error(error) do
    Logger.error("OTA error: #{inspect(error)}")
    # NervesHub hace rollback automático al firmware anterior si el dispositivo
    # no se reconecta dentro del timeout configurado
  end

  @impl true
  def handle_info(:check_connectivity, state) do
    connectivity = VintageNet.get(["interface", "wlan0", "connection"])
    Logger.debug("OTA check - connectivity: #{connectivity}")
    schedule_check()
    {:noreply, %{state | last_check: DateTime.utc_now()}}
  end

  defp schedule_check do
    Process.send_after(self(), :check_connectivity, @check_interval)
  end

  # Solo actualizamos si el sensor está en estado estable (sin errores recientes)
  defp safe_to_update? do
    case MyDevice.SensorStore.latest() do
      nil -> true                    # sin datos = seguro actualizar
      _   -> sensor_stable?()
    end
  end

  defp sensor_stable? do
    # Evitar actualizar si el sensor ha tenido errores en los últimos 5 minutos
    history = MyDevice.SensorStore.history(5)
    length(history) >= 3
  end
end
```

### Verificar estado OTA desde IEx en el dispositivo

```elixir
# Conectar al dispositivo via SSH (Nerves incluye servidor SSH por defecto)
# ssh nerves.local

# En IEx del dispositivo:
Nerves.Runtime.firmware_metadata()
# => %{
#   "nerves_fw_version"     => "1.1.0",
#   "nerves_fw_product"     => "my_device",
#   "nerves_fw_uuid"        => "abc123...",
#   "nerves_fw_platform"    => "rpi4",
#   "nerves_fw_architecture" => "arm"
# }

# Forzar verificación manual de updates
NervesHubLink.check_for_update()

# Ver estado de conectividad con el hub
NervesHubLink.connected?()
# => true

# Toolshed: herramientas de debugging en el dispositivo
import Toolshed
top()          # procesos BEAM por memoria/reductions
ifconfig()     # interfaces de red
ping("8.8.8.8")
dmesg()        # log del kernel Linux

# Historial de firmwares (U-Boot guarda A/B partitions)
cmd("fw_printenv nerves_fw_active")
# => nerves_fw_active=a
```

### rootfs_overlay — personalizar el sistema

```
rootfs_overlay/
├── etc/
│   ├── erlinit.config       # args del init de BEAM
│   └── fw_env.config        # particiones U-Boot
└── lib/
    └── udev/
        └── rules.d/
            └── 99-gpio.rules  # permisos GPIO sin sudo
```

```
# rootfs_overlay/etc/erlinit.config
-h 10000           # heart timeout en ms (reboot si BEAM muere)
-r /usr/lib/erlang
--verbose
```

```
# rootfs_overlay/lib/udev/rules.d/99-gpio.rules
SUBSYSTEM=="gpio", GROUP="gpio", MODE="0660"
SUBSYSTEM=="i2c-dev", GROUP="i2c", MODE="0660"
```

---

## Debugging en dispositivo real

```elixir
# Toolshed es la navaja suiza de Nerves en IEx

import Toolshed

# Sistema operativo
uname()
# => %{sysname: "Linux", nodename: "nerves-abc123", release: "6.1.0", ...}

Nerves.Runtime.uptime()
# => {3600, 234567}   # {segundos, microsegundos}

# Procesos BEAM
top()
#   PID         Name                   Memory  Reductions
#   <0.256.0>   MyDevice.SensorStore   24 KB   1,234,567
#   <0.255.0>   MyDevice.Sensor        12 KB   234,567

# Red
ifconfig()
# wlan0: flags=4163<UP,BROADCAST,RUNNING>
#   inet 192.168.1.42  netmask 255.255.255.0

# GPIO desde IEx
alias Circuits.GPIO
{:ok, gpio} = GPIO.open(18, :output)
GPIO.write(gpio, 1)   # encender LED
GPIO.write(gpio, 0)   # apagar LED
GPIO.close(gpio)

# I2C — detectar dispositivos conectados
alias Circuits.I2C
{:ok, ref} = I2C.open("i2c-1")
I2C.detect_devices(ref)
# => [0x76]   # BME280 en dirección 0x76
```

---

## Criterios de aceptación

- [ ] `Blinker.start_link/1` abre GPIO y comienza blink automático
- [ ] `Blinker.set_interval/1` cambia velocidad en caliente sin reiniciar
- [ ] `Blinker.stop_blink/0` apaga LED y deja GPIO en nivel 0
- [ ] `terminate/2` cierra el GPIO correctamente (verificar con `GPIO.close`)
- [ ] `SensorStore.insert/1` almacena en ETS con timestamp monotónico como clave
- [ ] `SensorStore.history/1` retorna readings en orden DESC por timestamp
- [ ] Evicción mantiene el store en máximo 100 entradas
- [ ] `GET /api/sensor/current` retorna JSON con el último reading
- [ ] `GET /api/system` retorna versión de firmware y uptime
- [ ] `OTAManager.update_available/1` retorna `:apply` o `:ignore` según estado
- [ ] Tests corren en host (`MIX_TARGET=host mix test`) sin hardware real

---

## Retos adicionales (opcional)

- Añadir un botón físico (GPIO input con interrupt via `GPIO.set_interrupts/2`) que pause/reanude el blink
- Implementar compensación completa del BME280 usando registros de calibración (Bosch datasheet)
- Exponer métricas en formato Prometheus: `GET /metrics` con `temperature_celsius` y `humidity_percent`
- Configurar `Nerves.Logging` para enviar logs del dispositivo a un servidor syslog remoto
- A/B firmware partitions: verificar que el rollback automático funciona flasheando un firmware intencionalmente roto
