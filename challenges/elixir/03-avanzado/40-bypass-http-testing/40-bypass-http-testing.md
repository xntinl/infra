# 40. Testing de HTTP Clients con Bypass

**Difficulty**: Avanzado

## Prerequisites
- Experiencia con ExUnit y testing en Elixir
- Conocimiento de HTTP (métodos, status codes, headers, body)
- Familiaridad con clientes HTTP en Elixir (Req, Finch, HTTPoison)
- Comprensión de supervisión OTP y gestión de puertos

## Learning Objectives
After completing this exercise, you will be able to:
- Levantar un servidor HTTP real en los tests con `Bypass.open/0`
- Configurar respuestas específicas con `Bypass.expect/3` y `Bypass.expect_once/3`
- Verificar que el cliente HTTP hace requests con los métodos, paths y headers correctos
- Simular errores HTTP (500, 429, timeouts) para testear retry logic y error handling
- Gestionar el lifecycle de Bypass en el contexto de tests async
- Distinguir cuándo usar Bypass vs Mox para testear código HTTP

## Concepts

### Bypass vs Mox: cuándo usar cuál

| Situación | Bypass | Mox |
|---|---|---|
| Testear parsing de respuestas HTTP reales | ✓ | |
| Testear headers, paths, body de los requests | ✓ | |
| Testear timeouts de conexión TCP reales | ✓ | |
| Testear retry logic y backoff | ✓ | |
| Testear lógica de negocio que llama a un servicio | | ✓ |
| Tests más rápidos (no hay I/O TCP) | | ✓ |
| Sin dependencias de infraestructura de red | | ✓ |

**Regla general**: usa Bypass cuando la interacción HTTP en sí es lo que testeas (parsing, headers, errores de red). Usa Mox cuando el HTTP es un detalle de implementación de la lógica que testeas.

### Bypass: un servidor HTTP real en el test

```elixir
# mix.exs
{:bypass, "~> 2.1", only: :test}

defmodule MyTest do
  use ExUnit.Case

  setup do
    bypass = Bypass.open()   # Levanta servidor en puerto aleatorio
    {:ok, bypass: bypass}
  end

  test "cliente parsea respuesta JSON", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/users/1", fn conn ->
      Plug.Conn.resp(conn, 200, Jason.encode!(%{id: 1, name: "Alice"}))
      |> Plug.Conn.put_resp_content_type("application/json")
    end)

    url = "http://localhost:#{bypass.port}"
    assert {:ok, %{name: "Alice"}} = MyClient.get_user(url, 1)
  end
end
```

### Funciones principales de Bypass

```elixir
Bypass.open()             # Levanta el servidor, retorna %Bypass{port: N}
Bypass.open(port: 8080)   # Puerto específico (cuidado con conflictos)

Bypass.expect(bypass, method, path, handler)
# Espera UN request (puede ser llamado múltiples veces)
# Si se llama más de una vez con expect_once, falla

Bypass.expect_once(bypass, method, path, handler)
# Espera exactamente UN request
# Falla si se llama 0 o 2+ veces

Bypass.stub(bypass, method, path, handler)
# Responde siempre sin verificar conteo
# Como Mox.stub — para dependencias de soporte

Bypass.pass(bypass)
# Indica que el test ha terminado sin esperar requests
# Necesario si el código puede no llamar al servidor en algunos paths

Bypass.down(bypass)        # Para el servidor (cleanup en teardown)
```

### El handler de Bypass

```elixir
Bypass.expect_once(bypass, "POST", "/api/data", fn conn ->
  # conn es un %Plug.Conn{}
  # Leer el body del request
  {:ok, body, conn} = Plug.Conn.read_body(conn)
  data = Jason.decode!(body)

  # Verificar el request
  assert data["name"] != nil
  assert Plug.Conn.get_req_header(conn, "authorization") == ["Bearer token123"]

  # Responder
  conn
  |> Plug.Conn.put_resp_content_type("application/json")
  |> Plug.Conn.resp(201, Jason.encode!(%{id: 42}))
end)
```

### Simular errores y condiciones especiales

