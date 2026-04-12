# Erlang Interop — Calling OTP Libraries from Elixir

**Project**: `erlang_interop` — an internal toolbox that wraps battle-tested Erlang/OTP libraries (`:crypto`, `:inet_res`, `:queue`, `:digraph`) behind idiomatic Elixir APIs.

**Difficulty**: ★★★★☆

**Estimated time**: 3–6 hours

---

## Project context

Elixir sits on top of the Erlang VM and every Erlang module is directly callable from Elixir. In production codebases, a large fraction of real performance-sensitive work is delegated to OTP libraries: `:crypto` for MACs and AES-GCM, `:queue` for O(1) FIFO, `:digraph` for dependency graphs, `:ets`/`:dets` for caches, `:ssl` for TLS, `:inet_res` for DNS. Idiomatic Elixir wrappers may not exist for what you need, or may lag upstream Erlang.

Knowing how Erlang interop really works — atom casing, records, keyword-vs-proplist arguments, iolists, binary vs charlist, `:error`-tuple conventions — separates a developer who "uses Elixir" from one who understands the BEAM ecosystem. Phoenix, Plug, Ecto, and Broadway all rely on Erlang modules under the hood.

In this exercise you'll build `erlang_interop`, a cohesive wrapper layer exposing Erlang functionality as Elixir modules with proper typespecs, idiomatic error tuples, and tests that pin behavior you might otherwise take on faith.

```
erlang_interop/
├── lib/
│   └── erlang_interop/
│       ├── crypto.ex         # :crypto wrapper — AES-GCM, HMAC
│       ├── fifo.ex           # :queue wrapper — O(1) FIFO
│       ├── dns.ex            # :inet_res wrapper — DNS lookups
│       ├── graph.ex          # :digraph wrapper — dependency graphs
│       └── records.ex        # Record.defrecord examples
├── test/
│   └── erlang_interop_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Atoms and module names

An Elixir module `Foo.Bar` compiles to the Erlang atom `Elixir.Foo.Bar`. Erlang modules are lowercase atoms (`:crypto`, `:inet`, `:ets`). To call an Erlang function:

```elixir
:crypto.hash(:sha256, "payload")   # Erlang: crypto:hash(sha256, <<"payload">>)
```

Rule: if the Erlang module/function starts with a lowercase letter, prefix `:`. Atoms in Erlang are unquoted lowercase identifiers; in Elixir they are prefixed with `:`.

### 2. Strings: binary vs charlist

Erlang's default string is a **charlist** (`[104, 101, 108, 108, 111]`). Elixir's default is a **binary** (`"hello"`). Some Erlang APIs are strict:

| Function | Expects | Note |
|---|---|---|
| `:file.read_file/1` | binary or charlist | both |
| `:inet.parse_address/1` | **charlist** | predates unicode binaries |
| `:crypto.hash/2` | binary | modern API |
| `:io_lib.format/2` | returns iolist | flatten with `IO.iodata_to_binary/1` |

A common source of `:badarg` errors is passing a binary where a charlist is required.

### 3. Records — compile-time tagged tuples

Erlang records are syntactic sugar for tagged tuples. `-record(user, {id, name})` becomes `{user, Id, Name}`. In Elixir, `Record.defrecord/2` generates macros:

```elixir
require Record
Record.defrecord(:user, id: nil, name: nil)

user()              # => {:user, nil, nil}
user(id: 1, name: "a")  # => {:user, 1, "a"}
user(rec, :id)      # => elem(rec, 1)
```

Used when interoperating with Erlang libraries that expose record definitions in `.hrl` files (`:mnesia`, `:dialyzer`, `:diameter`).

### 4. `atom_to_list` and the atom-table trap

`:erlang.atom_to_list/1` returns a charlist. The inverse, `:erlang.list_to_atom/1`, is **dangerous** — atoms are never garbage-collected and the VM caps them at 1,048,576 by default. User-controlled input can exhaust the table and crash the node.

```elixir
# SAFE
:erlang.list_to_existing_atom(~c"user_created")

