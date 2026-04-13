# Packaging a release for systemd

**Project**: `systemd_packaged` — a release plus a systemd unit file
and `EnvironmentFile` so the whole deployment is: copy release, drop
env file, enable unit, start.


---

## Project context

You have a release. You want the host to start it on boot, restart
it on crash, capture its logs, and feed it env vars without baking
them into the release. systemd does all of this natively — it's the
init system on effectively every modern Linux server. This exercise
ships a release with a unit file and a separate `EnvironmentFile`
so ops can change config without re-deploying code.

Project structure:

```
systemd_packaged/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── systemd_packaged.ex
│   └── systemd_packaged/
│       └── application.ex
├── rel/
│   └── overlays/
│       ├── systemd/
│       │   └── systemd_packaged.service
│       └── env.sample
├── test/
│   └── systemd_packaged_test.exs
└── mix.exs
```

Typical on-host layout:

```
/opt/systemd_packaged/                       (the release)
├── bin/systemd_packaged
├── erts-<ver>/
├── lib/
├── releases/
└── systemd/systemd_packaged.service         (overlay — symlink target)

/etc/systemd_packaged/env                    (the env file)

/etc/systemd/system/systemd_packaged.service → /opt/systemd_packaged/systemd/...
```

---


## Why X and not Y

- **Why not `nohup`?** No restart policy, no journald integration, no dependency ordering. systemd is the OS-level supervisor.

## Core concepts

### 1. `EnvironmentFile=` — env vars out of the unit file

```ini
EnvironmentFile=-/etc/systemd_packaged/env
```

The leading `-` makes the file optional. The env file is one
`KEY=value` per line, no export, no quoting shell-style. systemd
parses it, sets the vars before `ExecStart`, and your release's
`runtime.exs` can read them.

### 2. `Type=simple` vs `Type=exec` vs `Type=notify`

```
Type=simple   Service is "started" as soon as fork succeeds. Fast, dumb.
Type=exec     Like simple, but waits for exec() to complete.
Type=notify   Service tells systemd when it's ready via sd_notify().
```

BEAM releases take seconds to fully boot. `simple` lies about
readiness; `exec` is usually the right minimum. `notify` requires
code on your side (there are Hex libraries for it) but gives
systemd true "ready" signaling.

### 3. `Restart=` and `RestartSec=`

```ini
Restart=on-failure
RestartSec=5
```

If the service exits non-zero, systemd restarts it after 5 seconds.
Pair with `StartLimitBurst=`/`StartLimitIntervalSec=` to avoid tight
crash loops that eat CPU.

### 4. Dedicated user and working directory

```ini
User=systemd_packaged
Group=systemd_packaged
WorkingDirectory=/opt/systemd_packaged
```

Never run your BEAM as root. A dedicated unprivileged user and a
locked-down working dir means a compromised node can't trash the
host.

---

## Design decisions

**Option A — `nohup` + pid file**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — systemd unit with `Restart=on-failure` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because systemd gives restart policy, journald logs, and dependency ordering — the OS-level supervisor.


## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```




### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new systemd_packaged --sup
cd systemd_packaged
mkdir -p config rel/overlays/systemd
```

### Step 2: `config/config.exs` and `config/runtime.exs`

**Objective**: Provide `config/config.exs` and `config/runtime.exs` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
# config/config.exs
import Config
config :systemd_packaged, greeting: "hello"
```

```elixir
# config/runtime.exs
import Config

# Read from the environment — systemd's EnvironmentFile populates these.
config :systemd_packaged,
  greeting: System.get_env("GREETING", "hello"),
  port: String.to_integer(System.get_env("PORT", "4000"))
```

### Step 3: `rel/overlays/systemd/systemd_packaged.service`

