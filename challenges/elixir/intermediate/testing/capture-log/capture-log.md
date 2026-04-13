# Testing log output with `ExUnit.CaptureLog`

**Project**: `capture_log_demo` — a `PaymentGateway` module that logs each
attempt, tested with `ExUnit.CaptureLog` to assert on log content.

---

## Why capture log matters

Logging is part of behavior. When a payment fails, you don't just want the
error tuple back — you want a log line a human can search for at 3 AM.
Production incidents are solved by finding log messages; tests that verify
the log exists are first-line observability.

`ExUnit.CaptureLog` is the idiomatic answer. It captures the log output
of a block, returns it as a string, and suppresses it from the console.
You assert on the string. No mocking Logger, no weird handlers.

## Why `ExUnit.CaptureLog` and not X

**Why not mock `Logger` itself?** Logger is a complex async pipeline
(backends, metadata, formatters). Mocking it means reimplementing half
of OTP in the test. CaptureLog drops in at the right seam.

**Why not a custom `Logger` backend in `test_helper.exs`?** You could, but
now every test shares one backend and you lose isolation across `async`
tests. CaptureLog is per-block and per-process.

**Why not just disable logging in tests?** Because the log line **is** part
of the contract for an incident-debuggable system. Asserting on it is the
cheapest observability test you can write.

---

## Project structure

```
capture_log_demo/
├── lib/
│   └── capture_log_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── capture_log_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `capture_log/1` returns the captured string

```elixir
import ExUnit.CaptureLog

log = capture_log(fn -> Logger.error("boom") end)
assert log =~ "boom"
```

The log is returned AND suppressed from the console. `=~` is the match
operator — it works with regexes or substrings, which is what you want
for log assertions (exact matches are brittle).

### 2. `capture_log/2` can filter by level

```elixir
log = capture_log([level: :error], fn -> run() end)
```

Only messages at `:error` or higher are captured. Useful to assert "we
didn't log any warnings during this happy path".

### 3. `async: true` needs `capture_log: true`

Loggers are shared. Two async tests running `Logger.error` simultaneously
would bleed messages into each other's captures. ExUnit handles this if
you `use ExUnit.Case, async: true` AND set `capture_log: true` in
`test_helper.exs` — each test gets its own logical log stream.

### 4. What NOT to assert on

Don't assert on timestamps, PIDs, or the exact output format — Logger
formatters change. Assert on the **business message**: the error code,
the user id, the action name.

---

## Design decisions

**Option A — Assert exact log strings**
- Pros: Precise; catches formatting regressions.
- Cons: Breaks on every Logger formatter tweak; tests become maintenance
  burden.

**Option B — Assert on stable phrases (`=~`)** (chosen)
- Pros: Robust to formatter changes; captures the behavioral contract.
- Cons: A typo in the expected substring could match by accident.

→ Chose **B**. The log line is part of behavior, but the formatter is not
your concern — assert on business-relevant substrings (error code, user id)
and use regex (`~r/amount=\d+/`) when you need structural matches.

---

## Implementation

### `mix.exs`

```elixir
defmodule CaptureLogDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :capture_log_demo,
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
mix new capture_log_demo
cd capture_log_demo
```

### `lib/payment_gateway.ex`

**Objective**: Implement `payment_gateway.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.

```elixir
defmodule PaymentGateway do
  @moduledoc """
  A stub payment gateway that logs each attempt. Used to demonstrate
  `ExUnit.CaptureLog`. Deterministic — no randomness.
  """
  require Logger

  @type amount :: pos_integer()
  @type result :: {:ok, String.t()} | {:error, :declined | :invalid_amount}

  @doc "Returns charge result from _user_id and amount."
  @spec charge(String.t(), amount()) :: result()
  def charge(_user_id, amount) when amount <= 0 do
    Logger.warning("payment rejected: invalid_amount amount=#{amount}")
    {:error, :invalid_amount}
  end

  @doc "Returns charge result from user_id and amount."
  def charge(user_id, amount) when rem(amount, 13) == 0 do
    Logger.error("payment declined user_id=#{user_id} amount=#{amount}")
    {:error, :declined}
  end

  @doc "Returns charge result from user_id and amount."
  def charge(user_id, amount) do
    Logger.info("payment ok user_id=#{user_id} amount=#{amount}")
    {:ok, "txn_#{user_id}_#{amount}"}
  end
end
```

### Step 3: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the subject under test — shaped specifically to make the testing technique of this lab observable.

```elixir
# capture_log: true makes `Logger` output invisible by default AND isolates
# log streams across async tests.
ExUnit.start(capture_log: true)
```

