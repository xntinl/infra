# IO, Stdio, and ANSI: Building an Interactive Calculator REPL

**Project**: `calc_repl` — an interactive calculator with history, colored output, and commands

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Why IO matters for a senior developer

Every CLI tool you write uses `IO`. Understanding the module goes further than
`IO.puts`: you need to know the difference between `:stdio` and `:stderr`, how
`IO.gets` signals EOF, why `IO.inspect` returns its input, and how ANSI escape
codes render conditionally based on terminal capability.

Understanding `IO` matters when you need to:

- Build CLI tools that behave correctly when piped (`my_tool | grep X`) or redirected (`my_tool > out.log`)
- Produce colored output for humans without breaking automation
- Read user input without freezing on EOF or dropping trailing whitespace
- Debug pipelines with `IO.inspect` without rewriting the code

---

## The business problem

You want a calculator that runs interactively. The requirements:

1. Prompt the user repeatedly, read a line, evaluate it, print the result
2. Support basic arithmetic (`+`, `-`, `*`, `/`) with operator precedence
3. Keep a numbered history (`history` command) and allow recall (`!3`)
4. Use colors for prompts and errors, but ONLY when stdout is a TTY (not when piped)
5. Exit cleanly on `quit`, `exit`, or Ctrl-D (EOF)

---

## Project structure

```
calc_repl/
├── lib/
│   └── calc_repl/
│       ├── evaluator.ex
│       ├── history.ex
│       └── repl.ex
├── test/
│   └── calc_repl/
│       ├── evaluator_test.exs
│       └── history_test.exs
└── mix.exs
```

---

## The IO primitives you must know

| Function | Purpose | Gotcha |
|----------|---------|--------|
| `IO.puts/2` | Write line + newline | Writes to `:stdio` by default |
| `IO.write/2` | Write without newline | Useful for prompts on the same line |
| `IO.gets/2` | Read a line (includes `\n`) | Returns `:eof` on Ctrl-D, `{:error, reason}` on failure |
| `IO.inspect/2` | Print + return input unchanged | Honors `label:`, `limit:`, and `pretty:` opts |
| `IO.ANSI.format/2` | Render ANSI only if terminal supports it | Returns iodata, not string |
| `:stdio` vs `:stderr` | Two device atoms | Pipes affect `:stdio`, not `:stderr` |

**The `IO.inspect` trick**: because it returns its input, you can drop it anywhere
in a pipeline without rewriting:

```elixir
data
|> transform()
|> IO.inspect(label: "after transform")
|> validate()
```

---

## Implementation

### Step 1: Create the project

```bash
mix new calc_repl --sup
cd calc_repl
```

The `--sup` flag is for `History` — it runs as a supervised Agent so state
survives across REPL reads without being passed through every function.

### Step 2: `mix.exs`

```elixir
defmodule CalcRepl.MixProject do
  use Mix.Project

  def project do
    [
      app: :calc_repl,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: [],
      escript: [main_module: CalcRepl.REPL]
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {CalcRepl.Application, []}
    ]
  end
end
```

### Step 3: `lib/calc_repl/application.ex`

```elixir
defmodule CalcRepl.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      CalcRepl.History
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CalcRepl.Supervisor)
  end
end
```

### Step 4: `lib/calc_repl/history.ex`

```elixir
defmodule CalcRepl.History do
  @moduledoc """
  Stores evaluation history indexed from 1. Implemented as an Agent because the
  REPL loop calls `push/2` and `get/1` from the same process, but structuring
  it as a separate process keeps the REPL pure (no state threading).
  """

  use Agent

  @type entry :: %{expr: String.t(), result: number()}

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts) do
    Agent.start_link(fn -> [] end, name: __MODULE__)
  end

  @spec push(String.t(), number()) :: :ok
  def push(expr, result) do
    Agent.update(__MODULE__, fn history -> history ++ [%{expr: expr, result: result}] end)
  end

  @spec all() :: [entry()]
  def all do
    Agent.get(__MODULE__, & &1)
  end

  @spec at(pos_integer()) :: {:ok, entry()} | :error
  def at(index) when is_integer(index) and index >= 1 do
    case Enum.at(all(), index - 1) do
      nil -> :error
      entry -> {:ok, entry}
    end
  end

  @spec clear() :: :ok
  def clear do
    Agent.update(__MODULE__, fn _ -> [] end)
  end
end
```

### Step 5: `lib/calc_repl/evaluator.ex`

