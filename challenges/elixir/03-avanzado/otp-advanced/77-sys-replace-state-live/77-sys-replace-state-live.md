# Patching Live GenServer State with `:sys.replace_state/2`

**Project**: `sys_replace_state` — surgical state rewrites on running production processes.

---

## Project context

You operate a payments platform where a single GenServer — `PaymentsLedger` — tracks
the outstanding balance per merchant in memory. The ledger is fed by a stream of
events from Kafka and is the source of truth that the risk engine polls every 200 ms.
Restarting it is expensive: the supervisor would replay ~2 million events from the
journal, during which the risk engine has to fall back to the slower SQL view.

Last Sunday at 04:17 UTC a bad release corrupted the map under a single merchant id
(`"merchant_9f2b"`). The value became `{:error, :decode}` instead of a `Decimal.t()`.
Every call for that merchant crashed `{:badarg, Decimal.add/2}`. The platform SRE
has two options: (a) restart the ledger and pay the 8-minute replay, or (b) open
an IEx remote shell and surgically replace the offending key using
`:sys.replace_state/2`. Option (b) took 30 seconds.

This exercise teaches you option (b): how the `:sys` module speaks the OTP system
protocol to mutate the state of a live `GenServer`, when it is safe, and — more
importantly — when it is **not**. `:sys.replace_state/2` is a scalpel. Misused, it
leaves the process in a shape its own callbacks cannot handle and you end up with
a `FunctionClauseError` inside `handle_call/3` that is *permanent* until a restart.

Project layout:

```
sys_replace_state/
├── lib/
│   └── sys_replace_state/
│       ├── application.ex
│       ├── ledger.ex              # GenServer under surgery
│       └── rescue.ex              # operator-facing helpers
├── test/
│   └── sys_replace_state/
│       ├── ledger_test.exs
│       └── rescue_test.exs
└── mix.exs
```

---

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

**OTP-specific insight:**
The OTP framework enforces a discipline: supervision trees, callback modules, and standard return values. This structure is not a constraint — it's the contract that allows Erlang's release handler, hot code upgrades, and clustering to work. Every deviation from the pattern you'll pay for later in production debuggability and operational tooling.
### 1. The OTP system protocol

Every `gen_server`, `gen_statem`, and `gen_event` process speaks a second protocol
behind the user-facing API: the **system messages**. When you call `:sys.get_state/1`,
the caller does not run your `handle_call/3` — it sends a `{:system, From, Request}`
message that `gen_server.erl` intercepts before dispatch. This is how debugging is
possible without the server having to opt in.

```
        ┌──────────────────────────────────┐
caller ─┤ :sys.replace_state(pid, fun)     │
        └───────────────┬──────────────────┘
                        │ {:system, From, {:replace_state, fun}}
                        ▼
           ┌──────────────────────────┐
           │ gen_server receive loop  │
           │ (gen.erl / gen_server)   │
           ├──────────────────────────┤
           │ if msg = {:system, ...}  │
           │   → :sys.handle_system_msg │
           │   → StateFun.(State)     │
           │   → loop with NewState   │
           │ else dispatch to user    │
           │   callback               │
           └──────────────────────────┘
```

The key consequence: `replace_state` runs the mutator **in the GenServer's own
scheduler slice**, synchronously between two user messages. There is no race — your
callbacks will never see a half-mutated state — but the mutator is executing *inside*
your process, and a crash there will crash the server.

### 2. `get_state` vs `replace_state` vs `cast`

| API                          | Speaks OTP sys protocol | Runs in server | Bypasses callbacks |
|------------------------------|-------------------------|----------------|--------------------|
| `GenServer.call/2`           | no                      | yes            | no (goes to `handle_call`) |
| `GenServer.cast/2`           | no                      | yes            | no (goes to `handle_cast`) |
| `:sys.get_state/1`           | yes                     | yes            | yes |
| `:sys.replace_state/2`       | yes                     | yes            | yes |
| `:sys.get_status/1`          | yes                     | yes            | yes |

The third column is why `:sys.replace_state` exists: it is the only way to *write*
state while ignoring `handle_call`/`handle_cast`. If the bug lives inside a callback,
using the public API would re-trigger the bug and fail again.

