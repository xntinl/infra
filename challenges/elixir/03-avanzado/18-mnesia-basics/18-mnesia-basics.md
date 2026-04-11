# Mnesia Basics

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway handles authentication and session management
for the platform. Sessions must:
- Survive individual node restarts (DETS is per-node and doesn't replicate)
- Be readable from any node in the cluster (a client may hit any node)
- Support transactional operations — renewing a session and invalidating old ones
  must be atomic

DETS answered the durability question. Mnesia answers the distributed consistency question.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       ├── metrics/
│       ├── config/
│       └── auth/
│           ├── session_store.ex     # ← you implement this
│           └── account_system.ex   # ← and this
├── test/
│   └── api_gateway/
│       └── auth/
│           ├── session_store_test.exs
│           └── account_system_test.exs
└── mix.exs
```

---

## The business problem

Two requirements:

1. **Session store**: the gateway validates session tokens on every request. Sessions
   must be readable from any node, must survive node restarts, and must support
   "invalidate all sessions for user X" atomically.

2. **Credit transfers between accounts**: a billing module needs atomic debit + credit.
   Both updates must succeed or neither happens. A crash mid-transfer must not leave
   accounts in an inconsistent state.

---

## Why Mnesia and not DETS + replication

DETS is per-node. Sharing it across nodes would require a distributed file system
(NFS, FUSE) with all the associated failure modes. Mnesia is designed from the ground up
for distributed use: it replicates tables to multiple nodes, coordinates writes with
distributed locks, and recovers consistently after node failures.

## The Mnesia bootstrapping order

Mnesia has a strict initialization sequence that cannot be skipped:

```
1. :mnesia.create_schema([node()]) — once per node, persists to disk
2. :mnesia.start()                 — start the Mnesia application
3. :mnesia.create_table(...)       — define tables (idempotent with {:aborted, {:already_exists, T}})
4. :mnesia.wait_for_tables(...)    — ALWAYS wait before making queries
```

Skipping step 4 causes `{:aborted, {no_exists, TableName}}` errors on the first query
because Mnesia loads tables asynchronously.

## Transactional vs dirty operations

Every Mnesia read or write can be transactional or dirty:

```elixir
# Transactional: ACID, distributed lock, ~3-10x slower
:mnesia.transaction(fn ->
  [session] = :mnesia.read({Session, id}, :write)
  :mnesia.write(%{session | expires_at: new_expiry})
end)

# Dirty: no lock, no atomicity, ~10-100x faster
:mnesia.dirty_read(Session, id)
:mnesia.dirty_write(%Session{...})
```

Use dirty operations for reads of data that changes infrequently (cached config,
active session lookups where stale data is acceptable). Use transactions for any
operation that reads and then writes — without a write lock, two concurrent processes
can read the same value and both write conflicting updates.

---

## Implementation

### Step 1: `lib/api_gateway/auth/session_store.ex`

```elixir
defmodule ApiGateway.Auth.SessionStore do
  @moduledoc """
  Session store backed by Mnesia with disc_copies replication.

  Call setup/0 once at application startup (before start/0).
  The table is replicated to all nodes that call setup/0.
  """

  @table __MODULE__
  @ttl_seconds 3_600

  defstruct [:id, :user_id, :token, :expires_at, :metadata]

  # ---------------------------------------------------------------------------
  # Schema and table setup — call once at startup
  # ---------------------------------------------------------------------------

  @spec setup() :: :ok
  def setup do
    nodes = [node()]

    case :mnesia.create_schema(nodes) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
    end

    :ok = :mnesia.start()

    case :mnesia.create_table(@table, [
      attributes: [:id, :user_id, :token, :expires_at, :metadata],
      disc_copies: nodes,
      type: :set,
      index: [:user_id, :token]
    ]) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, @table}} -> :ok
    end

    # ALWAYS wait for tables before making queries
    :ok = :mnesia.wait_for_tables([@table], 10_000)
  end

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec create(pos_integer(), map()) :: {:ok, t()} | {:error, term()}
  def create(user_id, metadata \\ %{}) do
    session = %__MODULE__{
      id: :crypto.strong_rand_bytes(16) |> Base.url_encode64(padding: false),
      user_id: user_id,
      token: :crypto.strong_rand_bytes(32) |> Base.url_encode64(padding: false),
      expires_at: :erlang.system_time(:second) + @ttl_seconds,
      metadata: metadata
    }

    # HINT: :mnesia.transaction(fn -> :mnesia.write(session) end)
    # HINT: pattern match {:atomic, :ok} and return {:ok, session}
    # TODO: implement
  end

  @spec get_by_token(String.t()) :: {:ok, t()} | {:error, :not_found | :expired}
  def get_by_token(token) do
    # HINT: :mnesia.dirty_index_read(@table, token, :token)
    # HINT: check expires_at > :erlang.system_time(:second)
    # TODO: implement
  end

  @spec get_active_for_user(pos_integer()) :: [t()]
  def get_active_for_user(user_id) do
    now = :erlang.system_time(:second)
    sessions = :mnesia.dirty_index_read(@table, user_id, :user_id)
    Enum.filter(sessions, &(&1.expires_at > now))
  end

  @spec renew(String.t()) :: {:ok, t()} | {:error, :not_found}
  def renew(session_id) do
    # HINT: :mnesia.transaction with :mnesia.read({@table, session_id}, :write)
    # HINT: write lock (:write) prevents concurrent renew on the same session
    # HINT: :mnesia.abort(:not_found) if session not found — rolls back the transaction
    # TODO: implement
  end

  @spec invalidate(String.t()) :: :ok
  def invalidate(session_id) do
    {:atomic, :ok} = :mnesia.transaction(fn ->
      :mnesia.delete({@table, session_id})
    end)
    :ok
  end

  @spec invalidate_all_for_user(pos_integer()) :: {:ok, non_neg_integer()}
  def invalidate_all_for_user(user_id) do
    # HINT: :mnesia.transaction, index_read, Enum.each delete, return count
    # TODO: implement
  end

  @spec cleanup_expired() :: {:cleaned, non_neg_integer()}
  def cleanup_expired do
    now = :erlang.system_time(:second)
    ms = [
      {
        {@table, :"$1", :_, :_, :"$2", :_},
        [{:<, :"$2", now}],
        [:"$1"]
      }
    ]

    expired_ids = :mnesia.dirty_select(@table, ms)
    Enum.each(expired_ids, fn id -> :mnesia.dirty_delete({@table, id}) end)
    {:cleaned, length(expired_ids)}
  end
