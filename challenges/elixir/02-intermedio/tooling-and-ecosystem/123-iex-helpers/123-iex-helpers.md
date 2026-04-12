# Customizing IEx with `.iex.exs` and IEx helpers

**Project**: `iex_helpers_demo` — a project with a `.iex.exs` that imports
a custom helpers module, plus a tour of the built-in IEx helpers everyone
should know.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

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

Project structure:

```
iex_helpers_demo/
├── lib/
│   ├── iex_helpers_demo.ex
│   └── iex_helpers_demo/
│       └── shell.ex        # our custom helpers module
├── test/
│   └── shell_test.exs
├── .iex.exs                 # loaded on `iex -S mix`
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

See exercise 17 — `IEx.pry/0` is a breakpoint at the source level, useful
when paired with `iex -S mix` for interactive debugging.

---

## Implementation

### Step 1: Create the project

```bash
mix new iex_helpers_demo
cd iex_helpers_demo
```

### Step 2: The helpers module — `lib/iex_helpers_demo/shell.ex`

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

### Step 3: `lib/iex_helpers_demo.ex` — trivial library code

```elixir
defmodule IExHelpersDemo do
  @moduledoc "Demo library — see `IExHelpersDemo.Shell` for the actual content."

  @doc "Just so we have something to call from the shell."
  @spec hello() :: :world
  def hello, do: :world
end
```

### Step 4: `.iex.exs`

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

```elixir
defmodule IExHelpersDemo.ShellTest do
  use ExUnit.Case, async: true

  import ExUnit.CaptureIO
  alias IExHelpersDemo.Shell

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
```

### Step 6: Explore

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

## Resources

- [`IEx.Helpers`](https://hexdocs.pm/iex/IEx.Helpers.html) — all built-in helpers
- [`IEx` overview](https://hexdocs.pm/iex/IEx.html) — configuration, `.iex.exs`, history
- ["The `.iex.exs` file"](https://hexdocs.pm/iex/IEx.html#module-the-iex-exs-file) — loading order and examples
- [`:recon`](https://hexdocs.pm/recon/) — production-grade analog of `top/1`, covered in exercise 125
