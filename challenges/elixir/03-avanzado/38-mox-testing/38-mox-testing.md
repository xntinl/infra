# Mock-Based Testing with Mox

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` now calls three external services: a payment provider HTTP API, a fraud
scorer (the Python Port from exercise 31), and a Slack webhook for ops notifications.
Testing gateway logic without hitting live external services requires replacing these
dependencies with controllable fakes. Mox provides mock modules that implement the
same behaviour contract as the real module, making the fake indistinguishable from the
real thing at the type level.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── notifications/
│           ├── slack_behaviour.ex      # ← you implement this
│           ├── slack_client.ex         # ← and this (real implementation)
│           └── ops_notifier.ex         # ← and this (business logic)
├── test/
│   ├── test_helper.exs                 # ← defmock calls go here
│   └── api_gateway/
│       └── notifications/
│           └── ops_notifier_test.exs   # given tests
└── mix.exs
```

---

## The business problem

`OpsNotifier` sends Slack messages when payment events occur. The logic is:
- On a failed payment: send an alert to `#payments-alerts`
- On a recovered payment: send a recovery notice and upload a summary file
- Both calls go through `SlackBehaviour`, which has a real implementation and a mock

Tests must verify:
1. Which Slack functions are called and with what arguments
2. That upload does not happen if the alert call fails first
3. That the mock verifies expectations at end-of-test (no silent omissions)

---

## Why behaviours are required for Mox

Mox does not allow mocking arbitrary modules. The module under mock **must** declare a
behaviour with `@callback` specifications. The mock then implements those callbacks —
the type system catches mismatches at compile time.

```elixir
# Without behaviour: no compile-time guarantee the mock matches the real module
# With behaviour: Mox.defmock verifies every @callback is present in the mock
```

The production code never references the concrete module directly:

```elixir
defp slack_client do
  Application.get_env(:api_gateway, :slack_client, ApiGateway.Notifications.SlackClient)
end
```

In tests, `Application.put_env(:api_gateway, :slack_client, MockSlackClient)` swaps
the implementation. In production, the real client is used automatically.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:mox, "~> 1.0", only: :test}
  ]
end
```

### Step 2: `lib/api_gateway/notifications/slack_behaviour.ex`

The behaviour defines the contract that both the real client and the mock must satisfy.
Each callback has a full typespec, which Dialyzer uses to verify that callers match the
expected argument and return types.

```elixir
defmodule ApiGateway.Notifications.SlackBehaviour do
  @moduledoc """
  Contract for sending messages to Slack.
  Implemented by SlackClient (production) and MockSlackClient (tests).
  """

  @doc "Send a text message to a channel."
  @callback send_message(channel :: String.t(), text :: String.t()) ::
              {:ok, %{ts: String.t(), channel: String.t()}} | {:error, atom()}

  @doc "Upload a file to a channel."
  @callback upload_file(channel :: String.t(), filename :: String.t(), content :: binary()) ::
              {:ok, %{file_id: String.t()}} | {:error, atom()}
end
```

### Step 3: `lib/api_gateway/notifications/slack_client.ex`

The real Slack client implements the behaviour. In production, this would use `Req` or
`HTTPoison` to call the Slack Web API. The placeholder implementation logs and returns
success so the module compiles and satisfies the behaviour contract.

```elixir
defmodule ApiGateway.Notifications.SlackClient do
  @moduledoc "Real Slack client. Makes HTTP calls to the Slack Web API."
  @behaviour ApiGateway.Notifications.SlackBehaviour

  @impl true
  def send_message(channel, text) do
    IO.puts("[Slack] #{channel}: #{text}")
    {:ok, %{ts: "#{System.system_time(:second)}.000", channel: channel}}
  end

  @impl true
  def upload_file(channel, filename, _content) do
    IO.puts("[Slack] upload #{filename} to #{channel}")
    {:ok, %{file_id: "F#{:erlang.phash2(filename)}"}}
  end