```elixir
# Error del servidor
Bypass.expect_once(bypass, "GET", "/", fn conn ->
  Plug.Conn.resp(conn, 500, "Internal Server Error")
end)

# Rate limiting
Bypass.expect(bypass, "GET", "/", fn conn ->
  conn
  |> Plug.Conn.put_resp_header("retry-after", "60")
  |> Plug.Conn.resp(429, "Too Many Requests")
end)

# Timeout — servidor que no responde
Bypass.expect_once(bypass, "GET", "/slow", fn conn ->
  :timer.sleep(10_000)  # Más largo que el timeout del cliente
  Plug.Conn.resp(conn, 200, "too late")
end)

# Down del servidor (connection refused)
Bypass.down(bypass)   # El servidor deja de escuchar
```

## Exercises

### Exercise 1: Mock HTTP Server — Cliente que parsea JSON

Implementa un cliente HTTP y sus tests usando Bypass para simular la API externa.

```elixir
# lib/github_client.ex
defmodule GithubClient do
  @moduledoc """
  Cliente HTTP para la API de GitHub.
  La URL base es configurable para permitir testing con Bypass.
  """

  @default_base_url "https://api.github.com"

  def get_repo(owner, repo, opts \\ []) do
    base_url = Keyword.get(opts, :base_url, @default_base_url)
    token    = Keyword.get(opts, :token, nil)

    headers = build_headers(token)
    url     = "#{base_url}/repos/#{owner}/#{repo}"

    case Req.get(url, headers: headers) do
      {:ok, %{status: 200, body: body}} ->
        {:ok, parse_repo(body)}

      {:ok, %{status: 404}} ->
        {:error, :not_found}

      {:ok, %{status: 403}} ->
        {:error, :forbidden}

      {:ok, %{status: 422, body: body}} ->
        {:error, {:validation_error, body}}

      {:error, reason} ->
        {:error, {:network_error, reason}}
    end
  end

  def list_repos(username, opts \\ []) do
    base_url = Keyword.get(opts, :base_url, @default_base_url)
    token    = Keyword.get(opts, :token, nil)
    page     = Keyword.get(opts, :page, 1)

    headers = build_headers(token)
    url     = "#{base_url}/users/#{username}/repos?page=#{page}&per_page=30"

    case Req.get(url, headers: headers) do
      {:ok, %{status: 200, body: repos}} when is_list(repos) ->
        {:ok, Enum.map(repos, &parse_repo/1)}

      {:ok, %{status: 404}} ->
        {:error, :user_not_found}

      {:error, reason} ->
        {:error, {:network_error, reason}}
    end
  end

  defp parse_repo(%{"full_name" => name, "stargazers_count" => stars, "private" => private}) do
    %{name: name, stars: stars, private: private}
  end

  defp build_headers(nil),   do: [{"accept", "application/vnd.github+json"}]
  defp build_headers(token), do: [
    {"accept", "application/vnd.github+json"},
    {"authorization", "Bearer #{token}"}
  ]
end

# test/github_client_test.exs
defmodule GithubClientTest do
  use ExUnit.Case, async: true

  setup do
    bypass = Bypass.open()
    {:ok, bypass: bypass}
  end

  test "get_repo: parsea respuesta JSON correctamente", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/repos/elixir-lang/elixir", fn conn ->
      # TODO: Responder con JSON válido del repo
      body = Jason.encode!(%{
        "full_name"        => "elixir-lang/elixir",
        "stargazers_count" => 24_000,
        "private"          => false
      })

      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, body)
    end)

    base_url = "http://localhost:#{bypass.port}"
    assert {:ok, repo} = GithubClient.get_repo("elixir-lang", "elixir", base_url: base_url)

    # TODO: Verificar que el repo tiene los datos correctos
    assert repo.name  == "elixir-lang/elixir"
    assert repo.stars == 24_000
    assert repo.private == false
  end

  test "get_repo: retorna {:error, :not_found} cuando status 404", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/repos/nonexistent/repo", fn conn ->
      # TODO: Responder con 404
      Plug.Conn.resp(conn, 404, Jason.encode!(%{"message" => "Not Found"}))
    end)

    base_url = "http://localhost:#{bypass.port}"
    assert {:error, :not_found} =
      GithubClient.get_repo("nonexistent", "repo", base_url: base_url)
  end

  test "get_repo: incluye Authorization header cuando se provee token", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/repos/owner/private-repo", fn conn ->
      # TODO: Verificar que el header authorization está presente
      auth_headers = Plug.Conn.get_req_header(conn, "authorization")
      assert ["Bearer my_token_123"] = auth_headers

      body = Jason.encode!(%{
        "full_name"        => "owner/private-repo",
        "stargazers_count" => 0,
        "private"          => true
      })

      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, body)
    end)

    base_url = "http://localhost:#{bypass.port}"
    assert {:ok, repo} = GithubClient.get_repo("owner", "private-repo",
      base_url: base_url, token: "my_token_123")
    assert repo.private == true
  end

  test "list_repos: parsea lista de repos", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/users/octocat/repos", fn conn ->
      # TODO: Verificar query params (page=1, per_page=30)
      params = URI.decode_query(conn.query_string)
      assert params["page"]     == "1"
      assert params["per_page"] == "30"

      repos = Jason.encode!([
        %{"full_name" => "octocat/Hello-World", "stargazers_count" => 100, "private" => false},
        %{"full_name" => "octocat/Spoon-Knife",  "stargazers_count" => 200, "private" => false}
      ])

      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, repos)
    end)

    base_url = "http://localhost:#{bypass.port}"
    assert {:ok, repos} = GithubClient.list_repos("octocat", base_url: base_url)

    # TODO: Verificar que se retornan 2 repos con los datos correctos
    assert length(repos) == 2
    assert Enum.any?(repos, &(&1.name == "octocat/Hello-World"))
  end
end
```

