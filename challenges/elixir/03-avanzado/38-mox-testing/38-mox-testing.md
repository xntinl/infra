# 38. Mock-Based Testing con Mox

**Difficulty**: Avanzado

## Prerequisites
- Dominio de ExUnit y testing en Elixir
- Comprensión de behaviours (`@behaviour`, `@callback`)
- Experiencia con dependency injection en módulos Elixir
- Familiaridad con `Application.get_env/3` para configuración

## Learning Objectives
After completing this exercise, you will be able to:
- Definir behaviours como contratos testables entre módulos
- Crear mocks con `Mox.defmock/2` que implementan un behaviour
- Usar `Mox.expect/3` para verificar que funciones se llaman con los argumentos correctos
- Usar `Mox.stub/3` para respuestas por defecto sin verificar conteo
- Configurar Mox para tests asíncronos con modo global vs privado
- Coordinar múltiples mocks en tests complejos de integración

## Concepts

### El problema con módulos sin behaviour

Sin Mox, mockear implica:
- Condicionales en código de producción (`if Mix.env() == :test`)
- Módulos fake escritos a mano (frágiles, sin garantías de contrato)
- Monkey-patching (imposible en Elixir, que no permite modificar módulos en caliente)

### Mox: mocking basado en behaviours

La clave: el código de producción nunca referencia un módulo concreto directamente. Referencia un módulo configurado:

```elixir
# ❌ Hardcoded — no se puede mockear
def fetch_user(id) do
  HTTPoison.get("https://api.example.com/users/#{id}")
end

# ✓ Configurable — se puede mockear
def fetch_user(id) do
  http_client().get("https://api.example.com/users/#{id}")
end

defp http_client do
  Application.get_env(:my_app, :http_client, HTTPoison)
end
```

En `test/test_helper.exs`:
```elixir
Mox.defmock(MockHTTPClient, for: MyApp.HTTPClientBehaviour)
Application.put_env(:my_app, :http_client, MockHTTPClient)
```

### El workflow de Mox

```elixir
# 1. Definir el behaviour (contrato)
defmodule MyApp.HTTPClientBehaviour do
  @callback get(url :: String.t()) :: {:ok, map()} | {:error, atom()}
  @callback post(url :: String.t(), body :: map()) :: {:ok, map()} | {:error, atom()}
end

# 2. Implementación real
defmodule MyApp.HTTPClient do
  @behaviour MyApp.HTTPClientBehaviour
  # ...implementación real con HTTPoison...
end

# 3. En test_helper.exs
Mox.defmock(MyApp.MockHTTPClient, for: MyApp.HTTPClientBehaviour)

# 4. En los tests
test "fetch_user llama al cliente HTTP" do
  Mox.expect(MyApp.MockHTTPClient, :get, fn url ->
    assert url == "https://api.example.com/users/123"
    {:ok, %{id: 123, name: "Alice"}}
  end)

  assert {:ok, user} = MyModule.fetch_user(123)
  assert user.name == "Alice"
  # Al final del test, Mox verifica que :get fue llamado exactamente una vez
end
```

### expect vs stub

| | `expect/3` | `stub/3` |
|---|---|---|
| Verifica que se llamó | Sí (falla si no se llama) | No |
| Verifica conteo | Sí (N veces) | No |
| Bueno para | "Esta función DEBE llamarse" | "Siempre retorna X" |
| En loops | Necesitas N `expect` | Un `stub` cubre todas las llamadas |

```elixir
# expect: verificación estricta
Mox.expect(Mock, :get, fn _ -> {:ok, "data"} end)
# Falla si :get no se llama, o se llama más de una vez

# expect N veces
Mox.expect(Mock, :get, 3, fn _ -> {:ok, "data"} end)
# Debe llamarse exactamente 3 veces

# stub: respuesta permanente sin verificación
Mox.stub(Mock, :get, fn _ -> {:ok, "data"} end)
# Se puede llamar 0 o N veces — no importa
```

### Modo global vs privado en tests async

```elixir
# Modo privado (default, recomendado para async: true)
# Cada test tiene sus propias expectativas — no hay interferencia entre tests
Mox.set_mox_private(context)  # o automático en async: true

# Modo global (necesario cuando el código bajo test usa procesos spawneados)
Mox.set_mox_global(context)
# Todos los tests comparten el mismo mock — peligroso en async
```

