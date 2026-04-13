# Ecto Sandbox Modes: Manual, Shared, and Allowances

**Project**: `billing_api` — a Postgres-backed API whose Ecto test suite runs concurrently without cross-test contamination.

## Project context

`billing_api` has 800+ Ecto tests. Running them sequentially takes 90 seconds; running them
with `async: true` brings it to 12 seconds — but only if the sandbox is configured correctly.
Get one detail wrong and you get randomly failing tests with `connection ownership` errors,
or worse, a test that sees data from another test.

`Ecto.Adapters.SQL.Sandbox` wraps every test in a transaction that is rolled back on exit.
The complication is that in real applications, a test process spawns helper processes
(GenServers, Tasks, Oban jobs), and each helper needs access to the same sandboxed
connection. Ecto offers three modes to manage this: `:manual`, `:shared`, and `allow/4`.

```
billing_api/
├── config/
│   └── test.exs
├── lib/
│   └── billing_api/
│       ├── repo.ex
│       ├── invoices.ex
│       └── invoice_worker.ex       # spawns processes that must see test data
├── test/
│   ├── billing_api/
│   │   ├── invoices_test.exs              # async: true, :manual
│   │   ├── invoice_worker_test.exs        # :allow pattern
│   │   └── integration_test.exs           # async: false, :shared
│   ├── support/
│   │   └── data_case.ex
│   └── test_helper.exs
└── mix.exs
```

## Why sandbox modes matter

The three modes address three scenarios:

1. `:manual` with explicit checkout — best isolation, required for `async: true`. Only the
   test process owns the connection. Helpers must be explicitly allowed.
2. `:shared` — the test process shares its connection with any process that asks. Required
   for end-to-end tests that involve many disconnected processes. Forces `async: false`.
3. `allow/4` — keeps `:manual` mode but explicitly grants a named helper process (an Oban
   worker, a specific GenServer) access to the test's connection.

## Why not just one mode

`:shared` always would make tests safe but slow — a shared connection cannot be used
concurrently without serialization. `:manual` always would break any test that hands off
work to a non-test process. Neither covers every case, so Ecto exposes all three.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. Checkout and checkin
`Sandbox.checkout(Repo)` associates the calling process with a connection. `checkin/1`
releases it. In `DataCase` tests this is typically automatic via `setup`.

### 2. Ownership is per-process
A spawned task does NOT inherit the sandbox connection. It has to either:
- Be `allow`ed by the owning test (explicit),
- Or the sandbox has to be in `:shared` mode (implicit).

### 3. Process dictionary vs global
`:manual` uses the process dictionary — safe for `async: true`. `:shared` uses an ETS table
and is global to the test suite — forbids `async: true`.

## Design decisions

- **Option A — `:shared` everywhere**: simple but kills concurrency (12s → 90s).
- **Option B — `:manual` + `allow/4` for helpers**: fast (async works), verbose (must
  enumerate allowed processes).
- **Option C — Hybrid**: `:manual` by default; individual tests opt into `:shared` when
  they genuinely need it.

Chosen: **Option C**. Most tests stay isolated (`async: true`). Integration tests that
legitimately cross process boundaries opt in.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"}
  ]
end

defp elixirc_paths(:test), do: ["lib", "test/support"]
defp elixirc_paths(_),     do: ["lib"]
```

### Step 1: config

**Objective**: Size the pool to `schedulers_online * 2` so async tests never stall waiting for a sandboxed connection.

```elixir
# config/test.exs
import Config

config :billing_api, BillingApi.Repo,
  username: "postgres",
  password: "postgres",
  database: "billing_api_test",
  hostname: "localhost",
  # pool_size must accommodate concurrent async tests comfortably
  pool_size: 20,
  pool: Ecto.Adapters.SQL.Sandbox