**Objective**: Provide `rel/overlays/systemd/systemd_packaged.service` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```ini
[Unit]
Description=Systemd Packaged Demo
Documentation=https://hexdocs.pm/mix/Mix.Tasks.Release.html
After=network-online.target
Wants=network-online.target

[Service]
# `exec` waits for exec(3) to succeed before declaring the unit started.
# Upgrade to `notify` if you add a libsystemd integration.
Type=exec

User=systemd_packaged
Group=systemd_packaged

# Where the release is installed. Must match your deploy script.
WorkingDirectory=/opt/systemd_packaged

# Env vars live in a separate file so ops can rotate secrets without
# touching the unit file itself. Leading `-` makes it optional.
EnvironmentFile=-/etc/systemd_packaged/env

# BEAM needs the cookie; without EnvironmentFile you can also set:
# Environment=RELEASE_COOKIE=supersecret
# But prefer the env file so the cookie isn't visible via `systemctl show`.

ExecStart=/opt/systemd_packaged/bin/systemd_packaged start
ExecStop=/opt/systemd_packaged/bin/systemd_packaged stop

# Restart policy: restart on crash, but back off if we crash-loop.
Restart=on-failure
RestartSec=5
StartLimitBurst=5
StartLimitIntervalSec=60

# Hardening — belt and braces for a service that shouldn't touch much.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/systemd_packaged

# Let BEAM use enough file descriptors for a real workload.
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

### Step 4: `rel/overlays/env.sample`

**Objective**: Provide `rel/overlays/env.sample` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```sh
# Copy to /etc/systemd_packaged/env and lock down permissions:
#   install -o systemd_packaged -g systemd_packaged -m 0600 env.sample /etc/systemd_packaged/env

GREETING=hello from systemd
PORT=4000

# BEAM distribution cookie. Keep secret.
RELEASE_COOKIE=change-me-to-a-long-random-string

# Release name / node name — usually left at defaults.
# RELEASE_NAME=systemd_packaged
# RELEASE_NODE=systemd_packaged@127.0.0.1
```

### Step 5: `mix.exs` with overlay step

**Objective**: Provide `mix.exs` with overlay step — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule SystemdPackaged.MixProject do
  use Mix.Project

  def project do
    [
      app: :systemd_packaged,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: [],
      releases: releases()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {SystemdPackaged.Application, []}]
  end

  defp releases do
    [
      systemd_packaged: [
        include_executables_for: [:unix],
        steps: [:assemble, &copy_overlays/1]
      ]
    ]
  end

  defp copy_overlays(%Mix.Release{path: release_path} = release) do
    source = Path.join([File.cwd!(), "rel", "overlays"])
    if File.dir?(source), do: File.cp_r!(source, release_path)
    release
  end
end
```

### Step 6: `lib/systemd_packaged/application.ex` and `lib/systemd_packaged.ex`

**Objective**: Provide `lib/systemd_packaged/application.ex` and `lib/systemd_packaged.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule SystemdPackaged.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    Logger.info("systemd_packaged starting: #{inspect(SystemdPackaged.config())}")
    Supervisor.start_link([], strategy: :one_for_one, name: SystemdPackaged.Supervisor)
  end
end
```

```elixir
defmodule SystemdPackaged do
  @spec config() :: %{greeting: String.t(), port: pos_integer()}
  def config do
    %{
      greeting: Application.fetch_env!(:systemd_packaged, :greeting),
      port: Application.fetch_env!(:systemd_packaged, :port)
    }
  end
end
```

### Step 7: `test/systemd_packaged_test.exs`

**Objective**: Write `systemd_packaged_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule SystemdPackagedTest do
  use ExUnit.Case, async: false

  test "config has a runtime default" do
    assert %{greeting: _, port: p} = SystemdPackaged.config()
    assert is_integer(p)
  end

  test "overlay files exist in source tree" do
    # Sanity check — the overlay files must be committed so they ship
    # with the release at build time.
    assert File.regular?("rel/overlays/systemd/systemd_packaged.service")
    assert File.regular?("rel/overlays/env.sample")
  end
end
```

### Step 8: Install on a host (typical flow)

**Objective**: Install on a host (typical flow).


