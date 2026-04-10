# Ejercicio 58: Plug.Router y API sin Phoenix

## Objetivo

Construir una API REST completa usando solo Plug — sin Phoenix, sin Ecto. El objetivo no
es reemplazar Phoenix en producción, sino entender la capa que Phoenix construye encima:
routing, parsing de body, manejo de errores, y respuestas JSON. Cuando algo falla en
Phoenix, este conocimiento te dice dónde mirar.

## Conceptos clave

| Concepto | Descripción |
|---|---|
| `Plug.Router` | Router que es él mismo un plug. `match` + `dispatch` |
| Path params | `conn.path_params["id"]` tras `match "/users/:id"` |
| `fetch_query_params/1` | Parsea `?key=val` y los pone en `conn.query_params` |
| `Plug.Parsers` | Parsea el body (JSON, multipart, urlencoded) |
| `send_chunked/2` + `chunk/2` | Streaming HTTP sin buffering completo |
| `rescue` en pipeline | Captura excepciones en el plug y devuelve 500 controlado |

## Setup

```bash
mix new plug_api --sup
cd plug_api
```

`mix.exs`:
```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"}
  ]
end
```

```bash
mix deps.get
```

---

## Ejercicio 1: REST API completa sin Phoenix

### Contexto

CRUD de usuarios en memoria (un simple `Agent` con un `Map`). El pipeline es:
`Plug.Parsers` → `AuthPlug` → `Router`. Cada ruta lee params, llama al store, y
devuelve JSON.

### Store en memoria

`lib/plug_api/user_store.ex`:
```elixir
defmodule PlugApi.UserStore do
  @moduledoc """
  Store de usuarios en memoria usando Agent.
  En producción esto sería Ecto + Postgres.
  """
  use Agent

  def start_link(_opts) do
    Agent.start_link(fn -> %{next_id: 1, users: %{}} end, name: __MODULE__)
  end

  def list do
    Agent.get(__MODULE__, & &1.users) |> Map.values()
  end

  def get(id) do
    Agent.get(__MODULE__, fn state -> Map.get(state.users, id) end)
  end

  def create(attrs) do
    Agent.get_and_update(__MODULE__, fn state ->
      id = state.next_id
      user = Map.put(attrs, "id", id)
      new_state = %{state | next_id: id + 1, users: Map.put(state.users, id, user)}
      {user, new_state}
    end)
  end

  def delete(id) do
    Agent.get_and_update(__MODULE__, fn state ->
      case Map.pop(state.users, id) do
        {nil, _} -> {:error, state}
        {user, users} -> {{:ok, user}, %{state | users: users}}
      end
    end)
  end
end
```

### El Router

`lib/plug_api/router.ex`:
```elixir
defmodule PlugApi.Router do
  use Plug.Router
  import Plug.Conn

  # Pipeline interno del router
  plug :match
  plug Plug.Parsers,
    parsers: [:json],
    pass: ["application/json"],
    json_decoder: Jason
  plug :dispatch

  # GET /users — lista todos
  get "/users" do
    users = PlugApi.UserStore.list()
    json(conn, 200, %{users: users, count: length(users)})
  end

  # POST /users — crea uno
  post "/users" do
    case validate_user_params(conn.body_params) do
      {:ok, attrs} ->
        user = PlugApi.UserStore.create(attrs)
        json(conn, 201, %{user: user})

      {:error, reason} ->
        json(conn, 422, %{error: "Validation failed", details: reason})
    end
  end

  # GET /users/:id — muestra uno
  get "/users/:id" do
    id = parse_id(conn.path_params["id"])

    case PlugApi.UserStore.get(id) do
      nil -> json(conn, 404, %{error: "User not found", id: id})
      user -> json(conn, 200, %{user: user})
    end
  end

  # DELETE /users/:id — elimina uno
  delete "/users/:id" do
    id = parse_id(conn.path_params["id"])

    case PlugApi.UserStore.delete(id) do
      {:ok, user} -> json(conn, 200, %{deleted: user})
      :error -> json(conn, 404, %{error: "User not found", id: id})
    end
  end

  # GET /users/search?name=foo — búsqueda por query param
  get "/users/search" do
    conn = fetch_query_params(conn)
    name_filter = Map.get(conn.query_params, "name", "")

    results =
      PlugApi.UserStore.list()
      |> Enum.filter(fn u ->
        String.contains?(
          String.downcase(Map.get(u, "name", "")),
          String.downcase(name_filter)
        )
      end)

    json(conn, 200, %{results: results, query: name_filter})
  end

  # Catch-all — 404 para rutas no definidas
  match _ do
    json(conn, 404, %{error: "Route not found", path: conn.request_path})
  end

  # -- helpers privados --

  defp json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end

  defp parse_id(str) do
    case Integer.parse(str) do
      {id, ""} -> id
      _ -> nil
    end
  end

  defp validate_user_params(params) do
    name = Map.get(params, "name")
    email = Map.get(params, "email")

    cond do
      is_nil(name) or name == "" -> {:error, "name is required"}
      is_nil(email) or email == "" -> {:error, "email is required"}
      not String.contains?(email, "@") -> {:error, "email is invalid"}
      true -> {:ok, %{"name" => name, "email" => email}}
    end
  end
end
```

