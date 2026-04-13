# Customizing IEx with `.iex.exs` and IEx helpers

**Project**: `iex_helpers_demo` — a project with a `.iex.exs` that imports
a custom helpers module, plus a tour of the built-in IEx helpers everyone
should know.

---

## Why iex helpers matters

IEx is Elixir's interactive shell. Most developers use only `iex -S mix`
and a handful of commands — but IEx supports per-project startup files
(`.iex.exs`), custom helpers, and a rich set of built-ins that make
debugging production-class nodes dramatically easier.

This exercise:

1. Wires up a project `.iex.exs` that preloads a demo helpers module.
2. Writes a realistic helpers module (fixtures, "reset state", "inspect
   the Repo", etc.).
3. Takes a guided tour of the `IEx.Helpers` built-ins: `h/1`, `i/1`,
   `v/1`, `respawn/0`, `recompile/0`, `clear/0`, `pid/1`.

---

## Project structure

```
iex_helpers_demo/
├── lib/
│   └── iex_helpers_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── iex_helpers_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `.iex.exs` is regular Elixir, run on every IEx start

```elixir
# .iex.exs
IO.puts("Welcome to iex_helpers_demo!")
import IExHelpersDemo.Shell
```
IEx looks for `.iex.exs` in (1) the current directory, (2) the user home.
Project-local version wins. Use it to import shell helpers, set compiler
options, or start services you always need during interactive use.

### 2. Built-in `IEx.Helpers` — the ones that pay off

| Helper               | Purpose                                                    |
|----------------------|------------------------------------------------------------|
| `h/1`                | Print the docs for a function or module: `h(Enum.map/2)`.   |
| `i/1`                | Inspect the concrete Elixir representation of any term.    |
| `v/1`                | Recall the N-th history value. `v(-1)` is the previous.    |
| `recompile/0`        | Recompile changed files without exiting.                   |
| `respawn/0`          | Start a fresh IEx shell, keeping the VM alive.             |
| `clear/0`            | Clear the screen.                                          |
| `pid/1`, `pid/3`     | Build a pid from the familiar `<0.123.0>` triple.          |
| `flush/0`            | Print and clear the current process's mailbox.             |
| `runtime_info/0`     | Summary of VM memory, schedulers, CPU quotas.              |
| `break!/2`, `continue/0` | Set and step through breakpoints at the source level. |

### 3. Helper modules are plain Elixir modules

Nothing special — just a module full of 0-arity / 1-arity functions you
want to call by bare name inside IEx. Import it from `.iex.exs` and every
`iex -S mix` gets them.

### 4. `dbg/2` and `IEx.pry/0` integrate with IEx

`IEx.pry/0` is a breakpoint at the source level: drop it into a function,
run the call, and IEx pauses with the lexical scope available. Combined
with `break!/2` from `IEx.Helpers`, you get a GDB-style debugger wired
into the shell.

---

## Why X and not Y

**IEx helpers vs shell aliases (bash/zsh)**: shell aliases run outside
the BEAM and can't touch your running node. Helpers live inside the VM,
so `top/1` can enumerate processes, read reductions, and inspect state.

**Project-local `.iex.exs` vs `~/.iex.exs`**: user-home helpers are
convenient but travel with the developer, not the repo. Teammates and CI
don't get them. Project-local `.iex.exs` is code the team can review,
version, and rely on.

---

## Design decisions

**Option A — Put helpers directly in `.iex.exs`**
- Pros: One file, no extra module; fine for 2–3 one-liners.
- Cons: Untestable (ExUnit doesn't run `.iex.exs`); grows unwieldy fast;
  helpers can't be reused outside IEx.

**Option B — Module under `lib/`, imported from `.iex.exs`** (chosen)
- Pros: Helpers are normal code — documented, tested, refactorable.
  `.iex.exs` stays a thin wiring file (imports + banner).
- Cons: One extra file to maintain; helpers must be compilable even when
  nobody imports them.

→ Chose **B** because shell helpers that matter enough to exist also
  matter enough to test. A broken helper on a prod remote shell is worse
  than no helper at all.

---

## Implementation

### `mix.exs`

```elixir
defmodule IexHelpersDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :iex_helpers_demo,
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
mix new iex_helpers_demo
cd iex_helpers_demo
```

### Step 2: The helpers module — `lib/iex_helpers_demo/shell.ex`

**Objective**: Provide The helpers module — `lib/iex_helpers_demo/shell.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```elixir
defmodule IExHelpersDemo.Shell do
  @moduledoc """
  Helpers exposed to the IEx shell by importing this module from `.iex.exs`.

  Keep these tiny — they're for convenience, not for production code paths.
  """

  @doc """
  Prints a visual separator in the shell. Handy when running long sequences
  of commands and you want a clear break.
  """
  @spec sep() :: :ok
  def sep do
    IO.puts("\n" <> String.duplicate("─", 60) <> "\n")
    :ok
  end

  @doc """
  Returns a deterministic fixture map for ad-hoc REPL experiments.
  """
  @spec fixture_user() :: %{name: String.t(), age: integer(), roles: [atom()]}
  def fixture_user do
    %{name: "Ada Lovelace", age: 36, roles: [:admin, :analyst]}
  end

  @doc """
  Pretty-prints every process in the system, sorted by reduction count
  (proxy for "busiest process"). Truncates to `limit` processes.
  """
  @spec top(pos_integer()) :: :ok
  def top(limit \\ 10) do
    Process.list()
    |> Enum.map(fn pid ->
      info = Process.info(pid, [:registered_name, :reductions, :memory, :message_queue_len])
      {pid, info}
    end)
    |> Enum.filter(fn {_pid, info} -> info != nil end)
    |> Enum.sort_by(fn {_pid, info} -> -info[:reductions] end)
    |> Enum.take(limit)
    |> Enum.each(fn {pid, info} ->
      IO.puts("#{inspect(pid)}  #{inspect(info)}")
    end)
  end

  @doc """
  One-shot helper that times a function and prints both the elapsed µs and
  the result. Don't use for real benchmarking — use Benchee for that.
  """
  @spec time((-> result)) :: result when result: term()
  def time(fun) when is_function(fun, 0) do
    {micros, result} = :timer.tc(fun)
    IO.puts("elapsed: #{micros} µs")
    result
  end
end
```
### `lib/iex_helpers_demo.ex`

**Objective**: Edit `iex_helpers_demo.ex` — trivial library code, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule IExHelpersDemo do
  @moduledoc "Demo library — see `IExHelpersDemo.Shell` for the actual content."

  @doc "Just so we have something to call from the shell."
  @spec hello() :: :world
  def hello, do: :world
end
```
### Step 4: `.iex.exs`

**Objective**: Implement `.iex.exs` — code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
# Loaded automatically by `iex -S mix`. This is plain Elixir.

IO.puts("== iex_helpers_demo shell ==")
IO.puts("  helpers: sep/0, fixture_user/0, top/1, time/1")
IO.puts("  builtins: h/1, i/1, v/1, recompile/0, respawn/0, flush/0")

# Import the project's helpers so they can be called bare-name.
import IExHelpersDemo.Shell

# Convenient alias for interactive use.
alias IExHelpersDemo, as: Demo
```
### Step 5: `test/shell_test.exs`

**Objective**: Write `shell_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule IExHelpersDemo.ShellTest do
  use ExUnit.Case, async: true

  doctest IExHelpersDemo.Shell

  import ExUnit.CaptureIO
  alias IExHelpersDemo.Shell

  describe "core functionality" do
    test "sep/0 prints a separator and returns :ok" do
      io = capture_io(fn -> assert Shell.sep() == :ok end)
      assert io =~ "─"
    end

    test "fixture_user/0 returns a stable map" do
      user = Shell.fixture_user()
      assert user.name == "Ada Lovelace"
      assert :admin in user.roles
    end

    test "time/1 returns the function's result and prints elapsed" do
      io =
        capture_io(fn ->
          assert Shell.time(fn -> 1 + 1 end) == 2
        end)

      assert io =~ "elapsed:"
    end

    test "top/1 prints up to N process entries" do
      io = capture_io(fn -> assert Shell.top(3) == :ok end)
      # Each line contains a pid with the #PID<...> prefix.
      assert io =~ ~r/#PID<\d+\.\d+\.\d+>/
    end
  end
end
```
### Step 6: Explore

**Objective**: Explore.

```bash
iex -S mix

iex> fixture_user()
%{name: "Ada Lovelace", age: 36, roles: [:admin, :analyst]}

iex> h(Enum.map/2)        # built-in: pretty-print docs
iex> i(fixture_user())    # built-in: inspect representation
iex> v(-1)                # built-in: the previous return value
iex> top(5)               # our custom helper
iex> time(fn -> Enum.sum(1..1_000_000) end)
iex> recompile()          # reload changes without restarting the VM
iex> respawn()            # start a fresh shell, keep the VM
```

### Why this works

`.iex.exs` runs as ordinary Elixir at shell start with full access to
the compiled project (because `iex -S mix` compiles first). Importing a
tested helpers module gives bare-name ergonomics without sacrificing
testability. Built-in helpers (`h/1`, `i/1`, `v/1`) cover reflection and
history; custom helpers cover project-specific conveniences — together
they turn IEx from a REPL into an operational console.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule IExHelpersDemo.Shell do
    @moduledoc """
    Helpers exposed to the IEx shell by importing this module from `.iex.exs`.

    Keep these tiny — they're for convenience, not for production code paths.
    """

    @doc """
    Prints a visual separator in the shell. Handy when running long sequences
    of commands and you want a clear break.
    """
    @spec sep() :: :ok
    def sep do
      IO.puts("\n" <> String.duplicate("─", 60) <> "\n")
      :ok
    end

    @doc """
    Returns a deterministic fixture map for ad-hoc REPL experiments.
    """
    @spec fixture_user() :: %{name: String.t(), age: integer(), roles: [atom()]}
    def fixture_user do
      %{name: "Ada Lovelace", age: 36, roles: [:admin, :analyst]}
    end

    @doc """
    Pretty-prints every process in the system, sorted by reduction count
    (proxy for "busiest process"). Truncates to `limit` processes.
    """
    @spec top(pos_integer()) :: :ok
    def top(limit \\ 10) do
      Process.list()
      |> Enum.map(fn pid ->
        info = Process.info(pid, [:registered_name, :reductions, :memory, :message_queue_len])
        {pid, info}
      end)
      |> Enum.filter(fn {_pid, info} -> info != nil end)
      |> Enum.sort_by(fn {_pid, info} -> -info[:reductions] end)
      |> Enum.take(limit)
      |> Enum.each(fn {pid, info} ->
        IO.puts("#{inspect(pid)}  #{inspect(info)}")
      end)
    end

    @doc """
    One-shot helper that times a function and prints both the elapsed µs and
    the result. Don't use for real benchmarking — use Benchee for that.
    """
    @spec time((-> result)) :: result when result: term()
    def time(fun) when is_function(fun, 0) do
      {micros, result} = :timer.tc(fun)
      IO.puts("elapsed: #{micros} µs")
      result
    end
  end

  def main do
    IO.puts("=== IEX Demo ===
  ")
  
    # Demo: IEx helpers
  IO.puts("1. iex(1)> h(String) - get help")
  IO.puts("2. iex(1)> s(map_size) - source location")
  IO.puts("3. iex(1)> i(value) - inspect value")

  IO.puts("
  ✓ IEx helpers demo completed!")
  end

end

Main.main()
```
## Benchmark

<!-- benchmark N/A: IEx helpers are interactive; the value is
     ergonomics, not throughput. `top/1` enumerates processes in
     O(number_of_processes) which is acceptable for ad-hoc use. -->

---

## Trade-offs and production gotchas

**1. `.iex.exs` runs BEFORE the mix project is compiled**
If your helpers module lives in the project, `iex -S mix` works (mix
compiles first). Plain `iex` outside the project fails with "module not
found". Put generic helpers in `~/.iex.exs` (user home) and project helpers
in the project's `.iex.exs`.

**2. Functions in `.iex.exs` pollute the shell namespace**
Import shadowing is easy: if your helpers export a `count/1` and you
`import` it, then `count("foo")` shadows `Enum.count/1`. Keep helpers
specific, or scope them under a module you `alias` (not `import`).

**3. `recompile/0` vs restarting IEx**
`recompile/0` is the fastest feedback loop, but it doesn't re-initialize
long-lived processes. If you have a GenServer holding old code in its
closure via a captured function, that process still runs OLD code until
it's restarted. `respawn/0` is often not enough — you may need to kill
the supervisor subtree.

**4. `h/1` only shows docs if the module was compiled WITH docs**
Mix compiles with docs in `:dev` and `:test`; in `:prod` with releases,
docs can be stripped (`strip_beams: true`). On a prod node,
`h(SomeModule)` may say "no docs" — not a bug, an optimization.

**5. Helpers are not tested by default**
Your `.iex.exs` is not under ExUnit — if it raises, every `iex -S mix`
spews a stack trace. Wrap risky init in `try/rescue` or keep `.iex.exs`
minimal (imports, aliases, banner) and put logic in testable modules.

**6. When NOT to use IEx helpers**
- For production incident debugging — use `:observer`, `:recon`, or
  `remote_shell` + audited commands, not ad-hoc helpers that only exist
  in your dev shell.
- For recurring operational tasks — promote them to a Mix task or a
  release command so they're repeatable and testable.

---

## Reflection

- Your team has a shared `IExHelpers.Prod` module with helpers like
  `reset_cache/0` that work over `Node.list()`. One of them accidentally
  does `Process.exit/2` with the wrong PID in a staging incident. What
  guardrails would you add so a dangerous helper can't be invoked by
  typing its bare name?
- `recompile/0` is fast but doesn't re-init GenServers — a captured
  closure keeps holding old code. If a dev reports "my changes don't
  show up after recompile()", what's your mental model for deciding
  between `recompile/0`, `respawn/0`, and restarting the whole VM?

## Resources

- [`IEx.Helpers`](https://hexdocs.pm/iex/IEx.Helpers.html) — all built-in helpers
- [`IEx` overview](https://hexdocs.pm/iex/IEx.html) — configuration, `.iex.exs`, history
- ["The `.iex.exs` file"](https://hexdocs.pm/iex/IEx.html#module-the-iex-exs-file) — loading order and examples
- [`:recon`](https://hexdocs.pm/recon/) — production-grade analog of `top/1` for live-node diagnostics

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

### `test/iex_helpers_demo_test.exs`

```elixir
defmodule IexHelpersDemoTest do
  use ExUnit.Case, async: true

  doctest IexHelpersDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert IexHelpersDemo.run(:noop) == :ok
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