### verify_on_exit!

Automáticamente verifica al final de cada test que todos los `expect` fueron satisfechos:
```elixir
setup :verify_on_exit!
```

## Exercises

### Exercise 1: Define Behaviour y Defmock

Crea el contrato para un cliente de Slack, su mock, y verifica que el mock implementa el behaviour.

```elixir
# lib/my_app/slack_behaviour.ex
defmodule MyApp.SlackBehaviour do
  @moduledoc """
  Contrato para enviar mensajes a Slack.
  Permite mockear en tests sin llamar a la API real.
  """

  @doc "Envía un mensaje de texto a un canal"
  @callback send_message(channel :: String.t(), text :: String.t()) ::
    {:ok, %{ts: String.t(), channel: String.t()}} | {:error, atom()}

  @doc "Sube un archivo a Slack"
  @callback upload_file(channel :: String.t(), filename :: String.t(), content :: binary()) ::
    {:ok, %{file_id: String.t()}} | {:error, atom()}

  @doc "Obtiene la lista de miembros de un canal"
  @callback list_members(channel :: String.t()) ::
    {:ok, [String.t()]} | {:error, atom()}
end

# lib/my_app/slack_client.ex
defmodule MyApp.SlackClient do
  @behaviour MyApp.SlackBehaviour

  # TODO: Implementar con HTTPoison o similar
  # Por ahora, implementación fake para el ejercicio
  def send_message(channel, text) do
    IO.puts("Enviando a #{channel}: #{text}")
    {:ok, %{ts: "1234567890.123", channel: channel}}
  end

  def upload_file(channel, filename, _content) do
    {:ok, %{file_id: "F#{channel}_#{filename}"}}
  end

  def list_members(channel) do
    {:ok, ["U001", "U002", "U003"]}
  end
end

# lib/my_app/notifier.ex
defmodule MyApp.Notifier do
  @moduledoc "Envía notificaciones via Slack"

  def slack_client do
    Application.get_env(:my_app, :slack_client, MyApp.SlackClient)
  end

  def notify_team(channel, event) do
    message = format_message(event)
    slack_client().send_message(channel, message)
  end

  def notify_with_report(channel, report_name, report_data) do
    content = generate_report(report_data)

    with {:ok, _msg} <- slack_client().send_message(channel, "Reporte listo: #{report_name}"),
         {:ok, file} <- slack_client().upload_file(channel, "#{report_name}.txt", content) do
      {:ok, file}
    end
  end

  defp format_message(event), do: "Evento: #{inspect(event)}"
  defp generate_report(data), do: "REPORT\n#{inspect(data)}"
end

# test/test_helper.exs — añadir:
# Mox.defmock(MyApp.MockSlackClient, for: MyApp.SlackBehaviour)

# test/my_app/notifier_test.exs
defmodule MyApp.NotifierTest do
  use ExUnit.Case, async: true

  import Mox

  # TODO: Añadir setup :verify_on_exit!

  test "defmock crea un módulo que implementa el behaviour" do
    # TODO: Verificar que MyApp.MockSlackClient existe como módulo
    # Pista: Code.ensure_loaded?/1
    assert Code.ensure_loaded?(MyApp.MockSlackClient)

    # TODO: Verificar que implementa SlackBehaviour
    # Pista: los behaviours se listan en module_info(:attributes)
    behaviours = MyApp.MockSlackClient.module_info(:attributes)
                 |> Keyword.get_values(:behaviour)
                 |> List.flatten()
    assert MyApp.SlackBehaviour in behaviours
  end

  test "el mock responde correctamente cuando se configura" do
    # TODO: Configurar Application.put_env para que Notifier use el mock
    # TODO: Usar stub para que send_message retorne {:ok, %{ts: "test", channel: "#general"}}
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)

    Mox.stub(MyApp.MockSlackClient, :send_message, fn _channel, _text ->
      {:ok, %{ts: "test_ts", channel: "#general"}}
    end)

    result = MyApp.Notifier.notify_team("#general", :deployment_done)
    assert {:ok, %{ts: "test_ts"}} = result
  end
end
```

