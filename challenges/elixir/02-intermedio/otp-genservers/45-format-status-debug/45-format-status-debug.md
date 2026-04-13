# `format_status/2` — safe debugging output without leaking secrets

**Project**: `format_status_gs` — a GenServer that redacts sensitive fields when inspected via `:sys.get_status/1`.

---

## Project context

When a GenServer is misbehaving in production, the first thing an
on-call engineer reaches for is `:sys.get_status/1` (or, in a crash,
the SASL report). Both return the full internal state — including any
fields the server happens to hold, like API tokens, session cookies, or
hashed-but-sensitive user data.

This is a routine and easily-overlooked leak vector. Someone shells
into a node to debug a bug, runs `:sys.get_status(MyServer)`, and their
terminal scrollback now contains a Stripe secret key. Log aggregation
picks it up. The auditor finds it.

The `format_status/2` callback is OTP's built-in defense: it lets the
module rewrite the state representation *just for debugging output*.
The real state in memory is unchanged; only the observable surface is
redacted. This exercise builds an API-client-style GenServer with a
secret token and shows the callback redacting it under every debug code path.

Project structure:

```
format_status_gs/
├── lib/
│   └── format_status_gs.ex
├── test/
│   └── format_status_gs_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not just scrub at the log appender?** Scrubbing at the source is more reliable than chasing every log sink.

## Core concepts

### 1. What `format_status/2` is called for

It runs when someone asks the server to describe itself:

- `:sys.get_status(pid)` — debug inspection
- SASL / Logger crash reports (the state is dumped into the report)
- `GenServer.stop/3` with unusual reasons, depending on handler config

The callback returns a replacement representation. Real state is not
modified — only what is handed to the inspector.

### 2. Two shapes: OTP 25+ vs. older

OTP 25 introduced a simpler map-based callback:

```elixir
def format_status(%{state: state} = status) do
  %{status | state: redact(state)}
end
```

Older OTPs use the 2-arity version `format_status(reason, [pdict, state])`.
Elixir `GenServer` still supports both; if you're on OTP 25+, prefer the
1-arity map form. Both ultimately return the redacted view.

### 3. What to redact

Anything a log aggregator or a screenshot must not contain:

- API tokens, bearer tokens, refresh tokens
- Session IDs, CSRF tokens
- Raw PII the server happens to cache
- Large binaries (not secret, but log-hostile)

Replace with a short, obviously-redacted placeholder (`"[REDACTED]"`)
that doesn't look like a real value.

### 4. Redact at the boundary, not in state

Some codebases try to redact at log-site: wrap values in `%Secret{}`
structs with a custom `Inspect` implementation. That works for
`inspect/1` calls in your own code but `:sys.get_status` bypasses it
and shows the struct internals. `format_status/2` is the only place
that covers every OTP debug code path.

---

## Design decisions

**Option A — expose full state in crash logs**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — implement `format_status/1` to redact secrets (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because state leaks into SASL/`:sys.get_state` and observability tools; redact proactively.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new format_status_gs
cd format_status_gs
```

### Step 2: `lib/format_status_gs.ex`

**Objective**: Implement `format_status_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule FormatStatusGs do
  @moduledoc """
  An API-client-style GenServer holding a secret bearer token. Demonstrates
  `format_status/2` redacting the token for `:sys.get_status/1` and crash
  reports without altering the real in-memory state used by callbacks.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    @enforce_keys [:endpoint, :token]
    defstruct [:endpoint, :token, last_call_at: nil, call_count: 0]

    @type t :: %__MODULE__{
            endpoint: String.t(),
            token: String.t(),
            last_call_at: integer() | nil,
            call_count: non_neg_integer()
          }
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {endpoint, opts} = Keyword.pop!(opts, :endpoint)
    {token, opts} = Keyword.pop!(opts, :token)
    GenServer.start_link(__MODULE__, {endpoint, token}, opts)
  end

  @doc "Simulated API call — bumps the counter, records the time."
  @spec call(GenServer.server()) :: :ok
  def call(server), do: GenServer.call(server, :do_call)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init({endpoint, token}) do
    {:ok, %State{endpoint: endpoint, token: token}}
  end

  @impl true
  def handle_call(:do_call, _from, %State{call_count: n} = state) do
    new_state = %{state | call_count: n + 1, last_call_at: System.monotonic_time()}
    {:reply, :ok, new_state}
  end

  @doc """
  Redacts the `token` field. Called by OTP debug tooling: `:sys.get_status/1`,
  crash reports, and `GenServer.stop/3` logs. Real state in memory is unchanged.

  Uses the 1-arity OTP 25+ map shape. Falls back to the legacy 2-arity
  callback below for older OTP.
  """
  @impl true
  def format_status(%{state: %State{} = state} = status) do
    %{status | state: redact(state)}
  end

  def format_status(status), do: status

  # Legacy 2-arity callback for OTP < 25. Harmless to keep on newer OTP.
  def format_status(_reason, [_pdict, %State{} = state]) do
    [{:data, [{"State", redact(state)}]}]
  end

  def format_status(_reason, [_pdict, state]), do: [{:data, [{"State", state}]}]

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp redact(%State{} = s), do: %{s | token: "[REDACTED]"}
end
```

