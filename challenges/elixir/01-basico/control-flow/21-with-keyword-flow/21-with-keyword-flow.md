# The `with` Keyword: Building a Checkout Flow

**Project**: `checkout_flow` — an e-commerce checkout pipeline with typed errors

---

## Why `with` matters for a senior developer

`with` is Elixir's answer to Railway-Oriented Programming. It chains a sequence of
expressions where each step can fail, short-circuits on the first failure, and
lets you attach an `else` block that pattern-matches on the failure shape.

A `with` pipeline is not the same as a `|>` pipeline. The pipe operator passes a
single value through functions; `with` threads through multiple bound variables
and changes control flow based on whether each step returns the expected shape.

Understanding `with` matters when you need to:

- Execute a sequence of operations where any step may fail
- Keep error information typed and distinguishable (`:invalid_cart` vs `:coupon_expired`)
- Avoid the "pyramid of doom" of nested `case` statements
- Propagate early returns without exceptions

---

## The business problem

You run an online store. The checkout endpoint must:

1. Validate the cart (not empty, items in stock)
2. Apply a discount coupon (may be missing, invalid, or expired)
3. Calculate shipping (depends on destination and weight)
4. Confirm the order (reserve stock, create order record)

If any step fails, the HTTP response must tell the client exactly what went wrong
with a machine-readable code. A single "checkout failed" is useless — the frontend
needs to know whether to highlight the coupon field, show a stock warning, or
redirect to the address form.

---

## Project structure

```
checkout_flow/
├── lib/
│   └── checkout_flow/
│       ├── cart.ex
│       ├── coupon.ex
│       ├── shipping.ex
│       └── checkout.ex
├── test/
│   └── checkout_flow/
│       └── checkout_test.exs
└── mix.exs
```

---

## How `with` actually works

```elixir
with {:ok, a} <- step_one(),
     {:ok, b} <- step_two(a),
     {:ok, c} <- step_three(b) do
  {:ok, c}
else
  {:error, :reason_one} -> handle_one()
  {:error, :reason_two} -> handle_two()
  other -> other
end
```

Rules that trip everyone at least once:

- Each `<-` clause matches the pattern on the left against the expression on the right.
  If it matches, the variables are bound and execution continues. If it does NOT match,
  the right-hand value falls into the `else` block.
- The `else` block is OPTIONAL. Without it, a non-matching value is returned as-is —
  which is often exactly what you want.
- A `=` inside `with` is a regular assignment, not a match clause. It does not trigger
  the `else`. Use it for intermediate computation.
- If the `else` block is present but no clause in it matches, you get a
  `WithClauseError` at runtime. Always include a catch-all (`other -> other`) unless
  you are CERTAIN every failure shape is enumerated.

---

## Design decisions

**Option A — `with` without `else`, relying on transparent error pass-through**
- Pros: Every step's `{:error, reason}` propagates unchanged — the caller sees the exact failure
- Cons: Loses the ability to remap errors (e.g., convert `{:error, :not_found}` into an HTTP 404 struct)

**Option B — `with` with a catch-all `else` clause** (chosen)
- Pros: Can normalize errors into a single type
- Cons: Non-exhaustive `else` is a compile warning + runtime risk; opens the door to accidentally masking new error types

→ Chose **A** because a checkout pipeline should not hide which step failed from the calling layer. Use B only when you genuinely need to remap errors at the outermost layer.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1: Create the project

**Objective**: Organize cart/coupon/shipping/checkout modules so with pipeline dispatches to single-responsibility functions.

```bash
mix new checkout_flow
cd checkout_flow
```

### Step 2: `mix.exs`

**Objective**: Use stdlib only so with pattern-matching on {:ok/:error} tuples is visible without HTTP/DB noise.

