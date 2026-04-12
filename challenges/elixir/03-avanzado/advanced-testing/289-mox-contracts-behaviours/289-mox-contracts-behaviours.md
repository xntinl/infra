# Contract Testing with Mox and Behaviours

**Project**: `notification_service` — a notification dispatcher whose SMS and email adapters are mocked via contracts defined as Elixir behaviours.

## Project context

You maintain `notification_service`, which fans out user alerts to SMS (Twilio), email
(SendGrid), and push (Firebase) providers. The production adapters make HTTP calls. You
cannot let tests hit the network and you cannot swap adapters ad-hoc with `Application.put_env/3`
hacks without losing type safety — the test suite has 200+ cases and once broke silently
because a new callback was added to the real adapter but the mock never implemented it.

Mox solves this by generating mocks that are **verified against a behaviour at compile time**.
If a mock is used without a corresponding callback, the test fails loudly. If the behaviour
evolves and the mock is not updated, Mox complains.

```
notification_service/
├── lib/
│   └── notification_service/
│       ├── adapter.ex               # behaviour: the contract
│       ├── sms/
│       │   └── twilio_adapter.ex    # real adapter
│       ├── email/
│       │   └── sendgrid_adapter.ex  # real adapter
│       └── dispatcher.ex            # uses adapter via config
├── test/
│   ├── notification_service/
│   │   └── dispatcher_test.exs
│   ├── support/
│   │   └── mocks.ex                 # Mox.defmock declarations
│   └── test_helper.exs
└── mix.exs
```

## Why Mox and not Meck, Mimic, or manual stubs

- **Meck** monkey-patches modules at runtime. It works but is global: two tests running `async: true`
  can step on one another's patches. It does not verify against a behaviour.
- **Mimic** is ergonomic but, like Meck, swaps real modules and is process-global by default.
  You must be careful with async tests.
- **Manual stub modules** (`SmsStub`) work but nothing enforces that the stub implements the
  behaviour. A missing callback compiles fine and fails at runtime.
- **Mox** defines mock modules at compile time with `Mox.defmock(X, for: MyBehaviour)`.
  It is **explicit**, **per-process** (via `Mox.set_mox_from_context/1` or `allow/3`), supports
  `async: true`, and verifies behaviour conformance.

Mox is the José Valim-endorsed canonical solution — it follows the philosophy "mocks as a noun,
not a verb" from [this blog post](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/).

## Core concepts

### 1. The contract is a behaviour
A behaviour defines the interface with `@callback`. The real adapter implements it
(`@behaviour Adapter`). The mock is generated from the same behaviour.

### 2. The implementation module is resolved at runtime via config
Production config: `config :notification_service, :sms_adapter, TwilioAdapter`.
Test config: `config :notification_service, :sms_adapter, SmsMock`.

### 3. Every expectation must be consumed
`expect(SmsMock, :send, fn ... -> :ok end)` means "exactly one call". If the test ends
without that call, Mox fails the test via `verify_on_exit!`. For more lenient usage use `stub/3`.

## Design decisions

- **Option A — `:meck`/runtime monkey-patching**: global state, fights `async: true`.
- **Option B — wrapper module with `@behaviour` + manual stub**: works but no compile-time check
  that stub implements every callback.
- **Option C — Mox + behaviour + config indirection**: compile-time conformance check, per-process
  isolation, works with `async: true`. Extra wiring (`mix.exs` dep, `test_helper.exs` setup).

Chosen: **Option C**. The extra wiring is boilerplate you write once.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:mox, "~> 1.2", only: :test},
    {:req, "~> 0.5"}
  ]
end

defp elixirc_paths(:test), do: ["lib", "test/support"]
defp elixirc_paths(_),     do: ["lib"]
```

### Step 1: define the behaviour (the contract)

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
```

### Step 2: real adapter

```elixir
# lib/notification_service/sms/twilio_adapter.ex
defmodule NotificationService.Sms.TwilioAdapter do
  @behaviour NotificationService.Adapter

  @impl true
  def send(phone, %{body: body}) do
    # Simplified — production calls Twilio via Req
    case Req.post("https://api.twilio.com/send", json: %{to: phone, body: body}) do
      {:ok, %{status: 200, body: %{"sid" => sid}}} -> {:ok, sid}
      {:ok, %{status: 429}} -> {:error, :rate_limited}
      _ -> {:error, :provider_down}
    end
  end

  @impl true
  def healthcheck do
    case Req.get("https://status.twilio.com/api/v2/status.json") do
      {:ok, %{status: 200}} -> :ok
      _ -> {:error, :provider_down}
    end
  end
end
```

### Step 3: dispatcher resolves the adapter via config

