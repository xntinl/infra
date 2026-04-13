# Nerves OTA Updates with NervesHub and Signed Firmware

**Project**: `nerves_ota` — firmware delivery pipeline for a fleet of Nerves devices with cryptographically signed OTA updates distributed via NervesHub

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
nerves_ota/
├── lib/
│   └── nerves_ota.ex
├── script/
│   └── main.exs
├── test/
│   └── nerves_ota_test.exs
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
defmodule NervesOta.MixProject do
  use Mix.Project

  def project do
    [
      app: :nerves_ota,
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

### `lib/nerves_ota.ex`

```elixir
defmodule NervesOta.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    opts = [strategy: :one_for_one, name: NervesOta.Supervisor]
    Supervisor.start_link(children(target()), opts)
  end

  defp children(:host) do
    [{NervesOta.Health, []}]
  end

  defp children(_target) do
    [
      {NervesOta.Health, []},
      # nerves_hub_link starts its own supervision tree via Application callback.
      # We only add a process that validates firmware after health is green.
      {NervesOta.FirmwareValidator, []}
    ]
  end

  @doc "Returns target result."
  def target, do: Application.get_env(:nerves_ota, :target, :host)
end

defmodule NervesOta.NervesHubClient do
  @moduledoc """
  Policy for when and how to apply OTA updates.

  The device polls NervesHub over a persistent socket. When a deployment
  matches this device's tags, NervesHub announces an `UpdateInfo` payload
  to `update_available/1`. This module decides the next step.
  """

  @behaviour NervesHubLink.Client
  require Logger
  alias NervesOta.{UpdatePolicy, Health}

  @doc "Updates available result."
  @impl true
  def update_available(%{firmware_meta: meta} = update_info) do
    Logger.info("OTA announced: #{meta.uuid} version=#{meta.version}")

    case UpdatePolicy.decide(update_info, Health.snapshot(), DateTime.utc_now()) do
      :apply ->
        Logger.info("OTA policy: apply now")
        :apply

      {:reschedule, ms} ->
        Logger.info("OTA policy: defer #{ms}ms")
        {:reschedule, ms}

      {:ignore, reason} ->
        Logger.warning("OTA policy: ignoring (#{reason})")
        :ignore
    end
  end

  @doc "Handles fwup message result from percent."
  @impl true
  def handle_fwup_message({:progress, percent}) do
    if rem(percent, 10) == 0, do: Logger.info("fwup progress #{percent}%")
    :ok
  end

  @doc "Handles fwup message result from _msg."
  def handle_fwup_message({:ok, 0, _msg}) do
    Logger.info("fwup completed — rebooting")
    :ok
  end

  @doc "Handles fwup message result from _code and msg."
  def handle_fwup_message({:warning, _code, msg}) do
    Logger.warning("fwup warning: #{msg}")
    :ok
  end

  @doc "Handles fwup message result from code and msg."
  def handle_fwup_message({:error, code, msg}) do
    Logger.error("fwup error #{code}: #{msg}")
    :ok
  end

  @doc "Handles fwup message result from other."
  def handle_fwup_message(other) do
    Logger.debug("fwup: #{inspect(other)}")
    :ok
  end

  @doc "Handles error result from error."
  @impl true
  def handle_error(error) do
    Logger.error("NervesHub error: #{inspect(error)}")
    :ok
  end
end

defmodule NervesOta.UpdatePolicy do
  @moduledoc """
  Pure decision function — no side effects, fully testable.

  Rules (in order):
    1. If device is unhealthy, reject (`:ignore`). Fixing an unhealthy device
       by updating it is almost always the wrong move.
    2. If local time is within a configured maintenance window, apply now.
    3. Otherwise, defer until the next window.
  """

  @type decision :: :apply | {:reschedule, pos_integer()} | {:ignore, String.t()}
  @type health :: %{cpu_load: float(), disk_free_mb: non_neg_integer(), errors_last_hour: non_neg_integer()}

  @default_window {1, 4}  # 01:00 – 04:00 device local time

  @doc "Returns decide result from update_info, health, now and opts."
  @spec decide(map(), health(), DateTime.t(), keyword()) :: decision()
  def decide(update_info, health, now, opts \\ []) do
    window = Keyword.get(opts, :window, @default_window)

    cond do
      not healthy?(health) ->
        {:ignore, "health check failed"}

      force_apply?(update_info) ->
        :apply

      in_window?(now, window) ->
        :apply

      true ->
        {:reschedule, ms_until_window(now, window)}
    end
  end

  defp healthy?(%{cpu_load: cpu, disk_free_mb: disk, errors_last_hour: errs}) do
    cpu < 0.90 and disk > 100 and errs < 50
  end

  defp force_apply?(%{firmware_meta: %{description: desc}}) when is_binary(desc) do
    String.contains?(desc, "[force]")
  end

  defp force_apply?(_), do: false

  defp in_window?(%DateTime{hour: h}, {start, stop}) when stop > start, do: h >= start and h < stop
  defp in_window?(%DateTime{hour: h}, {start, stop}), do: h >= start or h < stop

  defp ms_until_window(%DateTime{hour: h, minute: m, second: s}, {start, _stop}) do
    hours_ahead = rem(start - h + 24, 24)
    seconds = hours_ahead * 3600 - m * 60 - s
    max(seconds, 60) * 1000
  end
end

defmodule NervesOta.Health do
  @moduledoc "Post-update health probe. Validates firmware only when green."
  use GenServer
  require Logger

  @probe_interval_ms 10_000
  @required_green_cycles 3

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  @doc "Returns snapshot result."
  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  @impl true
  def init(_opts) do
    Process.send_after(self(), :probe, @probe_interval_ms)
    {:ok, %{green_cycles: 0, last: %{cpu_load: 0.0, disk_free_mb: 0, errors_last_hour: 0}, validated?: false}}
  end

  @impl true
  def handle_call(:snapshot, _from, state), do: {:reply, state.last, state}

  @impl true
  def handle_info(:probe, state) do
    reading = probe()
    green? = green?(reading)

    state =
      state
      |> Map.put(:last, reading)
      |> Map.update!(:green_cycles, fn c -> if green?, do: c + 1, else: 0 end)
      |> maybe_validate_firmware()

    Process.send_after(self(), :probe, @probe_interval_ms)
    {:noreply, state}
  end

  defp probe do
    %{
      cpu_load: read_cpu_load(),
      disk_free_mb: read_disk_free_mb(),
      errors_last_hour: NervesOta.FirmwareMetadata.recent_error_count()
    }
  end

  defp green?(%{cpu_load: c, disk_free_mb: d, errors_last_hour: e}),
    do: c < 0.85 and d > 50 and e < 10

  defp maybe_validate_firmware(%{validated?: true} = s), do: s

  defp maybe_validate_firmware(%{green_cycles: c} = s) when c >= @required_green_cycles do
    Logger.info("Validating firmware after #{c} green health cycles")
    NervesOta.FirmwareMetadata.validate_firmware()
    %{s | validated?: true}
  end

  defp maybe_validate_firmware(s), do: s

  if Code.ensure_loaded?(:cpu_sup) do
    defp read_cpu_load do
      case :cpu_sup.avg1() do
        n when is_integer(n) -> n / 256.0
        _ -> 0.0
      end
    end
  else
    defp read_cpu_load, do: 0.0
  end

  defp read_disk_free_mb do
    case System.cmd("df", ["-m", "/root"], stderr_to_stdout: true) do
      {output, 0} ->
        output |> String.split("\n") |> Enum.at(1, "") |> String.split() |> Enum.at(3, "0") |> String.to_integer()

      _ ->
        1_000
    end
  rescue
    e in RuntimeError -> 1_000
  end
end

defmodule NervesOta.FirmwareMetadata do
  @moduledoc "Thin wrapper around `Nerves.Runtime.KV` and error counting."

  @doc "Returns active partition result."
  @spec active_partition() :: String.t()
  def active_partition do
    kv("nerves_fw_active") || "a"
  end

  @doc "Returns active uuid result."
  @spec active_uuid() :: String.t() | nil
  def active_uuid, do: kv("#{active_partition()}.nerves_fw_uuid")

  @doc "Returns active version result."
  @spec active_version() :: Version.t() | nil
  def active_version do
    case kv("#{active_partition()}.nerves_fw_version") do
      nil -> nil
      v -> Version.parse!(v)
    end
  end

  @doc "Returns whether validated holds."
  @spec validated?() :: boolean()
  def validated?, do: kv("nerves_fw_validated") == "1"

  @doc "Validates firmware result."
  @spec validate_firmware() :: :ok | {:error, term()}
  def validate_firmware do
    if Code.ensure_loaded?(Nerves.Runtime) do
      apply(Nerves.Runtime, :validate_firmware, [])
    else
      :ok
    end
  end

  @doc "Returns recent error count result."
  @spec recent_error_count() :: non_neg_integer()
  def recent_error_count do
    # In a real device, aggregate from :logger or a ring buffer.
    # Here we expose a configurable value for tests.
    Application.get_env(:nerves_ota, :error_count, 0)
  end

  defp kv(key) do
    case Application.get_env(:nerves_ota, :kv_override) do
      %{} = map -> Map.get(map, key)
      nil -> runtime_kv(key)
    end
  end

  defp runtime_kv(key) do
    if Code.ensure_loaded?(Nerves.Runtime.KV) do
      apply(Nerves.Runtime.KV, :get, [key])
    else
      nil
    end
  end
end

defmodule NervesOta.FirmwareMetadataTest do
  use ExUnit.Case, async: false
  doctest NervesOta.MixProject
  alias NervesOta.FirmwareMetadata

  setup do
    Application.put_env(:nerves_ota, :kv_override, %{
      "nerves_fw_active" => "b",
      "b.nerves_fw_uuid" => "uuid-b",
      "b.nerves_fw_version" => "1.2.3",
      "nerves_fw_validated" => "0"
    })

    on_exit(fn -> Application.delete_env(:nerves_ota, :kv_override) end)
  end

  describe "core functionality" do
    test "reports active partition, uuid, version" do
      assert FirmwareMetadata.active_partition() == "b"
      assert FirmwareMetadata.active_uuid() == "uuid-b"
      assert Version.compare(FirmwareMetadata.active_version(), Version.parse!("1.2.3")) == :eq
    end

    test "reports validated? correctly" do
      refute FirmwareMetadata.validated?()
    end
  end
end
```

### `test/nerves_ota_test.exs`

```elixir
defmodule NervesOta.UpdatePolicyTest do
  use ExUnit.Case, async: true
  doctest NervesOta.MixProject
  alias NervesOta.UpdatePolicy

  @healthy %{cpu_load: 0.2, disk_free_mb: 500, errors_last_hour: 0}
  @unhealthy %{cpu_load: 0.95, disk_free_mb: 10, errors_last_hour: 200}
  @update %{firmware_meta: %{uuid: "uuid-1", version: "1.2.3", description: ""}}

  describe "decide/4" do
    test "rejects when unhealthy" do
      now = ~U[2026-04-12 02:00:00Z]
      assert {:ignore, _} = UpdatePolicy.decide(@update, @unhealthy, now)
    end

    test "applies inside maintenance window" do
      now = ~U[2026-04-12 02:30:00Z]
      assert :apply = UpdatePolicy.decide(@update, @healthy, now, window: {1, 4})
    end

    test "reschedules outside window" do
      now = ~U[2026-04-12 10:00:00Z]
      assert {:reschedule, ms} = UpdatePolicy.decide(@update, @healthy, now, window: {1, 4})
      assert ms > 0 and ms < 24 * 3600 * 1000
    end

    test "[force] tag overrides window" do
      now = ~U[2026-04-12 10:00:00Z]
      update = put_in(@update, [:firmware_meta, :description], "[force] CVE fix")
      assert :apply = UpdatePolicy.decide(update, @healthy, now)
    end

    test "wraps midnight windows correctly" do
      now = ~U[2026-04-12 23:30:00Z]
      assert :apply = UpdatePolicy.decide(@update, @healthy, now, window: {22, 4})
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Nerves OTA Updates with NervesHub and Signed Firmware.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Nerves OTA Updates with NervesHub and Signed Firmware ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NervesOta.run(payload) do
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
        for _ <- 1..1_000, do: NervesOta.run(:bench)
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