**Hints**:
- `Mox.defmock/2` solo necesita ejecutarse una vez (en `test_helper.exs`), no en cada test
- Si defines el mock dentro del test file, obtendrás un error de "módulo ya definido" en la segunda ejecución
- `setup :verify_on_exit!` es necesario para que los `expect` fallen el test si no son satisfechos

**One possible solution** (sparse):
```elixir
# test_helper.exs:
Mox.defmock(MyApp.MockSlackClient, for: MyApp.SlackBehaviour)
Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)

# Setup en el test module:
setup :verify_on_exit!
```

---

### Exercise 2: expect/3 — Verificar Argumentos y Conteo

Tests que verifican exactamente qué argumentos se pasan al mock y cuántas veces se llama.

```elixir
defmodule MyApp.NotifierExpectTest do
  use ExUnit.Case, async: true
  import Mox

  setup :verify_on_exit!

  setup do
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)
    :ok
  end

  test "notify_team llama a send_message con el canal y texto correctos" do
    # TODO: Usar expect/3 para verificar que send_message es llamada
    # con channel == "#devops" y que el texto contiene "deployment_done"
    Mox.expect(MyApp.MockSlackClient, :send_message, fn channel, text ->
      # TODO: assert channel == "#devops"
      # TODO: assert String.contains?(text, "deployment_done") (o inspect del evento)
      {:ok, %{ts: "123", channel: channel}}
    end)

    MyApp.Notifier.notify_team("#devops", :deployment_done)
    # verify_on_exit! verifica automáticamente que se llamó exactamente una vez
  end

  test "notify_with_report llama a send_message Y upload_file exactamente una vez cada una" do
    # TODO: expect send_message una vez
    Mox.expect(MyApp.MockSlackClient, :send_message, fn _channel, text ->
      # TODO: assert que el texto menciona el nombre del reporte
      assert String.contains?(text, "weekly_report")
      {:ok, %{ts: "msg_ts", channel: "#reports"}}
    end)

    # TODO: expect upload_file una vez
    # Verificar que el filename es "weekly_report.txt"
    Mox.expect(MyApp.MockSlackClient, :upload_file, fn _channel, filename, _content ->
      assert filename == "weekly_report.txt"
      {:ok, %{file_id: "F12345"}}
    end)

    assert {:ok, %{file_id: "F12345"}} =
      MyApp.Notifier.notify_with_report("#reports", "weekly_report", %{data: "..."})
  end

  test "NO llama a upload_file si send_message falla" do
    # Configurar send_message para que falle
    Mox.expect(MyApp.MockSlackClient, :send_message, fn _channel, _text ->
      {:error, :channel_not_found}
    end)

    # upload_file NO debe llamarse — si se llama, el test falla
    # (porque no tenemos expect para upload_file)

    assert {:error, :channel_not_found} =
      MyApp.Notifier.notify_with_report("#nonexistent", "report", %{})
  end

  test "expect N veces: send_message llamada 3 veces en batch" do
    # Simular un notificador que envía a 3 canales
    # TODO: expect send_message 3 veces
    Mox.expect(MyApp.MockSlackClient, :send_message, 3, fn _channel, _text ->
      {:ok, %{ts: "ts", channel: "ch"}}
    end)

    channels = ["#dev", "#qa", "#ops"]
    Enum.each(channels, fn ch ->
      MyApp.Notifier.notify_team(ch, :deploy_started)
    end)
  end
end
```

**Hints**:
- Si `expect/3` espera 1 llamada y la función es llamada 2 veces, Mox falla con "unexpectedly called"
- Si `expect/3` espera 1 llamada y nunca se llama, `verify_on_exit!` falla con "expected ... to be called once but it was called 0 times"
- Para verificar que una función NO se llama, simplemente no pongas `expect` para ella — si se llama sin `expect`, el test falla automáticamente

**One possible solution** (sparse):
```elixir
# Test 1 — expect con asserts en la función:
Mox.expect(MyApp.MockSlackClient, :send_message, fn channel, text ->
  assert channel == "#devops"
  assert String.contains?(text, "deployment_done")
  {:ok, %{ts: "123", channel: channel}}
end)

# Test 4 — expect N veces:
Mox.expect(MyApp.MockSlackClient, :send_message, 3, fn _channel, _text ->
  {:ok, %{ts: "ts", channel: "ch"}}
end)
```

