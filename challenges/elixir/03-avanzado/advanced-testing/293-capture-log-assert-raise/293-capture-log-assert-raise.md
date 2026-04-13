# Disciplined Testing of Logs and Exceptions — capture_log and assert_raise

**Project**: `audit_log` — an audit subsystem whose tests assert precisely on the log output and on raised exceptions.

## Project context

`audit_log` records security-relevant events. The compliance team requires that every
`:forbidden` response be logged at `:warning` level with the user id, the action, and a
correlation id. Missing logs in production make audits fail, so the tests must prove the
logging contract — not just the return value.

Simultaneously, some functions in the system are expected to **raise** on invalid input
(fail-fast discipline). Tests must assert on the exception type AND on the message shape,
since loose assertions let messages drift and lose diagnostic value.

Two ExUnit tools cover both: `capture_log/2` and `assert_raise/3`.

```
audit_log/
├── lib/
│   └── audit_log/
│       ├── recorder.ex              # emits logs at multiple levels
│       └── validator.ex             # raises on bad inputs
├── test/
│   ├── audit_log/
│   │   ├── recorder_test.exs
│   │   └── validator_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why capture_log and not global ETS handlers

- `ExUnit.CaptureLog.capture_log/2` captures messages emitted during the function call,
  scoped to the **caller process** — safe with `async: true`.
- Custom Logger backends that write to ETS are process-global; they break async tests.
- `:meck.new(Logger, [:passthrough])` works but bypasses the whole Logger pipeline.

## Why assert_raise and not try/rescue

- `try/rescue` in a test is verbose and forgiving — you might accidentally swallow the wrong
  exception and pass the test.
- `assert_raise/3` with a regex on the message is exact and reads like a spec.

## Core concepts

### 1. capture_log is synchronous and pid-scoped
The string returned contains all messages emitted by the calling process (and by processes
using the same logger group leader) during the function call.

### 2. Log levels matter
Capturing `:warning` but only emitting `:debug` returns an empty string. Configure the test
logger to the lowest level you want to assert on.

### 3. assert_raise takes a regex, not a string
`assert_raise ArgumentError, ~r/expected positive integer/`
The regex anchors your assertion — the exact wording can evolve without breaking tests.

## Design decisions

- **Option A — assert only on return values**: fastest but ignores the observability contract.
  Missing logs in prod, test is green.
- **Option B — dedicated Logger backend for tests**: works but async-unsafe.
- **Option C — `capture_log/2` at the call site**: async-safe, targeted, reads like a spec.

Chosen: **Option C**.

For exceptions:
- **Option A — `try/rescue` + manual flag**: verbose, error-prone.
- **Option B — `assert_raise/3`**: canonical, concise.

Chosen: **Option B**.

## Implementation

### Dependencies (`mix.exs`)

```elixir
# Built-in tools — no extra deps needed
```

Set log level in the test helper so `:debug` and above are captured:

```elixir
# test/test_helper.exs
ExUnit.start(capture_log: true)
Logger.configure(level: :debug)
```

### Step 1: the recorder emits structured logs

**Objective**: Split `:allowed` vs `:forbidden` branches across `Logger.info` and `Logger.warning` so level-aware assertions can prove severity, not just content.

```elixir
# lib/audit_log/recorder.ex
defmodule AuditLog.Recorder do
  require Logger

  @type event :: %{user_id: String.t(), action: String.t(), result: :allowed | :forbidden}

  @spec record(event(), String.t()) :: :ok
  def record(%{result: :forbidden} = event, correlation_id) do
    Logger.warning(
      "audit: forbidden action user=#{event.user_id} action=#{event.action} cid=#{correlation_id}"
    )
    :ok
  end

  def record(%{result: :allowed} = event, correlation_id) do
    Logger.info(
      "audit: action allowed user=#{event.user_id} action=#{event.action} cid=#{correlation_id}"
    )
    :ok
  end
