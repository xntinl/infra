# Custom Mix tasks with `Mix.Task`

**Project**: `my_mix_task` — a hand-rolled `mix hello` task plus a real one
that parses arguments with `OptionParser`.

---

## Project context

Every Elixir project eventually grows a folder of one-off scripts — data
migrations, seed files, reports, import/export tools. The idiomatic home
for them is **custom Mix tasks**, not `scripts/*.exs`. A Mix task runs
inside your app's OTP environment (apps started, config loaded, deps
compiled), integrates with `mix help`, and is callable from CI and from
other Mix tasks.

In this exercise you implement:

1. `mix hello` — the minimal task, to learn the `Mix.Task` behaviour.
2. `mix my_mix_task.greet NAME --shout` — a realistic task with argument
   parsing via `OptionParser` and user-facing help.

You'll also learn the file-layout rule that trips up everyone who writes
their first Mix task: **the module name dictates the task name**.

Project structure:

```
my_mix_task/
├── lib/
│   ├── mix/
│   │   └── tasks/
│   │       ├── hello.ex
│   │       └── my_mix_task.greet.ex
│   └── my_mix_task.ex
├── test/
│   ├── mix/
│   │   └── tasks/
│   │       └── my_mix_task.greet_test.exs
│   └── my_mix_task_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The module-name convention

Mix discovers tasks by scanning modules named `Mix.Tasks.<Whatever>`.

| Module                       | Task name                |
|------------------------------|--------------------------|
| `Mix.Tasks.Hello`            | `mix hello`              |
| `Mix.Tasks.MyMixTask.Greet`  | `mix my_mix_task.greet`  |
| `Mix.Tasks.Db.Seed`          | `mix db.seed`            |

Dots in the task name become dots in the module path. Namespace your tasks
with your app name (`my_mix_task.*`) — otherwise they clash with built-ins
like `mix test` or tasks from other libraries.

### 2. The `Mix.Task` behaviour

You need exactly two things:

```elixir
use Mix.Task
@shortdoc "..."   # one-line summary shown by `mix help`
@moduledoc "..."  # long help shown by `mix help my_task`
def run(args), do: ...
```

`run/1` receives the raw argv list — everything after the task name on the
command line.

### 3. `OptionParser.parse!/2` — idiomatic argument parsing

```elixir
{opts, positional} =
  OptionParser.parse!(args,
    strict: [shout: :boolean, times: :integer],
    aliases: [s: :shout, t: :times]
  )
