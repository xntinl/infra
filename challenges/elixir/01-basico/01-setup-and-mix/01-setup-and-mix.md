# Setup and Mix: Bootstrapping payments_cli

**Project**: `payments_cli` — a CLI tool that processes payment transactions

---

## Project context

You are building `payments_cli`, a CLI tool that processes payment transactions from CSV
files, validates them, applies business rules, and produces ledger reports.

This exercise sets up the project from scratch and implements a minimal CLI entry point
that accepts a file path argument, prints a message, and returns typed results for
success and failure.

---

## Why Mix matters for a senior developer

Mix is not just a scaffold generator. It is the build system, task runner, dependency
manager, and release tool for every Elixir project. Understanding what Mix generates
and why matters when you need to:

- Configure compile-time behavior per environment (`:dev`, `:test`, `:prod`)
- Add Mix tasks for operational work (database migrations, data imports)
- Configure releases with `mix release` for deployment
- Understand the app supervision tree defined in `application/0`

Every Elixir project you encounter professionally starts with `mix.exs`. Reading
it tells you the Elixir version, dependencies, test configuration, and release
strategy in under 30 lines.

---

## The business problem

The payments team needs a CLI tool to process transaction CSV files exported by the bank.
The first version just needs to:

1. Accept a file path as a command-line argument
2. Print the number of transactions found
3. Exit with code 0 on success, 1 on error

---

## Implementation

### Step 1: Create the project

```bash
mix new payments_cli
cd payments_cli
mkdir -p lib/payments_cli
mkdir -p test/payments_cli
```

### Step 2: `mix.exs` — understand what was generated

Open `mix.exs`. You will see:

```elixir
# mix.exs
defmodule PaymentsCli.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_cli,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger]
    ]
  end

  defp deps do
    []
  end
end
```

Key decisions visible here:
- `start_permanent: Mix.env() == :prod` — in production, if the top-level supervisor
  crashes, the VM exits. In dev/test, it does not. This is intentional.
- `extra_applications: [:logger]` — the Logger application starts automatically.
  Remove it and you lose structured logging across your whole app.
- `deps/0` is private (`defp`) — dependencies are an internal concern of the build
  system, not a public API of the project module.

### Step 3: `lib/payments_cli/cli.ex`

The module is intentionally thin — it handles argument parsing and exit codes only.
Business logic lives elsewhere. Pattern matching on the `args` list dispatches to
the correct behavior without any `if/else` chain.

```elixir
defmodule PaymentsCli.CLI do
  @moduledoc """
  Entry point for the payments_cli command-line tool.

  Parses arguments and delegates to the appropriate subsystem.
  This module is intentionally thin — it only handles argument parsing
  and exit codes. Business logic lives elsewhere.
  """

  @doc """
  Main entry point. Called by `mix run` or the compiled escript.

  Receives the list of command-line arguments as strings.
  Returns :ok on success or {:error, reason} on failure.
  """
  @spec main([String.t()]) :: :ok | {:error, String.t()}
  def main([file_path]) when is_binary(file_path) do
    IO.puts("Processing: #{file_path}")
    :ok
  end

  def main([]) do
    print_error("no file path given")
  end

  def main(_other) do
    print_error("usage: payments_cli <file>")
  end

  @doc """
  Prints a formatted error message to stderr and returns the error.
  Keeping this separate from main/1 makes testing easier — tests can
  call main/1 and check the return value without capturing stderr.
  """
  @spec print_error(String.t()) :: {:error, String.t()}
  def print_error(message) do
    IO.puts(:stderr, message)
    {:error, message}
  end
end
```

**Why this works:**

- `main/1` uses three function clauses with pattern matching. The first clause matches
  a list with exactly one binary element — the file path. The second matches an empty
  list. The third is a catch-all for any other input (too many arguments, wrong types).
- Each clause returns a typed value (`:ok` or `{:error, reason}`) that callers and
  tests can match on. No side effects leak into the return value.
