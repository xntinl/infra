# Setup and Mix: Building a JSON Validator CLI

**Project**: `json_validator` — a CLI tool that validates JSON files against structural rules

---

## Why Mix matters for a senior developer

Mix is not just a scaffold generator. It is the build system, task runner, dependency
manager, and release tool for every Elixir project. Understanding what Mix generates
and why matters when you need to:

- Configure compile-time behavior per environment (`:dev`, `:test`, `:prod`)
- Add external dependencies and manage their versions
- Build escripts — single-binary CLI tools compiled from your project
- Configure `mix test --cover` for coverage metrics
- Customize `.formatter.exs` for consistent code style across teams

Every professional Elixir project starts with `mix.exs`. Reading it tells you the
Elixir version, dependencies, test configuration, and release strategy in under
30 lines.

---

## The business problem

Your team receives JSON configuration files from external partners. Before processing
them, you need a CLI tool that:

1. Accepts a file path and optional `--strict` flag via `OptionParser`
2. Reads the file, parses it as JSON using the `jason` library
3. Validates required keys are present
4. Reports errors to stderr, output to stdout
5. Can be compiled as an escript for distribution

---

## Project structure

```
json_validator/
├── lib/
│   └── json_validator/
│       ├── cli.ex
│       └── core.ex
├── test/
│   └── json_validator/
│       ├── cli_test.exs
│       └── core_test.exs
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── test.exs
│   ├── prod.exs
│   └── runtime.exs
├── .formatter.exs
└── mix.exs
```

---

## Design decisions

**Option A — escript**
- Pros: Single compiled binary, no Erlang runtime required on target, trivial distribution
- Cons: No supervision tree, one-shot execution, cannot be a long-running daemon

**Option B — mix release** (chosen)
- Pros: Full OTP release with runtime, supervision, hot upgrades
- Cons: Heavier artifact, target must match OS/arch, overkill for a CLI that runs and exits

→ Chose **A** because a validator is a one-shot tool, not a supervised application.

## Implementation

### Step 1: Create the project

```bash
mix new json_validator
cd json_validator
mkdir -p config
```

### Step 2: `mix.exs` — real-world configuration

```elixir
# mix.exs
defmodule JsonValidator.MixProject do
  use Mix.Project

  def project do
    [
      app: :json_validator,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      escript: escript(),
      test_coverage: [tool: ExCoveralls],
      preferred_cli_env: [
        coveralls: :test,
        "coveralls.detail": :test,
        "coveralls.html": :test
      ]
    ]
  end

  def application do
    [
      extra_applications: [:logger]
    ]
  end

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:excoveralls, "~> 0.18", only: :test}
    ]
  end

  defp escript do
    [main_module: JsonValidator.CLI]
  end
end
```

Key decisions visible here:

- `escript: escript()` declares this project produces a compiled binary. The
  `main_module` must export a `main/1` function that receives `argv` as a list
  of strings. Build it with `mix escript.build`.
- `deps/0` includes `jason` for JSON parsing — the de facto standard in the
  Elixir ecosystem. The `~>` operator allows patch-level upgrades only.
- `test_coverage` and `preferred_cli_env` configure `mix coveralls` to always
  run in the `:test` environment, preventing accidental coverage runs in dev.
- `start_permanent: Mix.env() == :prod` — in production, if the top-level
  supervisor crashes, the VM exits. In dev/test, it does not.

### Step 3: `.formatter.exs`

```elixir
# .formatter.exs
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98,
  locals_without_parens: [assert: 1, refute: 1]
]
```

The formatter ensures consistent style. `line_length: 98` fits most screens.
`locals_without_parens` allows test macros like `assert` without parentheses,
matching the idiomatic Elixir test style.

### Step 4: `config/config.exs` and `config/runtime.exs`

```elixir
# config/config.exs
import Config

config :json_validator,
  required_keys: ["name", "version", "type"]

import_config "#{config_env()}.exs"
```

```elixir
# config/dev.exs
import Config

config :json_validator,
  verbose: true
```

```elixir
# config/test.exs
import Config

config :json_validator,
  verbose: false
```

```elixir
# config/prod.exs
import Config
```

```elixir
# config/runtime.exs
import Config

if config_env() == :prod do
  config :json_validator,
    required_keys:
      System.get_env("REQUIRED_KEYS", "name,version,type")
      |> String.split(",", trim: true)
end
```

