# Exceptions, try/rescue, and defexception: Retryable HTTP Client

**Project**: `retry_http` — a minimal HTTP client wrapper that retries recoverable errors and surfaces unrecoverable ones

---

## Core concepts in this exercise

1. **`raise`, `try`/`rescue`, and `defexception`** — how exceptions actually work in Elixir and when to define your own.
2. **Exceptions vs `{:error, _}` tuples** — the senior-level distinction about which failure mode belongs to which tool.

---

## Why this matters for a senior developer

Elixir is often described as "you should never use exceptions, always return tuples."
That's an oversimplification that hurts real systems.

- `{:error, _}` is for **expected** failures: invalid input, missing records, 404s.
- `raise` is for **unexpected** failures: programmer errors, broken invariants, and
  deep stack unwinding where tuple plumbing would be pure noise.

A retry policy is the classic place where this distinction shines. Network timeouts
are recoverable and worth retrying. A 401 Unauthorized is not — retrying won't help.
A malformed URL at the call site is a bug and should crash loudly. Three categories,
three different mechanisms. Getting them right is a senior-level concern.

---

## Why exceptions and not only error tuples

**Option A — every failure returns `{:error, reason}`, no raises**
- Pros: uniform API; `with` composition is natural; no stack unwinding; callers cannot forget to rescue.
- Cons: a `nil` URL passed by accident becomes `{:error, :nil_url}` and propagates silently — bugs and business errors look identical.

**Option B — tuples for expected failures, exceptions for programmer errors and transport faults** (chosen)
- Pros: bugs crash with a useful stack trace; retryable transport faults are rescued at the boundary and converted to tuples; the two categories are visibly different in the code.
- Cons: requires discipline at the boundary — you must rescue *your* exceptions, not every Exception.

→ Chose **B** because the three failure categories (retryable, unrecoverable, programmer error) demand different mechanisms. Collapsing them into one shape hides the distinction that senior callers care about.

---

## Design decisions

**Option A — retry logic inside the transport module**
- Pros: callers never see the retry; one place to tune backoff.
- Cons: transport can't distinguish "this call is idempotent" from "this call mutates state"; retries silently duplicate POSTs.

**Option B — retry in `Client`, transport is a pure side-effecting function** (chosen)
- Pros: retry policy is explicit at the call site; unit tests inject a fake transport; idempotence is a caller concern, where it belongs.
- Cons: retry code is duplicated if multiple callers need retries; requires a helper.

→ Chose **B** because safety of retrying is context-dependent. The transport cannot know it; the caller must.

---

## Project structure

```
retry_http/
├── lib/
│   └── retry_http/
│       ├── client.ex
│       ├── errors.ex
│       └── transport.ex
├── script/
│   └── main.exs
├── test/
│   └── retry_http/
│       └── client_test.exs
└── mix.exs
```

---

## The business problem

Your service calls a flaky third-party billing API. The operational reality:

1. About 1% of requests time out or return 503. These should be retried with backoff.
2. About 0.1% return 401 (expired token) or 400 (bad request). These must NOT be
   retried — they need a fresh token or a code fix.
3. Occasionally the caller passes a `nil` URL because of a bug upstream. That's a
   programmer error and should crash with a clear stack trace.

You need a client that reacts differently to each class.

---

## Implementation