### 3. What `:sys.replace_state/2` actually does

The Erlang source (`lib/stdlib/src/sys.erl`) is short enough to read:

```erlang
replace_state(Name, StateFun) ->
    replace_state(Name, StateFun, 5000).

replace_state(Name, StateFun, Timeout) ->
    send_system_msg(Name, {replace_state, StateFun}, Timeout).
```

Inside `gen_server.erl`, the handler is approximately:

```erlang
system_replace_state(StateFun, [Name, State, Mod, Time, HibernateAfterTimeout]) ->
    NState = try StateFun(State) catch _:_ -> State end,
    {ok, NState, [Name, NState, Mod, Time, HibernateAfterTimeout]}.
```

Read that `catch _:_` carefully. If your mutator raises, the *old* state is kept
silently, but the caller still gets back the would-be new value from `StateFun`.
This is the #1 production footgun — you think you rewrote the state and you didn't.

### 4. Safe-by-construction rescue helpers

The way mature codebases ship with `:sys.replace_state` is: **never** call it from
anywhere but an operator-authored rescue module. That module validates the
replacement against an invariant *before* installing it, and logs a structured
audit entry with the diff, the caller, and the reason. You do not grant juniors
shell access and ask them to type the mutator fresh.

### 5. When it is safe

A mental check list before running `:sys.replace_state/2` in production:

1. Do I have a reproduction in a staging node? (usually no — production only bug)
2. Is there a journal/event log I can replay later to verify I did not lose data?
3. Is the mutator **pure**, side-effect-free, and under 10 lines?
4. Does the new state satisfy every invariant my callbacks assume? (Types, shape,
   arity, protocol implementations.)
5. Am I logging the before/after so the incident review has evidence?

If any answer is "no", **restart the process** instead. A restart is boring,
auditable, and correct.

---

## Design decisions

**Option A — restart the GenServer and replay from the event log**
- Pros: deterministic; exercises the same path as a real crash; no callback invariants bypassed.
- Cons: replay can take minutes for large ledgers; during replay the system is unavailable.

**Option B — patch live state via `:sys.replace_state/2` with a validated rescue helper** (chosen)
- Pros: sub-millisecond fix; avoids unavailability window; survives read-only replay sources.
- Cons: bypasses `handle_call` invariants; mutator errors are silent; demands an audit log.

→ Chose **B** for incident-response scenarios where unavailability cost dominates correctness purity, wrapped in a rescue helper that re-reads state after the mutation to catch silent failures.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Bootstrap project with OTP app config so Ledger GenServer starts under supervision.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule SysReplaceState.MixProject do
  use Mix.Project

  def project do
    [
      app: :sys_replace_state,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {SysReplaceState.Application, []}
    ]
  end

  defp deps, do: [{:decimal, "~> 2.1"}]
end
```

### Step 2: `lib/sys_replace_state/application.ex`

**Objective**: Wire supervision tree so Ledger GenServer binds lifecycle to OTP restart policies.

```elixir
defmodule SysReplaceState.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [SysReplaceState.Ledger]
    Supervisor.start_link(children, strategy: :one_for_one, name: SysReplaceState.Supervisor)
  end
end
```

### Step 3: `lib/sys_replace_state/ledger.ex`

**Objective**: Implement in-memory ledger with public API so :sys.replace_state/2 bypasses handle_call invariants.

```elixir
defmodule SysReplaceState.Ledger do
  @moduledoc """
  In-memory ledger mapping `merchant_id` to a `Decimal.t()` balance.

  The ledger is deliberately minimal: we only want the public API to
  demonstrate how `:sys.replace_state/2` bypasses `handle_call/3`.
  """

  use GenServer
  require Logger

  @type merchant_id :: String.t()
  @type state :: %{merchant_id() => Decimal.t()}

  # Public API ---------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, Keyword.put_new(opts, :name, __MODULE__))
  end

  @spec credit(merchant_id(), Decimal.t()) :: :ok
  def credit(merchant_id, amount) do
    GenServer.call(__MODULE__, {:credit, merchant_id, amount})
  end

  @spec balance(merchant_id()) :: Decimal.t()
  def balance(merchant_id) do
    GenServer.call(__MODULE__, {:balance, merchant_id})
  end

  # GenServer ----------------------------------------------------------------

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call({:credit, merchant_id, amount}, _from, state) do
    current = Map.get(state, merchant_id, Decimal.new(0))
    new_balance = Decimal.add(current, amount)
    {:reply, :ok, Map.put(state, merchant_id, new_balance)}
  end

  def handle_call({:balance, merchant_id}, _from, state) do
    {:reply, Map.get(state, merchant_id, Decimal.new(0)), state}
  end
