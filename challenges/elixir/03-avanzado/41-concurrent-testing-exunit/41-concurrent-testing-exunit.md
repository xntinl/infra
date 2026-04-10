# 41. Testing Concurrente y Aislamiento de Tests en ExUnit

**Difficulty**: Avanzado

## Prerequisites
- Dominio de ExUnit (setup, teardown, tags, fixtures)
- Experiencia con GenServer, ETS y supervisión OTP
- Comprensión de `async: true` en ExUnit y sus implicaciones
- Familiaridad con Ecto y el patrón Sandbox (para la sección de contexto)

## Learning Objectives
After completing this exercise, you will be able to:
- Convertir test suites lentas a `async: true` identificando y resolviendo conflictos de estado compartido
- Arrancar GenServers en tests con `start_supervised!/1` y garantizar cleanup automático
- Identificar y resolver conflictos de named ETS tables entre tests concurrentes
- Usar `ExUnit.Callbacks.on_exit/1` para cleanup de recursos globales
- Aplicar estrategias de naming único para evitar colisiones entre tests
- Entender cuándo `async: false` es la decisión correcta

## Concepts

### Por qué async: true importa

Sin `async: true`, ExUnit ejecuta tests en serie, uno a uno. Con `async: true`, tests de diferentes módulos se ejecutan en paralelo. La diferencia en tiempo puede ser de 10x en suites grandes.

```
# Suite con async: false — serial
[Test A] → [Test B] → [Test C] → [Test D]  = 4 segundos

# Suite con async: true — paralelo  
[Test A]
[Test B]  } = 1 segundo
[Test C]
[Test D]
```

Pero `async: true` solo funciona si los tests están realmente aislados. Si dos tests comparten estado global, pueden interferir — y los bugs son no-deterministas (aparecen según el orden de ejecución).

### Las fuentes de estado compartido

```elixir
# 1. Named ETS tables — problemático
:ets.new(:my_cache, [:named_table, :public])  # Mismo nombre en todos los tests

# 2. Named GenServers — problemático
GenServer.start_link(MyServer, [], name: :my_server)  # Colisión de nombre

# 3. Registry global — problemático
:global.register_name(:coordinator, self())

# 4. Application.put_env — problemático con async
Application.put_env(:my_app, :key, value)  # Modifica estado global

# 5. Archivos en disco — problemático
File.write!("/tmp/test.data", content)  # Dos tests escriben el mismo archivo
```

### start_supervised!/1: GenServers con cleanup automático

ExUnit integra supervisión en los tests:

```elixir
test "test con GenServer" do
  # start_supervised!/1 arranca el proceso bajo el supervisor de test
  # Cuando el test termina, ExUnit detiene el proceso automáticamente
  pid = start_supervised!(MyGenServer)
  assert MyGenServer.some_call(pid) == :ok
  # Al terminar el test, MyGenServer es detenido — sin cleanup manual
end

# Equivalente manual (no recomendado):
test "test con GenServer manual" do
  {:ok, pid} = GenServer.start_link(MyGenServer, [])
  on_exit(fn -> GenServer.stop(pid) end)  # Hay que recordar hacer esto
  # ...
end
```

### on_exit/1: cleanup garantizado

```elixir
test "test con recursos externos" do
  # El bloque en on_exit se ejecuta SIEMPRE al terminar el test,
  # incluso si el test falla o lanza una excepción
  on_exit(fn ->
    # Limpiar archivos, tablas ETS, registros, etc.
    :ets.delete(:my_table)
    File.rm("/tmp/test.data")
  end)

  # ...test code...
end
```

### Ecto Sandbox: el modelo de aislamiento perfecto

```elixir
# test_helper.exs
Ecto.Adapters.SQL.Sandbox.mode(MyRepo, :manual)

# En cada test:
setup do
  :ok = Ecto.Adapters.SQL.Sandbox.checkout(MyRepo)
  on_exit(fn -> Ecto.Adapters.SQL.Sandbox.checkin(MyRepo) end)
end
# Cada test tiene su propia transacción que hace rollback al terminar
# → sin datos persistentes entre tests → async: true seguro para DB
```