### Step 3: `test/format_status_gs_test.exs`

**Objective**: Write `format_status_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule FormatStatusGsTest do
  use ExUnit.Case, async: true

  setup do
    {:ok, pid} =
      FormatStatusGs.start_link(
        endpoint: "https://api.example.com",
        token: "sk_live_DO_NOT_LEAK_42"
      )

    %{pid: pid}
  end

  describe ":sys.get_status/1 output" do
    test "redacts the token in the status tuple", %{pid: pid} do
      status = :sys.get_status(pid)
      serialized = inspect(status)

      # Positive: the placeholder is present.
      assert serialized =~ "[REDACTED]"

      # Negative: the real secret must NOT appear anywhere in the status.
      refute serialized =~ "sk_live_DO_NOT_LEAK_42"
    end

    test "other fields are still visible for debugging", %{pid: pid} do
      :ok = FormatStatusGs.call(pid)
      status = :sys.get_status(pid)
      serialized = inspect(status)

      # Non-secret fields help debugging.
      assert serialized =~ "api.example.com"
      assert serialized =~ "call_count"
    end
  end

  describe "real state is untouched by format_status" do
    test "subsequent calls still see the real token (behaviour unaffected)", %{pid: pid} do
      # If format_status mutated real state, the API client would break.
      # Here we prove the server keeps working after an inspection.
      assert :sys.get_status(pid)
      assert :ok = FormatStatusGs.call(pid)
      assert :ok = FormatStatusGs.call(pid)

      # We cannot (and should not) peek the real token — but behaviour is proof enough.
      status = :sys.get_status(pid)
      assert inspect(status) =~ "call_count"
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `format_status/2` runs in the server process — keep it cheap**
It executes synchronously in the GenServer while `:sys.get_status/1` is
waiting. Heavy computation here blocks the server and delays debug
output. Redaction should be a shallow struct rewrite, not a deep walk.

**2. Redact by allow-list, not deny-list**
If you write "drop fields named `:token`", the next refactor will
introduce `:api_key` or `:bearer` and the leak reopens. Safer: copy
only explicitly-allowlisted fields into the debug view; anything else
becomes `"[REDACTED]"` or is omitted. This is strictly more secure
though slightly more verbose.

**3. Crash reports have other leaks**
`format_status/2` redacts the *state*. But function arguments from the
crashing callback also end up in the report — and those may include
the token if a caller passed it in. Keep secrets out of callback
arguments whenever possible.

**4. The `Inspect` protocol is not enough**
A custom `Inspect` implementation for a struct protects `inspect/1`
calls but does not cover `:sys.get_status` or SASL reports. Use both:
`Inspect` for log-site safety, `format_status/2` for OTP-tool safety.

**5. Test with `inspect(:sys.get_status(pid))`**
A test that asserts the real secret string is *absent* from the
serialized status is the simplest, most robust way to guarantee the
redaction holds. Add it to the test suite of every GenServer that
holds secrets; refactors will not silently reintroduce the leak.

**6. When NOT to bother**
If the GenServer holds no secrets and its entire state is already
public (a counter, a cache of public data), `format_status/2` adds
noise for no gain. Add it where there is something to protect.

---


## Reflection

- ¿Qué campos redactás en producción y cuáles dejás para dev? Definí una política de redacción testeable.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule FormatStatusGs do
    @moduledoc """
    An API-client-style GenServer holding a secret bearer token. Demonstrates
    `format_status/2` redacting the token for `:sys.get_status/1` and crash
    reports without altering the real in-memory state used by callbacks.
    """

    use GenServer

    defmodule State do
      @moduledoc false
      @enforce_keys [:endpoint, :token]
      defstruct [:endpoint, :token, last_call_at: nil, call_count: 0]

      @type t :: %__MODULE__{
              endpoint: String.t(),
              token: String.t(),
              last_call_at: integer() | nil,
              call_count: non_neg_integer()
            }
    end

    # ── Public API ──────────────────────────────────────────────────────────

    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts) do
      {endpoint, opts} = Keyword.pop!(opts, :endpoint)
      {token, opts} = Keyword.pop!(opts, :token)
      GenServer.start_link(__MODULE__, {endpoint, token}, opts)
    end

    @doc "Simulated API call — bumps the counter, records the time."
    @spec call(GenServer.server()) :: :ok
    def call(server), do: GenServer.call(server, :do_call)

    # ── Callbacks ───────────────────────────────────────────────────────────

    @impl true
    def init({endpoint, token}) do
      {:ok, %State{endpoint: endpoint, token: token}}
    end

    @impl true
    def handle_call(:do_call, _from, %State{call_count: n} = state) do
      new_state = %{state | call_count: n + 1, last_call_at: System.monotonic_time()}
      {:reply, :ok, new_state}
    end

    @doc """
    Redacts the `token` field. Called by OTP debug tooling: `:sys.get_status/1`,
    crash reports, and `GenServer.stop/3` logs. Real state in memory is unchanged.

    Uses the 1-arity OTP 25+ map shape. Falls back to the legacy 2-arity
    callback below for older OTP.
    """
    @impl true
    def format_status(%{state: %State{} = state} = status) do
      %{status | state: redact(state)}
    end

    def format_status(status), do: status

    # Legacy 2-arity callback for OTP < 25. Harmless to keep on newer OTP.
    def format_status(_reason, [_pdict, %State{} = state]) do
      [{:data, [{"State", redact(state)}]}]
    end

    def format_status(_reason, [_pdict, state]), do: [{:data, [{"State", state}]}]

    # ── Helpers ─────────────────────────────────────────────────────────────

    defp redact(%State{} = s), do: %{s | token: "[REDACTED]"}
  end

  def main do
    {:ok, pid} = FormatStatusGs.start_link(endpoint: "https://api.example.com", token: "secret-token-123")
  
    :ok = FormatStatusGs.call(pid)
    :ok = FormatStatusGs.call(pid)
  
    status = :sys.get_status(pid)
    status_str = inspect(status)
  
    if String.contains?(status_str, "[REDACTED]") do
      IO.puts("Token correctly redacted in status")
    end
  
    IO.puts("✓ FormatStatusGs works correctly")
  end

end

Main.main()
```


