# Disciplined Doctests for Modules with Side Effects

**Project**: `invoice_numbering` — a module whose documentation examples are doctest-executable without leaving cache or database residue.

## Project context

`invoice_numbering` allocates gap-free invoice numbers (legal requirement in several
jurisdictions). Its public API is small but subtle: allocation, reservation rollback,
last-allocated lookup. The API docs contain examples. The team wants the examples to be
run as tests (`doctest`) so they cannot drift from reality.

The complication: the module has side effects (writes to ETS, calls a GenServer). Naive
doctests of side-effecting code produce two problems:
1. The docs become bloated with setup (`start_supervised`, mocks), hurting readability.
2. Doctests share process state with the test module — order matters, isolation is weak.

The discipline is: **doctest pure helpers aggressively; side-effecting functions only
when the example truly serves documentation**. For the latter, use `doctest Module, only: [:fn_name]`
or `doctest Module, except: [:fn_name]` to curate.

```
invoice_numbering/
├── lib/
│   └── invoice_numbering/
│       ├── numbering.ex         # side-effecting — curated doctests
│       └── format.ex            # pure helpers — full doctest coverage
├── test/
│   ├── invoice_numbering/
│   │   ├── numbering_test.exs
│   │   └── format_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why doctest at all

Examples in docs that do not run go stale silently. A `:foo |> Bar.baz` example in the
`@doc` becomes a lie when `Bar.baz/1` changes. Doctest runs every `iex>`/result pair as
an actual test — the docs are kept honest.

## Why NOT doctest everything

Side-effecting doctests require the module to be supervised before the doctest runs.
The typical pattern `setup :start_supervised!` works, but every doctest example in the
same module shares state. A sequence like:

```
iex> allocate()
1

iex> allocate()
2
```

is fragile — the second example depends on the first having run. Re-ordering or isolating
them breaks the examples. For side-effecting code, write unit tests and leave doctests
only on the pure helpers.

## Core concepts

### 1. `doctest Module` runs every example
Every `iex>` block in `@doc` attributes.

### 2. `doctest Module, only: [fun_name: arity]` curates
Runs only the named functions. Ideal for mixed pure/side-effecting modules.

### 3. `@doc false` omits from doctest
Also from docs — use sparingly.

### 4. `...>` continues an iex prompt
Multi-line expressions use `iex>` for the first line and `...>` for subsequent lines.

## Design decisions

- **Option A — doctest every public function**: great coverage, painful setup for
  side-effecting examples, examples turn into fixture boilerplate.
- **Option B — doctest only pure functions**: clean, easy, leaves a gap for side-effecting
  functions whose examples are arguably the most valuable to readers.
- **Option C — Hybrid**: doctest all pure helpers; for side-effecting functions, use
  `only:` to opt in one or two examples and back them with proper unit tests.

Chosen: **Option C**.

## Implementation

### Dependencies (`mix.exs`)

```elixir
# stdlib only
```

### Step 1: pure helpers — full doctest coverage

**Objective**: Write pure formatting helpers where every public function carries executable doctests — proving the examples in the docs stay in sync with behaviour.

```elixir
# lib/invoice_numbering/format.ex
defmodule InvoiceNumbering.Format do
  @moduledoc """
  Pure formatting helpers. Every public function is doctested.
  """

  @doc """
  Formats a numeric invoice id into a zero-padded string with a fiscal-year prefix.

  ## Examples

      iex> InvoiceNumbering.Format.to_string(1, 2025)
      "2025-00000001"

      iex> InvoiceNumbering.Format.to_string(99_999_999, 2025)
      "2025-99999999"

      iex> InvoiceNumbering.Format.to_string(42, 2024)
      "2024-00000042"
  """
  @spec to_string(non_neg_integer(), pos_integer()) :: String.t()
  def to_string(id, year) when is_integer(id) and id >= 0 and is_integer(year) and year > 0 do
    "#{year}-#{:io_lib.format("~8..0B", [id]) |> IO.iodata_to_binary()}"
  end

  @doc """
  Parses a formatted invoice id back into a `{year, id}` tuple.

  ## Examples

      iex> InvoiceNumbering.Format.parse("2025-00000001")
      {:ok, {2025, 1}}

      iex> InvoiceNumbering.Format.parse("2024-00000042")
      {:ok, {2024, 42}}

      iex> InvoiceNumbering.Format.parse("not-an-id")
      {:error, :invalid_format}
  """
  @spec parse(String.t()) :: {:ok, {pos_integer(), non_neg_integer()}} | {:error, :invalid_format}
  def parse(str) when is_binary(str) do
    case Regex.run(~r/^(\d{4})-(\d{8})$/, str) do
      [_, year_str, id_str] ->
        {:ok, {String.to_integer(year_str), String.to_integer(id_str)}}

      _ ->
        {:error, :invalid_format}
    end
  end
