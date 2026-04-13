# Mox Stub-Many and Global Mode

**Project**: `mox_stub_many` — a notification dispatcher with five channels (email, sms,
push, slack, webhook).
---

## Project context

You've internalised basic Mox: `defmock`, `expect`, `verify_on_exit!`. Now you're dealing
with real-world mess: a notification dispatcher that fans out to five channels, each behind
a behaviour, each with its own test needs. Writing `expect(ChannelMock, :send, fn ... end)`
for five channels in every test creates 50 lines of boilerplate before you even assert
anything.

This exercise tackles three Mox patterns that collapse the noise:

- **`stub_with/2`** — wire an entire fake implementation once, override per-test with `expect`.
- **`set_mox_global`** — when the code under test spawns processes you don't control
  (Oban workers, Broadway pipelines, long-lived supervisors), private-mode allowance breaks
  down. Global mode trades `async: true` for simplicity.
- **Mixing both** within the same suite — private-mode tests that are fast and parallel, a
  small set of global-mode tests for integration coverage.

The deliverable is a dispatcher with realistic plumbing (retry, fan-out, idempotency) and a
test suite that proves each pattern.

Project structure:

```
mox_stub_many/
├── lib/
│   └── notify/
│       ├── application.ex
│       ├── dispatcher.ex          # fans out to channels
│       ├── channel.ex             # behaviour
│       ├── channels/
│       │   ├── email.ex
│       │   ├── sms.ex
│       │   ├── push.ex
│       │   ├── slack.ex
│       │   └── webhook.ex
│       └── worker.ex              # background sender (spawns tasks)
├── test/
│   ├── support/
│   │   ├── mocks.ex
│   │   └── fakes.ex               # behaviour-implementing fakes
│   ├── test_helper.exs
│   └── notify/
│       ├── dispatcher_private_test.exs
│       ├── dispatcher_stub_with_test.exs
│       └── worker_global_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. `stub_with/2` replaces boilerplate with a real fake

```elixir
Mox.stub_with(Notify.ChannelMock, Notify.Fakes.SuccessfulChannel)
```

Any call on `ChannelMock` that doesn't have a specific `expect` falls through to the fake.
The fake is a regular module that implements the behaviour — you can exercise its logic
from iex, reason about it, and reuse across suites.

### 2. Private vs global dispatch — when to switch

```
Private mode (default):

  Test pid ─────────┐
                    │ owns
                    ▼
            +---------------+
            | Mock registry |
            +---------------+
                    │
      allow(task_pid)
                    ▼
               Task pid (can now consume expectations)


Global mode:

  Any pid in the VM ──▶ Mock registry (no owner concept)
                       → only one test at a time, async: false
```

Global is only needed when you can't enumerate every pid that will call the mock (Oban,
Horde, cluster-wide singletons).

### 3. `set_mox_from_context` — let tags pick the mode

```elixir
setup :set_mox_from_context
```

Reads the `:mox` tag (`:private` or `:global`) and switches mode per test. Lets you keep
most tests `async: true` and flip specific ones to `:global` with `@tag :global`.

### 4. Combining stub_with with expect

`stub_with` provides a baseline. `expect` adds strictness for specific calls:

```elixir
stub_with(ChannelMock, Fakes.SuccessfulChannel)
expect(ChannelMock, :send, fn %{channel: :email}, _ -> {:error, :bounced} end)
```

The specific expectation (one `:email` call returning `:bounced`) runs first; everything
else falls through to the fake. `verify_on_exit!` still asserts that the expectation
consumed its quota.

### 5. Fakes that embed invariants

A good fake is more than "return :ok". Encode domain logic:

```elixir
defmodule Notify.Fakes.SuccessfulChannel do
  @behaviour Notify.Channel
  def send(%{to: ""} = _msg, _opts), do: {:error, :empty_recipient}
  def send(%{to: _} = msg, _opts), do: {:ok, "msg_#{msg.id}"}
end
```

Now tests that feed an empty recipient via `stub_with` verify the fake's behaviour, not
just that a function was called. Fakes become executable specifications.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Isolate Mox to `:test` scope and add `test/support` to compile paths to prevent mock code from leaking into production binaries.

```elixir
defmodule Notify.MixProject do
  use Mix.Project

  def project do
    [
      app: :notify,
      version: "0.1.0",
      elixir: "~> 1.16",
      elixirc_paths: elixirc_paths(Mix.env()),
      deps: deps()
    ]
  end

  def application, do: [extra_applications: [:logger], mod: {Notify.Application, []}]

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps, do: [{:mox, "~> 1.1", only: :test}]
end
```

### Step 2: Behaviour

**Objective**: Define sealed behaviour contract with typed reasons so Mox validates call signatures and pattern-matching on errors becomes exhaustive.

```elixir
# lib/notify/channel.ex
defmodule Notify.Channel do
  @type msg :: %{id: String.t(), to: String.t(), body: String.t(), channel: atom()}
  @type reason :: :bounced | :invalid_recipient | :rate_limited | :network | :empty_recipient

  @callback send(msg(), keyword()) :: {:ok, String.t()} | {:error, reason()}