**Hints**:
- `Bypass.open()` en `setup` asegura que cada test tiene su propio servidor en su propio puerto — perfecto para `async: true`
- El handler de Bypass recibe un `%Plug.Conn{}` y debe retornar una `%Plug.Conn{}` con la respuesta configurada
- Para verificar query params, usa `conn.query_string |> URI.decode_query/1` — `conn.params` puede no estar disponible sin el parser de Plug

**One possible solution** (sparse):
```elixir
# Test 404 — handler simple:
Bypass.expect_once(bypass, "GET", "/repos/nonexistent/repo", fn conn ->
  Plug.Conn.resp(conn, 404, Jason.encode!(%{"message" => "Not Found"}))
end)

# Verificar list_repos retorna 2 repos:
assert length(repos) == 2
names = Enum.map(repos, & &1.name)
assert "octocat/Hello-World" in names
```

---

### Exercise 2: Error Simulation — Retry Logic con 500 y 429

Testea que el cliente implementa retry con backoff exponencial cuando el servidor retorna errores.

```elixir
defmodule RetryableClient do
  @moduledoc """
  Cliente HTTP con retry logic:
  - 500 → retry hasta 3 veces con backoff exponencial
  - 429 → respetar Retry-After header
  - 404 → no retry (error permanente)
  - Network error → retry hasta 3 veces
  """

  @max_retries 3
  @base_backoff_ms 100  # En producción: 1_000

  def get_with_retry(url, opts \\ []) do
    max_retries = Keyword.get(opts, :max_retries, @max_retries)
    do_get(url, 0, max_retries)
  end

  defp do_get(url, attempt, max_retries) do
    case Req.get(url) do
      {:ok, %{status: 200, body: body}} ->
        {:ok, body}

      {:ok, %{status: 429, headers: headers}} ->
        retry_after = get_retry_after(headers)
        if attempt < max_retries do
          :timer.sleep(retry_after)
          do_get(url, attempt + 1, max_retries)
        else
          {:error, :rate_limited}
        end

      {:ok, %{status: 500}} ->
        if attempt < max_retries do
          backoff = trunc(@base_backoff_ms * :math.pow(2, attempt))
          :timer.sleep(backoff)
          do_get(url, attempt + 1, max_retries)
        else
          {:error, :server_error}
        end

      {:ok, %{status: 404}} ->
        {:error, :not_found}  # No retry — error permanente

      {:error, %{reason: :econnrefused}} ->
        if attempt < max_retries do
          backoff = trunc(@base_backoff_ms * :math.pow(2, attempt))
          :timer.sleep(backoff)
          do_get(url, attempt + 1, max_retries)
        else
          {:error, :connection_refused}
        end

      {:error, reason} ->
        {:error, {:network_error, reason}}
    end
  end

  defp get_retry_after(headers) do
    case List.keyfind(headers, "retry-after", 0) do
      {_, value} -> String.to_integer(value)
      nil        -> 1_000
    end
  end
end

defmodule RetryableClientTest do
  use ExUnit.Case, async: true

  setup do
    bypass = Bypass.open()
    {:ok, bypass: bypass}
  end

  test "éxito en el primer intento — no hace retry", %{bypass: bypass} do
    # TODO: expect_once para que falle si se llama más de una vez
    Bypass.expect_once(bypass, "GET", "/data", fn conn ->
      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, Jason.encode!(%{result: "ok"}))
    end)

    url = "http://localhost:#{bypass.port}/data"
    assert {:ok, body} = RetryableClient.get_with_retry(url)
    assert is_map(body)
  end

  test "retry en 500: hace 3 intentos y luego retorna error", %{bypass: bypass} do
    # TODO: Contar cuántas veces se llama el endpoint
    # Usar un Agent o contador externo para contar llamadas
    counter = :counters.new(1, [])

    Bypass.expect(bypass, "GET", "/flaky", fn conn ->
      # Incrementar contador atómicamente
      :counters.add(counter, 1, 1)
      Plug.Conn.resp(conn, 500, "Internal Server Error")
    end)

    url = "http://localhost:#{bypass.port}/flaky"
    # max_retries: 2 → 3 intentos total (1 inicial + 2 retries)
    assert {:error, :server_error} =
      RetryableClient.get_with_retry(url, max_retries: 2)

    # TODO: Verificar que se hicieron exactamente 3 llamadas
    assert :counters.get(counter, 1) == 3
  end

  test "retry: éxito en el segundo intento", %{bypass: bypass} do
    call_count = :counters.new(1, [])

    Bypass.expect(bypass, "GET", "/sometimes-ok", fn conn ->
      count = :counters.add(counter, 1, 1)  # Incrementa y retorna nuevo valor
      # TODO: Primera llamada → 500, segunda llamada → 200
      current = :counters.get(call_count, 1)
      if current == 1 do
        :counters.add(call_count, 1, 1)
        Plug.Conn.resp(conn, 500, "Error")
      else
        :counters.add(call_count, 1, 1)
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(200, Jason.encode!(%{ok: true}))
      end
    end)

    url = "http://localhost:#{bypass.port}/sometimes-ok"
    assert {:ok, _body} = RetryableClient.get_with_retry(url, max_retries: 3)
  end

  test "429 con Retry-After header: espera el tiempo indicado", %{bypass: bypass} do
    call_count = :counters.new(1, [])

    Bypass.expect(bypass, "GET", "/rate-limited", fn conn ->
      count_before = :counters.get(call_count, 1)
      :counters.add(call_count, 1, 1)

      if count_before == 0 do
        # Primera llamada: rate limited con retry-after de 50ms
        conn
        |> Plug.Conn.put_resp_header("retry-after", "50")
        |> Plug.Conn.resp(429, "Too Many Requests")
      else
        # Segunda llamada: éxito
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(200, Jason.encode!(%{result: "ok after wait"}))
      end
    end)

    url = "http://localhost:#{bypass.port}/rate-limited"
    start_time = System.monotonic_time(:millisecond)
    assert {:ok, _} = RetryableClient.get_with_retry(url, max_retries: 2)
    elapsed = System.monotonic_time(:millisecond) - start_time

    # TODO: Verificar que esperó al menos 50ms (el retry-after)
    assert elapsed >= 50, "Debió esperar al menos 50ms, esperó #{elapsed}ms"
    assert :counters.get(call_count, 1) == 2
  end

  test "404 no hace retry", %{bypass: bypass} do
    call_count = :counters.new(1, [])

    Bypass.expect_once(bypass, "GET", "/missing", fn conn ->
      :counters.add(call_count, 1, 1)
      Plug.Conn.resp(conn, 404, "Not Found")
    end)

    url = "http://localhost:#{bypass.port}/missing"
    assert {:error, :not_found} = RetryableClient.get_with_retry(url, max_retries: 3)

    # TODO: Verificar que solo se hizo 1 llamada (no hubo retry)
    assert :counters.get(call_count, 1) == 1
  end
end
```

