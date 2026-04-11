# 45. Build a Game Engine with ECS Architecture

## Context

Your team is prototyping a game for an internal hackathon. The game runs in a terminal — no browser, no GUI library. The first attempt used a simple loop with a big map of game state, and adding a second moving enemy required forking the entire update logic. Adding a power-up broke the renderer. The code became an inheritance tree six levels deep.

The team switches to an Entity-Component-System (ECS) architecture. After the refactor: adding a new enemy type is two lines of component registration, adding a power-up is a new system with no changes to existing code, and the renderer is completely decoupled from game logic.

You will build `GameEngine`: an ECS engine with a fixed-timestep game loop, ETS-backed world state, ANSI terminal renderer, and a playable Snake game as the demo.

## Why ECS and not OOP inheritance for game objects

OOP inheritance for game objects produces a diamond-inheritance problem within three levels. An entity that is "a flying enemy that is also a projectile collector" either requires multiple inheritance (not available in Elixir) or a deeply nested struct with duplicated fields.

ECS separates data (Components) from identity (Entities) from logic (Systems). A "flying enemy" is an entity with `%Position{}`, `%Velocity{}`, `%Gravity{}`, `%Sprite{}`, and `%EnemyAI{}` components. A "projectile collector" adds `%Collider{on_collision: &collect/2}`. No inheritance. No coupling. Systems operate on archetypes (sets of component types), not on specific entity types.

## Why ETS and not a GenServer for World state

At 60 updates/second with dozens of entities and multiple systems each reading and writing components, a GenServer becomes a serialization bottleneck. Every `:ets.lookup` and `:ets.insert` takes ~100–500ns and does not require a process context switch. A GenServer call at 60 FPS with 10 systems and 100 entities would serialize ~600 calls/second through a single mailbox.

ETS with `:public` access allows all system processes to read and write concurrently. The trade-off is that ETS transactions are per-key, not multi-key atomic — but game state updates are naturally idempotent within a single frame.

## Why fixed timestep and not variable delta time

Variable delta time (use elapsed since last frame) leads to non-deterministic physics: slow frames apply larger time steps, which can tunnel through thin colliders or cause energy gain in spring simulations. Fixed timestep (always update by 16.67ms, catch up if behind) produces deterministic, reproducible physics. The standard algorithm is Glenn Fiedler's accumulator: accumulate real elapsed time, consume in fixed chunks, interpolate remaining alpha for rendering.

## Project Structure

```
game_engine/
├── mix.exs
├── lib/
│   ├── game_engine/
│   │   ├── world.ex           # ETS-backed entity-component store
│   │   ├── query.ex           # Archetype queries with component-type index
│   │   ├── engine.ex          # Game loop: fixed timestep, accumulator
│   │   ├── systems/
│   │   │   ├── physics.ex     # Velocity integration, gravity, AABB collision
│   │   │   ├── render.ex      # ANSI terminal renderer, dirty tracking
│   │   │   └── input.ex       # Raw terminal input, arrow keys, buffering
│   │   └── components/
│   │       ├── position.ex    # %Position{x, y}
│   │       ├── velocity.ex    # %Velocity{vx, vy}
│   │       ├── sprite.ex      # %Sprite{char, fg, bg}
│   │       ├── collider.ex    # %Collider{w, h, on_collision}
│   │       ├── gravity.ex     # %Gravity{g}
│   │       └── input_listener.ex  # %InputListener{keys}
│   └── games/
│       └── snake.ex           # Snake game built on top of GameEngine
├── test/
│   ├── world_test.exs
│   ├── query_test.exs
│   ├── physics_test.exs
│   ├── render_test.exs
│   └── engine_test.exs
└── lib/mix/tasks/
    └── game.ex                # mix game.snake
```

## Step 1 — Components

```elixir
defmodule GameEngine.Components.Position do
  defstruct x: 0.0, y: 0.0
end

defmodule GameEngine.Components.Velocity do
  defstruct vx: 0.0, vy: 0.0
end

defmodule GameEngine.Components.Sprite do
  @enforce_keys [:char]
  defstruct [:char, fg: 7, bg: 0]
  # fg/bg: ANSI color codes 0-7 (0=black, 1=red, 2=green, 7=white)
end

defmodule GameEngine.Components.Collider do
  defstruct w: 1, h: 1, on_collision: nil
  # on_collision: fn(self_entity_id, other_entity_id, world) -> world
end

defmodule GameEngine.Components.Gravity do
  defstruct g: 9.8
end

defmodule GameEngine.Components.InputListener do
  @enforce_keys [:keys]
  defstruct [:keys, :handler]
  # keys: list of atoms like [:arrow_up, :arrow_down, :space]
  # handler: fn(entity_id, key, world) -> world
end
```

