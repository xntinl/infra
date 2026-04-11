# Alternative Actor Framework

**Project**: `typed_actors` — an Elixir actor framework with typed message protocols and location transparency

---

## Project context

You are building `typed_actors`, an actor framework for Elixir with a deliberately different API from OTP's GenServer. The framework enforces typed message protocols at the macro level, provides location transparency via `ActorRef`, supports hot state update, and benchmarks against native GenServer.

Project structure:

```
typed_actors/
├── lib/
│   └── typed_actors/
│       ├── application.ex           # framework supervisor
│       ├── actor.ex                 # core macro: use TypedActors.Actor, receive_message DSL
│       ├── actor_ref.ex             # ActorRef abstraction: local or remote delivery
│       ├── dispatch.ex              # message dispatch: struct-based routing
│       ├── supervisor.ex            # ActorSupervisor: one_for_one, one_for_all, rest_for_one
│       ├── hot_update.ex            # runtime behavior swap via :sys protocol
│       └── registry.ex              # name → ActorRef mapping (leverages Nexus.Registry)
├── test/
│   └── typed_actors/
│       ├── protocol_test.exs        # typed dispatch, UnknownMessageError
│       ├── actor_ref_test.exs       # local and remote delivery
│       ├── supervision_test.exs     # restart strategies
│       ├── hot_update_test.exs      # behavior swap without dropping messages
│       └── benchmark_test.exs       # throughput within 30% of GenServer
├── bench/
│   └── actors_bench.exs
└── mix.exs
```

---

## The problem

OTP's GenServer handles all message types through a single `handle_call/3` function. This is flexible but untyped — you cannot express at the module level which messages an actor accepts. A caller can send any term; the only feedback is a runtime error. Akka Typed and Proto.Actor solve this by requiring actors to declare their accepted message types. Any send of an undeclared type is rejected.

The second problem is location transparency. `GenServer.call(pid, msg)` only works if `pid` is local or you know the node. An `ActorRef` abstracts over locality — the framework routes the message to the correct node without the caller knowing.

---

## Why this design

**`receive_message` macro for typed dispatch**: the macro accumulates declared message types using `Module.register_attribute/2` in `@before_compile`. At compilation, it generates a `dispatch/2` function with a pattern-match clause per declared message type, plus a catch-all that raises `Actor.UnknownMessageError`. This converts a runtime discovery (wrong message type) into a clearly reported runtime error with the actor's declared types listed.

**Struct-based message types**: each message type is an Elixir struct module (`%CreateUser{}`, `%DeleteUser{}`). Pattern matching on structs is idiomatic Elixir, compiles to efficient BEAM pattern matching, and gives you type-checking-like feedback because you cannot accidentally pass the wrong struct type.

**`ActorRef` over raw PID**: the `ActorRef` struct holds either `{:local, pid}` or `{:remote, node, name}`. `ActorRef.send/2` pattern-matches on the variant and uses `send/2` or `:rpc.cast/4` accordingly. The caller never sees the distinction.

**Hot update via `:sys` protocol**: OTP's `:sys` module defines `code_change/3` for hot code upgrades. Your framework wraps this to allow swapping the dispatch module at runtime. The actor's state is preserved; subsequent messages go through the new module's `dispatch/2`. In-flight messages are handled by whichever dispatch module the actor is currently running.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new typed_actors --sup
cd typed_actors
mkdir -p lib/typed_actors test/typed_actors bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Actor macro

```elixir
# lib/typed_actors/actor.ex
defmodule TypedActors.Actor do
  @moduledoc """
  Macro for defining typed actors.

  Usage:
    defmodule MyActor do
      use TypedActors.Actor

      receive_message %CreateUser{} = msg do
        # handle create user
        {:ok, state}
      end

      receive_message %DeleteUser{} = msg do
        # handle delete user
        {:ok, state}
      end
    end

  Sending a message of any other type raises Actor.UnknownMessageError.
  """

  defmacro __using__(_opts) do
    quote do
      use GenServer
      import TypedActors.Actor, only: [receive_message: 2]
      @declared_messages []
      @before_compile TypedActors.Actor
    end
  end

  defmacro receive_message(pattern, do: body) do
    msg_type = extract_struct_type(pattern)

    quote do
      @declared_messages [unquote(msg_type) | @declared_messages]
      @handler_clauses {unquote(Macro.escape(pattern)), unquote(Macro.escape(body))}
    end
  end

  defp extract_struct_type({:=, _, [{:%, _, [type, _]}, _]}), do: type
  defp extract_struct_type({:%, _, [type, _]}), do: type
  defp extract_struct_type(other), do: other

  defmacro __before_compile__(env) do
    declared = Module.get_attribute(env.module, :declared_messages) |> Enum.uniq()
    clauses = Module.get_attribute(env.module, :handler_clauses) |> Enum.reverse()

    dispatch_clauses =
      Enum.map(clauses, fn {pattern, body} ->
        quote do
          def dispatch(unquote(pattern), var!(state)) do
            unquote(body)
          end
        end
      end)

    quote do
      unquote_splicing(dispatch_clauses)

      def dispatch(msg, _state) do
        raise TypedActors.Actor.UnknownMessageError,
          message: "Unknown message type: #{inspect(msg.__struct__)}",
          declared_types: unquote(declared)
      end

      def handle_call(msg, _from, state) when is_struct(msg) do
        case dispatch(msg, state) do
          {:reply, reply, new_state} -> {:reply, reply, new_state}
          {:noreply, new_state} -> {:noreply, new_state}
        end
      end

      def handle_cast(msg, state) when is_struct(msg) do
        case dispatch(msg, state) do
          {:reply, _reply, new_state} -> {:noreply, new_state}
          {:noreply, new_state} -> {:noreply, new_state}
        end
      end
    end
  end
end
```