end
```

Notice that if `state[merchant_id]` is not a `Decimal.t()`, `Decimal.add/2`
crashes and the process restarts. That is exactly the production bug we are
simulating.

### Step 4: `lib/sys_replace_state/rescue.ex`

**Objective**: Wrap :sys.replace_state/2 with invariant checks so operators can safely patch live state in production.

```elixir
defmodule SysReplaceState.Rescue do
  @moduledoc """
  Operator-facing helpers that wrap `:sys.replace_state/2` with invariant checks,
  structured logging, and a before/after diff.

  This module is the **only** blessed entry point for state surgery in production.
  """

  require Logger

  @type diff :: %{
          merchant_id: String.t(),
          before: term(),
          after: term(),
          reason: String.t()
        }

  @doc """
  Replaces the balance for `merchant_id` with `new_balance` on the given GenServer.

  The replacement is aborted if:

    * the current value is already a well-formed `Decimal.t()` (refuse to clobber good data);
    * `new_balance` is not a `Decimal.t()` (refuse to install garbage).

  Returns `{:ok, diff()}` on success, `{:error, reason}` otherwise.
  """
  @spec patch_balance(GenServer.server(), String.t(), Decimal.t(), String.t()) ::
          {:ok, diff()} | {:error, atom()}
  def patch_balance(server, merchant_id, new_balance, reason)
      when is_binary(merchant_id) and is_binary(reason) do
    with :ok <- validate_incoming(new_balance),
         {:ok, before} <- fetch_current(server, merchant_id),
         :ok <- validate_corrupt(before) do
      mutator = fn state -> Map.put(state, merchant_id, new_balance) end

      # :sys.replace_state swallows exceptions silently; we assert post-state.
      _ = :sys.replace_state(server, mutator)
      %{^merchant_id => installed} = :sys.get_state(server)

      if installed == new_balance do
        diff = %{merchant_id: merchant_id, before: before, after: installed, reason: reason}
        Logger.warning("sys_replace_state applied", diff)
        {:ok, diff}
      else
        {:error, :mutator_failed}
      end
    end
  end

  defp validate_incoming(%Decimal{} = _), do: :ok
  defp validate_incoming(_), do: {:error, :new_balance_not_decimal}

  defp fetch_current(server, merchant_id) do
    state = :sys.get_state(server)
    {:ok, Map.get(state, merchant_id)}
  end

  defp validate_corrupt(%Decimal{}), do: {:error, :current_is_healthy}
  defp validate_corrupt(_), do: :ok
end
```

### Step 5: `test/sys_replace_state/ledger_test.exs`

**Objective**: Test credit/balance operations and verify state mutation under supervised lifecycle.

```elixir
defmodule SysReplaceState.LedgerTest do
  use ExUnit.Case, async: false

  alias SysReplaceState.Ledger

  setup do
    pid = start_supervised!({Ledger, name: :ledger_test})
    %{pid: pid}
  end

  describe "SysReplaceState.Ledger" do
    test "credit adds to running balance", %{pid: pid} do
      :ok = GenServer.call(pid, {:credit, "m1", Decimal.new("10")})
      :ok = GenServer.call(pid, {:credit, "m1", Decimal.new("2.5")})
      assert Decimal.equal?(GenServer.call(pid, {:balance, "m1"}), Decimal.new("12.5"))
    end

    test "corrupt value crashes the server on credit", %{pid: pid} do
      _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => {:error, :decode}} end)
      Process.flag(:trap_exit, true)
      Process.monitor(pid)

      # handle_call will raise when Decimal.add gets a tuple.
      catch_exit(GenServer.call(pid, {:credit, "m_bad", Decimal.new(1)}))
      assert_receive {:DOWN, _ref, :process, ^pid, _reason}, 500
    end
  end
