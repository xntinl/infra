# 64 — Nerves: Networking y Cloud Integration

**Difficulty**: Avanzado  
**Tiempo estimado**: 6-8 horas  
**Área**: Nerves · VintageNet · MQTT · IoT Cloud · Fleet Management

---

## Contexto

Un dispositivo IoT sin conectividad no sirve de nada. Este ejercicio cubre la capa de red de
Nerves: `VintageNet` para gestión de interfaces (WiFi/Ethernet/LTE), `Tortoise311` para
telemetría MQTT hacia cloud (AWS IoT, HiveMQ), buffering offline cuando no hay red, y
fleet management de múltiples dispositivos.

La premisa es un gateway industrial que lee sensores cada minuto, publica a MQTT, y sigue
funcionando aunque la conexión a internet se corte durante horas.

---

## Setup del proyecto

```elixir
# mix.exs — dependencias de red y MQTT
{:vintage_net, "~> 0.13"},
{:vintage_net_wifi, "~> 0.12"},
{:vintage_net_direct, "~> 0.10"},
{:tortoise311, "~> 0.12"},          # cliente MQTT
{:jason, "~> 1.4"},
{:nerves_runtime, "~> 0.13"},
{:property_table, "~> 0.2"}         # estado reactivo compartido
```

---

## Ejercicio 1 — WiFi Manager con Reconexión y Backoff Exponencial

`VintageNet` gestiona el bajo nivel de red. Este GenServer construye encima: monitorea
el estado de conectividad via `PropertyTable`, detecta caídas y reconecta con backoff
exponencial para no sobrecargar el AP.

### WiFi Manager

```elixir
# lib/my_device/wifi_manager.ex
defmodule MyDevice.WiFiManager do
  use GenServer
  require Logger

  @interface      "wlan0"
  @max_backoff_ms 5 * 60 * 1_000    # máximo 5 minutos entre reintentos
  @base_backoff   5_000               # comenzar con 5 segundos

  # Estados de conectividad que reporta VintageNet
  @connected_states [:internet, :lan]

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def connected? do
    VintageNet.get(["interface", @interface, "connection"]) in @connected_states
  end

  def current_ip do
    VintageNet.get(["interface", @interface, "addresses"])
    |> List.first()
    |> case do
      nil  -> nil
      addr -> addr.address |> :inet.ntoa() |> to_string()
    end
  end

  def configure(ssid, psk) do
    GenServer.call(__MODULE__, {:configure, ssid, psk})
  end

  # Callbacks

  @impl true
  def init(opts) do
    ssid = Keyword.fetch!(opts, :ssid)
    psk  = Keyword.fetch!(opts, :psk)

    # Suscribirse a cambios de estado de la interfaz WiFi
    VintageNet.subscribe(["interface", @interface, "connection"])
    VintageNet.subscribe(["interface", @interface, "wifi", "bssid"])

    state = %{
      ssid:         ssid,
      psk:          psk,
      backoff:      @base_backoff,
      attempt:      0,
      reconnect_ref: nil
    }

    # Iniciar conexión inmediatamente
    {:ok, state, {:continue, :connect}}
  end

  @impl true
  def handle_continue(:connect, state) do
    :ok = apply_wifi_config(state.ssid, state.psk)
    {:noreply, state}
  end

  # VintageNet envía {:update, path, old_value, new_value}
  @impl true
  def handle_info(
    {VintageNet, ["interface", @interface, "connection"], _old, new_status, _meta},
    state
  ) do
    case new_status do
      status when status in @connected_states ->
        Logger.info("WiFi connected (#{status}) IP: #{current_ip()}")
        # Reset backoff al conectar exitosamente
        {:noreply, %{state | backoff: @base_backoff, attempt: 0}}

      :disconnected ->
        Logger.warning("WiFi disconnected — scheduling reconnect in #{state.backoff}ms")
        ref = Process.send_after(self(), :reconnect, state.backoff)
        {:noreply, %{state | reconnect_ref: ref}}

      other ->
        Logger.debug("WiFi status: #{other}")
        {:noreply, state}
    end
  end

  def handle_info(:reconnect, state) do
    attempt = state.attempt + 1
    Logger.info("WiFi reconnect attempt #{attempt}")

    :ok = apply_wifi_config(state.ssid, state.psk)

    # Backoff exponencial con jitter: evita que todos los dispositivos
    # del fleet se reconecten simultáneamente tras un corte de red
    next_backoff = min(state.backoff * 2, @max_backoff_ms)
    jitter = :rand.uniform(div(next_backoff, 4))

    {:noreply, %{state | backoff: next_backoff + jitter, attempt: attempt, reconnect_ref: nil}}
  end

  @impl true
  def handle_call({:configure, ssid, psk}, _from, state) do
    :ok = apply_wifi_config(ssid, psk)
    {:reply, :ok, %{state | ssid: ssid, psk: psk, backoff: @base_backoff, attempt: 0}}
  end

  # Helpers privados

  defp apply_wifi_config(ssid, psk) do
    config = %{
      type: VintageNetWiFi,
      vintage_net_wifi: %{
        networks: [%{ssid: ssid, psk: psk, key_mgmt: :wpa_psk}]
      },
      ipv4: %{method: :dhcp}
    }
    VintageNet.configure(@interface, config)
  end
end
```

