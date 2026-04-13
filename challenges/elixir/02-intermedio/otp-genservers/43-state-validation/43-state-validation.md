# State validation: invariants in `init/1` and every update

**Project**: `validated_state_gs` — a GenServer that enforces domain invariants on every state transition, fail-fast.

---

## Project context

A running GenServer is only as trustworthy as its state. Once state goes
bad — a negative balance on a "non-negative balance" account, a stale
timestamp, a broken invariant between two fields — every subsequent
callback operates on garbage. In Erlang folklore this is called *"the
server is lying"* and it is the worst kind of bug: no crash, no log, just
wrong answers.

The defense is explicit, cheap validation at exactly two boundaries:

1. **`init/1`**: reject bad initial state by returning `{:stop, reason}`.
   Never start a server in an invalid state. Fail fast — let the
   supervisor decide.
2. **Every callback that changes state**: validate the candidate new
   state before committing. If the candidate is invalid, reject the
   update and keep the old state.

This exercise builds a small bank-account GenServer with two invariants
(`balance >= 0`, `version monotonically increasing`) and validation at
both boundaries. The techniques generalize: the pattern is "compute
candidate state → validate → commit or reject".

Project structure:

```
validated_state_gs/
├── lib/
│   └── validated_state_gs.ex
├── test/
│   └── validated_state_gs_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For state validation, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. Invariants are properties, not checks

An invariant is a property that must hold *at every observable moment*:
"balance is never negative", "tree is always balanced", "set of keys is
a subset of known users". It is not a guard clause — it is a property
that you maintain by construction and verify at the boundaries.

### 2. Fail fast in `init/1`

Returning `{:stop, reason}` from `init/1` aborts `start_link` with
`{:error, reason}`. The supervisor sees the failure and follows its
restart strategy. Silently continuing with bad state is the anti-pattern
— your bug moves from "visible at boot" to "visible under production
load, three weeks later".

### 3. The candidate-state pattern

```
handle_call({:deposit, amount}, _from, state) do
  candidate = %{state | balance: state.balance + amount, version: state.version + 1}
  case validate(candidate) do
    :ok          -> {:reply, :ok, candidate}
    {:error, e}  -> {:reply, {:error, e}, state}   # state unchanged!
  end
end
```

Never mutate state before validation. If validation fails, the original
state is still correct — return an error, keep going.

### 4. A single `validate/1` for the struct

Put one `validate/1` function near the struct definition, and reuse it
in `init/1`, every state-changing callback, and any test that constructs
state directly. One source of truth for "what makes this struct valid".

---

## Design decisions

**Option A — trust callers, no invariant checks**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — validate state transitions in every callback (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because fail-fast on invalid transitions beats silent corruption that surfaces hours later.


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
mix new validated_state_gs
cd validated_state_gs
```

### Step 2: `lib/validated_state_gs.ex`

