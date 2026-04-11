# Rate Limiter con ETS y Sliding Window

**Dificultad**: ★★★★☆
**Tiempo estimado**: 4–6 horas
**Proyecto**: `api_gateway` — construido a lo largo del nivel avanzado

---

## Contexto del proyecto

Estás construyendo `api_gateway`, un gateway HTTP interno que enruta tráfico hacia
microservicios. Ya tenés routing básico funcionando (ejercicios anteriores). El siguiente
paso es proteger los servicios downstream de clientes que abusan de la API.

Estructura del proyecto en este punto:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex          # ya existe — supervisa el RateLimiter
│       ├── router.ex               # ya existe — llama a RateLimiter.Server.check/3
│       └── rate_limiter/
│           ├── server.ex           # ← vas a implementar esto
│           └── window.ex           # ← y esto
├── test/
│   └── api_gateway/
│       └── rate_limiter_test.exs   # tests dados — deben pasar sin modificación
├── bench/
│   └── rate_limiter_bench.exs      # benchmark — correr al final
└── mix.exs
```

---

## El problema de negocio

El equipo de infra reportó que un cliente mal configurado está enviando 10,000 requests/min
hacia el servicio de pagos, degradando el tiempo de respuesta para todos los demás clientes.
Necesitás un rate limiter que:

1. Opere por `client_id` (header `X-Client-ID` del request)
2. Use semántica de **sliding window** — no fixed window
3. Sea consultado en cada request **sin convertirse en un cuello de botella**
4. Limpie automáticamente entradas expiradas — el sistema corre 24/7

---

## Por qué sliding window y no fixed window

Un rate limiter de **fixed window** resetea el contador al inicio de cada intervalo.
Si el límite es 100 req/min, un cliente malintencionado puede hacer 100 requests a las
00:59 y 100 más a las 01:00 — 200 requests en menos de 2 segundos. Ambas ventanas estaban
dentro del límite.

El **sliding window** mantiene el timestamp de cada request individual. Al verificar,
cuenta cuántos timestamps caen dentro del último `window_ms`. No hay bordes que explotar.

El costo: más memoria (guardás N timestamps en lugar de 1 contador) y más CPU por
verificación (O(n) lookup en lugar de O(1)). En la práctica, para ventanas de 60s con
límites razonables (< 1000 req/min), este costo es negligible.

---

## Por qué ETS y no el estado del GenServer

El problema de usar un `%{client_id => [timestamps]}` map en el estado del GenServer:

```
request A ──GenServer.call──▶ GenServer (serializado)
request B ──GenServer.call──▶ (espera en mailbox)
request C ──GenServer.call──▶ (espera en mailbox)
```

Con carga alta, el mailbox crece. El GenServer procesa un mensaje a la vez. La latencia
de `check/3` sube proporcionalmente al backlog.

ETS con tabla `:public` permite **lecturas concurrentes sin pasar por ningún proceso**:

```
request A ──ets:lookup──▶ ETS table  (concurrent, no serialization)
request B ──ets:lookup──▶ ETS table
request C ──ets:lookup──▶ ETS table
request D ──GenServer.cast──▶ GenServer ──ets:insert──▶ ETS table
```

Solo las escrituras (`record/1`) pasan por el GenServer para garantizar que la tabla
existe mientras el proceso esté vivo. Las lecturas (`check/3`) van directo a ETS.

Este es el patrón **read-heavy ETS owner**: el GenServer es dueño de la tabla (si el
proceso muere, la tabla se destruye) pero no es el cuello de botella para lecturas.

---

## Implementación

### Paso 1: Crear el proyecto

```bash
mix new api_gateway --sup
cd api_gateway
mkdir -p lib/api_gateway/rate_limiter
mkdir -p test/api_gateway
mkdir -p bench
```

### Paso 2: `mix.exs` — agregar benchee como dependencia de dev

```elixir
# mix.exs
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Paso 3: `lib/api_gateway/rate_limiter/server.ex`