### Tests del WiFi Manager

```elixir
# test/my_device/wifi_manager_test.exs
defmodule MyDevice.WiFiManagerTest do
  use ExUnit.Case, async: false

  # VintageNet tiene modo mock para tests
  setup do
    Application.put_env(:vintage_net, :backend, VintageNet.Test.Backend)
    :ok
  end

  test "connected? retorna false cuando no hay interfaz activa" do
    refute MyDevice.WiFiManager.connected?()
  end

  test "backoff aumenta exponencialmente tras desconexiones" do
    {:ok, pid} = start_supervised(
      {MyDevice.WiFiManager, [ssid: "TestNet", psk: "secret123"]}
    )

    state = :sys.get_state(pid)
    initial_backoff = state.backoff

    # Simular desconexión
    send(pid, {VintageNet, ["interface", "wlan0", "connection"],
               :internet, :disconnected, %{}})
    :timer.sleep(20)

    state_after = :sys.get_state(pid)

    # Simular segundo intento fallido
    send(pid, {VintageNet, ["interface", "wlan0", "connection"],
               :disconnected, :disconnected, %{}})
    :timer.sleep(20)

    state_after2 = :sys.get_state(pid)

    # El backoff debe haber aumentado
    assert state_after2.backoff > initial_backoff
  end

  test "reset de backoff tras conexión exitosa" do
    {:ok, pid} = start_supervised(
      {MyDevice.WiFiManager, [ssid: "TestNet", psk: "secret123"]}
    )

    # Simular desconexión y múltiples reintentos
    send(pid, {VintageNet, ["interface", "wlan0", "connection"],
               :internet, :disconnected, %{}})
    :timer.sleep(20)

    # Simular reconexión exitosa
    send(pid, {VintageNet, ["interface", "wlan0", "connection"],
               :disconnected, :internet, %{}})
    :timer.sleep(20)

    state = :sys.get_state(pid)
    assert state.attempt == 0
  end
end
```

---

## Ejercicio 2 — MQTT Telemetría con Buffering Offline

El dispositivo publica readings de sensores a AWS IoT / HiveMQ via MQTT con QoS 1.
Cuando no hay conectividad, los mensajes se acumulan en un buffer en ETS (en RAM) o DETS
(persistente en disco). Al reconectar, se vacía el buffer en orden FIFO.

### MQTT Publisher con buffer offline