## Exercises

### Exercise 1: Convertir Suite Lenta a async — Identificar y Resolver Conflictos

Una suite de tests lenta tiene conflictos de estado. Conviértela a `async: true` resolviendo los problemas.

```elixir
# ANTES: Suite lenta y serial con conflictos de estado compartido
# Todos los tests comparten el mismo caché con nombre fijo

defmodule SlowCacheTest do
  use ExUnit.Case, async: false  # ← Problema: serial innecesario

  # Módulo bajo test — usa ETS con nombre fijo
  defmodule Cache do
    @table :global_cache  # ← Nombre fijo: problemático para async

    def start do
      :ets.new(@table, [:named_table, :public, :set])
    end

    def put(key, value), do: :ets.insert(@table, {key, value})
    def get(key) do
      case :ets.lookup(@table, key) do
        [{^key, val}] -> {:ok, val}
        []            -> {:error, :not_found}
      end
    end
    def clear, do: :ets.delete_all_objects(@table)
  end

  # Problema: todos los tests comparten el mismo :global_cache
  setup do
    Cache.start()
    on_exit(fn -> Cache.clear() end)  # ¿pero quién llama start() la primera vez?
    :ok
  end

  test "put y get básico" do
    Cache.put(:key1, "value1")
    assert {:ok, "value1"} = Cache.get(:key1)
  end

  test "get de key inexistente" do
    # Si otro test pone :key1 antes de que este test limpie, puede interferir
    assert {:error, :not_found} = Cache.get(:nonexistent)
  end

  test "clear elimina todos los valores" do
    Cache.put(:a, 1)
    Cache.put(:b, 2)
    Cache.clear()
    assert {:error, :not_found} = Cache.get(:a)
  end
end

# =====================================================================
# DESPUÉS: Refactored con unique names — async: true seguro

defmodule FastCacheTest do
  use ExUnit.Case, async: true  # ← Ahora seguro

  defmodule Cache do
    # Recibe el nombre de la tabla como parámetro — sin estado global
    def start(table_name) do
      :ets.new(table_name, [:named_table, :public, :set])
      table_name
    end

    def put(table, key, value), do: :ets.insert(table, {key, value})

    def get(table, key) do
      case :ets.lookup(table, key) do
        [{^key, val}] -> {:ok, val}
        []            -> {:error, :not_found}
      end
    end

    def clear(table), do: :ets.delete_all_objects(table)
    def stop(table),  do: :ets.delete(table)
  end

  setup do
    # TODO: Generar un nombre único para cada test
    # Pista: usar make_ref() y convertirlo a átomo, o usar el PID del test
    table_name = :"cache_#{System.unique_integer([:positive])}"
    cache = Cache.start(table_name)

    # TODO: Cleanup automático al terminar el test
    on_exit(fn -> Cache.stop(cache) end)

    {:ok, cache: cache}
  end

  test "put y get básico", %{cache: cache} do
    # TODO: Usar cache (nombre de tabla) en vez de nombre fijo
    Cache.put(cache, :key1, "value1")
    assert {:ok, "value1"} = Cache.get(cache, :key1)
  end

  test "get de key inexistente", %{cache: cache} do
    # Cada test tiene su propio cache — no hay interferencia
    assert {:error, :not_found} = Cache.get(cache, :nonexistent)
  end

  test "clear elimina todos los valores", %{cache: cache} do
    Cache.put(cache, :a, 1)
    Cache.put(cache, :b, 2)
    Cache.clear(cache)
    assert {:error, :not_found} = Cache.get(cache, :a)
  end

  # Test adicional para verificar aislamiento real
  test "modificaciones en un test no afectan a otro", %{cache: cache} do
    Cache.put(cache, :shared_key, "my_value")
    # Si los tests no están aislados, otro test podría sobrescribir este valor
    :timer.sleep(10)  # Dar tiempo a otros tests para interferir (si no están aislados)
    assert {:ok, "my_value"} = Cache.get(cache, :shared_key)
  end
end

# Verificar la mejora de performance:
# mix test test/slow_cache_test.exs --seed 12345    → comparar tiempo
# mix test test/fast_cache_test.exs --seed 12345    → debería ser más rápido
```