**Objective**: Implement `validated_state_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule ValidatedStateGs do
  @moduledoc """
  A bank-account-like GenServer that enforces two invariants:

    1. `balance >= 0`
    2. `version` strictly increases on every mutation

  Validation happens in `init/1` (reject bad initial state) and in every
  callback that produces a candidate new state (reject bad transitions).
  """

  use GenServer

  defmodule State do
    @moduledoc false
    @enforce_keys [:balance, :version]
    defstruct [:balance, :version]

    @type t :: %__MODULE__{balance: integer(), version: non_neg_integer()}

    @doc """
    Single source of truth for state validity. Returns `:ok` or `{:error, reason}`.
    Every boundary that constructs or mutates state must call this.
    """
    @spec validate(t()) :: :ok | {:error, atom()}
    def validate(%__MODULE__{balance: b}) when not is_integer(b),
      do: {:error, :balance_not_integer}

    def validate(%__MODULE__{balance: b}) when b < 0,
      do: {:error, :negative_balance}

    def validate(%__MODULE__{version: v}) when not is_integer(v) or v < 0,
      do: {:error, :invalid_version}

    def validate(%__MODULE__{}), do: :ok
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {initial_balance, opts} = Keyword.pop(opts, :initial_balance, 0)
    GenServer.start_link(__MODULE__, initial_balance, opts)
  end

  @spec deposit(GenServer.server(), pos_integer()) :: :ok | {:error, atom()}
  def deposit(server, amount), do: GenServer.call(server, {:deposit, amount})

  @spec withdraw(GenServer.server(), pos_integer()) :: :ok | {:error, atom()}
  def withdraw(server, amount), do: GenServer.call(server, {:withdraw, amount})

  @spec balance(GenServer.server()) :: integer()
  def balance(server), do: GenServer.call(server, :balance)

  @spec version(GenServer.server()) :: non_neg_integer()
  def version(server), do: GenServer.call(server, :version)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(initial_balance) do
    state = %State{balance: initial_balance, version: 0}

    case State.validate(state) do
      :ok -> {:ok, state}
      {:error, reason} -> {:stop, {:invalid_initial_state, reason}}
    end
  end

  @impl true
  def handle_call({:deposit, amount}, _from, %State{} = state)
      when is_integer(amount) and amount > 0 do
    candidate = %{state | balance: state.balance + amount, version: state.version + 1}
    commit_or_reject(candidate, state)
  end

  def handle_call({:deposit, _bad}, _from, state),
    do: {:reply, {:error, :invalid_amount}, state}

  def handle_call({:withdraw, amount}, _from, %State{} = state)
      when is_integer(amount) and amount > 0 do
    candidate = %{state | balance: state.balance - amount, version: state.version + 1}
    commit_or_reject(candidate, state)
  end

  def handle_call({:withdraw, _bad}, _from, state),
    do: {:reply, {:error, :invalid_amount}, state}

  def handle_call(:balance, _from, %State{balance: b} = state),
    do: {:reply, b, state}

  def handle_call(:version, _from, %State{version: v} = state),
    do: {:reply, v, state}

  # ── Helpers ─────────────────────────────────────────────────────────────

  # The heart of the pattern: validate the candidate, commit if ok, reject otherwise.
  defp commit_or_reject(candidate, old_state) do
    case State.validate(candidate) do
      :ok -> {:reply, :ok, candidate}
      {:error, reason} -> {:reply, {:error, reason}, old_state}
    end
  end
end
```

### Step 3: `test/validated_state_gs_test.exs`

