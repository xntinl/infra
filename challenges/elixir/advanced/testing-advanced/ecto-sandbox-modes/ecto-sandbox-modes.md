# Ecto Sandbox Modes: Manual, Shared, and Allowances

**Project**: `billing_api` — a Postgres-backed API whose Ecto test suite runs concurrently without cross-test contamination.

---

## Why Ecto sandbox modes matter

`billing_api` has 800+ Ecto tests. Running them sequentially takes 90 seconds; running them
with `async: true` brings it to 12 seconds — but only if the sandbox is configured correctly.
Get one detail wrong and you get randomly failing tests with `connection ownership` errors,
or worse, a test that sees data from another test.

`Ecto.Adapters.SQL.Sandbox` wraps every test in a transaction that is rolled back on exit.
The complication is that in real applications, a test process spawns helper processes
(GenServers, Tasks, Oban jobs), and each helper needs access to the same sandboxed
connection. Ecto offers three modes to manage this: `:manual`, `:shared`, and `allow/4`.

---

## The business problem

Three scenarios must be addressed:

1. `:manual` with explicit checkout — best isolation, required for `async: true`. Only the
   test process owns the connection. Helpers must be explicitly allowed.
2. `:shared` — the test process shares its connection with any process that asks. Required
   for end-to-end tests that involve many disconnected processes. Forces `async: false`.
3. `allow/4` — keeps `:manual` mode but explicitly grants a named helper process (an Oban
   worker, a specific GenServer) access to the test's connection.

`:shared` always would make tests safe but slow — a shared connection cannot be used
concurrently without serialization. `:manual` always would break any test that hands off
work to a non-test process. Neither covers every case, so Ecto exposes all three.

---

## Project structure

```
billing_api/
├── config/
│   └── test.exs
├── lib/
│   └── billing_api/
│       ├── repo.ex
│       ├── invoices.ex
│       └── invoice_worker.ex       # spawns processes that must see test data
├── script/
│   └── main.exs
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

---

## Design decisions

- **Option A — `:shared` everywhere**: simple but kills concurrency (12s → 90s).
- **Option B — `:manual` + `allow/4` for helpers**: fast (async works), verbose (must
  enumerate allowed processes).
- **Option C — Hybrid**: `:manual` by default; individual tests opt into `:shared` when
  they genuinely need it. (chosen)

Chosen: **Option C**. Most tests stay isolated (`async: true`). Integration tests that
legitimately cross process boundaries opt in.

---

## Implementation

### `mix.exs`

```elixir
defmodule BillingApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :billing_api,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {BillingApi.Application, []}]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"}
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_),     do: ["lib"]
end
```

### `lib/billing_api.ex` (config highlights)

```elixir
# config/test.exs
import Config

config :billing_api, BillingApi.Repo,
  username: "postgres",
  password: "postgres",
  database: "billing_api_test",
  hostname: "localhost",
  pool_size: 20,
  pool: Ecto.Adapters.SQL.Sandbox
```

### `test/support/data_case.ex`

```elixir
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

  def setup_sandbox(tags) do
    pid = Ecto.Adapters.SQL.Sandbox.start_owner!(BillingApi.Repo, shared: not tags[:async])
    on_exit(fn -> Ecto.Adapters.SQL.Sandbox.stop_owner(pid) end)
  end
end
```

### `test/billing_api_test.exs`

```elixir
# test/billing_api/invoices_test.exs
defmodule BillingApi.InvoicesTest do
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

# test/billing_api/invoice_worker_test.exs
defmodule BillingApi.InvoiceWorkerTest do
  use BillingApi.DataCase, async: true

  alias BillingApi.{Invoices, InvoiceWorker, Repo}

  describe "process_batch/1 — with allow/4" do
    test "worker can see rows created by the test" do
      {:ok, _} = Invoices.create(%{amount_cents: 50, currency: "EUR"})
      worker = Process.whereis(InvoiceWorker)
      :ok = Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), worker)

      assert {:ok, processed} = InvoiceWorker.process_batch(1)
      assert processed == 1
    end
  end
end

# test/billing_api/integration_test.exs
defmodule BillingApi.IntegrationTest do
  use BillingApi.DataCase, async: false

  describe "full invoice pipeline" do
    test "invoice flows through validation, persistence, and notification" do
      {:ok, _} = BillingApi.Pipeline.run(%{amount_cents: 999, currency: "USD"})
      assert BillingApi.Repo.aggregate(BillingApi.Invoices.Invoice, :count) == 1
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== Ecto Sandbox Demo ===")
    IO.puts("This example illustrates sandbox patterns for test isolation.")
    IO.puts("See test/ for executable sandbox tests with the real repo.")
    IO.puts("=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs` (or `mix test` for the real suite).

---

## Key concepts

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

### 4. `start_owner!/2` with `shared: not async`
When `async: true`, the owner is the test pid only. When `async: false`, the owner shares
its connection with the world for the duration of the test. `stop_owner/1` rolls back
the transaction deterministically.

### 5. Production gotchas

- Running Repo queries in `Task.async` without `allow/4` raises `DBConnection.OwnershipError`.
- `async: true` with `:shared` is impossible — random failures.
- Pool size too small causes queue waits; size it to `System.schedulers_online() * 2` at minimum.
- Oban workers run in their own processes; use `Oban.Testing` which integrates with the sandbox.
- For tests exercising real transactional behaviour (deadlocks, isolation levels), the
  sandbox hides precisely what you want to observe — use a dedicated `async: false` suite.

---

## Resources

- [`Ecto.Adapters.SQL.Sandbox`](https://hexdocs.pm/ecto_sql/Ecto.Adapters.SQL.Sandbox.html)
- [Ecto testing guide](https://hexdocs.pm/ecto/testing-with-ecto.html)
- [Oban.Testing](https://hexdocs.pm/oban/Oban.Testing.html)