## Step 2 — World

```elixir
defmodule GameEngine.World do
  @components_table :ecs_components
  @index_table :ecs_index

  defstruct next_id: 1, systems: []

  def new(systems \\ []) do
    :ets.new(@components_table, [:named_table, :public, :set])
    :ets.new(@index_table, [:named_table, :public, :bag])
    %__MODULE__{systems: systems}
  end

  @doc "Spawn a new entity, returning {world, entity_id}"
  def spawn_entity(%__MODULE__{next_id: id} = world) do
    {%{world | next_id: id + 1}, id}
  end

  @doc "Destroy an entity and all its components"
  def despawn(%__MODULE__{} = world, entity_id) do
    # TODO: find all component types for entity_id using @index_table
    # TODO: delete from @components_table: {entity_id, component_type}
    # TODO: delete from @index_table: {component_type, entity_id}
    world
  end

  @doc "Add a component to an entity"
  def add_component(%__MODULE__{} = world, entity_id, component) do
    component_type = component.__struct__
    # TODO: insert {entity_id, component_type} => component into @components_table
    # TODO: insert {component_type, entity_id} into @index_table (bag allows duplicates)
    world
  end

  @doc "Remove a component type from an entity"
  def remove_component(%__MODULE__{} = world, entity_id, component_type) do
    # TODO: delete {entity_id, component_type} from @components_table
    # TODO: delete {component_type, entity_id} from @index_table
    world
  end

  @doc "Get a single component for an entity"
  def get_component(entity_id, component_type) do
    case :ets.lookup(@components_table, {entity_id, component_type}) do
      [{_, component}] -> {:ok, component}
      [] -> :not_found
    end
  end

  @doc "Update a component for an entity in-place"
  def update_component(%__MODULE__{} = world, entity_id, component) do
    :ets.insert(@components_table, {{entity_id, component.__struct__}, component})
    world
  end
end
```

## Step 3 — Query

```elixir
defmodule GameEngine.Query do
  @index_table :ecs_index
  @components_table :ecs_components

  @doc """
  Return all entity_ids that have ALL the given component types.
  Returns: [{entity_id, %{component_type => component_value}}]
  """
  def query(component_types) when is_list(component_types) do
    # TODO: for each component_type, get set of entity_ids from @index_table
    # HINT: :ets.match(@index_table, {component_type, :"$1"}) |> Enum.map(fn [id] -> id end)
    # TODO: intersect all sets (entities that have ALL component types)
    # HINT: first_set = MapSet.new(ids_for_first_type)
    #       Enum.reduce(rest, first_set, fn ids, acc -> MapSet.intersection(acc, MapSet.new(ids)) end)
    # TODO: for each entity_id in result, fetch all requested components
    # TODO: return [{entity_id, %{ComponentType => value, ...}}]
  end

  @doc "Get all components for a specific entity (only the requested types)"
  def query_one(entity_id, component_types) do
    # TODO: for each type in component_types, do :ets.lookup(@components_table, {entity_id, type})
    # TODO: if any component is missing, return :not_found
    # TODO: else return {:ok, %{type => value}}
  end
end
```

## Step 4 — Physics system

```elixir
defmodule GameEngine.Systems.Physics do
  alias GameEngine.{World, Query}
  alias GameEngine.Components.{Position, Velocity, Gravity, Collider}

  @doc "Update positions and apply gravity. delta_time in seconds."
  def update(world, delta_time) do
    world
    |> integrate_velocity(delta_time)
    |> apply_gravity(delta_time)
    |> detect_collisions()
  end

  defp integrate_velocity(world, dt) do
    # TODO: query entities with [Position, Velocity]
    # TODO: for each: new_x = pos.x + vel.vx * dt, new_y = pos.y + vel.vy * dt
    # TODO: World.update_component(world, id, %Position{x: new_x, y: new_y})
    world
  end

  defp apply_gravity(world, dt) do
    # TODO: query entities with [Velocity, Gravity]
    # TODO: for each: new_vy = vel.vy + gravity.g * dt
    # TODO: World.update_component with updated Velocity
    world
  end

  defp detect_collisions(world) do
    # TODO: query entities with [Position, Collider]
    # TODO: for each pair (i, j), check AABB overlap:
    #   overlap_x = abs(pos_i.x - pos_j.x) < (col_i.w + col_j.w) / 2
    #   overlap_y = abs(pos_i.y - pos_j.y) < (col_i.h + col_j.h) / 2
    # TODO: if both overlap: call on_collision callback if present
    # HINT: use combination Enum.reduce to avoid checking (i,j) and (j,i)
    world
  end
end
```