end
```

### Step 2: `lib/api_gateway/auth/account_system.ex`

```elixir
defmodule ApiGateway.Auth.AccountSystem do
  @moduledoc """
  Credit account system demonstrating multi-table atomic Mnesia transactions.

  The canonical use case for transactions: debit + credit must be atomic.
  Uses write locks in canonical order to prevent deadlocks.
  """

  defmodule Account do
    defstruct [:id, :user_id, :balance, :currency]
  end

  defmodule Transfer do
    defstruct [:id, :from_account, :to_account, :amount, :timestamp, :status]
  end

  @account_table Account
  @transfer_table Transfer

  @spec setup() :: :ok
  def setup do
    nodes = [node()]
    :mnesia.create_schema(nodes)
    :mnesia.start()

    for {table, attrs} <- [
      {Account, [:id, :user_id, :balance, :currency]},
      {Transfer, [:id, :from_account, :to_account, :amount, :timestamp, :status]}
    ] do
      case :mnesia.create_table(table, [
        attributes: attrs,
        ram_copies: nodes,
        type: :set
      ]) do
        {:atomic, :ok} -> :ok
        {:aborted, {:already_exists, _}} -> :ok
      end
    end

    :mnesia.wait_for_tables([@account_table, @transfer_table], 10_000)
  end

  @spec create_account(pos_integer(), number(), String.t()) :: {:ok, Account.t()}
  def create_account(user_id, initial_balance, currency \\ "USD") do
    account = %Account{
      id: :erlang.unique_integer([:monotonic, :positive]),
      user_id: user_id,
      balance: initial_balance,
      currency: currency
    }

    {:atomic, :ok} = :mnesia.transaction(fn -> :mnesia.write(account) end)
    {:ok, account}
  end

  @spec get_balance(pos_integer()) :: {:ok, number(), String.t()} | {:error, :not_found}
  def get_balance(account_id) do
    case :mnesia.dirty_read(@account_table, account_id) do
      [account] -> {:ok, account.balance, account.currency}
      [] -> {:error, :not_found}
    end
  end

  @spec transfer(pos_integer(), pos_integer(), number()) ::
          {:ok, Transfer.t()} | {:error, term()}
  def transfer(from_id, to_id, amount) when amount > 0 do
    {:atomic, result} =
      :mnesia.transaction(fn ->
        # Acquire write locks in canonical order (lower ID first) to prevent deadlocks.
        # If process A locks account 1 then 2, and process B locks 2 then 1,
        # they deadlock. Canonical order eliminates that possibility.
        {first_key, second_key} =
          if from_id < to_id,
            do: {{@account_table, from_id}, {@account_table, to_id}},
            else: {{@account_table, to_id}, {@account_table, from_id}}

        from_account =
          case :mnesia.read(first_key, :write) do
            [acc] -> acc
            [] -> :mnesia.abort({:not_found, :from_account, from_id})
          end

        to_account =
          case :mnesia.read(second_key, :write) do
            [acc] -> acc
            [] -> :mnesia.abort({:not_found, :to_account, to_id})
          end

        if from_account.currency != to_account.currency do
          :mnesia.abort({:currency_mismatch, from_account.currency, to_account.currency})
        end

        if from_account.balance < amount do
          :mnesia.abort({:insufficient_funds, from_account.balance, amount})
        end

        :mnesia.write(%{from_account | balance: from_account.balance - amount})
        :mnesia.write(%{to_account | balance: to_account.balance + amount})

        transfer = %Transfer{
          id: :erlang.unique_integer([:monotonic, :positive]),
          from_account: from_id,
          to_account: to_id,
          amount: amount,
          timestamp: :erlang.system_time(:second),
          status: :completed
        }

        :mnesia.write(transfer)
        {:ok, transfer}
      end)

    result
  end

  @spec transfer_history(pos_integer()) :: [Transfer.t()]
  def transfer_history(account_id) do
    from = :mnesia.dirty_index_read(@transfer_table, account_id, :from_account)
    to = :mnesia.dirty_index_read(@transfer_table, account_id, :to_account)
    (from ++ to) |> Enum.sort_by(& &1.timestamp, :desc)
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/auth/session_store_test.exs
defmodule ApiGateway.Auth.SessionStoreTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Auth.SessionStore

  setup_all do
    SessionStore.setup()
    :ok
  end

  setup do
    SessionStore.cleanup_expired()
    :ok
  end

  describe "create/2 and get_by_token/1" do
    test "creates a session and retrieves it by token" do
      {:ok, session} = SessionStore.create(1, %{device: "mobile"})
      assert {:ok, fetched} = SessionStore.get_by_token(session.token)
      assert fetched.id == session.id
      assert fetched.user_id == 1
    end

    test "returns {:error, :not_found} for unknown token" do
      assert {:error, :not_found} = SessionStore.get_by_token("nonexistent_token")
    end
  end

  describe "renew/1" do
    test "extends the expiry and returns the updated session" do
      {:ok, session} = SessionStore.create(10)
      original_expiry = session.expires_at
      Process.sleep(10)

      {:ok, renewed} = SessionStore.renew(session.id)
      assert renewed.expires_at > original_expiry
    end
  end

  describe "invalidate_all_for_user/1" do
    test "removes all sessions for a user" do
      {:ok, _s1} = SessionStore.create(99)
      {:ok, _s2} = SessionStore.create(99)

      {:ok, count} = SessionStore.invalidate_all_for_user(99)
      assert count >= 2

      assert [] = SessionStore.get_active_for_user(99)
    end
  end