---

### Exercise 3: stub/3 — Respuestas por defecto sin verificar conteo

Usa `stub/3` para módulos de soporte en tests donde no importa cuántas veces se llaman.

```elixir
defmodule MyApp.NotifierStubTest do
  use ExUnit.Case, async: true
  import Mox

  setup :verify_on_exit!

  setup do
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)

    # TODO: Usar stub para que send_message siempre retorne éxito
    # Sin importar cuántas veces se llame
    Mox.stub(MyApp.MockSlackClient, :send_message, fn _ch, _txt ->
      {:ok, %{ts: "stub_ts", channel: "stub_ch"}}
    end)

    :ok
  end

  test "puede llamar send_message cualquier número de veces" do
    # Estos tests no verifican SI se llama, solo el resultado
    assert {:ok, _} = MyApp.Notifier.notify_team("#ch1", :event1)
    assert {:ok, _} = MyApp.Notifier.notify_team("#ch2", :event2)
    assert {:ok, _} = MyApp.Notifier.notify_team("#ch3", :event3)
    # No hay verify — stub no verifica conteo
  end

  test "stub puede sobreescribirse con expect en un test específico" do
    # El stub del setup está activo, pero podemos añadir un expect más específico
    Mox.expect(MyApp.MockSlackClient, :send_message, fn channel, _text ->
      # Este expect tiene prioridad sobre el stub para la primera llamada
      assert channel == "#alerts"
      {:ok, %{ts: "alert_ts", channel: "#alerts"}}
    end)

    assert {:ok, %{ts: "alert_ts"}} = MyApp.Notifier.notify_team("#alerts", :critical)
  end

  test "stub permite testear lógica de negocio sin acoplarse al mock" do
    # Este test verifica la transformación del mensaje, no las llamadas al mock
    # El stub absorbe las llamadas sin que el test se preocupe por ellas

    # TODO: verificar que notify_team devuelve {:ok, _} para cualquier evento
    events = [:deploy, :rollback, :alert, :info]
    results = Enum.map(events, fn event ->
      MyApp.Notifier.notify_team("#general", event)
    end)

    assert Enum.all?(results, &match?({:ok, _}, &1))
  end
end
```

**Hints**:
- `stub` en `setup` es ideal para dependencias "de soporte" — siempre necesitas que retornen algo para que el código no explote, pero el test no verifica esas llamadas
- `stub` + `expect` pueden coexistir: el `expect` toma prioridad para las llamadas que espera; el `stub` maneja el resto
- Si defines un `stub` en `setup` y el test añade un `expect`, la verificación del `expect` aún aplica (verify_on_exit! lo chequea)

**One possible solution** (sparse):
```elixir
# Test 3 — todos los resultados son éxito:
events = [:deploy, :rollback, :alert, :info]
results = Enum.map(events, &MyApp.Notifier.notify_team("#general", &1))
assert Enum.all?(results, &match?({:ok, _}, &1))
```

---

### Exercise 4: Async Tests — Modo Global vs Privado

Configura correctamente Mox para tests async que usan procesos externos.