end
```

### Step 6: `test/sys_replace_state/rescue_test.exs`

**Objective**: Test Rescue patch_balance guards and verify server remains live after state mutation.

```elixir
defmodule SysReplaceState.RescueTest do
  use ExUnit.Case, async: false

  alias SysReplaceState.{Ledger, Rescue}

  setup do
    pid = start_supervised!({Ledger, name: :rescue_test})
    %{pid: pid}
  end

  describe "SysReplaceState.Rescue" do
    test "patches a corrupt balance and logs a diff", %{pid: pid} do
      _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => {:error, :decode}} end)

      assert {:ok, diff} =
               Rescue.patch_balance(pid, "m_bad", Decimal.new("42.0"), "INC-2031 decode bug")

      assert diff.before == {:error, :decode}
      assert Decimal.equal?(diff.after, Decimal.new("42.0"))

      # The server keeps serving after the rescue; no restart occurred.
      assert Decimal.equal?(GenServer.call(pid, {:balance, "m_bad"}), Decimal.new("42.0"))
    end

    test "refuses to overwrite a healthy balance", %{pid: pid} do
      :ok = GenServer.call(pid, {:credit, "m_ok", Decimal.new("10")})
      assert {:error, :current_is_healthy} =
               Rescue.patch_balance(pid, "m_ok", Decimal.new("99"), "manual override")
    end

    test "refuses garbage replacements", %{pid: pid} do
      _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => :broken} end)
      assert {:error, :new_balance_not_decimal} =
               Rescue.patch_balance(pid, "m_bad", "not a decimal", "typo")
    end
  end
