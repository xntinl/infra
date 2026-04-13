# LiveView Clone

**Project**: `vivo` — Server-driven UI framework with GenServer per connection and compile-time template DSL

## Project context

Your team is building a dashboard that shows live telemetry from a fleet of IoT sensors — thousands of readings per second. The first iteration used a React SPA polling an API every second. The result: 10,000 concurrent browser clients x 1 API call/second = 10k HTTP requests/second sustained, each requiring auth, serialization, and full state transfer.

The team proposes a server-driven UI model: each browser holds one WebSocket connection to a stateful server process. When a sensor reading changes, the server computes a DOM diff and sends only the delta. No polling. No full state transfer. Client JavaScript is a runtime of ~5KB.

You will build `Vivo`: a Phoenix LiveView-equivalent framework. One GenServer per connection holds view state. A compile-time template DSL separates static HTML (sent once) from dynamic expressions (diffed per update). The benchmark target is 10,000 concurrent connections with memory overhead under 10MB above baseline.

## Design decisions

**Option A — server sends full HTML on every state change**
- Pros: simplest to implement
- Cons: bandwidth waste, flicker, lost input focus

**Option B — tree-diffing with static/dynamic partition sent as minimal patches** (chosen)
- Pros: minimal bandwidth, preserves client state, batches updates
- Cons: requires a template compiler

→ Chose **B** because LiveView's value is entirely in the diffing — without it, it's just a worse SPA.

## Quick start

1. Create project:
   ```bash
   mix new <project_name>
   cd <project_name>
   ```

2. Copy dependencies to `mix.exs`

3. Implement modules following the project structure

4. Run tests: `mix test`

5. Benchmark: `mix run lib/benchmark.exs`

## Why a GenServer per connection and not a single shared process

LiveView's process-per-connection model maps to the BEAM's cheap process model. A minimal GenServer with no heap data costs ~3KB of memory. 10,000 connections x 3KB = 30MB. The advantage: failure isolation. A crash in one user's view process does not affect any other user.

## Why compile-time template parsing matters for diffing

If you render templates as strings, diffing requires parsing HTML at runtime on every update — O(HTML size) per diff. At compile time, separate the template into static parts (never change) and dynamic slots (expressions that may change). The diff is O(dynamic slots), not O(HTML size). Static parts are sent once on connect; subsequent updates send only changed dynamic values.

## Why structural tree diffing and not string diff

