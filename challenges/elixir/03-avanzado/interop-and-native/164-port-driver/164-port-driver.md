# Port Drivers vs Ports — When C Meets BEAM Without a Pipe

**Project**: `port_driver_demo` — a comparative study: the same "uppercase this string" operation implemented as a Port (external OS process), a linked-in Port Driver (C shared library), and a NIF, with benchmarks showing the trade-offs.

**Difficulty**: ★★★★☆

**Estimated time**: 3–6 hours

---

## Project context

When people say "Port" in Elixir they usually mean `Port.open({:spawn_executable, ...})` — fork, exec, communicate via stdin/stdout. There's a second, almost-forgotten flavor: the **Port Driver** (also called "linked-in driver"). A port driver is a C shared library loaded into the BEAM process that exposes callbacks the BEAM calls directly — no OS process, no pipe, no serialization overhead, but it runs in-process like a NIF.

In practice, port drivers are a legacy mechanism. The OTP team recommends **NIFs over port drivers** for new code: NIFs have a saner API, dirty scheduler support, and less boilerplate. Port drivers still appear in old systems (`:ssl`, parts of `:crypto`, `:os_mon` for historical reasons), and understanding them clarifies why NIFs look the way they do.

This exercise builds three versions of the same "uppercase" function:
1. An **OS process Port** (`tr a-z A-Z`) via `Port.open`.
2. A **linked-in Port Driver** written in C.
3. A **NIF** (for contrast).

You'll measure each and see why the industry moved to NIFs.

```
port_driver_demo/
├── c_src/
│   ├── upcase_drv.c        # Port driver source
│   └── upcase_nif.c        # NIF source
├── lib/port_driver_demo/
│   ├── via_port.ex         # :os.cmd / Port.open wrapper
│   ├── via_driver.ex       # port-driver wrapper
│   └── via_nif.ex          # NIF wrapper
├── test/
│   └── port_driver_demo_test.exs
├── Makefile
└── mix.exs
```

---

## Core concepts

### 1. The three mechanisms at a glance

```
Port (OS process)       Port Driver (C dylib)       NIF (C or Rust)
─────────────────       ─────────────────────       ───────────────
fork + exec             erl_ddll:load + open_port   :erlang.load_nif
pipe IPC                direct function calls       direct function calls
separate address space  shared address space        shared address space
cannot segfault BEAM    CAN segfault BEAM           CAN segfault BEAM
1–10 ms startup         1 ms driver load            1 ms NIF load
~0.3 ms per call        ~0.5 µs per call            ~0.1 µs per call
```

Port Drivers and NIFs are both "native code in BEAM", but NIFs have a vastly better API: direct function calls with term arguments, no message-based control protocol.

### 2. Port driver API surface

A driver implements a `ErlDrvEntry` struct with callbacks:

| Callback | Called when |
|---|---|
| `init` | driver is loaded |
| `start` | `Port.open` creates a port for this driver |
| `stop` | port is closed |
| `output` | Elixir sends data via `Port.command/2` |
| `control` | synchronous `port_control/3` call |
| `ready_input/output` | I/O readiness (for sockets etc.) |

The driver gets preemptive control via `driver_async` (spawns a worker thread) or `erl_drv_consume_timeslice` (hints BEAM to yield).

### 3. Why Port Drivers lost to NIFs

- **No return values**. You send bytes, the driver sends bytes back in a message. Everything is async request/response — harder than a direct function call.
- **No dirty scheduler integration** (not quite — `driver_async` exists but is clumsier than `DirtyCpu`).
- **`port_control/3`** is the "synchronous" escape hatch but still uses the ErlDrvBinary protocol.
- **Term manipulation** requires `ei` library (Erlang interface) — much heavier than NIF's `enif_*`.

### 4. When Port Drivers still win

- **File-descriptor-driven I/O** where you want BEAM's select loop to watch an fd (`ready_input` callback). NIFs can't do this; you'd need a thread polling.
- **Legacy OTP libraries** where the driver already exists and works fine.
- **Global singletons** with driver-level init (rare).

For 99% of new native code — use a NIF.

### 5. Loading a port driver

```elixir
:ok = :erl_ddll.load_driver(~c"./priv", ~c"upcase_drv")
port = Port.open({:spawn_driver, ~c"upcase_drv"}, [:binary])
Port.command(port, "hello")
receive do {^port, {:data, data}} -> data end
```

Note: charlists, not strings — the driver API predates BEAM's binary-first era.

### 6. Segfault equivalence

Both NIFs and Port Drivers run in-process — a segfault in either crashes the BEAM VM. The upside over OS-process Ports is speed; the downside is you lose BEAM's famed crash isolation. "Let it crash" only works if the crash is supervisable — a segfault isn't.

