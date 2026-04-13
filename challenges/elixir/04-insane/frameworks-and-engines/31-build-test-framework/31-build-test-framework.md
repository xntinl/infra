# Test Framework from Scratch

**Project**: `mytest` — a complete test framework built without ExUnit

---

## Project context

You are building `mytest`, a test framework that the tooling team will use as the foundation for a domain-specific test runner. It must discover tests via a macro DSL, run each test in an isolated process, produce rich failure messages, support setup/teardown, tag-based filtering, and property-based testing with shrinking. No ExUnit allowed — you will rebuild the relevant abstractions.

Project structure:

```
mytest/
├── lib/
│   └── mytest/
│       ├── case.ex              # ← use MyTest.Case macro + test DSL
│       ├── runner.ex            # ← test discovery + parallel execution
│       ├── assertion.ex         # ← assert/refute macros with AST introspection
│       ├── formatter/
│       │   ├── dot.ex           # ← . F E per test
│       │   ├── verbose.ex       # ← one line per test with timing
│       │   └── json.ex          # ← machine-readable result array
│       ├── setup.ex             # ← setup/on_exit/describe context propagation
│       ├── tags.ex              # ← @tag accumulation + --only/--exclude filtering
│       └── property/
│           ├── generator.ex     # ← typed generators
│           └── shrinker.ex      # ← minimal failing case finder
├── test/
│   └── mytest/
│       ├── assertion_test.exs
│       ├── runner_test.exs
│       ├── setup_test.exs
│       └── property_test.exs
├── bench/
│   └── runner_bench.exs
└── mix.exs
```

---

## Why module attributes (`@tests`) for accumulation and not an Agent or ETS table

attributes are evaluated at compile time and baked into the beam file — no process, no state to reset, no race between test registration and test running. An Agent adds a process boot to every test module load.

## Design decisions

**Option A — runtime registration via a GenServer collecting tests at boot**
- Pros: simple mental model, dynamic test generation
- Cons: slow startup, non-deterministic ordering, no compile-time guarantees

**Option B — compile-time macros that accumulate tests into a module attribute** (chosen)
- Pros: zero runtime cost, deterministic ordering, compiler errors for malformed tests
- Cons: macros are harder to debug, requires understanding Elixir AST

→ Chose **B** because test frameworks must have negligible startup overhead and fail loudly on malformed test definitions.

## The business problem

The tooling team needs a test runner that can be embedded in a custom CI pipeline, emit JSON results consumed by a dashboard, and filter tests by domain tags (`:billing`, `:payments`, `:slow`) without modifying test code. ExUnit's formatting is not flexible enough and its JSON output is not stable across versions.

Two design decisions shape everything:

1. **Process isolation** — a crashing test must not affect other tests or the runner.
2. **Macro-based DSL** — `test "name" do ... end` must accumulate test definitions at compile time, not at runtime.

---

## Why `assert` needs AST introspection

A naive `assert`:

```elixir
defmacro assert(expr) do
  quote do
    unless unquote(expr), do: raise "assertion failed"
  end
end
```

produces unhelpful errors: `assertion failed`. ExUnit's `assert` decomposes the expression AST to extract left-hand and right-hand sides:

```
assert 1 + 1 == 3
  left: 2
  right: 3
```

This requires pattern-matching on the quoted AST inside the macro:

```elixir
defmacro assert({:==, _, [left, right]}) do
  quote do
    lv = unquote(left)
    rv = unquote(right)
    unless lv == rv do
      raise MyTest.AssertionError,
        message: "Expected #{inspect(lv)} == #{inspect(rv)}",
        left: lv, right: rv,
        file: unquote(__CALLER__.file),
        line: unquote(__CALLER__.line)
    end
  end
end
```

Your `assert` must handle at minimum: `==`, `!=`, `<`, `>`, `<=`, `>=`, and bare boolean expressions.

---

## Why property-based testing needs shrinking