**Hints**:
- `:counters.new/2` y `:counters.add/3` son atómicos y thread-safe — perfectos para contar llamadas en handlers concurrentes de Bypass
- `Bypass.expect/3` (sin `_once`) permite múltiples llamadas; `expect_once/3` falla si se llama más de una vez
- Para testear timing (que se esperó N ms), mide `System.monotonic_time/1` antes y después, con un margen razonable (±20%)

**One possible solution** (sparse):
```elixir
# Test "retry en 500" — el contador con :counters:
counter = :counters.new(1, [])
Bypass.expect(bypass, "GET", "/flaky", fn conn ->
  :counters.add(counter, 1, 1)
  Plug.Conn.resp(conn, 500, "Internal Server Error")
end)
# ...
assert :counters.get(counter, 1) == 3

# Test "éxito en segundo intento" — lógica de bifurcación:
current = :counters.get(call_count, 1)
:counters.add(call_count, 1, 1)
if current == 0, do: resp(conn, 500, "Error"), else: resp(conn, 200, ...)
```

---

### Exercise 3: Timeout Handling — Servidor que no Responde

Testea que el cliente maneja correctamente servidores que no responden o responden muy tarde.

```elixir
defmodule TimeoutAwareClient do
  @moduledoc """
  Cliente HTTP con timeout configurable.
  Distingue entre timeout de conexión y timeout de respuesta.
  """

  @default_connect_timeout 1_000  # 1 segundo para conectar
  @default_receive_timeout 3_000  # 3 segundos para recibir respuesta

  def get(url, opts \\ []) do
    connect_timeout = Keyword.get(opts, :connect_timeout, @default_connect_timeout)
    receive_timeout  = Keyword.get(opts, :receive_timeout, @default_receive_timeout)

    req_opts = [
      connect_options: [timeout: connect_timeout],
      receive_timeout: receive_timeout
    ]

    case Req.get(url, req_opts) do
      {:ok, %{status: 200, body: body}} ->
        {:ok, body}

      {:ok, %{status: status}} ->
        {:error, {:http_error, status}}

      {:error, %Req.TransportError{reason: :timeout}} ->
        {:error, :timeout}

      {:error, %Req.TransportError{reason: :econnrefused}} ->
        {:error, :connection_refused}

      {:error, reason} ->
        {:error, {:unexpected_error, reason}}
    end
  end
end

defmodule TimeoutAwareClientTest do
  use ExUnit.Case, async: true

  setup do
    bypass = Bypass.open()
    {:ok, bypass: bypass}
  end

  test "respuesta normal dentro del timeout", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/fast", fn conn ->
      # Responde inmediatamente
      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, Jason.encode!(%{data: "fast response"}))
    end)

    url = "http://localhost:#{bypass.port}/fast"
    assert {:ok, %{"data" => "fast response"}} =
      TimeoutAwareClient.get(url, receive_timeout: 1_000)
  end

  test "timeout: servidor tarda más que receive_timeout", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/slow", fn conn ->
      # TODO: Esperar más tiempo que el timeout del cliente
      :timer.sleep(500)  # 500ms > receive_timeout de 100ms en el test
      Plug.Conn.resp(conn, 200, "too late")
    end)

    url = "http://localhost:#{bypass.port}/slow"
    # receive_timeout muy corto para forzar timeout
    result = TimeoutAwareClient.get(url, receive_timeout: 100)

    # TODO: Verificar que retorna {:error, :timeout}
    assert {:error, :timeout} = result
  end

  test "connection refused: servidor caído", %{bypass: bypass} do
    # Bajar el servidor Bypass para simular connection refused
    Bypass.down(bypass)

    url = "http://localhost:#{bypass.port}/any-path"
    result = TimeoutAwareClient.get(url, connect_timeout: 500)

    # TODO: Verificar que retorna {:error, :connection_refused}
    assert {:error, :connection_refused} = result
  end

  test "servidor se cae en mitad de una respuesta larga", %{bypass: bypass} do
    Bypass.expect_once(bypass, "GET", "/partial", fn conn ->
      # Enviar headers pero no body completo
      conn = Plug.Conn.send_chunked(conn, 200)
      {:ok, conn} = Plug.Conn.chunk(conn, "partial da")
      # Cerrar conexión abruptamente (en la realidad, esto simula un crash del server)
      # Bypass cerrará la conexión cuando termine el handler
      conn
    end)

    url = "http://localhost:#{bypass.port}/partial"
    result = TimeoutAwareClient.get(url, receive_timeout: 500)

    # La respuesta puede ser error o un body parcial
    # Lo importante es que el cliente no se cuelga indefinidamente
    assert match?({:error, _}, result) or match?({:ok, _}, result)
  end

  test "múltiples requests concurrentes con timeout independiente", %{bypass: bypass} do
    # Configurar handler que responde lento a unos, rápido a otros
    Bypass.stub(bypass, "GET", "/concurrent", fn conn ->
      delay = conn.query_string
              |> URI.decode_query()
              |> Map.get("delay", "0")
              |> String.to_integer()
      :timer.sleep(delay)
      conn
      |> Plug.Conn.put_resp_content_type("application/json")
      |> Plug.Conn.resp(200, Jason.encode!(%{delay: delay}))
    end)

    base_url = "http://localhost:#{bypass.port}/concurrent"

    # Lanzar 5 requests concurrentes con distintos delays
    tasks = Enum.map([0, 50, 100, 200, 300], fn delay ->
      Task.async(fn ->
        url = "#{base_url}?delay=#{delay}"
        TimeoutAwareClient.get(url, receive_timeout: 150)
      end)
    end)

    results = Task.await_many(tasks, 5_000)

    # Los delays <= 150ms deben tener éxito, los > 150ms deben dar timeout
    fast_results = Enum.slice(results, 0, 3)  # delays 0, 50, 100
    slow_results  = Enum.slice(results, 3, 2) # delays 200, 300

    # TODO: Verificar que los fast tienen éxito
    assert Enum.all?(fast_results, &match?({:ok, _}, &1))
    # TODO: Verificar que los slow dan timeout
    assert Enum.all?(slow_results, &match?({:error, :timeout}, &1))
  end
end
```

