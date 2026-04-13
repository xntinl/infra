# Managing dependencies with Hex

**Project**: `deps_intro` — a tiny app that declares Hex, path, and Git
dependencies and teaches you how version constraints and the lockfile
actually work.

---

## Why dependencias hex matters

Every Elixir project is a `mix.exs` with a `deps/0` list. New developers
copy/paste `{:jason, "~> 1.4"}` without understanding what the `~>` does,
what `mix.lock` is for, or the difference between `mix deps.get` and
`mix deps.update`. This exercise exists to make those mechanics concrete.

You'll declare three kinds of dependencies — a Hex package, a path dep
(for local development inside an umbrella), and a Git dep (for a fork or
an unpublished library) — and walk through the four commands that anyone
maintaining an Elixir project runs weekly:

| Command                    | What it actually does                                   |
|----------------------------|---------------------------------------------------------|
| `mix deps.get`             | Download deps to match `mix.lock`; create it if absent. |
| `mix deps.update jason`    | Rewrite `mix.lock` for one dep; respect `mix.exs`.      |
| `mix deps.update --all`    | Rewrite `mix.lock` for all deps; respect `mix.exs`.     |
| `mix deps.unlock --unused` | Drop entries from `mix.lock` that no longer appear.     |

---

## Project structure

```
deps_intro/
├── lib/
│   └── deps_intro.ex
├── script/
│   └── main.exs
├── test/
│   └── deps_intro_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Version constraints: `~>`, `>=`, `==`

Hex uses SemVer. The constraint controls which NEW versions `deps.update`
is allowed to pick.

| Constraint           | Accepts                          | Example        |
|----------------------|----------------------------------|----------------|
| `"~> 1.4"`           | `>= 1.4.0 and < 2.0.0`           | most common    |
| `"~> 1.4.2"`         | `>= 1.4.2 and < 1.5.0`           | stricter patch |
| `">= 1.4.0"`         | anything ≥ 1.4.0 (no upper!)     | avoid in libs  |
| `"== 1.4.2"`         | exactly 1.4.2                    | pinning        |
| `"~> 1.4 or ~> 2.0"` | either major line                | migration time |

**Rule of thumb**: apps use `~> MAJOR.MINOR`, libraries use `~> MAJOR.MINOR`
and lean on SemVer + the lockfile to prevent surprises.

### 2. `mix.lock` is the source of truth for reproducible builds

`mix.exs` says "I want Jason ~> 1.4". `mix.lock` says "I resolved it to
1.4.4 with checksum abc123". CI and your teammates install **from
`mix.lock`**, not from `mix.exs`. Always commit it.

`deps.get` creates/respects the lock. `deps.update` deliberately ignores
the lock for the deps you name, re-resolves within the constraint, and
rewrites the lock.

### 3. Path deps for umbrellas and local development

```elixir
{:core, path: "../core"}
{:core, path: "../core", in_umbrella: true}  # umbrella apps
```

Path deps are recompiled on every change — no publishing, no caching. They
do NOT go into `mix.lock`. Use them only during local development.

### 4. Git deps for forks and unreleased code

```elixir
{:some_lib, git: "https://github.com/you/some_lib.git", tag: "v0.3.1"}
{:some_lib, git: "https://github.com/you/some_lib.git", branch: "main"}
{:some_lib, git: "https://github.com/you/some_lib.git", ref: "abc1234"}
```

Git deps **do** go into the lockfile (pinned to a SHA). Prefer `ref` or
`tag` over `branch` — a moving branch means non-reproducible builds.

### 5. `only:` and `runtime:` — scope deps to environments

```elixir
{:credo, "~> 1.7", only: [:dev, :test], runtime: false}
{:ex_doc, "~> 0.31", only: :dev, runtime: false}
```

`only:` skips the dep when building other envs (shrinks releases).
`runtime: false` tells OTP not to start it as an application — essential
for tool-only deps like Credo or ExDoc that would pollute your app start.

---

## Why Hex + lockfile and not vendoring

Vendoring (`git subtree`) gives you reproducibility but no dep graph
resolution — if two deps need the same lib at different versions you
solve it manually. Hex resolves the graph and `mix.lock` captures the
resolution with checksums, so CI and every teammate build byte-identical
trees. Vendoring has its place (forks you've modified, libs with no
active maintainer) but for the common case, `~>` + `mix.lock` is the
cheaper guarantee.

---

## Design decisions

**Option A — Loose constraints (`>= 1.0`) everywhere**
- Pros: Minimal upgrade friction; `deps.update` pulls latest always.
- Cons: Any upstream breaking release silently enters your build; as a
  library author, you force downstream into dependency hell.

**Option B — `~> MAJOR.MINOR` + committed `mix.lock`** (chosen)
- Pros: SemVer-safe upgrades (`deps.update` stays within the minor
  range); lockfile pins exact versions for reproducibility; CI builds
  are byte-identical across time.
- Cons: Requires deliberate `deps.update` runs to move off a minor;
  harmless but occasional "why is this version so old?" moments.

→ Chose **B** because reproducibility is the foundation of debuggable
  systems; the alternative ("works on my machine, today") is the origin
  of 3am pager incidents.

---

## Implementation

### `mix.exs`

```elixir
defmodule DepsIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :deps_intro,
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
mix new deps_intro
cd deps_intro
```

### Step 2: `mix.exs` — declare a realistic mix of deps

**Objective**: Edit `mix.exs` — declare a realistic mix of deps, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule DepsIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :deps_intro,
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
    [
      # --- Hex dependency, pessimistic on minor (recommended default) ---
      {:jason, "~> 1.4"},

      # --- Dev/test-only tooling; not a runtime application ---
      {:ex_doc, "~> 0.31", only: :dev, runtime: false},

      # --- Example path dep (commented out — uncomment if you have ../shared) ---
      # {:shared, path: "../shared"},

      # --- Example Git dep pinned to a tag; SWAP for a real one when needed ---
      # {:nimble_parsec, git: "https://github.com/dashbitco/nimble_parsec.git", tag: "v1.4.0"}
    ]
  end
end
```