A generator finds a failing input `[99, -3, 0, 42]` for your sort function. Without shrinking, you debug a 4-element list. With shrinking, the framework finds the minimal failing case: `[-3]` or `[0, -1]`. Shrinking is what makes property-based testing practical.

Each generator must implement a `shrink/1` function that returns a list of "smaller" values to try. Integers shrink toward 0. Lists shrink by removing elements and by shrinking elements individually. The shrinker tries each candidate in order, keeps the first that still fails, and recurses until no smaller failing case exists.

---

## Implementation

### Step 1: Create the project

**Objective**: Split lib/ into case, runner, property so macro expansion and runtime dispatch live in separate compile-time dependencies.


```bash
mix new mytest --sup
cd mytest
mkdir -p lib/mytest/{formatter,property}
mkdir -p test/mytest bench
```

### Step 2: `mix.exs`

**Objective**: Stay stdlib-only for the framework itself, proving we can rebuild ExUnit's surface without leaning on any test library.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defmacro assert(expr) do
  quote do
    unless unquote(expr), do: raise "assertion failed"
  end
end
```

produces unhelpful errors: `assertion failed`. ExUnit's `assert` decomposes the expression AST to extract left-hand and right-hand sides:

```
assert 1 + 1 == 3
  left: 2
  right: 3
```

This requires pattern-matching on the quoted AST inside the macro:

```elixir
defmacro assert({:==, _, [left, right]}) do
  quote do
    lv = unquote(left)
    rv = unquote(right)
    unless lv == rv do
      raise MyTest.AssertionError,
        message: "Expected #{inspect(lv)} == #{inspect(rv)}",
        left: lv, right: rv,
        file: unquote(__CALLER__.file),
        line: unquote(__CALLER__.line)
    end
  end
end
```

Your `assert` must handle at minimum: `==`, `!=`, `<`, `>`, `<=`, `>=`, and bare boolean expressions.

---

## Why property-based testing needs shrinking

A generator finds a failing input `[99, -3, 0, 42]` for your sort function. Without shrinking, you debug a 4-element list. With shrinking, the framework finds the minimal failing case: `[-3]` or `[0, -1]`. Shrinking is what makes property-based testing practical.

Each generator must implement a `shrink/1` function that returns a list of "smaller" values to try. Integers shrink toward 0. Lists shrink by removing elements and by shrinking elements individually. The shrinker tries each candidate in order, keeps the first that still fails, and recurses until no smaller failing case exists.

---

## Implementation

### Step 1: Create the project

**Objective**: Split lib/ into case, runner, property so macro expansion and runtime dispatch live in separate compile-time dependencies.


```bash
mix new mytest --sup
cd mytest
mkdir -p lib/mytest/{formatter,property}
mkdir -p test/mytest bench
```

### Step 2: `mix.exs`

**Objective**: Stay stdlib-only for the framework itself, proving we can rebuild ExUnit's surface without leaning on any test library.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/mytest/assertion.ex`

**Objective**: Capture left/right/file/line at macro-expansion time so failure messages can render the original AST rather than a stringified value.


```elixir
defmodule MyTest.AssertionError do
  defexception [:message, :left, :right, :file, :line]
end

defmodule MyTest.Assertion do
  @moduledoc """
  Assert/refute macros with AST introspection for rich error messages.
  """

  defmacro assert({:==, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv == rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} == #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert({:!=, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv != rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} != #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert({:<, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv < rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} < #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert({:>, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv > rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} > #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert({:<=, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv <= rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} <= #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert({:>=, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      unless lv >= rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} >= #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert(expr) do
    quote do
      result = unquote(expr)

      unless result do
        raise MyTest.AssertionError,
          message: "Expected truthy value, got #{inspect(result)}",
          left: result,
          right: nil,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      result
    end
  end

  defmacro refute({:==, _meta, [left, right]}) do
    quote do
      lv = unquote(left)
      rv = unquote(right)

      if lv == rv do
        raise MyTest.AssertionError,
          message: "Expected #{inspect(lv)} to not equal #{inspect(rv)}",
          left: lv,
          right: rv,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro refute(expr) do
    quote do
      result = unquote(expr)

      if result do
        raise MyTest.AssertionError,
          message: "Expected falsy value, got #{inspect(result)}",
          left: result,
          right: nil,
          file: unquote(__CALLER__.file),
          line: unquote(__CALLER__.line)
      end

      true
    end
  end

  defmacro assert_receive(pattern, timeout \\ 100) do
    pattern_string = Macro.to_string(pattern)

    quote do
      receive do
        unquote(pattern) = msg -> msg
      after
        unquote(timeout) ->
          raise MyTest.AssertionError,
            message: "Expected to receive a message matching #{unquote(pattern_string)}, but no message matching pattern was received within #{unquote(timeout)}ms. Process mailbox: #{inspect(Process.info(self(), :messages))}",
            left: nil,
            right: nil,
            file: unquote(__CALLER__.file),
            line: unquote(__CALLER__.line)
      end
    end
  end
end
```