Los `# TODO` son lo que tenés que implementar. Los `# HINT` dan dirección sin
spoilear la solución. No modifiques la firma de las funciones públicas — los tests
dependen de ellas.

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  @table :rate_limiter_windows
  @cleanup_interval_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API — entry points usados por el router y los tests
  # ---------------------------------------------------------------------------

  @doc """
  Verifica si `client_id` puede hacer un request dado el límite y ventana.

  Retorna `{:allow, remaining}` o `{:deny, retry_after_ms}`.

  Esta función lee directo de ETS — NO pasa por el GenServer.
  """
  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    # HINT: usa :ets.lookup/2 para obtener todos los timestamps de client_id
    # HINT: filtrá los que caen dentro de la ventana (ahora - window_ms)
    # HINT: si count < limit → {:allow, limit - count}
    # HINT: si count >= limit → {:deny, tiempo_hasta_que_expire_el_más_viejo}
    # TODO: implementar
  end

  @doc """
  Registra un nuevo request para `client_id` con el timestamp actual.

  Llamá esto SOLO si check/3 retornó :allow. Es un cast — fire and forget.
  """
  @spec record(String.t()) :: :ok
  def record(client_id) do
    ts = System.monotonic_time(:millisecond)
    GenServer.cast(__MODULE__, {:record, client_id, ts})
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    # TODO: crear la tabla ETS con las opciones correctas
    #
    # Opciones a considerar:
    #   :named_table   → acceso por nombre en lugar de por pid, necesario para reads desde check/3
    #   :public        → cualquier proceso puede leer/escribir (necesario para check/3 sin GenServer)
    #   :bag           → permite múltiples valores para la misma clave (necesario para timestamps)
    #
    # Pregunta de diseño: ¿por qué :bag y no :set aquí?
    # Con :set, {client_id} solo puede tener UN valor. Con :bag, puede tener N.
    # Necesitamos guardar un timestamp por request — necesitamos :bag.

    table = :ets.new(@table, [:named_table, :public, :bag])
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:ok, %{table: table}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_cast({:record, client_id, timestamp}, state) do
    # TODO: insertar {client_id, timestamp} en la tabla ETS
    # HINT: :ets.insert/2 recibe la tabla y una tupla {key, value}
    {:noreply, state}
  end

  @impl true
  def handle_info(:cleanup, state) do
    # Limpieza periódica: borrar entries más viejos que 1 hora.
    # ETS no tiene TTL nativo — la limpieza es responsabilidad del dueño.
    #
    # Opción A (simple, para empezar):
    #   :ets.tab2list/1 + Enum.filter + :ets.delete_object/2
    #   Pros: fácil de leer. Contras: O(n) en memoria (copia toda la tabla).
    #
    # Opción B (eficiente para producción):
    #   :ets.select_delete/2 con match spec
    #   Pros: opera directamente en ETS, sin copiar. Contras: sintaxis de match spec.
    #
    # Empezá con Opción A. Si los benchmarks muestran que cleanup es un cuello
    # de botella, migrá a Opción B.

    cutoff = System.monotonic_time(:millisecond) - 3_600_000

    # TODO: borrar todas las entradas con timestamp < cutoff
    # HINT (Opción A): :ets.tab2list(@table) devuelve [{client_id, ts}, ...]
    # HINT (Opción A): para borrar una entrada específica: :ets.delete_object(@table, {client_id, ts})

    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, state}
  end
end
```

### Paso 4: Tests dados — deben pasar sin modificación

Copiá este archivo exactamente. Tu implementación debe hacer pasar estos 4 tests.

```elixir
# test/api_gateway/rate_limiter_test.exs
defmodule ApiGateway.RateLimiterTest do
  use ExUnit.Case, async: false
  # async: false porque comparten la tabla ETS global :rate_limiter_windows

  alias ApiGateway.RateLimiter.Server

  setup do
    :ets.delete_all_objects(:rate_limiter_windows)
    :ok
  end

  describe "check/3 — sliding window semantics" do
    test "permite requests dentro del límite" do
      for _ <- 1..5, do: Server.record("client_allow")
      # Dar tiempo al GenServer para procesar los casts
      Process.sleep(10)

      assert {:allow, remaining} = Server.check("client_allow", 10, 60_000)
      assert remaining == 5
    end

    test "deniega cuando se supera el límite" do
      for _ <- 1..10, do: Server.record("client_deny")
      Process.sleep(10)

      assert {:deny, retry_after_ms} = Server.check("client_deny", 10, 60_000)
      assert retry_after_ms > 0 and retry_after_ms <= 60_000
    end

    test "requests expirados no cuentan en la ventana" do
      # Insertar timestamps artificiales que ya expiraron (90s atrás)
      old_ts = System.monotonic_time(:millisecond) - 90_000

      for _ <- 1..10 do
        :ets.insert(:rate_limiter_windows, {"client_expired", old_ts})
      end

      # Con ventana de 60s, esos timestamps ya expiró — debe permitir
      assert {:allow, _remaining} = Server.check("client_expired", 10, 60_000)
    end

    test "cliente sin historial tiene el límite completo disponible" do
      assert {:allow, 100} = Server.check("client_new", 100, 60_000)
    end
  end

  describe "check/3 — concurrent reads" do
    test "100 goroutines leen simultáneamente sin race condition" do
      # Poblar con algunos requests
      for _ <- 1..50, do: Server.record("client_concurrent")
      Process.sleep(20)

      tasks =
        for _ <- 1..100 do
          Task.async(fn -> Server.check("client_concurrent", 100, 60_000) end)
        end

      results = Task.await_many(tasks, 5_000)

      # Todos deben retornar una respuesta válida — ningún crash
      assert Enum.all?(results, fn
               {:allow, n} when is_integer(n) -> true
               {:deny, ms} when is_integer(ms) -> true
               _ -> false
             end)
    end
  end
