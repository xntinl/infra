# Jason vs Poison: API shape, performance, and the ecosystem shift

**Project**: `json_compare` — a tiny project that benchmarks
[Jason](https://hexdocs.pm/jason/) against
[Poison](https://hexdocs.pm/poison/) on encode and decode, exposes a single
`JsonCompare` module abstracting behind a behaviour, and documents why the
Elixir community moved off Poison as the default.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Before ~2018, Poison was the de-facto JSON library for Elixir — it shipped
with Phoenix, Ecto, and most community libraries. Then Jason appeared,
written by Michał Muskała (Elixir core team) as a drop-in replacement with
meaningfully better performance on decode-heavy workloads and a simpler
encoding protocol.

Today: **Phoenix, Ecto, Req, Plug, and nearly every major library declare
`Jason` as an optional or default dependency**. Poison is still maintained
(v6.0.0 was published on 2024-06-09, per hex.pm) and still downloaded
millions of times per month thanks to transitive deps, but new projects
should start with Jason. Many older tutorials still show Poison; knowing
both — and the differences — makes upgrading legacy code painless.

Project structure:

```
json_compare/
├── lib/
│   ├── json_compare.ex
│   └── json_compare/
│       ├── adapter.ex
│       ├── adapter/jason.ex
│       └── adapter/poison.ex
├── test/
│   └── json_compare_test.exs
├── bench/
│   └── encode_decode.exs
└── mix.exs
```

---

## Core concepts

### 1. API surface — near-identical

| Operation | Jason | Poison |
|-----------|------------------------|-------------------------|
| Decode (ok tuple) | `Jason.decode/2` | `Poison.decode/2` |
| Decode (bang) | `Jason.decode!/2` | `Poison.decode!/2` |
| Encode (ok tuple) | `Jason.encode/2` | `Poison.encode/2` |
| Encode (bang) | `Jason.encode!/2` | `Poison.encode!/2` |
| To iodata | `Jason.encode_to_iodata/2` | `Poison.encode_to_iodata/2` |

Both accept `keys: :atoms` on decode (dangerous — creates atoms from
untrusted input, which can exhaust the atom table). Both accept `:pretty`
on encode.

### 2. Protocols — subtly different

- **Jason** defines `Jason.Encoder` protocol. You derive it:
  `@derive {Jason.Encoder, only: [:id, :name]}`.
- **Poison** defines `Poison.Encoder`. Same idea, incompatible with Jason's.

If your struct needs to be serializable by both, derive both.

### 3. Performance — why Jason won

Jason uses binary pattern matching directly on the input (the same
technique powering NimbleParsec — see exercise 136), avoiding intermediate
lists and minimizing allocations. Poison's pipeline is more dynamic. On
typical JSON blobs (API payloads, tens of KB), Jason is roughly 2–3×
faster on decode and 1.5–2× faster on encode. We'll verify with Benchee.

### 4. Ecosystem status

- **Jason**: maintained, used by default in Phoenix ≥ 1.4, Ecto ≥ 3.0,
  Req, Plug, Oban, and most libraries.
- **Poison**: still maintained (v6.0.0, 2024-06-09) but no longer the
  default. The Poison repo README itself acknowledges Jason as the modern
  choice.
- **`:json` in OTP 27+**: Erlang/OTP now ships a stdlib `:json` module.
  For greenfield projects targeting OTP 27+ that only need basic
  encode/decode, consider it before pulling in a dep.

---

## Implementation

### Step 1: Create the project

```bash
mix new json_compare
cd json_compare
```

Deps in `mix.exs`:

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:poison, "~> 6.0"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: Behaviour + two adapters

`lib/json_compare/adapter.ex`:

```elixir
defmodule JsonCompare.Adapter do
  @moduledoc "Behaviour so application code does not bind to a specific lib."

  @callback encode(term()) :: {:ok, binary()} | {:error, term()}
  @callback decode(binary()) :: {:ok, term()} | {:error, term()}
end
```

`lib/json_compare/adapter/jason.ex`:

```elixir
defmodule JsonCompare.Adapter.Jason do
  @behaviour JsonCompare.Adapter

  @impl true
  def encode(term), do: Jason.encode(term)

  @impl true
  def decode(binary), do: Jason.decode(binary)
end
```

`lib/json_compare/adapter/poison.ex`:

```elixir
defmodule JsonCompare.Adapter.Poison do
  @behaviour JsonCompare.Adapter

  @impl true
  def encode(term) do
    {:ok, Poison.encode!(term)}
  rescue
    e -> {:error, e}
  end

  @impl true
  def decode(binary) do
    {:ok, Poison.decode!(binary)}
  rescue
    e -> {:error, e}
  end
end
```

### Step 3: Top-level module

`lib/json_compare.ex`:

```elixir
defmodule JsonCompare do
  @moduledoc """
  Thin facade over a configured JSON adapter. Swap implementations via
  `config :json_compare, :adapter, JsonCompare.Adapter.Jason`.
  """

  def encode(term), do: adapter().encode(term)
  def decode(bin), do: adapter().decode(bin)

  defp adapter,
    do: Application.get_env(:json_compare, :adapter, JsonCompare.Adapter.Jason)
end
```

### Step 4: Tests

`test/json_compare_test.exs`:

```elixir
defmodule JsonCompareTest do
  use ExUnit.Case, async: true

  @payload %{"name" => "Ada", "tags" => ["a", "b"], "n" => 42}

  describe "adapters produce compatible output" do
    test "Jason and Poison round-trip to the same map" do
      {:ok, j} = JsonCompare.Adapter.Jason.encode(@payload)
      {:ok, p} = JsonCompare.Adapter.Poison.encode(@payload)

      # Byte order of map keys isn't guaranteed, but decoding must match.
      assert {:ok, @payload} = JsonCompare.Adapter.Jason.decode(j)
      assert {:ok, @payload} = JsonCompare.Adapter.Jason.decode(p)
      assert {:ok, @payload} = JsonCompare.Adapter.Poison.decode(j)
    end

    test "invalid JSON returns an error tuple, not a raise" do
      assert {:error, _} = JsonCompare.Adapter.Jason.decode("{bad")
      assert {:error, _} = JsonCompare.Adapter.Poison.decode("{bad")
    end
  end

  describe "facade honours configuration" do
    test "defaults to Jason" do
      assert {:ok, bin} = JsonCompare.encode(%{a: 1})
      assert {:ok, %{"a" => 1}} = JsonCompare.decode(bin)
    end
  end
end
```

### Step 5: Benchmark

`bench/encode_decode.exs`:

```elixir
payload = %{
  "users" =>
    for i <- 1..500 do
      %{"id" => i, "name" => "User #{i}", "tags" => ["a", "b", "c"]}
    end
}

{:ok, encoded_jason} = Jason.encode(payload)
{:ok, encoded_poison} = Poison.encode(payload)

Benchee.run(
  %{
    "Jason.encode" => fn -> Jason.encode!(payload) end,
    "Poison.encode" => fn -> Poison.encode!(payload) end
  },
  time: 3,
  memory_time: 1
)

Benchee.run(
  %{
    "Jason.decode" => fn -> Jason.decode!(encoded_jason) end,
    "Poison.decode" => fn -> Poison.decode!(encoded_poison) end
  },
  time: 3,
  memory_time: 1
)
```

Run with `mix run bench/encode_decode.exs`. On a modern laptop you should
see Jason ~2–3× faster on decode and faster on encode, with less memory.

---

## Trade-offs and production gotchas

**1. `keys: :atoms` is a footgun**
Decoding untrusted JSON with `keys: :atoms` creates atoms forever (they're
never GC'd). One attacker request with random keys and you exhaust the
atom table (~1M default). Use `:atoms!` to only match *existing* atoms, or
keep string keys.

**2. Protocol incompatibility cuts both ways**
`@derive {Jason.Encoder, only: [...]}` doesn't make your struct encodable
by Poison. If your app has both libraries (common in migrations), derive
both or pick one.

**3. Precision of large integers and floats differs historically**
Jason decodes integers exactly and floats using Erlang's `:erlang.binary_to_float/1`.
Poison behaves the same way today but older versions differ. If you round-trip
financial data, add explicit tests.

**4. `Jason` is still in 1.x because it's stable, not because it's small**
Don't mistake "1.4.x" for immaturity. The API has been intentionally frozen;
breaking changes are reserved for a hypothetical 2.0.

**5. `:json` from OTP 27 — know it exists**
For pure encode/decode without derive, validation schemas, or fancy
options, the stdlib module is one less dependency. It doesn't replace
Jason for libraries that need protocols or streaming encoding.

**6. When NOT to migrate away from Poison**
If you inherit a large app with Poison embedded everywhere (custom
encoders, 3rd-party libs pinning it), migrating just for perf isn't worth
it — both work. Migrate only when Poison becomes a blocker (e.g.,
a dependency needs `Jason.Encoder`).

---

## Resources

- [Jason on HexDocs](https://hexdocs.pm/jason/Jason.html)
- [Poison on hex.pm](https://hex.pm/packages/poison) — still listed, still maintained
- [Benchee](https://hexdocs.pm/benchee/) — benchmarking
- [OTP 27 `:json` module](https://www.erlang.org/doc/apps/stdlib/json.html) — stdlib alternative
- [José Valim on atom table exhaustion](https://elixirforum.com/t/jason-decode-keys-atoms/) — context on `:atoms!`
