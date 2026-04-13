# Protocol `@derive` — Consuming and Implementing

**Project**: `protocol_derive` — use `@derive` for built-in protocols (`Inspect`, `Jason.Encoder`), then implement a custom protocol that supports its own `@derive` via the `Protocol` module's generation hooks.

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

## Why derivation and not manual impl

Hand implementations become N copies of the same `defimpl`. `@derive` calls a single code-generating function per struct at compile time, keeping the protocol's contract in one place.

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

**Metaprogramming-specific insight:**
Code generation is powerful and dangerous. Every macro you write is a place where intent is hidden. Use macros sparingly, only when they eliminate genuine boilerplate. If your macro is more than 10 lines, you probably need a function or data structure instead. Future maintainers will thank you.
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

## Design decisions

**Option A — hand-implement the protocol per struct**
- Pros: full control; no macro surprises.
- Cons: boilerplate multiplies with every new struct; drift between implementations.

**Option B — `@derive` + default implementation** (chosen)
- Pros: one-line opt-in; consistency enforced by the protocol.
- Cons: customization requires overriding, which is easy to forget; compile error surface is wider.

→ Chose **B** because most structs need the default behavior; deriving keeps that the obvious path.

---

## Implementation

### Step 1: `lib/protocol_derive/audit_encoder.ex`

**Objective**: Define protocol and implement __deriving__/3 on Any so @derive annotation generates struct-specific defimpl blocks.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

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

**Objective**: Isolate @pii_fields allow-list so all derived impls mask sensitive fields uniformly.

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

**Objective**: Define sample structs with @derive annotations (default, only, except) to exercise __deriving__/3 code paths.

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

**Objective**: Verify derive shapes, PII masking, the Any fallback, and that derive options round-trip through generated impls.

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

### Why this works

`@derive Protocol` triggers `Protocol.derive/3` at module compilation, which invokes the protocol's `__deriving__/3` callback. That callback emits a full `defimpl` block against the deriving module, so the result is indistinguishable from a hand-written impl — except maintenance lives in the protocol.

---


## Key Concepts: Automatic Implementation via Protocol Derive

Protocol derivation uses macros to automatically generate protocol implementations for structs. Instead of manually implementing `@derive Enumerable` via a module, you declare `@derive Enumerable` on the struct definition, and the macro generates the code at compile time. This saves boilerplate for common protocols like `Enumerable`, `Inspect`, `String.Chars`.

For custom protocols, you can define a custom derive implementation via a macro that receives the struct definition and generates the protocol functions. This is powerful for implementing repetitive patterns (e.g., auto-generating a JSON encoder from struct fields), but requires careful macro design to avoid hygiene issues.


## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

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

## Reflection

- Your protocol now needs a struct-specific knob (e.g. a formatting option). Do you keep `@derive` and take it via a module attribute, or switch to explicit impls? What is the readability cost of each?
- After consolidation, protocols become static. How does that change your feelings about `@derive` for libraries loaded at runtime by plugins?

---


## Executable Example

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

defmodule Main do
  def main do
      # Demonstrate @derive for automatic protocol implementations
      defmodule User do
        @derive [Inspect]
        defstruct name: "", age: 0
      end

      # Create instance
      user = %User{name: "Alice", age: 30}

      # Derived Inspect protocol
      inspected = inspect(user)

      IO.puts("✓ Derived Inspect:")
      IO.puts("  #{inspected}")

      # Create a custom protocol with @derive support
      defprotocol Serializable do
        def to_json(data)
      end

      # Implement for User
      defimpl Serializable, for: User do
        def to_json(user) do
          Jason.encode!(%{"name" => user.name, "age" => user.age})
        end
      end

      json = Serializable.to_json(user)
      IO.puts("✓ Custom protocol: #{json}")

      assert String.contains?(inspected, "Alice"), "Inspect works"
      assert String.contains?(json, "Alice"), "Serializable works"

      IO.puts("✓ Protocol @derive: automatic implementation working")
  end
end

Main.main()
```