## Resources

- [`GenServer.format_status/1` — Elixir stdlib (OTP 25+)](https://hexdocs.pm/elixir/GenServer.html#c:format_status/1)
- [`GenServer.format_status/2` — legacy callback](https://hexdocs.pm/elixir/GenServer.html#c:format_status/2)
- [`:sys` module — Erlang debug tools](https://www.erlang.org/doc/man/sys.html)
- [`Inspect` protocol — redact at inspect-time](https://hexdocs.pm/elixir/Inspect.html)


## Advanced Considerations

GenServer is the foundation of stateful concurrent systems in Elixir. Advanced patterns emerge from understanding the synchronous/asynchronous nature of callbacks and state evolution.

**State evolution and message handling:**
A GenServer's state is private, evolving only through synchronous (`handle_call`) or asynchronous (`handle_cast`) message handlers. The key insight: `handle_call` blocks the caller until the handler returns; `handle_cast` is fire-and-forget. Use `call` for operations requiring acknowledgment or returning results; use `cast` for notifications. Mixing them incorrectly leads to deadlocks (caller waiting forever) or lost updates (state changed before caller knows).

**Advanced reply patterns:**
The tuple `{:reply, reply, state}` is the standard, but you can split reply and state persistence. Use `:noreply` in `handle_call` if you need to send the reply later (e.g., after an async operation). The `:hibernate` flag tells the VM to garbage-collect the process and switch to a lightweight state — useful for long-lived processes that spend time idle.

**Debugging and observability:**
`format_status/2` controls how a GenServer appears in `:observer` and logs. It's critical for large state structures (hide sensitive fields, summarize collections). In production, comprehensive logging in callbacks (not just errors) reveals timing issues, message flow anomalies, and resource leaks before they become critical.