**Hints**:
- `Bypass.down/1` baja el servidor TCP — cualquier request posterior recibirá connection refused inmediatamente
- Para simular timeouts, el delay en el handler (`timer.sleep`) debe ser MAYOR que el `receive_timeout` configurado en el cliente
- `Bypass.stub/3` es ideal para el test de concurrencia — no verifica conteo y puede manejar requests variables
- El error exacto que Req reporta para timeout es `%Req.TransportError{reason: :timeout}` — los detalles varían por librería HTTP; ajusta el pattern matching según la librería que uses

**One possible solution** (sparse):
```elixir
# Test timeout — verificar:
assert {:error, :timeout} = result

# Test connection refused — verificar:
assert {:error, :connection_refused} = result

# Test concurrente — verificar fast:
assert Enum.all?(fast_results, &match?({:ok, _}, &1))
# Verificar slow:
assert Enum.all?(slow_results, &match?({:error, :timeout}, &1))
```

## Common Mistakes

### Mistake 1: No configurar la URL del cliente para apuntar a Bypass
```elixir
# ❌ El cliente usa la URL hardcoded de producción — Bypass nunca recibe la llamada
test "test", %{bypass: bypass} do
  Bypass.expect_once(bypass, "GET", "/users", fn conn -> ... end)
  MyClient.get_users()  # Llama a api.production.com, no a localhost:bypass.port
end

# ✓ Pasar la URL de Bypass al cliente
test "test", %{bypass: bypass} do
  Bypass.expect_once(bypass, "GET", "/users", fn conn -> ... end)
  base_url = "http://localhost:#{bypass.port}"
  MyClient.get_users(base_url: base_url)
end
```