### Step 4: ActorRef

```elixir
# lib/typed_actors/actor_ref.ex
defmodule TypedActors.ActorRef do
  @moduledoc """
  Location-transparent actor reference.

  %ActorRef{location: {:local, pid}} — same BEAM node
  %ActorRef{location: {:remote, node, name}} — remote BEAM node
  """

  defstruct [:location]

  def local(pid), do: %__MODULE__{location: {:local, pid}}
  def remote(node, name), do: %__MODULE__{location: {:remote, node, name}}

  @doc "Sends a message to the actor without waiting for a reply."
  @spec send(%__MODULE__{}, term()) :: :ok
  def send(%__MODULE__{location: {:local, pid}}, message) do
    GenServer.cast(pid, message)
    :ok
  end
  def send(%__MODULE__{location: {:remote, node, name}}, message) do
    GenServer.cast({name, node}, message)
    :ok
  end

  @doc "Sends a message and waits for a reply. Same semantics as GenServer.call/3."
  @spec call(%__MODULE__{}, term(), non_neg_integer()) :: term()
  def call(%__MODULE__{location: {:local, pid}}, message, timeout \\ 5_000) do
    GenServer.call(pid, message, timeout)
  end
  def call(%__MODULE__{location: {:remote, node, name}}, message, timeout) do
    :erpc.call(node, GenServer, :call, [{name, node}, message, timeout], timeout)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/typed_actors/protocol_test.exs
defmodule TypedActors.ProtocolTest do
  use ExUnit.Case, async: true

  defmodule CreateUser,  do: defstruct [:name]
  defmodule DeleteUser,  do: defstruct [:id]
  defmodule UnknownMsg,  do: defstruct [:data]

  defmodule UserActor do
    use TypedActors.Actor

    def init(_), do: {:ok, %{}}

    receive_message %CreateUser{name: name} do
      {:reply, {:created, name}, Map.put(state, name, true)}
    end

    receive_message %DeleteUser{id: id} do
      {:reply, {:deleted, id}, Map.delete(state, id)}
    end
  end

  setup do
    {:ok, pid} = GenServer.start_link(UserActor, nil)
    ref = TypedActors.ActorRef.local(pid)
    {:ok, ref: ref}
  end

  test "declared message types are handled", %{ref: ref} do
    assert {:created, "Alice"} = TypedActors.ActorRef.call(ref, %CreateUser{name: "Alice"})
  end

  test "undeclared message type raises UnknownMessageError", %{ref: ref} do
    assert_raise TypedActors.Actor.UnknownMessageError, fn ->
      TypedActors.ActorRef.call(ref, %UnknownMsg{data: "x"})
    end
  end

  test "error message includes declared types", %{ref: ref} do
    try do
      TypedActors.ActorRef.call(ref, %UnknownMsg{data: "x"})
    rescue
      e in TypedActors.Actor.UnknownMessageError ->
        assert CreateUser in e.declared_types
        assert DeleteUser in e.declared_types
    end
  end
end
```