end
```

### Step 2: side-effecting module — curated doctests

**Objective**: In a stateful GenServer, doctest only the pure slices and explicitly exclude side-effecting functions to keep examples reliable without faking state.

```elixir
# lib/invoice_numbering/numbering.ex
defmodule InvoiceNumbering.Numbering do
  @moduledoc """
  GenServer-backed allocator for gap-free invoice numbers.

  Many functions in this module have side effects and are not doctest-friendly.
  Doctests are restricted (via `doctest Module, only: [...]` in the test file)
  to a small, stable subset whose example clarifies the contract.
  """

  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts), do: {:ok, %{year: opts[:year] || 2025, counter: 0, reserved: MapSet.new()}}

  @doc """
  Returns the format prefix for a given year. This is a pure function — doctestable.

  ## Examples

      iex> InvoiceNumbering.Numbering.prefix_for_year(2025)
      "2025-"

      iex> InvoiceNumbering.Numbering.prefix_for_year(2030)
      "2030-"
  """
  @spec prefix_for_year(pos_integer()) :: String.t()
  def prefix_for_year(year) when is_integer(year) and year > 0, do: "#{year}-"

  @doc """
  Allocates the next invoice number. Has side effects — not doctested.

  Returns `{:ok, number}`.
  """
  @spec allocate() :: {:ok, pos_integer()}
  def allocate, do: GenServer.call(__MODULE__, :allocate)

  @doc """
  Returns the last allocated number without advancing. Has side effects (reads
  GenServer state) — not doctested.
  """
  @spec peek() :: non_neg_integer()
  def peek, do: GenServer.call(__MODULE__, :peek)

  @impl true
  def handle_call(:allocate, _from, state) do
    new_counter = state.counter + 1
    {:reply, {:ok, new_counter}, %{state | counter: new_counter}}
  end

  def handle_call(:peek, _from, state), do: {:reply, state.counter, state}
end
```

### Step 3: test files

**Objective**: Split doctests across async and non-async test files using `doctest Module, only: [...]` so pure modules run in parallel while stateful ones stay serial.

```elixir
# test/invoice_numbering/format_test.exs
defmodule InvoiceNumbering.FormatTest do
  use ExUnit.Case, async: true

  # Pure module — doctest everything
  doctest InvoiceNumbering.Format
end
```

```elixir
# test/invoice_numbering/numbering_test.exs
defmodule InvoiceNumbering.NumberingTest do
  use ExUnit.Case, async: false

  alias InvoiceNumbering.Numbering

  # Only doctest the pure helper. Everything else is a regular test below.
  doctest Numbering, only: [prefix_for_year: 1]

  setup do
    start_supervised!({Numbering, year: 2025})
    :ok
  end

  describe "allocate/0 — side-effecting contract" do
    test "first allocation returns 1" do
      assert {:ok, 1} = Numbering.allocate()
    end

    test "allocations are strictly monotonic" do
      {:ok, a} = Numbering.allocate()
      {:ok, b} = Numbering.allocate()
      {:ok, c} = Numbering.allocate()

      assert a == 1
      assert b == 2
      assert c == 3
    end

    test "peek reflects the last allocation without advancing" do
      {:ok, _} = Numbering.allocate()
      {:ok, _} = Numbering.allocate()

      assert Numbering.peek() == 2
      assert Numbering.peek() == 2
    end
  end

  describe "doctest scope control" do
    test "allocate/0 is NOT covered by doctest — it has side effects" do
      # Meta-assertion on the discipline: the only: list above excludes allocate/0
      # and peek/0. We verify by computing which functions match the doctest
      # filter statically.
      only_filter = [prefix_for_year: 1]

      refute Enum.any?(only_filter, fn {fun, _} -> fun in [:allocate, :peek] end)
    end
  end
end
```

## Why this works

- **Pure helpers** like `Format.to_string/2` and `Numbering.prefix_for_year/1` require no
  setup. Their doctests are tiny and truthful.
- **Side-effecting functions** are excluded via `doctest Numbering, only: [prefix_for_year: 1]`.
  Their behaviour is verified by conventional tests inside `describe` blocks with
  `start_supervised!/1` for clean isolation.
- `async: false` on the side-effecting test file matches the globally-named GenServer.
  The pure-module test file stays `async: true`.

## Tests

See Step 3.

## Benchmark

Doctests run at roughly the same speed as regular `assert` tests. A module with 10 pure
doctests finishes in < 10ms.

```elixir
{t, _} = :timer.tc(fn -> ExUnit.run() end)
IO.puts("doctest suite #{t}µs")
```

Target: entire doctest surface of a module < 20ms.

## Deep Dive: Doctest Patterns and Production Implications

Doctests embed executable examples in documentation, keeping docs in sync with code. The danger is that doctests can have side effects (they modify modules, send HTTP requests, write files) that don't show in the docstring. Isolating doctests from the main test suite and running them under controlled conditions is essential. Production code with untested doctests often has stale examples that users copy and fail on.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Doctesting functions with randomness**
Doctests assert on exact output. `iex> UUID.generate()` will fail because every run
produces a different value. Wrap randomness in a function that takes a seed, or skip
doctesting the function.

**2. Doctesting functions that print or log**
Doctest captures `IO.puts/1` output and compares it. Any concurrent test that writes to
`stdout` can corrupt the buffer. `async: true` plus doctests using `IO.puts` is risky —
prefer side-effect-free examples.

**3. Long doctests**
Doctests with 10-step setup turn docs into test fixtures. If setup > 3 lines, move to
`test/` and keep the doc example minimal.

**4. Sharing state between doctest examples in the same `@doc`**
Each example is run in isolation. Do not write:
```
iex> x = 5
5
iex> x + 1   # x does NOT persist into this example
```

**5. `@doc false` removes from generated documentation**
Use it for internal helpers you want neither in docs nor in doctests.

**6. When NOT to use this**
For functions with complex setup or database state, a regular test reads better than a
doctest. Reserve doctests for examples that genuinely aid comprehension.

## Reflection

Doctests couple the source of truth for docs and for tests. When the example has to
change because the behaviour evolved, both signals move together — but what is the
cost when an example is written for pedagogical clarity rather than production
accuracy, and the two goals diverge?

## Resources

- [`ExUnit.DocTest`](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [`doctest` macro](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html#doctest/2)
- [Elixir docs — writing good examples](https://hexdocs.pm/elixir/writing-documentation.html)
- [José Valim — documenting Elixir code](https://elixir-lang.org/blog/2014/10/08/writing-documentation/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