## Step 5 — Render system

```elixir
defmodule GameEngine.Systems.Render do
  alias GameEngine.Query
  alias GameEngine.Components.{Position, Sprite}

  # Double buffer: previous frame's {col, row} => {char, fg, bg}
  @buffer_table :render_buffer

  def init do
    :ets.new(@buffer_table, [:named_table, :public, :set])
  end

  @doc "Render current frame. Only emit ANSI for changed cells."
  def update(world, _dt) do
    entities = Query.query([Position, Sprite])
    new_buffer = Map.new(entities, fn {id, %{Position => pos, Sprite => sprite}} ->
      {{trunc(pos.x), trunc(pos.y)}, {sprite.char, sprite.fg, sprite.bg}}
    end)

    old_buffer = :ets.tab2list(@buffer_table) |> Map.new()

    # TODO: find cells in old_buffer not in new_buffer → erase them (print space)
    # TODO: find cells in new_buffer different from old_buffer → print with ANSI color
    # TODO: build one IO string and call IO.write/1 ONCE (minimize syscalls)
    # TODO: update @buffer_table with new_buffer

    world
  end

  @doc "Move cursor and set color; return ANSI escape string"
  def ansi_cell(col, row, char, fg, bg) do
    # TODO: "\e[#{row};#{col}H\e[3#{fg}m\e[4#{bg}m#{char}\e[0m"
  end

  @doc "Clear the terminal and hide cursor"
  def clear_screen do
    IO.write("\e[2J\e[?25l")
  end

  @doc "Show cursor and reset terminal on exit"
  def restore_terminal do
    IO.write("\e[?25h\e[0m\e[2J\e[H")
  end
end
```

## Step 6 — Input system

```elixir
defmodule GameEngine.Systems.Input do
  use GenServer
  alias GameEngine.Components.InputListener

  # Runs in a separate process; reads from stdin in raw mode
  # Sends {:key, atom} messages to the engine process

  def start_link(engine_pid) do
    GenServer.start_link(__MODULE__, engine_pid, name: __MODULE__)
  end

  def init(engine_pid) do
    # TODO: set terminal to raw mode (platform-specific)
    # HINT: System.cmd("stty", ["-icanon", "-echo", "min", "0", "time", "0"])
    schedule_poll()
    {:ok, %{engine: engine_pid}}
  end

  def handle_info(:poll, state) do
    case :io.get_chars("", 3) do
      :eof -> {:noreply, state}
      chars ->
        key = parse_key(chars)
        if key, do: send(state.engine, {:input, key})
        schedule_poll()
        {:noreply, state}
    end
  end

  defp parse_key("\e[A"), do: :arrow_up
  defp parse_key("\e[B"), do: :arrow_down
  defp parse_key("\e[C"), do: :arrow_right
  defp parse_key("\e[D"), do: :arrow_left
  defp parse_key(" "), do: :space
  defp parse_key("q"), do: :quit
  defp parse_key("\r"), do: :enter
  defp parse_key(_), do: nil

  defp schedule_poll, do: Process.send_after(self(), :poll, 16)
end
```

## Step 7 — Engine loop

```elixir
defmodule GameEngine.Engine do
  @fixed_step_ms 16.67
  @fixed_step_s @fixed_step_ms / 1000.0

  def run(world) do
    GameEngine.Systems.Render.clear_screen()
    loop(world, 0.0, System.monotonic_time(:millisecond))
  end

  defp loop(world, accumulator, last_time) do
    receive do
      {:input, :quit} ->
        GameEngine.Systems.Render.restore_terminal()
        :ok
      {:input, key} ->
        new_world = handle_input(world, key)
        loop(new_world, accumulator, last_time)
    after
      0 ->
        now = System.monotonic_time(:millisecond)
        elapsed = now - last_time
        new_accumulator = accumulator + elapsed

        # TODO: fixed-timestep update loop
        # while accumulator >= @fixed_step_ms:
        #   world = run all systems with @fixed_step_s
        #   accumulator -= @fixed_step_ms

        # TODO: render with alpha = accumulator / @fixed_step_ms (for interpolation)

        loop(world, new_accumulator, now)
    end
  end

  defp handle_input(world, key) do
    # TODO: query entities with InputListener component
    # TODO: for each entity, if key in listener.keys: call listener.handler.(entity_id, key, world)
    world
  end
end
```

## Step 8 — Snake game

