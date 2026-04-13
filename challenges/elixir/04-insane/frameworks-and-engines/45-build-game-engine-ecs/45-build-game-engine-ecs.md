# Game Engine with ECS Architecture

**Project**: `game_engine` — Entity-Component-System game engine with ETS world state and ANSI terminal renderer

## Project context

Your team is prototyping a game for an internal hackathon. The game runs in a terminal — no browser, no GUI library. The first attempt used a simple loop with a big map of game state, and adding a second moving enemy required forking the entire update logic. Adding a power-up broke the renderer.

The team switches to an Entity-Component-System (ECS) architecture. After the refactor: adding a new enemy type is two lines of component registration, adding a power-up is a new system with no changes to existing code, and the renderer is completely decoupled from game logic.

You will build `GameEngine`: an ECS engine with a fixed-timestep game loop, ETS-backed world state, ANSI terminal renderer, and a playable Snake game as the demo.

## Design decisions

**Option A — OOP-style entities with methods**
- Pros: familiar to most devs
- Cons: cache-unfriendly, hard to parallelize systems

**Option B — struct-of-arrays ECS with systems iterating over archetypes** (chosen)
- Pros: cache-friendly iteration, naturally parallel, composable
- Cons: mental model shift from OOP

→ Chose **B** because performance-critical ECS engines all converged on SoA layout — cache behavior matters more than code aesthetics.


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

## Why ECS and not OOP inheritance for game objects

OOP inheritance for game objects produces a diamond-inheritance problem within three levels. An entity that is "a flying enemy that is also a projectile collector" either requires multiple inheritance or a deeply nested struct with duplicated fields.

ECS separates data (Components) from identity (Entities) from logic (Systems). A "flying enemy" is an entity with `%Position{}`, `%Velocity{}`, `%Gravity{}`, `%Sprite{}`, and `%EnemyAI{}` components. Systems operate on archetypes (sets of component types), not on specific entity types.

## Why ETS and not a GenServer for World state

At 60 updates/second with dozens of entities and multiple systems each reading and writing components, a GenServer becomes a serialization bottleneck. Every `:ets.lookup` and `:ets.insert` takes ~100-500ns without a process context switch. ETS with `:public` access allows all system processes to read and write concurrently.

## Why fixed timestep and not variable delta time

Variable delta time leads to non-deterministic physics: slow frames apply larger time steps, which can tunnel through thin colliders. Fixed timestep (always update by 16.67ms, catch up if behind) produces deterministic, reproducible physics. The standard algorithm is Glenn Fiedler's accumulator: accumulate real elapsed time, consume in fixed chunks, interpolate remaining alpha for rendering.

## Project Structure

```
game_engine/
├── mix.exs
├── lib/
│   ├── game_engine/
│   │   ├── world.ex
│   │   ├── query.ex
│   │   ├── engine.ex
│   │   ├── systems/
│   │   │   ├── physics.ex
│   │   │   ├── render.ex
│   │   │   └── input.ex
│   │   └── components/
│   │       ├── position.ex
│   │       ├── velocity.ex
│   │       ├── sprite.ex
│   │       ├── collider.ex
│   │       ├── gravity.ex
│   │       └── input_listener.ex
│   └── games/
│       └── snake.ex
├── test/
│   ├── world_test.exs
│   ├── query_test.exs
│   ├── physics_test.exs
│   ├── render_test.exs
│   └── engine_test.exs
└── lib/mix/tasks/
    └── game.ex
```

### Step 1: Components

**Objective**: Implement the Components component required by the game engine ecs system.



### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule GameEngine.Components.Position do
  @moduledoc "2D position component."
  defstruct x: 0.0, y: 0.0
end

defmodule GameEngine.Components.Velocity do
  @moduledoc "2D velocity component."
  defstruct vx: 0.0, vy: 0.0
end

defmodule GameEngine.Components.Sprite do
  @moduledoc "Visual representation: a single character with ANSI foreground/background colors."
  @enforce_keys [:char]
  defstruct [:char, fg: 7, bg: 0]
end