### Application con pipeline completo

`lib/plug_api/application.ex`:
```elixir
defmodule PlugApi.Application do
  use Application

  def start(_type, _args) do
    children = [
      PlugApi.UserStore,
      {Plug.Cowboy,
       scheme: :http,
       plug: PlugApi.Endpoint,
       options: [port: 4001]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PlugApi.Supervisor)
  end
end
```

`lib/plug_api/endpoint.ex` — el punto de entrada con error handling:
```elixir
defmodule PlugApi.Endpoint do
  use Plug.Builder
  import Plug.Conn

  plug PlugApi.Plugs.RequestLogger
  plug PlugApi.Router

  # Captura cualquier excepción no manejada antes de que Cowboy devuelva 500 genérico
  @impl true
  def call(conn, opts) do
    super(conn, opts)
  rescue
    e ->
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(500, Jason.encode!(%{
        error: "Internal server error",
        message: Exception.message(e)
      }))
  end
end
```

`lib/plug_api/plugs/request_logger.ex`:
```elixir
defmodule PlugApi.Plugs.RequestLogger do
  import Plug.Conn
  require Logger

  def init(opts), do: opts

  def call(conn, _opts) do
    start = System.monotonic_time(:millisecond)

    register_before_send(conn, fn conn ->
      duration = System.monotonic_time(:millisecond) - start

      Logger.info("#{conn.method} #{conn.request_path} → #{conn.status} (#{duration}ms)")
      conn
    end)
  end
end
```

### Prueba con curl

```bash
mix run --no-halt

# Crear usuarios
curl -s -X POST http://localhost:4001/users \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice","email":"alice@example.com"}' | jq .
# {"user":{"id":1,"name":"Alice","email":"alice@example.com"}}

curl -s -X POST http://localhost:4001/users \
  -H "Content-Type: application/json" \
  -d '{"name":"Bob","email":"bob@example.com"}' | jq .

# Listar
curl -s http://localhost:4001/users | jq .
# {"users":[...],"count":2}

# Obtener por ID
curl -s http://localhost:4001/users/1 | jq .

# Buscar por nombre
curl -s "http://localhost:4001/users/search?name=ali" | jq .

# Eliminar
curl -s -X DELETE http://localhost:4001/users/1 | jq .

# 404
curl -s http://localhost:4001/users/999 | jq .
# {"error":"User not found","id":999}

# Validación
curl -s -X POST http://localhost:4001/users \
  -H "Content-Type: application/json" \
  -d '{"name":""}' | jq .
# {"error":"Validation failed","details":"name is required"}
```

---

## Ejercicio 2: Streaming Response con send_chunked

### Contexto

Un endpoint que genera datos grandes (CSV de 100k filas, logs, resultados de un query
pesado) no debería esperar a tener todo en memoria antes de responder. `send_chunked/2`
abre la conexión HTTP con `Transfer-Encoding: chunked` y `chunk/2` envía datos
incrementalmente.

### El código