**Hints**:
- `System.unique_integer([:positive])` genera un entero único por proceso — ideal para nombres únicos de tablas ETS
- `:ets.new` puede crear tablas sin nombre (sin `:named_table`) — considera si realmente necesitas named tables o si puedes pasar el table reference directamente
- El `on_exit` callback se ejecuta en el proceso de cleanup de ExUnit, no en el proceso del test — asegúrate de que el código de cleanup no dependa de estado del proceso del test

**One possible solution** (sparse):
```elixir
# Setup con nombre único:
setup do
  table_name = :"cache_test_#{System.unique_integer([:positive])}"
  cache = Cache.start(table_name)
  on_exit(fn -> :ets.delete(table_name) end)
  {:ok, cache: cache}
end

# Alternativamente, sin named_table:
setup do
  table = :ets.new(:cache, [:public, :set])  # Sin :named_table
  on_exit(fn -> :ets.delete(table) end)
  {:ok, cache: table}
end
```

---

### Exercise 2: start_supervised! — GenServer con Lifecycle Controlado por ExUnit

Arrancar GenServers en tests usando el supervisor de ExUnit para cleanup automático.

```elixir
defmodule CounterServer do
  @moduledoc "GenServer simple para testear start_supervised!"
  use GenServer

  def start_link(opts \\ []) do
    initial = Keyword.get(opts, :initial, 0)
    name    = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, initial, name: name)
  end

  def increment(server \\ __MODULE__), do: GenServer.cast(server, :increment)
  def decrement(server \\ __MODULE__), do: GenServer.cast(server, :decrement)
  def value(server \\ __MODULE__),     do: GenServer.call(server, :value)
  def reset(server \\ __MODULE__),     do: GenServer.cast(server, :reset)

  def init(initial), do: {:ok, initial}

  def handle_call(:value, _from, count),  do: {:reply, count, count}
  def handle_cast(:increment, count),     do: {:noreply, count + 1}
  def handle_cast(:decrement, count),     do: {:noreply, count - 1}
  def handle_cast(:reset, _count),        do: {:noreply, 0}
end

defmodule StartSupervisedTest do
  use ExUnit.Case, async: true

  test "start_supervised! arranca el GenServer y cleanup es automático" do
    # TODO: Usar start_supervised! con un nombre único para evitar conflictos
    # Pista: el spec de child puede incluir :name
    name = :"counter_#{System.unique_integer([:positive])}"
    pid = start_supervised!({CounterServer, [name: name, initial: 10]})

    assert is_pid(pid)
    assert Process.alive?(pid)

    # TODO: Verificar que el valor inicial es 10
    assert CounterServer.value(name) == 10

    # Al terminar el test, ExUnit detiene el proceso automáticamente
    # No necesitamos on_exit manual
  end

  test "start_supervised! permite múltiples servidores en el mismo test" do
    # TODO: Arrancar dos CounterServer con distintos nombres
    name1 = :"counter_a_#{System.unique_integer([:positive])}"
    name2 = :"counter_b_#{System.unique_integer([:positive])}"

    _pid1 = start_supervised!({CounterServer, [name: name1]})
    _pid2 = start_supervised!({CounterServer, [name: name2]})

    CounterServer.increment(name1)
    CounterServer.increment(name1)
    CounterServer.increment(name1)

    CounterServer.increment(name2)

    # TODO: Verificar que los contadores son independientes
    assert CounterServer.value(name1) == 3
    assert CounterServer.value(name2) == 1
  end

  test "start_supervised! en setup — servidor disponible en todos los tests del módulo" do
    # Este test demuestra que start_supervised! funciona en setup también
    # (ver el setup del módulo de abajo)
    :ok
  end

  test "stop_supervised! detiene el proceso explícitamente" do
    name = :"counter_stop_#{System.unique_integer([:positive])}"
    pid = start_supervised!({CounterServer, [name: name]})

    CounterServer.increment(name)
    assert CounterServer.value(name) == 1

    # TODO: Detener el proceso con stop_supervised!/1
    stop_supervised!(CounterServer)
    # Verificar que el proceso ya no existe
    refute Process.alive?(pid)

    # Después de stop, el nombre ya no está registrado
    assert :undefined == :erlang.whereis(name)
  end

  test "si el GenServer crashea, el test debería manejarlo" do
    name = :"crashing_counter_#{System.unique_integer([:positive])}"
    _pid = start_supervised!({CounterServer, [name: name]})

    # El proceso está bajo el supervisor del test
    # Si crashea, ExUnit puede detectarlo
    initial = CounterServer.value(name)
    assert initial == 0

    # En producción, aquí podrías verificar que el supervisor reinicia el proceso
    # start_supervised! usa restart: :temporary por defecto — no reinicia
  end
end

defmodule StartSupervisedInSetupTest do
  use ExUnit.Case, async: true

  # Arrancar el servidor en setup — disponible en todos los tests
  setup do
    name = :"shared_counter_#{System.unique_integer([:positive])}"
    _pid = start_supervised!({CounterServer, [name: name]})
    {:ok, counter: name}
  end

  test "test 1 usa el contador del setup", %{counter: counter} do
    CounterServer.increment(counter)
    CounterServer.increment(counter)
    assert CounterServer.value(counter) == 2
  end

  test "test 2 tiene su propio contador — no comparte con test 1", %{counter: counter} do
    # Cada test tiene su propio setup → su propio contador en :ok, counter: name
    assert CounterServer.value(counter) == 0  # No contaminado por test 1
    CounterServer.increment(counter)
    assert CounterServer.value(counter) == 1
  end
end
```