end
```

```elixir
# test/api_gateway/auth/account_system_test.exs
defmodule ApiGateway.Auth.AccountSystemTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Auth.AccountSystem

  setup_all do
    AccountSystem.setup()
    :ok
  end

  describe "transfer/3" do
    test "transfers funds atomically" do
      {:ok, alice} = AccountSystem.create_account(1, 1000, "USD")
      {:ok, bob} = AccountSystem.create_account(2, 500, "USD")

      {:ok, _txn} = AccountSystem.transfer(alice.id, bob.id, 200)

      assert {:ok, 800, "USD"} = AccountSystem.get_balance(alice.id)
      assert {:ok, 700, "USD"} = AccountSystem.get_balance(bob.id)
    end

    test "returns error and leaves balances unchanged on insufficient funds" do
      {:ok, alice} = AccountSystem.create_account(10, 100, "USD")
      {:ok, bob} = AccountSystem.create_account(11, 100, "USD")

      result = AccountSystem.transfer(alice.id, bob.id, 500)
      assert {:error, {:insufficient_funds, 100, 500}} = result

      # Balances must be unchanged — rollback verified
      assert {:ok, 100, "USD"} = AccountSystem.get_balance(alice.id)
      assert {:ok, 100, "USD"} = AccountSystem.get_balance(bob.id)
    end

    test "returns error on currency mismatch" do
      {:ok, usd_acc} = AccountSystem.create_account(20, 500, "USD")
      {:ok, eur_acc} = AccountSystem.create_account(21, 500, "EUR")

      result = AccountSystem.transfer(usd_acc.id, eur_acc.id, 100)
      assert {:error, {:currency_mismatch, "USD", "EUR"}} = result
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/auth/ --trace
```

---

## Trade-off analysis

| Aspect | `ram_copies` | `disc_copies` | `disc_only_copies` |
|--------|-------------|--------------|-------------------|
| Read latency | Nanoseconds (RAM) | Nanoseconds (RAM cache) | Microseconds (disk I/O) |
| Write latency | Nanoseconds | Microseconds (WAL write) | Milliseconds |
| Durability | Lost on all-nodes-crash | Survives any single-node crash | Always durable |
| RAM usage | Full table in RAM | Full table in RAM | Index only |
| Use case | Ephemeral sessions, caches | Persistent sessions, config | Large tables, infrequent reads |

| Operation | Transactional | Dirty |
|-----------|--------------|-------|
| Read-modify-write | Required | Race condition |
| Idempotent read | Overkill | Appropriate |
| Batch delete of expired entries | Can use dirty | Appropriate |
| Balance transfer | Required | Data loss |

Reflection: `transfer/3` acquires write locks in `min(from_id, to_id)` order.
What deadlock scenario does this prevent? Draw the scenario with two concurrent transfers.

---

## Common production mistakes

**1. Not calling `wait_for_tables` before making queries**
Mnesia loads tables asynchronously after `start/0`. The first query on a not-yet-loaded
table returns `{:aborted, {no_exists, TableName}}`. Always call `wait_for_tables` with
a reasonable timeout (10s) in your application startup sequence.

**2. Using dirty ops for read-modify-write**
```elixir
# WRONG — race condition when two processes run concurrently
[account] = :mnesia.dirty_read(Account, id)
:mnesia.dirty_write(%{account | balance: account.balance - amount})

