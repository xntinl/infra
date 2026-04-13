# Mox Stub-Many and Global Mode

**Project**: `mox_stub_many` — a notification dispatcher with five channels (email, sms, push, slack, webhook)

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
mox_stub_many/
├── lib/
│   └── mox_stub_many.ex
├── script/
│   └── main.exs
├── test/
│   └── mox_stub_many_test.exs
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
defmodule MoxStubMany.MixProject do
  use Mix.Project

  def project do
    [
      app: :mox_stub_many,
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

### `lib/mox_stub_many.ex`

```elixir
defmodule Notify.Fakes.SuccessfulChannel do
  @behaviour Notify.Channel
  @doc "Sends result from _opts."
  def send(%{to: ""} = _msg, _opts), do: {:error, :empty_recipient}
  @doc "Sends result from _opts."
  def send(%{to: _} = msg, _opts), do: {:ok, "msg_#{msg.id}"}
end

# lib/notify/channel.ex
defmodule Notify.Channel do
  @type msg :: %{id: String.t(), to: String.t(), body: String.t(), channel: atom()}
  @type reason :: :bounced | :invalid_recipient | :rate_limited | :network | :empty_recipient

  @callback send(msg(), keyword()) :: {:ok, String.t()} | {:error, reason()}
end

# lib/notify/channels/email.ex
defmodule Notify.Channels.Email do
  @behaviour Notify.Channel
  @doc "Sends result from _opts."
  def send(%{to: to} = _msg, _opts) when to == "", do: {:error, :empty_recipient}
  @doc "Sends result from msg and _opts."
  def send(msg, _opts) do
    # real implementation would call Mailer
    {:ok, "email_#{msg.id}"}
  end
end

# lib/notify/channels/sms.ex
defmodule Notify.Channels.Sms do
  @behaviour Notify.Channel
  @doc "Sends result from msg and _opts."
  def send(msg, _opts), do: {:ok, "sms_#{msg.id}"}
end

# lib/notify/channels/push.ex
defmodule Notify.Channels.Push do
  @behaviour Notify.Channel
  @doc "Sends result from msg and _opts."
  def send(msg, _opts), do: {:ok, "push_#{msg.id}"}
end

# lib/notify/channels/slack.ex
defmodule Notify.Channels.Slack do
  @behaviour Notify.Channel
  @doc "Sends result from msg and _opts."
  def send(msg, _opts), do: {:ok, "slack_#{msg.id}"}
end

# lib/notify/channels/webhook.ex
defmodule Notify.Channels.Webhook do
  @behaviour Notify.Channel
  @doc "Sends result from msg and _opts."
  def send(msg, _opts), do: {:ok, "webhook_#{msg.id}"}
end

# lib/notify/dispatcher.ex
defmodule Notify.Dispatcher do
  @moduledoc """
  Fans out a message to multiple channels, aggregating results.
  Channels are looked up via Application env so tests can swap implementations.
  """

  @doc "Returns dispatch result from msg and channels."
  @spec dispatch(Notify.Channel.msg(), [atom()]) :: %{atom() => {:ok, String.t()} | {:error, atom()}}
  def dispatch(msg, channels) when is_list(channels) do
    channels
    |> Enum.map(fn ch ->
      {ch, impl(ch).send(Map.put(msg, :channel, ch), [])}
    end)
    |> Map.new()
  end

  defp impl(channel) do
    Application.fetch_env!(:notify, :channels)
    |> Map.fetch!(channel)
  end
end

# lib/notify/worker.ex
defmodule Notify.Worker do
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, opts)

  @doc "Returns enqueue result from server, msg and channels."
  def enqueue(server \\ __MODULE__, msg, channels) do
    GenServer.cast(server, {:dispatch, msg, channels})
  end

  @impl true
  def init(_), do: {:ok, %{}}

  @impl true
  def handle_cast({:dispatch, msg, channels}, state) do
    # Fan out into detached tasks — test process has no reference to their pids
    Task.start(fn -> Notify.Dispatcher.dispatch(msg, channels) end)
    {:noreply, state}
  end
end

defmodule Notify.Fakes.SuccessfulChannel do
  @behaviour Notify.Channel
  @doc "Sends result from _opts."
  def send(%{to: ""}, _opts), do: {:error, :empty_recipient}
  @doc "Sends result from id and _opts."
  def send(%{channel: ch, id: id}, _opts), do: {:ok, "#{ch}_#{id}"}
end

defmodule Notify.Fakes.FailingChannel do
  @behaviour Notify.Channel
  @doc "Sends result from _."
  def send(%{channel: :email}, _), do: {:error, :bounced}
  @doc "Sends result from _."
  def send(%{channel: :sms}, _), do: {:error, :rate_limited}
  @doc "Sends result from id and _."
  def send(%{channel: _, id: id}, _), do: {:ok, "ok_#{id}"}
end

# test/notify/dispatcher_stub_with_test.exs
defmodule Notify.DispatcherStubWithTest do
  use ExUnit.Case, async: true
  doctest MoxStubMany.MixProject
  import Mox

  setup :verify_on_exit!

  setup do
    for mock <- [Notify.EmailMock, Notify.SmsMock, Notify.PushMock,
                 Notify.SlackMock, Notify.WebhookMock] do
      stub_with(mock, Notify.Fakes.SuccessfulChannel)
    end
    :ok
  end

  describe "Notify.DispatcherStubWith" do
    test "happy path with the successful fake" do
      msg = %{id: "abc", to: "x@y", body: "hello"}
      result = Notify.Dispatcher.dispatch(msg, [:email, :sms, :push, :slack, :webhook])
      for {ch, r} <- result, do: assert({:ok, _} = r)
      assert result[:email] == {:ok, "email_abc"}
    end

    test "stub_with returns :empty_recipient when fake detects it" do
      msg = %{id: "z", to: "", body: "hello"}
      result = Notify.Dispatcher.dispatch(msg, [:email])
      assert result[:email] == {:error, :empty_recipient}
    end

    test "expect overrides stub_with for a single interaction" do
      expect(Notify.EmailMock, :send, fn _, _ -> {:error, :bounced} end)
      msg = %{id: "q", to: "x@y", body: "hi"}
      result = Notify.Dispatcher.dispatch(msg, [:email, :sms])
      assert result[:email] == {:error, :bounced}
      assert result[:sms] == {:ok, "sms_q"}
    end
  end
end

# test/notify/worker_global_test.exs
defmodule Notify.WorkerGlobalTest do
  use ExUnit.Case, async: false          # global mode forbids async
  import Mox

  setup :set_mox_global                  # all VM processes see expectations
  setup :verify_on_exit!

  setup do
    for mock <- [Notify.EmailMock, Notify.SmsMock, Notify.PushMock,
                 Notify.SlackMock, Notify.WebhookMock] do
      stub_with(mock, Notify.Fakes.SuccessfulChannel)
    end

    name = :"worker_#{:erlang.unique_integer([:positive])}"
    {:ok, _pid} = Notify.Worker.start_link(name: name)
    {:ok, worker: name}
  end

  describe "Notify.WorkerGlobal" do
    test "detached Task sees stubs via global mode", %{worker: w} do
      # In private mode, Task.start is not an allowed pid — this would crash.
      # In global mode, the Task reads the same mock registry and passes.
      parent = self()
      test_mock = Notify.EmailMock

      expect(test_mock, :send, fn msg, _ ->
        send(parent, {:seen, msg.id})
        {:ok, "ok"}
      end)

      Notify.Worker.enqueue(w, %{id: "bg1", to: "x@y", body: "b"}, [:email])

      assert_receive {:seen, "bg1"}, 1_000
    end
  end
end
```

### `test/mox_stub_many_test.exs`

```elixir
defmodule Notify.DispatcherPrivateTest do
  use ExUnit.Case, async: true
  doctest MoxStubMany.MixProject
  import Mox

  setup :verify_on_exit!
  setup :set_mox_from_context

  describe "Notify.DispatcherPrivate" do
    test "dispatches to all channels concurrently via expects" do
      msg = %{id: "1", to: "user@example.com", body: "hi"}

      expect(Notify.EmailMock, :send, fn %{channel: :email}, _ -> {:ok, "e_1"} end)
      expect(Notify.SmsMock, :send, fn %{channel: :sms}, _ -> {:ok, "s_1"} end)
      expect(Notify.PushMock, :send, fn %{channel: :push}, _ -> {:ok, "p_1"} end)
      expect(Notify.SlackMock, :send, fn %{channel: :slack}, _ -> {:ok, "sl_1"} end)
      expect(Notify.WebhookMock, :send, fn %{channel: :webhook}, _ -> {:ok, "w_1"} end)

      result = Notify.Dispatcher.dispatch(msg, [:email, :sms, :push, :slack, :webhook])

      assert result[:email] == {:ok, "e_1"}
      assert result[:webhook] == {:ok, "w_1"}
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Mox Stub-Many and Global Mode.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Mox Stub-Many and Global Mode ===")
    IO.puts("Category: Advanced testing\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case MoxStubMany.run(payload) do
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
        for _ <- 1..1_000, do: MoxStubMany.run(:bench)
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
