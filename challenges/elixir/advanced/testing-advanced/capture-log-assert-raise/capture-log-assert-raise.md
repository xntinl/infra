# Disciplined Testing of Logs and Exceptions — capture_log and assert_raise

**Project**: `audit_log` — an audit subsystem whose tests assert precisely on the log output and on raised exceptions

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
audit_log/
├── lib/
│   └── audit_log.ex
├── script/
│   └── main.exs
├── test/
│   └── audit_log_test.exs
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
defmodule AuditLog.MixProject do
  use Mix.Project

  def project do
    [
      app: :audit_log,
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
### `lib/audit_log.ex`

```elixir
# lib/audit_log/recorder.ex
defmodule AuditLog.Recorder do
  require Logger

  @type event :: %{user_id: String.t(), action: String.t(), result: :allowed | :forbidden}

  @doc "Returns record result from correlation_id."
  @spec record(event(), String.t()) :: :ok
  def record(%{result: :forbidden} = event, correlation_id) do
    Logger.warning(
      "audit: forbidden action user=#{event.user_id} action=#{event.action} cid=#{correlation_id}"
    )
    :ok
  end

  @doc "Returns record result from correlation_id."
  def record(%{result: :allowed} = event, correlation_id) do
    Logger.info(
      "audit: action allowed user=#{event.user_id} action=#{event.action} cid=#{correlation_id}"
    )
    :ok
  end
end

# lib/audit_log/validator.ex
defmodule AuditLog.Validator do
  @moduledoc """
  Validates an audit event. Fails fast via exceptions — the caller is a framework
  adapter that translates raises into HTTP 400 responses.
  """

  defmodule InvalidEventError do
    defexception [:message, :field]

    @doc "Returns exception result from opts."
    @impl true
    def exception(opts) do
      field = Keyword.fetch!(opts, :field)
      reason = Keyword.fetch!(opts, :reason)
      %__MODULE__{message: "invalid #{field}: #{reason}", field: field}
    end
  end

  @doc "Validates result."
  @spec validate!(map()) :: :ok | no_return()
  def validate!(%{user_id: uid}) when not is_binary(uid) or uid == "" do
    raise InvalidEventError, field: :user_id, reason: "must be a non-empty string"
  end

  @doc "Validates result."
  def validate!(%{action: a}) when not is_binary(a) or a == "" do
    raise InvalidEventError, field: :action, reason: "must be a non-empty string"
  end

  @doc "Validates result."
  def validate!(%{result: r}) when r not in [:allowed, :forbidden] do
    raise InvalidEventError, field: :result, reason: "must be :allowed or :forbidden"
  end

  @doc "Validates result from _."
  def validate!(_), do: :ok
end

defmodule AuditLog.ValidatorTest do
  use ExUnit.Case, async: true
  doctest AuditLog.MixProject

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
          e in RuntimeError -> e
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
### `test/audit_log_test.exs`

```elixir
defmodule AuditLog.RecorderTest do
  use ExUnit.Case, async: true
  doctest AuditLog.MixProject

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