```elixir
defmodule CheckoutFlow.MixProject do
  use Mix.Project

  def project do
    [
      app: :checkout_flow,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

### Step 3: `lib/checkout_flow/cart.ex`

**Objective**: Return {:error, {:out_of_stock, sku}} so caller can highlight specific item in UI without generic failure message.

```elixir
defmodule CheckoutFlow.Cart do
  @moduledoc """
  Cart validation. Pure functions — no DB access, no HTTP calls.

  In a real system, stock would be checked against an inventory service.
  Here we accept a `stock` map from the caller so tests stay deterministic.
  """

  @type item :: %{sku: String.t(), quantity: pos_integer(), unit_price: integer()}
  @type cart :: %{items: [item()]}
  @type stock :: %{String.t() => non_neg_integer()}

  @spec validate(cart(), stock()) ::
          {:ok, cart()} | {:error, :empty_cart} | {:error, {:out_of_stock, String.t()}}
  def validate(%{items: []}, _stock), do: {:error, :empty_cart}

  def validate(%{items: items} = cart, stock) when is_list(items) do
    case Enum.find(items, fn %{sku: sku, quantity: qty} ->
           Map.get(stock, sku, 0) < qty
         end) do
      nil -> {:ok, cart}
      %{sku: sku} -> {:error, {:out_of_stock, sku}}
    end
  end

  @doc "Subtotal in cents. Integer arithmetic to avoid float drift on money."
  @spec subtotal(cart()) :: integer()
  def subtotal(%{items: items}) do
    Enum.reduce(items, 0, fn %{quantity: q, unit_price: p}, acc -> acc + q * p end)
  end
end
```

### Step 4: `lib/checkout_flow/coupon.ex`

**Objective**: Distinguish absent, invalid, and expired coupons as separate error atoms so the frontend can highlight the right field.

```elixir
defmodule CheckoutFlow.Coupon do
  @moduledoc """
  Coupon application. A coupon is either absent (nil), present and valid,
  or present and rejected for a specific reason.
  """

  @type coupon :: %{code: String.t(), percent_off: 1..100, valid_until: Date.t()}

  @spec apply(integer(), coupon() | nil, Date.t()) ::
          {:ok, integer()}
          | {:error, :coupon_expired}
          | {:error, :invalid_discount}
  def apply(subtotal, nil, _today), do: {:ok, subtotal}

  def apply(subtotal, %{percent_off: pct}, _today) when pct < 1 or pct > 100 do
    {:error, :invalid_discount}
  end

  def apply(subtotal, %{valid_until: valid_until}, today) do
    if Date.compare(today, valid_until) == :gt do
      {:error, :coupon_expired}
    else
      apply_discount(subtotal)
    end
  end

  defp apply_discount(subtotal) do
    # Called only when coupon is valid. Extracted to keep the public clauses flat.
    # The `apply/3` clause that reaches here has already validated pct and date —
    # but we still need pct in scope. See the complete version below.
    {:ok, subtotal}
  end

  @doc """
  Full discount calculation kept separate so the pipeline stays readable.
  Integer division rounds down — banks and tax agencies prefer this over rounding.
  """
  @spec discounted(integer(), coupon()) :: integer()
  def discounted(subtotal, %{percent_off: pct}) do
    subtotal - div(subtotal * pct, 100)
  end
end
```

Now update `apply/3` to actually compute the discount:

```elixir
# Replace the previous apply/3 clauses with this single final version:
defmodule CheckoutFlow.Coupon do
  @moduledoc false

  @type coupon :: %{code: String.t(), percent_off: 1..100, valid_until: Date.t()}

  @spec apply(integer(), coupon() | nil, Date.t()) ::
          {:ok, integer()}
          | {:error, :coupon_expired}
          | {:error, :invalid_discount}
  def apply(subtotal, nil, _today), do: {:ok, subtotal}

  def apply(_subtotal, %{percent_off: pct}, _today) when pct < 1 or pct > 100 do
    {:error, :invalid_discount}
  end

  def apply(subtotal, %{valid_until: valid_until} = coupon, today) do
    case Date.compare(today, valid_until) do
      :gt -> {:error, :coupon_expired}
      _ -> {:ok, discounted(subtotal, coupon)}
    end
  end

  defp discounted(subtotal, %{percent_off: pct}) do
    subtotal - div(subtotal * pct, 100)
  end