# DANGEROUS with untrusted input
:erlang.list_to_atom(user_input)
```

### 5. Error return conventions

Erlang functions typically return `{:ok, value}`, `{:error, reason}`, `:ok`, or a bare value. Wrappers often add a bang version that raises on error. Note the **inconsistencies** you have to normalize — `:crypto.crypto_one_time_aead` returns a bare `:error` atom on auth failure; `:queue.out` returns `{:empty, q}`, not `:empty`.

### 6. Options: keyword list vs proplist

Elixir idiom: `[timeout: 5000, retries: 3]`. Erlang "proplist" also accepts bare atoms (`[:ssl, {:timeout, 5000}]`). Check docs per call — mixing them can silently ignore options.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule ErlangInterop.MixProject do
  use Mix.Project

  def project do
    [app: :erlang_interop, version: "0.1.0", elixir: "~> 1.15", deps: []]
  end

  def application, do: [extra_applications: [:logger, :crypto, :ssl, :inets]]
end
```

### Step 2: `lib/erlang_interop/crypto.ex`

```elixir
defmodule ErlangInterop.Crypto do
  @moduledoc """
  Idiomatic wrapper over `:crypto`. AES-GCM and HMAC with consistent
  `{:ok, _} | {:error, _}` contracts.
  """

  @type key :: <<_::128>> | <<_::256>>
  @type iv :: <<_::96>>

  @spec hmac_sha256(binary(), binary()) :: binary()
  def hmac_sha256(key, data) when is_binary(key) and is_binary(data) do
    :crypto.mac(:hmac, :sha256, key, data)
  end

  @spec encrypt_aes_gcm(key(), iv(), binary(), binary()) ::
          {:ok, {binary(), <<_::128>>}} | {:error, term()}
  def encrypt_aes_gcm(key, iv, plaintext, aad \\ <<>>)
      when byte_size(iv) == 12 and byte_size(key) in [16, 32] do
    {ct, tag} =
      :crypto.crypto_one_time_aead(:aes_256_gcm, key, iv, plaintext, aad, true)

    {:ok, {ct, tag}}
  rescue
    e -> {:error, e}
  end

  @spec decrypt_aes_gcm(key(), iv(), binary(), <<_::128>>, binary()) ::
          {:ok, binary()} | {:error, :auth_failed | term()}
  def decrypt_aes_gcm(key, iv, ct, tag, aad \\ <<>>)
      when byte_size(iv) == 12 and byte_size(tag) == 16 do
    case :crypto.crypto_one_time_aead(:aes_256_gcm, key, iv, ct, aad, tag, false) do
      :error -> {:error, :auth_failed}
      pt when is_binary(pt) -> {:ok, pt}
    end
  rescue
    e -> {:error, e}
  end

  @spec random_iv() :: iv()
  def random_iv, do: :crypto.strong_rand_bytes(12)

  @spec random_key(128 | 256) :: key()
  def random_key(256), do: :crypto.strong_rand_bytes(32)
  def random_key(128), do: :crypto.strong_rand_bytes(16)
end
```

### Step 3: `lib/erlang_interop/fifo.ex`

```elixir
defmodule ErlangInterop.Fifo do
  @moduledoc "O(1) FIFO backed by Erlang's `:queue` (Okasaki's banker's queue)."

  @opaque t(a) :: {list(a), list(a)}

  @spec new() :: t(any())
  def new, do: :queue.new()

  @spec push(t(a), a) :: t(a) when a: var
  def push(q, item), do: :queue.in(item, q)

  @spec pop(t(a)) :: {:ok, a, t(a)} | :empty when a: var
  def pop(q) do
    case :queue.out(q) do
      {{:value, item}, rest} -> {:ok, item, rest}
      {:empty, _} -> :empty
    end
  end

  @spec size(t(any())) :: non_neg_integer()
  def size(q), do: :queue.len(q)

  @spec to_list(t(a)) :: [a] when a: var
  def to_list(q), do: :queue.to_list(q)
end
```

### Step 4: `lib/erlang_interop/dns.ex`