**Objective**: Write `validated_state_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ValidatedStateGsTest do
  use ExUnit.Case, async: true

  describe "init/1 validation" do
    test "accepts a valid initial balance" do
      assert {:ok, pid} = ValidatedStateGs.start_link(initial_balance: 100)
      assert 100 = ValidatedStateGs.balance(pid)
    end

    test "rejects a negative initial balance at start time" do
      assert {:error, {:invalid_initial_state, :negative_balance}} =
               ValidatedStateGs.start_link(initial_balance: -1)
    end

    test "rejects a non-integer initial balance" do
      assert {:error, {:invalid_initial_state, :balance_not_integer}} =
               ValidatedStateGs.start_link(initial_balance: "nope")
    end
  end

  describe "deposit/2" do
    setup do
      {:ok, acc} = ValidatedStateGs.start_link(initial_balance: 50)
      %{acc: acc}
    end

    test "increases balance and bumps version", %{acc: acc} do
      assert :ok = ValidatedStateGs.deposit(acc, 10)
      assert 60 = ValidatedStateGs.balance(acc)
      assert 1 = ValidatedStateGs.version(acc)
    end

    test "rejects non-positive amounts without mutating state", %{acc: acc} do
      assert {:error, :invalid_amount} = ValidatedStateGs.deposit(acc, 0)
      assert {:error, :invalid_amount} = ValidatedStateGs.deposit(acc, -5)
      assert 50 = ValidatedStateGs.balance(acc)
      assert 0 = ValidatedStateGs.version(acc)
    end
  end

  describe "withdraw/2 and invariant protection" do
    setup do
      {:ok, acc} = ValidatedStateGs.start_link(initial_balance: 50)
      %{acc: acc}
    end

    test "allows withdrawals up to the current balance", %{acc: acc} do
      assert :ok = ValidatedStateGs.withdraw(acc, 30)
      assert 20 = ValidatedStateGs.balance(acc)
    end

    test "rejects a withdrawal that would violate the invariant", %{acc: acc} do
      assert {:error, :negative_balance} = ValidatedStateGs.withdraw(acc, 9999)
      # State must be UNCHANGED — the rejected transition is invisible.
      assert 50 = ValidatedStateGs.balance(acc)
      assert 0 = ValidatedStateGs.version(acc)
    end

    test "version is monotonic across many mutations", %{acc: acc} do
      for _ <- 1..10, do: ValidatedStateGs.deposit(acc, 1)
      assert 10 = ValidatedStateGs.version(acc)
      assert 60 = ValidatedStateGs.balance(acc)
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


## Deep Dive: State Invariants and Defensive Validation Boundaries

State validation is not just runtime safety—it's a design contract. A GenServer that validates preconditions before mutation catches bugs in the handler, not hours later when a corrupted state causes silent logic errors downstream. The pattern is twofold: validate the message (does it make sense?), then validate the state transition (does the new state preserve our invariants?).

Guards (`when is_integer(x)`) catch type mismatches at the function head, failing fast and clearly. Inline validation (`if state.count < 0`) catches semantic invariants. Both are needed: guards prevent type errors, validation prevents logical errors (e.g., account balance going negative). When validation fails, emit structured context—what was the bad state, what message triggered it—so debugging doesn't become guesswork.

A critical insight: validation failure should be *rare*. If you're validating the same invariant repeatedly across many handlers, it suggests the state machine design has an edge case. Instead of adding more validations, refactor to prevent the invalid state from being reachable in the first place. This means thinking about state transitions globally: what invariants must hold at every state? Encode those in the state struct's types (e.g., `@type account :: %{balance: non_neg_integer}`), and validation becomes defensive, not exploratory.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Validation is a boundary concern, not a per-field concern**
Putting `is_integer` guards on every function signature is shotgun
validation — hard to read, easy to miss a branch. One `validate/1` at
the state boundary captures the invariants declaratively and applies
them uniformly.

**2. Returning `{:error, reason}` vs. crashing is a policy choice**
For business violations (negative balance, wrong user), return an error
— it's a legitimate "no" answer. For programmer errors (bug in caller
passing garbage), let it crash and let the supervisor restart. The rule
of thumb: errors the caller can recover from are returns; bugs are crashes.

**3. `{:stop, reason}` in `init/1` is your friend**
A supervisor seeing repeated init failures trips its restart intensity
and escalates. This is the correct failure mode for "I cannot start
with this configuration" — loud, visible, supervised. Silently booting
with default state hides bugs.

**4. Validation cost on hot paths**
If your invariant check is O(n) over a large collection inside a
hot-path callback, you pay that cost on every update. For performance-
critical paths, validate only on mutations that could break the
invariant, and consider property-based tests to cover the rest.

**5. Don't log validation failures silently**
A rejected transition is usually something a human wants to see.
`Logger.warning/1` on validation errors helps production diagnose
"why does this account keep rejecting withdrawals?" without requiring
a debugger. Just don't log PII.

**6. When NOT to use this pattern**
For trivial state (a counter, a cache), the validate-every-transition
overhead is noise. Use it when your state has business meaning and
subtle invariants that would cause a production incident if violated.

---


## Reflection

- Si agregás validación en cada transición y el cost pasa de 1µs a 10µs por call, ¿vale? Definí en qué cargas sí y en cuáles no.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule ValidatedStateGs do
    @moduledoc """
    A bank-account-like GenServer that enforces two invariants:

      1. `balance >= 0`
      2. `version` strictly increases on every mutation

    Validation happens in `init/1` (reject bad initial state) and in every
    callback that produces a candidate new state (reject bad transitions).
    """

    use GenServer

    defmodule State do
      @moduledoc false
      @enforce_keys [:balance, :version]
      defstruct [:balance, :version]

      @type t :: %__MODULE__{balance: integer(), version: non_neg_integer()}

      @doc """
      Single source of truth for state validity. Returns `:ok` or `{:error, reason}`.
      Every boundary that constructs or mutates state must call this.
      """
      @spec validate(t()) :: :ok | {:error, atom()}
      def validate(%__MODULE__{balance: b}) when not is_integer(b),
        do: {:error, :balance_not_integer}

      def validate(%__MODULE__{balance: b}) when b < 0,
        do: {:error, :negative_balance}

      def validate(%__MODULE__{version: v}) when not is_integer(v) or v < 0,
        do: {:error, :invalid_version}

      def validate(%__MODULE__{}), do: :ok
    end

    # ── Public API ──────────────────────────────────────────────────────────

    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts) do
      {initial_balance, opts} = Keyword.pop(opts, :initial_balance, 0)
      GenServer.start_link(__MODULE__, initial_balance, opts)
    end

    @spec deposit(GenServer.server(), pos_integer()) :: :ok | {:error, atom()}
    def deposit(server, amount), do: GenServer.call(server, {:deposit, amount})

    @spec withdraw(GenServer.server(), pos_integer()) :: :ok | {:error, atom()}
    def withdraw(server, amount), do: GenServer.call(server, {:withdraw, amount})

    @spec balance(GenServer.server()) :: integer()
    def balance(server), do: GenServer.call(server, :balance)

    @spec version(GenServer.server()) :: non_neg_integer()
    def version(server), do: GenServer.call(server, :version)

    # ── Callbacks ───────────────────────────────────────────────────────────

    @impl true
    def init(initial_balance) do
      state = %State{balance: initial_balance, version: 0}

      case State.validate(state) do
        :ok -> {:ok, state}
        {:error, reason} -> {:stop, {:invalid_initial_state, reason}}
      end
    end

    @impl true
    def handle_call({:deposit, amount}, _from, %State{} = state)
        when is_integer(amount) and amount > 0 do
      candidate = %{state | balance: state.balance + amount, version: state.version + 1}
      commit_or_reject(candidate, state)
    end

    def handle_call({:deposit, _bad}, _from, state),
      do: {:reply, {:error, :invalid_amount}, state}

    def handle_call({:withdraw, amount}, _from, %State{} = state)
        when is_integer(amount) and amount > 0 do
      candidate = %{state | balance: state.balance - amount, version: state.version + 1}
      commit_or_reject(candidate, state)
    end

    def handle_call({:withdraw, _bad}, _from, state),
      do: {:reply, {:error, :invalid_amount}, state}

    def handle_call(:balance, _from, %State{balance: b} = state),
      do: {:reply, b, state}

    def handle_call(:version, _from, %State{version: v} = state),
      do: {:reply, v, state}

    # ── Helpers ─────────────────────────────────────────────────────────────

    # The heart of the pattern: validate the candidate, commit if ok, reject otherwise.
    defp commit_or_reject(candidate, old_state) do
      case State.validate(candidate) do
        :ok -> {:reply, :ok, candidate}
        {:error, reason} -> {:reply, {:error, reason}, old_state}
      end
    end
  end

  def main do
    {:ok, pid} = ValidatedStateGs.start_link(initial_balance: 100)
  
    b1 = ValidatedStateGs.balance(pid)
    IO.puts("Starting balance: #{b1}")
  
    :ok = ValidatedStateGs.deposit(pid, 50)
    b2 = ValidatedStateGs.balance(pid)
    IO.puts("After deposit: #{b2}")
  
    :ok = ValidatedStateGs.withdraw(pid, 30)
    b3 = ValidatedStateGs.balance(pid)
    v3 = ValidatedStateGs.version(pid)
    IO.puts("After withdraw: balance=#{b3}, version=#{v3}")
  
    IO.puts("✓ ValidatedStateGs works correctly")
  end

end

Main.main()
```


## Resources

- [`GenServer.init/1` — return values and `{:stop, reason}`](https://hexdocs.pm/elixir/GenServer.html#c:init/1)
- [Joe Armstrong's "Let it crash" essay](https://erlang.org/download/armstrong_thesis_2003.pdf)
- [Saša Jurić, *Elixir in Action* — ch. 6 on stateful servers](https://www.manning.com/books/elixir-in-action-second-edition)
- [Hillel Wayne, "Invariants"](https://hillelwayne.com/post/invariants/) — language-agnostic take on why they matter