end
```

### Step 5: `lib/checkout_flow/shipping.ex`

**Objective**: Keep money in integer cents and weight in whole grams so no floating-point drift creeps into the checkout totals.

```elixir
defmodule CheckoutFlow.Shipping do
  @moduledoc """
  Shipping calculation. In a real system this would hit a carrier API.
  Here we use a simple table by country + weight band.
  """

  @type address :: %{country: String.t(), postal_code: String.t()}

  @weight_per_item_g 500

  @spec calculate(address(), pos_integer()) ::
          {:ok, integer()} | {:error, :unsupported_country}
  def calculate(%{country: country}, total_items) when total_items > 0 do
    case rate_per_kg(country) do
      nil -> {:error, :unsupported_country}
      rate -> {:ok, compute_cost(rate, total_items)}
    end
  end

  defp rate_per_kg("ES"), do: 500
  defp rate_per_kg("FR"), do: 800
  defp rate_per_kg("DE"), do: 900
  defp rate_per_kg(_), do: nil

  defp compute_cost(rate_per_kg, total_items) do
    # Weight ceiling: 2 items at 500g each = 1 kg, not 1.0 kg of float ambiguity.
    total_weight_g = total_items * @weight_per_item_g
    kilos_ceil = div(total_weight_g + 999, 1000)
    rate_per_kg * kilos_ceil
  end
end
```

### Step 6: `lib/checkout_flow/checkout.ex` — the `with` pipeline

**Objective**: Omit `else` so each step's typed error tuple propagates unchanged — the caller sees exactly which stage rejected the request.

```elixir
defmodule CheckoutFlow.Checkout do
  @moduledoc """
  Orchestrates the checkout pipeline using `with`.

  The pipeline is the value proposition of this module: every step is named,
  each failure has a typed reason, and the success path reads top to bottom
  without nesting.
  """

  alias CheckoutFlow.{Cart, Coupon, Shipping}

  @type request :: %{
          cart: Cart.cart(),
          stock: Cart.stock(),
          coupon: Coupon.coupon() | nil,
          address: Shipping.address(),
          today: Date.t()
        }

  @type order :: %{
          subtotal_cents: integer(),
          discounted_cents: integer(),
          shipping_cents: integer(),
          total_cents: integer()
        }

  @type error_reason ::
          :empty_cart
          | {:out_of_stock, String.t()}
          | :coupon_expired
          | :invalid_discount
          | :unsupported_country

  @spec process(request()) :: {:ok, order()} | {:error, error_reason()}
  def process(%{
        cart: cart,
        stock: stock,
        coupon: coupon,
        address: address,
        today: today
      }) do
    with {:ok, validated_cart} <- Cart.validate(cart, stock),
         subtotal = Cart.subtotal(validated_cart),
         {:ok, discounted} <- Coupon.apply(subtotal, coupon, today),
         total_items = count_items(validated_cart),
         {:ok, shipping} <- Shipping.calculate(address, total_items) do
      {:ok,
       %{
         subtotal_cents: subtotal,
         discounted_cents: discounted,
         shipping_cents: shipping,
         total_cents: discounted + shipping
       }}
    end
  end

  defp count_items(%{items: items}) do
    Enum.reduce(items, 0, fn %{quantity: q}, acc -> acc + q end)
  end