end
```

### Step 3: Channel implementations

**Objective**: Create concrete behaviour-implementing modules so Application.fetch_env/2 injects production or mock implementations without dispatcher code changes.

```elixir
# lib/notify/channels/email.ex
defmodule Notify.Channels.Email do
  @behaviour Notify.Channel
  def send(%{to: to} = _msg, _opts) when to == "", do: {:error, :empty_recipient}
  def send(msg, _opts) do
    # real implementation would call Mailer
    {:ok, "email_#{msg.id}"}
  end
end

# lib/notify/channels/sms.ex
defmodule Notify.Channels.Sms do
  @behaviour Notify.Channel
  def send(msg, _opts), do: {:ok, "sms_#{msg.id}"}
end

# lib/notify/channels/push.ex
defmodule Notify.Channels.Push do
  @behaviour Notify.Channel
  def send(msg, _opts), do: {:ok, "push_#{msg.id}"}
end

# lib/notify/channels/slack.ex
defmodule Notify.Channels.Slack do
  @behaviour Notify.Channel
  def send(msg, _opts), do: {:ok, "slack_#{msg.id}"}
end

# lib/notify/channels/webhook.ex
defmodule Notify.Channels.Webhook do
  @behaviour Notify.Channel
  def send(msg, _opts), do: {:ok, "webhook_#{msg.id}"}
end
```

### Step 4: Dispatcher

**Objective**: Look up channel implementations at runtime via Application env so each test gets injected mocks without code rewrites or module recompilation.

```elixir
# lib/notify/dispatcher.ex
defmodule Notify.Dispatcher do
  @moduledoc """
  Fans out a message to multiple channels, aggregating results.
  Channels are looked up via Application env so tests can swap implementations.
  """

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
```

### Step 5: Worker that spawns tasks (needs global mode in tests)

**Objective**: Spawn detached Task processes so private-mode Mox allowance breaks, forcing tests to switch to global registry mode for unsupervised pids.

```elixir
# lib/notify/worker.ex
defmodule Notify.Worker do
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, opts)

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
```

### Step 6: Mocks and fakes

**Objective**: Generate mocks via Mox.defmock and implement behaviour-backed fakes so stub_with/2 collapses repetitive expect/3 calls into reusable baselines.

```elixir
# test/support/mocks.ex
Mox.defmock(Notify.EmailMock, for: Notify.Channel)
Mox.defmock(Notify.SmsMock, for: Notify.Channel)
Mox.defmock(Notify.PushMock, for: Notify.Channel)
Mox.defmock(Notify.SlackMock, for: Notify.Channel)
Mox.defmock(Notify.WebhookMock, for: Notify.Channel)
```

```elixir
# test/support/fakes.ex
defmodule Notify.Fakes.SuccessfulChannel do
  @behaviour Notify.Channel
  def send(%{to: ""}, _opts), do: {:error, :empty_recipient}
  def send(%{channel: ch, id: id}, _opts), do: {:ok, "#{ch}_#{id}"}
end

defmodule Notify.Fakes.FailingChannel do
  @behaviour Notify.Channel
  def send(%{channel: :email}, _), do: {:error, :bounced}
  def send(%{channel: :sms}, _), do: {:error, :rate_limited}
  def send(%{channel: _, id: id}, _), do: {:ok, "ok_#{id}"}
end
```

### Step 7: test_helper and config

**Objective**: Configure Application env to point to mocks in tests and production modules in prod so dispatcher swaps channel implementations via configuration, not code edits.

```elixir
# test/test_helper.exs
Application.put_env(:notify, :channels, %{
  email: Notify.EmailMock,
  sms: Notify.SmsMock,
  push: Notify.PushMock,
  slack: Notify.SlackMock,
  webhook: Notify.WebhookMock
})
ExUnit.start()
```

```elixir
# config/config.exs
import Config
config :notify, :channels, %{
  email: Notify.Channels.Email,
  sms: Notify.Channels.Sms,
  push: Notify.Channels.Push,
  slack: Notify.Channels.Slack,
  webhook: Notify.Channels.Webhook
}
```

### Step 8: Private-mode tests (fast, async)

**Objective**: Write async tests with per-process mock registry so multiple tests parallelize without expectations bleeding across test pids.

```elixir
# test/notify/dispatcher_private_test.exs
defmodule Notify.DispatcherPrivateTest do
  use ExUnit.Case, async: true
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