```elixir
defmodule MyApp.AsyncWorker do
  @moduledoc "Worker que procesa en un proceso separado"

  def process_and_notify(items, channel) do
    # Este proceso es spawneado — no es el proceso del test
    parent = self()
    pid = spawn(fn ->
      results = Enum.map(items, fn item ->
        # Aquí se llama al mock — pero desde un proceso diferente al test
        slack_client = Application.get_env(:my_app, :slack_client, MyApp.SlackClient)
        slack_client.send_message(channel, "Procesado: #{inspect(item)}")
      end)
      send(parent, {:done, results})
    end)

    receive do
      {:done, results} -> {:ok, results}
    after
      5_000 -> {:error, :timeout}
    end
  end
end

defmodule MyApp.AsyncPrivateMoxTest do
  use ExUnit.Case, async: true  # Tests en paralelo
  import Mox

  setup :set_mox_private   # Modo privado: cada test tiene su propio contexto
  setup :verify_on_exit!

  setup do
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)
    :ok
  end

  test "modo privado: el mock solo es visible para el proceso del test" do
    # En modo privado, el mock configurado en este test
    # NO está disponible para procesos spawneados
    Mox.stub(MyApp.MockSlackClient, :send_message, fn _ch, _txt ->
      {:ok, %{ts: "private_ts", channel: "ch"}}
    end)

    # Una llamada directa desde el proceso del test funciona
    assert {:ok, _} = MyApp.MockSlackClient.send_message("#test", "hola")
  end
end

defmodule MyApp.AsyncGlobalMoxTest do
  use ExUnit.Case, async: false  # NO async cuando usas modo global
  import Mox

  setup :set_mox_global   # Modo global: visible para todos los procesos
  setup :verify_on_exit!

  setup do
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)
    :ok
  end

  test "modo global: el mock es visible para procesos spawneados" do
    # TODO: Usar expect/stub para send_message que retorne {:ok, %{ts: "global", channel: ch}}
    Mox.stub(MyApp.MockSlackClient, :send_message, fn channel, _txt ->
      {:ok, %{ts: "global_ts", channel: channel}}
    end)

    # AsyncWorker spawna un proceso — en modo global, el mock está disponible
    assert {:ok, results} = MyApp.AsyncWorker.process_and_notify([1, 2, 3], "#async")

    assert length(results) == 3
    assert Enum.all?(results, &match?({:ok, _}, &1))
  end

  test "modo global con allow/3: alternativa a set_mox_global" do
    # TODO: Demostrar Mox.allow/3 como alternativa más precisa a set_mox_global
    # allow permite que un proceso específico use el mock del test actual
    # Mox.allow(MockModule, self(), spawned_pid)
    # Nota: esto requiere conocer el PID del proceso hijo de antemano
    #       (o usar Task y capturar el pid)

    Mox.stub(MyApp.MockSlackClient, :send_message, fn _ch, _txt ->
      {:ok, %{ts: "allowed", channel: "ch"}}
    end)

    task = Task.async(fn ->
      # Mox.allow en el proceso padre antes de que el hijo llame al mock
      MyApp.MockSlackClient.send_message("#task", "from task")
    end)

    # Permitir que el proceso del Task use el mock de este test
    Mox.allow(MyApp.MockSlackClient, self(), task.pid)

    result = Task.await(task)
    assert {:ok, _} = result
  end
end
```

**Hints**:
- `set_mox_private` (default) es thread-safe para async — cada test aísla sus mocks
- `set_mox_global` comparte mocks entre tests — debe usarse con `async: false` para evitar interferencias
- `Mox.allow/3` es la alternativa más quirúrgica: permite explícitamente que un proceso hijo use el mock del proceso padre, sin hacer todo global

**One possible solution** (sparse):
```elixir
# El código de AsyncGlobalMoxTest ya tiene la solución completa.
# La clave es entender cuándo usar cada modo:
# - async: true + set_mox_private: para tests unitarios normales
# - async: false + set_mox_global: cuando el código spawna procesos
# - async: true + Mox.allow/3: para Tasks donde conoces el PID
```

---

### Exercise 5: Multiple Mocks — Coordinar Varios Mocks en Test Complejo

Test de integración que coordina tres mocks: Slack, una base de datos y un servicio de email.

