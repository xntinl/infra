# Scaffolding code with `Mix.Generator`

**Project**: `my_generator` — a custom Mix task that generates new module
files from templates using `Mix.Generator.copy_template/3`.

---

## Project context

Generators are the "`rails generate model User`" of the Elixir world.
Phoenix ships dozens (`phx.gen.html`, `phx.gen.schema`, `phx.gen.context`)
and they all use the same substrate: the `Mix.Generator` module and its
`copy_template/3`, `create_file/3`, and `create_directory/2` helpers.

In this exercise you build `mix my_generator.worker NAME` — a task that
scaffolds a GenServer worker module and an accompanying test file from
EEx templates. By the end, you'll understand:

- Why `copy_template/3` uses EEx instead of string concatenation.
- How Mix.Generator handles file conflicts (`--force`, prompts).
- Where to store templates so they're packaged with your library.

Project structure:

```
my_generator/
├── lib/
│   └── mix/
│       └── tasks/
│           └── my_generator.worker.ex
├── priv/
│   └── templates/
│       └── worker/
│           ├── worker.ex.eex
│           └── worker_test.exs.eex
├── test/
│   └── mix/
│       └── tasks/
│           └── my_generator.worker_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Mix.Generator` — six helpers you'll actually use

| Helper                                | Purpose                                      |
|---------------------------------------|----------------------------------------------|
| `create_file/3`                       | Write literal content to a path.             |
| `create_directory/2`                  | Ensure a directory exists.                   |
| `copy_file/3`                         | Copy a source file to a destination.         |
| `copy_template/3`                     | Render an EEx template + write it.           |
| `embed_template/2` (macro)            | Compile a template into the task module.     |
| `embed_text/2` (macro)                | Compile literal text into the task module.   |

Each `create_*` / `copy_*` takes a `force:` option that skips the "file
exists, overwrite? [y/n]" prompt. Your tests will need `force: true`.

### 2. `priv/` is where runtime-accessible files live

Templates must be reachable at runtime, not just compile time.
`Application.app_dir(:my_generator, "priv/templates/worker/worker.ex.eex")`
gives you a safe path that works in both dev (`mix test`) and releases.

### 3. EEx: Elixir's embedded-template engine

```eex
defmodule <%= @module %> do
  use GenServer
  # ...
end
```

`copy_template/3` takes `assigns` as a keyword list and renders the `<%= @... %>`
tags. Anything too clever (loops, conditionals) still works but usually
signals that you should split the template or use a dedicated lib.

### 4. Naming: `Macro.camelize/1` and `Macro.underscore/1`

Users pass `UserNotifier` or `user_notifier` — you accept both.

```elixir
base = String.trim_trailing(name, ".ex")
module = Macro.camelize(base)       # "UserNotifier"
underscored = Macro.underscore(base) # "user_notifier"
```

Use the camelized form in module names, the underscored form in filenames.

---

## Why Mix.Generator and not plain `File.write/2`

Rolling your own with `File.write/2` plus string interpolation technically
works but you reinvent three things Mix already solved: conflict prompts
(`file exists, overwrite?`), consistent shell output (`* creating …`), and
`priv/` path resolution across dev and releases. `Mix.Generator` gives you
all three for free and matches what every Phoenix user already reads when
they run `phx.gen.*`. Rolling your own also means your tests have to mock
`IO.gets/1` for the prompt; `force: true` handles it in one line.

---

## Design decisions

**Option A — Embedded templates via `embed_template/2`**
- Pros: No runtime `priv/` lookup; templates compile into the task module.
- Cons: Can't be edited or previewed without recompiling; awkward for
  multi-line templates; loses syntax highlighting in editors.

**Option B — External `.eex` files under `priv/templates/`** (chosen)
- Pros: Templates are real files (editable, highlightable, diffable);
  survive releases; match Phoenix's convention.
- Cons: One extra runtime lookup via `Application.app_dir/2`; a missing
  template file is a runtime error, not a compile error.

→ Chose **B** because the ergonomics of editable template files and the
  alignment with Phoenix's own generators outweigh the trivial runtime cost.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {genserver},
    {mix},
    {noreply},
    {ok},
    {reply},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new my_generator