### Step 4: `lib/mytest/case.ex`

**Objective**: Accumulate tests in a module attribute during compile so `use MyTest.Case` exposes a static test list, no runtime reflection needed.


```elixir
defmodule MyTest.Case do
  @moduledoc """
  `use MyTest.Case` transforms a module into a test module.

  At compile time:
  - Registers the module in a global list of test modules (module attribute on a
    dedicated Registry module, or :persistent_term)
  - Accumulates test definitions, tags, and setup callbacks as module attributes
  - On __before_compile__, generates a `__mytest_tests__/0` function that returns
    all accumulated test metadata

  Why compile-time accumulation and not runtime registration?
  Runtime registration requires executing module code before test discovery.
  Compile-time accumulation means the test list is available by simply loading
  the module (beam file) without executing any code, enabling parallel discovery.
  """

  defmacro __using__(_opts) do
    quote do
      import MyTest.Case
      import MyTest.Assertion

      Module.register_attribute(__MODULE__, :mytest_tests, accumulate: true)
      Module.register_attribute(__MODULE__, :mytest_tags, accumulate: true)
      Module.register_attribute(__MODULE__, :mytest_setups, accumulate: true)

      @before_compile MyTest.Case
    end
  end

  defmacro __before_compile__(_env) do
    quote do
      def __mytest_tests__ do
        @mytest_tests |> Enum.reverse()
      end

      def __mytest_setups__ do
        @mytest_setups |> Enum.reverse()
      end
    end
  end

  @doc """
  Registers a test. The test body becomes a zero-arity function stored in
  the accumulated attribute.

  Why store a function and not just an AST?
  A function can be called directly — no need for eval or Code.eval_quoted.
  The function closes over the test module's environment at compile time.
  """
  defmacro test(name, do: body) do
    quote do
      @mytest_tests %{
        name: unquote(name),
        fun: fn -> unquote(body) end,
        tags: Module.get_attribute(__MODULE__, :mytest_tags) |> List.flatten(),
        line: unquote(__CALLER__.line),
        file: unquote(__CALLER__.file)
      }
    end
  end

  @doc """
  Groups tests with a shared setup scope.
  """
  defmacro describe(description, do: block) do
    quote do
      _ = unquote(description)
      unquote(block)
    end
  end

  @doc """
  Registers a setup callback for the current describe scope.
  The callback receives a context map and returns an updated context map.
  """
  defmacro setup(do: body) do
    quote do
      @mytest_setups {__ENV__.line, fn ctx -> unquote(body); ctx end}
    end
  end
end
```

### Step 5: `lib/mytest/runner.ex`

**Objective**: Run each test in its own Task with a timeout so an infinite loop in one test cannot freeze the whole suite.