defmodule GameEngine.Components.Collider do
  @moduledoc "Axis-aligned bounding box collider with optional collision callback."
  defstruct w: 1, h: 1, on_collision: nil
end

defmodule GameEngine.Components.Gravity do
  @moduledoc "Gravity component: applies downward acceleration each tick."
  defstruct g: 9.8
end

defmodule GameEngine.Components.InputListener do
  @moduledoc "Marks an entity as responsive to keyboard input."
  @enforce_keys [:keys]
  defstruct [:keys, :handler]
end
```

### Step 2: World

**Objective**: Implement the World component required by the game engine ecs system.


```elixir
defmodule GameEngine.World do
  @moduledoc """
  ETS-backed entity-component store.
  Components are stored as {entity_id, component_type} => component.
  A secondary index table maps component_type => entity_id (bag).
  """

  @components_table :ecs_components
  @index_table :ecs_index

  defstruct next_id: 1, systems: []

  @doc "Create a new world, initializing ETS tables."
  @spec new([module()]) :: %__MODULE__{}
  def new(systems \\ []) do
    if :ets.whereis(@components_table) != :undefined, do: :ets.delete(@components_table)
    if :ets.whereis(@index_table) != :undefined, do: :ets.delete(@index_table)
    :ets.new(@components_table, [:named_table, :public, :set])
    :ets.new(@index_table, [:named_table, :public, :bag])
    %__MODULE__{systems: systems}
  end

  @doc "Spawn a new entity, returning {world, entity_id}."
  @spec spawn_entity(%__MODULE__{}) :: {%__MODULE__{}, pos_integer()}
  def spawn_entity(%__MODULE__{next_id: id} = world) do
    {%{world | next_id: id + 1}, id}
  end

  @doc "Destroy an entity and all its components."
  @spec despawn(%__MODULE__{}, pos_integer()) :: %__MODULE__{}
  def despawn(%__MODULE__{} = world, entity_id) do
    component_types =
      :ets.match(@index_table, {:"$1", entity_id})
      |> Enum.map(fn [type] -> type end)

    Enum.each(component_types, fn type ->
      :ets.delete(@components_table, {entity_id, type})
      :ets.match_delete(@index_table, {type, entity_id})
    end)

    world
  end

  @doc "Add a component to an entity."
  @spec add_component(%__MODULE__{}, pos_integer(), struct()) :: %__MODULE__{}
  def add_component(%__MODULE__{} = world, entity_id, component) do
    component_type = component.__struct__
    :ets.insert(@components_table, {{entity_id, component_type}, component})
    :ets.insert(@index_table, {component_type, entity_id})
    world
  end

  @doc "Remove a component type from an entity."
  @spec remove_component(%__MODULE__{}, pos_integer(), module()) :: %__MODULE__{}
  def remove_component(%__MODULE__{} = world, entity_id, component_type) do
    :ets.delete(@components_table, {entity_id, component_type})
    :ets.match_delete(@index_table, {component_type, entity_id})
    world
  end

  @doc "Get a single component for an entity."
  @spec get_component(pos_integer(), module()) :: {:ok, struct()} | :not_found
  def get_component(entity_id, component_type) do
    case :ets.lookup(@components_table, {entity_id, component_type}) do
      [{_, component}] -> {:ok, component}
      [] -> :not_found
    end
  end

  @doc "Update a component for an entity in-place."
  @spec update_component(%__MODULE__{}, pos_integer(), struct()) :: %__MODULE__{}
  def update_component(%__MODULE__{} = world, entity_id, component) do
    :ets.insert(@components_table, {{entity_id, component.__struct__}, component})
    world
  end