cd my_generator
mkdir -p priv/templates/worker
mkdir -p lib/mix/tasks
```

### Step 2: The templates

**Objective**: Provide The templates — these are the supporting fixtures the main module depends on to make its concept demonstrable.


`priv/templates/worker/worker.ex.eex`:

```eex
defmodule <%= @module %> do
  @moduledoc """
  A GenServer worker scaffolded by `mix my_generator.worker`.
  """

  use GenServer

  # ── Public API ──────────────────────────────────────────────────────────

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(_opts) do
    {:ok, %{}}
  end

  @impl true
  def handle_call(_request, _from, state), do: {:reply, :ok, state}

  @impl true
  def handle_cast(_msg, state), do: {:noreply, state}
end
```

`priv/templates/worker/worker_test.exs.eex`:

```eex
defmodule <%= @module %>Test do
  use ExUnit.Case, async: true

  test "starts" do
    assert {:ok, _pid} = <%= @module %>.start_link([])
  end
end
```

### Step 3: `lib/mix/tasks/my_generator.worker.ex`

**Objective**: Implement `my_generator.worker.ex` — code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.


```elixir
defmodule Mix.Tasks.MyGenerator.Worker do
  @moduledoc """
  Scaffolds a GenServer worker and its test file from templates.

      mix my_generator.worker MyApp.Workers.Emailer

  Creates:

      lib/my_app/workers/emailer.ex
      test/my_app/workers/emailer_test.exs

  ## Options

    * `--force` — overwrite existing files without prompting.
  """
  @shortdoc "Scaffolds a GenServer worker from a template"

  use Mix.Task
  import Mix.Generator

  @switches [force: :boolean]

  @impl Mix.Task
  def run(args) do
    {opts, positional} = OptionParser.parse!(args, strict: @switches)

    name =
      case positional do
        [n] -> n
        _ -> Mix.raise("Expected exactly one NAME. Usage: mix my_generator.worker NAME")
      end

    module = Macro.camelize(name)
    path = module_to_path(module)

    assigns = [module: module]
    create_directory(Path.dirname(lib_path(path)))
    create_directory(Path.dirname(test_path(path)))

    copy_template(
      template_path("worker/worker.ex.eex"),
      lib_path(path),
      assigns,
      force: Keyword.get(opts, :force, false)
    )

    copy_template(
      template_path("worker/worker_test.exs.eex"),
      test_path(path),
      assigns,
      force: Keyword.get(opts, :force, false)
    )

    Mix.shell().info("\nDone. Generated files for #{module}.")
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  # "MyApp.Workers.Emailer" -> "my_app/workers/emailer"
  defp module_to_path(module) do
    module
    |> String.split(".")
    |> Enum.map_join("/", &Macro.underscore/1)
  end

  defp lib_path(relative), do: Path.join("lib", relative <> ".ex")
  defp test_path(relative), do: Path.join("test", relative <> "_test.exs")

  defp template_path(rel) do
    # Application.app_dir resolves priv/ both in dev and in releases.
    Application.app_dir(:my_generator, Path.join("priv/templates", rel))
  end