```elixir
defmodule Games.Snake do
  alias GameEngine.{World, Engine}
  alias GameEngine.Components.{Position, Sprite, InputListener}

  @board_w 40
  @board_h 20

  def start do
    world = World.new([])
    world = setup_board(world)
    {world, snake_head_id} = spawn_snake(world)
    {world, _food_id} = spawn_food(world)
    world = World.add_component(world, snake_head_id, %InputListener{
      keys: [:arrow_up, :arrow_down, :arrow_left, :arrow_right],
      handler: &handle_input/3
    })
    Engine.run(world)
  end

  defp spawn_snake(world) do
    # TODO: spawn an entity with Position at center, Sprite of "@" in green
    # TODO: store snake body positions in process dictionary or a dedicated component
    # Snake state: direction (dx, dy), body list [{x, y}]
  end

  defp spawn_food(world) do
    # TODO: spawn entity with Position at random, Sprite of "*" in red
    {world, nil}
  end

  defp handle_input(entity_id, key, world) do
    # TODO: change snake direction based on key (prevent reversing)
    # TODO: store new direction in a component or process state
    world
  end

  defp setup_board(world) do
    # TODO: spawn border entities (walls) as Sprite "#" in white around @board_w x @board_h
    world
  end
end
```

## Given tests

```elixir
# test/world_test.exs
defmodule GameEngine.WorldTest do
  use ExUnit.Case, async: false
  alias GameEngine.World
  alias GameEngine.Components.{Position, Velocity}

  setup do
    # Clean up ETS tables between tests
    try do :ets.delete(:ecs_components) rescue _ -> :ok end
    try do :ets.delete(:ecs_index) rescue _ -> :ok end
    :ok
  end

  test "spawn_entity increments id" do
    world = World.new()
    {w2, id1} = World.spawn_entity(world)
    {_w3, id2} = World.spawn_entity(w2)
    assert id2 == id1 + 1
  end

  test "add_component and get_component round-trip" do
    world = World.new()
    {world, id} = World.spawn_entity(world)
    pos = %Position{x: 3.0, y: 4.0}
    world = World.add_component(world, id, pos)
    assert {:ok, ^pos} = World.get_component(id, Position)
  end

  test "remove_component cleans up index" do
    world = World.new()
    {world, id} = World.spawn_entity(world)
    world = World.add_component(world, id, %Position{x: 1.0, y: 1.0})
    world = World.remove_component(world, id, Position)
    assert :not_found = World.get_component(id, Position)
  end

  test "despawn removes all components" do
    world = World.new()
    {world, id} = World.spawn_entity(world)
    world = World.add_component(world, id, %Position{x: 1.0, y: 1.0})
    world = World.add_component(world, id, %Velocity{vx: 1.0, vy: 0.0})
    world = World.despawn(world, id)
    assert :not_found = World.get_component(id, Position)
    assert :not_found = World.get_component(id, Velocity)
  end
end

# test/query_test.exs
defmodule GameEngine.QueryTest do
  use ExUnit.Case, async: false
  alias GameEngine.{World, Query}
  alias GameEngine.Components.{Position, Velocity, Sprite}

  setup do
    try do :ets.delete(:ecs_components) rescue _ -> :ok end
    try do :ets.delete(:ecs_index) rescue _ -> :ok end
    :ok
  end

  test "query returns only entities with all requested components" do
    world = World.new()
    {world, id1} = World.spawn_entity(world)
    {world, id2} = World.spawn_entity(world)
    {world, id3} = World.spawn_entity(world)

    world = World.add_component(world, id1, %Position{x: 1.0, y: 0.0})
    world = World.add_component(world, id1, %Velocity{vx: 1.0, vy: 0.0})
    world = World.add_component(world, id2, %Position{x: 2.0, y: 0.0})
    # id2 has no Velocity
    world = World.add_component(world, id3, %Velocity{vx: 0.0, vy: 1.0})
    # id3 has no Position

    results = Query.query([Position, Velocity])
    ids = Enum.map(results, fn {id, _} -> id end)
    assert id1 in ids
    refute id2 in ids
    refute id3 in ids
  end
end

# test/physics_test.exs
defmodule GameEngine.PhysicsTest do
  use ExUnit.Case, async: false
  alias GameEngine.{World, Query}
  alias GameEngine.Systems.Physics
  alias GameEngine.Components.{Position, Velocity, Gravity}

  setup do
    try do :ets.delete(:ecs_components) rescue _ -> :ok end
    try do :ets.delete(:ecs_index) rescue _ -> :ok end
    :ok
  end

  test "velocity integration updates position" do
    world = World.new()
    {world, id} = World.spawn_entity(world)
    world = World.add_component(world, id, %Position{x: 0.0, y: 0.0})
    world = World.add_component(world, id, %Velocity{vx: 10.0, vy: 0.0})
    _world = Physics.update(world, 1.0)
    {:ok, pos} = World.get_component(id, Position)
    assert_in_delta pos.x, 10.0, 0.01
  end

  test "gravity accelerates velocity over time" do
    world = World.new()
    {world, id} = World.spawn_entity(world)
    world = World.add_component(world, id, %Velocity{vx: 0.0, vy: 0.0})
    world = World.add_component(world, id, %Gravity{g: 9.8})
    _world = Physics.update(world, 1.0)
    {:ok, vel} = World.get_component(id, Velocity)
    assert_in_delta vel.vy, 9.8, 0.01
  end
end

# test/engine_test.exs
defmodule GameEngine.EngineTest do
  use ExUnit.Case, async: false

  test "engine runs for N frames then stops on :quit input" do
    try do :ets.delete(:ecs_components) rescue _ -> :ok end
    try do :ets.delete(:ecs_index) rescue _ -> :ok end
    try do :ets.delete(:render_buffer) rescue _ -> :ok end

    world = GameEngine.World.new()
    GameEngine.Systems.Render.init()
    engine_pid = spawn(fn -> GameEngine.Engine.run(world) end)
    Process.sleep(100)
    send(engine_pid, {:input, :quit})
    Process.sleep(50)
    assert not Process.alive?(engine_pid)
  end
end
```