```elixir
defmodule CalcRepl.Evaluator do
  @moduledoc """
  Arithmetic evaluator. Intentionally does NOT use `Code.eval_string/1` —
  evaluating user input as Elixir code is a security hole that reads `File.rm_rf/1`
  as a valid expression. We implement a tiny parser for `+ - * /` with proper
  precedence and parentheses instead.
  """

  @type result :: {:ok, number()} | {:error, atom()}

  @spec evaluate(String.t()) :: result()
  def evaluate(input) when is_binary(input) do
    with {:ok, tokens} <- tokenize(String.trim(input)),
         {:ok, value, []} <- parse_expression(tokens) do
      {:ok, value}
    else
      {:error, reason} -> {:error, reason}
      {:ok, _value, _leftover} -> {:error, :unexpected_tokens}
    end
  end

  # ---------- Tokenizer ----------
  # Produces a flat list of tokens: numbers as floats/ints, operators as atoms.

  defp tokenize(str), do: tokenize(str, [])

  defp tokenize("", acc), do: {:ok, Enum.reverse(acc)}

  defp tokenize(<<c, rest::binary>>, acc) when c in ~c" \t" do
    tokenize(rest, acc)
  end

  defp tokenize(<<c, _::binary>> = str, acc) when c in ~c"0123456789" do
    {number, rest} = take_number(str, "")
    tokenize(rest, [number | acc])
  end

  defp tokenize(<<op, rest::binary>>, acc) when op in ~c"+-*/()" do
    tokenize(rest, [List.to_atom([op]) | acc])
  end

  defp tokenize(_bad, _acc), do: {:error, :invalid_character}

  defp take_number(<<c, rest::binary>>, acc) when c in ~c"0123456789." do
    take_number(rest, acc <> <<c>>)
  end

  defp take_number(rest, acc) do
    number =
      if String.contains?(acc, ".") do
        String.to_float(acc)
      else
        String.to_integer(acc)
      end

    {number, rest}
  end

  # ---------- Parser ----------
  # Recursive descent with two precedence levels. Returns {:ok, value, remaining_tokens}.

  defp parse_expression(tokens) do
    with {:ok, left, rest} <- parse_term(tokens) do
      parse_expression_rest(left, rest)
    end
  end

  defp parse_expression_rest(left, [op | rest]) when op in [:+, :-] do
    with {:ok, right, rest2} <- parse_term(rest) do
      parse_expression_rest(apply_op(op, left, right), rest2)
    end
  end

  defp parse_expression_rest(left, rest), do: {:ok, left, rest}

  defp parse_term(tokens) do
    with {:ok, left, rest} <- parse_factor(tokens) do
      parse_term_rest(left, rest)
    end
  end

  defp parse_term_rest(left, [op | rest]) when op in [:*, :/] do
    with {:ok, right, rest2} <- parse_factor(rest) do
      case apply_op(op, left, right) do
        {:error, _} = err -> err
        value -> parse_term_rest(value, rest2)
      end
    end
  end

  defp parse_term_rest(left, rest), do: {:ok, left, rest}

  defp parse_factor([n | rest]) when is_number(n), do: {:ok, n, rest}

  defp parse_factor([:"(" | rest]) do
    with {:ok, value, [:")" | after_paren]} <- parse_expression(rest) do
      {:ok, value, after_paren}
    else
      {:ok, _value, _} -> {:error, :missing_closing_paren}
      other -> other
    end
  end

  defp parse_factor(_), do: {:error, :expected_number}

  defp apply_op(:+, a, b), do: a + b
  defp apply_op(:-, a, b), do: a - b
  defp apply_op(:*, a, b), do: a * b
  defp apply_op(:/, _a, 0), do: {:error, :division_by_zero}
  defp apply_op(:/, _a, 0.0), do: {:error, :division_by_zero}
  defp apply_op(:/, a, b), do: a / b
end
```

### Step 6: `lib/calc_repl/repl.ex`

