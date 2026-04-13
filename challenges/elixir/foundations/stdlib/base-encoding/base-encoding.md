# Base16/32/64 encoding for stable cache keys

**Project**: `cache_key` — builds deterministic, filesystem-safe cache keys from arbitrary inputs.

---

## Project structure

```
cache_key/
├── lib/
│   └── cache_key.ex
├── test/
│   └── cache_key_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

---

## The business problem
You're writing the cache layer for an image-processing pipeline. A cache key must:

1. Be **deterministic** — same inputs always yield the same key.
2. Be **short and safe** — usable as a filename and as a Redis key.
3. Cover **arbitrary inputs** — maps, strings, tuples, regardless of key order.

Hashing gives us (1) and kind-of (2) — but the raw bytes from `:crypto.hash/2` contain
nulls and high-range bytes that aren't safe as filenames. That's why we need Base encoding.

Project structure:

```
cache_key/
├── lib/
│   └── cache_key.ex
├── test/
│   └── cache_key_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The `Base` module encodes binaries to ASCII

`Base.encode16/2` — hex, 2 chars per byte (doubles size). Great for debugging, readable.
`Base.encode32/2` — 5 bits per char, 8 chars per 5 bytes (1.6x). Case-insensitive.
`Base.encode64/2` — 6 bits per char, 4 chars per 3 bytes (1.33x). Most compact ASCII.

None of these are *compression* — they're the opposite, inflating bytes into a safe
alphabet. The point is safety in text-only contexts (URLs, filenames, JSON).

### 2. `encode64` vs `url_encode64`

Standard Base64 uses `+` and `/`. Both are problematic:

- `/` becomes a path separator in filenames and URLs.
- `+` decodes to a space in `application/x-www-form-urlencoded`.

`Base.url_encode64/2` swaps them for `-` and `_`. Always prefer it for keys that
end up in paths or URLs.

### 3. Padding (`:padding`)

Base64 pads with `=` to make the length a multiple of 4. For cache keys the `=`
adds nothing useful and some URL parsers misbehave with trailing `=`. Pass
`padding: false` to drop it.

### 4. Hash first, encode second

The hash compresses arbitrary-length input to a fixed digest. The encoding makes
that digest printable. Reverse the order and the result is huge and still unsafe.

---

## Why hash + Base and not `inspect/1` or `:erlang.phash2/1`

Using `inspect/1` as a cache key is the shortcut that always breaks: `inspect(%{a: 1, b: 2})` and `inspect(%{b: 2, a: 1})` may render the same pair in different orders on different OTP releases, so two nodes produce two keys for equivalent data and the cache silently misses. `:erlang.phash2/1` returns 27 bits — fine for sharding but a collision factory at million-entry scale, and collisions mean the wrong cached value returned, not a miss. Hashing with SHA-256 gives 256 bits of collision resistance and `term_to_binary([:deterministic])` gives a stable byte representation across nodes. Base encoding makes the raw digest safe for filenames and URLs without losing any of that resistance.

---

## Design decisions

**Option A — `:erlang.phash2(term)` stringified**
- Pros: one line; no deps beyond stdlib; very fast.
- Cons: 27 bits of entropy collides under million-entry caches; silently returns wrong values on hit.

**Option B — `inspect/1` or `Jason.encode!/1` as the key**
- Pros: human-readable; trivial to implement.
- Cons: not stable across releases/map orderings; unbounded length; unsafe in filenames (quotes, newlines).