# CORRECT — write lock prevents concurrent modification
:mnesia.transaction(fn ->
  [account] = :mnesia.read({Account, id}, :write)
  :mnesia.write(%{account | balance: account.balance - amount})
end)
```

**3. Calling `create_schema` on multiple nodes independently**
If two nodes call `create_schema([node()])` independently, they create incompatible
schemas. Only the primary node calls `create_schema` with the full node list.
Secondary nodes call `start/0` and `change_config(:extra_db_nodes, [primary])`.

**4. Acquiring locks in inconsistent order**
If process A locks account 1 then 2, and process B locks account 2 then 1,
they deadlock. Mnesia detects this and aborts one transaction, which then retries.
The retry storm can degrade performance significantly. Establish a canonical lock
order and document it in the module.

**5. Using `disc_copies` for tables that are purely ephemeral**
`disc_copies` writes to disk on every committed transaction. For rate limiter counters
or ephemeral session tokens that don't need to survive a cluster-wide crash,
`ram_copies` is significantly faster and avoids unnecessary I/O.

---

## Resources

- [Erlang Mnesia user guide](https://www.erlang.org/doc/apps/mnesia/mnesia_chap1.html) — official guide with replication examples
- [Mnesia reference manual](https://www.erlang.org/doc/man/mnesia.html) — full API reference
- [Learn You Some Erlang — Mnesia chapter](https://learnyousomeerlang.com/mnesia) — clear tutorial with transaction examples
- [Elixir in Action 2nd ed. — Saša Jurić](https://www.manning.com/books/elixir-in-action-second-edition) — persistence patterns chapter