## Trade-offs

| Design | Selected | Alternative | Trade-off |
|---|---|---|---|
| World storage | ETS public set | GenServer state | ETS ~100ns/lookup; GenServer adds process context switch per read |
| Component index | ETS bag per type | Linear scan | Index intersection O(min entity count); scan O(total entities × systems) |
| Render strategy | Dirty tracking (diff frames) | Full clear per frame | Full clear flickers; dirty tracking draws only changed cells |
| Input reading | Separate process polling 16ms | Blocking `:io.gets` | Blocking `:io.gets` stalls the game loop; separate process keeps loop unblocked |
| Fixed timestep | Fiedler accumulator | Variable delta | Variable delta: different hardware → different physics; fixed: deterministic |
| ANSI output | Single `IO.write` per frame | One write per cell | Batching reduces syscalls from N_changed to 1 per frame |

## Production mistakes

**Forgetting to restore terminal on crash.** If the game crashes with the terminal in raw mode, the user's shell becomes unusable (no echo, no line buffering). Always use `Process.flag(:trap_exit, true)` and restore in `terminate/2`. Also catch `System.stop/1` signals.

**Not batching ANSI output.** Calling `IO.write` for each changed cell at 60 FPS with 100 entities generates 6000 syscalls per second. Buffer all changes into a single string with `IO.iodata_to_binary/1` and write once per frame.

**ETS dirty reads in physics.** When the PhysicsSystem reads a Position, updates it, reads another entity's Position, updates it, the second entity may be reading a position that the first entity's collision callback just modified. For game-speed ECS this is acceptable but document the behavior: systems observe a mix of pre-update and post-update state within a single tick. Truly consistent reads require double-buffering the component state (read from buffer A, write to buffer B, swap at tick end).

**Ignoring BEAM scheduler preemption in the game loop.** The BEAM preempts processes at reduction boundaries (~2000 reductions). A compute-heavy physics system may be preempted mid-tick, causing the next `render` call to use partially updated state. This manifests as occasional visual glitches. Mitigate by keeping system update functions short and using `:erlang.yield/0` at safe checkpoints.

**Not randomizing food spawn position using a seeded generator.** `:rand.uniform/1` in tests produces different food positions on every run, making snapshot tests flaky. Pass an explicit seed via `Application.get_env` or a system argument for reproducible test runs.

## Resources

- Nystrom — "Game Programming Patterns" — https://gameprogrammingpatterns.com/ (free online; Chapter 12: Component)
- Fiedler — "Fix Your Timestep!" — https://gafferongames.com/post/fix_your_timestep/ (definitive accumulator algorithm)
- ANSI Escape Codes — https://en.wikipedia.org/wiki/ANSI_escape_code (cursor positioning, color codes)
- Stoyan Nikolov — "OOP Is Dead, Long Live Data-Oriented Design" (CppCon 2018) — applies to ECS design reasoning
- Erlang `:ets` documentation — https://www.erlang.org/doc/man/ets.html (`:bag`, `match`, `select`)
- Erlang `:io` documentation — https://www.erlang.org/doc/man/io.html (`get_chars`, `setopts`)