```elixir
# Behaviours adicionales para el ejercicio
defmodule MyApp.DatabaseBehaviour do
  @callback get_users(role :: String.t()) :: {:ok, [map()]} | {:error, atom()}
  @callback save_audit_log(event :: map()) :: {:ok, integer()} | {:error, atom()}
end

defmodule MyApp.EmailBehaviour do
  @callback send(to :: String.t(), subject :: String.t(), body :: String.t()) ::
    {:ok, String.t()} | {:error, atom()}
end

# En test_helper.exs:
# Mox.defmock(MyApp.MockDB, for: MyApp.DatabaseBehaviour)
# Mox.defmock(MyApp.MockEmail, for: MyApp.EmailBehaviour)

defmodule MyApp.ReportService do
  @moduledoc "Servicio que genera y distribuye reportes"

  def generate_and_distribute(report_type, channel) do
    db     = Application.get_env(:my_app, :database, MyApp.Database)
    slack  = Application.get_env(:my_app, :slack_client, MyApp.SlackClient)
    email  = Application.get_env(:my_app, :email, MyApp.Email)

    with {:ok, users}    <- db.get_users("admin"),
         {:ok, _slack}   <- slack.send_message(channel, "Generando #{report_type}..."),
         {:ok, _audit}   <- db.save_audit_log(%{type: report_type, users: length(users)}),
         {:ok, _email}   <- email.send("team@company.com", "Report Ready", "#{report_type} completado") do
      {:ok, %{users: length(users), report: report_type}}
    end
  end
end

defmodule MyApp.MultipleMocksTest do
  use ExUnit.Case, async: true
  import Mox

  setup :verify_on_exit!

  setup do
    Application.put_env(:my_app, :slack_client, MyApp.MockSlackClient)
    Application.put_env(:my_app, :database, MyApp.MockDB)
    Application.put_env(:my_app, :email, MyApp.MockEmail)
    :ok
  end

  test "generate_and_distribute: flujo feliz con los 3 servicios" do
    # TODO: Configurar los 3 mocks para el flujo feliz

    # Mock DB: get_users retorna 3 admins
    Mox.expect(MyApp.MockDB, :get_users, fn role ->
      assert role == "admin"
      {:ok, [%{id: 1, name: "Alice"}, %{id: 2, name: "Bob"}, %{id: 3, name: "Charlie"}]}
    end)

    # TODO: Mock Slack: send_message retorna éxito
    Mox.expect(MyApp.MockSlackClient, :send_message, fn _channel, text ->
      assert String.contains?(text, "monthly")
      {:ok, %{ts: "msg_ts", channel: "#reports"}}
    end)

    # TODO: Mock DB: save_audit_log retorna {:ok, 1} (1 fila insertada)
    Mox.expect(MyApp.MockDB, :save_audit_log, fn audit ->
      assert audit.type == "monthly"
      assert audit.users == 3
      {:ok, 1}
    end)

    # TODO: Mock Email: send retorna {:ok, "message_id_123"}
    Mox.expect(MyApp.MockEmail, :send, fn to, subject, _body ->
      assert to == "team@company.com"
      assert String.contains?(subject, "Ready")
      {:ok, "email_id_123"}
    end)

    assert {:ok, %{users: 3, report: "monthly"}} =
      MyApp.ReportService.generate_and_distribute("monthly", "#reports")
  end

  test "generate_and_distribute: falla si get_users falla" do
    # TODO: Mock DB: get_users falla
    Mox.expect(MyApp.MockDB, :get_users, fn _role ->
      {:error, :connection_timeout}
    end)

    # Slack y Email NO deben llamarse si DB falla primero
    # (no ponemos expect para ellos — si se llaman, el test falla)

    assert {:error, :connection_timeout} =
      MyApp.ReportService.generate_and_distribute("monthly", "#reports")
  end

  test "generate_and_distribute: falla si Slack falla (y no se envía email)" do
    # DB funciona
    Mox.expect(MyApp.MockDB, :get_users, fn _role ->
      {:ok, [%{id: 1}]}
    end)

    # Slack falla
    Mox.expect(MyApp.MockSlackClient, :send_message, fn _ch, _txt ->
      {:error, :channel_archived}
    end)

    # TODO: save_audit_log y email NO deben llamarse
    # (with/2 hace short-circuit en el primer error)

    assert {:error, :channel_archived} =
      MyApp.ReportService.generate_and_distribute("monthly", "#archived")
  end
end
```

**Hints**:
- Cuando usas `with/2` en el código de producción, el short-circuit garantiza que funciones downstream no se llaman — úsalo a tu favor en tests verificando que ciertos mocks NO son llamados
- Múltiples `expect` en el mismo test se ejecutan en el orden en que se llaman; para el mismo mock y función, se encolan (FIFO)
- `setup` de módulo es el lugar ideal para configurar `Application.put_env` de todos los mocks — evita repetición y garantiza que cada test empieza limpio