```elixir
defmodule CalcRepl.REPL do
  @moduledoc """
  Interactive read-eval-print loop.

  The loop is a straight recursion that terminates when the user types `quit`,
  `exit`, or sends EOF (Ctrl-D). Because we rely on `IO.gets/2`, behavior is
  identical whether the user is at a terminal or feeding input from a file:

      echo "1 + 2" | ./calc_repl
  """

  alias CalcRepl.{Evaluator, History}

  @spec main([String.t()]) :: :ok
  def main(_argv) do
    # When started as an escript, the Application doesn't auto-boot.
    {:ok, _} = Application.ensure_all_started(:calc_repl)

    print_welcome()
    loop()
  end

  defp loop do
    case IO.gets(prompt()) do
      :eof ->
        # Ctrl-D or end of piped input — exit cleanly.
        IO.puts("")
        farewell()

      {:error, reason} ->
        # Very rare: terminal read error. Exit 1 so the shell sees failure.
        IO.puts(:stderr, format_error("input error: #{inspect(reason)}"))
        System.halt(1)

      line when is_binary(line) ->
        line
        |> String.trim()
        |> handle_input()
    end
  end

  defp handle_input(""), do: loop()

  defp handle_input(cmd) when cmd in ["quit", "exit"], do: farewell()

  defp handle_input("history") do
    History.all()
    |> Enum.with_index(1)
    |> Enum.each(fn {%{expr: e, result: r}, i} ->
      IO.puts("  #{i}: #{e} = #{format_number(r)}")
    end)

    loop()
  end

  defp handle_input("clear") do
    History.clear()
    IO.puts("history cleared")
    loop()
  end

  defp handle_input("help") do
    IO.puts("""
    Commands:
      <expression>   evaluate arithmetic (e.g. "1 + 2 * 3")
      !<n>           recall and re-run history entry n
      history        show numbered history
      clear          clear history
      help           this message
      quit | exit    leave the REPL
    """)

    loop()
  end

  defp handle_input("!" <> rest) do
    case Integer.parse(rest) do
      {index, ""} -> recall(index)
      _ -> print_error("invalid recall: #{inspect("!" <> rest)}"); loop()
    end
  end

  defp handle_input(expr) do
    evaluate_and_record(expr)
    loop()
  end

  defp recall(index) do
    case History.at(index) do
      {:ok, %{expr: expr}} ->
        IO.puts(dim("replaying: #{expr}"))
        evaluate_and_record(expr)

      :error ->
        print_error("no entry #{index} in history")
    end

    loop()
  end

  defp evaluate_and_record(expr) do
    case Evaluator.evaluate(expr) do
      {:ok, value} ->
        IO.puts(success("= #{format_number(value)}"))
        History.push(expr, value)

      {:error, reason} ->
        print_error(Atom.to_string(reason))
    end
  end

  # ---------- Output helpers ----------

  defp prompt do
    # IO.ANSI.format/2 returns iodata. When stdout is not a TTY (piped/redirected),
    # the default emit? = IO.ANSI.enabled?() returns false and the escape codes
    # are stripped automatically. That's why you don't get garbage in log files.
    IO.ANSI.format([:cyan, "calc> "]) |> IO.iodata_to_binary()
  end

  defp success(text), do: IO.ANSI.format([:green, text]) |> IO.iodata_to_binary()

  defp dim(text), do: IO.ANSI.format([:faint, text]) |> IO.iodata_to_binary()

  defp format_error(text) do
    IO.ANSI.format([:red, "error: ", :reset, text]) |> IO.iodata_to_binary()
  end

  defp print_error(text) do
    # Errors go to stderr — never stdout. Scripts that pipe stdout into another
    # program must not have errors mixed into the data stream.
    IO.puts(:stderr, format_error(text))
  end

  defp print_welcome do
    IO.puts(IO.ANSI.format([:bright, "calc_repl — type 'help' for commands"]))
  end

  defp farewell do
    IO.puts("bye")
    :ok
  end

  defp format_number(n) when is_integer(n), do: Integer.to_string(n)
  defp format_number(n) when is_float(n), do: :erlang.float_to_binary(n, [:compact, decimals: 10])
end
```

### Step 7: Tests

```elixir
# test/calc_repl/evaluator_test.exs
defmodule CalcRepl.EvaluatorTest do
  use ExUnit.Case, async: true

  alias CalcRepl.Evaluator

  describe "basic arithmetic" do
    test "addition" do
      assert {:ok, 3} = Evaluator.evaluate("1 + 2")
    end

    test "operator precedence" do
      assert {:ok, 7} = Evaluator.evaluate("1 + 2 * 3")
    end

    test "parentheses override precedence" do
      assert {:ok, 9} = Evaluator.evaluate("(1 + 2) * 3")
    end

    test "floats" do
      assert {:ok, result} = Evaluator.evaluate("1.5 + 2.25")
      assert result == 3.75
    end

    test "mixed int and float promotes to float" do
      assert {:ok, 2.5} = Evaluator.evaluate("5 / 2")
    end
  end

  describe "errors" do
    test "division by zero" do
      assert {:error, :division_by_zero} = Evaluator.evaluate("10 / 0")
    end

    test "missing closing paren" do
      assert {:error, :missing_closing_paren} = Evaluator.evaluate("(1 + 2")
    end

    test "unexpected tokens" do
      assert {:error, :unexpected_tokens} = Evaluator.evaluate("1 + 2 3")
    end

    test "invalid character" do
      assert {:error, :invalid_character} = Evaluator.evaluate("1 & 2")
    end
  end

  describe "whitespace tolerance" do
    test "extra spaces" do
      assert {:ok, 10} = Evaluator.evaluate("  2  *  5  ")
    end
  end
end
```

