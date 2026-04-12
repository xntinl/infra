# Adapter pattern with behaviours — a `Notifier` with Email and Slack backends

**Project**: `notifier_adapter` — a `Notifier` behaviour that abstracts "send a notification", with `Email` and `Slack` adapters selected at runtime via configuration.

---

## Project context

You have a feature — "alert on failed payment" — that must send a message.
In dev you want a no-op or a log line. In staging you want email. In
production you want Slack (or both). Hard-coding the backend in the caller
makes every environment split painful; scattering `Application.get_env/2`
calls across caller code is worse.

The Adapter pattern with a behaviour is the canonical answer in Elixir:

- Define a `@behaviour` with the contract (`deliver/2`).
- Implement one module per backend.
- Select the active module at config time.
- Callers depend on the contract, not on the implementation.

This is the same pattern `Bamboo`, `Swoosh`, `Finch`, and `Phoenix.PubSub`
all use internally. Once you've built one, you recognize it everywhere.

Project structure:

```
notifier_adapter/
├── lib/
│   ├── notifier.ex
│   ├── notifier/email.ex
│   ├── notifier/slack.ex
│   └── notifier/test_adapter.ex
├── test/
│   └── notifier_test.exs
├── config/
│   └── config.exs
└── mix.exs
```

---

## Core concepts

### 1. The behaviour defines the contract

```elixir
@callback deliver(recipient :: String.t(), message :: String.t()) ::
            :ok | {:error, term()}
```

Every adapter must implement this exactly. The compiler enforces shape;
docs enforce semantics.

### 2. The "facade" module dispatches to the configured adapter

```elixir
def deliver(to, msg), do: adapter().deliver(to, msg)
defp adapter, do: Application.fetch_env!(:notifier_adapter, :adapter)
```

Callers only see `Notifier.deliver/2`. They never name an adapter. This
means tests can swap adapters freely, and swapping backends in production
is a config change, not a code change.

### 3. `Application.get_env` vs compile-time dispatch

`Application.fetch_env!/2` at each call is flexible but slightly slower.
For hot paths, some libraries read the module at compile time and emit
`@adapter Application.compile_env!(...)`. The trade-off: no runtime
swapping. For most cases, runtime lookup is correct.

### 4. Always ship a test adapter

A process-local adapter that captures sent messages makes every downstream
test trivial and never flakes on network. This is standard practice —
`Bamboo.TestAdapter`, `Swoosh.Adapters.Test`, etc.

---

## Why a behaviour + façade and not direct `Application.get_env` at every call

**Direct `Application.get_env/2` at every call site.** Couples every caller to the config key and to `apply/3`-style indirection. No compile-time check that the configured module conforms.

**Branching on env in a single function (`if Mix.env() == :prod`).** Works for tiny apps; breaks down the moment you need a test adapter, a staging backend, or per-tenant overrides.

**Behaviour + façade (chosen).** The façade is the single dispatch point; the behaviour is the compile-time contract; `Application.fetch_env!/2` is read once per call (or at compile time for hot paths). Tests swap adapters with `put_env/3`, production swaps them in `runtime.exs`.

---

## Design decisions

**Option A — `Application.compile_env!/2` in the façade, baked at compile time**
- Pros: Zero per-call lookup; adapter inlined.
- Cons: No runtime swapping; tests must recompile to change backends.

**Option B — `Application.fetch_env!/2` at each call** (chosen)
- Pros: Tests override with `put_env/3` and take effect immediately; same code path in dev, test, staging, prod; per-tenant or per-request overrides remain possible.
- Cons: Microseconds per call from the env lookup — noise for notifications, matters for RPC-heavy hot paths.

→ Chose **B** because notifications are not a hot path and the test-time flexibility is load-bearing. For `Finch`/`Req`-style RPC adapters, flip to A.

---

## Implementation

### Step 1: Create the project

```bash
mix new notifier_adapter
cd notifier_adapter
```

### Step 2: `lib/notifier.ex`

