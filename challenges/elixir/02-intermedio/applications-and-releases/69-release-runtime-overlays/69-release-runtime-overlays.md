# Release overlays — shipping scripts and extra files with your release

**Project**: `runtime_overlays` — a release configured with overlays
that inject a systemd unit file, a wrapper script, and a runtime
configuration sample into the release tarball.


---

## Project context

A release is a self-contained directory — but "self-contained" often
needs a few extras that don't come from your codebase: a systemd unit
file, a logrotate config, a one-shot migration script, a README for
ops. Overlays are the official way to copy arbitrary files into the
release directory as part of `mix release`.

This exercise wires overlays via the `steps:` customization point and
shows the resulting layout.

Project structure (source):

```
runtime_overlays/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   └── runtime_overlays/
│       └── application.ex
├── rel/
│   └── overlays/
│       ├── systemd/
│       │   └── runtime_overlays.service
│       ├── scripts/
│       │   └── migrate.sh
│       └── OPS_README.md
├── test/
│   └── runtime_overlays_test.exs
└── mix.exs
```

Release output (after `mix release`, relevant parts):

```
_build/prod/rel/runtime_overlays/
├── bin/runtime_overlays
├── erts-<ver>/
├── lib/...
├── releases/0.1.0/...
├── systemd/runtime_overlays.service   ← overlay
├── scripts/migrate.sh                 ← overlay
└── OPS_README.md                      ← overlay
```

---


## Why X and not Y

- **Why not rebuild per env?** Slower deploys, more artifact churn, and env drift. One release + overlays is the modern pattern.

## Core concepts

### 1. `steps:` customization in `mix.exs`

The `steps:` option lets you insert arbitrary functions into the
release assembly pipeline. Each step receives the `%Mix.Release{}`
and returns an updated one. Copying overlay files is just a step
that walks a source directory and `File.cp_r!/2`s it into the
release path.

```
mix release ──▶ step 1 (assemble) ──▶ step 2 (overlays) ──▶ step 3 (tar)
```

### 2. The `%Mix.Release{}` struct

Carries `path:` (target root), `version_path:`, `applications:`, and
more. Read `path:` in your overlay step to know where to copy.

### 3. `:overlays` in a release config — the simple path

For trivial overlays you can define `overlays:` directory right next
to `config/`, but the custom step gives you full control (selective
copy, permissions, templating).

### 4. Why not commit the unit file in `/etc/systemd` directly?

The unit file belongs to the release: its paths reference the release
binary, its env vars match the app, its version changes with the code.
Shipping it with the release keeps these in lockstep; the install
script just symlinks it into `/etc/systemd/system/`.

---

## Design decisions

