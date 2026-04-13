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

## Why a GenServer per connection and not a single shared process

LiveView's process-per-connection model maps to the BEAM's cheap process model. A minimal GenServer with no heap data costs ~3KB of memory. 10,000 connections x 3KB = 30MB. The advantage: failure isolation. A crash in one user's view process does not affect any other user.

## Why compile-time template parsing matters for diffing

If you render templates as strings, diffing requires parsing HTML at runtime on every update — O(HTML size) per diff. At compile time, separate the template into static parts (never change) and dynamic slots (expressions that may change). The diff is O(dynamic slots), not O(HTML size). Static parts are sent once on connect; subsequent updates send only changed dynamic values.

## Why structural tree diffing and not string diff

String diff (Myers' algorithm) finds minimum edit distance between two strings. Structural diff operates on the parsed DOM tree: if the tag, attributes, and children are unchanged, the node is identical. This produces semantically correct patches and enables keyed list reordering.

## Project Structure

```
vivo/
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



### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule Vivo.Socket do
  @moduledoc """
  WebSocket connection manager.
  Handles: handshake upgrade, framing (RFC 6455), channel multiplexing.
  """

  @magic_guid "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

  @doc "Compute WebSocket accept key from Sec-WebSocket-Key header."
  @spec accept_key(String.t()) :: String.t()
  def accept_key(client_key) do
    (client_key <> @magic_guid)
    |> then(&:crypto.hash(:sha, &1))
    |> Base.encode64()
  end

  @doc """
  Parse a WebSocket frame from binary.
  Returns {:ok, opcode, payload, rest} or {:error, reason}.
  Opcodes: 0x1 = text, 0x2 = binary, 0x8 = close, 0x9 = ping, 0xA = pong.
  """
  @spec parse_frame(binary()) :: {:ok, integer(), binary(), binary()} | {:error, atom()}
  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 1::1, 126::7, len::16, mask::binary-size(4), rest::binary>>) do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, unmask(payload, mask), remaining}
  end

  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 1::1, 127::7, len::64, mask::binary-size(4), rest::binary>>) do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, unmask(payload, mask), remaining}
  end

  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 1::1, len::7, mask::binary-size(4), rest::binary>>)
      when len < 126 do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, unmask(payload, mask), remaining}
  end

  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 0::1, 126::7, len::16, rest::binary>>) do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, payload, remaining}
  end

  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 0::1, 127::7, len::64, rest::binary>>) do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, payload, remaining}
  end

  def parse_frame(<<_fin::1, _rsv::3, opcode::4, 0::1, len::7, rest::binary>>)
      when len < 126 do
    <<payload::binary-size(len), remaining::binary>> = rest
    {:ok, opcode, payload, remaining}
  end

  def parse_frame(_), do: {:error, :incomplete}

  @doc "Build a server-to-client WebSocket frame (no masking for server frames)."
  @spec build_frame(integer(), binary()) :: binary()
  def build_frame(opcode, payload) when is_binary(payload) do
    len = byte_size(payload)

    cond do
      len < 126 ->
        <<1::1, 0::3, opcode::4, 0::1, len::7, payload::binary>>

      len < 65536 ->
        <<1::1, 0::3, opcode::4, 0::1, 126::7, len::16, payload::binary>>

      true ->
        <<1::1, 0::3, opcode::4, 0::1, 127::7, len::64, payload::binary>>
    end
  end

  defp unmask(payload, mask_key) do
    mask_bytes = :binary.bin_to_list(mask_key)

    payload
    |> :binary.bin_to_list()
    |> Enum.with_index()
    |> Enum.map(fn {byte, i} -> Bitwise.bxor(byte, Enum.at(mask_bytes, rem(i, 4))) end)
    |> :binary.list_to_bin()
  end
end

defmodule Vivo.Socket.Helpers do
  @moduledoc "Helper functions available inside LiveView modules."

  @doc "Assign one or more key-value pairs to the socket."
  @spec assign(map(), keyword() | map()) :: map()
  def assign(socket, key_values) when is_list(key_values) or is_map(key_values) do
    new_assigns = Enum.into(key_values, socket.assigns)
    %{socket | assigns: new_assigns}
  end

  @doc "Assign a single key-value pair."
  @spec assign(map(), atom(), term()) :: map()
  def assign(socket, key, value) do
    %{socket | assigns: Map.put(socket.assigns, key, value)}
  end

  @doc "Push a client-side event."
  @spec push_event(map(), String.t(), map()) :: map()
  def push_event(socket, event, payload) do
    events = Map.get(socket, :push_events, [])
    Map.put(socket, :push_events, [{event, payload} | events])
  end
end

defmodule Vivo.Channel do
  @moduledoc """
  Phoenix channel protocol implementation.
  Client sends: [join_ref, message_ref, topic, event, payload]
  Server sends: [join_ref, message_ref, topic, event, payload]
  """

  @doc "Handle an incoming channel message and route it appropriately."
  @spec handle_message(pid(), list()) :: :ok
  def handle_message(conn_pid, [join_ref, msg_ref, topic, event, payload]) do
    case event do
      "phx_join" ->
        send(conn_pid, {:send_frame, Jason.encode!([join_ref, msg_ref, topic, "phx_reply", %{status: "ok"}])})

      "phx_heartbeat" ->
        send(conn_pid, {:send_frame, Jason.encode!([nil, msg_ref, "phoenix", "phx_reply", %{status: "ok"}])})

      other_event ->
        send(conn_pid, {:channel_event, topic, other_event, payload})
    end

    :ok
  end

  @doc "Push an event to the client on the given topic."
  @spec push(pid(), String.t(), String.t(), map()) :: :ok
  def push(conn_pid, topic, event, payload) do
    frame = Jason.encode!([nil, nil, topic, event, payload])
    send(conn_pid, {:send_frame, frame})
    :ok
  end
end
```

### Step 2: LiveView behaviour and process

**Objective**: Define the behaviour contract and its required callbacks.


```elixir
defmodule Vivo.LiveView do
  @moduledoc "Behaviour for server-rendered live views."

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
# test/diff_test.exs
defmodule Vivo.DiffTest do
  use ExUnit.Case, async: true
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

## Benchmark

```elixir
# bench/connections.exs
# Run with: mix run bench/connections.exs
defmodule Vivo.Bench.Connections do
  @target_connections 10_000
  @duration_s 60

  def run do
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
def main do
  IO.puts("[Vivo.Channel] GenServer demo")
  :ok
end

```

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

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