```elixir
defmodule ErlangInterop.Dns do
  @moduledoc "DNS lookups via `:inet_res`. Takes **charlists**, not binaries."

  @spec resolve(String.t(), :a | :aaaa | :txt | :mx | :srv, timeout()) ::
          {:ok, [term()]} | {:error, term()}
  def resolve(hostname, type \\ :a, timeout \\ 5_000) when is_binary(hostname) do
    host = String.to_charlist(hostname)

    case :inet_res.resolve(host, :in, type, [timeout: timeout]) do
      {:ok, {:dns_rec, _, _, answers, _, _}} ->
        {:ok, Enum.map(answers, &extract_rdata/1)}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp extract_rdata({:dns_rr, _name, _type, _class, _cnt, _ttl, data, _, _, _}), do: data
end
```

### Step 5: `lib/erlang_interop/graph.ex`

```elixir
defmodule ErlangInterop.Graph do
  @moduledoc """
  Directed acyclic graph via `:digraph` — a process-backed mutable structure.
  Callers MUST call `delete/1` to free the underlying ETS tables.
  """

  @opaque t :: :digraph.graph()

  @spec new() :: t()
  def new, do: :digraph.new([:acyclic])

  @spec delete(t()) :: true
  def delete(g), do: :digraph.delete(g)

  @spec add_dependency(t(), term(), term()) :: :ok | {:error, :cycle}
  def add_dependency(g, from, to) do
    :digraph.add_vertex(g, from)
    :digraph.add_vertex(g, to)

    case :digraph.add_edge(g, from, to) do
      {:error, {:bad_edge, _}} -> {:error, :cycle}
      _edge -> :ok
    end
  end

  @spec topological_sort(t()) :: [term()] | :cycle_detected
  def topological_sort(g) do
    case :digraph_utils.topsort(g) do
      false -> :cycle_detected
      sorted -> sorted
    end
  end
end
```

### Step 6: `lib/erlang_interop/records.ex`

```elixir
defmodule ErlangInterop.Records do
  @moduledoc "Example usage of `Record.defrecord/2` for Erlang record shapes."

  require Record
  Record.defrecord(:user, id: nil, name: nil, email: nil)

  @type user :: record(:user, id: integer(), name: String.t(), email: String.t())

  @spec new_user(integer(), String.t(), String.t()) :: user()
  def new_user(id, name, email), do: user(id: id, name: name, email: email)

  @spec user_id(user()) :: integer()
  def user_id(u) when Record.is_record(u, :user), do: user(u, :id)

  @spec is_user(term()) :: boolean()
  def is_user(term), do: Record.is_record(term, :user)
end
```

### Step 7: `test/erlang_interop_test.exs`