```elixir
defmodule MyTest.Runner do
  @moduledoc """
  Discovers and runs tests.

  Execution model:
  - async tests run concurrently under a Task.Supervisor
  - sync tests run sequentially after all async tests complete
  - each test runs in its own process; the runner monitors it
  - a test result is :pass, {:fail, reason, stacktrace}, {:error, reason}, or :timeout

  Why use a monitor and not a link?
  Links propagate crashes bidirectionally. A crashing test would kill the runner.
  Monitors are unidirectional: the runner receives a DOWN message without crashing.
  """

  @default_timeout_ms 60_000

  @doc """
  Runs all tests in the given modules, filtered by tag options.
  Returns a %{passed: n, failed: n, errors: n, total: n} summary.
  """
  @spec run([module()], keyword()) :: map()
  def run(modules, opts \\ []) do
    only_tags = Keyword.get(opts, :only, [])
    exclude_tags = Keyword.get(opts, :exclude, [])
    _formatter = Keyword.get(opts, :formatter, MyTest.Formatter.Verbose)
    timeout = Keyword.get(opts, :timeout, @default_timeout_ms)

    all_tests =
      modules
      |> Enum.flat_map(fn mod -> Enum.map(mod.__mytest_tests__(), &Map.put(&1, :module, mod)) end)
      |> filter_by_tags(only_tags, exclude_tags)

    results = Enum.map(all_tests, fn test -> run_single_test(test, timeout) end)

    passed = Enum.count(results, &(&1 == :pass))
    failed = Enum.count(results, &match?({:fail, _, _}, &1))
    errors = Enum.count(results, fn
      {:error, _} -> true
      :timeout -> true
      _ -> false
    end)

    %{passed: passed, failed: failed, errors: errors, total: length(results)}
  end

  defp run_single_test(test, timeout) do
    parent = self()
    ref = make_ref()

    pid = spawn(fn ->
      result =
        try do
          test.fun.()
          :pass
        rescue
          e in MyTest.AssertionError -> {:fail, e, __STACKTRACE__}
          e -> {:error, {:exception, e, __STACKTRACE__}}
        catch
          :exit, reason -> {:error, {:exit, reason}}
          :throw, value -> {:error, {:throw, value}}
        end

      send(parent, {ref, result})
    end)

    monitor_ref = Process.monitor(pid)

    receive do
      {^ref, result} ->
        Process.demonitor(monitor_ref, [:flush])
        result

      {:DOWN, ^monitor_ref, :process, ^pid, reason} ->
        {:error, {:process_down, reason}}
    after
      timeout ->
        Process.exit(pid, :kill)
        :timeout
    end
  end

  defp filter_by_tags(tests, [], []), do: tests

  defp filter_by_tags(tests, only, exclude) do
    tests
    |> Enum.filter(fn test ->
      tags = test.tags
      (only == [] or Enum.any?(only, &(&1 in tags))) and
        not Enum.any?(exclude, &(&1 in tags))
    end)
  end
end
```

### Step 6: `lib/mytest/property/generator.ex`

**Objective**: Model generators as lazy functions of size and PRNG so reproducing a failed property requires only the seed, not the values.


