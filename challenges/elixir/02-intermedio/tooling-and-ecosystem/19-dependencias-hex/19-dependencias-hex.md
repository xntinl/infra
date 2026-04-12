# Managing dependencies with Hex

**Project**: `deps_intro` — a tiny app that declares Hex, path, and Git
dependencies and teaches you how version constraints and the lockfile
actually work.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

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

Project structure:

```
deps_intro/
├── lib/
│   └── deps_intro.ex
├── test/
│   └── deps_intro_test.exs
├── mix.exs
└── mix.lock      # generated — COMMIT THIS
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

## Implementation

### Step 1: Create the project

```bash
mix new deps_intro
cd deps_intro
```

### Step 2: `mix.exs` — declare a realistic mix of deps

```elixir
defmodule DepsIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :deps_intro,
      version: "0.1.0",
      elixir: "~> 1.15",
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

### Step 3: `lib/deps_intro.ex` — prove Jason works

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

```elixir
defmodule DepsIntroTest do
  use ExUnit.Case, async: true

  test "round-trips a map" do
    original = %{"x" => 1, "y" => [1, 2, 3]}
    encoded = DepsIntro.to_json(original)
    assert {:ok, ^original} = DepsIntro.from_json(encoded)
  end

  test "returns an error tuple on invalid JSON" do
    assert {:error, %Jason.DecodeError{}} = DepsIntro.from_json("{not json")
  end
end
```

### Step 5: Run through the commands

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

## Resources

- ["Managing Deps" — Mix guide](https://hexdocs.pm/mix/Mix.Tasks.Deps.html) — every `deps.*` task explained
- [`Version` — Elixir stdlib](https://hexdocs.pm/elixir/Version.html) — the exact rules for `~>`, `>=`, etc.
- [Hex.pm docs — "Publishing a package"](https://hex.pm/docs/publish)
- [`mix hex.outdated`](https://hexdocs.pm/hex/Mix.Tasks.Hex.Outdated.html)
- ["SemVer"](https://semver.org/) — the spec that Hex constraints are built on