end
```

### Step 2: the validator raises on contract violations

**Objective**: Raise a struct-bearing `InvalidEventError` with `:field` so adapters can pattern-match on the violation instead of parsing error messages.

```elixir
# lib/audit_log/validator.ex
defmodule AuditLog.Validator do
  @moduledoc """
  Validates an audit event. Fails fast via exceptions — the caller is a framework
  adapter that translates raises into HTTP 400 responses.
  """

  defmodule InvalidEventError do
    defexception [:message, :field]

    @impl true
    def exception(opts) do
      field = Keyword.fetch!(opts, :field)
      reason = Keyword.fetch!(opts, :reason)
      %__MODULE__{message: "invalid #{field}: #{reason}", field: field}
    end
  end

  @spec validate!(map()) :: :ok | no_return()
  def validate!(%{user_id: uid}) when not is_binary(uid) or uid == "" do
    raise InvalidEventError, field: :user_id, reason: "must be a non-empty string"
  end

  def validate!(%{action: a}) when not is_binary(a) or a == "" do
    raise InvalidEventError, field: :action, reason: "must be a non-empty string"
  end

  def validate!(%{result: r}) when r not in [:allowed, :forbidden] do
    raise InvalidEventError, field: :result, reason: "must be :allowed or :forbidden"
  end

  def validate!(_), do: :ok
end
```

### Step 3: log assertion tests

**Objective**: Use `capture_log/2` with explicit `:level` so async tests assert both presence of the expected line and absence of the wrong-severity one.

```elixir
# test/audit_log/recorder_test.exs
defmodule AuditLog.RecorderTest do
  use ExUnit.Case, async: true

  import ExUnit.CaptureLog

  alias AuditLog.Recorder

  describe "record/2 — forbidden actions" do
    test "emits a warning with user, action, and correlation id" do
      log =
        capture_log([level: :warning], fn ->
          Recorder.record(
            %{user_id: "u_42", action: "delete_account", result: :forbidden},
            "req_abc"
          )
        end)

      assert log =~ "[warning]"
      assert log =~ "user=u_42"
      assert log =~ "action=delete_account"
      assert log =~ "cid=req_abc"
    end

    test "does NOT emit an info line for a forbidden event" do
      log =
        capture_log([level: :info], fn ->
          Recorder.record(
            %{user_id: "u_1", action: "x", result: :forbidden},
            "cid_1"
          )
        end)

      refute log =~ "[info]"
    end
  end

  describe "record/2 — allowed actions" do
    test "emits exactly one info line, no warning" do
      log =
        capture_log([level: :debug], fn ->
          Recorder.record(
            %{user_id: "u_9", action: "login", result: :allowed},
            "cid_login"
          )
        end)

      assert log =~ "[info]"
      refute log =~ "[warning]"
      assert log =~ "user=u_9"
    end
  end
end
```

### Step 4: exception assertion tests

**Objective**: Combine `assert_raise/3` with regex and struct-level rescue so both the message shape and the `:field` tag form part of the contract.

```elixir
# test/audit_log/validator_test.exs
defmodule AuditLog.ValidatorTest do
  use ExUnit.Case, async: true

  alias AuditLog.Validator
  alias AuditLog.Validator.InvalidEventError

  describe "validate!/1 — user_id" do
    test "raises when user_id is not a binary" do
      assert_raise InvalidEventError, ~r/invalid user_id: must be a non-empty string/, fn ->
        Validator.validate!(%{user_id: 123, action: "x", result: :allowed})
      end
    end

    test "raises when user_id is an empty string" do
      assert_raise InvalidEventError, ~r/invalid user_id/, fn ->
        Validator.validate!(%{user_id: "", action: "x", result: :allowed})
      end
    end
  end

  describe "validate!/1 — action" do
    test "raises when action is nil" do
      assert_raise InvalidEventError, ~r/invalid action/, fn ->
        Validator.validate!(%{user_id: "u", action: nil, result: :allowed})
      end
    end
  end

  describe "validate!/1 — result" do
    test "raises when result is not in the whitelist" do
      assert_raise InvalidEventError, ~r/invalid result/, fn ->
        Validator.validate!(%{user_id: "u", action: "a", result: :unknown})
      end
    end
  end

  describe "validate!/1 — structured exception access" do
    test "exposes the failing field on the struct" do
      error =
        try do
          Validator.validate!(%{user_id: "", action: "x", result: :allowed})
        rescue
          e -> e
        end

      assert %InvalidEventError{field: :user_id} = error
    end
  end

  describe "validate!/1 — happy path" do
    test "returns :ok for a fully valid event" do
      assert :ok =
               Validator.validate!(%{user_id: "u", action: "login", result: :allowed})
    end
  end