### `lib/deps_intro.ex`

**Objective**: Edit `deps_intro.ex` — prove Jason works, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule DepsIntro do
  @moduledoc """
  Trivial user of a Hex dependency (`jason`) — just to demonstrate that after
  `mix deps.get` and `mix compile`, external deps are available at runtime.
  """

  @doc """
  Encodes a map to JSON via Jason.

  ## Examples

      iex> DocsDemo = DepsIntro
      iex> DepsIntro.to_json(%{a: 1}) |> Jason.decode!()
      %{"a" => 1}
  """
  @spec to_json(map()) :: String.t()
  def to_json(map) when is_map(map) do
    Jason.encode!(map)
  end

  @doc "Decodes a JSON string into a map with string keys."
  @spec from_json(String.t()) :: {:ok, term()} | {:error, Jason.DecodeError.t()}
  def from_json(json) when is_binary(json) do
    Jason.decode(json)
  end
end
```

### Step 4: `test/deps_intro_test.exs`

**Objective**: Write `deps_intro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule DepsIntroTest do
  use ExUnit.Case, async: true

  doctest DepsIntro

  describe "core functionality" do
    test "round-trips a map" do
      original = %{"x" => 1, "y" => [1, 2, 3]}
      encoded = DepsIntro.to_json(original)
      assert {:ok, ^original} = DepsIntro.from_json(encoded)
    end

    test "returns an error tuple on invalid JSON" do
      assert {:error, %Jason.DecodeError{}} = DepsIntro.from_json("{not json")
    end
  end
end
```

### Step 5: Run through the commands

**Objective**: Run through the commands.

```bash
mix deps.get      # fetches Jason; creates mix.lock
mix deps.tree     # shows the dependency tree (your deps + their transitives)
mix hex.outdated  # shows which deps have newer versions available
mix test          # build and run tests
mix deps.update jason --only        # update Jason, respecting its constraint
mix deps.unlock --unused             # prune stale lockfile entries
```

Peek at `mix.lock`:

```elixir
# mix.lock (abbreviated)
%{
  "jason": {:hex, :jason, "1.4.4", "b9226...", [:mix], [], "hexpm", "c5ef..."}
}
```

The important bits: the resolved version (`1.4.4`) and the checksum. That's
what guarantees your teammate installs byte-identical code.

### Why this works

`mix.exs` declares intent (a constraint); `mix.lock` captures the
resolution (exact version + checksum). `deps.get` honors the lock,
guaranteeing reproducibility. `deps.update` deliberately ignores the
lock for named deps and re-resolves within the constraint — that's how
upgrades happen on purpose instead of drifting. Environment scoping
(`only:`, `runtime: false`) keeps dev tools out of production releases
and out of OTP's application-start list.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule DepsIntro do
    @moduledoc """
    Trivial user of a Hex dependency (`jason`) — just to demonstrate that after
    `mix deps.get` and `mix compile`, external deps are available at runtime.
    """

    @doc """
    Encodes a map to JSON via Jason.

    ## Examples

        iex> DocsDemo = DepsIntro
        iex> DepsIntro.to_json(%{a: 1}) |> Jason.decode!()
        %{"a" => 1}
    """
    @spec to_json(map()) :: String.t()
    def to_json(map) when is_map(map) do
      Jason.encode!(map)
    end

    @doc "Decodes a JSON string into a map with string keys."
    @spec from_json(String.t()) :: {:ok, term()} | {:error, Jason.DecodeError.t()}
    def from_json(json) when is_binary(json) do
      Jason.decode(json)
    end
  end

  def main do
    IO.puts("=== Deps Demo ===
  ")
  
    # Demo: Hex dependencies
  IO.puts("1. mix deps.get - fetch dependencies")
  IO.puts("2. mix deps.tree - show dependency tree")
  IO.puts("3. Version pinning in mix.lock")

  IO.puts("
  ✓ Hex dependencies demo completed!")
  end

end

Main.main()
```