```elixir
# lib/my_device/telemetry_publisher.ex
defmodule MyDevice.TelemetryPublisher do
  use GenServer
  require Logger

  @topic_prefix   "devices"
  @max_buffer     500      # máximo mensajes en buffer offline
  @flush_interval 1_000    # intentar flush cada segundo cuando hay buffer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def publish(payload) when is_map(payload) do
    GenServer.cast(__MODULE__, {:publish, payload})
  end

  # Callbacks

  @impl true
  def init(opts) do
    host     = Keyword.fetch!(opts, :host)
    port     = Keyword.get(opts, :port, 8883)
    device_id = Keyword.fetch!(opts, :device_id)

    # Buffer en ETS — :queue semántica FIFO, ETS para acceso rápido
    buffer = :queue.new()

    state = %{
      host:         host,
      port:         port,
      device_id:    device_id,
      mqtt_pid:     nil,
      connected:    false,
      buffer:       buffer,
      buffer_count: 0
    }

    VintageNet.subscribe(["interface", "wlan0", "connection"])
    {:ok, state, {:continue, :connect_mqtt}}
  end

  @impl true
  def handle_continue(:connect_mqtt, state) do
    case start_mqtt_client(state) do
      {:ok, pid} ->
        Logger.info("MQTT connected to #{state.host}")
        {:noreply, %{state | mqtt_pid: pid, connected: true}}

      {:error, reason} ->
        Logger.warning("MQTT connect failed: #{inspect(reason)} — buffering offline")
        schedule_flush()
        {:noreply, state}
    end
  end

  @impl true
  def handle_cast({:publish, payload}, state) do
    message = encode_message(state.device_id, payload)

    if state.connected do
      case mqtt_publish(state.mqtt_pid, topic(state.device_id), message) do
        :ok ->
          {:noreply, state}
        {:error, _reason} ->
          # Publicación falló — bufferizar
          {:noreply, buffer_message(state, message)}
      end
    else
      {:noreply, buffer_message(state, message)}
    end
  end

  # VintageNet notifica reconexión
  def handle_info(
    {VintageNet, ["interface", "wlan0", "connection"], _old, :internet, _meta},
    state
  ) do
    Logger.info("Network up — reconnecting MQTT and flushing buffer")
    case start_mqtt_client(state) do
      {:ok, pid} ->
        new_state = %{state | mqtt_pid: pid, connected: true}
        {:noreply, flush_buffer(new_state)}

      {:error, reason} ->
        Logger.warning("MQTT reconnect failed: #{inspect(reason)}")
        {:noreply, state}
    end
  end

  def handle_info(
    {VintageNet, ["interface", "wlan0", "connection"], _old, :disconnected, _meta},
    state
  ) do
    Logger.warning("Network down — MQTT disconnected, buffering messages")
    {:noreply, %{state | connected: false, mqtt_pid: nil}}
  end

  def handle_info(:flush_buffer, %{connected: false} = state) do
    schedule_flush()
    {:noreply, state}
  end

  def handle_info(:flush_buffer, state) do
    {:noreply, flush_buffer(state)}
  end

  # Helpers privados

  defp start_mqtt_client(state) do
    {:ok, _pid} = Tortoise311.Supervisor.start_child(
      client_id: state.device_id,
      handler: {MyDevice.MQTTHandler, []},
      server: {
        Tortoise311.Transport.SSL,
        host: state.host,
        port: state.port,
        # Certificados del dispositivo embebidos en el firmware
        cacertfile: Application.app_dir(:my_device, "priv/certs/aws-root-ca.pem"),
        certfile:   Application.app_dir(:my_device, "priv/certs/device-cert.pem"),
        keyfile:    Application.app_dir(:my_device, "priv/certs/device-key.pem")
      }
    )
  end

  defp mqtt_publish(pid, topic, message) do
    Tortoise311.publish(pid, topic, message, qos: 1)
  end

  defp topic(device_id), do: "#{@topic_prefix}/#{device_id}/telemetry"

  defp encode_message(device_id, payload) do
    Jason.encode!(%{
      device_id:  device_id,
      timestamp:  DateTime.utc_now() |> DateTime.to_iso8601(),
      payload:    payload
    })
  end

  defp buffer_message(%{buffer_count: count} = state, _message) when count >= @max_buffer do
    Logger.warning("MQTT buffer full (#{@max_buffer} msgs) — dropping oldest")
    {_, smaller_queue} = :queue.out(state.buffer)
    state
    |> Map.put(:buffer, smaller_queue)
    |> buffer_message_unsafe(state.buffer_count)
  end

  defp buffer_message(state, message) do
    buffer_message_unsafe(%{state | buffer: :queue.in(message, state.buffer)},
                           state.buffer_count + 1)
  end

  defp buffer_message_unsafe(state, count), do: %{state | buffer_count: count}

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

      {{:value, message}, rest} ->
        case mqtt_publish(state.mqtt_pid, topic(state.device_id), message) do
          :ok ->
            do_flush(%{state | buffer: rest, buffer_count: state.buffer_count - 1})

          {:error, reason} ->
            Logger.warning("Flush failed: #{inspect(reason)} — stopping flush")
            # Devolver el mensaje al frente de la queue
            %{state | buffer: :queue.in_r(message, rest)}
        end
    end
  end

  defp schedule_flush do
    Process.send_after(self(), :flush_buffer, @flush_interval)
  end
end
```