**One possible solution** (sparse):
```elixir
# Test "falla si Slack falla":
# Simplemente no añadas expect para :save_audit_log ni :send
# Si se llaman, Mox falla con "unexpectedly called"
# El with/2 en ReportService hace short-circuit automáticamente

# Para el test completo, el orden de expects refleja el orden de ejecución:
# 1. get_users
# 2. send_message (Slack)
# 3. save_audit_log
# 4. send (Email)
```

## Common Mistakes

### Mistake 1: Definir defmock en el test file en lugar de test_helper.exs
```elixir
# ❌ En test/my_module_test.exs — falla en la segunda ejecución
Mox.defmock(MyMock, for: MyBehaviour)

# ✓ En test/test_helper.exs — se ejecuta solo una vez por suite
Mox.defmock(MyMock, for: MyBehaviour)
```

### Mistake 2: async: true con set_mox_global
```elixir
# ❌ Con async: true y set_mox_global, los expects de un test
# pueden ser consumidos por otro test en paralelo → fallos aleatorios
use ExUnit.Case, async: true
setup :set_mox_global  # Peligroso con async: true

# ✓ set_mox_global solo con async: false
use ExUnit.Case, async: false
setup :set_mox_global
```

### Mistake 3: Olvidar verify_on_exit!
```elixir
# ❌ Sin verify_on_exit!, un expect no satisfecho NO falla el test
test "llama al cliente" do
  Mox.expect(MockClient, :get, fn _ -> {:ok, "data"} end)
  # Olvidamos llamar a la función que usa el mock
  # El test pasa — falsa seguridad
end

# ✓ Con setup :verify_on_exit!, el test falla si el expect no se satisface
setup :verify_on_exit!
```

### Mistake 4: No inyectar el mock en el código de producción
```elixir
# ❌ El código de producción referencia el módulo real directamente
defmodule MyModule do
  def fetch(id), do: HTTPClient.get("/api/#{id}")  # No se puede mockear
end

# ✓ Configurar la dependencia via Application.get_env
defmodule MyModule do
  def fetch(id) do
    http_client().get("/api/#{id}")
  end
  defp http_client, do: Application.get_env(:my_app, :http_client, HTTPClient)
end
```

## Verification
```bash
# Ejecutar la suite de tests de Mox
mix test test/my_app/notifier_test.exs
mix test test/my_app/notifier_expect_test.exs
mix test test/my_app/multiple_mocks_test.exs

# Verificar que todos pasan
# Verificar que si eliminas un expect, el test falla con mensaje claro de Mox
```

Checklist de verificación:
- [ ] `Mox.defmock/2` en `test_helper.exs`, no en los test files
- [ ] `setup :verify_on_exit!` en todos los módulos de test que usan `expect`
- [ ] `expect` falla cuando la función no es llamada (verifica el mensaje de error)
- [ ] `stub` no falla si la función no es llamada
- [ ] Tests async usan `set_mox_private` (o ninguno — es el default)
- [ ] Tests con procesos spawneados usan `set_mox_global` + `async: false`
- [ ] El código de producción inyecta dependencias via `Application.get_env`

## Summary
- Mox requiere behaviours como contratos — el mock implementa el mismo contrato que el módulo real
- `expect/3` verifica que la función se llama exactamente N veces con los argumentos esperados
- `stub/3` configura una respuesta por defecto sin verificar conteo — para dependencias de soporte
- `verify_on_exit!` es el guard que hace que los expects fallen el test si no se satisfacen
- Modo privado (default) es seguro para `async: true`; modo global requiere `async: false`
- La inyección de dependencias via `Application.get_env` es el patrón estándar de Elixir para testabilidad

## What's Next
**39-property-based-testing**: Después de dominar el testing con mocks, aprende a generar casos de prueba automáticamente con StreamData para descubrir edge cases que nunca hubieras escrito manualmente.

## Resources
- [Mox — HexDocs](https://hexdocs.pm/mox/Mox.html)
- [Mox — GitHub](https://github.com/dashbitco/mox)
- [José Valim — Mocks and explicit contracts](https://dashbit.co/blog/mocks-and-explicit-contracts)
- [Testing Elixir — O'Reilly (capítulo sobre Mox)](https://pragprog.com/titles/lmelixir/testing-elixir/)