`config/config.exs` runs at compile time. `config/runtime.exs` runs at boot time —
critical for reading environment variables in production. Never put `System.get_env/1`
in `config.exs` — it captures the value at compile time, which is almost never what
you want in a deployed system.

### Step 5: `lib/json_validator/cli.ex`

```elixir
defmodule JsonValidator.CLI do
  @moduledoc """
  Entry point for the json_validator escript.

  Parses command-line arguments with OptionParser and delegates
  to the validator. Handles exit codes following Unix conventions.
  """

  @spec main([String.t()]) :: :ok
  def main(argv) do
    argv
    |> parse_args()
    |> run()
    |> exit_with_code()
  end

  @doc """
  Parses argv into a structured command using OptionParser.

  OptionParser distinguishes between switches (--strict), flags,
  and positional arguments. It returns {parsed, remaining, invalid}.
  """
  @spec parse_args([String.t()]) ::
          {:validate, String.t(), keyword()} | :help
  def parse_args(argv) do
    switches = [strict: :boolean, help: :boolean, keys: :string]
    aliases = [s: :strict, h: :help, k: :keys]

    case OptionParser.parse(argv, switches: switches, aliases: aliases) do
      {opts, [file_path], []} ->
        if opts[:help], do: :help, else: {:validate, file_path, opts}

      {opts, [], []} ->
        if opts[:help], do: :help, else: :help

      {_opts, _args, invalid} when invalid != [] ->
        IO.puts(:stderr, "Invalid options: #{inspect(invalid)}")
        :help

      _ ->
        :help
    end
  end

  @spec run(:help | {:validate, String.t(), keyword()}) ::
          :ok | {:error, String.t()}
  defp run(:help) do
    IO.puts("""
    Usage: json_validator <file.json> [options]

    Options:
      -s, --strict   Reject unknown keys (not in required list)
      -k, --keys     Comma-separated required keys (overrides config)
      -h, --help     Show this help message
    """)

    :ok
  end

  defp run({:validate, file_path, opts}) do
    required_keys =
      if opts[:keys] do
        String.split(opts[:keys], ",", trim: true)
      else
        Application.get_env(:json_validator, :required_keys, ["name", "version"])
      end

    strict? = Keyword.get(opts, :strict, false)

    JsonValidator.Core.validate_file(file_path, required_keys, strict?)
  end

  @spec exit_with_code(:ok | {:error, String.t()}) :: :ok
  defp exit_with_code(:ok) do
    IO.puts("Validation passed.")
    :ok
  end

  defp exit_with_code({:error, reason}) do
    IO.puts(:stderr, "Validation failed: #{reason}")
    System.halt(1)
  end
end
```

**Why this works:**

- `OptionParser.parse/2` returns a three-element tuple: `{parsed_opts, remaining_args, invalid}`.
  The `switches` keyword constrains types — `:boolean` for flags, `:string` for values.
  `aliases` maps short flags to long ones.
- `parse_args/1` is public and returns data (not side effects) — making it testable
  without capturing IO.
- `run/1` is private — it performs IO and delegates to the core validator.
- `exit_with_code/1` calls `System.halt(1)` on failure. This is intentionally in the
  CLI layer, never in the core logic, so tests can call `Core.validate_file/3` without
  the process dying.

### Step 6: `lib/json_validator/core.ex`