### `mix.exs`
```elixir
defmodule RetryHttp.MixProject do
  use Mix.Project

  def project do
    [
      app: :retry_http,
      version: "0.1.0",
      elixir: "~> 1.19"
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

**Objective**: Organize errors/transport/client into modules so exception hierarchy is visible at directory level.

```bash
mix new retry_http
cd retry_http
```

### Step 2: `mix.exs` and others (reading to find all Steps...)

**Objective**: Keep the dep list empty so the exception machinery rests on stdlib alone — `defexception` and `rescue` are fully self-contained.

```elixir
defmodule RetryHttp.MixProject do
  use Mix.Project

  def project do
    [
      app: :retry_http,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```
### `lib/retry_http.ex`

```elixir
defmodule RetryHttp do
  @moduledoc """
  Exceptions, try/rescue, and defexception: Retryable HTTP Client.

  Elixir is often described as "you should never use exceptions, always return tuples.".
  """
end
```
### `lib/retry_http/errors.ex`

**Objective**: Use `defexception` to give each failure mode a nominal type, so `rescue` can branch on category instead of parsing error messages.

```elixir
defmodule RetryHttp.Errors do
  @moduledoc """
  Custom exception types for the client.

  `defexception` generates a struct with a `:message` field plus an `exception/1`
  callback. Separating them by type lets callers rescue only what they know how
  to handle, which is the whole point of custom exceptions.
  """

  # Transient errors worth retrying: timeouts, 5xx, connection reset.
  defmodule RecoverableError do
    # `defexception` creates the struct AND implements the Exception behaviour.
    # We include `:reason` so the caller can inspect WHY the retry fired.
    defexception [:reason, :attempt, message: "recoverable transport error"]

    @impl true
    def exception(opts) do
      reason = Keyword.fetch!(opts, :reason)
      attempt = Keyword.get(opts, :attempt, 1)
      %__MODULE__{reason: reason, attempt: attempt, message: "recoverable: #{inspect(reason)}"}
    end
  end

  # Non-retryable failures: 4xx responses (except 408/429), malformed server reply.
  defmodule UnrecoverableError do
    defexception [:status, :body, message: "unrecoverable response"]

    @impl true
    def exception(opts) do
      status = Keyword.fetch!(opts, :status)
      body = Keyword.get(opts, :body, "")
      %__MODULE__{status: status, body: body, message: "unrecoverable status: #{status}"}
    end
  end
end
```
**Why custom exceptions and not plain `RuntimeError`:**

- Callers can `rescue RetryHttp.Errors.RecoverableError` and let other exceptions
  propagate. `rescue RuntimeError` rescues anything, including bugs you didn't mean
  to catch.
- The struct carries structured data (`:reason`, `:status`) instead of encoding it
  in a string. Logging and metrics can key off the field.

### `lib/retry_http/transport.ex`

**Objective**: Inject a fake transport behind a behaviour so tests can deterministically raise each exception class without any real network.

```elixir
defmodule RetryHttp.Transport do
  @moduledoc """
  A fake transport used by the client and tests.

  In production this would wrap `:httpc`, `Finch`, or `Req`. Keeping it behind a
  single function lets tests inject deterministic responses without mocking libs.
  """

  alias RetryHttp.Errors.{RecoverableError, UnrecoverableError}

  @type response :: %{status: pos_integer(), body: binary()}

  @doc """
  Performs a single HTTP request attempt. Raises on anything that is not a clean 2xx.

  Why raise instead of returning `{:error, _}`:
    The client layer wraps this in try/rescue to decide whether to retry. Raising
    keeps the happy-path code straight: a successful request returns the response
    directly with no tuple unwrapping at every call site.
  """
  @spec request(String.t(), (-> response() | {:error, atom()})) :: response()
  def request(url, fake_response_fn) when is_binary(url) do
    case fake_response_fn.() do
      %{status: status} = resp when status in 200..299 ->
        resp

      %{status: status} = resp when status in [408, 429] or status in 500..599 ->
        # 408 Request Timeout and 429 Too Many Requests ARE worth retrying,
        # same as 5xx. Treating them as recoverable is a spec-compliant choice.
        raise RecoverableError, reason: {:http_status, status}, attempt: 1

      %{status: status, body: body} ->
        raise UnrecoverableError, status: status, body: body

      {:error, reason} when reason in [:timeout, :econnrefused, :closed] ->
        raise RecoverableError, reason: reason, attempt: 1

      {:error, reason} ->
        raise UnrecoverableError, status: 0, body: "transport error: #{inspect(reason)}"
    end
  end

  # Pattern matching on `nil` would silently match. Guarding forces a FunctionClauseError
  # at the call site — this is the "fail fast" posture for programmer errors.
end
```
### `lib/retry_http/client.ex`

**Objective**: Keep retry logic out of the transport so `try/rescue` lives at the public boundary, where the retry policy is the business rule.

```elixir
defmodule RetryHttp.Client do
  @moduledoc """
  Public client. Wraps the transport with retry-on-recoverable-error semantics.
  """

  alias RetryHttp.Errors.{RecoverableError, UnrecoverableError}
  alias RetryHttp.Transport

  require Logger

  @default_max_attempts 3
  @default_backoff_ms 50

  @doc """
  Executes `fake_response_fn` against `url`, retrying recoverable errors up to
  `max_attempts` times with linear backoff.

  Returns:
    * `{:ok, response}` on success
    * `{:error, {:unrecoverable, status}}` for 4xx and similar
    * `{:error, {:exhausted, last_reason}}` after max_attempts recoverable failures

  Raises `ArgumentError` if `url` is nil — that's a bug, not a runtime condition.
  """
  @spec get(String.t(), (-> any()), keyword()) ::
          {:ok, Transport.response()}
          | {:error, {:unrecoverable, pos_integer()} | {:exhausted, any()}}
  def get(url, fake_response_fn, opts \\ [])

  def get(nil, _fn, _opts) do
    # Programmer error. Crashing is correct — there's nothing the caller can do
    # at runtime to "handle" a missing URL.
    raise ArgumentError, "url cannot be nil"
  end

  def get(url, fake_response_fn, opts) when is_binary(url) do
    max = Keyword.get(opts, :max_attempts, @default_max_attempts)
    backoff = Keyword.get(opts, :backoff_ms, @default_backoff_ms)
    attempt(url, fake_response_fn, 1, max, backoff, nil)
  end

  defp attempt(_url, _fn, n, max, _backoff, last_reason) when n > max do
    {:error, {:exhausted, last_reason}}
  end

  defp attempt(url, fake_response_fn, n, max, backoff, _last) do
    {:ok, Transport.request(url, fake_response_fn)}
  rescue
    # Only rescue what we planned to handle. Everything else (ArgumentError,
    # FunctionClauseError from a nil URL, etc.) propagates. That's intentional:
    # unknown exceptions are bugs, and swallowing them hides them in logs.
    e in RecoverableError ->
      Logger.warning("retry_http recoverable (attempt #{n}/#{max}): #{inspect(e.reason)}")
      if n < max, do: Process.sleep(backoff * n)
      attempt(url, fake_response_fn, n + 1, max, backoff, e.reason)

    e in UnrecoverableError ->
      # 4xx and friends: no retry, return a tuple. Callers expect this case
      # to be frequent enough that tuples are the right shape.
      {:error, {:unrecoverable, e.status}}
  end
end
```
### Why this works

- `rescue` binds the exception struct to `e`, so we can inspect `e.reason` and
  `e.status`. Without custom exceptions we'd be parsing string messages.
- The `when n > max` clause on `attempt/6` terminates recursion without another
  explicit `if`. Small wins like this compound into readable modules.
- `Process.sleep(backoff * n)` is linear backoff. For real production, use
  exponential backoff with jitter (see resources). Kept simple here to keep focus
  on the exception semantics.

### `test/retry_http_test.exs`

**Objective**: Exercise each exception branch so the retry policy is contract-checked: transient faults retry, permanent faults bubble out.

```elixir
defmodule RetryHttp.ClientTest do
  use ExUnit.Case, async: true
  doctest RetryHttp.Client

  alias RetryHttp.Client

  describe "get/3 — happy path" do
    test "returns the response when the transport succeeds on the first try" do
      fake = fn -> %{status: 200, body: "ok"} end
      assert {:ok, %{status: 200, body: "ok"}} = Client.get("https://x.test/", fake)
    end
  end

  describe "get/3 — recoverable errors" do
    test "retries on 503 and eventually succeeds" do
      # Use an Agent to simulate a flaky server across retries.
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      fake = fn ->
        n = Agent.get_and_update(counter, &{&1 + 1, &1 + 1})
        if n < 3, do: %{status: 503, body: "busy"}, else: %{status: 200, body: "finally"}
      end

      assert {:ok, %{status: 200, body: "finally"}} =
               Client.get("https://x.test/", fake, max_attempts: 5, backoff_ms: 1)
    end

    test "gives up after max_attempts with exhausted error" do
      fake = fn -> %{status: 503, body: ""} end
      assert {:error, {:exhausted, {:http_status, 503}}} =
               Client.get("https://x.test/", fake, max_attempts: 2, backoff_ms: 1)
    end

    test "retries on :timeout transport failures" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      fake = fn ->
        n = Agent.get_and_update(counter, &{&1 + 1, &1 + 1})
        if n < 2, do: {:error, :timeout}, else: %{status: 200, body: "recovered"}
      end

      assert {:ok, %{status: 200}} =
               Client.get("https://x.test/", fake, max_attempts: 5, backoff_ms: 1)
    end
  end

  describe "get/3 — unrecoverable errors" do
    test "returns {:error, {:unrecoverable, 401}} without retrying" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      fake = fn ->
        Agent.update(counter, &(&1 + 1))
        %{status: 401, body: "unauthorized"}
      end

      assert {:error, {:unrecoverable, 401}} =
               Client.get("https://x.test/", fake, max_attempts: 5, backoff_ms: 1)

      # Proves we did not retry — the counter is exactly 1.
      assert Agent.get(counter, & &1) == 1
    end

    test "does not retry on 400 Bad Request" do
      fake = fn -> %{status: 400, body: "bad input"} end
      assert {:error, {:unrecoverable, 400}} =
               Client.get("https://x.test/", fake, max_attempts: 5, backoff_ms: 1)
    end
  end

  describe "get/3 — programmer errors" do
    test "raises ArgumentError when the URL is nil" do
      assert_raise ArgumentError, "url cannot be nil", fn ->
        Client.get(nil, fn -> %{status: 200, body: ""} end)
      end
    end
  end
end
```
### Step 7: Run and verify

**Objective**: Run the suite to confirm no rescue block silently swallows an unrecognised exception — the classic "broad rescue" bug.

```bash
mix test --trace
mix compile --warnings-as-errors
```

All 7 tests must pass.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== RetryHttp: demo ===\n")

    result_1 = Agent.get(counter, & &1)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Mix.env()
    IO.puts("Demo 2: #{inspect(result_2)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

## Trade-off analysis

| Failure class              | Mechanism                       | Why                                            |
|----------------------------|---------------------------------|------------------------------------------------|
| Expected business failure  | `{:ok, _} / {:error, _}` tuple  | Caller always needs to handle it; no stack    |
| Recoverable transient      | Exception rescued by retry      | Retry policy lives in one place                |
| Unrecoverable transient    | Exception converted to tuple    | Caller can pattern-match without rescue        |
| Programmer error / bug     | `raise` without rescue          | Crash loud, get a stack trace, fix the bug     |

The rule of thumb: if a function can fail for reasons the caller must handle on
every call, use tuples. If failure is exceptional and usually deep in the call
stack, raise. If the failure is "the caller broke the contract," raise without
mercy.

---

## Common production mistakes

**1. `rescue _ -> ...` — rescuing everything**
The bare `rescue` clause rescues every exception, including `ArithmeticError`,
`UndefinedFunctionError`, and other bugs. You lose the stack trace and keep running
with broken state. Always rescue specific types.

**2. Returning `{:error, e}` with the whole exception struct**
The caller now depends on the exception implementation. Convert to a stable
contract (`{:error, :timeout}`, `{:error, {:unrecoverable, 401}}`) at the boundary.

**3. Using exceptions for control flow**
Raising and rescuing has a real cost — stack unwinding, process dictionary snapshot
(in older versions), heap allocation for the struct. For a loop iteration check,
use a tuple or a guard. Exceptions are not `goto`.

**4. Catching `:throw` and `:exit` by accident**
`rescue` only catches exceptions. `catch` catches throws and exits too. If your
rescue inexplicably doesn't fire, check whether the callee is using `throw` or
`exit` instead of `raise` (rare, but happens in Erlang interop).

**5. Forgetting to re-raise**
If you rescue, log, and do not re-raise, you silently swallow the error. Use
`reraise e, __STACKTRACE__` to rescue for a side effect (logging) but still let
it propagate.

---

## Benchmark

Measure the cost of the happy path (no raise) vs. a raised-and-rescued path so you can decide whether rescue is acceptable in a hot loop.

```elixir
happy = fn -> :ok end

raising = fn ->
  try do
    raise RetryHttp.Errors.RecoverableError, reason: :timeout
  rescue
    _ -> :ok
  end
end

{us_ok, _}    = :timer.tc(fn -> for _ <- 1..100_000, do: happy.() end)
{us_raise, _} = :timer.tc(fn -> for _ <- 1..100_000, do: raising.() end)

IO.puts("no raise:     #{us_ok / 100_000} µs")
IO.puts("raise+rescue: #{us_raise / 100_000} µs")
```
Target esperado: happy path <0.1 µs, raise+rescue 5–15 µs. A raise is 50–100× a normal function return. That is fine at an HTTP boundary (once per request); it is not fine inside an inner loop over a million records.

---

## Reflection

- The current retry policy is linear (`backoff * n`). If you replace it with exponential-plus-jitter, do you keep the retry logic in `Client`, move it into `:telemetry` handlers, or reach for a library like `Retry`? Which choice makes the *tests* harder?
- `RecoverableError` and `UnrecoverableError` are decided by HTTP status. What happens if a 500 actually means "the API is in a broken state and retrying will make it worse"? How would you promote a status-based classification to one that also reads the response body?

---

## When NOT to use exceptions

- **Validation failures the user must see**: return `{:error, reason}`. Form
  validation is the textbook "every caller handles it" case.
- **Pattern matching failures where the absent case is normal**: `Map.fetch/2`
  returns `:error`, not raises. That's because "key missing" is common.
- **Inside hot loops or pipelines**: the allocation and unwinding cost dominates
  anything you're measuring.

---

## Resources

- [Elixir `Kernel.defexception/1` — HexDocs](https://hexdocs.pm/elixir/Kernel.html#defexception/1)
- [Elixir `Kernel.SpecialForms.try/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1) — authoritative reference for `try/rescue/catch/after`
- [Erlang the Movie II: The Sequel — Joe Armstrong on "let it crash"](https://www.youtube.com/watch?v=xrIjfIjssLE)
- [AWS Architecture Blog — Exponential backoff and jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) — the retry math this exercise simplifies

---

## Key concepts
### 1. Exceptions Are for Exceptional Conditions

Exceptions represent things that should not happen in normal operation (bugs, system failures). Use `{:ok, result}` / `{:error, reason}` for expected failures (validation, not found). This distinction keeps code readable and performant.

### 2. Exception Types Matter for Rescue

```elixir
try do
  File.read!("nonexistent.txt")
rescue
  e in File.Error -> handle_io_error(e)
  e in RuntimeError -> handle_runtime_error(e)
end
```
Different exceptions trigger different handlers. Catching all exceptions (bare `rescue`) is rarely correct—you usually want specific types.

### 3. Exception Handling is Expensive

Raising and catching involves stack unwinding. For frequently-occurring failures (validation), use tuples. For rare, unexpected errors, exceptions are fine.

---