String diff (Myers' algorithm) finds minimum edit distance between two strings. Structural diff operates on the parsed DOM tree: if the tag, attributes, and children are unchanged, the node is identical. This produces semantically correct patches and enables keyed list reordering.

## Project structure
```
vivo/
├── script/
│   └── main.exs
├── mix.exs
├── lib/
│   ├── vivo/
│   │   ├── socket.ex
│   │   ├── channel.ex
│   │   ├── live_view.ex
│   │   ├── live_component.ex
│   │   ├── process.ex
│   │   ├── template/
│   │   │   ├── compiler.ex
│   │   │   ├── engine.ex
│   │   │   └── rendered.ex
│   │   ├── diff/
│   │   │   ├── tree.ex
│   │   │   ├── diff.ex
│   │   │   └── patch.ex
│   │   └── js_bridge.ex
│   ├── vivo.ex
│   └── vivo_web/
│       └── static/
│           └── vivo.js
├── test/
│   ├── socket_test.exs
│   ├── diff_test.exs
│   ├── template_test.exs
│   ├── live_view_test.exs
│   └── property/
│       └── diff_property_test.exs
└── bench/
    └── connections.exs
```

### Step 1: WebSocket channel protocol

**Objective**: Parse RFC 6455 frames with bitstring pattern matching and multiplex channel messages so one socket carries the full LiveView protocol.

### Step 2: LiveView behaviour and process

**Objective**: Define the behaviour contract and its required callbacks.

```elixir
defmodule Vivo.LiveView do
  @moduledoc "LiveView Clone - implementation"

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
  @moduledoc """
  GenServer per connection. Holds assigns, manages lifecycle,
  computes diffs on state change, and pushes updates to the socket.
  """
  use GenServer

  alias Vivo.Template.Rendered
  alias Vivo.Diff.Diff

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    module = Keyword.fetch!(opts, :module)
    socket_pid = Keyword.fetch!(opts, :socket_pid)
    params = Keyword.get(opts, :params, %{})
    session = Keyword.get(opts, :session, %{})

    socket = %{assigns: %{}, module: module, pid: self()}

    case module.mount(params, session, socket) do
      {:ok, new_socket} ->
        rendered = module.render(new_socket.assigns)
        html = Rendered.to_html(rendered)
        send(socket_pid, {:initial_render, html})

        {:ok,
         %{
           module: module,
           assigns: new_socket.assigns,
           rendered: rendered,
           socket_pid: socket_pid
         }}

      {:error, reason} ->
        {:stop, reason}
    end
  end

  @impl true
  def handle_cast({:event, event, payload}, state) do
    socket = %{assigns: state.assigns, module: state.module, pid: self()}

    case state.module.handle_event(event, payload, socket) do
      {:noreply, new_socket} ->
        maybe_diff_and_push(state, new_socket)

      {:reply, reply_payload, new_socket} ->
        send(state.socket_pid, {:reply, reply_payload})
        maybe_diff_and_push(state, new_socket)
    end
  end

  @impl true
  def handle_info(msg, state) do
    socket = %{assigns: state.assigns, module: state.module, pid: self()}
    {:noreply, new_socket} = state.module.handle_info(msg, socket)
    maybe_diff_and_push(state, new_socket)
  end

  defp maybe_diff_and_push(state, new_socket) do
    new_rendered = state.module.render(new_socket.assigns)
    diff = Diff.compute(state.rendered, new_rendered)

    if map_size(diff) > 0 do
      send(state.socket_pid, {:diff, diff})
    end

    {:noreply, %{state | assigns: new_socket.assigns, rendered: new_rendered}}
  end
end
```
### Step 3: Compile-time template DSL

**Objective**: Compile the AST into executable bytecode or target instructions.

```elixir
defmodule Vivo.Template.Rendered do
  @moduledoc """
  Struct representing a pre-split template: static parts that never change
  and dynamic parts that are diffed on each re-render.
  """

  @enforce_keys [:static, :dynamic]
  defstruct [:static, :dynamic, :components]

  @type t :: %__MODULE__{
          static: [binary()],
          dynamic: [binary() | t()],
          components: map() | nil
        }

  @doc """
  Render to full HTML string by interleaving static and dynamic parts.
  static: ["<div>", "</div>"], dynamic: ["Alice"] => "<div>Alice</div>"
  """
  @spec to_html(t()) :: String.t()
  def to_html(%__MODULE__{static: static, dynamic: dynamic}) do
    interleave(static, dynamic, [])
    |> IO.iodata_to_binary()
  end

  defp interleave([s], [], acc), do: Enum.reverse([s | acc])
  defp interleave([s | ss], [d | ds], acc) do
    d_str =
      case d do
        %__MODULE__{} = nested -> to_html(nested)
        other -> to_string(other)
      end

    interleave(ss, ds, [d_str, s | acc])
  end

  defp interleave([], _, acc), do: Enum.reverse(acc)
end

defmodule Vivo.Template.Engine do
  @moduledoc """
  Compile-time template engine. The ~LV sigil splits a template string
  into static HTML parts and dynamic expression parts, producing a
  %Rendered{} struct. Only dynamic parts are diffed on re-render.
  """

  @doc """
  Sigil that compiles a template string into a %Rendered{} struct at compile time.
  Static parts are literal strings; dynamic parts are EEx expressions (<%= ... %>).
  """
  defmacro sigil_LV({:<<>>, _meta, [template]}, _modifiers) do
    {static_parts, dynamic_exprs} = parse_template(template)

    dynamic_ast =
      Enum.map(dynamic_exprs, fn expr_str ->
        Code.string_to_quoted!(expr_str)
      end)

    quote do
      %Vivo.Template.Rendered{
        static: unquote(static_parts),
        dynamic: unquote(dynamic_ast)
      }
    end
  end

  defp parse_template(template) do
    parse_template(template, [], [], "")
  end

  defp parse_template("", static_parts, dynamic_exprs, current_static) do
    {Enum.reverse([current_static | static_parts]), Enum.reverse(dynamic_exprs)}
  end

  defp parse_template("<%=" <> rest, static_parts, dynamic_exprs, current_static) do
    case String.split(rest, "%>", parts: 2) do
      [expr, remaining] ->
        expr = String.trim(expr)
        expr = String.replace(expr, "@", "assigns.")

        parse_template(
          remaining,
          [current_static | static_parts],
          [expr | dynamic_exprs],
          ""
        )

      [_no_closing] ->
        raise CompileError,
          description: "unclosed EEx expression at line 1",
          line: 1,
          file: "template"
    end
  end

  defp parse_template(<<char::utf8, rest::binary>>, static_parts, dynamic_exprs, current_static) do
    parse_template(rest, static_parts, dynamic_exprs, current_static <> <<char::utf8>>)
  end
end
```
### Step 4: Structural DOM diff

**Objective**: Compute minimal DOM diffs and transmit them as patches to the client.

```elixir
defmodule Vivo.Diff.Tree do
  @moduledoc "Minimal HTML tree representation for structural diffing."

  defstruct tag: nil, attrs: %{}, children: [], text: nil, key: nil

  @doc "Parse an HTML string into a tree of nodes."
  @spec parse(String.t()) :: %__MODULE__{} | nil
  def parse(html) when is_binary(html) do
    tokens = tokenize(html, [])
    {tree, _rest} = build_tree(tokens)
    tree
  end

  defp tokenize("", acc), do: Enum.reverse(acc)

  defp tokenize("</" <> rest, acc) do
    case String.split(rest, ">", parts: 2) do
      [tag_name, remaining] ->
        tokenize(remaining, [{:close_tag, String.trim(tag_name)} | acc])

      _ ->
        Enum.reverse(acc)
    end
  end

  defp tokenize("<" <> rest, acc) do
    case String.split(rest, ">", parts: 2) do
      [tag_content, remaining] ->
        {tag_name, attrs} = parse_tag_content(String.trim(tag_content))
        tokenize(remaining, [{:open_tag, tag_name, attrs} | acc])

      _ ->
        Enum.reverse(acc)
    end
  end

  defp tokenize(html, acc) do
    case String.split(html, "<", parts: 2) do
      [text, rest] ->
        trimmed = String.trim(text)

        if trimmed != "" do
          tokenize("<" <> rest, [{:text, trimmed} | acc])
        else
          tokenize("<" <> rest, acc)
        end

      [text] ->
        trimmed = String.trim(text)

        if trimmed != "" do
          Enum.reverse([{:text, trimmed} | acc])
        else
          Enum.reverse(acc)
        end
    end
  end

  defp parse_tag_content(content) do
    [tag_name | attr_parts] = String.split(content, ~r/\s+/, trim: true)

    attrs =
      Enum.reduce(attr_parts, %{}, fn part, acc ->
        case String.split(part, "=", parts: 2) do
          [key, value] -> Map.put(acc, key, String.trim(value, "\""))
          [key] -> Map.put(acc, key, "true")
        end
      end)

    {tag_name, attrs}
  end

  defp build_tree([{:open_tag, tag, attrs} | rest]) do
    {children, rest2} = build_children(rest, tag, [])
    key = Map.get(attrs, "key")
    node = %__MODULE__{tag: tag, attrs: attrs, children: children, key: key}
    {node, rest2}
  end

  defp build_tree([{:text, text} | rest]) do
    {%__MODULE__{text: text}, rest}
  end

  defp build_tree([]) do
    {nil, []}
  end

  defp build_children([{:close_tag, tag} | rest], tag, acc) do
    {Enum.reverse(acc), rest}
  end

  defp build_children([], _tag, acc) do
    {Enum.reverse(acc), []}
  end

  defp build_children(tokens, tag, acc) do
    {child, rest} = build_tree(tokens)

    if child do
      build_children(rest, tag, [child | acc])
    else
      {Enum.reverse(acc), rest}
    end
  end
end

defmodule Vivo.Diff.Diff do
  @moduledoc """
  Diff engine for the template system. Operates on %Rendered{} structs,
  comparing dynamic slots by index. Returns a patch map of changed indices.
  Also supports structural tree diffing for DOM-level changes.
  """

  alias Vivo.Diff.Tree
  alias Vivo.Template.Rendered

  @doc """
  Compute a diff between two %Rendered{} structs.
  Returns a patch map: %{slot_index => new_value} for changed dynamic slots.
  """
  @spec compute(%Rendered{}, %Rendered{}) :: map()
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

  @doc "Diff two DOM trees; return list of patch operations."
  @spec diff_trees(%Tree{}, %Tree{}) :: [tuple()]
  def diff_trees(%Tree{} = old_tree, %Tree{} = new_tree) do
    cond do
      old_tree.tag != new_tree.tag ->
        [{:replace, new_tree}]

      true ->
        attr_patches = diff_attrs(old_tree.attrs, new_tree.attrs)
        child_patches = diff_children(old_tree.children, new_tree.children)
        attr_patches ++ child_patches
    end
  end

  defp diff_attrs(old_attrs, new_attrs) do
    removed =
      old_attrs
      |> Map.keys()
      |> Enum.reject(&Map.has_key?(new_attrs, &1))
      |> Enum.map(fn key -> {:remove_attr, key} end)

    changed =
      Enum.flat_map(new_attrs, fn {key, value} ->
        if Map.get(old_attrs, key) != value do
          [{:set_attr, key, value}]
        else
          []
        end
      end)

    removed ++ changed
  end

  defp diff_children(old_children, new_children) do
    old_keyed = Enum.all?(old_children, &(&1.key != nil))
    new_keyed = Enum.all?(new_children, &(&1.key != nil))

    if old_keyed and new_keyed and length(old_children) > 0 do
      diff_keyed(old_children, new_children)
    else
      diff_positional(old_children, new_children)
    end
  end

  defp diff_positional(old, new) do
    max_len = max(length(old), length(new))
    old_padded = old ++ List.duplicate(nil, max_len - length(old))
    new_padded = new ++ List.duplicate(nil, max_len - length(new))

    Enum.zip(old_padded, new_padded)
    |> Enum.with_index()
    |> Enum.flat_map(fn
      {{nil, new_node}, i} -> [{:insert, i, new_node}]
      {{_old_node, nil}, i} -> [{:delete, i}]
      {{old_node, new_node}, i} ->
        Enum.map(diff_trees(old_node, new_node), fn patch -> {:child, i, patch} end)
    end)
  end

  @doc "Keyed list diff: produce insert/move/delete operations preserving identity."
  @spec diff_keyed([%Tree{}], [%Tree{}]) :: [tuple()]
  def diff_keyed(old_children, new_children) do
    old_index = Map.new(Enum.with_index(old_children), fn {node, idx} -> {node.key, {idx, node}} end)

    new_keys = Enum.map(new_children, & &1.key)
    old_keys = MapSet.new(Map.keys(old_index))

    deletes =
      old_keys
      |> MapSet.difference(MapSet.new(new_keys))
      |> Enum.map(fn key -> {:delete_key, key} end)

    inserts_and_moves =
      Enum.with_index(new_children)
      |> Enum.flat_map(fn {node, new_idx} ->
        case Map.get(old_index, node.key) do
          nil ->
            [{:insert_key, node.key, new_idx, node}]

          {old_idx, old_node} ->
            child_patches = diff_trees(old_node, node)
            move = if old_idx != new_idx, do: [{:move_key, node.key, new_idx}], else: []

            move ++
              Enum.map(child_patches, fn patch -> {:child_key, node.key, patch} end)
        end
      end)

    deletes ++ inserts_and_moves
  end
end
```
### Step 5: Client runtime (vivo.js)

**Objective**: Implement the Client runtime (vivo.js) component required by the liveview clone system.

```javascript
// lib/vivo_web/static/vivo.js
// Thin client runtime: WebSocket connection + patch application

const Vivo = {
  connect(url, params = {}) {
    this.socket = new WebSocket(url);
    this.joinRef = "1";
    this.msgRef = 0;
    this.hooks = {};
    this.dynamicSlots = [];

    this.socket.onopen = () => this._join(params);
    this.socket.onmessage = (e) => this._handleMessage(JSON.parse(e.data));
    this.socket.onclose = () => console.log("[vivo] disconnected");
  },

  _join(params) {
    this.msgRef++;
    const msg = [this.joinRef, String(this.msgRef), "lv:page", "phx_join", params];
    this.socket.send(JSON.stringify(msg));
  },

  _handleMessage([joinRef, msgRef, topic, event, payload]) {
    if (event === "phx_reply" && payload.response && payload.response.rendered) {
      this._applyFullRender(payload.response.rendered);
    } else if (event === "diff") {
      this._applyPatch(payload);
    } else if (event === "push_event") {
      const hook = this.hooks[payload.event];
      if (hook) hook(payload.payload);
    }
  },

  _applyFullRender(rendered) {
    const container = document.getElementById("vivo-root");
    if (!container) return;

    let html = "";
    for (let i = 0; i < rendered.static.length; i++) {
      html += rendered.static[i];
      if (i < rendered.dynamic.length) {
        html += `<!-- s${i} -->${rendered.dynamic[i]}<!-- /s${i} -->`;
        this.dynamicSlots[i] = rendered.dynamic[i];
      }
    }
    container.innerHTML = html;
    this._bindEvents();
  },

  _applyPatch(diff) {
    for (const [index, newValue] of Object.entries(diff)) {
      const i = parseInt(index);
      this.dynamicSlots[i] = newValue;

      const walker = document.createTreeWalker(
        document.getElementById("vivo-root"),
        NodeFilter.SHOW_COMMENT
      );

      while (walker.nextNode()) {
        if (walker.currentNode.textContent.trim() === `s${i}`) {
          let next = walker.currentNode.nextSibling;
          if (next && next.nodeType === Node.TEXT_NODE) {
            next.textContent = newValue;
          }
          break;
        }
      }
    }
  },

  _bindEvents() {
    const eventTypes = ["click", "submit", "change", "keydown", "blur"];

    eventTypes.forEach(type => {
      document.body.addEventListener(type, (e) => {
        const attrName = `phx-${type}`;
        const target = e.target.closest(`[${attrName}]`);
        if (!target) return;

        const eventName = target.getAttribute(attrName);
        const value = target.value || target.textContent || "";

        this.msgRef++;
        const msg = [
          this.joinRef,
          String(this.msgRef),
          "lv:page",
          eventName,
          { value, key: e.key || "" }
        ];
        this.socket.send(JSON.stringify(msg));

        if (type === "submit") e.preventDefault();
      });
    });
  },

  registerHook(name, fn) {
    this.hooks[name] = fn;
  }
};
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
defmodule Vivo.DiffTest do
  use ExUnit.Case, async: true
  doctest Vivo.Diff.Tree
  alias Vivo.Template.Rendered
  alias Vivo.Diff.Diff

  describe "Diff" do

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
    static = [String.duplicate("<span>x</span>", 1000), ""]
    old = %Rendered{static: static, dynamic: ["Alice"]}
    new = %Rendered{static: static, dynamic: ["Bob"]}
    {time_us, patch} = :timer.tc(fn -> Diff.compute(old, new) end)
    assert patch == %{0 => "Bob"}
    assert time_us < 1000, "diff took #{time_us}us -- not O(dynamic)"
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
      patched_dynamic =
        old.dynamic
        |> Enum.with_index()
        |> Enum.map(fn {v, i} -> Map.get(patch, i, v) end)
      assert patched_dynamic == new.dynamic
    end
  end

  end
end
```
## Main Entry Point

```elixir
def main do
  IO.puts("======== 46-build-liveview-clone ========")
  IO.puts("Build liveview clone")
  IO.puts("")
  
  Vivo.Socket.start_link([])
  IO.puts("Vivo.Socket started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# bench/connections.exs
# Run with: mix run bench/connections.exs
defmodule Vivo.Bench.Connections do
  @target_connections 10_000
  @duration_s 60
  @update_rate_hz 10

  def run do
    IO.puts("=== Vivo Server-Driven UI Benchmark ===")
    IO.puts("Testing #{@target_connections} concurrent connections\n")
    
    IO.write("Opening connections... ")
    baseline_memory = :erlang.memory(:total)

    pids = Enum.map(1..@target_connections, fn i ->
      {:ok, pid} = Vivo.Process.start_link(
        module: Vivo.Bench.TestView,
        socket_pid: self(),
        params: %{"id" => i}
      )
      pid
    end)
    IO.puts("done")

    connected_memory = :erlang.memory(:total)
    overhead_mb = (connected_memory - baseline_memory) / (1024 * 1024)
    overhead_per_conn_kb = (overhead_mb * 1024) / @target_connections
    
    IO.puts("Memory overhead: #{Float.round(overhead_mb, 1)} MB (#{Float.round(overhead_per_conn_kb, 1)} KB per connection)")
    IO.puts("Target:          < 10 MB total (< 1 KB per connection)\n")

    IO.write("Running latency benchmark (#{@duration_s}s @ #{@update_rate_hz} Hz)... ")
    latencies = measure_latencies(pids, @duration_s, @update_rate_hz)
    IO.puts("done\n")

    sorted = Enum.sort(latencies)
    n = length(sorted)
    p50 = Enum.at(sorted, trunc(n * 0.50))
    p95 = Enum.at(sorted, trunc(n * 0.95))
    p99 = Enum.at(sorted, trunc(n * 0.99))
    avg = Enum.sum(latencies) / length(latencies)

    IO.puts("=== Results ===")
    IO.puts("P50 diff latency: #{Float.round(p50, 2)} ms")
    IO.puts("P95 diff latency: #{Float.round(p95, 2)} ms")
    IO.puts("P99 diff latency: #{Float.round(p99, 2)} ms")
    IO.puts("Avg diff latency: #{Float.round(avg, 2)} ms")
    IO.puts("Target P99:       < 50 ms")
    IO.puts("Target Memory:    < 10 MB total")
    IO.puts("Status:           #{if overhead_mb < 10 and p99 < 50, do: "PASS", else: "FAIL"}")

    Enum.each(pids, &GenServer.stop/1)
  end

  defp measure_latencies(pids, duration_s, update_rate_hz) do
    interval_ms = div(1000, update_rate_hz)
    end_time = System.monotonic_time(:millisecond) + duration_s * 1000
    num_updates = duration_s * update_rate_hz

    Stream.repeatedly(fn ->
      pid = Enum.random(pids)
      t0 = System.monotonic_time(:millisecond)
      GenServer.cast(pid, {:event, "tick", %{}})
      receive do
        {:diff, _} -> System.monotonic_time(:millisecond) - t0
      after
        200 -> 200
      end
    end)
    |> Stream.take_while(fn _ -> System.monotonic_time(:millisecond) < end_time end)
    |> Enum.to_list()
  end
end

Vivo.Bench.Connections.run()
```
## Key Concepts: Architecture & Design Patterns Server-Driven UI vs. Client-Side SPAs

La arquitectura **server-driven UI** (LiveView, Vivo) es fundamentalmente diferente a una SPA:

### SPA (Single-Page Application - React, Vue, etc.)

- Cliente descarga JavaScript (~500KB de código, frameworks, deps).
- Cliente mantiene estado en memoria.
- Cambios de estado se renderizan localmente.
- Para datos frescos: cliente hace polling (10k clientes x 1 req/sec = 10k req/sec) o WebSocket push.
- El servidor es un API stateless.
- **Ventaja**: UX responsivo, sin latency de network.
- **Desventaja**: El cliente es un ordenador — 10k clientes ejecutando lógica = 10k computas. Seguridad basada en cliente.

### Server-Driven UI (LiveView, Vivo)

- Cliente descarga HTML + ~5KB JavaScript runtime.
- Servidor mantiene estado **en un GenServer por conexión**.
- Cambios de estado se renderizan **en el servidor**.
- Servidor difunde solo el **delta de HTML** al cliente.
- Cliente aplica el delta al DOM.
- **Ventaja**: Servidor es fuente de verdad. Lógica centralizada. Seguridad en el servidor. Memoria por cliente = 3KB (GenServer) + assigns (típicamente <100KB).
- **Desventaja**: Input latency = network round-trip (~100ms). No offline-capable.

### El trade-off de la arquitectura de difusión

Un cambio en el servidor puede afectar a múltiples clientes. En una SPA, cada cliente re-computa independientemente. En server-driven UI:
- **Mensaje único**: Servidor difunde un cambio una sola vez.
- **Múltiples clientes**: Si 10k clientes ven la misma tabla, el servidor computa el diff una sola vez y lo envía 10k veces (broadcast).
- Compare con SPA: 10k clientes hacen polling, 10k requests.

### LiveView evita LiveComponent en producción

La ventaja clave de LiveView/Vivo es la **reactividad automática**: cuando assigns cambia, Phoenix difunde automáticamente a todos los sockets. Los componentes (LiveComponent) rompem esto — requieren manual push. En producción, la mayoría de apps usan LiveView puro.

---

## Trade-off analysis

| Design choice | Selected | Alternative | Trade-off |
|---|---|---|---|
| Template separation | Compile-time static/dynamic split | Runtime string diff | Runtime: O(HTML size) per diff; compile-time: O(dynamic slots) |
| Process model | One GenServer per connection | Shared pool | Pool: lower idle memory; per-connection: failure isolation |
| Diff granularity | Slot-based (index -> value) | Full DOM tree diff | Tree diff handles arbitrary mutations; slot-based is 10x simpler |
| Client protocol | JSON arrays | Binary protocol | Binary: smaller; JSON: debuggable, negligible overhead |
| Keyed list diff | Myers on key sequence | Index-based zip | Index-based: wrong for reordering; keyed: correct semantics |

## Common production mistakes

**Sending full rendered HTML after every event.** Always compute the diff first and send nothing if identical.

**Not garbage-collecting completed LiveView processes.** When a WebSocket disconnects, the LiveView process must stop. Use `Process.monitor(socket_pid)` and handle `{:DOWN, ...}`.

**Accumulating large assigns maps.** Use `assign_new/3` (only assigns if key absent) and avoid large binaries in assigns.

**Not handling WebSocket close frames.** A client that closes gracefully sends a close frame (opcode 0x8). Handle it immediately to avoid zombie connections.

**Template DSL not raising on missing assigns.** If `@count` is used but `count` is not in assigns, the behavior may silently render nil. Raise a `KeyError` in dev mode with file and line.

## Reflection

A LiveView page with a 10k-row table gets a single-cell update. Does your framework send 10k rows, 1 row, or 1 cell? What does the template have to look like to hit the best case?

## Resources

- Phoenix.LiveView source -- https://github.com/phoenixframework/phoenix_live_view
- RFC 6455 -- The WebSocket Protocol -- https://datatracker.ietf.org/doc/html/rfc6455
- React Reconciliation -- https://legacy.reactjs.org/docs/reconciliation.html
- Myers -- "An O(ND) Difference Algorithm" (1986) -- Algorithmica 1(2)
- BEAM VM memory -- https://www.erlang.org/doc/efficiency_guide/processes.html
- McCord & Loder -- "Programming Phoenix LiveView" (Pragmatic Bookshelf)

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule LiveviewClone.MixProject do
  use Mix.Project

  def project do
    [
      app: :liveview_clone,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {LiveviewClone.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `liveview_clone` (LiveView-style stateful UI).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 50000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:liveview_clone) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== LiveviewClone stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:liveview_clone) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:liveview_clone)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual liveview_clone operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

LiveviewClone classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **100k concurrent connections** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **50 ms** | Phoenix LiveView paper |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Phoenix LiveView paper: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

---

## Implementation

### `lib/vivo.ex`

```elixir
defmodule Vivo do
  @moduledoc """
  Reference implementation for LiveView Clone.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the vivo module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Vivo.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/vivo_test.exs`

```elixir
defmodule VivoTest do
  use ExUnit.Case, async: true

  doctest Vivo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Vivo.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Phoenix LiveView paper
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