end
```

## Why this works

`capture_log/2` swaps the test process's logger config for the duration of the anonymous
function. Because the swap is pid-scoped, two `async: true` tests capturing logs in parallel
do not see each other's output. The returned string is plain text, asserted with the
`=~` and `refute ... =~` operators.

`assert_raise/3` takes an expected module and a regex. If the callback does not raise, the
assertion fails. If it raises a different exception, the assertion fails. If the message
does not match the regex, the assertion fails. Three orthogonal checks in one assertion.

## Tests

See Step 3 and Step 4. Forbidden logs, allowed logs, and five exception scenarios covered.

## Benchmark

`capture_log/2` adds roughly 20µs of overhead per call (logger swap + capture buffer).
Negligible for a 100-test suite:

```elixir
Benchee.run(%{
  "capture_log noop" => fn ->
    ExUnit.CaptureLog.capture_log(fn -> :ok end)
  end
}, time: 2)
```

Target: < 50µs/op on a modern laptop.

## Deep Dive: Logging Patterns and Production Implications

Capturing logs during tests ensures that error messages and diagnostics are actionable for operators. The trap is logging too much (noise) or too little (invisibility). Senior teams log at boundaries (HTTP handlers, database calls) and sparingly on happy paths, then rigorously test error paths to ensure operators see actionable information. Under production load, verbose logging becomes a bottleneck; testing log output early prevents information overload in critical moments.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Default log level too high**
If your Logger backend is configured at `:notice` in the test env, `capture_log` captures
nothing below that level. Pass `level: :debug` explicitly to `capture_log/2` when testing
`:debug`/`:info` emissions.

**2. Capturing logs across processes**
`capture_log` captures the caller's logger group leader. A GenServer started outside the
capture block uses its own group leader — its logs are NOT captured. Either capture inside
the setup block that also starts the process, or attach a telemetry handler.

**3. Asserting on exact log strings**
Loggers add timestamps, levels, and metadata formatters. Exact-string asserts break on
any format change. Use substrings with `=~` or regex.

**4. `assert_raise` with only a module, no regex**
`assert_raise ArgumentError, fn -> ... end` passes for ANY ArgumentError — including
unrelated ones raised by library code. Always pair with a regex on the message.

**5. Swallowing exceptions in production code to make tests pass**
If a test was previously asserting on a raise and you make it green by catching the
exception in the source, you've degraded the contract. Tests should drive the
production behaviour, not the other way around.

**6. When NOT to use this**
If the code uses `Logger.metadata/1` for structured fields and a JSON console backend, a
string-based log capture drops the structure. Switch to a test Logger backend that captures
metadata, or use `ExUnit.CaptureIO` where applicable.

## Reflection

`capture_log` is pid-scoped, but Logger macros can be called from spawned tasks. How would
you write a test that asserts on the combined logs of the test process plus a child Task,
without falling back to `Process.sleep`?

## Resources

- [`ExUnit.CaptureLog`](https://hexdocs.pm/ex_unit/ExUnit.CaptureLog.html)
- [`ExUnit.Assertions.assert_raise/3`](https://hexdocs.pm/ex_unit/ExUnit.Assertions.html#assert_raise/3)
- [`Logger` configuration](https://hexdocs.pm/logger/Logger.html)
- [Structured logging — Dashbit](https://dashbit.co/blog/structured-logging-with-elixir)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