```elixir
defmodule Notifier do
  @moduledoc """
  Facade over pluggable notification backends. Callers use `Notifier.deliver/2`;
  the actual delivery module is picked from the `:adapter` key of the
  `:notifier_adapter` application config.
  """

  @type recipient :: String.t()
  @type message :: String.t()
  @type reason :: term()

  @callback deliver(recipient, message) :: :ok | {:error, reason}

  @doc """
  Send a message via the configured adapter. Returns whatever the adapter
  returns; do not swallow errors here — callers need the signal.
  """
  @spec deliver(recipient, message) :: :ok | {:error, reason}
  def deliver(recipient, message) when is_binary(recipient) and is_binary(message) do
    adapter().deliver(recipient, message)
  end

  # Private: resolved at every call so tests can override via
  # `Application.put_env/3` without restarting.
  defp adapter do
    Application.fetch_env!(:notifier_adapter, :adapter)
  end
end
```

### Step 3: `lib/notifier/email.ex`

```elixir
defmodule Notifier.Email do
  @moduledoc """
  Email adapter. In a real project this would call SMTP via a library like
  Swoosh; here we stub the network call and return `:ok` or `{:error, _}`
  based on input shape.
  """

  @behaviour Notifier

  @impl Notifier
  def deliver(recipient, message) do
    cond do
      not String.contains?(recipient, "@") ->
        {:error, :invalid_email}

      message == "" ->
        {:error, :empty_message}

      true ->
        # Network call would happen here; we keep the adapter side-effect-free
        # in this exercise so tests don't need a mock server.
        :ok
    end
  end
end
```

### Step 4: `lib/notifier/slack.ex`

