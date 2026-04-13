# Port Drivers for Long-Running External Processes

**Project**: `market_tape` — ingest a high-volume binary market data feed from a native C decoder that runs continuously and streams parsed quotes into BEAM processes.

## Project context

A trading platform receives a 400Mbps UDP multicast feed of exchange messages in a proprietary
binary format. A vendor-provided C decoder handles the protocol — the Elixir side must keep
a continuously-running decoder process alive, push it raw bytes, and receive decoded quote
structs. The decoder runs for days at a time.

A **port driver** (linked-in driver) is the right tool: the driver code runs inside the BEAM
OS process (unlike a Port, which is a separate OS process), exposes a socket-like interface
to Elixir, and can push messages asynchronously to the owning process. Compared to a NIF,
the driver is inherently long-running, has an `output` callback that does not block schedulers
(it queues), and supports sync/async without the 1ms scheduler budget.

```
market_tape/
├── lib/
│   └── market_tape/
│       ├── application.ex
│       └── decoder_port.ex
├── c_src/
│   └── feed_driver.c
├── priv/                          # compiled .so lands here
├── Makefile
├── test/market_tape/decoder_port_test.exs
├── bench/decoder_bench.exs
└── mix.exs
```

## Why a port driver and not a NIF or a plain Port

| Aspect | Port driver | NIF | Port (`Port.open`) |
|---|---|---|---|
| Address space | inside BEAM | inside BEAM | separate OS process |
| Blocking tolerated | yes (driver threads) | no (1ms budget) | yes |
| Crash impact | kills BEAM | kills BEAM | crash isolated |
| Async to Elixir | native (`driver_output`) | cumbersome | via messages |
| Complexity | highest | medium | lowest |

The vendor decoder expects to be fed a continuous byte stream and to emit a continuous
quote stream — neither a NIF nor a plain Port expresses that cleanly. A driver's `output`
callback accepts bytes on demand and a background driver thread produces messages at its
own rate.

## Why a driver and not a C node

A C node speaks the Erlang distribution protocol and lives in a separate OS process.
It is safer (crashes isolated) but orders of magnitude slower for small messages — each
message traverses a TCP socket and the distribution encoding. At 400Mbps with 64-byte
messages (6.2M msgs/sec), only in-process data transfer keeps up. Drivers pay zero
serialization cost.

## Core concepts

### 1. The driver entry table

`ErlDrvEntry` is a struct of function pointers: `start`, `stop`, `output`, `ready_input`,
`control`. The BEAM resolves these at load time. The name in the `.driver_name` field is
what `Port.open({:spawn_driver, "name"})` uses.

### 2. Driver callbacks

- **`start(port, cmd)`** — allocate per-port state. Returns an opaque pointer kept alive
  across calls.
- **`output(drv_data, buf, len)`** — called when Elixir sends bytes (`Port.command/2`).
  Runs on a scheduler thread; must be fast. Queue the work and return.
- **`ready_input(drv_data, event)`** — called when a registered fd is readable. This is
  how you do async I/O without blocking.
- **`stop(drv_data)`** — free per-port state. Called when Port is closed or owner dies.

### 3. `driver_output` for async messages to Elixir

From any driver thread, `driver_output(port, buf, len)` sends a binary to the controlling
process. This is the backbone of streaming quotes out of the driver.

### 4. Driver linked-in lifecycle

`driver_init` — dlopen of the .so — happens once. `start` fires per port. If the owning
Elixir process dies, `stop` runs; allocated memory must be freed there.

## Design decisions

- **Option A — synchronous request/response via `port_control`**: Elixir calls `:erlang.port_control/3`
  with a cmd byte, driver processes it, returns a binary reply. Pros: straightforward.
  Cons: serialized, no streaming.
- **Option B — streaming via `output` + `driver_output`**: Elixir pushes bytes; driver pushes
  quotes back as they are decoded. Pros: matches the problem shape. Cons: more complex.

→ We use **Option B** because the workload is inherently streamed. We retain `control` for
out-of-band commands (start/stop/stats).