end
```

### Step 3: Query

**Objective**: Evaluate search queries against the index and rank matching documents.


```elixir
defmodule GameEngine.Query do
  @moduledoc """
  Archetype queries: find all entities that have ALL the given component types.
  Uses the index table to intersect entity sets per component type.
  """

  @index_table :ecs_index
  @components_table :ecs_components

  @doc """
  Return all entities that have ALL the given component types.
  Returns: [{entity_id, %{component_type => component_value}}]
  """
  @spec query([module()]) :: [{pos_integer(), map()}]
  def query(component_types) when is_list(component_types) do
    sets =
      Enum.map(component_types, fn type ->
        :ets.match(@index_table, {type, :"$1"})
        |> Enum.map(fn [id] -> id end)
        |> MapSet.new()
      end)

    case sets do
      [] ->
        []

      [first | rest] ->
        entity_ids = Enum.reduce(rest, first, &MapSet.intersection(&2, &1))

        Enum.map(entity_ids, fn eid ->
          components =
            Map.new(component_types, fn type ->
              [{_, comp}] = :ets.lookup(@components_table, {eid, type})
              {type, comp}
            end)

          {eid, components}
        end)
    end
  end

  @doc "Get specific components for a single entity."
  @spec query_one(pos_integer(), [module()]) :: {:ok, map()} | :not_found
  def query_one(entity_id, component_types) do
    results =
      Enum.reduce_while(component_types, %{}, fn type, acc ->
        case :ets.lookup(@components_table, {entity_id, type}) do
          [{_, comp}] -> {:cont, Map.put(acc, type, comp)}
          [] -> {:halt, :not_found}
        end
      end)

    case results do
      :not_found -> :not_found
      map -> {:ok, map}
    end
  end
end
```

### Step 4: Physics system

**Objective**: Implement the Physics system component required by the game engine ecs system.


```elixir
defmodule GameEngine.Systems.Physics do
  @moduledoc """
  Physics system: velocity integration, gravity application,
  and AABB collision detection.
  """

  alias GameEngine.{World, Query}
  alias GameEngine.Components.{Position, Velocity, Gravity, Collider}

  @doc "Update positions, apply gravity, and detect collisions."
  @spec update(%World{}, float()) :: %World{}
  def update(world, delta_time) do
    world
    |> integrate_velocity(delta_time)
    |> apply_gravity(delta_time)
    |> detect_collisions()
  end

  defp integrate_velocity(world, dt) do
    Query.query([Position, Velocity])
    |> Enum.reduce(world, fn {id, %{Position => pos, Velocity => vel}}, w ->
      new_pos = %Position{x: pos.x + vel.vx * dt, y: pos.y + vel.vy * dt}
      World.update_component(w, id, new_pos)
    end)
  end

  defp apply_gravity(world, dt) do
    Query.query([Velocity, Gravity])
    |> Enum.reduce(world, fn {id, %{Velocity => vel, Gravity => grav}}, w ->
      new_vel = %Velocity{vx: vel.vx, vy: vel.vy + grav.g * dt}
      World.update_component(w, id, new_vel)
    end)
  end

  defp detect_collisions(world) do
    entities = Query.query([Position, Collider])

    pairs =
      for {id_a, comps_a} <- entities,
          {id_b, comps_b} <- entities,
          id_a < id_b,
          do: {id_a, comps_a, id_b, comps_b}

    Enum.reduce(pairs, world, fn {id_a, comps_a, id_b, comps_b}, w ->
      pos_a = comps_a[Position]
      col_a = comps_a[Collider]
      pos_b = comps_b[Position]
      col_b = comps_b[Collider]

      overlap_x = abs(pos_a.x - pos_b.x) < (col_a.w + col_b.w) / 2
      overlap_y = abs(pos_a.y - pos_b.y) < (col_a.h + col_b.h) / 2

      if overlap_x and overlap_y do
        w = if col_a.on_collision, do: col_a.on_collision.(id_a, id_b, w), else: w
        if col_b.on_collision, do: col_b.on_collision.(id_b, id_a, w), else: w
      else
        w
      end
    end)
  end
