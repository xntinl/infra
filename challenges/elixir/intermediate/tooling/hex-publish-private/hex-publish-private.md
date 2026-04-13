# Publishing to Hex and private Hex organizations

**Project**: `private_hex_demo` — a library correctly configured for
`mix hex.publish`, demonstrating the split between public publishing and
private-organization publishing.

---

## Why hex publish private matters

Hex is Elixir's package registry. You can publish to:

- **Public Hex** (hex.pm) — free, world-readable. Most of the ecosystem.
- **Private organizations** — paid; `hexpm:your_org` repo. Internal libs
  inside a company are the usual use case.
- **Self-hosted Hex** — via `mini_repo` or a commercial plan. Rare.

The mechanics are the same in all three: `mix hex.publish` builds a tarball
from files declared in the `package/0` section of `mix.exs`, uploads it,
and triggers a docs build on hexdocs.pm (or your org's hexdocs).

This exercise prepares a library for publishing **without actually
publishing it** (we use `mix hex.build` to produce the tarball locally).
You'll see exactly what goes in, what's required, and how organizations
change the workflow.

---

## Project structure

```
private_hex_demo/
├── lib/
│   └── private_hex_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── private_hex_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The `package/0` keyword list — what Hex needs

```elixir
defp package do
  [
    name: "private_hex_demo",
    description: "One-line summary that shows on hex.pm.",
    licenses: ["Apache-2.0"],
    links: %{"GitHub" => "https://github.com/org/private_hex_demo"},
    files: ~w(lib mix.exs README.md LICENSE CHANGELOG.md .formatter.exs),
    maintainers: ["Your Name <you@example.com>"]
  ]
end
```

Required: `name` (inferred from app if omitted), `description`, `licenses`,
`links`. Everything else is conventional.

### 2. `files:` is a whitelist — default includes things you don't want

Without an explicit `files:` list, Hex packages everything in `lib/`, plus
a few common files (`mix.exs`, `README*`, `LICENSE*`, etc.). Declare
`files:` yourself to guarantee no accidental inclusion of `config/`,
`priv/secret.key`, or `.env`.

### 3. Public vs private organization

| Target           | Authentication                          | Publish command                            |
|------------------|------------------------------------------|--------------------------------------------|
| Public Hex       | `mix hex.user auth`                      | `mix hex.publish`                          |
| Private org      | `mix hex.user auth` then org keys        | `mix hex.publish --organization your_org`  |
| Self-hosted      | `mix hex.repo add my_repo <url> <key>`   | `mix hex.publish --repo my_repo`           |

An organization is just a namespace on hex.pm that requires auth to read.
Consumers add it with `mix hex.organization auth your_org` before running
`mix deps.get`.

### 4. Consuming from a private org

```elixir
{:private_hex_demo, "~> 0.1", organization: "your_org"}
```

Without the `organization:` flag, Hex looks only in the public repo and
gives you a confusing "no package found" error.

### 5. `mix hex.build` — dry-run before publishing

`mix hex.build` produces a local tarball and prints what's inside. Run it
BEFORE `hex.publish`, every single time. It's the only safeguard against
shipping secrets or unwanted files.

---

## Why Hex private orgs and not a Git dep

A Git dep (`{:lib, git: "git@github.com:org/lib.git", tag: "v0.1.0"}`)
works for sharing closed code, but it has three sharp edges: no immutable
versions (a tag can be force-moved), no resolved dep graph (your tag may
depend on Git tags of other libs recursively), and no docs hosting. Hex
private orgs give you immutable versions, proper resolution, and
hexdocs.pm in one step. Git deps are fine for one-off prototypes; once
more than one team consumes a lib, pay for the org.

---

## Design decisions

**Option A — Publish everything to public Hex**
- Pros: Free; zero auth ceremony; the widest reach.
- Cons: Impossible if the code is proprietary; leaks internal naming
  conventions and architecture; immutable forever.

**Option B — Private org on hex.pm** (chosen)
- Pros: Same workflow as public Hex (same `mix.exs` shape, same commands),
  immutable versions, hosted docs, proper dep resolution.
- Cons: Paid; every consumer (humans + CI) needs `mix hex.organization
  auth`; forgetting the `organization:` flag on a dep produces a confusing
  "not found" error.

→ Chose **B** because closed-source libraries shared across teams need
  the same guarantees as public Hex (immutability, resolution) without
  exposing the code.

---

## Implementation

### `mix.exs`

```elixir
defmodule PrivateHexDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :private_hex_demo,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new private_hex_demo
cd private_hex_demo
```

### Step 2: `mix.exs` — publish-ready configuration

**Objective**: Edit `mix.exs` — publish-ready configuration, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule PrivateHexDemo.MixProject do
  use Mix.Project

  @version "0.1.0"
  @source_url "https://github.com/example/private_hex_demo"

  def project do
    [
      app: :private_hex_demo,
      version: @version,
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),

      # --- Publishing metadata ---
      name: "PrivateHexDemo",
      description: "Demo library for learning `mix hex.publish`.",
      package: package(),
      source_url: @source_url,
      docs: docs()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end

  defp package do
    [
      # --- Required ---
      licenses: ["Apache-2.0"],
      links: %{"GitHub" => @source_url, "Changelog" => "#{@source_url}/blob/main/CHANGELOG.md"},

      # --- Whitelist: nothing outside this gets shipped ---
      files: ~w(lib mix.exs README.md LICENSE CHANGELOG.md .formatter.exs),

      # --- For private publishing, uncomment:
      # organization: "your_org",

      maintainers: ["Your Name <you@example.com>"]
    ]
  end

  defp docs do
    [
      main: "readme",
      extras: ["README.md", "CHANGELOG.md"],
      source_ref: "v#{@version}",
      source_url: @source_url
    ]
  end
end
```

### `lib/private_hex_demo.ex`

**Objective**: Implement `private_hex_demo.ex` — code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule PrivateHexDemo do
  @moduledoc """
  A trivial library used to demonstrate Hex publishing. Nothing to see here —
  the interesting content is in `mix.exs`.
  """

  @doc """
  Returns the current library version.

  ## Examples

      iex> PrivateHexDemo.version()
      "0.1.0"
  """
  @spec version() :: String.t()
  def version, do: "0.1.0"
end
```

### Step 4: The accompanying files

**Objective**: Provide The accompanying files — these are the supporting fixtures the main module depends on to make its concept demonstrable.

`README.md`:

```markdown
# PrivateHexDemo

Demo library for learning `mix hex.publish`.

### `script/main.exs`

```elixir
defmodule Main do
  defmodule PrivateHexDemo.MixProject do
    use Mix.Project

    @version "0.1.0"
    @source_url "https://github.com/example/private_hex_demo"

    def project do
      [
        app: :private_hex_demo,
        version: @version,
        elixir: "~> 1.19",
        start_permanent: Mix.env() == :prod,
        deps: deps(),

        # --- Publishing metadata ---
        name: "PrivateHexDemo",
        description: "Demo library for learning `mix hex.publish`.",
        package: package(),
        source_url: @source_url,
        docs: docs()
      ]
    end

    def application, do: [extra_applications: [:logger]]

    defp deps do
      [
        {:ex_doc, "~> 0.31", only: :dev, runtime: false}
      ]
    end

    defp package do
      [
        # --- Required ---
        licenses: ["Apache-2.0"],
        links: %{"GitHub" => @source_url, "Changelog" => "#{@source_url}/blob/main/CHANGELOG.md"},

        # --- Whitelist: nothing outside this gets shipped ---
        files: ~w(lib mix.exs README.md LICENSE CHANGELOG.md .formatter.exs),

        # --- For private publishing, uncomment:
        # organization: "your_org",

        maintainers: ["Your Name <you@example.com>"]
      ]
    end

    defp docs do
      [
        main: "readme",
        extras: ["README.md", "CHANGELOG.md"],
        source_ref: "v#{@version}",
        source_url: @source_url
      ]
    end
  end

  def main do
    IO.puts("=== Package Demo ===
  ")
  
    # Demo: Hex package publishing
  IO.puts("1. mix hex.publish - publish to hex.pm")
  IO.puts("2. Private Hex for internal packages")
  IO.puts("3. Semver versioning")

  IO.puts("
  ✓ Hex publishing demo completed!")
  end

end

Main.main()
```

## Installation

For the private org variant:

```elixir
{:private_hex_demo, "~> 0.1", organization: "your_org"}
```
```

`CHANGELOG.md`:

```markdown
# Changelog

## v0.1.0

- Initial release.
```

`LICENSE` — paste the Apache-2.0 text, or whichever license you chose.

### Step 5: `test/private_hex_demo_test.exs`

**Objective**: Write `private_hex_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule PrivateHexDemoTest do
  use ExUnit.Case, async: true

  doctest PrivateHexDemo

  describe "core functionality" do
    test "version/0 is a non-empty string" do
      assert is_binary(PrivateHexDemo.version())
      assert byte_size(PrivateHexDemo.version()) > 0
    end
  end
end
```

### Step 6: Dry run and (optionally) publish

**Objective**: Dry run and (optionally) publish.

```bash
mix deps.get
mix test
mix docs
mix hex.build           # writes private_hex_demo-0.1.0.tar
tar tf private_hex_demo-0.1.0.tar   # inspect contents

# Authenticate once per machine:
mix hex.user auth

# Public publish (do NOT run unless you intend to):
# mix hex.publish

# Private-org publish:
# mix hex.organization auth your_org
# mix hex.publish --organization your_org
```

### Why this works

The `package/0` whitelist is the only source of truth Hex consults when
building the tarball, so declaring `files:` explicitly gives you a
reviewable list instead of the implicit default. `mix hex.build` produces
the exact artifact that would be uploaded, so `tar tf` is a reliable
audit. The `organization:` field on the package and the dep side give
the same library two routing modes (public vs private) through one
configuration surface.

---

## Benchmark

<!-- benchmark N/A: publishing is a one-shot network operation; the
     relevant metric is correctness of the tarball, not throughput. -->

---

## Trade-offs and production gotchas

**1. `files:` is a whitelist — audit it**
Every published release is immutable. If you accidentally ship
`config/prod.secret.exs` once, it lives forever on Hex unless you contact
support. `mix hex.build` + `tar tf` is the last checkpoint. Make it a
habit, not a "someday".

**2. Versions on Hex are immutable**
You CANNOT re-publish `0.1.0`. If you make a mistake, you must either
retire the version (`mix hex.retire`) and release `0.1.1`, or contact
Hex support. Treat `hex.publish` with the same care as `git push --force`.

**3. Private orgs require every consumer to authenticate**
CI needs an auth token (`HEX_AUTH_KEY`). Developers run
`mix hex.organization auth your_org` once. Forgetting this in CI is the
#1 private-org bug ("package not found" despite the dep being valid).

**4. Org members vs repo members**
On Hex.pm, an organization has MEMBERS (people who can publish) and the
packages have READ keys (what your CI uses). Don't give your CI a publish
key — scope the key to read-only.

**5. `organization:` dep flag is needed per-dep**
`{:private_hex_demo, "~> 0.1", organization: "your_org"}` — without the
tuple option, Hex only looks in public, gives "no package" error. Every
private dep needs the flag.

**6. When NOT to publish**
- The code is shared between only 2 repos — a Git dep is simpler.
- It changes weekly and you don't want to bump versions — keep it inside
  an umbrella or as a path dep during development.
- It contains proprietary code you cannot leak publicly — use a private
  org or Git, never public Hex.

---

## Reflection

- Your company has 14 internal libraries. Two choices: publish each to
  the private org, or use Git deps via SSH. Under what team size / release
  cadence does the Hex org pay off, and when is Git deps still the
  pragmatic choice?
- A junior accidentally shipped a version with a hardcoded token to the
  private org. `mix hex.retire` marks it as retired, but the tarball is
  still downloadable. What process and tooling changes would you put in
  place to make that class of bug impossible, not just retrievable?

## Resources

- [Hex.pm docs — Publishing a package](https://hex.pm/docs/publish) — the canonical guide
- [Hex.pm — Private packages](https://hex.pm/docs/private) — organizations, keys, access
- [`mix hex.publish`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Publish.html)
- [`mix hex.build`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Build.html) — dry-run the tarball
- [`mix hex.retire`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Retire.html) — mark a bad version as retired
- [`mix hex.organization`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Organization.html) — manage organization auth

## Deep Dive

Elixir's tooling ecosystem extends beyond the language into DevOps, profiling, and observability. Understanding each tool's role prevents misuse and false optimizations.

**Mix tasks and releases:**
Custom mix tasks (`mix myapp.setup`, `mix myapp.migrate`) encapsulate operational knowledge. Tasks run in the host environment (not the compiled app), so they're ideal for setup, teardown, or scripting. Releases, built with `mix release`, create self-contained OTP applications deployable without Elixir installed. They're immutable: no source code changes after release — all config comes from environment variables or runtime files.

**Debugging and profiling tools:**
- `:observer` (GUI): real-time process tree, metrics, and port inspection
- `Recon`: production-safe introspection (stable even under high load)
- `:eprof`: function-level timing; lower overhead than `:fprof`
- `:fprof`: detailed trace analysis; use only in staging

**Profiling approaches:**
Ceiling profiling (e.g., "which modules consume CPU?") is cheap; go there first with `perf` or `eprof`. Floor profiling (e.g., "which lines in this function are slow?") is expensive; reserve for specific functions. In production, prefer metrics (Prometheus, New Relic) over profiling — continuous profiling has overhead. Store profiling data for post-mortem analysis, not real-time dashboards.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/private_hex_demo_test.exs`

```elixir
defmodule PrivateHexDemoTest do
  use ExUnit.Case, async: true

  doctest PrivateHexDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert PrivateHexDemo.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