`lib/plug_api/plugs/stream_plug.ex`:
```elixir
defmodule PlugApi.StreamPlug do
  @moduledoc """
  Endpoint de streaming — envía datos en chunks sin acumular en memoria.
  Útil para: CSV grandes, logs, feeds de eventos, resultados paginados.
  """
  import Plug.Conn

  def init(opts), do: opts

  def call(%{path_info: ["stream", "csv"]} = conn, _opts) do
    conn = fetch_query_params(conn)
    rows = conn.query_params |> Map.get("rows", "100") |> String.to_integer()

    conn
    |> put_resp_content_type("text/csv")
    |> put_resp_header("content-disposition", "attachment; filename=\"data.csv\"")
    |> send_chunked(200)
    |> stream_csv(rows)
  end

  def call(conn, _opts), do: conn

  # -- privado --

  defp stream_csv(conn, total_rows) do
    # Header del CSV
    {:ok, conn} = chunk(conn, "id,name,value,timestamp\n")

    # Genera y envía una fila a la vez — O(1) en memoria
    Enum.reduce_while(1..total_rows, conn, fn i, conn ->
      row = "#{i},item_#{i},#{:rand.uniform(1000)},#{DateTime.utc_now()}\n"

      case chunk(conn, row) do
        {:ok, conn} -> {:cont, conn}
        # El cliente cerró la conexión — para el stream limpiamente
        {:error, :closed} -> {:halt, conn}
      end
    end)
  end
end
```

Añadir al router:
```elixir
get "/stream/csv" do
  conn = fetch_query_params(conn)
  rows = conn.query_params |> Map.get("rows", "1000") |> String.to_integer()

  conn
  |> put_resp_content_type("text/csv")
  |> put_resp_header("content-disposition", "attachment; filename=\"export.csv\"")
  |> send_chunked(200)
  |> stream_rows(rows)
end

defp stream_rows(conn, total) do
  {:ok, conn} = chunk(conn, "id,name,value\n")

  Enum.reduce_while(1..total, conn, fn i, conn ->
    row = "#{i},item_#{i},#{:rand.uniform(9999)}\n"
    case chunk(conn, row) do
      {:ok, conn} -> {:cont, conn}
      {:error, :closed} -> {:halt, conn}
    end
  end)
end
```

### Prueba

```bash
# Descargar CSV de 10,000 filas y ver que llegan en chunks
curl -N http://localhost:4001/stream/csv?rows=10000 | head -5
# id,name,value
# 1,item_1,847
# 2,item_2,123
# ...

# Medir tiempo — con streaming el primer chunk llega inmediatamente
time curl -s "http://localhost:4001/stream/csv?rows=100000" > /dev/null
```

La diferencia con un response normal: con `send_resp/3` Cowboy espera a tener el body
completo en memoria. Con `send_chunked/2` + `chunk/2`, el cliente empieza a recibir
datos mientras el servidor sigue generando.

---

## Ejercicio 3: WebSocket Handler con Cowboy

### Contexto

Phoenix Channels son una abstracción sobre WebSockets. Aquí vemos la capa cruda:
Cowboy permite hacer upgrade de una conexión HTTP a WebSocket directamente, sin Phoenix.
Útil para entender qué hace Phoenix por debajo, o para servicios muy simples.

### El código

`lib/plug_api/websocket_handler.ex`:
```elixir
defmodule PlugApi.WebSocketHandler do
  @moduledoc """
  Handler WebSocket de Cowboy puro.
  Implementa el behaviour :cowboy_websocket.
  Echo server con broadcast a todos los clientes conectados.
  """
  @behaviour :cowboy_websocket

  # Registro de clientes conectados
  @registry :ws_clients

  # -- callbacks de Cowboy WebSocket --

  @impl true
  def init(req, state) do
    # Devuelve {:cowboy_websocket, req, state} para hacer el upgrade
    {:cowboy_websocket, req, state}
  end

  @impl true
  def websocket_init(state) do
    # Se ejecuta después del handshake WebSocket
    # Registramos este proceso para poder hacer broadcast
    :ets.insert(@registry, {self()})
    {:ok, state}
  end

  @impl true
  def websocket_handle({:text, message}, state) do
    # Parsea el mensaje JSON del cliente
    case Jason.decode(message) do
      {:ok, %{"type" => "echo", "data" => data}} ->
        reply = Jason.encode!(%{type: "echo_reply", data: data, from: inspect(self())})
        {:reply, {:text, reply}, state}

      {:ok, %{"type" => "broadcast", "data" => data}} ->
        broadcast_to_all(data)
        {:ok, state}

      {:ok, _unknown} ->
        error = Jason.encode!(%{type: "error", message: "unknown message type"})
        {:reply, {:text, error}, state}

      {:error, _} ->
        error = Jason.encode!(%{type: "error", message: "invalid JSON"})
        {:reply, {:text, error}, state}
    end
  end

  # Frame binario — ignoramos, respondemos con error
  def websocket_handle({:binary, _}, state) do
    {:ok, state}
  end

  @impl true
  def websocket_info({:broadcast, message}, state) do
    # Mensaje enviado desde otro proceso (otro cliente haciendo broadcast)
    {:reply, {:text, message}, state}
  end

  def websocket_info(_msg, state) do
    {:ok, state}
  end

  @impl true
  def terminate(_reason, _req, _state) do
    # Limpia el registro al desconectar
    :ets.delete(@registry, self())
    :ok
  end

  # -- privado --

  defp broadcast_to_all(data) do
    message = Jason.encode!(%{type: "broadcast", data: data, from: inspect(self())})

    :ets.tab2list(@registry)
    |> Enum.each(fn {pid} ->
      # No te envíes el broadcast a ti mismo
      if pid != self(), do: send(pid, {:broadcast, message})
    end)
  end
end
```