**Hints**:
- `start_supervised!/1` acepta un child spec o un módulo (que implementa `child_spec/1`) — el segundo argumento es la spec del hijo
- `stop_supervised!/1` toma el mismo `id` que usaste en `start_supervised!` — por defecto, el id es el módulo. Si arrancas múltiples del mismo módulo, necesitas especificar un id único
- Cada `setup` block crea un proceso de test nuevo — los recursos creados con `start_supervised!` son locales a ese test, no compartidos

**One possible solution** (sparse):
```elixir
# start_supervised! con nombre único:
pid = start_supervised!({CounterServer, [name: name, initial: 10]})
assert CounterServer.value(name) == 10

# stop_supervised! — usa el id, no el módulo directamente:
# Si el child spec tiene un id explícito:
start_supervised!({CounterServer, [name: name]}, id: :my_counter)
stop_supervised!(:my_counter)

# Si usas el módulo como id (default):
stop_supervised!(CounterServer)
```

---

### Exercise 3: ETS en Tests — Conflictos entre Tests y Soluciones

Named ETS tables son la fuente más común de fallos no-deterministas en tests async. Aquí el problema completo y sus soluciones.

```elixir
# Módulo de producción con ETS named table — como viene "de fábrica"
defmodule SessionStore do
  @moduledoc "Almacén de sesiones en ETS"

  @table :session_store

  def init do
    if :ets.whereis(@table) == :undefined do
      :ets.new(@table, [:named_table, :public, :set])
    end
    :ok
  end

  def put_session(token, data) do
    :ets.insert(@table, {token, data, System.system_time(:second)})
    {:ok, token}
  end

  def get_session(token) do
    case :ets.lookup(@table, token) do
      [{^token, data, _ts}] -> {:ok, data}
      []                    -> {:error, :session_not_found}
    end
  end

  def delete_session(token) do
    :ets.delete(@table, token)
    :ok
  end

  def all_sessions do
    :ets.tab2list(@table)
  end
end

# =====================================================================
# PROBLEMA: Tests con async: true que comparten :session_store
# =====================================================================

defmodule BrokenSessionTest do
  use ExUnit.Case, async: true  # ← Con async: true, ESTO ROMPERÁ

  setup do
    SessionStore.init()  # Si el test A ya lo creó, esto no falla (whereis check)
    # Pero clear es necesario para aislar los tests
    :ets.delete_all_objects(:session_store)
    :ok
  end

  test "put y get de sesión" do
    {:ok, token} = SessionStore.put_session("token_A", %{user_id: 1})
    # Si otro test concurrente inserta entre put y get, el comportamiento es raro
    assert {:ok, %{user_id: 1}} = SessionStore.get_session(token)
  end

  test "sesión inexistente" do
    # Si otro test insertó "missing_token", este test falla aleatoriamente
    assert {:error, :session_not_found} = SessionStore.get_session("missing_token")
  end
end

# =====================================================================
# SOLUCIÓN 1: Unique table names por test
# =====================================================================

defmodule SessionStoreV2 do
  @moduledoc "SessionStore con tabla configurable — testeable de forma aislada"

  def init(table_name) do
    :ets.new(table_name, [:named_table, :public, :set])
    table_name
  end

  def put_session(table, token, data) do
    :ets.insert(table, {token, data, System.system_time(:second)})
    {:ok, token}
  end

  def get_session(table, token) do
    case :ets.lookup(table, token) do
      [{^token, data, _ts}] -> {:ok, data}
      []                    -> {:error, :session_not_found}
    end
  end

  def delete_session(table, token) do
    :ets.delete(table, token)
    :ok
  end
end

defmodule FixedSessionTest do
  use ExUnit.Case, async: true  # ← Ahora seguro

  setup do
    # TODO: Crear tabla con nombre único para este test
    table = :"sessions_#{System.unique_integer([:positive])}"
    store = SessionStoreV2.init(table)

    # TODO: Cleanup con on_exit
    on_exit(fn -> :ets.delete(table) end)

    {:ok, store: store}
  end

  test "put y get de sesión", %{store: store} do
    {:ok, _} = SessionStoreV2.put_session(store, "tok_1", %{user_id: 42})
    assert {:ok, %{user_id: 42}} = SessionStoreV2.get_session(store, "tok_1")
  end

  test "sesión inexistente", %{store: store} do
    assert {:error, :session_not_found} =
      SessionStoreV2.get_session(store, "nonexistent_token")
  end

  test "delete de sesión", %{store: store} do
    SessionStoreV2.put_session(store, "tok_2", %{user_id: 99})
    SessionStoreV2.delete_session(store, "tok_2")
    assert {:error, :session_not_found} =
      SessionStoreV2.get_session(store, "tok_2")
  end

  test "múltiples sesiones aisladas de otros tests", %{store: store} do
    tokens = Enum.map(1..10, fn i ->
      {:ok, token} = SessionStoreV2.put_session(store, "tok_#{i}", %{id: i})
      token
    end)

    # TODO: Verificar que hay exactamente 10 sesiones en este store
    all = :ets.tab2list(store)
    assert length(all) == 10

    # Verificar que cada token es accesible
    Enum.each(tokens, fn token ->
      assert {:ok, _} = SessionStoreV2.get_session(store, token)
    end)
  end
end

# =====================================================================
# SOLUCIÓN 2: async: false con cleanup global (cuando V1 no se puede cambiar)
# =====================================================================

defmodule LegacySessionTest do
  # Cuando no puedes cambiar el módulo de producción (legacy code),
  # usa async: false con cleanup estricto
  use ExUnit.Case, async: false

  setup do
    # Asegurar que la tabla existe y está limpia
    case :ets.whereis(:session_store) do
      :undefined -> :ets.new(:session_store, [:named_table, :public, :set])
      _          -> :ets.delete_all_objects(:session_store)
    end

    on_exit(fn ->
      # Limpiar al terminar — para el siguiente test
      if :ets.whereis(:session_store) != :undefined do
        :ets.delete_all_objects(:session_store)
      end
    end)

    :ok
  end

  test "legacy: put y get básico" do
    SessionStore.put_session("legacy_token", %{user: "alice"})
    assert {:ok, %{user: "alice"}} = SessionStore.get_session("legacy_token")
  end

  test "legacy: todas las sesiones" do
    SessionStore.put_session("t1", %{a: 1})
    SessionStore.put_session("t2", %{b: 2})
    all = SessionStore.all_sessions()
    assert length(all) == 2
  end
end

# =====================================================================
# DEMOSTRACIÓN: Problema de on_exit en proceso incorrecto
# =====================================================================

defmodule OnExitDemoTest do
  use ExUnit.Case, async: true

  test "on_exit se ejecuta en proceso separado — cuidado con captures" do
    # TRAMPITA: on_exit se ejecuta en el proceso de cleanup de ExUnit,
    # no en el proceso del test. Variables de proceso NO están disponibles.

    counter = :counters.new(1, [])

    on_exit(fn ->
      # Este código corre en proceso de cleanup
      count = :counters.get(counter, 1)
      # :counters es una referencia — sí está disponible en el closure
      IO.puts("on_exit: contador = #{count}")
    end)

    :counters.add(counter, 1, 1)
    :counters.add(counter, 1, 1)
    # count ahora es 2 — on_exit verá el valor correcto porque counter
    # es una referencia (no estado de proceso)
    assert :counters.get(counter, 1) == 2
  end

  test "on_exit con ETS: la tabla debe existir en on_exit" do
    # TODO: Crear una tabla ETS, usarla, y limpiarla en on_exit
    table = :"demo_ets_#{System.unique_integer([:positive])}"
    :ets.new(table, [:named_table, :public])

    on_exit(fn ->
      # La tabla ETS sobrevive a la muerte del proceso del test
      # porque ETS está en el proceso ETS, no en el proceso del test
      # PERO: si el proceso owner de la tabla muere sin heredero,
      # la tabla se destruye automáticamente (si no es :public con :heir)
      # Para named tables: limpiar explícitamente
      if :ets.whereis(table) != :undefined do
        :ets.delete(table)
      end
    end)

    :ets.insert(table, {:key, :value})
    assert [{:key, :value}] = :ets.lookup(table, :key)
  end
end
```