```elixir
defmodule ErlangInteropTest do
  use ExUnit.Case, async: true

  alias ErlangInterop.{Crypto, Fifo, Graph, Records}

  describe "Crypto" do
    test "HMAC-SHA256 is deterministic and 32 bytes" do
      mac = Crypto.hmac_sha256("secret", "payload")
      assert byte_size(mac) == 32
      assert mac == Crypto.hmac_sha256("secret", "payload")
    end

    test "AES-GCM roundtrip" do
      key = Crypto.random_key(256)
      iv = Crypto.random_iv()
      {:ok, {ct, tag}} = Crypto.encrypt_aes_gcm(key, iv, "top secret", "aad")
      assert {:ok, "top secret"} = Crypto.decrypt_aes_gcm(key, iv, ct, tag, "aad")
    end

    test "AES-GCM detects tampering" do
      key = Crypto.random_key(256)
      iv = Crypto.random_iv()
      {:ok, {ct, tag}} = Crypto.encrypt_aes_gcm(key, iv, "hi", "")
      tampered = <<0>> <> binary_part(ct, 1, byte_size(ct) - 1)
      assert {:error, :auth_failed} = Crypto.decrypt_aes_gcm(key, iv, tampered, tag, "")
    end
  end

  describe "Fifo" do
    test "push/pop is FIFO" do
      q = Fifo.new() |> Fifo.push(:a) |> Fifo.push(:b) |> Fifo.push(:c)
      assert {:ok, :a, q} = Fifo.pop(q)
      assert {:ok, :b, q} = Fifo.pop(q)
      assert Fifo.size(q) == 1
    end

    test "pop on empty" do
      assert :empty = Fifo.pop(Fifo.new())
    end
  end

  describe "Graph" do
    test "topological sort on acyclic graph" do
      g = Graph.new()
      :ok = Graph.add_dependency(g, :compile, :test)
      :ok = Graph.add_dependency(g, :test, :release)
      assert [:compile, :test, :release] = Graph.topological_sort(g)
      Graph.delete(g)
    end

    test "cycle rejected" do
      g = Graph.new()
      :ok = Graph.add_dependency(g, :a, :b)
      assert {:error, :cycle} = Graph.add_dependency(g, :b, :a)
      Graph.delete(g)
    end
  end

  describe "Records" do
    test "record access via macro" do
      u = Records.new_user(1, "Ada", "ada@example.com")
      assert Records.user_id(u) == 1
      assert Records.is_user(u)
      assert elem(u, 0) == :user
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. Atom table exhaustion.** Never call `String.to_atom/1` or `:erlang.list_to_atom/1` on untrusted input. Atoms are permanent and capped. Use the `_to_existing_atom` variants.

**2. Charlist vs binary confusion.** `:inet_res`, `:inet`, `:gen_tcp` options differ in what they accept. Normalize at the wrapper boundary — don't leak the confusion to callers.

**3. Records are macros, not structs.** `Record.is_record/2` is guard-safe; `user(u, :id)` expands to `elem(u, 1)` at compile time. No key-based access. For new Elixir code prefer structs.

**4. `:digraph` owns ETS tables.** Not GC'd. Forgetting `:digraph.delete/1` leaks 3 ETS tables per graph. In long-lived processes wrap with `try/after`.

**5. Error shape inconsistency.** Bare `:error` vs `{:error, reason}` vs `{:empty, q}` varies per function. Your wrapper must normalize or callers will branch inconsistently.

**6. Iolists leak everywhere.** `:io_lib.format/2`, `:ssl` and `:gen_tcp` often return iolists. `String.length/1` on an iolist raises. Flatten with `IO.iodata_to_binary/1` before exposing.

**7. Dialyzer strictness.** Erlang specs use wide union types. Narrow them in your `@spec`s at the wrapper boundary if you want Dialyzer to catch misuse.

**8. When NOT to use this.** If an idiomatic Elixir wrapper exists (`Argon2`, `libgraph`, `:telemetry`) and is maintained, prefer it — better docs, typespecs, and community feedback. Wrap Erlang directly only when no Elixir equivalent exists or the wrapper adds measurable overhead.

---

## Performance notes

`:queue` is a persistent (immutable) banker's queue with amortized O(1) `in/2` and `out/1`. Never substitute `list ++ [x]` for FIFO — that's O(n).

```elixir
Benchee.run(%{
  "queue.in/2"   => fn q -> :queue.in(:x, q) end,
  "list ++ [x]"  => fn l -> l ++ [:x] end
}, inputs: %{"1k" => {Enum.to_list(1..1000) |> Enum.reduce(:queue.new(), &:queue.in/2),
                     Enum.to_list(1..1000)}})
```

On a 1,000-element FIFO, `list ++ [x]` is roughly 200× slower.

---

## Resources

- https://hexdocs.pm/elixir/Record.html — Record module reference
- https://www.erlang.org/doc/man/crypto.html — `:crypto` reference
- https://www.erlang.org/doc/man/queue.html — banker's queue internals
- https://www.erlang.org/doc/man/digraph.html — `:digraph`
- https://learnyousomeerlang.com/a-short-visit-to-common-data-structures — queue, digraph, gb_trees
- https://dashbit.co/blog/writing-assertive-code-with-elixir — defensive patterns at interop boundaries
- https://www.erlang.org/doc/apps/erts/absform.html — atom table limits