end
```

### Why this works

`:sys.replace_state/2` runs the mutator inside the target process, serialised against the mailbox, so no `handle_call` can observe a half-patched state. The rescue wrapper adds two things the raw primitive lacks: type-checked transforms and post-mutation re-reads that surface silent mutator crashes. Together these make the operation safe to run during an incident without coupling it to replay infrastructure.

---

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Silent mutator failure.** `:sys.replace_state/2` catches exceptions in the
mutator and returns the function result **without** telling you it failed. Always
follow the call with `:sys.get_state/1` and assert the change is installed, as
`Rescue.patch_balance/4` does.

**2. No callback involvement.** Whatever invariants `handle_call/3` maintains —
derived counters, Telemetry events, downstream notifications — are skipped. If
the ledger also tracked a `:total_volume` mirror, a surgical credit that only
touched the merchant map would leave the mirror stale.

**3. Concurrent-message reordering.** Messages queued *after* your
`replace_state` see the new state. Messages already dispatched to the callback
see the old one. In practice this is not a race (they are serial) but is a
source of "I saw the old value one millisecond later" confusion post-incident.

**4. Version skew after hot upgrades.** During a `code_change/3`, the state
shape can evolve. A mutator hard-coded for v1 state shape, applied to a node
already upgraded to v2, corrupts the server silently.

**5. Shell-typed closures.** `:sys.replace_state(pid, fn state -> ... end)` —
with the closure typed at the remote IEx prompt — compiles on the operator node
and is sent as a function reference. If the target node runs a different
Elixir/OTP minor and the closure references an internal module that moved, you
get `:undef` inside the target and your mutator is silently swallowed (see #1).

**6. Log everything.** A `:sys.replace_state` call with no `Logger.warning/2`
is a ghost. Six months later, the merchant sees a reconciliation mismatch and
nobody remembers that manual patch. Audit log is non-negotiable.

**7. When NOT to use this.** If the GenServer is under a supervisor with a
`:transient` or `:permanent` restart, *and* you have an event replay mechanism,
**restart instead**. `:sys.replace_state` is for cases where replay is too slow,
lossy, or impossible (ephemeral connection state, cached derivations). It is
never the first choice.

**8. Distributed surgery.** In a multi-node cluster with state that is replicated
(e.g. `Horde.Registry`, CRDTs), `:sys.replace_state` on one node does not
propagate. You will diverge replicas. Use the library's public merge API, not
the scalpel.

---

## Benchmark

`:sys.replace_state/2` costs roughly the same as a `GenServer.call/2` round-trip
plus the mutator runtime. Because it runs inside the server, a heavy mutator
blocks every other message in the mailbox for its duration. Rule of thumb:
**keep mutators sub-millisecond**. If your replacement is a full `Map` rebuild
over a 10k-entry state, measure it first with `:timer.tc/1` on staging. A
100 ms mutator on a GenServer receiving 1000 msg/s will build a 100-message
backlog behind it.

```elixir
{us, _} = :timer.tc(fn -> :sys.replace_state(pid, fun) end)
IO.puts("replace_state took #{us} µs")
```

Target: mutator runtime ≤ 1 ms for point edits on maps up to ~10k entries.

---

## Reflection

1. Your rescue helper re-reads state after the patch to detect silent mutator failure. How would you extend it to also verify invariants across multiple related GenServers (e.g. ledger + mirror + cache) in one atomic operator action, and what is the cost of that atomicity?
2. A teammate argues every `:sys.replace_state` should be replaced by a dedicated `handle_call({:admin_patch, ...}, ...)`. Under which incident profiles is this advice correct, and under which does it make the system worse?

---

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      # No external dependencies — pure Elixir
    ]
  end

  defmodule SysReplaceState.MixProject do
    end
    use Mix.Project

    def project do
      [
        app: :sys_replace_state,
        version: "0.1.0",
        elixir: "~> 1.16",
        start_permanent: Mix.env() == :prod,
        deps: deps()
      ]
    end

    def application do
      [
        extra_applications: [:logger],
        mod: {SysReplaceState.Application, []}
      ]
    end

    defp deps, do: [{:decimal, "~> 2.1"}]
  end

  defmodule SysReplaceState.Application do
    @moduledoc false
    use Application

    @impl true
    def start(_type, _args) do
      children = [SysReplaceState.Ledger]
      Supervisor.start_link(children, strategy: :one_for_one, name: SysReplaceState.Supervisor)
    end
  end

  defmodule SysReplaceState.Ledger do
    end
    @moduledoc """
    In-memory ledger mapping `merchant_id` to a `Decimal.t()` balance.

    The ledger is deliberately minimal: we only want the public API to
    demonstrate how `:sys.replace_state/2` bypasses `handle_call/3`.
    """

    use GenServer
    require Logger

    @type merchant_id :: String.t()
    @type state :: %{merchant_id() => Decimal.t()}

    # Public API ---------------------------------------------------------------

    def start_link(opts \\ []) do
      GenServer.start_link(__MODULE__, %{}, Keyword.put_new(opts, :name, __MODULE__))
    end

    @spec credit(merchant_id(), Decimal.t()) :: :ok
    def credit(merchant_id, amount) do
      GenServer.call(__MODULE__, {:credit, merchant_id, amount})
    end

    @spec balance(merchant_id()) :: Decimal.t()
    def balance(merchant_id) do
      GenServer.call(__MODULE__, {:balance, merchant_id})
    end

    # GenServer ----------------------------------------------------------------

    @impl true
    def init(state), do: {:ok, state}

    @impl true
    def handle_call({:credit, merchant_id, amount}, _from, state) do
      current = Map.get(state, merchant_id, Decimal.new(0))
      new_balance = Decimal.add(current, amount)
      {:reply, :ok, Map.put(state, merchant_id, new_balance)}
    end

    def handle_call({:balance, merchant_id}, _from, state) do
      {:reply, Map.get(state, merchant_id, Decimal.new(0)), state}
    end
  end

  defmodule SysReplaceState.Rescue do
    @moduledoc """
    Operator-facing helpers that wrap `:sys.replace_state/2` with invariant checks,
    structured logging, and a before/after diff.

    This module is the **only** blessed entry point for state surgery in production.
    """

    require Logger

    @type diff :: %{
            merchant_id: String.t(),
            before: term(),
            after: term(),
            reason: String.t()
          }

    @doc """
    Replaces the balance for `merchant_id` with `new_balance` on the given GenServer.

    The replacement is aborted if:

      * the current value is already a well-formed `Decimal.t()` (refuse to clobber good data);
      * `new_balance` is not a `Decimal.t()` (refuse to install garbage).

    Returns `{:ok, diff()}` on success, `{:error, reason}` otherwise.
    """
    @spec patch_balance(GenServer.server(), String.t(), Decimal.t(), String.t()) ::
            {:ok, diff()} | {:error, atom()}
    def patch_balance(server, merchant_id, new_balance, reason)
        when is_binary(merchant_id) and is_binary(reason) do
      with :ok <- validate_incoming(new_balance),
           {:ok, before} <- fetch_current(server, merchant_id),
           :ok <- validate_corrupt(before) do
        mutator = fn state -> Map.put(state, merchant_id, new_balance) end

        # :sys.replace_state swallows exceptions silently; we assert post-state.
        _ = :sys.replace_state(server, mutator)
        %{^merchant_id => installed} = :sys.get_state(server)

        if installed == new_balance do
          diff = %{merchant_id: merchant_id, before: before, after: installed, reason: reason}
          Logger.warning("sys_replace_state applied", diff)
          {:ok, diff}
        else
          {:error, :mutator_failed}
        end
      end
    end

    defp validate_incoming(%Decimal{} = _), do: :ok
    defp validate_incoming(_), do: {:error, :new_balance_not_decimal}

    defp fetch_current(server, merchant_id) do
      state = :sys.get_state(server)
      {:ok, Map.get(state, merchant_id)}
    end

    defp validate_corrupt(%Decimal{}), do: {:error, :current_is_healthy}
    defp validate_corrupt(_), do: :ok
  end

  defmodule SysReplaceState.LedgerTest do
    use ExUnit.Case, async: false

    alias SysReplaceState.Ledger

    setup do
      pid = start_supervised!({Ledger, name: :ledger_test})
      %{pid: pid}
    end

    describe "SysReplaceState.Ledger" do
      test "credit adds to running balance", %{pid: pid} do
        :ok = GenServer.call(pid, {:credit, "m1", Decimal.new("10")})
        :ok = GenServer.call(pid, {:credit, "m1", Decimal.new("2.5")})
        assert Decimal.equal?(GenServer.call(pid, {:balance, "m1"}), Decimal.new("12.5"))
      end

      test "corrupt value crashes the server on credit", %{pid: pid} do
        _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => {:error, :decode}} end)
        Process.flag(:trap_exit, true)
        Process.monitor(pid)

        # handle_call will raise when Decimal.add gets a tuple.
        catch_exit(GenServer.call(pid, {:credit, "m_bad", Decimal.new(1)}))
        assert_receive {:DOWN, _ref, :process, ^pid, _reason}, 500
      end
    end
  end

  defmodule SysReplaceState.RescueTest do
    use ExUnit.Case, async: false

    alias SysReplaceState.{Ledger, Rescue}

    setup do
      pid = start_supervised!({Ledger, name: :rescue_test})
      %{pid: pid}
    end

    describe "SysReplaceState.Rescue" do
      test "patches a corrupt balance and logs a diff", %{pid: pid} do
        _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => {:error, :decode}} end)

        assert {:ok, diff} =
                 Rescue.patch_balance(pid, "m_bad", Decimal.new("42.0"), "INC-2031 decode bug")

        assert diff.before == {:error, :decode}
        assert Decimal.equal?(diff.after, Decimal.new("42.0"))

        # The server keeps serving after the rescue; no restart occurred.
        assert Decimal.equal?(GenServer.call(pid, {:balance, "m_bad"}), Decimal.new("42.0"))
      end

      test "refuses to overwrite a healthy balance", %{pid: pid} do
        :ok = GenServer.call(pid, {:credit, "m_ok", Decimal.new("10")})
        assert {:error, :current_is_healthy} =
                 Rescue.patch_balance(pid, "m_ok", Decimal.new("99"), "manual override")
      end

      test "refuses garbage replacements", %{pid: pid} do
        _ = :sys.replace_state(pid, fn _ -> %{"m_bad" => :broken} end)
        assert {:error, :new_balance_not_decimal} =
                 Rescue.patch_balance(pid, "m_bad", "not a decimal", "typo")
      end
    end
  end

  defmodule Main do
    def main do
        # Demonstrating 77-sys-replace-state-live
        :ok
    end
  end
end

Main.main()
```