end
```

### Step 5: Render system

**Objective**: Implement the Render system component required by the game engine ecs system.


```elixir
defmodule GameEngine.Systems.Render do
  @moduledoc """
  ANSI terminal renderer with dirty tracking.
  Only emits ANSI escape codes for cells that changed since the last frame.
  Batches all output into a single IO.write call per frame.
  """

  alias GameEngine.Query
  alias GameEngine.Components.{Position, Sprite}

  @buffer_table :render_buffer

  @doc "Initialize the render buffer ETS table."
  @spec init() :: :ok
  def init do
    if :ets.whereis(@buffer_table) != :undefined, do: :ets.delete(@buffer_table)
    :ets.new(@buffer_table, [:named_table, :public, :set])
    :ok
  end

  @doc "Render current frame. Only emit ANSI for changed cells."
  @spec update(%GameEngine.World{}, float()) :: %GameEngine.World{}
  def update(world, _dt) do
    entities = Query.query([Position, Sprite])

    new_buffer =
      Map.new(entities, fn {_id, %{Position => pos, Sprite => sprite}} ->
        {{trunc(pos.x), trunc(pos.y)}, {sprite.char, sprite.fg, sprite.bg}}
      end)

    old_buffer = :ets.tab2list(@buffer_table) |> Map.new()

    erased =
      old_buffer
      |> Map.keys()
      |> Enum.reject(&Map.has_key?(new_buffer, &1))
      |> Enum.map(fn {col, row} -> ansi_cell(col, row, " ", 7, 0) end)

    drawn =
      new_buffer
      |> Enum.reject(fn {pos, cell} -> Map.get(old_buffer, pos) == cell end)
      |> Enum.map(fn {{col, row}, {char, fg, bg}} -> ansi_cell(col, row, char, fg, bg) end)

    output = IO.iodata_to_binary(erased ++ drawn)
    if byte_size(output) > 0, do: IO.write(output)

    :ets.delete_all_objects(@buffer_table)
    Enum.each(new_buffer, fn {pos, cell} -> :ets.insert(@buffer_table, {pos, cell}) end)

    world
  end

  @doc "Move cursor and set color; return ANSI escape string."
  @spec ansi_cell(integer(), integer(), String.t(), integer(), integer()) :: String.t()
  def ansi_cell(col, row, char, fg, bg) do
    "\e[#{row};#{col}H\e[3#{fg}m\e[4#{bg}m#{char}\e[0m"
  end

  @doc "Clear the terminal and hide cursor."
  @spec clear_screen() :: :ok
  def clear_screen do
    IO.write("\e[2J\e[?25l")
  end

  @doc "Show cursor and reset terminal on exit."
  @spec restore_terminal() :: :ok
  def restore_terminal do
    IO.write("\e[?25h\e[0m\e[2J\e[H")
  end
end
```

### Step 6: Input system

**Objective**: Implement the Input system component required by the game engine ecs system.


```elixir
defmodule GameEngine.Systems.Input do
  @moduledoc """
  Keyboard input system. Runs in a separate process, polling stdin
  in raw mode and sending {:input, key} messages to the engine process.
  """
  use GenServer
  alias GameEngine.Components.InputListener

  @spec start_link(pid()) :: GenServer.on_start()
  def start_link(engine_pid) do
    GenServer.start_link(__MODULE__, engine_pid, name: __MODULE__)
  end

  @impl true
  def init(engine_pid) do
    try do
      System.cmd("stty", ["-icanon", "-echo", "min", "0", "time", "0"])
    rescue
      _ -> :ok
    end

    schedule_poll()
    {:ok, %{engine: engine_pid}}
  end

  @impl true
  def handle_info(:poll, state) do
    case :io.get_chars("", 3) do
      :eof ->
        schedule_poll()
        {:noreply, state}

      chars when is_list(chars) ->
        key = parse_key(List.to_string(chars))
        if key, do: send(state.engine, {:input, key})
        schedule_poll()
        {:noreply, state}

      chars when is_binary(chars) ->
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
  defp parse_key(" " <> _), do: :space
  defp parse_key("q" <> _), do: :quit
  defp parse_key("\r" <> _), do: :enter
  defp parse_key(_), do: nil

  defp schedule_poll, do: Process.send_after(self(), :poll, 16)