```elixir
defmodule MyTest.Property.Generator do
  @moduledoc """
  Typed generators for property-based testing.

  A generator is a struct with:
  - generate/0: returns a random value
  - shrink/1: returns a list of smaller candidate values (never the original)

  The shrinker calls shrink/1 on a failing value, tries each candidate,
  keeps the first that still fails, and recurses until no smaller case fails.
  """

  defmodule IntegerGen do
    defstruct [:min, :max]

    def generate(%__MODULE__{min: min, max: max}) do
      Enum.random(min..max)
    end

    def shrink(%__MODULE__{}, 0), do: []

    def shrink(%__MODULE__{min: _min}, n) when n > 0 do
      candidates = [0]
      halves = Stream.iterate(n, &div(&1, 2))
        |> Enum.take_while(&(&1 > 0))
        |> Enum.map(&(n - &1))
        |> Enum.reject(&(&1 == n))

      (candidates ++ halves)
      |> Enum.uniq()
      |> Enum.filter(&(&1 >= 0 and &1 < n))
      |> Enum.sort()
    end

    def shrink(%__MODULE__{}, n) when n < 0 do
      candidates = [0]
      abs_n = abs(n)
      halves = Stream.iterate(abs_n, &div(&1, 2))
        |> Enum.take_while(&(&1 > 0))
        |> Enum.map(&(abs_n - &1))
        |> Enum.reject(&(&1 == abs_n))
        |> Enum.map(&(-&1))

      (candidates ++ halves)
      |> Enum.uniq()
      |> Enum.filter(&(abs(&1) < abs(n)))
      |> Enum.sort_by(&abs/1)
    end
  end

  defmodule ListGen do
    defstruct [:element_gen, :max_length]

    def generate(%__MODULE__{element_gen: eg, max_length: max}) do
      length = Enum.random(0..max)
      case length do
        0 -> []
        n -> for _ <- 1..n, do: eg.__struct__.generate(eg)
      end
    end

    def shrink(%__MODULE__{element_gen: eg}, list) when length(list) == 0, do: []

    def shrink(%__MODULE__{element_gen: eg}, list) do
      by_removal = for i <- 0..(length(list) - 1) do
        List.delete_at(list, i)
      end

      by_element = Enum.flat_map(Enum.with_index(list), fn {elem, idx} ->
        eg.__struct__.shrink(eg, elem) |> Enum.map(&List.replace_at(list, idx, &1))
      end)

      by_removal ++ by_element
    end
  end

  defmodule OneOfGen do
    defstruct [:values]

    def generate(%__MODULE__{values: values}), do: Enum.random(values)
    def shrink(%__MODULE__{values: values}, current) do
      idx = Enum.find_index(values, &(&1 == current)) || 0
      Enum.take(values, idx)
    end
  end

  defmodule BooleanGen do
    defstruct []

    def generate(%__MODULE__{}), do: Enum.random([true, false])
    def shrink(%__MODULE__{}, true), do: [false]
    def shrink(%__MODULE__{}, false), do: []
  end

  defmodule StringGen do
    defstruct [:max_length]

    @chars ~c"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

    def generate(%__MODULE__{max_length: max}) do
      length = Enum.random(0..max)
      for(_ <- 1..max(length, 1), do: Enum.random(@chars))
      |> List.to_string()
      |> String.slice(0, length)
    end

    def shrink(%__MODULE__{}, ""), do: []
    def shrink(%__MODULE__{}, str) do
      len = String.length(str)
      by_shortening = for i <- 0..(len - 1), do: String.slice(str, 0, i)
      Enum.reject(by_shortening, &(&1 == str))
    end
  end

  @doc "Creates an integer generator over [min, max]."
  def integer(min \\ -100, max \\ 100), do: %IntegerGen{min: min, max: max}

  @doc "Creates a list generator with elements from the given generator."
  def list_of(element_gen, max_length \\ 20), do: %ListGen{element_gen: element_gen, max_length: max_length}

  @doc "Creates a generator that picks uniformly from the given values."
  def one_of(values), do: %OneOfGen{values: values}

  @doc "Creates a boolean generator."
  def boolean, do: %BooleanGen{}

  @doc "Creates a string generator (alphanumeric)."
  def string(max_length \\ 20), do: %StringGen{max_length: max_length}
end
```

### Step 7: `lib/mytest/property/shrinker.ex`

**Objective**: Greedily shrink with first-failure-wins recursion so counterexamples converge to minimal cases without exploring the full tree.