### MQTT Handler — callbacks de eventos Tortoise311

```elixir
# lib/my_device/mqtt_handler.ex
defmodule MyDevice.MQTTHandler do
  use Tortoise311.Handler
  require Logger

  @impl true
  def init(_opts), do: {:ok, %{}}

  @impl true
  def connection(:up, state) do
    Logger.info("MQTT connection established")
    {:ok, state}
  end

  def connection(:down, state) do
    Logger.warning("MQTT connection lost")
    {:ok, state}
  end

  # Mensajes recibidos desde el cloud (comandos al dispositivo)
  @impl true
  def handle_message(["devices", _device_id, "commands"], payload, state) do
    case Jason.decode(payload) do
      {:ok, %{"command" => "set_interval", "value" => ms}} ->
        Logger.info("Cloud command: set_interval #{ms}ms")
        MyDevice.Blinker.set_interval(ms)

      {:ok, %{"command" => "reboot"}} ->
        Logger.warning("Cloud command: reboot")
        Nerves.Runtime.reboot()

      {:ok, unknown} ->
        Logger.warning("Unknown command: #{inspect(unknown)}")

      {:error, _} ->
        Logger.error("Invalid command payload")
    end

    {:ok, state}
  end

  def handle_message(topic, _payload, state) do
    Logger.debug("Unhandled MQTT topic: #{inspect(topic)}")
    {:ok, state}
  end
end
```

### Tests del publisher

```elixir
# test/my_device/telemetry_publisher_test.exs
defmodule MyDevice.TelemetryPublisherTest do
  use ExUnit.Case, async: false

  # Mock del cliente MQTT para tests
  defmodule MockMQTT do
    def publish(_pid, _topic, message, _opts) do
      send(:test_process, {:published, message})
      :ok
    end
  end

  setup do
    Process.register(self(), :test_process)
    :ok
  end

  test "bufferiza mensajes cuando no hay conexión" do
    {:ok, pid} = start_supervised({
      MyDevice.TelemetryPublisher,
      [host: "localhost", device_id: "test-001"]
    })

    # Sin conexión MQTT, los mensajes deben acumularse
    MyDevice.TelemetryPublisher.publish(%{temperature: 22.5})
    MyDevice.TelemetryPublisher.publish(%{temperature: 23.0})

    :timer.sleep(50)
    state = :sys.get_state(pid)
    assert state.buffer_count == 2
    refute_received {:published, _}
  end

  test "buffer no supera max_buffer (drops oldest)" do
    {:ok, pid} = start_supervised({
      MyDevice.TelemetryPublisher,
      [host: "localhost", device_id: "test-002"]
    })

    # Publicar 510 mensajes (por encima del límite de 500)
    for i <- 1..510 do
      MyDevice.TelemetryPublisher.publish(%{seq: i})
    end

    :timer.sleep(100)
    state = :sys.get_state(pid)
    assert state.buffer_count <= 500
  end

  test "flush vacía el buffer en orden FIFO" do
    {:ok, pid} = start_supervised({
      MyDevice.TelemetryPublisher,
      [host: "localhost", device_id: "test-003"]
    })

    # Añadir mensajes al buffer manualmente
    for i <- 1..3 do
      MyDevice.TelemetryPublisher.publish(%{seq: i})
    end

    :timer.sleep(20)

    # Simular reconexión de red
    send(pid, {VintageNet, ["interface", "wlan0", "connection"],
               :disconnected, :internet, %{}})
    :timer.sleep(100)

    state = :sys.get_state(pid)
    assert state.buffer_count == 0
  end
end
```

