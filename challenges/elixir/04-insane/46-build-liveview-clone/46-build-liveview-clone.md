# 46. Build a LiveView Clone

## Context

Your team is building a dashboard that shows live telemetry from a fleet of IoT sensors — thousands of readings per second. The first iteration used a React SPA polling an API every second. The result: 10,000 concurrent browser clients × 1 API call/second = 10k HTTP requests/second sustained, each requiring auth, serialization, and full state transfer.

The team proposes a server-driven UI model: each browser holds one WebSocket connection to a stateful server process. When a sensor reading changes, the server computes a DOM diff and sends only the delta. The browser applies the patch. No polling. No full state transfer. Client JavaScript is a runtime of ~5KB.

You will build `Vivo`: a Phoenix LiveView-equivalent framework. One GenServer per connection holds view state. A compile-time template DSL separates static HTML (sent once) from dynamic expressions (diffed per update). The benchmark target is 10,000 concurrent connections with memory overhead under 10MB above baseline.

## Why a GenServer per connection and not a single shared process

LiveView's process-per-connection model maps naturally to the BEAM's cheap process model. A minimal GenServer with no heap data costs ~3KB of memory. 10,000 connections × 3KB = 30MB, well within limits. The advantage: failure isolation. A crash in one user's view process does not affect any other user. A shared process would serialize all view updates through one mailbox, capping throughput at ~1M msg/s on one core, and a bug affecting one user's state corrupts everyone.

## Why compile-time template parsing matters for diffing

If you render templates as strings, diffing requires parsing HTML at runtime on every update — O(HTML size) per diff. LiveView's insight: at compile time, separate the template into static parts (never change) and dynamic slots (expressions that may change). The render function returns a struct like `{static: ["<div>", "</div>"], dynamic: [expr1, expr2]}`. The diff is O(dynamic slots), not O(HTML size). Static parts are sent once on connect; subsequent updates send only changed dynamic values.

## Why structural tree diffing and not string diff