end
```

**Why this works:**

- Each `<-` step can fail with a typed tuple. If `Cart.validate/2` returns
  `{:error, :empty_cart}`, the pipeline short-circuits and the caller receives
  exactly that tuple — no transformation, no loss of information.
- `subtotal = Cart.subtotal(...)` and `total_items = count_items(...)` are plain
  assignments inside `with`. They cannot fail. Mixing them with `<-` steps keeps
  the intermediate data visible.
- There is no `else` block. When every failure tuple already has the shape the
  caller expects (`{:error, reason}`), let `with` pass them through unchanged.
  The `else` block is for when you need to TRANSFORM errors (e.g., to wrap them
  in an envelope).

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.



---
## Key Concepts

### 1. `with` Chains Multiple Pattern Matches

```elixir
with {:ok, user} <- fetch_user(id),
     {:ok, profile} <- fetch_profile(user),
     :ok <- validate(profile) do
  {:ok, {user, profile}}
else
  error -> error
end
```

Each line pattern-matches. If any line fails, control jumps to `else`. This reads left-to-right like a Unix pipeline and avoids nested `case` statements.

### 2. `with` Threads Successful Results

Each successful result is bound to the variable on the left. Subsequent lines can reference previous variables. This is powerful for stateful transformations where each step depends on the previous.

### 3. `else` Handles All Failures

The `else` block pattern-matches on any non-matching result from the `with` lines. You can handle specific errors differently. If no pattern matches in `else`, an exception is raised.

---
## When you DO need `else`

Add an `else` when you want to change the error shape:

```elixir
def process_with_wrapped_errors(request) do
  with {:ok, order} <- process(request) do
    {:ok, order}
  else
    {:error, {:out_of_stock, sku}} -> {:error, %{code: "STOCK", sku: sku}}
    {:error, reason} -> {:error, %{code: to_string(reason)}}
  end
end
```

### Step 7: Tests

**Objective**: Assert empty cart short-circuits before the expired coupon check, proving `with` halts at the first failure rather than collecting them.

```elixir
# test/checkout_flow/checkout_test.exs
defmodule CheckoutFlow.CheckoutTest do
  use ExUnit.Case, async: true

  alias CheckoutFlow.Checkout

  @today ~D[2026-04-12]

  @item_a %{sku: "A", quantity: 2, unit_price: 1000}
  @item_b %{sku: "B", quantity: 1, unit_price: 500}

  @full_stock %{"A" => 10, "B" => 10}

  @valid_coupon %{code: "SAVE10", percent_off: 10, valid_until: ~D[2026-12-31]}
  @expired_coupon %{code: "OLD", percent_off: 10, valid_until: ~D[2024-01-01]}

  @address_es %{country: "ES", postal_code: "28001"}
  @address_xx %{country: "XX", postal_code: "00000"}

  defp request(overrides \\ %{}) do
    Map.merge(
      %{
        cart: %{items: [@item_a, @item_b]},
        stock: @full_stock,
        coupon: nil,
        address: @address_es,
        today: @today
      },
      overrides
    )
  end

  describe "happy path" do
    test "no coupon, valid address" do
      assert {:ok, order} = Checkout.process(request())
      # subtotal: 2*1000 + 500 = 2500
      assert order.subtotal_cents == 2500
      assert order.discounted_cents == 2500
      # 3 items * 500g = 1500g -> ceil to 2kg * 500 = 1000
      assert order.shipping_cents == 1000
      assert order.total_cents == 3500
    end

    test "valid coupon applies 10 percent off" do
      assert {:ok, order} = Checkout.process(request(%{coupon: @valid_coupon}))
      assert order.subtotal_cents == 2500
      assert order.discounted_cents == 2250
      assert order.total_cents == 2250 + 1000
    end
  end

  describe "cart failures" do
    test "empty cart" do
      assert {:error, :empty_cart} =
               Checkout.process(request(%{cart: %{items: []}}))
    end

    test "out of stock names the sku" do
      assert {:error, {:out_of_stock, "A"}} =
               Checkout.process(request(%{stock: %{"A" => 0, "B" => 10}}))
    end
  end

  describe "coupon failures" do
    test "expired coupon" do
      assert {:error, :coupon_expired} =
               Checkout.process(request(%{coupon: @expired_coupon}))
    end

    test "coupon with invalid percent" do
      bad = %{code: "BAD", percent_off: 150, valid_until: ~D[2026-12-31]}
      assert {:error, :invalid_discount} = Checkout.process(request(%{coupon: bad}))
    end
  end

  describe "shipping failures" do
    test "unsupported country" do
      assert {:error, :unsupported_country} =
               Checkout.process(request(%{address: @address_xx}))
    end
  end

  describe "short-circuit order" do
    test "cart failure happens before coupon check" do
      # If the cart is empty, the expired coupon never runs.
      assert {:error, :empty_cart} =
               Checkout.process(request(%{cart: %{items: []}, coupon: @expired_coupon}))
    end
  end