---

## Ejercicio 3 — Fleet Management: Identificación por MAC y Reporting

En producción hay decenas o cientos de dispositivos. Cada uno se identifica por su MAC address,
registra sus métricas en un servidor central, y puede recibir comandos individuales o en grupo.

### Device Identity — MAC como identificador único

```elixir
# lib/my_device/device_identity.ex
defmodule MyDevice.DeviceIdentity do
  @moduledoc """
  Identidad estable del dispositivo basada en MAC address de la interfaz primaria.
  La MAC no cambia entre reboots ni actualizaciones de firmware.
  """

  def device_id do
    case mac_address("eth0") do
      {:ok, mac} -> format_device_id(mac)
      {:error, _} -> fallback_device_id()
    end
  end

  def mac_address(interface) do
    path = "/sys/class/net/#{interface}/address"
    case File.read(path) do
      {:ok, mac} -> {:ok, String.trim(mac)}
      error      -> error
    end
  end

  def device_info do
    %{
      device_id:        device_id(),
      firmware_version: firmware_version(),
      platform:         Nerves.Runtime.firmware_metadata()["nerves_fw_platform"],
      uptime_seconds:   Nerves.Runtime.uptime() |> elem(0),
      ip_address:       MyDevice.WiFiManager.current_ip(),
      connected:        MyDevice.WiFiManager.connected?()
    }
  end

  defp format_device_id(mac) do
    "device-" <> String.replace(mac, ":", "")
  end

  defp fallback_device_id do
    # Fallback: usar el hostname generado por Nerves (incluye hash único)
    {:ok, hostname} = :inet.gethostname()
    to_string(hostname)
  end

  defp firmware_version do
    Nerves.Runtime.firmware_metadata()
    |> Map.get("nerves_fw_version", "unknown")
  end
end
```

### Fleet Reporter — heartbeat periódico al dashboard

```elixir
# lib/my_device/fleet_reporter.ex
defmodule MyDevice.FleetReporter do
  use GenServer
  require Logger

  @heartbeat_interval 60 * 1_000   # heartbeat cada minuto
  @metrics_interval   5 * 60 * 1_000  # métricas completas cada 5 minutos

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    mqtt_publisher = Keyword.get(opts, :publisher, MyDevice.TelemetryPublisher)
    state = %{publisher: mqtt_publisher, last_metrics: nil}
    schedule_heartbeat()
    schedule_metrics()
    {:ok, state}
  end

  @impl true
  def handle_info(:heartbeat, state) do
    payload = %{
      type:       "heartbeat",
      device:     MyDevice.DeviceIdentity.device_info(),
      timestamp:  DateTime.utc_now() |> DateTime.to_iso8601()
    }
    state.publisher.publish(payload)
    schedule_heartbeat()
    {:noreply, state}
  end

  def handle_info(:full_metrics, state) do
    payload = %{
      type:      "metrics",
      device:    MyDevice.DeviceIdentity.device_info(),
      memory:    collect_memory_metrics(),
      processes: collect_process_metrics(),
      sensor:    MyDevice.SensorStore.latest(),
      timestamp: DateTime.utc_now() |> DateTime.to_iso8601()
    }
    state.publisher.publish(payload)
    schedule_metrics()
    {:noreply, %{state | last_metrics: DateTime.utc_now()}}
  end

  defp collect_memory_metrics do
    mem = :erlang.memory()
    %{
      total_mb:   div(mem[:total], 1_048_576),
      process_mb: div(mem[:processes], 1_048_576),
      binary_mb:  div(mem[:binary], 1_048_576),
      ets_mb:     div(mem[:ets], 1_048_576)
    }
  end

  defp collect_process_metrics do
    %{
      count: length(Process.list()),
      # Top 3 por memoria
      top_by_memory:
        Process.list()
        |> Enum.map(fn pid ->
          info = Process.info(pid, [:registered_name, :memory]) || []
          {Keyword.get(info, :registered_name, pid), Keyword.get(info, :memory, 0)}
        end)
        |> Enum.sort_by(&elem(&1, 1), :desc)
        |> Enum.take(3)
    }
  end

  defp schedule_heartbeat do
    Process.send_after(self(), :heartbeat, @heartbeat_interval)
  end

  defp schedule_metrics do
    Process.send_after(self(), :full_metrics, @metrics_interval)
  end
end
```