end
```

### Paso 5: Correr los tests

```bash
mix test test/api_gateway/rate_limiter_test.exs --trace
```

Los 4 tests fallan inicialmente — eso es correcto. Tu trabajo es implementar
`Server` hasta que todos pasen.

### Paso 6: Benchmark de concurrencia

Una vez que los tests pasan, medí el throughput real:

```elixir
# bench/rate_limiter_bench.exs
Benchee.run(
  %{
    "check — tabla vacía" => fn ->
      ApiGateway.RateLimiter.Server.check("bench_new", 1_000, 60_000)
    end,
    "check — 500 entries en tabla" => fn ->
      ApiGateway.RateLimiter.Server.check("bench_heavy", 1_000, 60_000)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Sembrar datos antes del benchmark:

```elixir
# En iex -S mix, antes de correr el bench:
ts = System.monotonic_time(:millisecond)
for _ <- 1..500, do: :ets.insert(:rate_limiter_windows, {"bench_heavy", ts})
```

```bash
mix run bench/rate_limiter_bench.exs
```

**Resultado esperado en hardware moderno**: `check` < 10µs en p99 para tabla con 500 entries.
Si ves latencias > 100µs, revisá que `check/3` NO esté haciendo `GenServer.call`.

---

## Análisis de trade-offs

Completá esta tabla basándote en tu implementación y los resultados del benchmark.

| Aspecto | ETS `:bag` (tu implementación) | State map en GenServer | Redis externo |
|---------|-------------------------------|----------------------|---------------|
| Lecturas concurrentes | sin serialización | serializadas por mailbox | red round-trip |
| Consistencia | eventual (casts async) | strong (calls sync) | configurable |
| Latencia p50 | < 5µs (medir) | proporcional al backlog | > 500µs |
| Memoria por 10k clientes activos | estimar | estimar | n/a (off-heap) |
| Persiste si el nodo cae | no | no | sí |
| Complejidad de limpieza | manual (tu cleanup) | manual | TTL nativo |

Pregunta de reflexión: ¿en qué escenarios preferirías la alternativa de `GenServer.call`
sobre la lectura directa de ETS? (Pista: consistencia transaccional.)

---

## Errores comunes

**1. `handle_call` para lecturas de ETS**
Si `check/3` hace `GenServer.call`, el GenServer serializa todas las lecturas.
La tabla ETS existe para evitar exactamente eso. Lee directo con `:ets.lookup/2`.

**2. No limpiar entradas expiradas**
La tabla crece indefinidamente. En producción con 10k clientes activos y ventanas de
60s, podés acumular millones de entradas en horas. El cleanup periódico no es opcional.

**3. `:set` en lugar de `:bag`**
Con `:set`, solo podés guardar UN valor por clave. Si insertás `{"client", ts2}` después
de `{"client", ts1}`, el segundo reemplaza al primero. Perdés el historial de timestamps
que necesita sliding window. Necesitás `:bag`.

**4. `System.os_time` en lugar de `System.monotonic_time`**
`os_time` puede retroceder (ajuste NTP, leap seconds). Para ventanas de tiempo donde
comparás "ahora - window_ms", necesitás `monotonic_time` que garantiza avance monotónico.

**5. `record/1` como `call` en lugar de `cast`**
Registrar un timestamp no necesita confirmación. Usar `cast` libera al caller
inmediatamente. Usar `call` hace que cada request espere confirmación de escritura.

---

## Recursos

- [`:ets` documentation — Erlang/OTP](https://www.erlang.org/doc/man/ets.html) — leer la sección sobre `type` y `access`
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — capítulo sobre ETS en producción (free PDF)
- [Plug.Session.ETS source](https://github.com/elixir-plug/plug/blob/main/lib/plug/session/ets.ex) — cómo Plug usa ETS como session store (patrón similar al tuyo)
- [Benchee](https://github.com/bencheeorg/benchee) — benchmarking idiomático en Elixir