### Mistake 2: Usar expect_once cuando el cliente hace retry
```elixir
# ❌ Si el cliente hace 3 intentos, expect_once falla en el segundo intento
Bypass.expect_once(bypass, "GET", "/flaky", fn conn ->
  Plug.Conn.resp(conn, 500, "Error")
end)

# ✓ Usar expect (sin _once) cuando hay múltiples llamadas esperadas
Bypass.expect(bypass, "GET", "/flaky", fn conn ->
  Plug.Conn.resp(conn, 500, "Error")
end)
```

### Mistake 3: Olvidar que Bypass es async en el handler
```elixir
# ❌ Modificar state externo sin sincronización en handlers concurrentes
count = 0
Bypass.expect(bypass, "GET", "/", fn conn ->
  count = count + 1  # No funciona — Elixir tiene variables inmutables
  Plug.Conn.resp(conn, 200, "ok")
end)

# ✓ Usar :counters (atómico) o Agent para contar llamadas
counter = :counters.new(1, [])
Bypass.expect(bypass, "GET", "/", fn conn ->
  :counters.add(counter, 1, 1)
  Plug.Conn.resp(conn, 200, "ok")
end)
```

### Mistake 4: No verificar que el path coincide exactamente
```elixir
# ❌ Bypass matchea paths exactos — una barra final puede causar 404
Bypass.expect_once(bypass, "GET", "/users", fn conn -> ... end)
# Si el cliente llama a /users/ (con barra final), Bypass no matchea

# ✓ Asegurarse de que el path del cliente y el de Bypass coinciden
# O usar Bypass.expect con un handler que inspeccione conn.request_path
Bypass.expect(bypass, fn conn ->
  assert conn.request_path == "/users"
  ...
end)
```