### PropertyTable — estado compartido reactivo

```elixir
# lib/my_device/device_state.ex
defmodule MyDevice.DeviceState do
  @moduledoc """
  Estado global reactivo del dispositivo usando PropertyTable.
  Otros procesos pueden suscribirse a cambios de estado específicos.
  """

  @table __MODULE__

  def child_spec(_opts) do
    PropertyTable.child_spec(name: @table, properties: [])
  end

  # Setters

  def set_connectivity(status) when status in [:connected, :disconnected, :connecting] do
    PropertyTable.put(@table, [:network, :status], status)
  end

  def set_sensor_status(status) when status in [:ok, :error, :degraded] do
    PropertyTable.put(@table, [:sensor, :status], status)
  end

  def set_last_reading(reading) do
    PropertyTable.put(@table, [:sensor, :last_reading], reading)
  end

  # Getters

  def connectivity, do: PropertyTable.get(@table, [:network, :status], :unknown)
  def sensor_status, do: PropertyTable.get(@table, [:sensor, :status], :unknown)
  def last_reading, do: PropertyTable.get(@table, [:sensor, :last_reading])

  # Suscripción reactiva a cambios
  # Llama a PropertyTable.subscribe(@table, [:network, :status]) desde cualquier proceso
  # Recibirás: {PropertyTable, [:network, :status], old_value, new_value, metadata}
  def subscribe_connectivity(pid \\ self()) do
    PropertyTable.subscribe(@table, [:network, :status], pid)
  end

  def subscribe_sensor(pid \\ self()) do
    PropertyTable.subscribe(@table, [:sensor, :status], pid)
  end
end
```

### Application con árbol completo

```elixir
# lib/my_device/application.ex
defmodule MyDevice.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = children(target())
    opts = [strategy: :one_for_one, name: MyDevice.Supervisor]
    Supervisor.start_link(children, opts)
  end

  def children(:host), do: [MyDevice.DeviceState]

  def children(_target) do
    device_id = MyDevice.DeviceIdentity.device_id()
    Logger.info("Starting device: #{device_id}")

    [
      MyDevice.DeviceState,
      MyDevice.SensorStore,
      {MyDevice.WiFiManager,
        ssid: System.fetch_env!("WIFI_SSID"),
        psk:  System.fetch_env!("WIFI_PSK")},
      {MyDevice.TelemetryPublisher,
        host:      System.fetch_env!("MQTT_HOST"),
        port:      8883,
        device_id: device_id},
      {MyDevice.Sensor, [bus: "i2c-1", address: 0x76]},
      {MyDevice.Blinker, [pin: 18, interval_ms: 1_000]},
      {MyDevice.FleetReporter, []}
    ]
  end

  defp target do
    Application.get_env(:my_device, :target, Mix.target())
  end
end
```

### Tests de fleet reporter