```elixir
# test/calc_repl/history_test.exs
defmodule CalcRepl.HistoryTest do
  # async: false because the Agent is a named singleton shared across tests.
  use ExUnit.Case, async: false

  alias CalcRepl.History

  setup do
    History.clear()
    :ok
  end

  test "push and retrieve" do
    assert :ok = History.push("1 + 1", 2)
    assert [%{expr: "1 + 1", result: 2}] = History.all()
  end

  test "at/1 is 1-indexed" do
    History.push("1 + 1", 2)
    History.push("2 * 3", 6)

    assert {:ok, %{expr: "1 + 1"}} = History.at(1)
    assert {:ok, %{expr: "2 * 3"}} = History.at(2)
    assert :error = History.at(3)
  end

  test "clear empties history" do
    History.push("1 + 1", 2)
    History.clear()
    assert [] = History.all()
  end
end
```

### Step 8: Run the tests and the REPL

```bash
mix test --trace

# Interactive mode
mix run --no-halt -e "CalcRepl.REPL.main([])"

# Pipe mode — ANSI is stripped automatically
echo "1 + 2 * 3" | mix run --no-halt -e "CalcRepl.REPL.main([])"

# Build an escript
mix escript.build
./calc_repl
```

---

## Trade-off analysis

| Aspect | `IO.gets` + loop (this impl) | `IO.stream` + `Enum` | Raw `:stdio` port |
|--------|------------------------------|----------------------|-------------------|
| Readability | straightforward recursion | functional, one-shot | verbose, manual |
| Backpressure | natural (blocks on `gets`) | same | manual |
| EOF detection | `:eof` atom | stream ends | `{:error, :closed}` |
| Testability | mockable via `:capture_io` | same | hard |
| Works piped | yes | yes | yes |

When `IO.stream` wins: when the whole program is a transform over stdin
(`cat file | my_tool > out`). For interactive loops with commands, recursion is
clearer.

---

## Common production mistakes

**1. Writing errors to stdout**
If your CLI emits errors with `IO.puts(...)` (default device `:stdio`), piping
the output into another program sends errors as data. Always use
`IO.puts(:stderr, ...)` for errors. This is not cosmetic — it's the Unix contract.

**2. Forgetting EOF handling**
A REPL that never checks for `:eof` from `IO.gets/2` hangs when input is piped
from a file: after the last line, `IO.gets` keeps returning `:eof` and if you
treat that as a string, you crash. Always pattern-match `:eof` explicitly.

**3. Hardcoding ANSI codes as strings**
Writing `"\e[31mred\e[0m"` directly is tempting but breaks when the output is
redirected — you get literal `^[[31m` in your log file. Use `IO.ANSI.format/2`,
which checks `IO.ANSI.enabled?/0` (true only when stdout is an interactive TTY).

**4. Using `Code.eval_string/1` for a calculator**
It works for `1+2`, but also for `File.rm_rf!("/")`. Never feed user input into
`Code.eval_string/1`. Write a real parser — for this project it's ~50 lines.

**5. `IO.inspect` left in production code**
`IO.inspect(data)` returns `data`, so removing it doesn't change logic — which
is exactly why developers leave it in. It prints unconditionally, bypassing
Logger levels and dumping PII. Add a Credo check for `IO.inspect` in non-test
files, or use `Kernel.dbg/2` which is stripped in production builds.

---

## When NOT to roll your own REPL

- When you need line editing, history navigation, and tab completion — use
  `iex` itself, or bind `readline` via a port
- When the CLI is non-interactive (reads args, runs, exits) — an escript with
  `OptionParser` is simpler
- When the tool will be invoked by scripts more than humans — stdin/stdout
  transforms without a prompt are easier to automate

---

## Resources

- [`IO` module — HexDocs](https://hexdocs.pm/elixir/IO.html)
- [`IO.ANSI` — escape code helpers](https://hexdocs.pm/elixir/IO.ANSI.html)
- [`ExUnit.CaptureIO` — test stdio in unit tests](https://hexdocs.pm/ex_unit/ExUnit.CaptureIO.html)
- [`IEx` source — a real-world REPL in Elixir](https://github.com/elixir-lang/elixir/tree/main/lib/iex)
- [Unix philosophy: stdout is for data, stderr is for humans](https://clig.dev/#the-basics)