```elixir
# test/typed_actors/hot_update_test.exs
defmodule TypedActors.HotUpdateTest do
  use ExUnit.Case, async: true

  defmodule Ping, do: defstruct []
  defmodule Pong, do: defstruct []

  defmodule ActorV1 do
    use TypedActors.Actor
    def init(_), do: {:ok, :v1}
    receive_message %Ping{} do
      {:reply, :v1_pong, state}
    end
  end

  defmodule ActorV2 do
    use TypedActors.Actor
    def init(_), do: {:ok, :v2}
    receive_message %Ping{} do
      {:reply, :v2_pong, state}
    end
  end

  test "hot update swaps behavior without losing state" do
    {:ok, pid} = GenServer.start_link(ActorV1, nil)
    ref = TypedActors.ActorRef.local(pid)

    assert :v1_pong = TypedActors.ActorRef.call(ref, %Ping{})

    TypedActors.Actor.update_behavior(pid, ActorV2)

    # Behavior is now V2, state is preserved
    assert :v2_pong = TypedActors.ActorRef.call(ref, %Ping{})
  end

  test "in-flight messages are not dropped during hot update" do
    {:ok, pid} = GenServer.start_link(ActorV1, nil)
    ref = TypedActors.ActorRef.local(pid)

    # Send a batch of messages, update behavior mid-flight
    tasks = for _ <- 1..50, do: Task.async(fn -> TypedActors.ActorRef.call(ref, %Ping{}) end)
    TypedActors.Actor.update_behavior(pid, ActorV2)
    results = Task.await_many(tasks, 5_000)

    # All replies must be valid (either :v1_pong or :v2_pong — no errors)
    assert Enum.all?(results, fn r -> r in [:v1_pong, :v2_pong] end)
  end
end
```

### Step 6: Run the tests

```bash
mix test test/typed_actors/ --trace
```

### Step 7: Benchmark

```elixir
# bench/actors_bench.exs
defmodule Noop, do: defstruct []

defmodule TypedNoop do
  use TypedActors.Actor
  def init(_), do: {:ok, 0}
  receive_message %Noop{} do
    {:reply, :ok, state + 1}
  end
end

defmodule OTPNoop do
  use GenServer
  def init(_), do: {:ok, 0}
  def handle_call(:noop, _from, n), do: {:reply, :ok, n + 1}
end

{:ok, typed} = GenServer.start_link(TypedNoop, nil)
{:ok, otp}   = GenServer.start_link(OTPNoop, nil)

typed_ref = TypedActors.ActorRef.local(typed)

Benchee.run(
  %{
    "TypedActors — call"  => fn -> TypedActors.ActorRef.call(typed_ref, %Noop{}) end,
    "GenServer — call"    => fn -> GenServer.call(otp, :noop) end
  },
  parallel: 1,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Target: TypedActors throughput within 30% of GenServer for calls, 20% for casts.

---

## Trade-off analysis

| Aspect | TypedActors (your impl) | OTP GenServer | Akka Typed (reference) |
|--------|------------------------|---------------|----------------------|
| Message type enforcement | compile-time accumulation + runtime check | none | compile-time (Scala types) |
| Location transparency | ActorRef (local/remote) | PID (local) or explicit node call | ActorRef (automatic) |
| Hot update | `:sys` code_change wrapper | `code_change/3` callback | not supported |
| Throughput overhead | dispatch layer | native | JVM overhead |
| Supervision | OTP Supervisor delegation | OTP native | akka.actor.typed.Behavior |

Document your measured overhead relative to GenServer in the benchmark results section of the bench file.

---

## Common production mistakes

**1. Accumulating `@declared_messages` after `use TypedActors.Actor`**
The `Module.register_attribute/2` accumulation in `__using__` runs when `use` is expanded. If the developer calls `receive_message` before `use TypedActors.Actor`, the attribute is not yet declared. Document that `use TypedActors.Actor` must come first.

**2. Hot update without draining the mailbox**
When you call `update_behavior/2`, messages already in the GenServer mailbox will be handled by the new dispatch module. If the new module does not declare a type that the old module did, those messages raise `UnknownMessageError`. Document the invariant: both old and new modules must handle the same message types, or the update must be coordinated with all senders.

**3. Remote ActorRef using `:rpc.cast` instead of `GenServer.cast`**
`:rpc.cast/4` calls `apply(module, function, args)` on the remote node. Calling `GenServer.cast(name, msg)` via `:rpc.cast` works but introduces an extra RPC layer. A cleaner approach is `GenServer.cast({name, node}, msg)`, which uses Elixir's distributed GenServer primitives directly.

**4. UnknownMessageError not listing declared types**
The error message must include the list of declared types to be useful. Without this, the developer sees `UnknownMessageError: got %BadMsg{}` and has no idea what types are valid. Accumulate the types in `@declared_messages` and embed them in the error struct.

---

## Resources

- [Akka Typed documentation](https://doc.akka.io/docs/akka/current/typed/index.html) — reference for typed actor API design
- [Proto.Actor design](https://proto.actor/docs/) — alternative typed actor model
- [Erlang `:sys` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/sys.erl) and [`:gen` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen.erl) — the `:sys` protocol underpinning hot updates
- Armstrong, J. — *Programming Erlang* — Chapter 14 (Behaviors)
- [OTP hot code upgrade guide](https://www.erlang.org/doc/design_principles/release_handling)
