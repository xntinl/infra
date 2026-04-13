# Contract Testing with Mox and Behaviours

**Project**: `notification_service` — a notification dispatcher whose SMS and email adapters are mocked via contracts defined as Elixir behaviours

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
notification_service/
├── lib/
│   └── notification_service.ex
├── script/
│   └── main.exs
├── test/
│   └── notification_service_test.exs
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
defmodule NotificationService.MixProject do
  use Mix.Project

  def project do
    [
      app: :notification_service,
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

### `lib/notification_service.ex`

```elixir
# lib/notification_service/adapter.ex
defmodule NotificationService.Adapter do
  @moduledoc "Contract every notification adapter must satisfy."

  @type recipient :: String.t()
  @type payload :: %{required(:subject) => String.t(), required(:body) => String.t()}
  @type reason :: :rate_limited | :invalid_recipient | :provider_down | atom()

  @callback send(recipient(), payload()) :: {:ok, message_id :: String.t()} | {:error, reason()}
  @callback healthcheck() :: :ok | {:error, term()}
end

# lib/notification_service/sms/twilio_adapter.ex
defmodule NotificationService.Sms.TwilioAdapter do
  @behaviour NotificationService.Adapter

  @doc "Sends result from phone."
  @impl true
  def send(phone, %{body: body}) do
    # Simplified — production calls Twilio via Req
    case Req.post("https://api.twilio.com/send", json: %{to: phone, body: body}) do
      {:ok, %{status: 200, body: %{"sid" => sid}}} -> {:ok, sid}
      {:ok, %{status: 429}} -> {:error, :rate_limited}
      _ -> {:error, :provider_down}
    end
  end

  @doc "Returns healthcheck result."
  @impl true
  def healthcheck do
    case Req.get("https://status.twilio.com/api/v2/status.json") do
      {:ok, %{status: 200}} -> :ok
      _ -> {:error, :provider_down}
    end
  end
end

# lib/notification_service/dispatcher.ex
defmodule NotificationService.Dispatcher do
  @moduledoc "Dispatches a notification to the configured adapter per channel."

  @type channel :: :sms | :email

  @doc "Returns dispatch result from recipient and payload."
  @spec dispatch(channel(), String.t(), map()) :: {:ok, String.t()} | {:error, term()}
  def dispatch(:sms, recipient, payload), do: sms_adapter().send(recipient, payload)
  @doc "Returns dispatch result from recipient and payload."
  def dispatch(:email, recipient, payload), do: email_adapter().send(recipient, payload)

  defp sms_adapter,   do: Application.fetch_env!(:notification_service, :sms_adapter)
  defp email_adapter, do: Application.fetch_env!(:notification_service, :email_adapter)
end
```

### `test/notification_service_test.exs`

```elixir
defmodule NotificationService.DispatcherTest do
  use ExUnit.Case, async: true
  doctest NotificationService.Adapter

  import Mox

  # Every test body is isolated by Mox.set_mox_from_context (process dictionary).
  setup :set_mox_from_context
  setup :verify_on_exit!

  alias NotificationService.{Dispatcher, SmsMock, EmailMock}

  describe "dispatch/3 — happy path" do
    test "routes :sms to the SMS adapter with the recipient and payload" do
      expect(SmsMock, :send, fn "+5491150001234", %{subject: _, body: "hello"} ->
        {:ok, "SM123"}
      end)

      assert {:ok, "SM123"} =
               Dispatcher.dispatch(:sms, "+5491150001234", %{subject: "hi", body: "hello"})
    end

    test "routes :email to the email adapter without touching SMS" do
      expect(EmailMock, :send, fn "user@example.com", _payload -> {:ok, "MSG_9"} end)

      assert {:ok, "MSG_9"} =
               Dispatcher.dispatch(:email, "user@example.com", %{subject: "s", body: "b"})
    end
  end

  describe "dispatch/3 — error propagation" do
    test "returns provider rate-limit errors unchanged" do
      expect(SmsMock, :send, fn _to, _payload -> {:error, :rate_limited} end)

      assert {:error, :rate_limited} =
               Dispatcher.dispatch(:sms, "+100", %{subject: "x", body: "y"})
    end

    test "returns provider-down when adapter signals it" do
      expect(EmailMock, :send, fn _, _ -> {:error, :provider_down} end)

      assert {:error, :provider_down} =
               Dispatcher.dispatch(:email, "u@e", %{subject: "x", body: "y"})
    end
  end

  describe "dispatch/3 — strict expectation semantics" do
    test "unexpected extra call fails the test" do
      expect(SmsMock, :send, 1, fn _, _ -> {:ok, "SM1"} end)

      Dispatcher.dispatch(:sms, "+1", %{subject: "a", body: "b"})

      assert_raise Mox.UnexpectedCallError, fn ->
        Dispatcher.dispatch(:sms, "+1", %{subject: "a", body: "b"})
      end
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Contract Testing with Mox and Behaviours.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Contract Testing with Mox and Behaviours ===")
    IO.puts("Category: Advanced testing\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NotificationService.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: NotificationService.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
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