```elixir
defmodule JsonValidator.Core do
  @moduledoc """
  Pure validation logic. No IO, no side effects.

  This module validates parsed JSON data against structural rules.
  It does not read files or print output — that responsibility
  belongs to the CLI layer.
  """

  @doc """
  Reads a JSON file and validates it against the given rules.

  Returns :ok if all required keys are present, {:error, reason} otherwise.
  """
  @spec validate_file(String.t(), [String.t()], boolean()) ::
          :ok | {:error, String.t()}
  def validate_file(file_path, required_keys, strict?) do
    with {:ok, content} <- read_file(file_path),
         {:ok, data} <- parse_json(content),
         :ok <- check_is_object(data),
         :ok <- check_required_keys(data, required_keys),
         :ok <- check_strict_mode(data, required_keys, strict?) do
      :ok
    end
  end

  @spec read_file(String.t()) :: {:ok, String.t()} | {:error, String.t()}
  defp read_file(path) do
    case File.read(path) do
      {:ok, content} -> {:ok, content}
      {:error, reason} -> {:error, "cannot read file: #{:file.format_error(reason)}"}
    end
  end

  @spec parse_json(String.t()) :: {:ok, term()} | {:error, String.t()}
  defp parse_json(content) do
    case Jason.decode(content) do
      {:ok, data} -> {:ok, data}
      {:error, %Jason.DecodeError{} = err} -> {:error, "invalid JSON: #{Exception.message(err)}"}
    end
  end

  @spec check_is_object(term()) :: :ok | {:error, String.t()}
  defp check_is_object(data) when is_map(data), do: :ok
  defp check_is_object(_), do: {:error, "top-level value must be a JSON object"}

  @spec check_required_keys(map(), [String.t()]) :: :ok | {:error, String.t()}
  defp check_required_keys(data, required_keys) do
    missing =
      required_keys
      |> Enum.reject(&Map.has_key?(data, &1))

    case missing do
      [] -> :ok
      keys -> {:error, "missing required keys: #{Enum.join(keys, ", ")}"}
    end
  end

  @spec check_strict_mode(map(), [String.t()], boolean()) ::
          :ok | {:error, String.t()}
  defp check_strict_mode(_data, _required_keys, false), do: :ok

  defp check_strict_mode(data, required_keys, true) do
    allowed = MapSet.new(required_keys)
    extra = data |> Map.keys() |> Enum.reject(&MapSet.member?(allowed, &1))

    case extra do
      [] -> :ok
      keys -> {:error, "unknown keys in strict mode: #{Enum.join(keys, ", ")}"}
    end
  end
end
```

**Why this works:**

- `validate_file/3` uses `with` to chain multiple validation steps. Each step
  returns `{:ok, value}` on success or `{:error, reason}` on failure. If any
  step fails, `with` short-circuits and returns the error. This avoids nested
  `case` statements.
- Every private function has a clear responsibility and a typed return.
- `check_strict_mode/3` uses pattern matching on the boolean: the `false` clause
  is a no-op, the `true` clause performs the check. No `if` needed.
- `:file.format_error/1` converts Erlang file error atoms (`:enoent`, `:eacces`)
  into human-readable strings. This is an Erlang interop pattern you will use
  frequently.

### Step 7: Tests

```elixir
# test/json_validator/core_test.exs
defmodule JsonValidator.CoreTest do
  use ExUnit.Case, async: true

  alias JsonValidator.Core

  @fixtures_dir Path.join([__DIR__, "..", "fixtures"])

  setup_all do
    File.mkdir_p!(Path.join(@fixtures_dir, ""))

    File.write!(
      Path.join(@fixtures_dir, "valid.json"),
      Jason.encode!(%{"name" => "app", "version" => "1.0", "type" => "service"})
    )

    File.write!(
      Path.join(@fixtures_dir, "missing_keys.json"),
      Jason.encode!(%{"name" => "app"})
    )

    File.write!(
      Path.join(@fixtures_dir, "invalid.json"),
      "not json at all"
    )

    File.write!(
      Path.join(@fixtures_dir, "array.json"),
      Jason.encode!([1, 2, 3])
    )

    File.write!(
      Path.join(@fixtures_dir, "extra_keys.json"),
      Jason.encode!(%{"name" => "app", "version" => "1.0", "type" => "service", "debug" => true})
    )

    :ok
  end

  describe "validate_file/3" do
    test "returns :ok for valid JSON with all required keys" do
      path = Path.join(@fixtures_dir, "valid.json")
      assert :ok = Core.validate_file(path, ["name", "version", "type"], false)
    end

    test "returns error for missing required keys" do
      path = Path.join(@fixtures_dir, "missing_keys.json")
      assert {:error, message} = Core.validate_file(path, ["name", "version", "type"], false)
      assert message =~ "missing required keys"
      assert message =~ "version"
    end

    test "returns error for non-existent file" do
      assert {:error, message} = Core.validate_file("/tmp/nope.json", ["name"], false)
      assert message =~ "cannot read file"
    end

    test "returns error for invalid JSON" do
      path = Path.join(@fixtures_dir, "invalid.json")
      assert {:error, message} = Core.validate_file(path, ["name"], false)
      assert message =~ "invalid JSON"
    end

    test "returns error when top-level is not an object" do
      path = Path.join(@fixtures_dir, "array.json")
      assert {:error, message} = Core.validate_file(path, ["name"], false)
      assert message =~ "must be a JSON object"
    end

    test "passes in non-strict mode with extra keys" do
      path = Path.join(@fixtures_dir, "extra_keys.json")
      assert :ok = Core.validate_file(path, ["name", "version", "type"], false)
    end

    test "fails in strict mode with extra keys" do
      path = Path.join(@fixtures_dir, "extra_keys.json")
      assert {:error, message} = Core.validate_file(path, ["name", "version", "type"], true)
      assert message =~ "unknown keys"
      assert message =~ "debug"
    end
  end
end
```