end
```

### Step 7: Engine loop

**Objective**: Implement the Engine loop component required by the game engine ecs system.


```elixir
defmodule GameEngine.Engine do
  @moduledoc """
  Fixed-timestep game loop using Glenn Fiedler's accumulator pattern.
  Updates in fixed 16.67ms steps; renders after all steps are consumed.
  """

  alias GameEngine.{World, Query}
  alias GameEngine.Systems.{Physics, Render}
  alias GameEngine.Components.InputListener

  @fixed_step_ms 16.67
  @fixed_step_s @fixed_step_ms / 1000.0

  @doc "Start the game loop."
  @spec run(%World{}) :: :ok
  def run(world) do
    Render.clear_screen()
    loop(world, 0.0, System.monotonic_time(:millisecond))
  end

  defp loop(world, accumulator, last_time) do
    receive do
      {:input, :quit} ->
        Render.restore_terminal()
        :ok

      {:input, key} ->
        new_world = handle_input(world, key)
        loop(new_world, accumulator, last_time)
    after
      0 ->
        now = System.monotonic_time(:millisecond)
        elapsed = now - last_time
        new_accumulator = accumulator + elapsed

        {updated_world, remaining_acc} = consume_steps(world, new_accumulator)
        _final_world = Render.update(updated_world, remaining_acc / @fixed_step_ms)

        loop(updated_world, remaining_acc, now)
    end
  end

  defp consume_steps(world, accumulator) when accumulator >= @fixed_step_ms do
    updated_world = Physics.update(world, @fixed_step_s)
    consume_steps(updated_world, accumulator - @fixed_step_ms)
  end

  defp consume_steps(world, accumulator), do: {world, accumulator}

  defp handle_input(world, key) do
    Query.query([InputListener])
    |> Enum.reduce(world, fn {entity_id, %{InputListener => listener}}, w ->
      if key in listener.keys and listener.handler do
        listener.handler.(entity_id, key, w)
      else
        w
      end
    end)
  end
end
```

### Step 8: Snake game

**Objective**: Implement the Snake game component required by the game engine ecs system.


```elixir
defmodule Games.Snake do
  @moduledoc """
  Snake game built on top of GameEngine ECS.
  Demonstrates the engine with a playable terminal game.
  """

  alias GameEngine.{World, Engine}
  alias GameEngine.Components.{Position, Sprite, InputListener}

  @board_w 40
  @board_h 20

  @doc "Start the Snake game."
  @spec start() :: :ok
  def start do
    GameEngine.Systems.Render.init()
    world = World.new([])
    world = setup_board(world)
    {world, snake_head_id} = spawn_snake(world)
    {world, _food_id} = spawn_food(world)

    world =
      World.add_component(world, snake_head_id, %InputListener{
        keys: [:arrow_up, :arrow_down, :arrow_left, :arrow_right],
        handler: &handle_input/3
      })

    Engine.run(world)
  end

  defp spawn_snake(world) do
    {world, id} = World.spawn_entity(world)
    center_x = div(@board_w, 2) * 1.0
    center_y = div(@board_h, 2) * 1.0
    world = World.add_component(world, id, %Position{x: center_x, y: center_y})
    world = World.add_component(world, id, %Sprite{char: "@", fg: 2, bg: 0})

    Process.put(:snake_direction, {1.0, 0.0})
    Process.put(:snake_body, [{center_x, center_y}])
    Process.put(:snake_length, 3)

    {world, id}
  end

  defp spawn_food(world) do
    {world, id} = World.spawn_entity(world)
    food_x = (:rand.uniform(@board_w - 2) + 1) * 1.0
    food_y = (:rand.uniform(@board_h - 2) + 1) * 1.0
    world = World.add_component(world, id, %Position{x: food_x, y: food_y})
    world = World.add_component(world, id, %Sprite{char: "*", fg: 1, bg: 0})
    {world, id}
  end

  defp handle_input(_entity_id, key, world) do
    {dx, dy} = Process.get(:snake_direction, {1.0, 0.0})

    new_dir =
      case key do
        :arrow_up when dy != 1.0 -> {0.0, -1.0}
        :arrow_down when dy != -1.0 -> {0.0, 1.0}
        :arrow_left when dx != 1.0 -> {-1.0, 0.0}
        :arrow_right when dx != -1.0 -> {1.0, 0.0}
        _ -> {dx, dy}
      end

    Process.put(:snake_direction, new_dir)
    world
  end

  defp setup_board(world) do
    positions =
      for col <- 0..(@board_w - 1), row <- [0, @board_h - 1] do
        {col * 1.0, row * 1.0}
      end ++
        for row <- 1..(@board_h - 2), col <- [0, @board_w - 1] do
          {col * 1.0, row * 1.0}
        end

    Enum.reduce(positions, world, fn {x, y}, w ->
      {w, id} = World.spawn_entity(w)
      w = World.add_component(w, id, %Position{x: x, y: y})
      World.add_component(w, id, %Sprite{char: "#", fg: 7, bg: 0})
    end)
  end