```

- `strict:` rejects unknown flags — use it. The non-strict variant silently
  accepts typos.
- `aliases:` maps short flags (`-s`) to long ones (`--shout`).
- `parse!/2` raises on invalid options — the right call for tasks.

### 4. `Mix.Task.run/1` — calling tasks from tasks

Inside a task you can chain to others:

```elixir
Mix.Task.run("app.start")   # start your OTP app if you need it
Mix.Task.run("compile")     # compile if needed
```

Mix memoizes this — re-running a task within the same invocation is a
no-op, which is what you want for most dependencies.

---

## Why Mix tasks and not `scripts/*.exs`

A plain `mix run scripts/seed.exs` works but loses four things a Mix
task gives you for free: argument parsing (`OptionParser`), `mix help`
integration, composability (`Mix.Task.run("app.start")`), and test
harnesses (`Mix.Shell.Process`). Tasks also live where teammates expect
to find them — `mix <tab>` lists them all. Scripts become trivia after
the author leaves.

---

## Design decisions

**Option A — Put all logic inside the task module (`Mix.Tasks.*`)**
- Pros: Single file per task; no extra plumbing.
- Cons: Task modules are awkward to unit-test (they call `Mix.shell()`,
  side effects everywhere); logic is coupled to the CLI surface.

**Option B — Thin task module delegating to a library module** (chosen)
- Pros: The library (`MyMixTask`) is pure and unit-testable; the task
  module is tiny and tests only the CLI wiring via `Mix.Shell.Process`.
- Cons: Two modules instead of one; slight indirection.

→ Chose **B** because tasks with non-trivial logic inevitably get
  imported into other contexts (another task, a release command, a
  LiveBook). Keeping the behavior in a plain library makes that free.

---

## Implementation

### Step 1: Create the project

```bash
mix new my_mix_task
cd my_mix_task
```

### Step 2: The minimal task — `lib/mix/tasks/hello.ex`

```elixir
defmodule Mix.Tasks.Hello do
  @moduledoc """
  A minimal Mix task. Prints "Hello, world!".

      $ mix hello
      Hello, world!
  """
  @shortdoc "Says hello"

  use Mix.Task

  @impl Mix.Task
  def run(_args) do
    Mix.shell().info("Hello, world!")
  end
end
```

`Mix.shell().info/1` is the preferred way to print from a task — tests can
swap the shell out and capture messages (see the test in step 5).

### Step 3: A real task — `lib/mix/tasks/my_mix_task.greet.ex`

```elixir
defmodule Mix.Tasks.MyMixTask.Greet do
  @moduledoc """
  Greets the given name(s).

      $ mix my_mix_task.greet Alice
      Hello, Alice!

      $ mix my_mix_task.greet Alice Bob --shout
      HELLO, ALICE!
      HELLO, BOB!

      $ mix my_mix_task.greet Alice --times 3
      Hello, Alice!
      Hello, Alice!
      Hello, Alice!

  ## Options

    * `--shout` / `-s` — uppercase the greeting.
    * `--times N` / `-t N` — repeat the greeting N times (default 1).
  """
  @shortdoc "Greets the given name(s)"

  use Mix.Task

  @switches [shout: :boolean, times: :integer]
  @aliases [s: :shout, t: :times]

  @impl Mix.Task
  def run(args) do
    {opts, names} = OptionParser.parse!(args, strict: @switches, aliases: @aliases)

    if names == [] do
      # Fail loudly — Mix tasks should not silently succeed on missing input.
      Mix.raise("mix my_mix_task.greet expects at least one NAME. See `mix help my_mix_task.greet`.")
    end

    shout? = Keyword.get(opts, :shout, false)
    times = Keyword.get(opts, :times, 1)

    if times < 1, do: Mix.raise("--times must be >= 1, got: #{times}")

    for name <- names, _ <- 1..times do
      Mix.shell().info(MyMixTask.greeting(name, shout: shout?))
    end

    :ok
  end
end
```

### Step 4: The library function it uses — `lib/my_mix_task.ex`

```elixir
defmodule MyMixTask do
  @moduledoc """
  Library code used by the custom Mix tasks.

  Putting the logic here (not inside the task module) keeps tasks thin and
  the real behavior unit-testable without invoking Mix.
  """

  @doc """
  Builds a greeting string.

  ## Examples

      iex> MyMixTask.greeting("Ada")
      "Hello, Ada!"

      iex> MyMixTask.greeting("Ada", shout: true)
      "HELLO, ADA!"
  """
  @spec greeting(String.t(), keyword()) :: String.t()
  def greeting(name, opts \\ []) when is_binary(name) do
    msg = "Hello, #{name}!"
    if Keyword.get(opts, :shout, false), do: String.upcase(msg), else: msg
  end
end
```

### Step 5: Tests

`test/my_mix_task_test.exs`:

```elixir
defmodule MyMixTaskTest do
  use ExUnit.Case, async: true

  doctest MyMixTask

  test "greeting/2 with shout uppercases" do
    assert MyMixTask.greeting("Ada", shout: true) == "HELLO, ADA!"
  end

  test "greeting/2 without shout is plain" do
    assert MyMixTask.greeting("Ada") == "Hello, Ada!"
  end
end
```

`test/mix/tasks/my_mix_task.greet_test.exs`:

```elixir
defmodule Mix.Tasks.MyMixTask.GreetTest do
  # async: false — Mix.shell() is a process-wide setting for the test node.
  use ExUnit.Case, async: false

  setup do
    # :process shell captures messages instead of writing to stdout.
    Mix.shell(Mix.Shell.Process)
    on_exit(fn -> Mix.shell(Mix.Shell.IO) end)
    :ok
  end

  test "greets a single name" do
    Mix.Tasks.MyMixTask.Greet.run(["Ada"])
    assert_received {:mix_shell, :info, ["Hello, Ada!"]}
  end

  test "greets multiple names with --shout" do
    Mix.Tasks.MyMixTask.Greet.run(["Ada", "Bob", "--shout"])
    assert_received {:mix_shell, :info, ["HELLO, ADA!"]}
    assert_received {:mix_shell, :info, ["HELLO, BOB!"]}
  end

  test "--times N repeats N times" do
    Mix.Tasks.MyMixTask.Greet.run(["Ada", "--times", "2"])
    assert_received {:mix_shell, :info, ["Hello, Ada!"]}
    assert_received {:mix_shell, :info, ["Hello, Ada!"]}
  end

  test "short aliases work (-s, -t)" do
    Mix.Tasks.MyMixTask.Greet.run(["Ada", "-s", "-t", "2"])
    assert_received {:mix_shell, :info, ["HELLO, ADA!"]}
    assert_received {:mix_shell, :info, ["HELLO, ADA!"]}
  end

  test "raises on missing NAME" do
    assert_raise Mix.Error, ~r/expects at least one NAME/, fn ->
      Mix.Tasks.MyMixTask.Greet.run([])
    end
  end

  test "rejects unknown options" do
    assert_raise OptionParser.ParseError, fn ->
      Mix.Tasks.MyMixTask.Greet.run(["Ada", "--unknown"])
    end
  end
end
```

### Step 6: Run

```bash
mix test
mix hello
mix my_mix_task.greet Ada Bob --shout
mix help my_mix_task.greet
```

The last command prints your `@moduledoc` — free documentation.

### Why this works

Mix discovers tasks purely by module-name convention (`Mix.Tasks.*`),
so the file layout under `lib/mix/tasks/` is the whole registration
protocol. `OptionParser.parse!/2` with `strict:` gives fail-fast
argument parsing — unknown flags raise instead of silently dropping.
`Mix.shell().info/1` routes output through a swappable abstraction,
which is why the test can substitute `Mix.Shell.Process` and assert on
received messages instead of scraping stdout.

---

## Benchmark

<!-- benchmark N/A: Mix task startup is dominated by VM and dep
     compilation; task body throughput depends on what the task does.
     For CLI ergonomics, the target is sub-second startup, which is
     achievable with `--no-compile` when running from a warm project. -->

---

## Trade-offs and production gotchas

**1. Tasks run without your app started by default**
`run/1` is called before `application: :my_mix_task` boots. If your task
needs `Repo`, `Application.get_env/2`, or started children, you must call
`Mix.Task.run("app.start")` first. Forgetting this is the #1 "my task can't
find the Repo" bug.

**2. Tasks are not in production releases**
Mix tasks live in `lib/mix/tasks/` and are loaded from your app. In a
production release (`mix release`), Mix itself is NOT included — your
custom tasks disappear. For production operations, use a release CLI
(`lib/my_app/release.ex`) or a script module invoked by the release.

**3. `Mix.shell().info` vs `IO.puts`**
Tests can swap `Mix.shell()` to `Mix.Shell.Process` and assert on the
messages. `IO.puts` is untestable without `ExUnit.CaptureIO`. Use the
shell abstraction.

**4. `OptionParser.parse/2` vs `parse!/2` vs `parse_head!/2`**
- `parse/2` — returns invalid opts in a third tuple element; easy to ignore
  and forget, which hides typos.
- `parse!/2` — raises on invalid; use for tasks.
- `parse_head!/2` — stops at the first positional arg; use when you want
  `my_task foo --flag` vs `my_task --flag foo` to both work predictably.

**5. Namespacing conflicts**
If you name your task `mix build`, you shadow no built-in — but you might
shadow one from a library that is added later. Namespace with your app:
`my_app.build`. Exceptions: tasks you genuinely intend to be global (and
which you own — don't ship `mix build` in a published Hex package).

**6. When NOT to write a Mix task**
- One-off migrations that need database transactions with rollback — use
  `Ecto.Migration`, not a task.
- Anything that runs in production from an OTP release — put it on a
  `release.ex` module and call it via `bin/my_app eval`.
- Logic shared across apps in an umbrella — put it in a library and call
  the library from each app's task.

---

## Reflection

- You ship a Mix task `mix my_app.import_users FILE`. It works in dev
  but fails in production because the release doesn't include Mix.
  Walk through the refactor to move the logic behind a release command
  invoked via `bin/my_app eval "MyApp.Release.import_users('...')"`.
  What stays in the task module, what moves, and why?
- A new task needs to read the project's Ecto `Repo` and update records.
  Do you call `Mix.Task.run("app.start")` or `Mix.Task.run("ecto.create")`?
  What are the failure modes of each, and how do you test a task that
  depends on the app being started?

---

## Resources

- [`Mix.Task` — Elixir docs](https://hexdocs.pm/mix/Mix.Task.html)
- [`Mix` overview and conventions](https://hexdocs.pm/mix/Mix.html)
- [`OptionParser`](https://hexdocs.pm/elixir/OptionParser.html) — every detail of argument parsing
- [`Mix.Shell`](https://hexdocs.pm/mix/Mix.Shell.html) and [`Mix.Shell.Process`](https://hexdocs.pm/mix/Mix.Shell.Process.html) — testable task I/O
- ["Writing a Mix task"](https://hexdocs.pm/mix/Mix.Task.html#module-examples) — the canonical walkthrough