```elixir
# test/my_device/fleet_reporter_test.exs
defmodule MyDevice.FleetReporterTest do
  use ExUnit.Case, async: false

  defmodule MockPublisher do
    def publish(payload) do
      send(:test_process, {:published, payload})
      :ok
    end
  end

  setup do
    Process.register(self(), :test_process)
    :ok
  end

  test "publica heartbeat con información del dispositivo" do
    {:ok, _pid} = start_supervised(
      {MyDevice.FleetReporter, [publisher: MockPublisher]}
    )

    # Forzar heartbeat inmediato
    Process.send(:fleet_reporter_pid, :heartbeat, [:noconnect])
    :timer.sleep(50)

    assert_received {:published, %{type: "heartbeat", device: device_info}}
    assert Map.has_key?(device_info, :firmware_version)
    assert Map.has_key?(device_info, :uptime_seconds)
  end

  test "full_metrics incluye datos de memoria y procesos" do
    {:ok, pid} = start_supervised(
      {MyDevice.FleetReporter, [publisher: MockPublisher]}
    )

    send(pid, :full_metrics)
    :timer.sleep(50)

    assert_received {:published, %{type: "metrics", memory: memory, processes: procs}}
    assert memory.total_mb > 0
    assert procs.count > 0
  end
end
```

---

## Debugging de red en dispositivo real

```elixir
# Conectar via SSH al dispositivo: ssh nerves.local

import Toolshed

# Estado completo de interfaces de red
VintageNet.info()
# Interface wlan0
#   Type: VintageNetWiFi
#   Present: true
#   State: :configured
#   Connection: :internet
#   MAC Address: b8:27:eb:xx:xx:xx
#   IPv4 Addresses: 192.168.1.42/24

# Detalles WiFi
VintageNet.get(["interface", "wlan0", "wifi"])
# => %{bssid: "aa:bb:cc:dd:ee:ff", ssid: "MyNetwork", signal_dbm: -52, ...}

# Cambiar WiFi en caliente (sin reboot)
VintageNet.configure("wlan0", %{
  type: VintageNetWiFi,
  vintage_net_wifi: %{
    networks: [%{ssid: "NewNetwork", psk: "newpassword", key_mgmt: :wpa_psk}]
  },
  ipv4: %{method: :dhcp}
})

# Ping para verificar conectividad
ping("8.8.8.8")
# => Response from 8.8.8.8: time=12.3ms

# Ver procesos del sistema (MuonTrap ejecuta comandos del OS)
MuonTrap.cmd("ip", ["route"])
# => {"default via 192.168.1.1 dev wlan0 ...\n", 0}

# Estado del buffer MQTT
:sys.get_state(MyDevice.TelemetryPublisher)
# => %{connected: true, buffer_count: 0, ...}

# Estado de PropertyTable
MyDevice.DeviceState.connectivity()
# => :connected

MyDevice.DeviceState.last_reading()
# => %{temperature_c: 22.5, humidity_pct: 61.0, pressure_hpa: 1013.2, timestamp: ...}
```

---

## Criterios de aceptación

- [ ] `WiFiManager` se suscribe a eventos de VintageNet al init
- [ ] Reconexión automática se activa al recibir `:disconnected` de VintageNet
- [ ] Backoff exponencial no supera `@max_backoff_ms`
- [ ] Jitter aleatorio en backoff (verificar que dos instancias tienen tiempos distintos)
- [ ] `TelemetryPublisher.publish/1` bufferiza cuando `connected: false`
- [ ] Buffer no supera 500 mensajes (drops oldest al superar límite)
- [ ] Flush vaía el buffer en orden FIFO al reconectar
- [ ] `DeviceIdentity.device_id/0` lee MAC de `/sys/class/net/eth0/address`
- [ ] `FleetReporter` publica heartbeat cada minuto con info del dispositivo
- [ ] `PropertyTable` notifica a suscriptores cuando cambia el estado de red o sensor
- [ ] Todos los tests corren en host con mocks (sin hardware real)

---

## Retos adicionales (opcional)

- Implementar soporte LTE: añadir `VintageNetMobile` y configurar un modem USB Sierra Wireless
- Añadir prioridad de interfaz: Ethernet > WiFi > LTE con failover automático
- Persistir el buffer offline en DETS (disco) para sobrevivir reboots
- Implementar comando MQTT de actualización de WiFi credentials desde el cloud
- Dashboard simple en Phoenix LiveView que muestre el mapa de dispositivos del fleet en tiempo real
- Integración con AWS IoT Device Shadow para sincronizar configuración deseada vs reportada