## Verification
```bash
# Tests que deben pasar:
mix test test/github_client_test.exs
mix test test/retryable_client_test.exs
mix test test/timeout_aware_client_test.exs

# Verificar que los tests son async (todos en paralelo):
mix test test/ --trace
# Debe mostrar tests de distintos módulos intercalados

# Verificar que el timeout test falla si eliminas la verificación:
# Cambiar el test para que expect recibir {:ok, _} y ver que falla
```

Checklist de verificación:
- [ ] Cada test tiene su propio `Bypass.open()` — no hay puerto compartido
- [ ] `expect_once` se usa cuando exactamente 1 llamada es esperada
- [ ] `expect` se usa cuando puede haber múltiples llamadas (retry)
- [ ] Headers del request se verifican con `Plug.Conn.get_req_header/2`
- [ ] Query params se verifican con `conn.query_string |> URI.decode_query/1`
- [ ] Timeout tests usan `System.monotonic_time` para verificar tiempo de espera
- [ ] Connection refused tests usan `Bypass.down/1`

## Summary
- Bypass levanta un servidor HTTP TCP real — testea la serialización, headers y comportamiento de red real
- `expect_once/3` para 1 llamada exacta; `expect/3` para N llamadas; `stub/3` para respuestas permanentes sin verificar
- Simular errores: status codes específicos, `Bypass.down/1` para connection refused, `timer.sleep > timeout` para timeouts
- Para tests async: cada test crea su propio `bypass = Bypass.open()` — no hay conflictos de puerto
- Bypass vs Mox: usa Bypass cuando el HTTP en sí importa (parsing, headers, errores de red); Mox cuando es un detalle de implementación

## What's Next
**41-concurrent-testing-exunit**: El testing concurrente introduce challenges únicos: estado compartido, named ETS tables, GenServers globales. Aprende a convertir suites lentas a async y resolver los conflictos que emergen.

## Resources
- [Bypass — HexDocs](https://hexdocs.pm/bypass/Bypass.html)
- [Bypass — GitHub](https://github.com/PSPDFKit-labs/bypass)
- [Req — HexDocs](https://hexdocs.pm/req/Req.html)
- [Plug.Conn — HexDocs](https://hexdocs.pm/plug/Plug.Conn.html)