**Hints**:
- `System.unique_integer([:positive])` es thread-safe y genera enteros únicos en el proceso del nodo — ideal para nombres únicos en tests
- Las tablas ETS creadas con `:named_table` sin `:heir` se destruyen cuando muere el proceso propietario. En tests async, el proceso propietario puede morir antes del `on_exit` — usa `:ets.give_away/3` si necesitas que la tabla sobreviva al proceso del test
- La diferencia entre `:public` y `:protected` importa en tests concurrentes: `:public` permite escritura desde cualquier proceso, lo que puede ser necesario si el cleanup corre en un proceso diferente

**One possible solution** (sparse):
```elixir
# Setup con unique name y on_exit:
setup do
  table = :"sessions_#{System.unique_integer([:positive])}"
  store = SessionStoreV2.init(table)
  on_exit(fn ->
    if :ets.whereis(table) != :undefined, do: :ets.delete(table)
  end)
  {:ok, store: store}
end

# Verificar exactamente 10 sesiones:
all = :ets.tab2list(store)
assert length(all) == 10
```

## Common Mistakes

### Mistake 1: async: true con estado global — Fallos no-deterministas
```elixir
# ❌ Dos tests modifican :global_config al mismo tiempo
defmodule ConfigTest do
  use ExUnit.Case, async: true

  test "test A" do
    Application.put_env(:app, :mode, :strict)  # Modifica estado global
    assert Application.get_env(:app, :mode) == :strict
    # Test B puede cambiar :mode entre el put y el assert de este test
  end
end

# ✓ Opción A: async: false si el estado global es inevitable
# ✓ Opción B: Inyección de dependencias — pasar la configuración como parámetro
# ✓ Opción C: Usar Process dictionary para estado local al test
```

