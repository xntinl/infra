# Nerves OTA Updates with NervesHub and Signed Firmware

**Project**: `nerves_ota` — firmware delivery pipeline for a fleet of Nerves devices with cryptographically signed OTA updates distributed via NervesHub.

---

## Project context

You maintain a fleet of ~2,000 Raspberry Pi CM4 gateways deployed across industrial sites — cold-storage warehouses, mining installations, wind-farm telemetry hubs. Physical access to devices is expensive (a field technician visit is $300–$800 all-in). Shipping firmware updates through the network is not optional: it is the product.

The fleet currently runs Nerves 1.10 with `:nerves_hub_link` but the CI/CD story is manual — someone builds a firmware locally, uploads `.fw` files through the NervesHub web UI, and waits. You were hired to productionize this: every merge to `main` produces a signed firmware, publishes it to a staging channel, and a gated workflow promotes it to production groups. Rollback must be automatic on boot failure (A/B partitioning is the BEAM's friend here, not the enemy).

Security is non-negotiable. Firmware is signed with an offline-generated `fwup` key pair; only signed `.fw` files pass verification on the device. NervesHub authenticates devices via client X.509 certificates issued at provisioning time, and publishes updates over TLS 1.3 only. You never ship an unsigned firmware, even to dev. You never commit the signing key.

The exercise reproduces the interesting parts at miniature scale: key generation, a signed build, deployment groups, delta updates (bsdiff — NervesHub can produce delta patches that are 10–100× smaller than full firmware), and the reconnect-on-update lifecycle handled by `NervesHub.Client` behaviours. You will not run actual hardware — the target is `host` plus a mocked NervesHub HTTP endpoint for integration tests.

---

```
nerves_ota/
├── config/
│   ├── config.exs
│   ├── target.exs
│   └── host.exs
├── lib/
│   └── nerves_ota/
│       ├── application.ex
│       ├── nerves_hub_client.ex      # NervesHub.Client behaviour impl
│       ├── update_policy.ex          # decide apply/defer/reject
│       ├── health.ex                 # post-update health probe
│       └── firmware_metadata.ex      # read :nerves_fw_active, UUIDs
├── rel/
│   └── overlays/
│       └── etc/
│           └── iex.exs
├── test/
│   ├── nerves_ota/
│   │   ├── nerves_hub_client_test.exs
│   │   ├── update_policy_test.exs
│   │   └── firmware_metadata_test.exs
│   └── test_helper.exs
├── .github/workflows/
│   └── firmware.yml
├── mix.exs
└── README.md
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. The `fwup` signing chain

`fwup` is the low-level Erlang-Solutions tool that builds and applies Nerves firmware files. Signing uses Ed25519 by default (fast, small signatures, no parameter choice bikeshedding). You generate a key pair once:

```
fwup -g                 # produces fwup-key.pub and fwup-key.priv
```

The private key stays offline — ideally on a hardware token or at least in a sealed secret manager (HashiCorp Vault, AWS KMS, 1Password CLI). The public key is baked into every device image at build time via the `fwup.conf` resources list. On update, `fwup apply -V` verifies the signature before touching any partition.

If you lose the private key, you cannot update your fleet. If you leak the public key, nothing bad happens (it is by design public). If you leak the private key, attackers can ship arbitrary firmware to your devices — rotate all devices via an on-site reflash. Budget for this: it has happened at real companies.

```
+---------+   sign    +-----+   upload    +-----------+   pull   +--------+
| builder |---------->| .fw |------------>| NervesHub |--------->| device |
+---------+  offline  +-----+   HTTPS     +-----------+  HTTPS   +--------+
    ^          key                             |                     |
    |                                          |                     |
    +--------- push commits --------- GitHub --+                     |
                                                                     v
                                                            fwup apply -V
                                                            (verifies sig)
```

### 2. A/B partitions and automatic rollback

Nerves boots from one of two rootfs partitions labelled `a` and `b`. The bootloader (U-Boot on most targets) holds a `nerves_fw_active` variable and a `nerves_fw_validated` counter. The lifecycle:

1. Device runs partition `a` validated.
2. `fwup apply` writes the new firmware to partition `b`. `nerves_fw_active` flips to `b`, `nerves_fw_validated=0`.
3. Device reboots into `b`.
4. Your app runs a `Health.validate/0` check. If it passes, it calls `Nerves.Runtime.validate_firmware/0` which sets `nerves_fw_validated=1`.
5. If the device reboots again before validation (e.g. crash loop, watchdog reset), the bootloader sees `nerves_fw_validated=0` and flips back to `a`.

This gives you free automatic rollback — no cloud logic required. The only thing you must do is *never* call `validate_firmware/0` until the system is actually healthy. A common mistake is to call it from `application.ex`'s `start/2`: that defeats the point, because the app always starts before it crashes.

### 3. Deployment groups and phased rollouts

NervesHub groups devices by `tags` (free-form strings set at provisioning). A *deployment* binds a firmware to `(product, tag, version_condition)` and a rollout percentage. A typical pipeline:

| Stage        | Tag           | Rollout | Promotion criteria                               |
|--------------|---------------|---------|--------------------------------------------------|
| `canary`     | `canary`      | 100%    | Manual — 3 internal devices                      |
| `beta`       | `beta`        | 100%    | 24h in canary with zero rollbacks                |
| `prod-early` | `prod`        | 10%     | 72h in beta, error budget respected              |
| `prod`       | `prod`        | 100%    | 24h at 10% with no SLO regression                |

The device polls NervesHub over a persistent WebSocket (Phoenix Channels under the hood). When a new firmware is available, NervesHub sends an `update` message with the firmware URL and metadata. Your `NervesHub.Client` callback decides whether to apply it now, defer, or reject.

### 4. Delta updates with `bsdiff`

For a fleet on metered cellular links (LTE-M, NB-IoT), a 40 MB firmware is painful — at ~50 kbps that is ~2 hours per device and real money per update. NervesHub can produce delta patches: given firmware `v1.2.3` (source) and `v1.2.4` (target), it uses `bsdiff` to produce a patch that is typically 1–5 MB. The device downloads the patch and reconstructs the new firmware locally.

Delta updates require the source firmware UUID to be known to NervesHub, so you cannot skip versions. Your deployment policy must enforce sequential upgrades through intermediate versions — or fall back to full firmware if the device is several versions behind.

### 5. The `NervesHub.Client` behaviour

This is the seam where policy lives:

```elixir
@callback update_available(NervesHubLink.Message.UpdateInfo.t()) ::
            :apply | {:reschedule, milliseconds :: pos_integer()} | :ignore

@callback handle_fwup_message(fwup_message :: term()) :: :ok
@callback handle_error(error :: term()) :: :ok
```

`update_available/1` is called whenever NervesHub announces a new firmware. Returning `:apply` starts the download and `fwup apply` dance. `{:reschedule, ms}` defers for that interval (useful: don't update a point-of-sale terminal during lunch rush). `:ignore` rejects outright (useful: don't update a device that is currently running a diagnostic).

### 6. Provisioning and certificate rotation

Each device is provisioned with:
- A NervesHub device certificate (mTLS client cert) — identifies the device
- A product certificate CA pinned in firmware — authenticates NervesHub to the device
- The firmware signing public key — verifies firmware authenticity

Device certificates expire. In a 7-year fleet lifetime, you will rotate them at least twice. Design for rotation at day zero: the device must be able to accept a new certificate delivered through an update, and must not lock itself out if rotation fails (keep the old cert valid until the new one is confirmed working).

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Project skeleton

```bash
mix nerves.new nerves_ota --target rpi4
cd nerves_ota
mix deps.get
```

### Step 2: `mix.exs` — dependencies

```elixir
defmodule NervesOta.MixProject do
  use Mix.Project

  @app :nerves_ota
  @version "0.1.0"
  @all_targets [:rpi4, :rpi3a, :bbb]

  def project do
    [
      app: @app,
      version: @version,
      elixir: "~> 1.16",
      archives: [nerves_bootstrap: "~> 1.13"],
      start_permanent: Mix.env() == :prod,
      build_embedded: true,
      aliases: [loadconfig: [&bootstrap/1]],
      deps: deps(),
      releases: [{@app, release()}]
    ]
  end

  def application do
    [mod: {NervesOta.Application, []}, extra_applications: [:logger, :runtime_tools]]
  end

  defp deps do
    [
      {:nerves, "~> 1.10", runtime: false},
      {:shoehorn, "~> 0.9"},
      {:ring_logger, "~> 0.11"},
      {:toolshed, "~> 0.3"},
      {:nerves_hub_link, "~> 2.5"},
      {:nerves_runtime, "~> 0.13"},
      {:nerves_time, "~> 0.4"},
      {:mox, "~> 1.1", only: :test},
      {:nerves_system_rpi4, "~> 1.24", runtime: false, targets: :rpi4}
    ]
  end

  defp release do
    [overwrite: true, cookie: "#{@app}_cookie", include_erts: &Nerves.Release.erts/0,
     steps: [&Nerves.Release.init/1, :assemble], strip_beams: Mix.env() == :prod]
  end

  defp bootstrap(args) do
    set_target()
    Application.start(:nerves_bootstrap)
    Mix.Task.run("loadconfig", args)
  end

  defp set_target, do: System.put_env("MIX_TARGET", System.get_env("MIX_TARGET") || "host")
end
```

### Step 3: `config/target.exs` — NervesHub client

```elixir
import Config

config :nerves_hub_link,
  client: NervesOta.NervesHubClient,
  fwup_public_keys: [:devkey],
  connect_wait_for_network: true,
  socket: [
    heartbeat_interval_ms: 45_000,
    reconnect_after_msec: [1_000, 2_000, 5_000, 10_000, 30_000]
  ],
  ssl: [
    cacertfile: Application.app_dir(:nerves_hub_link, "priv/nerveshub-ca.pem"),
    server_name_indication: ~c"device.nerves-hub.org"
  ]

config :nerves, :firmware, fwup_conf: "config/fwup.conf"
```

### Step 4: `lib/nerves_ota/application.ex`

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

  def target, do: Application.get_env(:nerves_ota, :target, :host)
end
```

### Step 5: `lib/nerves_ota/nerves_hub_client.ex`

```elixir
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

  @impl true
  def handle_fwup_message({:progress, percent}) do
    if rem(percent, 10) == 0, do: Logger.info("fwup progress #{percent}%")
    :ok
  end

  def handle_fwup_message({:ok, 0, _msg}) do
    Logger.info("fwup completed — rebooting")
    :ok
  end

  def handle_fwup_message({:warning, _code, msg}) do
    Logger.warning("fwup warning: #{msg}")
    :ok
  end

  def handle_fwup_message({:error, code, msg}) do
    Logger.error("fwup error #{code}: #{msg}")
    :ok
  end

  def handle_fwup_message(other) do
    Logger.debug("fwup: #{inspect(other)}")
    :ok
  end

  @impl true
  def handle_error(error) do
    Logger.error("NervesHub error: #{inspect(error)}")
    :ok
  end
end
```

### Step 6: `lib/nerves_ota/update_policy.ex`

```elixir
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
```

### Step 7: `lib/nerves_ota/health.ex`

```elixir
defmodule NervesOta.Health do
  @moduledoc "Post-update health probe. Validates firmware only when green."
  use GenServer
  require Logger

  @probe_interval_ms 10_000
  @required_green_cycles 3

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
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
    _ -> 1_000
  end
end
```

### Step 8: `lib/nerves_ota/firmware_metadata.ex`

```elixir
defmodule NervesOta.FirmwareMetadata do
  @moduledoc "Thin wrapper around `Nerves.Runtime.KV` and error counting."

  @spec active_partition() :: String.t()
  def active_partition do
    kv("nerves_fw_active") || "a"
  end

  @spec active_uuid() :: String.t() | nil
  def active_uuid, do: kv("#{active_partition()}.nerves_fw_uuid")

  @spec active_version() :: Version.t() | nil
  def active_version do
    case kv("#{active_partition()}.nerves_fw_version") do
      nil -> nil
      v -> Version.parse!(v)
    end
  end

  @spec validated?() :: boolean()
  def validated?, do: kv("nerves_fw_validated") == "1"

  @spec validate_firmware() :: :ok | {:error, term()}
  def validate_firmware do
    if Code.ensure_loaded?(Nerves.Runtime) do
      apply(Nerves.Runtime, :validate_firmware, [])
    else
      :ok
    end
  end

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
```

### Step 9: Tests

```elixir
# test/nerves_ota/update_policy_test.exs
defmodule NervesOta.UpdatePolicyTest do
  use ExUnit.Case, async: true
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

```elixir
# test/nerves_ota/firmware_metadata_test.exs
defmodule NervesOta.FirmwareMetadataTest do
  use ExUnit.Case, async: false
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

  test "reports active partition, uuid, version" do
    assert FirmwareMetadata.active_partition() == "b"
    assert FirmwareMetadata.active_uuid() == "uuid-b"
    assert Version.compare(FirmwareMetadata.active_version(), Version.parse!("1.2.3")) == :eq
  end

  test "reports validated? correctly" do
    refute FirmwareMetadata.validated?()
  end
end
```

### Step 10: CI — `.github/workflows/firmware.yml`

```yaml
name: firmware
on:
  push:
    branches: [main]
    tags: ['v*']
jobs:
  build:
    runs-on: ubuntu-22.04
    strategy:
      matrix: { target: [rpi4, rpi3a] }
    steps:
      - uses: actions/checkout@v4
      - uses: erlef/setup-beam@v1
        with: { otp-version: '26.2', elixir-version: '1.16' }
      - name: Install fwup
        run: |
          wget https://github.com/fwup-home/fwup/releases/download/v1.10.2/fwup_1.10.2_amd64.deb
          sudo dpkg -i fwup_1.10.2_amd64.deb
      - name: Import signing key
        env: { FWUP_PRIV_KEY: ${{ secrets.FWUP_PRIV_KEY }} }
        run: echo "$FWUP_PRIV_KEY" > /tmp/fwup-key.priv
      - name: Build firmware
        env: { MIX_TARGET: ${{ matrix.target }} }
        run: mix deps.get && mix firmware && mix firmware.sign --key /tmp/fwup-key.priv
      - name: Publish to NervesHub (canary)
        env: { NH_TOKEN: ${{ secrets.NERVES_HUB_TOKEN }} }
        run: mix nerves_hub.firmware publish --deployment canary
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Trade-offs and production gotchas

**1. Key management is the whole game.** The fwup private key is the crown jewel. Never check it into Git, never store it on the CI runner's persistent disk. Use GitHub encrypted secrets or (better) a short-lived signing service that the runner calls over mTLS. Rotate every 2–3 years — which requires a firmware update that installs both the old and new public keys. Plan this from day one.

**2. `validate_firmware/0` must be gated on a real probe.** If you call it unconditionally from `Application.start/2`, every firmware validates itself before proving it works, and the A/B rollback is useless. The probe must exercise the actual failure modes — crashes, disk-full, network partition, peripheral I/O, not just `:ok = GenServer.call(...)`.

**3. Clock drift breaks the maintenance window.** `nerves_time` (NTP) takes seconds to minutes to lock. Before that, `DateTime.utc_now/0` returns the build time or the RTC-saved time, which can be weeks old. Your policy should short-circuit with `{:reschedule, 30_000}` if `nerves_time` has not synced.

**4. Delta updates need monotonic versions.** `bsdiff` produces a patch against a specific source UUID. If the device is on v1.2.0 and you have a delta from v1.2.1→v1.2.2, the device cannot use it. NervesHub handles this by falling back to the full firmware, but you pay the full bandwidth cost. Tag your device cohorts and manage upgrade paths.

**5. Large fleets need rollout limiting on your side too.** If NervesHub announces an update to 2,000 devices simultaneously, all 2,000 start downloading. Your WAN egress, or NervesHub's CDN budget, may not appreciate this. Use `{:reschedule, :rand.uniform(3_600_000)}` to naturally spread download starts across an hour. Saša Jurić's "Designing for scalability" is required reading here.

**6. mTLS client cert expiry is a dead fleet.** Device certificates default to 1-year validity. If a device is offline past the expiry, NervesHub rejects it and you cannot push the renewal. Either use very long-lived certs (the Nerves default is 10 years) or rotate aggressively while devices are still connectable.

**7. When NOT to use this.** If you have < 10 devices or they all live in one rack, a `git pull && systemctl restart` over SSH is cheaper, faster, and less to operate. OTA with signing and A/B and NervesHub is overkill below ~50 devices or when you can physically touch them in a day. Also: if your firmware changes happen more than twice a day, you are not yet at the point where OTA discipline matters — make the changes cheap first.

**8. Rollback telemetry is how you find silent bugs.** A device that boots partition `b`, fails health, falls back to `a`, and quietly stays on `a` is invisible from NervesHub unless you emit telemetry on the `a`-after-failed-`b` transition. Wire `:telemetry` events for `[:nerves_ota, :rollback, :detected]` and alert when the rate exceeds a few percent of a rollout cohort.

---

## Performance notes

The interesting numbers are bandwidth and validation latency:

| Metric                                  | Typical value           |
|-----------------------------------------|-------------------------|
| Full firmware size (rpi4 minimal)       | 35–45 MB                |
| Delta patch size (minor Elixir change)  | 1–3 MB                  |
| Delta patch size (system deps bump)     | 15–25 MB                |
| `fwup apply` time (SD-card class 10)    | 20–40 s                 |
| Reboot + BEAM cold start                | 12–18 s                 |
| Total user-visible downtime             | 40–60 s                 |

Measure on your actual target. A raspberry pi 3B+ with a slow SD card can easily hit 90 s. If downtime matters, use `:nerves_runtime_shell` + graceful handoff + systemd `ExecStopPost` hooks so upstream load balancers drain the device first.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Nerves project — official docs](https://hexdocs.pm/nerves/) — A/B partition model, `Nerves.Runtime.KV`, validation flow.
- [NervesHub architecture](https://docs.nerves-hub.org/) — deployments, delta updates, device certificates.
- [`fwup` manual](https://github.com/fwup-home/fwup) — signing, verification, `.fw` format.
- [Frank Hunleth — "Nerves in production"](https://www.youtube.com/watch?v=PSLUcRxozfY) — Frank is the creator of Nerves and fwup; the constraints discussed here come from his talks.
- [Justin Schneck — "Smart Rockets with Nerves"](https://www.youtube.com/watch?v=oSE4Lc4LTyw) — co-creator of Nerves, covers OTA at fleet scale.
- [`nerves_hub_link` source](https://github.com/nerves-hub/nerves_hub_link) — reference implementation of the `Client` behaviour, connection supervisor, fwup GenServer.
- [Fred Hébert — "Reliable software" chapter on upgrades](https://ferd.ca/) — language-agnostic discussion of staged rollouts and rollback.
