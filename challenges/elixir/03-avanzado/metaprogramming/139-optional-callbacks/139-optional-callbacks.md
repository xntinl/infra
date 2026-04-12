# Optional Callbacks and Runtime `ensure_loaded?`

**Project**: `optional_callbacks` — design a plugin system where most hooks are optional, guard every call with `Code.ensure_loaded?/1` + `function_exported?/3`, and understand why the combination matters in releases.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

You run a payment gateway that supports 15 PSPs (Payment Service Providers). Each
`PSP` module implements `charge/2` (required) and may optionally implement:

- `capture/2` — two-phase capture
- `refund/3` — refund a charge
- `void/2` — cancel before capture
- `webhook_validate/2` — verify incoming webhooks

Forcing every PSP to implement all 5 bloats code and misrepresents capability. Optional
callbacks + runtime introspection let the orchestrator ask: *"does this PSP support
refund?"* before routing the request.

```
optional_callbacks/
├── lib/
│   └── optional_callbacks/
│       ├── psp.ex                # behaviour
│       ├── orchestrator.ex       # dispatches with runtime checks
│       └── psps/
│           ├── stripe.ex         # implements all 5
│           ├── wire_transfer.ex  # implements charge only
│           └── paypal.ex         # implements charge, refund, void
├── test/
│   └── optional_callbacks_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `@optional_callbacks`

Declares which callbacks are optional. The compiler then does NOT warn when an
implementing module leaves them out.

```
@callback charge(term, term) :: :ok | {:error, term}
@callback refund(term, term, term) :: :ok | {:error, term}
@optional_callbacks [refund: 3]
```

Without `@optional_callbacks`, you'd get `@behaviour` warning spam from every
impl missing a callback.

### 2. `function_exported?/3` vs `Code.ensure_loaded?/1`

- `function_exported?(mod, fun, arity)` checks the module's export table. **Requires
  the module to already be loaded.**
- `Code.ensure_loaded?(mod)` loads the module if it is not yet, returns boolean.

In `:embedded` release mode, modules are loaded lazily. `function_exported?` alone
returns `false` for unloaded modules. Combine them:

```
Code.ensure_loaded?(mod) and function_exported?(mod, :refund, 3)
```

### 3. Cache the capability map

Computing the capability on every call is wasteful. Compute once on app start,
store in ETS or `:persistent_term`:

```
%{
  Stripe => [:charge, :capture, :refund, :void, :webhook_validate],
  WireTransfer => [:charge],
  PayPal => [:charge, :refund, :void]
}
```

### 4. `@impl true` correctness

`@impl true` on a callback impl makes the compiler verify the callback exists in the
behaviour. Optional callbacks still work with `@impl true` — the warning fires only
if the listed callback does not exist in the behaviour at all.

### 5. `module_info(:exports)` vs `:attributes`

- `module_info(:exports)` → `[{:charge, 2}, {:refund, 3}, ...]`, the source of truth.
- `module_info(:attributes)` → custom attributes (including `:behaviour` list).
  Check `Keyword.get(attrs, :behaviour, [])` to detect whether a module claims
  to implement `PSP`.

---

## Implementation

### Step 1: `lib/optional_callbacks/psp.ex`

```elixir
defmodule OptionalCallbacks.PSP do
  @moduledoc "Behaviour for Payment Service Providers."

  @type charge_id :: String.t()
  @type amount :: non_neg_integer()
  @type reason :: term()

  @callback charge(amount(), map()) :: {:ok, charge_id()} | {:error, reason()}
  @callback capture(charge_id(), amount()) :: :ok | {:error, reason()}
  @callback refund(charge_id(), amount(), map()) :: :ok | {:error, reason()}
  @callback void(charge_id(), map()) :: :ok | {:error, reason()}
  @callback webhook_validate(binary(), map()) :: :ok | {:error, reason()}

  @optional_callbacks [capture: 2, refund: 3, void: 2, webhook_validate: 2]

  @optional_funs [capture: 2, refund: 3, void: 2, webhook_validate: 2]

  @doc false
  def optional_funs, do: @optional_funs
end
```

### Step 2: `lib/optional_callbacks/psps/stripe.ex`

```elixir
defmodule OptionalCallbacks.PSPs.Stripe do
  @behaviour OptionalCallbacks.PSP

  @impl true
  def charge(_amount, _opts), do: {:ok, "ch_stripe_1"}

  @impl true
  def capture(_id, _amount), do: :ok

  @impl true
  def refund(_id, _amount, _opts), do: :ok

  @impl true
  def void(_id, _opts), do: :ok

  @impl true
  def webhook_validate(_body, _headers), do: :ok
end
```

### Step 3: `lib/optional_callbacks/psps/wire_transfer.ex`

```elixir
defmodule OptionalCallbacks.PSPs.WireTransfer do
  @behaviour OptionalCallbacks.PSP

  @impl true
  def charge(amount, _opts) when amount > 0, do: {:ok, "wire_#{amount}"}
  def charge(_, _), do: {:error, :invalid_amount}

  # omits capture, refund, void, webhook_validate on purpose
end
```

### Step 4: `lib/optional_callbacks/psps/paypal.ex`

```elixir
defmodule OptionalCallbacks.PSPs.PayPal do
  @behaviour OptionalCallbacks.PSP

  @impl true
  def charge(_amount, _opts), do: {:ok, "pp_1"}

  @impl true
  def refund(_id, _amount, _opts), do: :ok

  @impl true
  def void(_id, _opts), do: :ok

  # no capture, no webhook_validate