- `print_error/1` writes to `:stderr` (not stdout), following Unix convention where
  error messages go to stderr and program output goes to stdout. This matters when
  the CLI output is piped to another program.

### Step 4: Tests

```elixir
# test/payments_cli/cli_test.exs
defmodule PaymentsCli.CLITest do
  use ExUnit.Case, async: true

  alias PaymentsCli.CLI

  describe "main/1" do
    test "returns :ok when given a file path" do
      assert :ok = CLI.main(["transactions.csv"])
    end

    test "returns error when no arguments given" do
      assert {:error, message} = CLI.main([])
      assert is_binary(message)
      assert String.length(message) > 0
    end

    test "returns error when too many arguments given" do
      assert {:error, message} = CLI.main(["file1.csv", "file2.csv"])
      assert is_binary(message)
    end
  end

  describe "print_error/1" do
    test "returns {:error, message}" do
      assert {:error, "something went wrong"} = CLI.print_error("something went wrong")
    end

    test "returns the original message unchanged" do
      msg = "unexpected input format"
      assert {:error, ^msg} = CLI.print_error(msg)
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/payments_cli/cli_test.exs --trace
```

All tests should pass with the implementation above.

### Step 6: Explore Mix tasks

```bash
# See all available tasks
mix help

# Understand what test does
mix help test

# Format your code
mix format

# Check for compilation warnings
mix compile --warnings-as-errors
```

```bash
# Run with arguments via mix run
mix run -e 'PaymentsCli.CLI.main(["transactions.csv"])'
```

---

## Trade-off analysis

| Aspect | Current approach | Alternative |
|--------|-----------------|-------------|
| Argument parsing | Pattern match on `args` list | `OptionParser.parse/2` for flags |
| Error reporting | Return `{:error, reason}` | Raise exception immediately |
| stderr vs stdout | `IO.puts(:stderr, ...)` | `IO.puts(...)` (stdout) |
| Entry point | `main/1` called by `mix run` | escript compiled binary |

Reflection question: why does `main/1` return `{:error, reason}` instead of calling
`System.halt(1)` directly? Think about testability.

---

## Common production mistakes

**1. `iex` without `-S mix` loses your project**
`iex` opens a bare Elixir session. `iex -S mix` compiles and loads your project.
Running `PaymentsCli.CLI.main/1` in plain `iex` gives `UndefinedFunctionError`.
Always use `iex -S mix` when working interactively on a project.

**2. `recompile()` in IEx is not the same as restarting**
`recompile()` reloads changed modules but does not restart the supervision tree.
For changes to `application/0` in `mix.exs`, you must fully restart `iex -S mix`.

**3. Forgetting `mix deps.get` after editing `mix.exs`**
Adding a dependency to `deps/0` does not automatically download it. Run
`mix deps.get` before `mix compile`. Skipping this gives cryptic "module not found"
errors at compile time.

**4. `start_permanent` confusion in dev**
`start_permanent: Mix.env() == :prod` means crashes are more visible in production
(the VM exits) but silent in dev (the supervisor restarts). If something crashes in
dev and you don't see it, check `Mix.env()`.

**5. Test files in the wrong directory**
Mix only picks up test files under `test/`. A file at `lib/my_test.exs` is never
run by `mix test`. Follow the convention: `test/<module_path>_test.exs`.

---

## Resources

- [Mix documentation — HexDocs](https://hexdocs.pm/mix/Mix.html) — read `project/0`, `application/0`, and `deps/0` sections
- [IEx documentation — HexDocs](https://hexdocs.pm/iex/IEx.html) — `h/1`, `recompile/0`, `i/1`
- [Mix.Task — writing custom tasks](https://hexdocs.pm/mix/Mix.Task.html)
- [Elixir releases — mix release](https://hexdocs.pm/mix/Mix.Tasks.Release.html) — how `start_permanent` matters in production