- **Option A — own thread per port**: clean isolation; scales poorly with many ports.
- **Option B — shared pool of driver threads**: harder to implement; scales well.

→ With one port per feed (a handful total), **Option A** is correct.

## Implementation

### Dependencies (`mix.exs`)

No Elixir deps; the Makefile handles C compilation.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule MarketTape.MixProject do
  use Mix.Project

  def project do
    [
      app: :market_tape,
      version: "0.1.0",
      elixir: "~> 1.17",
      compilers: [:elixir_make] ++ Mix.compilers(),
      make_makefile: "Makefile",
      make_clean: ["clean"],
      deps: [
        {:elixir_make, "~> 0.8", runtime: false},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {MarketTape.Application, []}]
end
```

### Step 1: `Makefile`

**Objective**: Query running BEAM for erl_interface and ERTS paths so compiled driver matches the exact runtime ABI.

```make
ERL_EI_INCLUDE := $(shell erl -eval 'io:format("~s", [code:lib_dir(erl_interface, include)])' -s init stop -noshell)
ERTS_INCLUDE   := $(shell erl -eval 'io:format("~s/erts-~s/include", [code:root_dir(), erlang:system_info(version)])' -s init stop -noshell)

CFLAGS  := -O3 -fPIC -Wall -std=c11 -I$(ERTS_INCLUDE) -I$(ERL_EI_INCLUDE)
LDFLAGS := -shared

PRIV_DIR := priv
TARGET   := $(PRIV_DIR)/feed_driver.so
SRC      := c_src/feed_driver.c

all: $(TARGET)

$(TARGET): $(SRC)
	mkdir -p $(PRIV_DIR)
	$(CC) $(CFLAGS) $(LDFLAGS) $< -o $@

clean:
	rm -rf $(PRIV_DIR)
```

### Step 2: The driver in C (`c_src/feed_driver.c`)

**Objective**: Accumulate partial bytes across driver_output calls so unaligned feed chunks never lose frames.

```c
#include "erl_driver.h"
#include <string.h>
#include <stdint.h>
#include <stdlib.h>

/*
 * Minimal streaming driver.
 * Elixir pushes raw feed bytes with Port.command/2.
 * Driver emits one 24-byte "quote" per parsed message:
 *     <<symbol:64, price:64/float, ts:64/unsigned>>.
 * The real decoder parses the vendor's binary format — here we
 * emit a synthetic quote for every 8 input bytes so tests are deterministic.
 */

typedef struct {
    ErlDrvPort port;
    uint64_t   messages_emitted;
    uint8_t    carry[8];
    size_t     carry_len;
} feed_state_t;

static ErlDrvData feed_start(ErlDrvPort port, char *cmd) {
    (void)cmd;
    feed_state_t *st = driver_alloc(sizeof(*st));
    if (!st) return ERL_DRV_ERROR_GENERAL;
    st->port = port;
    st->messages_emitted = 0;
    st->carry_len = 0;
    return (ErlDrvData)st;
}

static void feed_stop(ErlDrvData drv_data) {
    driver_free(drv_data);
}

static void emit_quote(feed_state_t *st, const uint8_t *chunk8) {
    uint8_t out[24];
    memcpy(out,      chunk8, 8);            // symbol
    double price = (double)st->messages_emitted;
    memcpy(out + 8,  &price, 8);            // price
    uint64_t ts = st->messages_emitted;
    memcpy(out + 16, &ts, 8);               // ts
    driver_output(st->port, (char *)out, sizeof(out));
    st->messages_emitted++;
}

static void feed_output(ErlDrvData drv_data, char *buf, ErlDrvSizeT len) {
    feed_state_t *st = (feed_state_t *)drv_data;
    const uint8_t *p = (const uint8_t *)buf;
    size_t remaining = len;

    /* Drain carry if we have partial bytes from last call. */
    if (st->carry_len > 0) {
        size_t take = 8 - st->carry_len;
        if (take > remaining) take = remaining;
        memcpy(st->carry + st->carry_len, p, take);
        st->carry_len += take;
        p += take; remaining -= take;
        if (st->carry_len == 8) {
            emit_quote(st, st->carry);
            st->carry_len = 0;
        }
    }

    while (remaining >= 8) {
        emit_quote(st, p);
        p += 8; remaining -= 8;
    }

    if (remaining > 0) {
        memcpy(st->carry, p, remaining);
        st->carry_len = remaining;
    }
}

/* Out-of-band command: byte 1 = reset counter, byte 2 = read counter. */
static ErlDrvSSizeT feed_control(ErlDrvData drv_data, unsigned int cmd,
                                  char *buf, ErlDrvSizeT len,
                                  char **rbuf, ErlDrvSizeT rlen) {
    (void)buf; (void)len;
    feed_state_t *st = (feed_state_t *)drv_data;
    if (cmd == 1) {
        st->messages_emitted = 0;
        (*rbuf)[0] = 0;
        return 1;
    }
    if (cmd == 2) {
        if (rlen < 8) *rbuf = driver_alloc(8);
        memcpy(*rbuf, &st->messages_emitted, 8);
        return 8;
    }
    return 0;
}

static ErlDrvEntry feed_driver_entry = {
    NULL,              /* init */
    feed_start,
    feed_stop,
    feed_output,
    NULL,              /* ready_input */
    NULL,              /* ready_output */
    "feed_driver",
    NULL,              /* finish */
    NULL,              /* handle */
    feed_control,
    NULL,              /* timeout */
    NULL,              /* outputv */
    NULL,              /* ready_async */
    NULL,              /* flush */
    NULL,              /* call */
    NULL,              /* event */
    ERL_DRV_EXTENDED_MARKER,
    ERL_DRV_EXTENDED_MAJOR_VERSION,
    ERL_DRV_EXTENDED_MINOR_VERSION,
    ERL_DRV_FLAG_USE_PORT_LOCKING,
    NULL,              /* handle2 */
    NULL,              /* process_exit */
    NULL               /* stop_select */
};

DRIVER_INIT(feed_driver) {
    return &feed_driver_entry;
}
```

### Step 3: Elixir wrapper (`lib/market_tape/decoder_port.ex`)

**Objective**: Use port_control/3 for side-channel ops so hot-path Port.command never waits for synchronous replies.

```elixir
defmodule MarketTape.DecoderPort do
  @moduledoc """
  Owns one port bound to the feed_driver linked-in driver.

  Lifecycle:
    - `start_link/1` loads the driver .so and opens a port.
    - Incoming binaries (24 bytes each) are decoded into %Quote{} and dispatched.
    - When this GenServer dies, the port is closed automatically and the
      driver's `stop` callback runs, freeing per-port state.
  """
  use GenServer
  require Logger

  @driver :feed_driver
  @reset_cmd 1
  @count_cmd 2

  defmodule Quote do
    defstruct [:symbol, :price, :timestamp]
  end

  # ------------------------------------------------------------------ Public

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Push raw feed bytes into the driver."
  def push(bytes) when is_binary(bytes), do: GenServer.cast(__MODULE__, {:push, bytes})

  @doc "Read the driver-side emitted-quotes counter."
  def count, do: GenServer.call(__MODULE__, :count)

  @doc "Reset the emitted-quotes counter to 0."
  def reset, do: GenServer.call(__MODULE__, :reset)

  # ---------------------------------------------------------------- Callbacks

  @impl true
  def init(opts) do
    subscriber = Keyword.fetch!(opts, :subscriber)
    :ok = load_driver()
    port = Port.open({:spawn_driver, Atom.to_string(@driver)}, [:binary])
    {:ok, %{port: port, subscriber: subscriber}}
  end

  @impl true
  def handle_cast({:push, bytes}, state) do
    true = Port.command(state.port, bytes)
    {:noreply, state}
  end

  @impl true
  def handle_call(:count, _from, state) do
    <<n::little-64>> = :erlang.port_control(state.port, @count_cmd, <<>>)
    {:reply, n, state}
  end

  def handle_call(:reset, _from, state) do
    <<_::8>> = :erlang.port_control(state.port, @reset_cmd, <<>>)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info({port, {:data, data}}, %{port: port} = state) do
    decode_stream(data, state.subscriber)
    {:noreply, state}
  end

  def handle_info({port, {:exit_status, status}}, %{port: port} = state) do
    Logger.error("feed_driver port exited: #{status}")
    {:stop, :driver_exit, state}
  end

  # ----------------------------------------------------------------- Helpers

  defp load_driver do
    priv = :code.priv_dir(:market_tape) |> List.to_string()
    case :erl_ddll.load_driver(priv, @driver) do
      :ok -> :ok
      {:error, :already_loaded} -> :ok
      other -> raise "driver load failed: #{inspect(other)}"
    end
  end

  defp decode_stream(<<sym::binary-8, price::float-little-64, ts::little-64, rest::binary>>, sub) do
    send(sub, {:quote, %Quote{symbol: sym, price: price, timestamp: ts}})
    decode_stream(rest, sub)
  end
  defp decode_stream(<<>>, _sub), do: :ok
  defp decode_stream(_partial, _sub), do: :ok
end
```

### Step 4: Application supervision

**Objective**: Supervise port owner so driver crash triggers stop/cleanup and per-port state rebuilds on restart.

```elixir
defmodule MarketTape.Application do
  use Application

  @impl true
  def start(_, _) do
    children = [
      {MarketTape.DecoderPort, subscriber: self()}
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: MarketTape.Supervisor)
  end
end
```

## Why this works

```
Elixir                  BEAM driver boundary                 driver code
----------              --------------------                ---------------
Port.command(port,     ───────output()──────▶   feed_output()  (zero-copy, scheduler thread)
             bytes)
                                                    │
                                                    ├── parses 8-byte chunks
                                                    │
                                                    ▼
GenServer.handle_info  ◀────driver_output()────  emit_quote()   (back to owner as {port,{:data,_}})
```

- `Port.command` is async from Elixir's side; it enqueues bytes into the driver's
  input queue and returns immediately.
- `driver_output` sends a binary message to the controlling process without any
  allocation on the BEAM side beyond the binary itself (refc-shared).
- The driver state is per-port; if the controlling process dies, `stop` frees state
  — no leaks. Locking is per-port (`ERL_DRV_FLAG_USE_PORT_LOCKING`), so multiple
  ports process in parallel without contention.

## Tests (`test/market_tape/decoder_port_test.exs`)

```elixir
defmodule MarketTape.DecoderPortTest do
  use ExUnit.Case, async: false
  alias MarketTape.{DecoderPort, DecoderPort.Quote}

  setup do
    # Start one port per test to get isolated state.
    {:ok, _pid} = start_supervised({DecoderPort, subscriber: self()})
    :ok = DecoderPort.reset()
    :ok
  end

  describe "streaming decode" do
    test "one 8-byte chunk produces one quote" do
      DecoderPort.push(<<"AAPL0001">>)
      assert_receive {:quote, %Quote{symbol: "AAPL0001"}}, 500
    end

    test "partial chunks are buffered across pushes" do
      DecoderPort.push(<<"AAPL">>)
      DecoderPort.push(<<"0001">>)
      assert_receive {:quote, %Quote{symbol: "AAPL0001"}}, 500
    end

    test "bulk push produces N quotes" do
      bulk = for i <- 1..100, into: <<>>, do: <<"SY", i::16, "abcd"::binary-4>>
      DecoderPort.push(bulk)
      for _ <- 1..100, do: assert_receive({:quote, %Quote{}}, 500)
    end
  end

  describe "port_control commands" do
    test "count reflects emitted messages" do
      DecoderPort.push(<<"12345678">>)
      DecoderPort.push(<<"87654321">>)
      # Give driver a moment to process.
      Process.sleep(20)
      assert DecoderPort.count() == 2
    end

    test "reset clears the counter" do
      DecoderPort.push(<<"12345678">>)
      Process.sleep(20)
      assert DecoderPort.count() == 1
      DecoderPort.reset()
      assert DecoderPort.count() == 0
    end
  end
end
```

## Benchmark (`bench/decoder_bench.exs`)

```elixir
{:ok, _} = MarketTape.DecoderPort.start_link(subscriber: self())

payload_1mb = :crypto.strong_rand_bytes(1_048_576)

Benchee.run(
  %{
    "push 1MB feed" => fn ->
      MarketTape.DecoderPort.push(payload_1mb)
      # Drain roughly 131k expected messages
      for _ <- 1..131_072 do
        receive do {:quote, _} -> :ok after 5_000 -> exit(:timeout) end
      end
    end
  },
  time: 10, warmup: 3
)
```

**Expected**: > 1GB/sec decoded throughput on a modern CPU (the decoder is trivial here; a
real vendor decoder might cut that to 200MB/sec which is still well above a 400Mbps feed).

## Advanced Considerations: NIF Isolation and Scheduler Integration

NIF calls run atomically on a scheduler thread, blocking all other processes on that scheduler until the function returns. For operations exceeding ~1 millisecond, this starvation becomes visible: heartbeat processes delay, ETS owner replies hang, supervision timeouts fire. The BEAM's dirty scheduler pool (8 CPU + 10 IO by default) isolates long NIFs from the main scheduler ring, but they're still a finite resource.

Understanding scheduler capacity is critical. Each dirty CPU scheduler can run ~1,000 100-microsecond operations per second, or ~5 100-millisecond operations. Beyond that, callers queue. A GenServer pool capping concurrency and applying backpressure prevents cascade failures: if the dirty pool saturates, reject new work immediately instead of queuing unboundedly.

Resource management inside NIFs differs from pure Elixir. A `Binary<'a>` is a borrow tied to the NIF call; it cannot escape to threads or be stored in resources. An `OwnedBinary` allocation isn't visible to BEAM's garbage collector, so memory limits must be enforced in the Elixir layer. Hybrid architectures (Port processes for I/O, NIFs for CPU work) offer better observability and failure isolation than trying to do everything in a single NIF crate.

---


## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. A driver panic kills the VM.** Port drivers run in-process. `abort()` in C, a null deref,
or a double-free takes the whole node with it. Only use drivers for vendor code you trust.

**2. The controlling process mailbox can balloon.** `driver_output` pushes one message per
quote. If the owning GenServer is slow, 6M msgs/sec fills a mailbox in seconds and the
scheduler eats itself on mailbox scans. Aggregate in the driver or use a bounded process.

**3. `:erl_ddll.load_driver` without `unload_driver` leaks on code reload.** In release
upgrades, old driver versions stay resident. Track loaded drivers and unload explicitly.

**4. No scheduler budget, but no preemption either.** A driver callback that loops forever
pegs a scheduler until it returns. Split long work across callbacks — let the scheduler
run other ports.

**5. Cross-platform compilation.** The `.so` name, extension, and ABI differ per platform
(`.dylib` on macOS, `.dll` on Windows). `:erl_ddll` handles naming, but your Makefile
must emit the right file per target.

**6. When NOT to use a driver.** If crash isolation matters at all (third-party code,
untrusted binaries), use a `Port` so the OS isolates. Drivers are the right answer only
when throughput demands in-process data flow.

## Reflection

The test-time `subscriber: self()` pattern works for one consumer. In production you
likely fan out to N subscribers (order book builders, persistence, analytics). Would you
do the fan-out in the driver (extra `driver_output` calls per subscriber) or in Elixir
(one receive, dispatch)? Reason about the mailbox vs. native memory trade-off.

## Resources

- [`erl_driver` — Erlang/OTP man page](https://www.erlang.org/doc/man/erl_driver.html)
- [`erl_ddll` — dynamic driver loader](https://www.erlang.org/doc/man/erl_ddll.html)
- [Inside port drivers — Happi Hacking](https://happi.github.io/blog/)
- [elixir_make — make integration for Mix](https://github.com/elixir-lang/elixir_make)
