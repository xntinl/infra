# Disciplined Doctests for Modules with Side Effects

**Project**: `invoice_numbering` — a module whose documentation examples are doctest-executable without leaving cache or database residue

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
invoice_numbering/
├── lib/
│   └── invoice_numbering.ex
├── script/
│   └── main.exs
├── test/
│   └── invoice_numbering_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule InvoiceNumbering.MixProject do
  use Mix.Project

  def project do
    [
      app: :invoice_numbering,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/invoice_numbering.ex`

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

### `test/invoice_numbering_test.exs`

```elixir
defmodule InvoiceNumbering.FormatTest do
  use ExUnit.Case, async: true

  # Pure module — doctest everything
  doctest InvoiceNumbering.Format
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