```elixir
defmodule Notifier.Slack do
  @moduledoc """
  Slack adapter. Expects the recipient to be a channel name starting with `#`.
  Network call stubbed, same as Email.
  """

  @behaviour Notifier

  @impl Notifier
  def deliver("#" <> _channel, message) when message != "" do
    :ok
  end

  def deliver("#" <> _channel, "") do
    {:error, :empty_message}
  end

  def deliver(_recipient, _message) do
    {:error, :invalid_channel}
  end
end
```

### Step 5: `lib/notifier/test_adapter.ex`

```elixir
defmodule Notifier.TestAdapter do
  @moduledoc """
  Captures delivered messages into the calling process's mailbox so tests
  can assert on them with `assert_receive`. No network, no state — just
  `send(self(), ...)`.
  """

  @behaviour Notifier

  @impl Notifier
  def deliver(recipient, message) do
    send(self(), {:notifier_delivery, recipient, message})
    :ok
  end
end
```

### Step 6: `config/config.exs`

```elixir
import Config

# Default adapter in production-like builds. Tests override in setup.
config :notifier_adapter, :adapter, Notifier.Email
```

### Step 7: `test/notifier_test.exs`

```elixir
defmodule NotifierTest do
  # async: false because we mutate application config, which is global.
  use ExUnit.Case, async: false

  setup do
    # Snapshot the original adapter so later tests see the configured value.
    original = Application.get_env(:notifier_adapter, :adapter)
    on_exit(fn -> Application.put_env(:notifier_adapter, :adapter, original) end)
    :ok
  end

  describe "TestAdapter (captures into mailbox)" do
    setup do
      Application.put_env(:notifier_adapter, :adapter, Notifier.TestAdapter)
      :ok
    end

    test "delivers via whatever adapter is configured" do
      assert :ok = Notifier.deliver("anyone", "hi")
      assert_receive {:notifier_delivery, "anyone", "hi"}
    end
  end

  describe "Email adapter" do
    setup do
      Application.put_env(:notifier_adapter, :adapter, Notifier.Email)
      :ok
    end

    test "accepts a valid email and non-empty message" do
      assert :ok = Notifier.deliver("user@example.com", "hi")
    end

    test "rejects malformed emails" do
      assert {:error, :invalid_email} = Notifier.deliver("not-an-email", "hi")
    end

    test "rejects empty messages" do
      assert {:error, :empty_message} = Notifier.deliver("user@example.com", "")
    end
  end

  describe "Slack adapter" do
    setup do
      Application.put_env(:notifier_adapter, :adapter, Notifier.Slack)
      :ok
    end

    test "accepts channel-prefixed recipient" do
      assert :ok = Notifier.deliver("#alerts", "deploy failed")
    end

    test "rejects non-channel recipient" do
      assert {:error, :invalid_channel} = Notifier.deliver("alerts", "x")
    end
  end
end
```

### Step 8: Run

```bash
mix test
```

### Why this works

The behaviour declares the `deliver/2` contract once and the compiler enforces that every adapter implements it. The façade reads the adapter module from application env at each call, so tests use `Application.put_env/3` in `setup` and the next `Notifier.deliver/2` picks it up — no recompilation, no global mutation beyond the scoped config. `TestAdapter` turns delivery into a mailbox message, so `assert_receive` replaces network-dependent assertions.

---

## Benchmark

```elixir
Application.put_env(:notifier_adapter, :adapter, Notifier.TestAdapter)

{time, _} =
  :timer.tc(fn ->
    Enum.each(1..100_000, fn _ -> Notifier.deliver("dest", "msg") end)
  end)

IO.puts("avg dispatch: #{time / 100_000} µs")
```

Target esperado: <2 µs por llamada a `Notifier.deliver/2` con `TestAdapter` (runtime env lookup + mailbox send). Si migrás a `compile_env!/2`, esperá ~0.3–0.5 µs por llamada — el delta justifica la rigidez solo en hot paths.

---

## Trade-offs and production gotchas

**1. Runtime vs compile-time adapter lookup**
`Application.fetch_env!/2` at each call is ~microseconds. For most
notifications that's noise. For per-request RPC adapters (`Finch`, `Req`),
prefer `Application.compile_env!/2` to move the lookup to compile time —
pay the runtime-rigidity cost for latency.

**2. Adapters should NOT return `{:ok, _}` variants**
The Ecto convention (`:ok | {:error, reason}`) is cleaner than
(`{:ok, _} | {:error, _}`) for side-effect-only operations. Return data
only when the adapter is a query.

**3. Mock vs fake — use a fake**
A process-mailbox "TestAdapter" is a FAKE (real implementation, different
backend). A `Mox` verified mock is fine too, but fakes are usually enough
and don't require `async: false`. Pick fakes unless you specifically need
per-test expectation verification.

**4. Configuration precedence trap**
`config/config.exs` is compile-time; `config/runtime.exs` is boot-time.
If your adapter depends on a runtime value (environment variable), set it
in `runtime.exs` — otherwise releases ignore the env var entirely.

**5. When NOT to use an adapter behaviour**
If you have exactly one implementation and no near-term need for another,
don't. YAGNI. A direct `Notifier.Email.deliver(...)` is simpler; refactor
to a behaviour the day you add the second backend.

---

## Reflection

- You need to deliver via *both* Email and Slack on production alerts. Does the adapter pattern still fit, or do you need a "composite adapter" that calls a list of backends? What does that do to error semantics (any failure fails the call vs all-or-nothing)?
- A PR adds `Application.get_env(:notifier_adapter, :adapter)` inline at four different call sites "to avoid the indirection". What concretely breaks in async tests, and what's the shortest review comment that justifies reverting it?

---

## Resources

- [`Application.fetch_env!/2` — Elixir stdlib](https://hexdocs.pm/elixir/Application.html#fetch_env!/2)
- [`Swoosh.Adapter`](https://hexdocs.pm/swoosh/Swoosh.Adapter.html) — a real adapter behaviour
- [`Bamboo.Adapter`](https://hexdocs.pm/bamboo/Bamboo.Adapter.html)
- [`Mox`](https://hexdocs.pm/mox/Mox.html) — for explicit mocks over behaviours
- ["Mocks and explicit contracts" — José Valim](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/)