### Mistake 2: start_supervised! con nombre compartido entre tests
```elixir
# ❌ Dos tests async arrancan el mismo GenServer con el mismo nombre
test "test A" do
  start_supervised!(MyServer)  # Nombre: MyServer
  # ...
end
test "test B" do
  start_supervised!(MyServer)  # Nombre: MyServer — colisión con test A si son async
  # ...
end

# ✓ Usar nombres únicos
test "test A" do
  name = :"server_#{System.unique_integer()}"
  start_supervised!({MyServer, name: name})
end
```

### Mistake 3: ETS cleanup en on_exit que falla silenciosamente
```elixir
# ❌ Si la tabla ya fue borrada (por otro cleanup), :ets.delete falla
on_exit(fn -> :ets.delete(:my_table) end)
# Si el test falla Y hay otro cleanup, :ets.delete puede lanzar ArgumentError silenciosa

# ✓ Verificar antes de borrar
on_exit(fn ->
  if :ets.whereis(:my_table) != :undefined do
    :ets.delete(:my_table)
  end
end)
```

### Mistake 4: on_exit capturando estado de proceso que ya no existe
```elixir
# ❌ Process.get/1 en on_exit devuelve nil — on_exit corre en otro proceso
setup do
  Process.put(:test_data, %{key: "value"})
  on_exit(fn ->
    data = Process.get(:test_data)  # nil — el proceso del test ya murió
    IO.puts("Cleanup: #{inspect(data)}")  # "Cleanup: nil"
  end)
end

# ✓ Capturar en variables del closure, no en Process dictionary
setup do
  test_data = %{key: "value"}
  on_exit(fn ->
    IO.puts("Cleanup: #{inspect(test_data)}")  # Funciona — closure captura la variable
  end)
end
```