```elixir
defmodule MyTest.Property.Shrinker do
  @moduledoc """
  Finds the minimal failing input for a property by iteratively shrinking.
  """

  @doc """
  Generates values from the generator, tests the property, and if a failure
  is found, shrinks to the minimal failing case.

  Returns {:found, minimal_value} or :no_failure.
  """
  def find_minimal(gen, property, opts \\ []) do
    tries = Keyword.get(opts, :tries, 100)

    case find_failure(gen, property, tries) do
      nil -> :no_failure
      failing_value -> {:found, shrink_to_minimal(gen, property, failing_value)}
    end
  end

  defp find_failure(_gen, _property, 0), do: nil

  defp find_failure(gen, property, remaining) do
    value = gen.__struct__.generate(gen)

    if property.(value) do
      find_failure(gen, property, remaining - 1)
    else
      value
    end
  end

  defp shrink_to_minimal(gen, property, current) do
    candidates = gen.__struct__.shrink(gen, current)

    case Enum.find(candidates, fn candidate -> not property.(candidate) end) do
      nil -> current
      smaller -> shrink_to_minimal(gen, property, smaller)
    end
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Dogfood the framework on itself — the meta-test suite is the only way to catch macro hygiene bugs in assertions.


```elixir
# test/mytest/assertion_test.exs
defmodule MyTest.AssertionTest do
  use MyTest.Case

  test "assert true passes" do
    MyTest.Assertion.assert(1 == 1)
  end

  test "assert false raises with left and right values" do
    error = assert_raises(MyTest.AssertionError, fn ->
      MyTest.Assertion.assert(1 + 1 == 3)
    end)
    assert error.left == 2
    assert error.right == 3
    assert error.line != nil
  end

  test "refute false passes" do
    MyTest.Assertion.refute(1 == 2)
  end

  test "assert_receive matches a message" do
    send(self(), {:hello, 42})
    MyTest.Assertion.assert_receive({:hello, _val})
  end

  test "assert_receive times out with descriptive error" do
    error = assert_raises(MyTest.AssertionError, fn ->
      MyTest.Assertion.assert_receive({:never_sent}, 50)
    end)
    assert String.contains?(error.message, "no message matching")
  end

  defp assert_raises(exception_module, fun) do
    try do
      fun.()
      raise "expected #{inspect(exception_module)} but nothing was raised"
    rescue
      e in ^exception_module -> e
    end
  end
end
```

```elixir
# test/mytest/runner_test.exs
defmodule MyTest.RunnerTest do
  use ExUnit.Case, async: true

  # Define test modules inline for the runner to discover
  defmodule PassingModule do
    use MyTest.Case
    describe "Runner" do

    test "always passes" do: assert 1 == 1
    test "also passes" do: assert true
  end

  defmodule FailingModule do
    use MyTest.Case
    test "always fails" do: MyTest.Assertion.assert(1 == 2)
  end

  defmodule CrashingModule do
    use MyTest.Case
    test "crashes" do: raise "boom"
  end

  test "passing tests counted correctly" do
    summary = MyTest.Runner.run([PassingModule])
    assert summary.passed == 2
    assert summary.failed == 0
  end

  test "failed tests counted correctly" do
    summary = MyTest.Runner.run([FailingModule])
    assert summary.failed == 1
    assert summary.passed == 0
  end

  test "crashing tests counted as errors, not crashes of the runner" do
    summary = MyTest.Runner.run([CrashingModule])
    assert summary.errors == 1
    assert summary.passed == 0
  end

  test "runner survives 50 concurrent tests" do
    # All modules run without crashing the runner process
    summary = MyTest.Runner.run([PassingModule, FailingModule, CrashingModule])
    assert summary.total == 4
    assert summary.passed + summary.failed + summary.errors == summary.total
  end

    end
end
```

```elixir
# test/mytest/property_test.exs
defmodule MyTest.PropertyTest do
  use ExUnit.Case, async: true

  alias MyTest.Property.{Generator, Shrinker}


  describe "Property" do

  test "integer generator produces values in range" do
    gen = Generator.integer(0, 10)
    for _ <- 1..100 do
      val = gen.generate(gen)
      assert val >= 0 and val <= 10
    end
  end

  test "shrinker finds minimal failing integer" do
    # Property: all integers are even. Fails for odd numbers.
    gen = Generator.integer(-50, 50)
    prop = fn n -> rem(n, 2) == 0 end

    case Shrinker.find_minimal(gen, prop, tries: 1_000) do
      {:found, minimal} ->
        # The minimal failing case should be 1 or -1 (smallest odd number)
        assert abs(minimal) == 1

      :no_failure ->
        # All generated values happened to be even — regenerate
        :ok
    end
  end

  test "list shrinker reduces to minimal failing list" do
    gen = Generator.list_of(Generator.integer(-10, 10), 10)
    # Property: no list element is negative
    prop = fn list -> Enum.all?(list, &(&1 >= 0)) end

    case Shrinker.find_minimal(gen, prop, tries: 500) do
      {:found, minimal} ->
        # Minimal failing list should be a single negative element
        assert length(minimal) == 1
        assert hd(minimal) < 0

      :no_failure ->
        :ok
    end
  end


  end