end
```

### Step 4: `lib/api_gateway/notifications/ops_notifier.ex`

The OpsNotifier contains the business logic. It depends on a SlackBehaviour implementation
injected via Application config. The `with` expression in `payment_recovered/1` ensures that
`upload_file` is only called if `send_message` succeeds — this is the key behavior that
the Mox tests verify.

```elixir
defmodule ApiGateway.Notifications.OpsNotifier do
  @moduledoc """
  Sends operational notifications to Slack.

  Depends on a SlackBehaviour implementation injected via Application config.
  In production: SlackClient. In tests: MockSlackClient.
  """

  @alerts_channel "#payments-alerts"

  @doc "Notify the payments team of a failed payment."
  @spec payment_failed(map()) :: {:ok, term()} | {:error, term()}
  def payment_failed(event) do
    text = "Payment FAILED: #{inspect(event)}"
    slack_client().send_message(@alerts_channel, text)
  end

  @doc "Notify of recovery. Sends a message then uploads a summary file."
  @spec payment_recovered(map()) :: {:ok, term()} | {:error, term()}
  def payment_recovered(event) do
    summary = generate_summary(event)

    with {:ok, _msg} <- slack_client().send_message(@alerts_channel, "Payment recovered"),
         {:ok, file} <- slack_client().upload_file(@alerts_channel, "recovery.txt", summary) do
      {:ok, file}
    end
  end

  defp slack_client do
    Application.get_env(:api_gateway, :slack_client, ApiGateway.Notifications.SlackClient)
  end

  defp generate_summary(event), do: "Recovery summary\n#{inspect(event)}"
