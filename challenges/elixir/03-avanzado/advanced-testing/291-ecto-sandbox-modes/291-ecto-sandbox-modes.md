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

## Resources

- [`Ecto.Adapters.SQL.Sandbox`](https://hexdocs.pm/ecto_sql/Ecto.Adapters.SQL.Sandbox.html)
- [José Valim — Concurrent transactional tests](https://dashbit.co/blog/ecto-sql-3-changesets-are-data)
- [Phoenix test guide](https://hexdocs.pm/phoenix/testing.html)
- [`Oban.Testing`](https://hexdocs.pm/oban/Oban.Testing.html)
