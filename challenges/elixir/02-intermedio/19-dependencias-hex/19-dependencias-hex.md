# Hex Dependencies and mix.lock

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

You're working on `task_queue`, a background job processing system. As it grows, it needs external libraries: JSON encoding for job payloads, HTTP for webhook notifications, and dev-only tooling for static analysis and documentation.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex          # already exists — starts the supervision tree
│       ├── worker.ex               # already exists — processes jobs
│       ├── queue_server.ex         # already exists — GenServer holding the queue
│       ├── scheduler.ex            # already exists — dispatches jobs to workers
│       └── registry.ex             # already exists — tracks running workers
├── test/
│   └── task_queue/
│       └── queue_server_test.exs   # given tests — must pass without modification
└── mix.exs                         # ← you configure dependencies here
```

---

## The business problem

The team needs to add three capabilities to `task_queue`:

1. **JSON serialization** — job payloads are encoded as JSON before enqueueing and decoded when processed
2. **HTTP webhook notifications** — workers call an external URL when a job completes
3. **Static analysis and docs** — Dialyxir and ExDoc for dev workflow, never deployed

These map directly to different dependency scopes in `mix.exs`.

---

## Why the lockfile is a production concern

`mix.lock` records exact resolved versions — including transitive dependencies. Without committing it, two developers running `mix deps.get` on the same `mix.exs` may resolve different transitive versions. A bug that only reproduces on the CI server often traces back to a missing or inconsistent lockfile.

The lockfile also records SHA hashes of downloaded tarballs, making supply-chain tampering detectable.

```
mix.lock entry anatomy:
"jason": {:hex, :jason, "1.4.4",
  "b9226785a9aa77b6857ca22832cffa5d5150298a",   ← tarball SHA
  [:mix], [{:decimal, "~> 1.0 or ~> 2.0", [hex: :decimal, optional: true]}],
  "hexpm", "..."}
```

Rule: `mix.lock` is always committed alongside `mix.exs`. It is never in `.gitignore`.

---

## Why `~>` and not `>=`

`~>` (the optimistic operator) pins the upper bound to the next breaking version:

- `~> 1.4` means `>= 1.4.0 and < 2.0.0` — allows minor and patch bumps
- `~> 1.4.2` means `>= 1.4.2 and < 1.5.0` — only allows patch bumps
- `>= 1.4.0` has no upper bound — could resolve `2.0.0` with breaking changes

For stable, widely-used libraries like Jason, `~> 1.4` is idiomatic. For less stable APIs, `~> 1.4.2` gives tighter control.

---

## Implementation

### Step 1: Create the project

```bash
mix new task_queue --sup
cd task_queue
```

### Step 2: `mix.exs` — configure dependencies by environment

```elixir
# mix.exs
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps do
    [
      # Production dependencies — available in all environments
      {:jason, "~> 1.4"},
      {:req, "~> 0.5"},

      # Dev-only — static analysis, never included in a release
      {:dialyxir, "~> 1.0", only: :dev, runtime: false},

      # Dev-only — documentation generation
      {:ex_doc, "~> 0.31", only: :dev, runtime: false},

      # Test-only — mocking for external HTTP calls
      {:mox, "~> 1.0", only: :test},

      # Dev + test — seed data generation
      {:faker, "~> 0.18", only: [:dev, :test]}
    ]
  end