## Verification
```bash
# Medir la diferencia de performance
time mix test test/slow_cache_test.exs
time mix test test/fast_cache_test.exs

# Verificar aislamiento ejecutando múltiples veces
for i in 1..5, do: mix test test/fixed_session_test.exs --seed $RANDOM
# Todos deben pasar — si hay fallos no-deterministas, el aislamiento falló

# Ver tests en paralelo
mix test test/start_supervised_test.exs --trace
# Los tests deben intercalarse en el output (señal de ejecución paralela)

# Verificar cleanup con verbose
mix test test/ets_conflict_demo_test.exs --trace 2>&1 | grep -E "(ETS|session|cleanup)"
```

Checklist de verificación:
- [ ] Tests con `async: true` no modifican estado global (`Application.put_env`, `:global.register_name`)
- [ ] Named ETS tables usan nombres únicos por test (`System.unique_integer`)
- [ ] GenServers arrancados con `start_supervised!` — no con `GenServer.start_link` manual
- [ ] `on_exit` limpia todos los recursos creados en el test
- [ ] `on_exit` usa closures para capturar datos, no `Process.get/1`
- [ ] Los módulos `async: false` son los que tienen estado global inevitable

## Summary
- `async: true` requiere aislamiento real — no solo "esperamos que no interfieran"
- Las tres fuentes principales de conflicto: named ETS tables, named GenServers, `Application.put_env`
- `start_supervised!` es la forma idiomática de arrancar procesos en tests — cleanup automático garantizado
- `on_exit` es el mecanismo de cleanup — se ejecuta en proceso separado, usa closures para capturar datos
- `System.unique_integer([:positive])` es la herramienta para nombres únicos por test
- Cuando el código legacy no se puede cambiar, `async: false` con cleanup estricto es la solución pragmática

## What's Next
Has completado el nivel Avanzado de Elixir. Los próximos pasos:
- **Phoenix LiveView avanzado**: WebSockets, state management, optimistic UI
- **Distributed Elixir en producción**: Libcluster, Horde, CRDT
- **Performance tuning**: recon, observer, flamegraph en producción real
- **Elixir en la práctica**: contribuir a proyectos open source de Elixir/Erlang

## Resources
- [ExUnit — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.html)
- [ExUnit.Callbacks — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html)
- [Testing Elixir — Pragmatic Programmers](https://pragprog.com/titles/lmelixir/testing-elixir/)
- [ETS — Erlang Docs](https://www.erlang.org/doc/man/ets.html)
- [Ecto.Adapters.SQL.Sandbox — HexDocs](https://hexdocs.pm/ecto_sql/Ecto.Adapters.SQL.Sandbox.html)