end
```

### Step 5: `lib/optional_callbacks/orchestrator.ex`

```elixir
defmodule OptionalCallbacks.Orchestrator do
  @moduledoc "Routes operations to PSPs, skipping unsupported ones gracefully."

  alias OptionalCallbacks.PSP

  @spec supports?(module(), atom(), arity()) :: boolean()
  def supports?(mod, fun, arity) do
    Code.ensure_loaded?(mod) and function_exported?(mod, fun, arity)
  end

  @spec capabilities(module()) :: [{atom(), arity()}]
  def capabilities(mod) do
    _ = Code.ensure_loaded(mod)

    for {fun, arity} <- PSP.optional_funs(), supports?(mod, fun, arity) do
      {fun, arity}
    end
  end

  @spec charge(module(), non_neg_integer(), map()) ::
          {:ok, String.t()} | {:error, term()}
  def charge(mod, amount, opts), do: mod.charge(amount, opts)

  @spec refund(module(), String.t(), non_neg_integer(), map()) ::
          :ok | {:error, term()} | {:error, :not_supported}
  def refund(mod, id, amount, opts) do
    if supports?(mod, :refund, 3), do: mod.refund(id, amount, opts), else: {:error, :not_supported}
  end

  @spec void(module(), String.t(), map()) ::
          :ok | {:error, term()} | {:error, :not_supported}
  def void(mod, id, opts) do
    if supports?(mod, :void, 2), do: mod.void(id, opts), else: {:error, :not_supported}
  end

  @spec any_supporting(atom(), arity(), [module()]) :: [module()]
  def any_supporting(fun, arity, mods) do
    Enum.filter(mods, &supports?(&1, fun, arity))
  end
end
```

### Step 6: Tests

```elixir
defmodule OptionalCallbacksTest do
  use ExUnit.Case, async: true

  alias OptionalCallbacks.Orchestrator
  alias OptionalCallbacks.PSPs.{Stripe, WireTransfer, PayPal}

  describe "supports?/3" do
    test "Stripe supports everything" do
      assert Orchestrator.supports?(Stripe, :refund, 3)
      assert Orchestrator.supports?(Stripe, :capture, 2)
    end

    test "WireTransfer does NOT support refund" do
      refute Orchestrator.supports?(WireTransfer, :refund, 3)
    end

    test "PayPal supports refund but not capture" do
      assert Orchestrator.supports?(PayPal, :refund, 3)
      refute Orchestrator.supports?(PayPal, :capture, 2)
    end
  end

  describe "capabilities/1" do
    test "lists only implemented optional callbacks" do
      assert Orchestrator.capabilities(Stripe) ==
               [capture: 2, refund: 3, void: 2, webhook_validate: 2]

      assert Orchestrator.capabilities(WireTransfer) == []
      assert Orchestrator.capabilities(PayPal) == [refund: 3, void: 2]
    end
  end

  describe "orchestrator dispatch" do
    test "refund is :not_supported on WireTransfer" do
      assert {:error, :not_supported} = Orchestrator.refund(WireTransfer, "w_1", 100, %{})
    end

    test "refund works on PayPal" do
      assert :ok = Orchestrator.refund(PayPal, "pp_1", 50, %{})
    end

    test "any_supporting returns only capable PSPs" do
      psps = [Stripe, WireTransfer, PayPal]
      assert Orchestrator.any_supporting(:refund, 3, psps) == [Stripe, PayPal]
      assert Orchestrator.any_supporting(:capture, 2, psps) == [Stripe]
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `function_exported?/3` returns false for unloaded modules.** In release mode
(`:embedded`), modules are loaded lazily. Without `Code.ensure_loaded/1`, a brand
new worker process on a fresh node sees "no support" for everything. Combine both.

**2. Capability caching to avoid cost.** `Code.ensure_loaded` is cheap after the
first call, but not free. For hot paths, compute the capability map at application
start and store in `:persistent_term.put/2`.

**3. Avoid `rescue UndefinedFunctionError`.** It works but is 1000× slower than a
pre-check. Always guard explicitly.

**4. `@optional_callbacks` does NOT enforce "at least one".** A module can `@behaviour
PSP` and implement zero functions. If you require `charge/2`, do NOT list it in
`@optional_callbacks` and enable `warnings_as_errors: true`.

**5. Dialyzer has trouble with `function_exported?`.** The branch is dynamic; spec
the return to include both success and `{:error, :not_supported}`.

**6. Compile-time enumeration alternative.** Instead of runtime checks, you can
enumerate the behaviour's optional callbacks at compile time and generate
`supports_refund?/0` etc. for each impl. Trades flexibility for zero-cost calls.

**7. Behaviours vs protocols.** Behaviours dispatch by explicit module; protocols
dispatch by value type. For "PSP chosen by config", behaviour + optional callbacks
is the right tool. For "encode any value", use protocols.

**8. When NOT to use optional callbacks.** If 90% of impls will provide the "optional"
callback anyway, make it required and accept the minor compilation friction. If
25% of implementations leave it out, optional is correct.

---

## Resources

- [`Module` — `@optional_callbacks`](https://hexdocs.pm/elixir/Module.html#module-optional_callbacks)
- [`Code.ensure_loaded?/1`](https://hexdocs.pm/elixir/Code.html#ensure_loaded?/1)
- [`function_exported?/3`](https://hexdocs.pm/elixir/Kernel.html#function_exported?/3)
- [Phoenix.Endpoint optional init/2](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/endpoint.ex)
- [Plug — optional call/2 variants](https://github.com/elixir-plug/plug/blob/main/lib/plug.ex)
- [Erlang docs — `:embedded` mode](https://www.erlang.org/doc/man/erl.html)
- [Dashbit blog — behaviours in practice](https://dashbit.co/blog)