### Step 4: `test/payment_gateway_test.exs`

**Objective**: Write `payment_gateway_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.

```elixir
defmodule PaymentGatewayTest do
  use ExUnit.Case, async: true

  doctest PaymentGateway
  import ExUnit.CaptureLog

  describe "charge/2" do
    test "logs an info line on success" do
      log =
        capture_log(fn ->
          assert {:ok, _txn} = PaymentGateway.charge("user_1", 100)
        end)

      assert log =~ "payment ok"
      assert log =~ "user_id=user_1"
      assert log =~ "amount=100"
    end

    test "logs an error line when declined" do
      log =
        capture_log(fn ->
          assert {:error, :declined} = PaymentGateway.charge("user_2", 26)
        end)

      assert log =~ "payment declined"
      assert log =~ "[error]"
    end

    test "logs a warning for invalid amount" do
      # Filter the capture to warning+ so info lines don't pollute.
      log =
        capture_log([level: :warning], fn ->
          assert {:error, :invalid_amount} = PaymentGateway.charge("user_3", -5)
        end)

      assert log =~ "invalid_amount"
      refute log =~ "payment ok"
    end

    test "no errors are logged on the happy path" do
      log =
        capture_log([level: :error], fn ->
          PaymentGateway.charge("user_4", 50)
        end)

      # Nothing at :error level or above should appear.
      assert log == ""
    end
  end

  describe "regex matching on logs" do
    test "error log contains a transaction amount that parses as an integer" do
      log =
        capture_log(fn ->
          PaymentGateway.charge("user_5", 39)
        end)

      assert Regex.match?(~r/amount=\d+/, log)
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
mix test --trace
```

### Why this works

`ExUnit.CaptureLog` temporarily attaches a capture backend scoped to the
calling process and the duration of the fun. Messages emitted inside the
block go to the capture instead of the normal backends; the function
returns the captured string after a flush. Because the capture is
process-scoped, `async: true` tests can use it in parallel without
cross-contamination — as long as `capture_log: true` is in
`test_helper.exs`.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `CaptureLogDemo`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== CaptureLogDemo demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    :ok
  end
end

Main.main()
```

## Benchmark

<!-- benchmark N/A: tema sobre aserciones en tests; no hay workload a medir. -->

---

## Trade-offs and production gotchas

**1. `capture_log: true` in `test_helper.exs` hides ALL logs by default**
Which is exactly what you want in CI, but a surprise when debugging
locally. Temporarily set `ExUnit.configure(capture_log: false)` or use
`mix test --trace` + `Logger.configure(level: :debug)` to inspect live.

**2. Logger is asynchronous**
`Logger.info/1` returns before the message is actually flushed to handlers.
`capture_log/1` handles the sync internally, but if you roll your own
assertions, you might need `Logger.flush/0` first.

**3. `=~` is a substring/regex match — don't pin your tests to whitespace**
`assert log =~ "payment ok"` is resilient to formatter changes. Don't do
`assert log == "…exact multiline string…"`.

**4. Don't over-assert on log content**
Each assertion ties your test to a specific wording. Three assertions
per log line means every log tweak becomes a 15-file PR. Assert on one
stable phrase per log line.

**5. When NOT to use CaptureLog**
For structured logging (`Logger.metadata/1`, JSON formatters), you're
better off attaching a test-only Logger backend that captures structured
events, or using `:telemetry` which is designed for this. CaptureLog is
for human-readable messages.

---

## Reflection

- Your team rolls out a new JSON logger for production and half the
  CaptureLog assertions break. What changes in the assertion style to
  survive both backends, and is there a deeper problem the test was
  supposed to catch?
- You want to verify that "no warnings are emitted on the happy path"
  for a critical flow. Write the smallest CaptureLog test that encodes
  that invariant, and explain how it fails when someone adds a stray
  `Logger.warning/1`.

---
## Resources

- [`ExUnit.CaptureLog`](https://hexdocs.pm/ex_unit/ExUnit.CaptureLog.html)
- [`ExUnit.CaptureIO`](https://hexdocs.pm/ex_unit/ExUnit.CaptureIO.html) — same idea for stdout/stderr
- [`Logger`](https://hexdocs.pm/logger/Logger.html)
- [`:telemetry`](https://hexdocs.pm/telemetry/) — for structured/metric-style assertions

## Key concepts
ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/capture_log_demo_test.exs`

```elixir
defmodule CaptureLogDemoTest do
  use ExUnit.Case, async: true

  doctest CaptureLogDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CaptureLogDemo.run(:noop) == :ok
    end
  end
end
```