end
```

### Step 4: Test — `test/mix/tasks/my_generator.worker_test.exs`

**Objective**: Write `my_generator.worker_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule Mix.Tasks.MyGenerator.WorkerTest do
  use ExUnit.Case, async: false

  @tmp_dir Path.join(System.tmp_dir!(), "my_generator_test")

  setup do
    Mix.shell(Mix.Shell.Process)
    on_exit(fn -> Mix.shell(Mix.Shell.IO) end)

    File.rm_rf!(@tmp_dir)
    File.mkdir_p!(@tmp_dir)
    File.cd!(@tmp_dir)

    on_exit(fn ->
      File.cd!(File.cwd!() |> Path.dirname())
      File.rm_rf!(@tmp_dir)
    end)

    :ok
  end

  test "generates a worker and its test file" do
    Mix.Tasks.MyGenerator.Worker.run(["Demo.Worker", "--force"])

    assert File.exists?(Path.join(@tmp_dir, "lib/demo/worker.ex"))
    assert File.exists?(Path.join(@tmp_dir, "test/demo/worker_test.exs"))

    content = File.read!(Path.join(@tmp_dir, "lib/demo/worker.ex"))
    assert content =~ "defmodule Demo.Worker do"
    assert content =~ "use GenServer"
  end

  test "raises with no arguments" do
    assert_raise Mix.Error, ~r/Expected exactly one NAME/, fn ->
      Mix.Tasks.MyGenerator.Worker.run([])
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
mix my_generator.worker MyApp.Workers.Emailer
# Inspect the two new files under lib/ and test/.
```

### Why this works

`copy_template/3` composes three primitives: EEx rendering, conflict
detection, and consistent shell output. By keeping templates in `priv/`
we get release-safe paths via `Application.app_dir/2`, and by splitting
the name with `Macro.camelize/1` + `Macro.underscore/1` we accept either
casing convention without branching. `force: true` in tests sidesteps the
interactive prompt that would otherwise hang CI.

---

## Benchmark

<!-- benchmark N/A: generator task is I/O bound and runs once per invocation;
     throughput is not a meaningful metric. Expected wall time is <50ms
     for a two-file scaffold on a warm filesystem. -->

---

## Trade-offs and production gotchas

**1. Templates in `priv/` survive releases; templates in `lib/` do not**
`lib/` is compiled and stripped. Any runtime file — templates, SQL seeds,
static assets — must live under `priv/`. That's the whole point of `priv/`.

**2. `copy_template/3` prompts by default — force in tests**
Without `force: true`, a second run of the task asks "overwrite?" and hangs
in non-interactive environments (CI). Your test and any automation must
pass `force: true`.

**3. EEx doesn't escape output**
If you interpolate user input into a template that gets compiled (not what
we're doing, but adjacent mistake), you have a code-injection vulnerability.
Generators are for trusted local input; never drive them from untrusted data.

**4. `Macro.camelize/1` doesn't validate**
`Macro.camelize("foo bar")` returns `"FooBar"` silently — it strips spaces.
Validate the name matches `~r/^[A-Z][A-Za-z0-9_.]*$/` before accepting it,
otherwise users produce files with surprising paths.

**5. Generators should be idempotent or they should FAIL**
Nothing worse than a generator that succeeds on the second run and leaves
you wondering what changed. Either prompt on conflict (default), or fail
(raise `Mix.Error`) — don't silently overwrite.

**6. When NOT to use a generator**
- The thing you're generating has logic — write a function/behaviour.
- You'll change the generated code immediately anyway — the template is
  bikeshedded forever.
- There are 3 variations — use a macro or a protocol instead of 3 templates.

Good generators produce *boring, non-logic, copy-ish* files: boilerplate
test files, migration stubs, config skeletons. Not business logic.

---

## Reflection

- Your team asks for a generator that scaffolds a full "context" with
  schema, migration, and service module — three files with inter-dependent
  names. Would you keep using `Mix.Generator` directly, or would you now
  reach for `Mix.Generator` wrapped in a higher-level DSL (like Phoenix's
  `Mix.Phoenix` helpers)? What's the breaking point?
- Users start asking for "dry-run" mode (show what would be generated
  without writing). How would you retrofit that onto `copy_template/3`
  without forking the function?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Solution do
    def main do
      IO.puts("=== Elixir Tooling Info ===\n")
    
      IO.puts("Elixir version: " <> System.version())
      IO.puts("OTP version: " <> :erlang.system_info(:otp_release) |> to_string())
    
      IO.puts("\nLoaded modules:")
      modules = :code.all_loaded() |> length()
      IO.puts("Total: " <> inspect(modules))
    
      IO.puts("\nMemory info:")
      info = :erlang.memory()
      total = :proplists.get_value(:total, info)
      IO.puts("Total memory: " <> inspect(total) <> " bytes")
    end
  end

  def main do
    IO.puts("=== Generator Demo ===
  ")
  
    # Demo: Mix generator pattern
  IO.puts("1. mix my_generator creates project structure")
  IO.puts("2. Copies templates and runs eex")
  IO.puts("3. Automates boilerplate")

  IO.puts("
  ✓ Mix generator demo completed!")
  end

end

Main.main()
```


## Resources

- [`Mix.Generator`](https://hexdocs.pm/mix/Mix.Generator.html) — every helper documented
- [`EEx`](https://hexdocs.pm/eex/EEx.html) — the template engine
- [Phoenix generators source](https://github.com/phoenixframework/phoenix/tree/main/lib/mix/tasks) — canonical real-world examples
- [`Macro.camelize/1` and `Macro.underscore/1`](https://hexdocs.pm/elixir/Macro.html) — the casing helpers every generator uses
- [`Application.app_dir/2`](https://hexdocs.pm/elixir/Application.html#app_dir/2) — resolving `priv/` paths


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