---

## Implementation

### Step 1: `Makefile`

```make
ERTS_INCLUDE := $(shell erl -eval 'io:format("~s", [code:root_dir() ++ "/erts-" ++ erlang:system_info(version) ++ "/include"])' -s init stop -noshell)
ERL_INTERFACE_DIR := $(shell erl -eval 'io:format("~s", [filename:join([code:root_dir(), "lib", "erl_interface-"] ++ [element(1, string:to_integer(erlang:system_info(version)))])])' -s init stop -noshell)

CFLAGS := -O2 -fPIC -I$(ERTS_INCLUDE) -Wall

priv/upcase_drv.so: c_src/upcase_drv.c
	mkdir -p priv
	$(CC) $(CFLAGS) -shared -o $@ $<

priv/upcase_nif.so: c_src/upcase_nif.c
	mkdir -p priv
	$(CC) $(CFLAGS) -shared -o $@ $<

all: priv/upcase_drv.so priv/upcase_nif.so
```

### Step 2: `c_src/upcase_drv.c` — minimal port driver

```c
#include <string.h>
#include <ctype.h>
#include <erl_driver.h>

typedef struct { ErlDrvPort port; } upcase_data;

static ErlDrvData drv_start(ErlDrvPort port, char *buff) {
    (void)buff;
    upcase_data *d = (upcase_data*) driver_alloc(sizeof(upcase_data));
    d->port = port;
    return (ErlDrvData) d;
}

static void drv_stop(ErlDrvData handle) {
    driver_free((char*) handle);
}

static void drv_output(ErlDrvData handle, char *buf, ErlDrvSizeT len) {
    upcase_data *d = (upcase_data*) handle;
    char *out = driver_alloc(len);
    for (ErlDrvSizeT i = 0; i < len; i++) out[i] = (char) toupper((unsigned char) buf[i]);
    driver_output(d->port, out, len);
    driver_free(out);
}

static ErlDrvEntry upcase_driver_entry = {
    NULL, drv_start, drv_stop, drv_output,
    NULL, NULL, "upcase_drv",
    NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    ERL_DRV_EXTENDED_MARKER,
    ERL_DRV_EXTENDED_MAJOR_VERSION,
    ERL_DRV_EXTENDED_MINOR_VERSION,
    0, NULL, NULL, NULL
};

DRIVER_INIT(upcase_drv) { return &upcase_driver_entry; }
```

### Step 3: `c_src/upcase_nif.c` — C NIF for comparison

```c
#include <string.h>
#include <ctype.h>
#include <erl_nif.h>

static ERL_NIF_TERM upcase(ErlNifEnv* env, int argc, const ERL_NIF_TERM argv[]) {
    ErlNifBinary in;
    if (argc != 1 || !enif_inspect_binary(env, argv[0], &in))
        return enif_make_badarg(env);

    ErlNifBinary out;
    enif_alloc_binary(in.size, &out);
    for (size_t i = 0; i < in.size; i++)
        out.data[i] = (unsigned char) toupper(in.data[i]);

    return enif_make_binary(env, &out);
}

static ErlNifFunc funcs[] = { {"upcase", 1, upcase} };
ERL_NIF_INIT(Elixir.PortDriverDemo.ViaNif, funcs, NULL, NULL, NULL, NULL)
```

### Step 4: `lib/port_driver_demo/via_port.ex`

```elixir
defmodule PortDriverDemo.ViaPort do
  @moduledoc "Uppercase via external `tr` process."

  @spec upcase(binary()) :: binary()
  def upcase(data) when is_binary(data) do
    port = Port.open({:spawn_executable, System.find_executable("tr")},
                     [:binary, :exit_status, {:args, ["a-z", "A-Z"]}])
    Port.command(port, data)
    Port.close(port)
    collect(port, [])
  end

  defp collect(port, acc) do
    receive do
      {^port, {:data, chunk}} -> collect(port, [acc, chunk])
      {^port, {:exit_status, _}} -> IO.iodata_to_binary(acc)
    after
      1_000 -> IO.iodata_to_binary(acc)
    end
  end
end
```

### Step 5: `lib/port_driver_demo/via_driver.ex`

```elixir
defmodule PortDriverDemo.ViaDriver do
  @moduledoc "Uppercase via linked-in port driver."

  @priv_path Path.expand("../../priv", __DIR__)

  @spec upcase(binary()) :: binary()
  def upcase(data) when is_binary(data) do
    :ok = ensure_loaded()
    port = Port.open({:spawn_driver, ~c"upcase_drv"}, [:binary])
    Port.command(port, data)

    result =
      receive do
        {^port, {:data, d}} -> d
      after
        1_000 -> raise "driver timeout"
      end

    Port.close(port)
    result
  end

  defp ensure_loaded do
    case :erl_ddll.load_driver(String.to_charlist(@priv_path), ~c"upcase_drv") do
      :ok -> :ok
      {:error, :already_loaded} -> :ok
      {:error, reason} -> raise "driver load: #{inspect(reason)}"
    end
  end
end
```