String diff (Myers' algorithm) finds the minimum edit distance between two strings. For HTML, a one-character change in an attribute might require inserting and deleting many bytes across the string diff. Structural diff operates on the parsed DOM tree: if the tag, attributes, and children are unchanged, the node is identical regardless of its position in the string. This produces semantically correct patches (no attribute-order artifacts) and enables keyed list reordering.

## Project Structure

```
vivo/
├── mix.exs
├── lib/
│   ├── vivo/
│   │   ├── socket.ex          # WebSocket handshake (RFC 6455) + channel multiplexing
│   │   ├── channel.ex         # Topic routing, join/leave, push/reply protocol
│   │   ├── live_view.ex       # Behaviour: mount/3, render/1, handle_event/3, handle_info/2
│   │   ├── live_component.ex  # Behaviour: mount/1, update/2, render/1, handle_event/3
│   │   ├── process.ex         # GenServer per connection: assigns, lifecycle
│   │   ├── template/
│   │   │   ├── compiler.ex    # Compile-time parser: static/dynamic split
│   │   │   ├── engine.ex      # EEx engine extension for ~LV sigil
│   │   │   └── rendered.ex    # %Rendered{static: [...], dynamic: [...]} struct
│   │   ├── diff/
│   │   │   ├── tree.ex        # DOM tree struct: tag, attrs, children
│   │   │   ├── diff.ex        # Structural tree diff: compute patch
│   │   │   └── patch.ex       # Patch serialization to JSON/binary
│   │   └── js_bridge.ex       # push_event/3, phx-* event routing
│   ├── vivo.ex
│   └── vivo_web/
│       └── static/
│           └── vivo.js        # Thin client runtime (~5KB)
├── test/
│   ├── socket_test.exs
│   ├── diff_test.exs
│   ├── template_test.exs
│   ├── live_view_test.exs
│   └── property/
│       └── diff_property_test.exs   # StreamData-based diff correctness
└── bench/
    └── connections.exs        # 10k concurrent connection benchmark
```

## Step 1 — WebSocket channel protocol

```elixir
defmodule Vivo.Socket do
  @moduledoc """
  Manages a single WebSocket connection.
  Handles: handshake upgrade, framing (RFC 6455), channel multiplexing.
  """

  @magic_guid "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

  @doc "Compute WebSocket accept key from Sec-WebSocket-Key header"
  def accept_key(client_key) do
    # TODO: client_key <> @magic_guid |> then(&:crypto.hash(:sha, &1)) |> Base.encode64()
  end

  @doc "Parse a WebSocket frame from binary. Returns {opcode, payload, rest}."
  def parse_frame(<<fin::1, _rsv::3, opcode::4, mask::1, payload_len::7, rest::binary>>) do
    # TODO: handle payload_len == 126 (next 2 bytes) and 127 (next 8 bytes)
    # TODO: if mask == 1, read 4-byte masking key, unmask payload
    # TODO: return {:ok, opcode, payload, remaining_binary}
    # Opcodes: 0x1 = text, 0x2 = binary, 0x8 = close, 0x9 = ping, 0xA = pong
  end

  @doc "Build a server-to-client WebSocket frame (no masking for server frames)"
  def build_frame(opcode, payload) when is_binary(payload) do
    # TODO: encode as <<1::1, 0::3, opcode::4, 0::1, byte_size(payload)::7, payload::binary>>
    # TODO: handle lengths > 125 (use extended length fields)
  end
end

defmodule Vivo.Channel do
  @doc """
  Phoenix channel protocol:
  Client sends: [join_ref, message_ref, topic, event, payload]
  Server sends: [join_ref, message_ref, topic, event, payload]
  Events: "phx_join", "phx_leave", "phx_reply", "phx_error", "phx_heartbeat"
  """

  def handle_message(conn_pid, [join_ref, msg_ref, topic, event, payload]) do
    # TODO: route to appropriate LiveView process based on topic
    # TODO: "phx_join" → spawn LiveView process, call mount/3
    # TODO: "phx_heartbeat" → reply with {nil, msg_ref, "phoenix", "phx_reply", %{status: "ok"}}
    # TODO: other events → forward to LiveView process
  end

  def push(conn_pid, topic, event, payload) do
    # TODO: encode as JSON: [nil, nil, topic, event, payload]
    # TODO: send as WebSocket text frame
  end
end
```

## Step 2 — LiveView behaviour and process

```elixir
defmodule Vivo.LiveView do
  @callback mount(params :: map(), session :: map(), socket :: map()) ::
    {:ok, socket :: map()} | {:error, reason :: term()}

  @callback render(assigns :: map()) :: Vivo.Template.Rendered.t()

  @callback handle_event(event :: String.t(), payload :: map(), socket :: map()) ::
    {:noreply, socket :: map()} | {:reply, map(), socket :: map()}

  @callback handle_info(msg :: term(), socket :: map()) ::
    {:noreply, socket :: map()}

  @callback handle_params(params :: map(), uri :: String.t(), socket :: map()) ::
    {:noreply, socket :: map()}

  @optional_callbacks [handle_event: 3, handle_info: 2, handle_params: 3]

  defmacro __using__(_opts) do
    quote do
      @behaviour Vivo.LiveView
      import Vivo.Socket.Helpers, only: [assign: 2, assign: 3, push_event: 3]
      def handle_event(_event, _params, socket), do: {:noreply, socket}
      def handle_info(_msg, socket), do: {:noreply, socket}
      def handle_params(_params, _uri, socket), do: {:noreply, socket}
      defoverridable [handle_event: 3, handle_info: 2, handle_params: 3]
    end
  end
end

defmodule Vivo.Process do
  use GenServer

  # State: %{module, assigns, rendered, socket_pid, topic}

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  def init(opts) do
    module = Keyword.fetch!(opts, :module)
    socket_pid = Keyword.fetch!(opts, :socket_pid)
    params = Keyword.get(opts, :params, %{})
    session = Keyword.get(opts, :session, %{})

    socket = %{assigns: %{}, module: module, pid: self()}
    case module.mount(params, session, socket) do
      {:ok, new_socket} ->
        rendered = module.render(new_socket.assigns)
        # TODO: send initial render to client (full static + dynamic)
        {:ok, %{module: module, assigns: new_socket.assigns, rendered: rendered, socket_pid: socket_pid}}
      {:error, reason} ->
        {:stop, reason}
    end
  end

  def handle_cast({:event, event, payload}, state) do
    socket = %{assigns: state.assigns, module: state.module, pid: self()}
    case state.module.handle_event(event, payload, socket) do
      {:noreply, new_socket} ->
        maybe_diff_and_push(state, new_socket)
      {:reply, reply_payload, new_socket} ->
        # TODO: send reply to client
        maybe_diff_and_push(state, new_socket)
    end
  end

  def handle_info(msg, state) do
    socket = %{assigns: state.assigns, module: state.module, pid: self()}
    {:noreply, new_socket} = state.module.handle_info(msg, socket)
    maybe_diff_and_push(state, new_socket)
  end

  defp maybe_diff_and_push(state, new_socket) do
    new_rendered = state.module.render(new_socket.assigns)
    # TODO: compute diff between state.rendered and new_rendered
    # TODO: if diff is non-empty, push to socket_pid
    # TODO: update state with new assigns and rendered
    {:noreply, %{state | assigns: new_socket.assigns, rendered: new_rendered}}
  end
end
```

## Step 3 — Compile-time template DSL

```elixir
defmodule Vivo.Template.Engine do
  @moduledoc """
  EEx engine that separates static HTML from dynamic expressions.
  Produces a %Rendered{static: [binary], dynamic: [expr]} struct.
  """

  # At compile time, the template "<div><%= @name %></div>" becomes:
  # %Rendered{static: ["<div>", "</div>"], dynamic: [assigns.name]}
  # This means only the dynamic parts need to be diffed on re-render.

  @doc "Compile a template string into a %Rendered{} AST at compile time"
  defmacro sigil_LV({:<<>>, _meta, [template]}, _modifiers) do
    # TODO: parse template string at compile time
    # TODO: split into static segments and dynamic expression ASTs
    # TODO: generate code that constructs %Rendered{static: [...], dynamic: [...]}
    # HINT: use EEx.compile_string/2 with custom engine module
    # HINT: custom engine accumulates {static_parts, dynamic_exprs} instead of concat string
  end
end

defmodule Vivo.Template.Rendered do
  @enforce_keys [:static, :dynamic]
  defstruct [:static, :dynamic, :components]

  @type t :: %__MODULE__{
    static: [binary()],
    dynamic: [binary() | t()],    # nested for components
    components: map() | nil
  }

  @doc "Render to full HTML string (for initial page load)"
  def to_html(%__MODULE__{static: static, dynamic: dynamic}) do
    # TODO: interleave static and dynamic parts into one string
    # static: ["<div>", "</div>"], dynamic: ["Alice"] → "<div>Alice</div>"
  end
end
```

## Step 4 — Structural DOM diff

```elixir
defmodule Vivo.Diff.Tree do
  defstruct tag: nil, attrs: %{}, children: [], text: nil, key: nil

  @doc "Parse an HTML string into a tree. Minimal: supports tag, attributes, text, nesting."
  def parse(html) when is_binary(html) do
    # TODO: tokenize html into [{:open_tag, tag, attrs}, {:text, content}, {:close_tag, tag}]
    # TODO: build tree recursively from token stream
    # HINT: use a stack-based approach; push on open_tag, pop and add child on close_tag
  end
end

defmodule Vivo.Diff.Diff do
  alias Vivo.Diff.Tree
  alias Vivo.Template.Rendered

  @doc """
  Compute a diff between two %Rendered{} structs.
  Returns a patch map: %{slot_index => new_value} for changed dynamic slots.
  Nested components tracked separately.
  """
  def compute(%Rendered{dynamic: old_d}, %Rendered{dynamic: new_d}) do
    old_d
    |> Enum.zip(new_d)
    |> Enum.with_index()
    |> Enum.reduce(%{}, fn {{old, new}, i}, acc ->
      if old == new do
        acc
      else
        Map.put(acc, i, new)
      end
    end)
  end

  @doc "Diff two DOM trees; return list of patches"
  def diff_trees(%Tree{} = old_tree, %Tree{} = new_tree) do
    # TODO: if tag differs: replace entire node
    # TODO: if attrs differ: {:set_attr, key, value} or {:remove_attr, key} per changed attr
    # TODO: diff children:
    #   - if children have :key attributes, use keyed diff (Myers-like on key sequence)
    #   - else: zip and recurse, emit {:insert}, {:delete} for length difference
  end

  @doc "Keyed list diff: produce insert/move/delete operations preserving identity"
  def diff_keyed(old_children, new_children) do
    # TODO: build old index: %{key => {index, node}}
    # TODO: for each new child: if key exists in old, it's a move/update; else insert
    # TODO: keys in old not in new: delete
    # HINT: This is the algorithm React calls "reconciliation"
  end
end
```

## Step 5 — Client runtime (vivo.js)

```javascript
// lib/vivo_web/static/vivo.js
// Thin client runtime: WebSocket connection + patch application

const Vivo = {
  connect(url, params = {}) {
    this.socket = new WebSocket(url);
    this.socket.onopen = () => this._join(params);
    this.socket.onmessage = (e) => this._handleMessage(JSON.parse(e.data));
    this.hooks = {};
  },

  _join(params) {
    // TODO: send [null, "1", "lv:page", "phx_join", params]
  },

  _handleMessage([joinRef, msgRef, topic, event, payload]) {
    if (event === "phx_reply" && payload.response?.rendered) {
      // TODO: initial render: apply full static+dynamic to DOM
      this._applyFullRender(payload.response.rendered);
    } else if (event === "diff") {
      // TODO: apply patch: update only changed dynamic slots
      this._applyPatch(payload);
    } else if (event === "push_event") {
      // TODO: dispatch to registered JS hooks
      const hook = this.hooks[payload.event];
      if (hook) hook(payload.payload);
    }
  },

  _applyPatch(diff) {
    // TODO: for each changed slot index, update the corresponding DOM text node
    // HINT: static parts are separated by <!-- s0 -->, <!-- s1 --> comments as markers
  },

  _bindEvents() {
    // TODO: use event delegation on document.body
    // TODO: listen for click/submit/change/keydown/blur
    // TODO: find closest element with phx-click, phx-submit, etc.
    // TODO: send event to server: ["1", ref, topic, event_name, {value, key, etc}]
  }
};
```

## Given tests

```elixir
# test/diff_test.exs
defmodule Vivo.DiffTest do
  use ExUnit.Case, async: true
  alias Vivo.Template.Rendered
  alias Vivo.Diff.Diff

  test "no diff when dynamic values unchanged" do
    r = %Rendered{static: ["<div>", "</div>"], dynamic: ["Alice"]}
    assert Diff.compute(r, r) == %{}
  end

  test "diff detects changed dynamic slot" do
    old = %Rendered{static: ["<div>", "</div>"], dynamic: ["Alice"]}
    new = %Rendered{static: ["<div>", "</div>"], dynamic: ["Bob"]}
    assert Diff.compute(old, new) == %{0 => "Bob"}
  end

  test "diff is O(dynamic slots) not O(HTML size)" do
    # Large static parts, one dynamic slot
    static = [String.duplicate("<span>x</span>", 1000), ""]
    old = %Rendered{static: static, dynamic: ["Alice"]}
    new = %Rendered{static: static, dynamic: ["Bob"]}
    {time_us, patch} = :timer.tc(fn -> Diff.compute(old, new) end)
    assert patch == %{0 => "Bob"}
    # Should complete in microseconds regardless of static size
    assert time_us < 1000, "diff took #{time_us}µs — not O(dynamic)"
  end
end

# test/template_test.exs
defmodule Vivo.TemplateTest do
  use ExUnit.Case, async: true
  import Vivo.Template.Engine
  alias Vivo.Template.Rendered

  test "sigil_LV produces Rendered struct with static/dynamic split" do
    assigns = %{name: "World"}
    result = ~LV"<div>Hello <%= @name %>!</div>"
    assert %Rendered{} = result
    assert result.static == ["<div>Hello ", "!</div>"]
    assert result.dynamic == [assigns.name]
  end

  test "template syntax error raises CompileError with line number" do
    assert_raise CompileError, ~r/line \d+/, fn ->
      Code.eval_string(~S'import Vivo.Template.Engine; ~LV"<div><%= @x"')
    end
  end
end

# test/live_view_test.exs
defmodule Vivo.LiveViewTest do
  use ExUnit.Case, async: false

  defmodule CounterView do
    use Vivo.LiveView
    import Vivo.Template.Engine

    def mount(_params, _session, socket) do
      {:ok, assign(socket, count: 0)}
    end

    def render(assigns) do
      ~LV"<div><span><%= @count %></span></div>"
    end

    def handle_event("increment", _params, socket) do
      {:noreply, assign(socket, count: socket.assigns.count + 1)}
    end

    def handle_info(:tick, socket) do
      {:noreply, assign(socket, count: socket.assigns.count + 1)}
    end
  end

  test "mount initializes assigns" do
    socket = %{assigns: %{}, module: CounterView, pid: self()}
    {:ok, new_socket} = CounterView.mount(%{}, %{}, socket)
    assert new_socket.assigns.count == 0
  end

  test "handle_event increments count" do
    socket = %{assigns: %{count: 5}, module: CounterView, pid: self()}
    {:noreply, new_socket} = CounterView.handle_event("increment", %{}, socket)
    assert new_socket.assigns.count == 6
  end

  test "handle_info ticks count" do
    socket = %{assigns: %{count: 0}, module: CounterView, pid: self()}
    {:noreply, new_socket} = CounterView.handle_info(:tick, socket)
    assert new_socket.assigns.count == 1
  end

  test "render produces Rendered struct" do
    result = CounterView.render(%{count: 42})
    assert %Vivo.Template.Rendered{} = result
    html = Vivo.Template.Rendered.to_html(result)
    assert html =~ "42"
  end
end

# test/property/diff_property_test.exs
defmodule Vivo.DiffPropertyTest do
  use ExUnit.Case, async: true
  use ExUnitProperties
  alias Vivo.Template.Rendered
  alias Vivo.Diff.Diff

  property "applying a patch to old rendered produces new rendered" do
    check all(
      dynamic_values <- list_of(string(:alphanumeric), min_length: 1, max_length: 5),
      new_values <- list_of(string(:alphanumeric), min_length: 1, max_length: 5),
      min_runs: 100
    ) do
      n = min(length(dynamic_values), length(new_values))
      old = %Rendered{static: List.duplicate("", n + 1), dynamic: Enum.take(dynamic_values, n)}
      new = %Rendered{static: List.duplicate("", n + 1), dynamic: Enum.take(new_values, n)}
      patch = Diff.compute(old, new)
      # Apply patch to old dynamic values
      patched_dynamic =
        old.dynamic
        |> Enum.with_index()
        |> Enum.map(fn {v, i} -> Map.get(patch, i, v) end)
      assert patched_dynamic == new.dynamic
    end
  end
end
```

## Benchmark

```elixir
# bench/connections.exs
# Run with: mix run bench/connections.exs
defmodule Vivo.Bench.Connections do
  @target_connections 10_000
  @duration_s 60
  @update_interval_ms 1000

  def run do
    {:ok, _} = Vivo.start(:normal, [])
    Process.sleep(500)

    IO.puts("Opening #{@target_connections} connections...")
    baseline_memory = :erlang.memory(:total)

    pids = Enum.map(1..@target_connections, fn i ->
      {:ok, pid} = Vivo.Process.start_link(
        module: Vivo.Bench.TestView,
        socket_pid: self(),
        params: %{"id" => i}
      )
      pid
    end)

    connected_memory = :erlang.memory(:total)
    overhead_mb = (connected_memory - baseline_memory) / (1024 * 1024)
    IO.puts("Memory overhead: #{Float.round(overhead_mb, 1)} MB (target: < 10 MB)")

    # Send updates and measure diff delivery latency
    IO.puts("Sending updates for #{@duration_s}s...")
    latencies = measure_latencies(pids, @duration_s)

    sorted = Enum.sort(latencies)
    n = length(sorted)
    p99 = Enum.at(sorted, trunc(n * 0.99))

    IO.puts("P99 diff latency: #{Float.round(p99, 2)} ms (target: < 50 ms)")
    IO.puts("Pass: #{if overhead_mb < 10 and p99 < 50, do: "YES", else: "NO"}")

    Enum.each(pids, &GenServer.stop/1)
  end

  defp measure_latencies(pids, duration_s) do
    end_time = System.monotonic_time(:millisecond) + duration_s * 1000
    # Sample 100 random processes per second and measure diff roundtrip
    Stream.repeatedly(fn ->
      pid = Enum.random(pids)
      t0 = System.monotonic_time(:millisecond)
      GenServer.cast(pid, {:event, "tick", %{}})
      receive do
        {:diff, _} -> System.monotonic_time(:millisecond) - t0
      after
        100 -> 100
      end
    end)
    |> Stream.take_while(fn _ -> System.monotonic_time(:millisecond) < end_time end)
    |> Enum.to_list()
  end
end

Vivo.Bench.Connections.run()
```

## Trade-offs

| Design choice | Selected | Alternative | Trade-off |
|---|---|---|---|
| Template separation | Compile-time static/dynamic split | Runtime string diff | Runtime: O(HTML size) per diff; compile-time: O(dynamic slots) — 10× faster for large templates |
| Process model | One GenServer per connection | Shared pool of GenServers | Pool: lower memory per idle connection; GenServer-per: failure isolation, no cross-user state |
| Diff granularity | Slot-based (index → value) | Full DOM tree diff | Tree diff: handles arbitrary HTML mutations; slot-based: 10× simpler, sufficient for template-generated HTML |
| Client protocol | JSON arrays `[join_ref, msg_ref, topic, event, payload]` | Binary protocol | Binary: smaller frames; JSON: debuggable, no decoder needed, ~100 bytes overhead negligible |
| Keyed list diff | Myers algorithm on key sequence | Index-based zip | Index-based: wrong for reordering (treats move as delete+insert); keyed: O(N) extra memory, correct semantics |

## Production mistakes

**Sending full rendered HTML after every event.** The whole point of this architecture is to send only diffs. If `handle_event` returns assigns that produce a new render, always compute the diff first and send nothing if the rendered output is identical. An early implementation may send full re-renders "to be safe," destroying the bandwidth advantage.

**Not garbage-collecting completed LiveView processes.** When a WebSocket disconnects, the LiveView process must be stopped. If the socket process dies without sending a `:DOWN` message to the LiveView process, the view process becomes an orphan. Use `Process.monitor(socket_pid)` in the LiveView process and handle `{:DOWN, ...}` to self-terminate.

**Accumulating large assigns maps.** Every assign call creates a new map. If `handle_info` runs 60 times per second (for an animation) and assigns a large binary on each call, each update pins a new copy. Use `assign_new/3` (only assigns if key absent) and avoid storing large binaries in assigns — store them in ETS and keep only the key in assigns.

**Not handling WebSocket close frames.** A client that closes gracefully sends a close frame (opcode 0x8). If the server ignores it and continues sending, the TCP connection will eventually be forcibly reset, but only after a timeout. Handle close frames by immediately stopping the socket process, which cascades to stopping the LiveView process.

**Template DSL not raising on missing assigns.** If `@count` is used in a template but `count` is not in assigns, the current behavior may silently render `nil` as empty string. LiveView raises a `KeyError` in dev mode. Add a `fetch_assign!/2` macro that raises with the template file and line number — this catches bugs at development time rather than silently breaking production UIs.

## Resources

- Phoenix.LiveView source — https://github.com/phoenixframework/phoenix_live_view (the reference implementation)
- McCord — "LiveView: Interactive, Real-Time Apps" — ElixirConf EU 2019 (original design motivation)
- RFC 6455 — The WebSocket Protocol — https://datatracker.ietf.org/doc/html/rfc6455
- React Reconciliation documentation — https://legacy.reactjs.org/docs/reconciliation.html (keyed diffing algorithm)
- Myers — "An O(ND) Difference Algorithm and Its Variations" (1986) — Algorithmica 1(2) (foundation for list diff)
- BEAM VM memory — https://www.erlang.org/doc/efficiency_guide/processes.html (process cost model)
- McCord & Loder — "Programming Phoenix LiveView" (Pragmatic Bookshelf)