Registrar la ruta WebSocket en Cowboy (en el endpoint):

`lib/plug_api/application.ex` — usa Cowboy directamente para mezclar Plug y WebSocket:
```elixir
def start(_type, _args) do
  :ets.new(:ws_clients, [:named_table, :public, :bag])

  dispatch = :cowboy_router.compile([
    {:_, [
      {"/ws", PlugApi.WebSocketHandler, []},
      {:_, Plug.Cowboy.Handler, {PlugApi.Endpoint, []}}
    ]}
  ])

  children = [
    PlugApi.UserStore,
    Supervisor.child_spec(
      {:ranch, :http, :ranch_tcp, [port: 4001],
       :cowboy_clear, %{env: %{dispatch: dispatch}}},
      id: :cowboy_listener
    )
  ]

  Supervisor.start_link(children, strategy: :one_for_one)
end
```

### Prueba con wscat

```bash
npm install -g wscat

# Terminal 1
wscat -c ws://localhost:4001/ws

# Dentro de wscat:
> {"type":"echo","data":"hola mundo"}
< {"type":"echo_reply","data":"hola mundo","from":"#PID<0.234.0>"}

> {"type":"broadcast","data":"mensaje para todos"}
# En otro terminal conectado verás:
< {"type":"broadcast","data":"mensaje para todos","from":"#PID<0.234.0>"}
```

---

## Comparación: Plug vs Phoenix

| Aspecto | Solo Plug | Phoenix |
|---|---|---|
| Routing | `Plug.Router` | Phoenix.Router + LiveView |
| Auth | Plug module manual | Guardian, Pow, etc. |
| Body parsing | `Plug.Parsers` | Configurado en Endpoint |
| WebSockets | `:cowboy_websocket` raw | Phoenix.Channel |
| Templates | No aplica | HEEx, LiveView |
| Boilerplate | Mínimo | Generadores `mix phx.gen.*` |

Plug puro es apropiado para: microservicios HTTP simples, sidecars, proxies internos,
servicios de webhooks, o cuando el overhead de Phoenix no está justificado.

---

## Preguntas para reflexión

1. En `Plug.Router`, el orden de `match` vs `dispatch` en el pipeline interno importa
   mucho. ¿Qué pasa si pones `Plug.Parsers` antes de `plug :match`? ¿Funciona? ¿Por
   qué sí o no?

2. El endpoint `/users/search` usa `get "/users/search"` pero también existe
   `get "/users/:id"`. ¿Qué pasa si defines el de `:id` primero? ¿Cómo resuelve
   `Plug.Router` este conflicto?

3. En el WebSocket handler, ¿qué problema tiene el registro con ETS si un proceso
   muere sin llamar a `terminate/3`? ¿Cómo lo resolverías?

4. `send_chunked/2` retorna la conn inmediatamente — ¿por qué no puedes modificar
   headers después de llamarla? ¿Qué hace HTTP por debajo?

5. ¿Por qué el `rescue` en `Endpoint.call/2` debe estar en el endpoint y no en cada
   ruta del router? ¿Qué pasa si un plug lanza una excepción antes de que llegue al
   router?

---

## Puntos clave

- `Plug.Router` usa macros que generan pattern matching — rutas más específicas deben ir antes
- `conn.body_params` solo tiene valor después de que `Plug.Parsers` procesó el body
- `fetch_query_params/1` es lazy — si no la llamas, `conn.query_params` está vacío
- `send_chunked/2` hace que headers sean inmutables — deben estar listos antes de llamarla
- `:cowboy_websocket` es el protocolo crudo que Phoenix.Channel envuelve con PubSub y presencia
- Un plug que lanza sin rescatar mata la request — el `rescue` en el endpoint es el safety net