end
```

### Step 8: Run the tests

**Objective**: Run with `--trace` so the short-circuit order is visible in the output and nobody ships a silently reordered pipeline.

```bash
mix test --trace
```

---

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of checkout/1 over 100k carts
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 200ms total; the bottleneck should be your actual business logic, not the `with` dispatch**.

## Trade-off analysis

| Aspect | `with` pipeline | Nested `case` | Exceptions |
|--------|----------------|---------------|------------|
| Readability of happy path | linear, top-to-bottom | pyramid grows with steps | linear |
| Error information | typed tuples, precise | same (but harder to read) | stack trace, less precise |
| Short-circuit | automatic | automatic | automatic via raise |
| Testability | assert on tuples | assert on tuples | assert_raise + inspect msg |
| Cost of a new step | one more `<-` line | another nesting level | try/rescue to handle |

When `with` loses: if each step needs to collect ALL errors (e.g., form validation
showing every field error at once), `with` short-circuits too eagerly. Use a
validation library or reduce over the steps accumulating errors.

---

## Common production mistakes

**1. `WithClauseError` from an incomplete `else`**
If you add `else` with `{:error, :a} -> ...`, and later a step starts returning
`{:error, :b}`, you get a crash at runtime. Always add `other -> other` (or at
least `{:error, reason} -> {:error, reason}`) as a catch-all in non-trivial
`else` blocks.

**2. Using `with` where `|>` would do**
If none of the steps can fail, `with` adds ceremony. Use the pipe operator. `with`
pays for itself only when there are real short-circuit points.

**3. Hiding which step failed**
An `else` block that reduces every error to `{:error, :failed}` throws away the
information the pipeline produces. The whole point of `with` is that each step
returns a specific reason — preserve it.

**4. Mixing `<-` and `=` without understanding**
`{:ok, x} <- f()` pattern-matches and triggers `else` on mismatch. `x = f()` is
a plain assignment; it cannot fail (except on explicit pattern failure like
`{:ok, x} = f()` with `=`, which raises `MatchError`). Use `<-` for fallible
steps, `=` for deterministic assignments.

**5. Floating-point money**
`subtotal * 0.10` looks innocent until you see `2499.9999999998`. Use integer
cents and `div/2`. This project keeps all amounts in cents for this reason.

---

## When NOT to use `with`

- When you need ALL errors from ALL steps (form validation) — `with` short-circuits
- When there is only one fallible step — a `case` is clearer
- When the happy path has more than ~6 steps — at that point, split into named
  functions each with their own smaller `with`

---

## Reflection

If two steps in your `with` can both return `{:error, :not_found}` but for different entities (cart, payment method), how do you tell them apart at the top of the stack? Redesign the error tuples to preserve context.

Your teammate adds an `else` clause that rescues unexpected errors and logs them. Is that the right place for cross-cutting logging, or does it belong in a middleware layer? Argue both sides.

## Resources

- [`with` expression — Elixir docs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
- [Railway-Oriented Programming — Scott Wlaschin](https://fsharpforfunandprofit.com/rop/)
- [Credo's `with` style guide](https://github.com/rrrene/credo/blob/master/guides/custom_checks/ELS002.md)
- [`Date` and `Date.compare/2`](https://hexdocs.pm/elixir/Date.html)