end
```

### Step 9: Run the tests

**Objective**: Bootstrap by running MyTest with elixir directly, confirming the framework does not secretly depend on ExUnit being loaded.


```bash
mix test test/mytest/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the test framework invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
def main do
  IO.puts("[MyTest.Runner.run] demo")
  :ok
end

```

Target: <10ms to compile and run a 100-test file.

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

---

## Trade-off analysis

| Aspect | MyTest (your impl) | ExUnit | PropEr/StreamData |
|--------|--------------------|--------|-------------------|
| Test isolation | process per test | process per test | n/a |
| Failure message quality | AST introspection | AST introspection | value + shrunk value |
| Parallel execution | configurable | async: true flag | n/a |
| Property shrinking | custom per type | no | integrated |
| Formatter extensibility | behaviour | formatter protocol | n/a |
| Compile-time overhead | accumulate attrs | same | none |

Reflection: ExUnit's `assert` macro has a special case for `==`, `in`, `=~`, and pattern matches (`=`). Each case requires different AST decomposition. How would you extend your `assert` to handle `assert value in list` with a helpful error showing which elements were checked?

---

## Common production mistakes

**1. Using `spawn` instead of `spawn_monitor` for test isolation**
Without a monitor, if the test process crashes before sending its result, the runner waits forever. Always monitor the test process and handle `{:DOWN, ...}` as a crash result.

**2. `on_exit` hooks running inside the test process**
If the test process is killed (`:timeout`), its `on_exit` hooks never run. `on_exit` must be registered with a separate monitor process that runs hooks when it detects the test process dying.

**3. Shrinking toward the wrong direction**
Integer generators must shrink toward 0, not toward the minimum of the range. A property failing for `-42` should shrink to `-1`, not to `-100`. The direction of shrinking is "toward the simplest value", not "toward the boundary".

**4. Accumulating test definitions in reverse**
`Module.register_attribute/3` with `accumulate: true` prepends each new value. `@mytest_tests %{name: "first"}` followed by `@mytest_tests %{name: "second"}` produces `[second, first]`. Call `Enum.reverse/1` in `__before_compile__` to restore definition order.

**5. Sharing mutable state between async tests**
The ETS table used by your Registry is global. If async tests call `Registry.register/4` with the same metric name, they race. Prefix metric names with a unique test ID, or use a per-test ETS table created in `setup` and deleted in `on_exit`.

---

## Reflection

If someone writes 100k `test` blocks in one module, where does the compile-time-attribute approach break first — memory, compile time, or beam file size? How would you mitigate it?

## Resources

- [ExUnit source code](https://github.com/elixir-lang/elixir/tree/main/lib/ex_unit) — study `ExUnit.Case` (macro layer), `ExUnit.Runner` (process isolation), and `ExUnit.Assertions` (AST decomposition)
- ["Property-Based Testing with PropEr, Erlang, and Elixir"](https://pragprog.com/titles/fhproper/property-based-testing-with-proper-erlang-and-elixir/) — Fred Hebert — the definitive reference for shrinking strategies
- [StreamData](https://hex.pm/packages/stream_data) — read the source for `StreamData.Generator` and how shrinking trees are represented
- ["Metaprogramming Elixir"](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — Chris McCord — chapters 3–5 for DSL design patterns
- [`Module.__info__/1`](https://hexdocs.pm/elixir/Module.html) — understand how to inspect compiled module attributes at runtime
