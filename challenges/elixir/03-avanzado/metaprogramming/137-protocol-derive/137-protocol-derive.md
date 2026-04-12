# Protocol `@derive` — Consuming and Implementing

**Project**: `protocol_derive` — use `@derive` for built-in protocols (`Inspect`, `Jason.Encoder`), then implement a custom protocol that supports its own `@derive` via the `Protocol` module's generation hooks.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

Your domain has ~60 structs: orders, line items, customers, addresses, payments. Each
needs consistent `inspect/2` (mask PII), JSON encoding (drop internal fields), and
audit-log serialization (include metadata). Writing `defimpl` for each protocol on
each struct is 180 `defimpl` blocks — pure boilerplate.

The idiomatic Elixir answer is `@derive`: put an annotation on the struct, and the
protocol infrastructure auto-generates the impl. Built-in protocols (`Inspect`,
`Jason.Encoder`, `Enumerable`) support this. You will:

1. Drive the built-ins with configuration (`@derive {Inspect, only: [:id]}`).
2. Implement your own `AuditLog.Encoder` protocol that accepts `@derive` as the
   default path.

```
protocol_derive/
├── lib/
│   └── protocol_derive/
│       ├── audit_encoder.ex        # defprotocol with derive hook
│       ├── schemas.ex              # sample structs
│       └── helpers.ex
├── test/
│   └── protocol_derive_test.exs
└── mix.exs
```

---

## Core concepts

### 1. How `@derive` works

`@derive X` — before `defstruct` — records the instruction. When `defstruct`
runs, it looks up `X` and calls `X.__deriving__/3` (if defined) with the struct
module and options. `__deriving__/3` returns a quoted expression containing a
`defimpl` block.

```
              @derive {Inspect, only: [:id]}
              defstruct [:id, :name, :ssn]
                         │
                         ▼
          Inspect.__deriving__(MyStruct, struct_data, [only: [:id]])
                         │
                         ▼
                ast for defimpl Inspect, for: MyStruct
```

### 2. Required callbacks to support `@derive`

To make a protocol derivable:

```
defprotocol MyProto do
  @fallback_to_any true
  def encode(value)
end

defimpl MyProto, for: Any do
  # Default fallback — used when no specific impl and no derive.
  defmacro __deriving__(module, _struct, opts) do
    # return a quote that defines `defimpl MyProto, for: module do ... end`
  end

  def encode(_), do: raise("must derive or implement")
end
```

### 3. `:only` and `:except`

Conventional options for field-level control. `@derive {Jason.Encoder, only:
[:id, :name]}` means "encode those fields; ignore the rest". Your custom
protocol should honor the same convention.

### 4. Composability

Multiple `@derive` lines pile up:

```
@derive {Jason.Encoder, only: [:id]}
@derive {Inspect, except: [:ssn]}
@derive AuditLog.Encoder
```

The order does not matter — each runs independently in its own `defimpl`.

### 5. Consolidation interaction

After compile, `Protocol.consolidate/2` merges all derived impls with explicit ones.
If you later want to `defimpl` manually, remove `@derive` first — otherwise you get
a "conflicting implementation" error.

---

## Implementation

### Step 1: `lib/protocol_derive/audit_encoder.ex`

```elixir
defprotocol ProtocolDerive.AuditEncoder do
  @moduledoc """
  Converts a term into an audit-log map: `%{type:, id:, fields:}`.
  Supports `@derive ProtocolDerive.AuditEncoder` on structs.
  """

  @fallback_to_any true

  @spec to_audit(term()) :: map()
  def to_audit(value)
end

defimpl ProtocolDerive.AuditEncoder, for: Any do
  defmacro __deriving__(module, struct_fields, opts) do
    only = Keyword.get(opts, :only, Map.keys(struct_fields) -- [:__struct__])
    except = Keyword.get(opts, :except, [])
    fields = only -- except

    quote do
      defimpl ProtocolDerive.AuditEncoder, for: unquote(module) do
        def to_audit(struct) do
          %{
            type: unquote(module),
            id: Map.get(struct, :id),
            fields:
              struct
              |> Map.take(unquote(fields))
              |> ProtocolDerive.AuditEncoder.Helpers.mask_pii()
          }
        end
      end
    end
  end

  def to_audit(value) do
    %{type: :anonymous, id: nil, fields: %{value: inspect(value)}}
  end
end
```

### Step 2: `lib/protocol_derive/helpers.ex`

```elixir
defmodule ProtocolDerive.AuditEncoder.Helpers do
  @moduledoc false

  @pii_fields [:ssn, :credit_card, :password, :token]

  @spec mask_pii(map()) :: map()
  def mask_pii(map) do
    Map.new(map, fn
      {k, _v} when k in @pii_fields -> {k, "[MASKED]"}
      {k, v} -> {k, v}
    end)
  end
end
```

### Step 3: `lib/protocol_derive/schemas.ex`