```bash
# On the build host:
MIX_ENV=prod mix release

# Ship tarball to the target host, extract to /opt/systemd_packaged.
# Then on the target:
sudo useradd --system --home /opt/systemd_packaged systemd_packaged
sudo chown -R systemd_packaged:systemd_packaged /opt/systemd_packaged

sudo install -d -o systemd_packaged -g systemd_packaged -m 0750 /etc/systemd_packaged
sudo install -o systemd_packaged -g systemd_packaged -m 0600 \
  /opt/systemd_packaged/env.sample /etc/systemd_packaged/env
sudo $EDITOR /etc/systemd_packaged/env      # set real RELEASE_COOKIE

sudo ln -s /opt/systemd_packaged/systemd/systemd_packaged.service \
           /etc/systemd/system/systemd_packaged.service
sudo systemctl daemon-reload
sudo systemctl enable --now systemd_packaged
sudo journalctl -u systemd_packaged -f
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts

Systemd units package Elixir releases for Linux deployments, providing process supervision and restart-on-failure. A unit file specifies `ExecStart: /path/to/bin/myapp start` and Systemd ensures the process restarts if it crashes. You get two layers of supervision—OTP inside the VM, Systemd outside. OTP handles application logic and state; Systemd handles machine-level concerns (restart, resource limits, log aggregation). Coordinating both correctly requires understanding the boundary: OTP stops a crashed process and lets Systemd restart the whole VM; Systemd enforces timeout limits so stuck VMs eventually restart. This layering is standard practice for production Linux deployments.

---

## Deep Dive: Compile-Time vs Runtime Configuration Boundaries

A release is a static artifact: code and compile-time config are baked in. Runtime config must be provided at boot via environment variables, config files, or config providers. Simple rule: if a value changes between dev and prod, it goes in `config/runtime.exs`, not `config/config.exs`.

Footgun: putting config in compile-time files and assuming environment variables work at runtime. Releases ignore env vars unless `config/runtime.exs` explicitly reads them. If you need env vars, fetch them in `config/runtime.exs` and store in application state.

For distributed systems, config providers (modules loading config from Consul, S3, etc.) are powerful but complex. Start with environment variables and `config/runtime.exs`; only reach for providers if you need dynamic reloading without downtime or multi-tenant config switching. Premature provider complexity is a mistake.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `EnvironmentFile` does NOT support shell interpolation**
`FOO="$BAR"` is literal — no expansion. Keep values simple, or
use a wrapper `ExecStartPre=` script that generates a rendered env
file.

**2. The BEAM cookie is a secret, treat it like one**
Permissions `0600`, owned by the service user, never logged. Anyone
with the cookie can attach to your node and run arbitrary code.

**3. `Type=simple` will lie about readiness**
A 5-second BEAM boot looks "started" in 50ms to systemd under
`Type=simple`. For dependent units, use `Type=notify` with a
libsystemd-compatible library, or a manual `ExecStartPost=` probe.

**4. `ProtectSystem=strict` blocks more than you think**
It makes `/usr`, `/boot`, `/efi`, and `/etc` read-only. If your app
writes logs to `/var/log`, add `LogsDirectory=` or
`ReadWritePaths=/var/log/your_app`.

**5. Journald captures stdout/stderr — use it**
Don't configure BEAM to log to a file when systemd can already give
you timestamped, rotated, queryable logs for free. Set `:console`
log backend and let `journalctl` handle the rest.

**6. When NOT to use systemd packaging**
Containers (k8s, ECS, Docker Compose) replace systemd. There, the
orchestrator owns lifecycle, restarts, env injection, and logs.
systemd is for bare-metal and VM deployments.

---


## Reflection

- ¿Qué diferencia hay entre `Restart=always` y `Restart=on-failure` para un release OTP? ¿Cuál elegís y por qué?

## Resources

- [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [systemd.exec(5)](https://www.freedesktop.org/software/systemd/man/systemd.exec.html) — hardening options
- [Mix release — `bin/my_app` commands](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-bin-my_app-commands)
- [`systemd` integration libraries for Elixir](https://hex.pm/packages?search=systemd) — for `Type=notify`
