# Release Overlays and Custom Assemble Steps

**Project**: `overlays_assemble` — a release that bundles migration SQL, a Swagger UI, and a node-bootstrap script into the tarball.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You ship a Phoenix-like Elixir service in a container. The Dockerfile today does something
awful: it runs `mix release`, then uses `COPY` to drop twelve additional files into the
image — SQL migrations, a static `swagger.json`, a `node_bootstrap.sh` that exports the
right `RELEASE_COOKIE` and `RELEASE_NODE` for Kubernetes, and a `licenses/` folder required
by the legal review. The Dockerfile has grown into a Rube Goldberg machine and every time
someone changes the directory layout inside the release, an environment breaks.

Releases already have a first-class mechanism for this: **overlays**. Overlays copy extra
files into the release tarball. They run after the default steps (`:assemble`) but before
`:tar`, so the resulting artifact is self-contained — the Dockerfile shrinks to a `COPY
--from=builder /app/_build/prod/rel/` and a single `ENTRYPOINT`. More importantly, you can
add a **custom release step** that runs real Elixir code between `:assemble` and `:tar`:
stamp a git SHA into `VERSION`, generate a Dialyzer PLT, pull secrets from Vault at build
time (don't), or pre-compile protocols.

Your job: write a release whose steps list is
`[:assemble, &copy_migrations/1, &stamp_git_sha/1, :tar]` and whose `overlays` copies the
Swagger UI assets and the bootstrap script into `rel/` so they land at the top of the
tarball. You will then verify the contents with `tar -tzf` and run the release from the
unpacked tarball to confirm it still boots.

---

## Tree

```
overlays_assemble/
├── lib/
│   ├── overlays_assemble.ex
│   ├── overlays_assemble/application.ex
│   └── overlays_assemble/service.ex
├── priv/
│   └── swagger/
│       ├── index.html
│       └── openapi.json
├── rel/
│   ├── overlays/
│   │   ├── bin/
│   │   │   └── node_bootstrap.sh
│   │   └── licenses/
│   │       └── THIRD_PARTY.md
│   └── migrations/
│       └── 20260101000000_init.sql
├── test/
│   ├── overlays_assemble_test.exs
│   └── release_layout_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The release pipeline

```
   mix release
       │
       ▼
   ┌──────────┐   ┌──────────────┐   ┌──────┐
   │ :assemble│──▶│ your step(s) │──▶│ :tar │
   └──────────┘   └──────────────┘   └──────┘
       │                                 │
       │                                 ▼
       │                        overlays_assemble-0.1.0.tar.gz
       ▼
   _build/prod/rel/overlays_assemble/
       ├── bin/
       ├── erts-X.Y/
       ├── lib/
       └── releases/
```

Each step is either a built-in atom (`:assemble`, `:tar`) or a 1-arity function receiving
the `%Mix.Release{}` struct and returning it (possibly modified). Steps run in order.

### 2. Overlays vs custom steps

| Mechanism | When to use | Pros | Cons |
|-----------|-------------|------|------|
| `overlays: ["rel/overlays"]` | Static files known at build time | Zero code, declarative | Cannot compute content |
| Custom step function | Generated content (git SHA, PLT, version) | Full power of Elixir | Runs on every release build |
| External `COPY` in Dockerfile | Truly external files | Decoupled from Mix | Drifts from release layout |

Prefer overlays for static assets. Drop into a custom step for anything computed.

### 3. The `%Mix.Release{}` struct

Steps receive a struct exposing:

- `path` — absolute path to the unpacked release directory (`_build/.../rel/<app>`)
- `version` — release version string
- `name` — release name as atom
- `applications` — map of resolved applications
- `erts_source`, `erts_version`
- `steps` — remaining steps after this one

Your step mutates filesystem inside `release.path` and returns the struct unchanged
(or you may add runtime config via the struct's `boot_scripts` / `config_providers` fields
for more advanced scenarios).

### 4. Overlay precedence

Files in overlays **overwrite** files produced by `:assemble`. This is a double-edged
sword: useful when patching a vendored asset, dangerous when a casual rename at the root
of the repo silently replaces `bin/overlays_assemble` with your shell script. Treat
`rel/overlays/` as production surface area; review every file.

### 5. Relative paths at runtime

Inside the running release, your app sees its files via `:code.priv_dir/1` for files in
`priv/`. Files dropped by overlays into `bin/` or `licenses/` live at `Application.app_dir(:overlays_assemble, "..")` or simply relative to `System.get_env("RELEASE_ROOT")`.
The boot script exports `RELEASE_ROOT` before the VM starts.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule OverlaysAssemble.MixProject do
  use Mix.Project

  def project do
    [
      app: :overlays_assemble,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {OverlaysAssemble.Application, []}
    ]
  end

  defp deps, do: []

  defp releases do
    [
      overlays_assemble: [
        include_executables_for: [:unix],
        overlays: ["rel/overlays"],
        steps: [:assemble, &copy_migrations/1, &stamp_git_sha/1, :tar]
      ]
    ]
  end

  @doc false
  def copy_migrations(%Mix.Release{path: dest} = release) do
    src = Path.expand("rel/migrations", File.cwd!())
    target = Path.join(dest, "migrations")
    File.mkdir_p!(target)
    File.cp_r!(src, target)
    Mix.shell().info("[overlays_assemble] copied migrations -> #{target}")
    release
  end

  @doc false
  def stamp_git_sha(%Mix.Release{path: dest, version: version} = release) do
    sha =
      case System.cmd("git", ["rev-parse", "--short", "HEAD"], stderr_to_stdout: true) do
        {out, 0} -> String.trim(out)
        _ -> "unknown"
      end

    content = "version=#{version}\ngit_sha=#{sha}\nbuilt_at=#{DateTime.utc_now()}\n"
    File.write!(Path.join(dest, "VERSION"), content)
    Mix.shell().info("[overlays_assemble] stamped VERSION with sha=#{sha}")
    release
  end
end
```

### Step 2: `lib/overlays_assemble/application.ex`

```elixir
defmodule OverlaysAssemble.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [OverlaysAssemble.Service]
    Supervisor.start_link(children, strategy: :one_for_one, name: OverlaysAssemble.Supervisor)
  end
end
```

### Step 3: `lib/overlays_assemble/service.ex`

```elixir
defmodule OverlaysAssemble.Service do
  @moduledoc """
  Exposes runtime lookups into the release layout: VERSION, Swagger asset path,
  migrations dir. Real services would serve these over HTTP.
  """
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec release_root() :: String.t()
  def release_root do
    case System.get_env("RELEASE_ROOT") do
      nil -> Path.expand("..", :code.priv_dir(:overlays_assemble) |> to_string())
      val -> val
    end
  end

  @spec version_info() :: %{version: String.t(), git_sha: String.t(), built_at: String.t()}
  def version_info do
    path = Path.join(release_root(), "VERSION")

    case File.read(path) do
      {:ok, content} -> parse_version(content)
      {:error, _} -> %{version: "dev", git_sha: "dev", built_at: "dev"}
    end
  end

  @spec swagger_path() :: String.t()
  def swagger_path, do: Path.join(:code.priv_dir(:overlays_assemble), "swagger/index.html")

  @spec migrations_dir() :: String.t()
  def migrations_dir, do: Path.join(release_root(), "migrations")

  @impl true
  def init(_), do: {:ok, %{}}

  defp parse_version(content) do
    content
    |> String.split("\n", trim: true)
    |> Enum.map(&String.split(&1, "=", parts: 2))
    |> Enum.filter(&match?([_, _], &1))
    |> Map.new(fn [k, v] -> {String.to_atom(k), v} end)
    |> Map.merge(%{version: "unknown", git_sha: "unknown", built_at: "unknown"}, fn _, new, _ -> new end)
  end
end
```

### Step 4: `rel/overlays/bin/node_bootstrap.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

: "${RELEASE_COOKIE:?RELEASE_COOKIE must be set}"
: "${RELEASE_NODE:?RELEASE_NODE must be set (e.g. app@10.0.0.17)}"

exec "$RELEASE_ROOT/bin/overlays_assemble" "$@"
```

### Step 5: `rel/overlays/licenses/THIRD_PARTY.md`

```markdown
# Third-party licenses bundled with overlays_assemble

- Elixir: Apache 2.0
- Erlang/OTP: Apache 2.0
```

### Step 6: `rel/migrations/20260101000000_init.sql`

```sql
-- Version-controlled schema migration.
CREATE TABLE IF NOT EXISTS health (id SERIAL PRIMARY KEY, ts TIMESTAMPTZ DEFAULT NOW());
```

### Step 7: `priv/swagger/index.html`

```html
<!doctype html>
<html><head><title>API Docs</title></head>
<body><div id="swagger-ui"></div></body></html>
```

### Step 8: `priv/swagger/openapi.json`

```json
{"openapi": "3.0.3", "info": {"title": "overlays_assemble", "version": "0.1.0"}, "paths": {}}
```

### Step 9: `test/release_layout_test.exs`

```elixir
defmodule OverlaysAssemble.ReleaseLayoutTest do
  use ExUnit.Case, async: true

  @moduletag :release

  setup do
    release_dir = Path.expand("../_build/prod/rel/overlays_assemble", __DIR__)

    unless File.dir?(release_dir) do
      {:skip, "run `MIX_ENV=prod mix release` first"}
    else
      {:ok, release_dir: release_dir}
    end
  end

  test "VERSION is stamped with version + git sha", %{release_dir: dir} do
    content = File.read!(Path.join(dir, "VERSION"))
    assert content =~ "version=0.1.0"
    assert content =~ "git_sha="
  end

  test "migrations are copied by the custom step", %{release_dir: dir} do
    assert File.exists?(Path.join(dir, "migrations/20260101000000_init.sql"))
  end

  test "bootstrap script is present and executable via overlay", %{release_dir: dir} do
    path = Path.join(dir, "bin/node_bootstrap.sh")
    assert File.exists?(path)
  end

  test "licenses overlay is at release root", %{release_dir: dir} do
    assert File.exists?(Path.join(dir, "licenses/THIRD_PARTY.md"))
  end

  test "swagger priv asset survives :assemble", %{release_dir: dir} do
    glob = Path.wildcard(Path.join(dir, "lib/overlays_assemble-*/priv/swagger/index.html"))
    assert glob != []
  end
end
```

### Step 10: `test/overlays_assemble_test.exs`

```elixir
defmodule OverlaysAssembleTest do
  use ExUnit.Case, async: true

  test "version_info returns dev defaults when VERSION is absent" do
    info = OverlaysAssemble.Service.version_info()
    assert %{version: _, git_sha: _, built_at: _} = info
  end

  test "swagger_path points inside priv" do
    path = OverlaysAssemble.Service.swagger_path()
    assert path =~ "swagger/index.html"
  end
end
```

### Step 11: Build and inspect

```bash
MIX_ENV=prod mix release
ls _build/prod/rel/overlays_assemble
tar -tzf _build/prod/overlays_assemble-0.1.0.tar.gz | head -40
_build/prod/rel/overlays_assemble/bin/overlays_assemble eval "IO.inspect(OverlaysAssemble.Service.version_info())"
```

Expected output of the `eval`: a map with `version: "0.1.0"`, `git_sha: "<7 hex>"`,
`built_at: "<ISO-8601>"`.

---

## Trade-offs and production gotchas

**1. Custom steps run on every build.** A step that does `git rev-parse` is cheap. A step
that does `mix dialyzer` or `mix test` is not. Keep release steps fast; heavy artifacts
belong in a pre-release CI stage that feeds files into `rel/overlays/`.

**2. Overlays overwrite assemble output.** A file in `rel/overlays/bin/overlays_assemble`
will replace the generated start script. This is useful for stripping ERTS debug symbols
by shipping a patched binary; dangerous when someone renames a repo directory and doesn't
notice the shadowing. Add a CI check: `test 1 = $(find rel/overlays -name overlays_assemble | wc -l)` failing if that file appears.

**3. `include_erts: false` and rebar3 releases.** For multi-arch targets, you sometimes
ship a release without ERTS and depend on the host's. Overlays still work; custom steps
that compile native code will fail on cross-compile. Gate with `System.otp_release()` or
build inside the target Docker image.

**4. Overlays and `runtime.exs`.** `runtime.exs` is evaluated at boot with `RELEASE_ROOT`
exported. If you put a `runtime.exs` in an overlay to override an assemble-generated one,
be aware Mix may regenerate it on every `:assemble`. Prefer editing `config/runtime.exs`
in the source tree.

**5. Permission bits.** `File.cp_r!` in a custom step preserves mode bits. `overlays` does
too. A shell script checked in without `+x` will fail at runtime with an opaque error.
Set permissions in source and verify via `ls -la rel/overlays/bin/`.

**6. Secrets at build time.** Do not `System.cmd("vault", ["read", ...])` inside a step.
That bakes secrets into the image layer. Use `runtime.exs` + a secret-mount or a
config provider.

**7. Step errors and cleanup.** If your step raises after `:assemble` wrote files, the
release directory is left in a partial state. Subsequent `mix release` may refuse to
proceed. Run `mix release --overwrite` or `rm -rf _build/prod/rel` to reset.

**8. When NOT to use this.** If your deployment target is not a tarball — you deploy
with `mix phx.digest` + direct scp + `iex -S mix`, or run on Nerves — release overlays do
not apply. For the Nerves case, use firmware overlays. For everything else that ships as
a tarball or container layer, overlays are the idiomatic tool.

---

## Performance notes

Measure `MIX_ENV=prod mix release` time before and after adding overlays. Empirically,
copying a few MB of static assets adds < 200ms. A custom step doing
`mix protocols.consolidate` can add 10–30s; hoist it into the build image instead.

To benchmark, use `time mix release` and compare against a bare release without steps.
If build time is dominated by `:assemble` itself (typical), your custom steps are not
the bottleneck.

---

## Resources

- [`mix release` — Steps and overlays](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-steps)
- [`Mix.Release` struct](https://hexdocs.pm/mix/Mix.Release.html)
- [Phoenix deployment guide](https://hexdocs.pm/phoenix/deployment.html)
- [Distillery → Mix.Release migration notes](https://hexdocs.pm/distillery/) (historic context)
- [Dashbit — Elixir v1.9 releases](https://dashbit.co/blog/elixir-v1-9-released) — José Valim
- [Plataformatec — Releases deep dive](https://blog.plataformatec.com.br/) archive