end
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
# test/world_test.exs
defmodule GameEngine.WorldTest do
  use ExUnit.Case, async: false
  alias GameEngine.World
  alias GameEngine.Components.{Position, Velocity}

  setup do
    try do :ets.delete(:ecs_components) rescue _ -> :ok end
    try do :ets.delete(:ecs_index) rescue _ -> :ok end
    :ok
  end


  describe "World" do

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
    world = World.add_component(world, id3, %Velocity{vx: 0.0, vy: 1.0})

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
end
```


## Main Entry Point

```elixir
def main do
  IO.puts("======== 45-build-game-engine-ecs ========")
  IO.puts("Build game engine ecs")
  IO.puts("")
  
  GameEngine.Components.Position.start_link([])
  IO.puts("GameEngine.Components.Position started")
  
  IO.puts("Run: mix test")
end
```



## Benchmark

```elixir
# bench/ecs_throughput.exs
# Run with: mix escript.build && ./game_engine bench/ecs_throughput.exs

defmodule GameEngine.Bench do
  def run_benchmark do
    IO.puts("=== ECS Throughput Benchmark ===")
    IO.puts("Setup: #{10_000} entities, 4 components per entity, 3 systems (Physics, Render, Input)")
    
    # Initialize world and populate
    world = GameEngine.World.new()
    
    IO.write("Populating 10k entities... ")
    world = Enum.reduce(1..10_000, world, fn _i, w ->
      {w, eid} = GameEngine.World.spawn_entity(w)
      w = GameEngine.World.add_component(w, eid, %GameEngine.Components.Position{x: :rand.uniform() * 100, y: :rand.uniform() * 100})
      w = GameEngine.World.add_component(w, eid, %GameEngine.Components.Velocity{vx: :rand.uniform() - 0.5, vy: :rand.uniform() - 0.5})
      w = GameEngine.World.add_component(w, eid, %GameEngine.Components.Collider{w: 1, h: 1})
      GameEngine.World.add_component(w, eid, %GameEngine.Components.Sprite{char: "*", fg: 2})
    end)
    IO.puts("Done")
    
    # Warm up
    IO.write("Warmup (100 frames)... ")
    for _ <- 1..100 do
      GameEngine.Systems.Physics.update(world, 0.01667)
      GameEngine.Systems.Render.update(world, 0)
    end
    IO.puts("Done")
    
    # Benchmark: measure time to run 1000 frames
    IO.write("Benchmarking (1000 frames)... ")
    {total_us, _} = :timer.tc(fn ->
      Enum.reduce(1..1000, world, fn _frame, w ->
        GameEngine.Systems.Physics.update(w, 0.01667)
      end)
    end)
    
    total_ms = total_us / 1000.0
    per_frame_ms = total_ms / 1000.0
    entities_per_sec = 10_000_000 / total_us
    
    IO.puts("Done\n")
    IO.puts("Results:")
    IO.puts("  Total time: #{Float.round(total_ms, 2)} ms")
    IO.puts("  Per-frame:  #{Float.round(per_frame_ms, 3)} ms")
    IO.puts("  Throughput: #{Float.round(entities_per_sec, 0)} entities/sec")
    IO.puts("  Target:     <1.67 ms per frame @ 60 FPS")
    
    if per_frame_ms < 1.67 do
      IO.puts("  Result:     PASS")
    else
      IO.puts("  Result:     FAIL (#{Float.round(per_frame_ms / 1.67, 1)}x too slow)")
    end
  end
end