end
```

### Step 3: `lib/task_queue/worker.ex` — use Jason for payload encoding

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Processes a single job from the queue.

  Job payloads are JSON-encoded strings. Workers decode them, execute the job,
  and optionally notify a webhook when done.
  """

  @doc """
  Executes a job given its JSON-encoded payload.

  Returns `{:ok, result}` or `{:error, reason}`.
  """
  @spec execute(String.t()) :: {:ok, term()} | {:error, term()}
  def execute(encoded_payload) when is_binary(encoded_payload) do
    case Jason.decode(encoded_payload) do
      {:ok, decoded} ->
        do_work(decoded)

      {:error, reason} ->
        {:error, {:bad_payload, reason}}
    end
  end

  @doc """
  Encodes a job map as a JSON string for enqueueing.

  Returns `{:ok, json_string}` or `{:error, reason}`.
  """
  @spec encode_job(map()) :: {:ok, String.t()} | {:error, term()}
  def encode_job(job_map) when is_map(job_map) do
    Jason.encode(job_map)
  end

  defp do_work(%{"type" => type, "args" => args}) do
    # Simulate work — in production this dispatches by job type
    {:ok, %{type: type, args: args, processed_at: DateTime.utc_now()}}
  end

  defp do_work(_invalid), do: {:error, :missing_required_fields}
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/task_queue/worker_test.exs
defmodule TaskQueue.WorkerTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Worker

  describe "encode_job/1 and execute/1 round-trip" do
    test "encodes and decodes a valid job" do
      job = %{"type" => "send_email", "args" => %{"to" => "user@example.com"}}

      assert {:ok, encoded} = Worker.encode_job(job)
      assert is_binary(encoded)

      assert {:ok, result} = Worker.execute(encoded)
      assert result.type == "send_email"
    end

    test "returns error for invalid JSON payload" do
      assert {:error, {:bad_payload, _}} = Worker.execute("not valid json {{{")
    end

    test "returns error for payload missing required fields" do
      assert {:ok, encoded} = Worker.encode_job(%{"incomplete" => true})
      assert {:error, :missing_required_fields} = Worker.execute(encoded)
    end
  end
end
```

### Step 5: Run the tests

```bash
mix deps.get
mix test test/task_queue/worker_test.exs --trace
```

All 3 tests should pass. The `execute/1` function decodes JSON via `Jason.decode/1`, pattern-matches on the decoded map to extract `"type"` and `"args"`, and delegates to `do_work/1`. The `encode_job/1` function delegates directly to `Jason.encode/1`.

### Step 6: Verify environment isolation

```bash
# Only jason, req appear — no dialyxir, ex_doc, mox, faker
MIX_ENV=prod mix deps

# All deps appear
MIX_ENV=dev mix deps

# Verify the lockfile was created
ls -la mix.lock
```

---

## Trade-off analysis

| Aspect | `~> 1.4` (minor range) | `~> 1.4.2` (patch range) | `>= 1.4.0` (no ceiling) |
|--------|------------------------|--------------------------|-------------------------|
| Flexibility | accepts minor bumps | patch bumps only | accepts breaking changes |
| CI reproducibility | depends on lock | depends on lock | lock required, high risk |
| Typical use | stable public APIs | unstable minor releases | almost never appropriate |
| lock needed? | yes | yes | especially yes |

Reflection question: why does `runtime: false` on `dialyxir` matter for releases, even though `only: :dev` already excludes it from production builds?

Answer: `only: :dev` prevents the dependency from being fetched and compiled in prod. `runtime: false` prevents the dependency's OTP application from being started when the app boots in dev. Without `runtime: false`, `dialyxir` registers itself as a started application, adding startup overhead and potentially interfering with the dev supervision tree. Both options serve different purposes and should be used together for CLI tools.

---

## Common production mistakes

**1. `mix.lock` in `.gitignore`**
Two developers resolve different transitive versions. Bug appears in CI but not locally. Always commit `mix.lock`.

**2. `mix compile` without `mix deps.get` first**
After editing `mix.exs`, you must run `mix deps.get`. The compiler uses whatever is in `deps/` — it does not auto-fetch.

**3. `~>` with the wrong precision**
`{:plug, "~> 1.14"}` allows `1.15`, `1.16`, etc. `{:plug, "~> 1.14.2"}` locks to `1.14.x`. Using two-digit `~>` for well-maintained libraries is idiomatic; three-digit when you want strict patch stability.

**4. Forgetting `runtime: false` on CLI tools**
Without it, `dialyxir` starts as an OTP application at runtime — adding startup overhead and potentially conflicting with your app's process tree.

**5. `path:` dependencies left in production**
`{:my_lib, path: "../my_lib"}` is useful during local development but must never be deployed. Use it only in dev, with the Hex version commented out alongside it.

---

## Resources

- [Hex Package Manager](https://hex.pm)
- [Mix.Tasks.Deps — official docs](https://hexdocs.pm/mix/Mix.Tasks.Deps.html)
- [Version constraints in Elixir](https://hexdocs.pm/elixir/Version.html)
- [Jason — JSON library](https://hexdocs.pm/jason/Jason.html)
- [Req — HTTP client](https://hexdocs.pm/req/Req.html)