end
```

### Step 5: `test/test_helper.exs` additions

The `defmock` call creates a module that implements all callbacks from the behaviour.
It must run exactly once per test suite — placing it in `test_helper.exs` guarantees this.

```elixir
# Add to test/test_helper.exs — defmock must run exactly once per test suite
Mox.defmock(ApiGateway.Notifications.MockSlackClient,
  for: ApiGateway.Notifications.SlackBehaviour)
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/notifications/ops_notifier_test.exs
defmodule ApiGateway.Notifications.OpsNotifierTest do
  use ExUnit.Case, async: true

  import Mox

  alias ApiGateway.Notifications.OpsNotifier
  alias ApiGateway.Notifications.MockSlackClient

  setup :verify_on_exit!

  setup do
    Application.put_env(:api_gateway, :slack_client, MockSlackClient)
    :ok
  end

  describe "payment_failed/1" do
    test "calls send_message with the alerts channel and event text" do
      expect(MockSlackClient, :send_message, fn channel, text ->
        assert channel == "#payments-alerts"
        assert String.contains?(text, "FAILED")
        {:ok, %{ts: "123.456", channel: channel}}
      end)

      event = %{id: "pay_001", amount: 99.0, reason: :insufficient_funds}
      assert {:ok, _} = OpsNotifier.payment_failed(event)
    end

    test "returns the Slack error when send_message fails" do
      expect(MockSlackClient, :send_message, fn _channel, _text ->
        {:error, :channel_not_found}
      end)

      assert {:error, :channel_not_found} =
        OpsNotifier.payment_failed(%{id: "pay_002"})
    end
  end

  describe "payment_recovered/1" do
    test "sends message then uploads file on success" do
      expect(MockSlackClient, :send_message, fn _channel, text ->
        assert String.contains?(text, "recovered")
        {:ok, %{ts: "rec.001", channel: "#payments-alerts"}}
      end)

      expect(MockSlackClient, :upload_file, fn _channel, filename, content ->
        assert filename == "recovery.txt"
        assert is_binary(content)
        {:ok, %{file_id: "F_recovery_001"}}
      end)

      event = %{id: "pay_003", recovered_at: DateTime.utc_now()}
      assert {:ok, %{file_id: "F_recovery_001"}} =
        OpsNotifier.payment_recovered(event)
    end

    test "does NOT call upload_file if send_message fails" do
      # Only one expect — if upload_file is called, Mox raises "unexpected call"
      expect(MockSlackClient, :send_message, fn _channel, _text ->
        {:error, :channel_archived}
      end)

      assert {:error, :channel_archived} =
        OpsNotifier.payment_recovered(%{id: "pay_004"})
    end

    test "upload_file receives the binary content of the summary" do
      expect(MockSlackClient, :send_message, fn _ch, _txt ->
        {:ok, %{ts: "ts", channel: "#payments-alerts"}}
      end)

      expect(MockSlackClient, :upload_file, fn _ch, _fname, content ->
        assert is_binary(content)
        assert byte_size(content) > 0
        {:ok, %{file_id: "F_ok"}}
      end)

      assert {:ok, _} = OpsNotifier.payment_recovered(%{id: "pay_005"})
    end
  end

  describe "stub: send_message always succeeds" do
    test "can call send_message any number of times without expect" do
      stub(MockSlackClient, :send_message, fn _ch, _txt ->
        {:ok, %{ts: "stub_ts", channel: "ch"}}
      end)

      # Called 3 times — stub does not verify count
      Enum.each(1..3, fn i ->
        assert {:ok, _} = OpsNotifier.payment_failed(%{id: "pay_00#{i}"})
      end)
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/notifications/ops_notifier_test.exs --trace
```

---

## Trade-off analysis

| | `expect/3` | `stub/3` |
|--|-----------|---------|
| Verifies call happened | Yes (fails if not called) | No |
| Verifies call count | Yes (N times) | No |
| Verifies arguments | Yes (inside the function) | Optional |
| Use when | "This MUST be called" | "Always return X — I don't care how often" |
| Typical use | Core business calls | Background dependencies |

| Mox mode | `async: true` safe | When to use |
|----------|-------------------|------------|
| Private (default) | Yes | Unit tests — each test owns its mock state |
| Global (`set_mox_global`) | No — use `async: false` | Code that spawns processes which call the mock |
| `Mox.allow/3` | Yes | Tasks where you know the PID before spawning |

---

## Common production mistakes

**1. Calling `Mox.defmock/2` inside a test file**
`defmock` creates a module. If two test files define the same mock module name, the
second load raises `{:error, :already_loaded}`. Always put `defmock` in
`test/test_helper.exs`, which runs exactly once per suite.

**2. Forgetting `setup :verify_on_exit!`**
Without it, an `expect` that is never satisfied silently passes. The test gives false
confidence that the function was called. Always add `setup :verify_on_exit!` to every
test module that uses `expect`.

**3. Using `set_mox_global` with `async: true`**
Global mode shares mock state across all concurrently-running tests. Expects set in
test A can be consumed by test B. Result: non-deterministic failures that appear only
under parallel execution. If you need global mode, you must use `async: false`.

**4. Production code referencing the concrete module directly**
```elixir
# Non-injectable — cannot be mocked
SlackClient.send_message(ch, msg)

# Injectable — swap via Application config in tests
Application.get_env(:api_gateway, :slack_client, SlackClient).send_message(ch, msg)
```
The behaviour contract is useless if the production code hard-codes the implementation.

**5. Not implementing the full behaviour in the real module**
If `SlackClient` is missing a callback declared in `SlackBehaviour`, Elixir will warn
at compile time, but not error. The mock will implement it because `defmock` enforces
it, creating a false sense of parity. Run `mix compile --warnings-as-errors` in CI.

---

## Resources

- [Mox — HexDocs](https://hexdocs.pm/mox/Mox.html)
- [Mox — GitHub](https://github.com/dashbitco/mox)
- [Jose Valim — Mocks and explicit contracts](https://dashbit.co/blog/mocks-and-explicit-contracts)
- [Testing Elixir — Pragmatic Programmers](https://pragprog.com/titles/lmelixir/testing-elixir/)