GameEngine.Bench.run_benchmark()
```

**Target**: <1.67ms per frame para actualizar 10k entidades @ 60 FPS (16.67ms presupuesto total).

## Key Concepts: Cache-Friendly Data Layouts and SIMD-Ready Structures

La disposición de datos en memoria determina la velocidad de caché y la capacidad de vectorización. Un motor ECS con arquitectura struct-of-arrays (SoA) es dramáticamente más rápido que array-of-structs (AoS) porque:

1. **Coherencia de caché**: Cuando un sistema (Physics) itera sobre Position y Velocity, la CPU carga ambos campos consecutivos en caché. Con AoS, los campos están dispersos entre entidades — memoria fragmentada, fallos de caché costosos.

2. **Predicción de rama**: Los datos homogéneos permiten desenrollamiento de bucles (`for i in 0..n { positions[i] += velocities[i] * dt }`). Con structs heterogéneos, el compilador no sabe qué tamaño usar per loop.

3. **Vectorización SIMD**: Una operación `_mm256_add_pd` suma 4 doubles simultáneamente. Con datos contiguos, el compilador puede generar SIMD automáticamente. Con structs entrelazados, imposible.

**En Elixir**: ETS con claves de la forma `{entity_id, ComponentType}` y un índice secundario por tipo es SoA: todas las Position están juntas en la tabla. Cuando Query itera sobre archetype, recupera 100 Position en secuencia — una sola llamada a `:ets.match/2` trae el bloque caché.

**Tradeoff**: SoA requiere más lógica para manejar jerarquías (una entidad es un map de componentes). AoS es más intuitivo pero pierde rendimiento por caché. Para juegos donde "60 FPS = 16.67ms de presupuesto", SoA es obligatorio.

---

## Trade-off analysis

| Design | Selected | Alternative | Trade-off |
|---|---|---|---|
| World storage | ETS public set | GenServer state | ETS ~100ns/lookup; GenServer adds process context switch per read |
| Component index | ETS bag per type | Linear scan | Index intersection O(min entity count); scan O(total entities x systems) |
| Render strategy | Dirty tracking (diff frames) | Full clear per frame | Full clear flickers; dirty tracking draws only changed cells |
| Input reading | Separate process polling 16ms | Blocking `:io.gets` | Blocking stalls the game loop; separate process keeps loop unblocked |
| Fixed timestep | Fiedler accumulator | Variable delta | Variable delta: different hardware = different physics; fixed: deterministic |
| ANSI output | Single `IO.write` per frame | One write per cell | Batching reduces syscalls from N_changed to 1 per frame |

## Common production mistakes

**Forgetting to restore terminal on crash.** If the game crashes with the terminal in raw mode, the user's shell becomes unusable. Always use `Process.flag(:trap_exit, true)` and restore in `terminate/2`.

**Not batching ANSI output.** Calling `IO.write` for each changed cell at 60 FPS with 100 entities generates 6000 syscalls per second. Buffer all changes into a single string and write once per frame.

**ETS dirty reads in physics.** When the PhysicsSystem reads a Position, updates it, reads another entity's Position, the second may see partially updated state. For game-speed ECS this is acceptable but document the behavior.

**Ignoring BEAM scheduler preemption in the game loop.** The BEAM preempts processes at reduction boundaries (~2000 reductions). A compute-heavy physics system may be preempted mid-tick, causing visual glitches. Keep system update functions short.

**Not randomizing food spawn using a seeded generator.** `:rand.uniform/1` in tests produces different positions on every run, making snapshot tests flaky. Pass an explicit seed for reproducible test runs.

## Reflection

Your game has 10k entities and 50 components. A system iterates over entities with `Position + Velocity + Collider` each frame. How does frame time change if you go from 3 components to 30 in the query?

## Resources

- Nystrom -- "Game Programming Patterns" -- https://gameprogrammingpatterns.com/ (Chapter 12: Component)
- Fiedler -- "Fix Your Timestep!" -- https://gafferongames.com/post/fix_your_timestep/
- ANSI Escape Codes -- https://en.wikipedia.org/wiki/ANSI_escape_code
- Erlang `:ets` documentation -- https://www.erlang.org/doc/man/ets.html
- Erlang `:io` documentation -- https://www.erlang.org/doc/man/io.html