## Benchmark

<!-- benchmark N/A: dependency management is a build-time concern.
     The metric that matters is "is my build reproducible?" — measured
     by running `mix deps.get && mix compile` on two different machines
     and comparing the resolved versions. Target: identical. -->

---

## Trade-offs and production gotchas

**1. Never use `>= X` without an upper bound in a library**
`>= 1.0.0` means "any future version, forever, including major breaking
releases I haven't reviewed". In a published library this forces downstream
users into dependency hell. Use `~> X.Y` in `mix.exs` for libraries.

**2. `mix deps.update` ignores the lock — that's the point**
People confuse `deps.get` (honor lock) with `deps.update` (rewrite lock).
If your lock is already correct and you just added a dep, `deps.get` is
what you want. `deps.update --all` in CI is a very easy way to accidentally
"update everything on every build".

**3. Path deps DO NOT go in `mix.lock`**
If you ship an app with a `{:core, path: "../core"}` dep, whoever clones
the repo must also have `../core`. This is fine in umbrellas (the umbrella
root guarantees the layout) but bad for open source. Publish the lib to
Hex or vendor it.

**4. Git deps on `branch:` are non-reproducible**
If you point at `branch: "main"`, `mix deps.get` on day 1 and day 30 give
you different code — even though `mix.lock` pins the SHA. The moment
someone runs `deps.update`, the SHA jumps. Prefer `tag:` or `ref:`.

**5. `:runtime` flag on tool deps prevents boot-time application starts**
`{:credo, "~> 1.7", only: [:dev, :test], runtime: false}` tells Mix not to
list Credo in `:applications`. Without `runtime: false`, Credo's OTP app
would try to start with your app in dev — usually harmless, but confusing
and wasteful.

**6. When NOT to add a dep**
- It's 50 lines of code and you'd spend more time vendoring than reading it.
- It depends on 30 transitives for a trivial feature.
- It has not been updated in 4 years and has open CVEs.
- You can `:telemetry`, `:persistent_term`, or standard-lib your way out.

Every dep is a support contract you inherit. Read the CHANGELOG and the
maintainer's release cadence before adding.

---

## Reflection

- Your CI starts failing because a transitive dep (not one in your
  `deps/0`) silently released a breaking change under a loose
  constraint from a library you use. Walk through the diagnosis using
  `mix deps.tree`, `mix hex.outdated`, and the lockfile. What would
  you change in your setup to catch this before prod?
- You inherit a repo with no `mix.lock` committed. Two developers
  report "works on my machine" mismatches. Before adding the lockfile,
  what's the fastest way to determine which versions are currently in
  use by each developer, and how would you reconcile them?

## Resources

- ["Managing Deps" — Mix guide](https://hexdocs.pm/mix/Mix.Tasks.Deps.html) — every `deps.*` task explained
- [`Version` — Elixir stdlib](https://hexdocs.pm/elixir/Version.html) — the exact rules for `~>`, `>=`, etc.
- [Hex.pm docs — "Publishing a package"](https://hex.pm/docs/publish)
- [`mix hex.outdated`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Outdated.html)
- ["SemVer"](https://semver.org/) — the spec that Hex constraints are built on

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

### `test/deps_intro_test.exs`

```elixir
defmodule DepsIntroTest do
  use ExUnit.Case, async: true

  doctest DepsIntro

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert DepsIntro.run(:noop) == :ok
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