```elixir
# test/json_validator/cli_test.exs
defmodule JsonValidator.CLITest do
  use ExUnit.Case, async: true

  alias JsonValidator.CLI

  describe "parse_args/1" do
    test "parses file path without options" do
      assert {:validate, "config.json", []} = CLI.parse_args(["config.json"])
    end

    test "parses file path with --strict flag" do
      assert {:validate, "config.json", [strict: true]} =
               CLI.parse_args(["config.json", "--strict"])
    end

    test "parses short aliases" do
      assert {:validate, "config.json", [strict: true]} =
               CLI.parse_args(["config.json", "-s"])
    end

    test "parses --keys option" do
      assert {:validate, "f.json", opts} = CLI.parse_args(["f.json", "-k", "a,b,c"])
      assert opts[:keys] == "a,b,c"
    end

    test "returns :help when no arguments given" do
      assert :help = CLI.parse_args([])
    end

    test "returns :help when --help flag is present" do
      assert :help = CLI.parse_args(["--help"])
    end
  end
end
```

### Step 8: Run and verify

```bash
# Install dependencies
mix deps.get

# Compile with strict warnings
mix compile --warnings-as-errors

# Run tests with trace output
mix test --trace

# Run tests with coverage
mix test --cover

# Format the project
mix format

# Build the escript (produces a ./json_validator binary)
mix escript.build

# Run the binary
./json_validator sample.json --strict
./json_validator --help
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of parse_args
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **1000 invocations under 10ms total (single parse < 10µs)**.

## Trade-off analysis

| Aspect | Current approach | Alternative |
|--------|-----------------|-------------|
| Argument parsing | `OptionParser` with typed switches | Raw pattern match on `argv` |
| JSON library | `jason` (pure Elixir, fast) | `poison` (older, less maintained) |
| Validation chain | `with` expression | Nested `case` |
| Distribution | escript (single binary) | `mix release` (full OTP release) |
| Config at runtime | `config/runtime.exs` | `System.get_env/1` inline |

When to use escript vs release: escripts are for CLI tools that run and exit. Releases
are for long-running OTP applications with supervision trees. A validator CLI is a
perfect escript use case.

---

## Common production mistakes

**1. `System.get_env/1` in `config.exs` instead of `runtime.exs`**
`config.exs` runs at compile time. If you read `DATABASE_URL` there, it captures the
value from your CI environment and bakes it into the release. Use `runtime.exs` for
anything that should be read at boot time.

**2. Forgetting `mix deps.get` after editing `mix.exs`**
Adding a dependency to `deps/0` does not automatically download it. Run
`mix deps.get` before `mix compile`. Skipping this gives cryptic "module not found"
errors at compile time.

**3. `start_permanent` confusion in dev**
`start_permanent: Mix.env() == :prod` means crashes are more visible in production
(the VM exits) but silent in dev (the supervisor restarts). If something crashes in
dev and you don't see it, check `Mix.env()`.

**4. Not using `--warnings-as-errors` in CI**
`mix compile --warnings-as-errors` turns unused variables, unreachable clauses, and
deprecated function calls into hard failures. Without it, warnings accumulate silently
in CI and you lose the compiler's safety net.

**5. Escript missing `main/1`**
The module specified in `escript: [main_module: MyModule]` must export `main/1`.
If it does not, `mix escript.build` succeeds but the binary crashes at runtime with
`UndefinedFunctionError`. Always verify with `./your_binary --help` after building.

---

## Reflection

If your validator needed to process a directory of 10k JSON files in parallel, would you stay with an escript, or move to a Mix release with a Task supervisor? Justify based on startup cost and supervision needs.

Suppose `required_keys` came from a database instead of config — where would that lookup live, and how would you keep the CLI testable?

## Resources

- [Mix documentation — HexDocs](https://hexdocs.pm/mix/Mix.html)
- [OptionParser — HexDocs](https://hexdocs.pm/elixir/OptionParser.html)
- [Escript — mix escript.build](https://hexdocs.pm/mix/Mix.Tasks.Escript.Build.html)
- [Config and runtime.exs — HexDocs](https://hexdocs.pm/elixir/Config.html)
- [Jason library — HexDocs](https://hexdocs.pm/jason/Jason.html)