### Step 9: stub_with tests (less boilerplate)

**Objective**: Use stub_with/2 with fake modules to eliminate per-test expect boilerplate while expect/3 overrides specific edge-case interactions.

```elixir
# test/notify/dispatcher_stub_with_test.exs
defmodule Notify.DispatcherStubWithTest do
  use ExUnit.Case, async: true
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
```

### Step 10: Global-mode tests (for detached workers)

**Objective**: Switch to global mock registry with async: false so Task children inherit expectations without explicit allow/3, trading parallelism for coverage.

```elixir
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

### Step 11: Run

**Objective**: Execute with --trace flag to observe private-mode tests parallelizing while global tests serialize, proving the Mox dispatch mode semantics.

```bash
mix test --trace
# Observe: the private tests run concurrently; the worker test runs serially.
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: Mox Patterns and Production Implications

Testing through explicit behavior contracts requires careful design of expectations. In production systems with many mocked dependencies, the cost of maintaining contracts grows with each new adapter or integration point. The key insight is that Mox's private-mode isolation prevents test pollution only when all pids involved are accounted for—the moment you spawn unowned processes (Tasks, Oban workers, Broadway pipelines), you must switch to global mode, trading parallelism for simplicity. Understanding when to reach for expect/3 vs stub_with/2 vs global mode separates brittle test suites from maintainable ones. A senior engineer recognizes that mocking boundaries—not implementation details—pays dividends over time.

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.

---

## Trade-offs and production gotchas

**1. `stub_with` requires a real behaviour-implementing module**
You can't `stub_with` an anonymous module. This is intentional — a fake is a first-class
citizen, not a throwaway lambda. It costs a file; it buys reusability and debuggability.

**2. Global mode ≠ `async: true`**
Setting global mode in an `async: true` test is an error: two concurrent tests would share
one mock registry and overwrite each other's expectations. Mox will complain at runtime;
fix by switching to `async: false`.

**3. `verify_on_exit!` still fires in global mode**
If you set `expect(Mock, :f, 2, fn _ -> :ok end)` and only 1 call happens, the test fails
on exit regardless of mode. Global mode relaxes allowance, not assertion.

**4. Mixing expect and stub_with is legal but ordering matters conceptually**
`expect` always wins for its quota. Once the quota is exhausted, calls fall through to the
stub. Design your tests with this in mind: specific expectations first, fallback via
`stub_with`.

**5. Multiple mocks per behaviour**
You can call `defmock` multiple times with the same `for:` to support parameterised tests
that swap entire stacks. Don't abuse it — one mock per behaviour is usually enough.

**6. `set_mox_from_context` precedence**
If you have both `setup :set_mox_from_context` and `setup :set_mox_global`, the last one
wins. Pick one strategy per module.

**7. When NOT to use Mox at all**
- Your "dependency" is a function you wrote two files over. Don't. Call it.
- The behaviour shape changes monthly. Mocking a moving target creates test churn greater
  than the value it provides — write a hand-rolled fake that you refactor with the API.
- You need to simulate wire-level failures (partial responses, TLS errors). Use Bypass.

**8. Watch the noise-to-signal ratio**
A test with 30 lines of `expect` and 2 lines of assertions is communicating nothing but
plumbing. Either reach for `stub_with`, or the design under test has too many collaborators
— consider refactoring.

---

## Performance notes

`stub_with` indirection adds ~1µs per call vs a plain stub. For a suite with 10k mock calls
via `stub_with`, that's 10ms — negligible.

Global-mode tests pay a serialization cost: the whole `async: false` set runs sequentially.
Keep global tests small; push coverage of fine-grained behaviour into private-mode tests.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Mox.stub_with/2 hexdocs](https://hexdocs.pm/mox/Mox.html#stub_with/2)
- [Mox.set_mox_global/1 hexdocs](https://hexdocs.pm/mox/Mox.html#set_mox_global/1)
- ["Mocks and explicit contracts" — José Valim](https://dashbit.co/blog/mocks-and-explicit-contracts)
- [Testing Elixir (book)](https://pragprog.com/titles/lmelixir/testing-elixir/) — ch. 6
- [Mox source code](https://github.com/dashbitco/mox/blob/main/lib/mox.ex) — `dispatch_mode` logic
- ["Use mocks cautiously" — Chris Keathley](https://keathley.io/blog/mocking.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