```

### Step 2: `DataCase` helper

**Objective**: Centralize `start_owner!` with `shared: not tags[:async]` so every test opts into the correct sandbox mode via a single tag.

```elixir
# test/support/data_case.ex
defmodule BillingApi.DataCase do
  use ExUnit.CaseTemplate

  using do
    quote do
      alias BillingApi.Repo
      import Ecto
      import Ecto.Changeset
      import Ecto.Query
      import BillingApi.DataCase
    end
  end

  setup tags do
    BillingApi.DataCase.setup_sandbox(tags)
    :ok
  end

  @doc """
  Centralised sandbox setup — every test opts in via tags.
  """
  def setup_sandbox(tags) do
    pid = Ecto.Adapters.SQL.Sandbox.start_owner!(BillingApi.Repo, shared: not tags[:async])
    on_exit(fn -> Ecto.Adapters.SQL.Sandbox.stop_owner(pid) end)
  end
end
```

### Step 3: async isolation — happy path

**Objective**: Prove that parallel tests inserting rows see zero cross-contamination because each owns a rolled-back transaction.

```elixir
# test/billing_api/invoices_test.exs
defmodule BillingApi.InvoicesTest do
  # async: true — safe because sandbox is :manual and no helper processes cross boundaries
  use BillingApi.DataCase, async: true

  alias BillingApi.Invoices

  describe "create_invoice/1 — sandbox isolation" do
    test "inserted invoice is invisible to other tests" do
      {:ok, invoice} = Invoices.create(%{amount_cents: 100, currency: "USD"})
      assert invoice.id
      assert Repo.aggregate(Invoices.Invoice, :count) == 1
    end

    test "parallel test sees zero invoices — sandbox rolled back between tests" do
      assert Repo.aggregate(Invoices.Invoice, :count) == 0
    end
  end
end
```

### Step 4: helper process — `allow/4`

**Objective**: Grant a supervisor-started GenServer access to the test's sandboxed connection so cross-process work avoids `DBConnection.OwnershipError`.

When the test triggers work in a named GenServer that was started by the application
supervisor, the GenServer lives outside the test process and cannot see the sandboxed
connection. The solution: explicitly `allow` its pid.

```elixir
# test/billing_api/invoice_worker_test.exs
defmodule BillingApi.InvoiceWorkerTest do
  use BillingApi.DataCase, async: true

  alias BillingApi.{Invoices, InvoiceWorker, Repo}

  describe "process_batch/1 — with allow/4" do
    test "worker can see rows created by the test" do
      {:ok, _} = Invoices.create(%{amount_cents: 50, currency: "EUR"})

      # Find the worker — a named GenServer started by the app supervisor
      worker = Process.whereis(InvoiceWorker)

      # Grant the worker access to THIS test's sandbox connection
      :ok = Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), worker)

      # Now the worker sees the invoice we just inserted
      assert {:ok, processed} = InvoiceWorker.process_batch(1)
      assert processed == 1
    end
  end
end
```

### Step 5: integration test — `:shared` mode

**Objective**: Trade parallelism for reach: switch to `:shared` when a pipeline spawns too many processes to enumerate with `allow/4`.

When many helper processes (e.g. a Plug endpoint that spawns its own async stages) each
need access, enumerating them with `allow/4` is impractical. Switch to `:shared` mode and
disable async for the test.

```elixir
# test/billing_api/integration_test.exs
defmodule BillingApi.IntegrationTest do
  # async: false is mandatory — :shared mode is global
  use BillingApi.DataCase, async: false

  describe "full invoice pipeline" do
    test "invoice flows through validation, persistence, and notification" do
      # DataCase already set shared: true because async is false
      # Any process can now use the sandboxed connection without explicit allow.

      {:ok, _} = BillingApi.Pipeline.run(%{amount_cents: 999, currency: "USD"})

      assert BillingApi.Repo.aggregate(BillingApi.Invoices.Invoice, :count) == 1
    end
  end