**Option C — `deep_sort` → `term_to_binary([:deterministic])` → SHA-256 → `Base.url_encode64(padding: false)`** (chosen)
- Pros: cross-node stable (deterministic external term format); map-order independent; 256-bit collision resistance; 43-char fixed output safe in URLs and filenames; one knob for algorithm choice.
- Cons: keys are opaque (can't recover the input from the key); slower than `phash2` (microseconds rather than nanoseconds).

Chose **C** because caches live in shared storage (Redis, filesystem) where a collision or a character mismatch is a production incident. The microsecond cost is invisible next to any real cache read.

---

## Implementation

### `mix.exs`
```elixir
defmodule CacheKey.MixProject do
  use Mix.Project

  def project do
    [
      app: :cache_key,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Hash + Base encode: SHA-256 (256-bit collision resistance) + url_encode64 (URL-safe) produce stable cache keys.

```bash
mix new cache_key
cd cache_key
```

### `lib/cache_key.ex`

**Objective**: term_to_binary([:deterministic]) ensures same data = same bytes across nodes; inspect/1 fails on map ordering.

```elixir
defmodule CacheKey do
  @moduledoc """
  Builds deterministic, URL- and filesystem-safe cache keys from arbitrary terms.

  Strategy: canonicalize term -> hash -> base-encode.
  """

  @type encoding :: :hex | :base32 | :base64_url

  @doc """
  Builds a cache key from any Elixir term.

    * `:namespace` — optional prefix (e.g. "thumbnails:"), kept unencoded for readability
    * `:encoding`  — `:hex | :base32 | :base64_url`, default `:base64_url`
    * `:algorithm` — any supported `:crypto.hash/2` algo, default `:sha256`
  """
  @spec build(term(), keyword()) :: String.t()
  def build(term, opts \\ []) do
    namespace = Keyword.get(opts, :namespace, "")
    encoding = Keyword.get(opts, :encoding, :base64_url)
    algo = Keyword.get(opts, :algorithm, :sha256)

    digest =
      term
      |> canonicalize()
      |> then(&:crypto.hash(algo, &1))

    namespace <> encode(digest, encoding)
  end

  # --- canonicalization ------------------------------------------------------

  # We need the SAME bytes for %{a: 1, b: 2} and %{b: 2, a: 1}. Erlang term
  # external format is NOT stable across map iteration order, so :erlang.term_to_binary
  # alone would produce different digests for equivalent maps. We sort maps recursively
  # first so the digest is order-independent.
  defp canonicalize(term) do
    term
    |> deep_sort()
    # minor_version: 2 gives us the stable external term format.
    |> :erlang.term_to_binary([:deterministic])
  end

  defp deep_sort(term) when is_map(term) do
    term
    |> Enum.map(fn {k, v} -> {deep_sort(k), deep_sort(v)} end)
    |> Enum.sort()
  end

  defp deep_sort(term) when is_list(term), do: Enum.map(term, &deep_sort/1)
  defp deep_sort(term) when is_tuple(term), do: term |> Tuple.to_list() |> deep_sort() |> List.to_tuple()
  defp deep_sort(term), do: term

  # --- encoding dispatch -----------------------------------------------------

  # Base.encode16/2 returns uppercase hex by default. Lowercase is nicer in logs.
  defp encode(bin, :hex), do: Base.encode16(bin, case: :lower)

  # Base32 with padding: false so the key has no trailing "=" clutter.
  defp encode(bin, :base32), do: Base.encode32(bin, padding: false, case: :lower)

  # url_encode64 swaps + and / for - and _ — safe in URLs and filenames.
  defp encode(bin, :base64_url), do: Base.url_encode64(bin, padding: false)
end
```

### Step 3: `test/cache_key_test.exs`

**Objective**: Test map ordering independence, nested structures, key stability across restarts, no unsafe chars in output.

```elixir
defmodule CacheKeyTest do
  use ExUnit.Case, async: true
  doctest CacheKey

  describe "build/2 — determinism" do
    test "same input produces same key" do
      assert CacheKey.build("hello") == CacheKey.build("hello")
    end

    test "map key order does not affect the result" do
      a = CacheKey.build(%{a: 1, b: 2, c: 3})
      b = CacheKey.build(%{c: 3, a: 1, b: 2})
      assert a == b
    end

    test "different input produces different key" do
      refute CacheKey.build("hello") == CacheKey.build("world")
    end
  end

  describe "build/2 — encoding safety" do
    test "base64_url keys contain no + or / characters" do
      # These chars break filenames and URL paths.
      key = CacheKey.build(%{large: String.duplicate("x", 1000)})
      refute key =~ "+"
      refute key =~ "/"
      refute key =~ "="
    end

    test "hex encoding produces only 0-9 a-f" do
      key = CacheKey.build("thing", encoding: :hex)
      assert key =~ ~r/^[0-9a-f]+$/
    end

    test "namespace prefix is preserved unencoded" do
      key = CacheKey.build("thing", namespace: "thumb:")
      assert String.starts_with?(key, "thumb:")
    end
  end

  describe "build/2 — key length" do
    test "sha256 + base64_url produces 43-char keys (no padding)" do
      key = CacheKey.build("thing", encoding: :base64_url)
      # 32-byte sha256 -> ceil(32 * 8 / 6) = 43 chars without padding
      assert String.length(key) == 43
    end
  end
end
```

### Step 4: Run

**Objective**: --warnings-as-errors catches unused encoding options; test coverage validates key format never changes.

```bash
mix test
```

### Why this works

`deep_sort/1` recursively normalises maps to sorted key-value lists, so `%{a: 1, b: 2}` and `%{b: 2, a: 1}` reach `term_to_binary` with identical shapes. Passing `[:deterministic]` to `term_to_binary` stabilises the encoding of otherwise-internal representations (small vs big integers, atom indexes) across nodes. SHA-256 compresses arbitrary-length input to a fixed 32-byte digest with cryptographic collision resistance. `Base.url_encode64(padding: false)` produces a 43-char ASCII string using only `A–Z a–z 0–9 - _` — safe as a filename, safe in a URL path, safe in JSON, and free of padding `=` that trips some parsers.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== CacheKey: demo ===\n")

    result_1 = CacheKey.build("hello")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Mix.env()
    IO.puts("Demo 2: #{inspect(result_2)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating module concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    term = %{user_id: 42, filters: [%{field: :status, op: :eq, value: :active}], page: 1}

    {us, _} =
      :timer.tc(fn ->
        Enum.each(1..100_000, fn _ -> CacheKey.build(term) end)
      end)

    IO.puts("build/1 x100k: #{us} µs (#{us / 100_000} µs/call)")
  end
end

Bench.run()
```

Target: under 10 µs per call for a small map. SHA-256 dominates; `:blake2b` is roughly 2x faster if you need more throughput and don't require SHA-256 specifically.

---

## Trade-offs and production gotchas

**1. Why `:deterministic` matters in `term_to_binary`**
Without it, Erlang can serialize the same map in different byte orders depending on
internal hash-map structure. Two nodes in a cluster might produce different keys for
the same data — a silent cache miss you won't catch in local tests.

**2. Hash choice**
`:sha256` is a reasonable default. For cache keys where you just need collision
resistance (not cryptographic integrity), `:crypto.hash(:blake2b, ...)` is faster.
`:md5` is fine for non-adversarial cache keys but avoid it for anything user-visible —
someone will ask "is MD5 secure?" and the answer invites a conversation you don't need.

**3. Why not just `:erlang.phash2/1`?**
`phash2` returns 27 bits. For a cache with millions of entries you'll hit collisions
that silently return the wrong cached value. Use it for sharding, not for keys.

**4. Never decode cache keys**
They're one-way. If you find yourself wanting to recover the original input from the
key, store the input alongside the value instead.

**5. When NOT to use base64**
If the key ends up in a case-insensitive context (DNS, some Windows filesystems),
Base64 collides (`A` vs `a`). Use Base32 there — it's case-insensitive by design.

---

## Reflection

1. Cache keys are SHA-256 (32 bytes) encoded to 43 chars. For a Redis cache holding 100M entries, that's roughly 4.3 GB just for keys. Would you shorten the digest (truncate to 128 bits), switch to Base32 with fewer bits, or push the problem to Redis with a hash-tag scheme? At what scale does key length stop being a rounding error?
2. Two services compute keys for the same term and must agree. One runs OTP 26 on Linux, the other OTP 27 on macOS. The `[:deterministic]` flag guarantees stability, but an engineer proposes replacing `term_to_binary` with `Jason.encode!/1` for "portability with non-Erlang services". What do you gain, what do you lose, and how do you run the migration without breaking every in-flight cache entry?

---

## Resources

- [`Base` — Elixir stdlib](https://hexdocs.pm/elixir/Base.html)
- [RFC 4648 — Base encoding standards](https://datatracker.ietf.org/doc/html/rfc4648)
- [`:erlang.term_to_binary/2` with `:deterministic`](https://www.erlang.org/doc/man/erlang.html#term_to_binary-2) — added in OTP 22

---

## Why Base16/32/64 encoding for stable cache keys matters

Mastering **Base16/32/64 encoding for stable cache keys** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/cache_key_test.exs`

```elixir
defmodule CacheKeyTest do
  use ExUnit.Case, async: true

  doctest CacheKey

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CacheKey.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. `Base.encode64/1` and `Base.url_encode64/1`
Standard Base64 includes `+` and `/`, sometimes with padding. URL-safe Base64 replaces these and omits padding. Use URL-safe for URLs and JSON.

### 2. Hex Encoding
`Base.encode16` represents bytes as pairs of hex digits. Less dense than Base64 but more human-readable for small data.

### 3. When to Use What
Base64: maximum density. Hex: readability. URL-safe Base64: URLs and JSON.

---