```elixir
# lib/notification_service/dispatcher.ex
defmodule NotificationService.Dispatcher do
  @moduledoc "Dispatches a notification to the configured adapter per channel."

  @type channel :: :sms | :email

  @spec dispatch(channel(), String.t(), map()) :: {:ok, String.t()} | {:error, term()}
  def dispatch(:sms, recipient, payload), do: sms_adapter().send(recipient, payload)
  def dispatch(:email, recipient, payload), do: email_adapter().send(recipient, payload)

  defp sms_adapter,   do: Application.fetch_env!(:notification_service, :sms_adapter)
  defp email_adapter, do: Application.fetch_env!(:notification_service, :email_adapter)
end
```

### Step 4: declare the mocks

```elixir
# test/support/mocks.ex
Mox.defmock(NotificationService.SmsMock,   for: NotificationService.Adapter)
Mox.defmock(NotificationService.EmailMock, for: NotificationService.Adapter)
```

```elixir
# test/test_helper.exs
ExUnit.start()

Application.put_env(:notification_service, :sms_adapter,   NotificationService.SmsMock)
Application.put_env(:notification_service, :email_adapter, NotificationService.EmailMock)

Code.require_file("support/mocks.ex", __DIR__)
```

### Step 5: tests

```elixir
# test/notification_service/dispatcher_test.exs
defmodule NotificationService.DispatcherTest do
  use ExUnit.Case, async: true

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

## Why this works

`Mox.defmock(SmsMock, for: Adapter)` generates `SmsMock` at compile time, implementing every
`@callback` of `Adapter`. Each callback is wired to the Mox runtime, which looks up
expectations keyed by the test process. Because the lookup is per-process, two async tests
can each set their own expectations on the same mock without interference.

`set_mox_from_context/1` is required because `async: true` tests run in child tasks; it copies
the expectation ownership so helper processes can also use the mock.

`verify_on_exit!/1` runs at the end of each test and raises if any `expect/3` was not consumed.
This catches silent "my code path did not run" bugs.

## Tests

See Step 5 — three describe blocks cover happy path, error propagation, and strict cardinality.

## Benchmark

Mox overhead per call is an ETS lookup plus a process-dictionary read — typically < 1µs.

```elixir
Benchee.run(%{
  "Mox expect + call" => fn ->
    Mox.expect(NotificationService.SmsMock, :send, fn _, _ -> {:ok, "x"} end)
    NotificationService.Dispatcher.dispatch(:sms, "+1", %{subject: "s", body: "b"})
  end
}, time: 3, warmup: 1)
```

Target: each iteration < 5µs. If Mox becomes the bottleneck of a suite, the suite has another
issue (probably synchronous I/O).

## Trade-offs and production gotchas

**1. Forgetting `verify_on_exit!/1`**
Without it, unsatisfied expectations are silently accepted. The test "passes" even though
the code never called the adapter. Always add this to `setup`.

**2. Using `stub/3` when you meant `expect/3`**
`stub/3` is lenient (any number of calls, including zero). `expect/3` requires the exact call
count. Use `stub` for "this module may or may not be called", `expect` for "this module must
be called exactly N times".

**3. Shared mocks across async tests without `set_mox_from_context`**
By default Mox expectations belong to the owning process. Helper tasks inherit nothing unless
you call `allow/3` or `set_mox_from_context/1`. Forgetting this produces intermittent
`NoExpectationError`.

**4. Mocking your own code instead of the boundary**
Mocks are for external I/O adapters (HTTP, DB, file system, message brokers). Mocking internal
domain modules (your own `Order`, `Cart`) creates tests that verify your mock setup rather
than your behaviour. Draw the boundary at the side-effect edge.

**5. Behaviour drift**
If you add a `@callback` to `Adapter` and forget to update the real adapter, the compiler
warns (`@impl true` is unused or a callback is unimplemented). Mox-generated mocks
automatically implement the new callback — but the tests may not call it, giving a false
sense of coverage. Review coverage, not just compile output.

**6. When NOT to use this**
Pure functions do not need mocks. If a function takes data in and returns data out, use
example-based or property-based tests instead.

## Reflection

Mox requires a compile-time `@behaviour`. In what circumstances is it worth paying the cost
of designing a behaviour just to make a piece of code mockable, and when does that design
cost outweigh the testability benefit?

## Resources

- [Mox on hex](https://hexdocs.pm/mox/Mox.html)
- [José Valim — Mocks and explicit contracts](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/)
- [Adopting Mox at scale — Dashbit blog](https://dashbit.co/blog/mocks-and-explicit-contracts)
- [`Mox.set_mox_from_context/1` docs](https://hexdocs.pm/mox/Mox.html#set_mox_from_context/1)