### Step 6: `lib/port_driver_demo/via_nif.ex`

```elixir
defmodule PortDriverDemo.ViaNif do
  @on_load :load_nif

  def load_nif do
    path = Path.join(:code.priv_dir(:port_driver_demo), ~c"upcase_nif")
    :erlang.load_nif(path, 0)
  end

  @spec upcase(binary()) :: binary()
  def upcase(_data), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 7: `mix.exs`

```elixir
defmodule PortDriverDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :port_driver_demo,
      version: "0.1.0",
      elixir: "~> 1.15",
      compilers: [:elixir_make | Mix.compilers()],
      make_targets: ["all"],
      make_clean: ["clean"],
      deps: [{:elixir_make, "~> 0.8", runtime: false}, {:benchee, "~> 1.3", only: :dev}]
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 8: `test/port_driver_demo_test.exs`

```elixir
defmodule PortDriverDemoTest do
  use ExUnit.Case, async: false

  alias PortDriverDemo.{ViaPort, ViaDriver, ViaNif}

  @data "hello world"

  test "all three produce the same output" do
    assert ViaPort.upcase(@data)   == "HELLO WORLD"
    assert ViaDriver.upcase(@data) == "HELLO WORLD"
    assert ViaNif.upcase(@data)    == "HELLO WORLD"
  end

  test "empty input" do
    assert ViaNif.upcase("") == ""
    assert ViaDriver.upcase("") == ""
  end

  test "unicode bytes untouched by ASCII-only upcase" do
    # We upcase ASCII only — multibyte UTF-8 bytes stay identical.
    bin = "héllo"
    assert byte_size(ViaNif.upcase(bin)) == byte_size(bin)
  end
end
```

---

## Trade-offs and production gotchas

**1. Port driver = legacy.** The API is clunky (callback tables, `ErlDrvEntry` with ~20 nullable fields). OTP recommends NIFs for new code. Use drivers only when you need fd-watching via `ready_input`.

**2. `erl_ddll` load race.** Two processes calling `load_driver` concurrently race. Wrap loads in a GenServer or use `:code.ensure_loaded/1` patterns.

**3. No typespecs for driver data.** Everything is raw bytes — there's no term-level argument passing. You must serialize your own framing (length prefix, etc.).

**4. Driver and NIF both crash BEAM on fault.** No BEAM isolation. Null-deref → SIGSEGV → whole VM down. Port (OS process) is the only truly isolated option.

**5. `driver_async`.** The driver's "dirty scheduler" equivalent. Spawns a thread from a thread pool. Interface is uglier than NIF's `DirtyCpu`.

**6. `.so`/`.dylib` path in releases.** `:code.priv_dir/1` returns the wrong thing under some Mix release configurations. Use `Application.app_dir(:my_app, "priv")` in releases.

**7. Lifecycle bugs.** If you `Port.open` a driver port and forget `Port.close/1`, the driver's `start` state leaks. Under heavy use this grows `driver_alloc` memory unboundedly.

**8. When NOT to use this.** Any new code — use a NIF. Anything that can tolerate a 0.3 ms round-trip — use a Port. Anything with BEAM isolation requirements — always Port.

---

## Benchmark

```elixir
data = :crypto.strong_rand_bytes(1_024)

Benchee.run(%{
  "via Port (OS process)" => fn -> ViaPort.upcase(data) end,
  "via Port Driver"       => fn -> ViaDriver.upcase(data) end,
  "via NIF"               => fn -> ViaNif.upcase(data) end
})
```

Typical results on 1 KB input:

```
via NIF:         ~0.5 µs
via Port Driver: ~3   µs
via Port:        ~3 000 µs (3 ms — fork+exec dominates)
```

6000× difference between Port and NIF. That's why NIFs exist.

---

## Resources

- https://www.erlang.org/doc/man/erl_driver.html — Port Driver C API
- https://www.erlang.org/doc/tutorial/c_portdriver.html — Port Driver tutorial
- https://www.erlang.org/doc/man/erl_nif.html — NIF C API
- https://www.erlang.org/doc/design_principles/erl_interface.html — `ei` vs `enif`
- https://ferd.ca/a-guide-to-tracing-in-elixir.html — internals perspective
- https://github.com/erlang/otp/tree/master/erts/emulator/drivers — real OTP driver source