```elixir
defmodule ProtocolDerive.Schemas do
  defmodule Customer do
    @derive {Inspect, except: [:ssn]}
    @derive {ProtocolDerive.AuditEncoder, only: [:id, :name, :email]}
    defstruct [:id, :name, :email, :ssn]
  end

  defmodule Order do
    @derive ProtocolDerive.AuditEncoder
    defstruct [:id, :customer_id, :total, :status, :internal_notes]
  end

  defmodule Payment do
    @derive {ProtocolDerive.AuditEncoder, except: [:internal_notes]}
    defstruct [:id, :order_id, :amount, :credit_card, :internal_notes]
  end
end
```

### Step 4: Tests

```elixir
defmodule ProtocolDeriveTest do
  use ExUnit.Case, async: true

  alias ProtocolDerive.AuditEncoder
  alias ProtocolDerive.Schemas.{Customer, Order, Payment}

  describe "Inspect derive" do
    test "Customer hides :ssn" do
      assert inspect(%Customer{id: 1, name: "A", email: "a@b", ssn: "123"}) =~ "#Customer<"
      refute inspect(%Customer{id: 1, ssn: "secret"}) =~ "secret"
    end
  end

  describe "AuditEncoder — explicit fields" do
    test "Customer: only name/email/id" do
      c = %Customer{id: 1, name: "A", email: "a@b.c", ssn: "123"}
      audit = AuditEncoder.to_audit(c)
      assert audit.type == Customer
      assert audit.id == 1
      assert Map.keys(audit.fields) |> Enum.sort() == [:email, :id, :name]
      refute Map.has_key?(audit.fields, :ssn)
    end
  end

  describe "AuditEncoder — all fields (no only/except)" do
    test "Order includes every non-struct field" do
      o = %Order{id: 9, customer_id: 1, total: 100, status: :paid, internal_notes: "ok"}
      audit = AuditEncoder.to_audit(o)
      assert audit.type == Order
      assert audit.id == 9
      assert audit.fields == %{
               id: 9,
               customer_id: 1,
               total: 100,
               status: :paid,
               internal_notes: "ok"
             }
    end
  end

  describe "AuditEncoder — except + PII masking" do
    test "Payment masks :credit_card automatically, excludes :internal_notes" do
      p = %Payment{id: 7, order_id: 9, amount: 100, credit_card: "4242", internal_notes: "x"}
      audit = AuditEncoder.to_audit(p)
      assert audit.fields.credit_card == "[MASKED]"
      refute Map.has_key?(audit.fields, :internal_notes)
    end
  end

  describe "Any fallback" do
    test "non-derived value gets :anonymous type" do
      assert %{type: :anonymous, id: nil} = AuditEncoder.to_audit(42)
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `__deriving__/3` is a macro.** It runs at compile time and receives AST-ready
fields — not runtime values. Field masking logic that uses `@pii_fields` must be a
constant list known at compile time.

**2. Order of `@derive` matters only within `Inspect`.** If you stack `@derive
{Inspect, only: [:id]}` and then `@derive {Inspect, except: [:ssn]}`, only the
last wins — multiple derives for the *same* protocol conflict.

**3. `@derive` must come BEFORE `defstruct`.** After `defstruct` it silently
no-ops (compile warning in newer versions).

**4. Consolidation freezes derived impls.** Recompiling only the struct without
the protocol may leave the consolidated module stale. Run `mix compile --force` if
in doubt.

**5. `Any` fallback vs Any derived.** `defimpl ... for: Any` is different from
`@derive ... for: Any`. The fallback fires for terms with no struct at all
(integers, maps); derive fires for structs that opted in.

**6. `__deriving__/3` is private-ish.** The official API is documented but few
community protocols support it. Always wrap the struct-fields argument defensively
(it may be a keyword list of defaults, not a map, in some Elixir versions).

**7. Performance.** Derived impls are regular `defimpl` — same dispatch cost
as hand-written ones. The savings are purely in code authorship, not runtime.

**8. When NOT to use this.** For 2–3 structs, writing `defimpl` manually is
clearer. `@derive` shines at 10+ consumers where the same shape recurs.

---

## Benchmark

```elixir
# bench/derive_bench.exs
c = %ProtocolDerive.Schemas.Customer{id: 1, name: "X", email: "x@y", ssn: "1"}

Benchee.run(%{
  "AuditEncoder.to_audit (derived)" => fn -> ProtocolDerive.AuditEncoder.to_audit(c) end
})
```

Expect ~1–3 µs per call — dominated by the `Map.take/2` step, not the protocol
dispatch.

---

## Resources

- [`Protocol` — hexdocs.pm](https://hexdocs.pm/elixir/Protocol.html) — `__deriving__` explanation
- [Jason.Encoder derive source](https://github.com/michalmuskala/jason/blob/master/lib/encoder.ex)
- [Inspect.Any + defprotocol](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/inspect.ex)
- [*Elixir in Action* — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — protocols chapter
- [Dashbit blog on protocols](https://dashbit.co/blog)
- [Ecto @derive usage](https://hexdocs.pm/ecto/Ecto.Schema.html#module-the-ecto-changeset-derive)