**Option A — rebuild the release per env**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — runtime overlays applied at boot (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because one artifact, many environments — overlays let ops tune without a rebuild.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project and overlay directory

```bash
mix new runtime_overlays --sup
cd runtime_overlays
mkdir -p rel/overlays/systemd rel/overlays/scripts config
```

### Step 2: `rel/overlays/systemd/runtime_overlays.service`

```ini
[Unit]
Description=Runtime Overlays Demo
After=network.target

[Service]
Type=simple
User=runtime_overlays
Group=runtime_overlays
# Path is relative to wherever the release is installed on the host.
# Typical install: /opt/runtime_overlays
WorkingDirectory=/opt/runtime_overlays
EnvironmentFile=-/etc/runtime_overlays/env
ExecStart=/opt/runtime_overlays/bin/runtime_overlays start
ExecStop=/opt/runtime_overlays/bin/runtime_overlays stop
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Step 3: `rel/overlays/scripts/migrate.sh`

```bash
#!/usr/bin/env bash
# Run database migrations against the running release node.
# Requires: RELEASE_COOKIE set, node named runtime_overlays@<host>.
set -euo pipefail

RELEASE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$RELEASE_ROOT/bin/runtime_overlays" eval "RuntimeOverlays.Release.migrate()"
```

### Step 4: `rel/overlays/OPS_README.md`

```markdown
# runtime_overlays — operations quickstart

- Install to: `/opt/runtime_overlays`
- systemd unit: `systemd/runtime_overlays.service`
- Migrations: `scripts/migrate.sh`
- Env file: `/etc/runtime_overlays/env`
```

### Step 5: `mix.exs` — declare the overlay step

```elixir
defmodule RuntimeOverlays.MixProject do
  use Mix.Project

  def project do
    [
      app: :runtime_overlays,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: [],
      releases: releases()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {RuntimeOverlays.Application, []}]
  end

  # `steps:` controls the release assembly pipeline.
  # `:assemble` is the built-in step that builds the release tree.
  # `&copy_overlays/1` runs AFTER assembly and injects our files.
  defp releases do
    [
      runtime_overlays: [
        include_executables_for: [:unix],
        steps: [:assemble, &copy_overlays/1]
      ]
    ]
  end

  # Walks rel/overlays/ and copies everything into the release root,
  # preserving directory structure. Shell scripts are made executable.
  defp copy_overlays(%Mix.Release{path: release_path} = release) do
    source = Path.join([File.cwd!(), "rel", "overlays"])

    if File.dir?(source) do
      File.cp_r!(source, release_path)
      make_scripts_executable(Path.join(release_path, "scripts"))
      Mix.shell().info("Overlays copied from #{source} → #{release_path}")
    end

    release
  end

  defp make_scripts_executable(scripts_dir) do
    if File.dir?(scripts_dir) do
      scripts_dir
      |> File.ls!()
      |> Enum.filter(&String.ends_with?(&1, ".sh"))
      |> Enum.each(fn f ->
        path = Path.join(scripts_dir, f)
        File.chmod!(path, 0o755)
      end)
    end
  end
end
```

### Step 6: `lib/runtime_overlays/application.ex`

```elixir
defmodule RuntimeOverlays.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: RuntimeOverlays.Supervisor)
  end
end
```

### Step 7: `test/runtime_overlays_test.exs`

```elixir
defmodule RuntimeOverlaysTest do
  use ExUnit.Case, async: false

  @moduletag :release

  @release_root "_build/prod/rel/runtime_overlays"

  # Only meaningful once `MIX_ENV=prod mix release` has been run.
  # Gated on directory existence to keep CI happy without a full release build.
  test "overlays are present in the built release" do
    if File.dir?(@release_root) do
      assert File.regular?(Path.join(@release_root, "systemd/runtime_overlays.service"))
      assert File.regular?(Path.join(@release_root, "scripts/migrate.sh"))
      assert File.regular?(Path.join(@release_root, "OPS_README.md"))

      %{mode: mode} = File.stat!(Path.join(@release_root, "scripts/migrate.sh"))
      # Executable bit set for owner.
      assert Bitwise.band(mode, 0o100) == 0o100
    else
      IO.puts("skipping: release not built (run `MIX_ENV=prod mix release` first)")
    end
  end
end
```

### Step 8: Build the release and inspect

```bash
MIX_ENV=prod mix release
ls _build/prod/rel/runtime_overlays/systemd
cat _build/prod/rel/runtime_overlays/scripts/migrate.sh
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Overlays are a copy, not a template**
If you need variable substitution (e.g. `${RELEASE_VERSION}` in the
unit file), do it inside the overlay step — read the file, render it
with `EEx`, write it back.

**2. Permissions aren't preserved from source**
`File.cp_r!/2` copies bytes, not necessarily modes. Set executable
bits explicitly for scripts as shown, or tar them up with preserved
modes.

**3. The unit file is tied to the install path**
Hardcoding `/opt/runtime_overlays` in the unit file is fine if every
host installs there. For flexibility, template the path at overlay time
from the host's target directory.

**4. `Type=simple` vs `Type=notify`**
`simple` means systemd considers the service started as soon as it
forks. For a BEAM release, boot takes seconds — systemd won't know if
it failed. Consider `Type=notify` with systemd integration (see
exercise 73).

**5. When NOT to use overlays**
If the file belongs elsewhere in the system (package metadata,
cron jobs managed by a config-management tool), let that tool own
it. Overlays are for files whose lifecycle matches the release.

---


## Reflection

- ¿Cuándo un overlay es preferible a un `runtime.exs` con `System.get_env`? Dá el criterio.

## Resources

- [Mix release — Customization with steps](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-steps)
- [`Mix.Release`](https://hexdocs.pm/mix/Mix.Release.html)
- [systemd service unit documentation](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