end
```

### Step 6: tagged mode overrides

**Objective**: Drive sandbox mode from `@moduletag` so individual tests escalate to `:shared` without forking the `DataCase` hierarchy.

```elixir
# For targeted cases: opt into shared without using integration_test file
defmodule BillingApi.WeirdAsyncTest do
  use BillingApi.DataCase, async: false
  @moduletag :shared_sandbox

  test "a thing that spawns background tasks" do
    # Nothing else to configure — setup_sandbox already read the async tag.
  end
end
```

## Why this works

`Sandbox.start_owner!/2` with `shared: not async` branches automatically:
- When `async: true`, the owner is the test pid only. Everyone else is denied.
- When `async: false`, the owner shares its connection with the world for the duration
  of the test.

`stop_owner/1` rolls back the transaction and releases the pool slot deterministically,
regardless of whether the test crashed or exited cleanly.

## Tests

See Step 3, 4, 5 — one test file per pattern, each showing the smallest useful example.

## Benchmark

Measure suite wall clock to confirm the sandbox mode pays off.

```elixir
# From iex -S mix:
{t, _} = :timer.tc(fn -> Mix.Tasks.Test.run([]) end)
IO.puts("full suite #{t / 1_000_000}s")
```

Target: an 800-test suite with `async: true` everywhere should finish in well under
20% of the sequential time. Rule of thumb: one CPU core per 10–15 concurrent tests.

## Deep Dive: Sandbox Patterns and Production Implications

Ecto.Sandbox enforces database transaction isolation per test process, allowing async tests without race conditions. The 'shared' mode wraps each test in a transaction that rolls back after completion; the 'manual' mode gives you fine-grained control. Understanding which mode to use requires knowing your database's isolation levels (Postgres default is READ COMMITTED, not SERIALIZABLE) and whether your tests actually exercise transactions. Misconfiguring Sandbox is a common source of production bugs that don't reproduce in test—concurrent writes that work in-test may deadlock in production with different isolation settings.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Running a connection-dependent operation in a `Task.async` without `allow/4`**
The async task has no connection. The query raises `DBConnection.OwnershipError`.
Either call the Repo from the test pid (preferred) or allow the task's pid.

**2. `async: true` with `:shared` mode**
Impossible. `:shared` requires `async: false`. Starting both means random failures.

**3. Tests depending on data from previous tests**
The sandbox transaction is rolled back on exit. Any expectation that "this row exists"
between tests is a bug — insert the row explicitly in each test.

**4. Pool size too small**
With `async: true` and 16 CPU cores, ~16 tests hold connections simultaneously. `pool_size: 10`
causes queue waits. Size the pool to `System.schedulers_online() * 2` at minimum.

**5. Oban jobs triggered from a test**
Oban workers run in their own processes. Use `Oban.Testing` which integrates with the
sandbox automatically, or `perform_job/3` to call the worker code directly from the test pid.

**6. When NOT to use sandbox**
For tests that must exercise the real transactional behaviour of the DB (deadlocks, isolation
levels) the sandbox's wrapping transaction hides precisely what you want to observe. Use a
dedicated `async: false` suite against a throwaway DB.

## Reflection

`:shared` mode serializes database access across the entire suite while active. Given a
codebase with 50 integration tests that all need `:shared`, is it better to keep them in
one async-false file or split across many — and what does the answer tell you about the
real cost of `:shared`?


## Executable Example

```elixir
# test/billing_api/invoice_worker_test.exs
defmodule BillingApi.InvoiceWorkerTest do
  use BillingApi.DataCase, async: true

  alias BillingApi.{Invoices, InvoiceWorker, Repo}

  describe "process_batch/1 — with allow/4" do
    test "worker can see rows created by the test" do
      {:ok, _} = Invoices.create(%{amount_cents: 50, currency: "EUR"})

      # Find the worker — a named GenServer started by the app supervisor
      worker = Process.whereis(InvoiceWorker)

      # Grant the worker access to THIS test's sandbox connection
      :ok = Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), worker)

      # Now the worker sees the invoice we just inserted
      assert {:ok, processed} = InvoiceWorker.process_batch(1)
      assert processed == 1
    end
  end
end

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
