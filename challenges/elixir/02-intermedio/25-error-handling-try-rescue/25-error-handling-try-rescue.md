# 25. Error Handling: try/rescue/catch/else/after

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–24
- Familiarity with `{:ok, result}` / `{:error, reason}` pattern
- Understanding of Elixir exceptions vs Erlang errors
- Comfortable with pattern matching and anonymous functions

## Learning Objectives
After completing this exercise, you will be able to:
- Capturar excepciones con `try/rescue` y manejar múltiples tipos de error
- Usar la cláusula `else` para manejar el caso de éxito por separado
- Usar la cláusula `after` para ejecutar limpieza garantizada (como `finally`)
- Lanzar excepciones con `raise/1` y `raise/2`
- Relanzar excepciones preservando el stack trace con `reraise/2`
- Definir excepciones personalizadas con `defexception`
- Distinguir entre errores de Elixir (`RuntimeError`) y errores de Erlang (`:error`)

## Concepts

### La filosofía de errores en Elixir: dos estilos

Elixir tiene dos patrones de manejo de errores que coexisten: el estilo funcional con `{:ok, value}` / `{:error, reason}`, y el estilo de excepciones con `raise` / `rescue`. El primero es preferido para errores esperados (validaciones, recursos no encontrados). El segundo se reserva para errores inesperados o situaciones excepcionales.

La regla práctica es: si un error es parte del flujo normal del programa, usa `{:ok, _}` / `{:error, _}`. Si es verdaderamente excepcional (bug, corrupción de datos, fallo de hardware), usa excepciones.

```elixir
# Estilo funcional — para errores esperados
def find_user(id) do
  case Database.lookup(id) do
    nil -> {:error, :not_found}
    user -> {:ok, user}
  end
end

# Estilo excepción — para situaciones excepcionales
def load_config! do
  case File.read("config.json") do
    {:ok, content} -> Jason.decode!(content)
    {:error, reason} -> raise "Cannot load config: #{reason}"
  end
end
```

### try/rescue: capturar excepciones específicas

`rescue` captura excepciones por su tipo. Puedes capturar múltiples tipos en una sola cláusula o en cláusulas separadas. La variable después de `in` es la excepción misma, con campos accesibles como `e.message`.

```elixir
try do
  String.to_integer("not_a_number")
rescue
  e in ArgumentError ->
    IO.puts("Argument error: #{e.message}")
    {:error, :bad_argument}
end

# Capturar múltiples tipos en una cláusula
try do
  risky_operation()
rescue
  e in [RuntimeError, ArgumentError] ->
    {:error, Exception.message(e)}
  e in KeyError ->
    {:error, {:missing_key, e.key}}
end
```

### La cláusula else

`else` en `try` se ejecuta cuando el bloque `try` termina sin lanzar excepción. Permite hacer pattern matching sobre el valor de retorno del bloque `try`. Es útil cuando el éxito tiene múltiples casos que quieres manejar por separado:

```elixir
try do
  perform_operation(input)
rescue
  e in RuntimeError -> {:error, e.message}
else
  {:ok, result} ->
    process_result(result)
  {:partial, result} ->
    {:ok, result, :partial}
end
```

### La cláusula after: cleanup garantizado

`after` se ejecuta siempre, sin importar si hubo excepción o no. Es el equivalente de `finally` en otros lenguajes. El valor de retorno de `after` es descartado — el valor del `try` completo es el de la cláusula que manejó (rescue/else/try body).

```elixir
file = File.open!("data.txt")
try do
  process_file(file)
rescue
  e -> Logger.error("Processing failed: #{e.message}")
after
  File.close(file)   # siempre se ejecuta, incluso si hubo excepción
end
```

### raise/1, raise/2 y reraise/2

`raise/1` lanza un `RuntimeError` con el mensaje dado. `raise/2` lanza el tipo de excepción especificado con opciones. `reraise/2` relanza una excepción preservando el stack trace original — importante cuando capturas solo para limpiar recursos y quieres propagar el error original.

```elixir
# raise/1: RuntimeError con mensaje
raise "Something went wrong"

# raise/2: tipo específico con opciones
raise ArgumentError, message: "Expected a non-negative integer"
raise KeyError, key: :missing_field, term: %{}

# reraise: propaga la excepción original con su stack trace
try do
  dangerous_operation()
rescue
  e ->
    Logger.error("Operation failed: #{inspect(e)}")
    reraise e, __STACKTRACE__   # conserva el stack trace original
end
```

### Excepciones personalizadas con defexception

`defexception` define un módulo que implementa la excepción. El campo `:message` es obligatorio (define el texto por defecto). Puedes agregar campos adicionales para contexto estructurado.

```elixir
defmodule DatabaseError do
  defexception [:message, :query, :code]

  def exception(opts) do
    query = Keyword.get(opts, :query, "<unknown>")
    code  = Keyword.get(opts, :code, 0)
    msg   = "Database error #{code} executing: #{query}"
    %__MODULE__{message: msg, query: query, code: code}
  end
end

# Uso:
raise DatabaseError, query: "SELECT * FROM users", code: 1045
# Captura:
rescue
  e in DatabaseError ->
    Logger.error("DB error #{e.code}: #{e.message}")
```

### Errores de Erlang: catch vs rescue

Las funciones de Erlang no lanzan `Exception` — lanzan `:error`, `:exit`, o `:throw`. Para capturarlos, necesitas `catch` en lugar de `rescue`:

```elixir
# rescue captura excepciones de Elixir (structs de Exception)
# catch captura throws/exits/errors de Erlang

try do
  :erlang.error(:badarg)
rescue
  e in ErlangError -> "ErlangError: #{inspect(e)}"
catch
  :error, :badarg -> "Erlang error: badarg"
  :exit, reason  -> "Process exit: #{inspect(reason)}"
  value          -> "Throw: #{inspect(value)}"
end
```

---

## Exercises

### Exercise 1: rescue básico con RuntimeError

```elixir
defmodule BasicRescue do
  def run do
    # TODO 1: Usa try/rescue para capturar el RuntimeError lanzado por risky/0
    # Cuando hay error, retorna {:error, message} donde message es e.message
    # Cuando no hay error, retorna {:ok, result}
    result = try do
      risky()
    rescue
      # TODO: captura RuntimeError como `e`, retorna {:error, e.message}
    end
    IO.inspect(result)   # => {:error, "intentional failure"}

    # TODO 2: Escribe safe_divide/2 que usa try/rescue para capturar
    # ArithmeticError cuando se divide entre cero.
    # Retorna {:ok, result} o {:error, :division_by_zero}
    IO.inspect(safe_divide(10, 2))   # => {:ok, 5.0}
    IO.inspect(safe_divide(5, 0))    # => {:error, :division_by_zero}

    # TODO 3: ¿Qué pasa si haces Integer.parse("abc")?
    # No lanza excepción — retorna :error. Esto NO se puede capturar con rescue.
    # Escribe un comentario explicando la diferencia.
    # Integer.parse("abc") => :error (no exception, no rescue needed)
  end

  defp risky do
    raise "intentional failure"
  end

  def safe_divide(a, b) do
    # TODO: try/rescue que captura ArithmeticError
    # PISTA: en Elixir, 5 / 0 lanza ArithmeticError
    # Pero 5.0 / 0 retorna Infinity (no lanza)
    # Usa división entera: div(a, b) para provocar el error
  end
end

BasicRescue.run()
```

---

### Exercise 2: Múltiples rescue y tipos de error

```elixir
defmodule MultiRescue do
  @doc """
  Intenta parsear un valor según el tipo especificado.
  Maneja diferentes tipos de error con diferentes respuestas.
  """
  def parse(value, type) do
    try do
      do_parse(value, type)
    rescue
      # TODO 1: Captura ArgumentError y retorna {:error, {:bad_argument, e.message}}
      # TODO 2: Captura RuntimeError y retorna {:error, {:runtime, e.message}}
      # TODO 3: Captura UndefinedFunctionError y retorna {:error, :unsupported_type}
      # PISTA: puedes tener múltiples cláusulas rescue separadas
      # O usar: e in [ArgumentError, RuntimeError] -> ...
    end
  end

  defp do_parse(value, :integer) do
    case Integer.parse(value) do
      {n, ""} -> n
      _       -> raise ArgumentError, "not a valid integer: #{value}"
    end
  end

  defp do_parse(value, :float) do
    case Float.parse(value) do
      {f, ""} -> f
      _       -> raise ArgumentError, "not a valid float: #{value}"
    end
  end

  defp do_parse(_value, :boolean) do
    raise RuntimeError, "boolean parsing not implemented"
  end

  defp do_parse(value, unknown_type) do
    raise UndefinedFunctionError,
      module: __MODULE__,
      function: :"parse_#{unknown_type}",
      arity: 1
  end

  def run do
    IO.inspect(parse("42", :integer))      # => 42
    IO.inspect(parse("abc", :integer))     # => {:error, {:bad_argument, "..."}}
    IO.inspect(parse("3.14", :float))      # => 3.14
    IO.inspect(parse("true", :boolean))    # => {:error, {:runtime, "..."}}
    IO.inspect(parse("hi", :xml))          # => {:error, :unsupported_type}
  end
end

MultiRescue.run()
```

---

### Exercise 3: La cláusula else

```elixir
defmodule ElseClause do
  def process_file(path) do
    try do
      File.read!(path)
    rescue
      e in File.Error ->
        {:error, "Cannot read file: #{e.message}"}
    else
      # TODO 1: La cláusula else recibe el valor retornado por el bloque try
      # Haz pattern matching sobre el contenido del archivo:
      # - Si está vacío (""), retorna {:error, :empty_file}
      # - Si tiene contenido, retorna {:ok, content, byte_size(content)}
      # PISTA: else recibe el resultado del try body (el string del archivo)
      "" ->
        # TODO
      content ->
        # TODO
    end
  end

  def safe_call(fun) when is_function(fun, 0) do
    try do
      fun.()
    rescue
      e -> {:rescued, Exception.message(e)}
    else
      # TODO 2: La cláusula else solo se ejecuta si NO hubo excepción
      # Haz pattern matching sobre el resultado:
      # - {:ok, value} -> retorna {:success, value}
      # - {:error, reason} -> retorna {:failure, reason}
      # - otro valor -> retorna {:unknown, valor}
      {:ok, value} ->
        # TODO
      {:error, reason} ->
        # TODO
      other ->
        # TODO
    end
  end

  def run do
    # Crea un archivo temporal para el test
    File.write!("/tmp/test_else.txt", "Hello, Elixir!")
    IO.inspect(process_file("/tmp/test_else.txt"))    # => {:ok, "Hello, Elixir!", 14}

    File.write!("/tmp/empty_else.txt", "")
    IO.inspect(process_file("/tmp/empty_else.txt"))   # => {:error, :empty_file}

    IO.inspect(process_file("/nonexistent.txt"))      # => {:error, "Cannot read file: ..."}

    IO.inspect(safe_call(fn -> {:ok, 42} end))        # => {:success, 42}
    IO.inspect(safe_call(fn -> {:error, :nope} end))  # => {:failure, :nope}
    IO.inspect(safe_call(fn -> raise "boom" end))     # => {:rescued, "boom"}
    IO.inspect(safe_call(fn -> :weird end))           # => {:unknown, :weird}
  end
end

ElseClause.run()
```

---

### Exercise 4: La cláusula after (cleanup garantizado)

```elixir
defmodule AfterClause do
  @doc """
  Lee un archivo, procesa su contenido, y garantiza que se registra
  en un log de auditoría sin importar si tuvo éxito o error.
  """
  def process_with_audit(path) do
    # TODO 1: Usa try/rescue/after para:
    # - En el bloque try: lee el archivo con File.read!/1, retorna {:ok, content}
    # - En rescue: captura File.Error, retorna {:error, e.message}
    # - En after: siempre imprime "Audit: attempted to read #{path}"
    #   (simula un log de auditoría que debe ejecutarse siempre)
    try do
      # TODO
    rescue
      # TODO
    after
      # TODO: IO.puts("Audit: attempted to read #{path}")
    end
  end

  def with_resource(resource_name, fun) do
    IO.puts("Opening resource: #{resource_name}")
    # TODO 2: Ejecuta fun.() dentro de un try
    # En after: siempre imprime "Closing resource: #{resource_name}"
    # El valor de retorno debe ser el de fun.() (o la excepción relanzada)
    # IMPORTANTE: after NO cambia el valor de retorno — es solo cleanup
    try do
      fun.()
    rescue
      e -> reraise e, __STACKTRACE__   # propaga la excepción original
    after
      # TODO
    end
  end

  def run do
    # Test 1: archivo que existe
    File.write!("/tmp/audit_test.txt", "data")
    result = process_with_audit("/tmp/audit_test.txt")
    IO.inspect(result)
    # Output: "Audit: attempted to read /tmp/audit_test.txt"
    # Result: {:ok, "data"}

    # Test 2: archivo que no existe
    result2 = process_with_audit("/nonexistent_audit.txt")
    IO.inspect(result2)
    # Output: "Audit: attempted to read /nonexistent_audit.txt" (SIEMPRE)
    # Result: {:error, "..."}

    # Test 3: with_resource muestra que after siempre cierra
    with_resource("database_connection", fn ->
      IO.puts("Working with resource...")
      {:ok, "done"}
    end)
    # Output: Opening resource: database_connection
    #         Working with resource...
    #         Closing resource: database_connection
  end
end

AfterClause.run()
```

---

### Exercise 5: Excepciones personalizadas

```elixir
# TODO 1: Define ValidationError con campos :message, :field, y :value
defmodule ValidationError do
  defexception [:message, :field, :value]

  # TODO: Implementa exception/1 que recibe un keyword list y construye
  # el mensaje: "Validation failed for field ':field' with value '#{value}'"
  # PISTA: def exception(opts) do ... %__MODULE__{...} end
  def exception(opts) do
    field = Keyword.fetch!(opts, :field)
    value = Keyword.get(opts, :value)
    # TODO: construye el mensaje y retorna el struct
  end
end

# TODO 2: Define NetworkError con campos :message, :status_code, y :url
defmodule NetworkError do
  defexception [:message, :status_code, :url]

  def exception(opts) do
    code = Keyword.get(opts, :status_code, 0)
    url  = Keyword.get(opts, :url, "<unknown>")
    msg  = "HTTP #{code} error fetching #{url}"
    # TODO: retorna el struct con todos los campos
  end
end

defmodule CustomExceptions do
  def validate_age(age) when is_integer(age) and age >= 0 and age <= 150, do: {:ok, age}
  def validate_age(age) do
    # TODO 3: Lanza ValidationError con field: :age, value: age
    raise ValidationError, field: :age, value: age
  end

  def fetch_url(url) do
    # Simula una respuesta HTTP fallida
    if String.contains?(url, "fail") do
      # TODO 4: Lanza NetworkError con status_code: 503, url: url
      raise NetworkError, status_code: 503, url: url
    else
      {:ok, "Response from #{url}"}
    end
  end

  def run do
    # Test ValidationError
    try do
      validate_age(-5)
    rescue
      e in ValidationError ->
        IO.puts("Caught: #{e.message}")
        IO.inspect({e.field, e.value})
    end
    # => "Caught: Validation failed for field ':age' with value '-5'"
    # => {:age, -5}

    # Test NetworkError
    try do
      fetch_url("https://fail.example.com/api")
    rescue
      e in NetworkError ->
        IO.puts("Network error: #{e.message}")
        IO.inspect(e.status_code)
    end
    # => "Network error: HTTP 503 error fetching https://fail.example.com/api"
    # => 503

    # TODO 5: Escribe un try/rescue que captura AMBAS excepciones en una sola cláusula
    # usando la sintaxis: rescue e in [ValidationError, NetworkError] -> ...
    try do
      if :rand.uniform() > 0.5, do: validate_age(-1), else: fetch_url("http://fail.com")
    rescue
      # TODO: e in [ValidationError, NetworkError] -> imprime e.message
    end
  end
end

CustomExceptions.run()
```

---

## Common Mistakes

### Usar try/rescue para control de flujo normal

```elixir
# MAL: try/rescue es costoso y semánticamente incorrecto para flujo normal
def find_or_default(map, key, default) do
  try do
    Map.fetch!(map, key)
  rescue
    KeyError -> default
  end
end

# BIEN: Map.get ya maneja el caso de clave ausente
def find_or_default(map, key, default) do
  Map.get(map, key, default)
end
```

### reraise sin __STACKTRACE__

```elixir
# MAL: reraise(e, []) pierde el stack trace original
rescue
  e -> reraise e, []   # stack trace apunta aquí, no al origen

# BIEN: __STACKTRACE__ es una variable especial disponible en rescue
rescue
  e -> reraise e, __STACKTRACE__   # preserva el origen del error
```

### after cambia el valor de retorno (falsa creencia)

```elixir
# after NO afecta el valor de retorno del try completo
result = try do
  42
after
  IO.puts("cleanup")   # se ejecuta, pero el resultado del try sigue siendo 42
end
# result => 42 (not nil, not the return of after)
```

### defexception sin implementar exception/1 cuando se necesitan campos

```elixir
# Si solo defines los campos pero no exception/1, `raise` con opciones fallará
defmodule MyError do
  defexception [:message, :code]
  # Sin exception/1, solo funciona: raise MyError, message: "..."
  # Para campos adicionales y mensaje automático, implementa exception/1
end
```

### Rescatar Exception (captura demasiado)

```elixir
# MAL: captura absolutamente todo, incluyendo errores de programación
rescue
  e in Exception -> handle(e)

# BIEN: captura solo los tipos que sabes manejar
rescue
  e in [MyError, ArgumentError] -> handle(e)
```

---

## Try It Yourself

Implementa `safe_parse/1` que intenta parsear un string como JSON, luego como CSV (una línea de valores separados por comas), y finalmente como un entero simple. Maneja errores específicos de cada intento con mensajes descriptivos.

```elixir
defmodule SafeParser do
  @doc """
  Tries to parse input as JSON, CSV, or integer, in that order.
  Returns {:ok, {format, value}} for the first successful parse,
  or {:error, reasons} with a list of what was tried and failed.
  """
  def safe_parse(input) when is_binary(input) do
    attempts = [
      {:json, fn -> parse_json(input) end},
      {:csv,  fn -> parse_csv(input) end},
      {:integer, fn -> parse_integer(input) end}
    ]

    # TODO: Itera los intentos en orden. Para cada uno:
    # - Ejecuta la función en un try/rescue
    # - Si tiene éxito, retorna {:ok, {format, value}} inmediatamente
    # - Si falla, acumula {:error, format, reason} en una lista
    # Al final, si ninguno funcionó, retorna {:error, reasons_list}
    Enum.reduce_while(attempts, [], fn {format, parse_fn}, errors ->
      result = try do
        {:ok, parse_fn.()}
      rescue
        e -> {:error, Exception.message(e)}
      end

      case result do
        {:ok, value} ->
          {:halt, {:ok, {format, value}}}
        {:error, reason} ->
          {:cont, [{format, reason} | errors]}
      end
    end)
    |> case do
      {:ok, _} = success -> success
      errors when is_list(errors) -> {:error, Enum.reverse(errors)}
    end
  end

  # TODO: Implementa parse_json/1
  # Usa Jason.decode!/1 si está disponible, o simula lanzando para inputs no-JSON
  defp parse_json(input) do
    # Si el input empieza con { o [, simula JSON válido
    # Si no, raise RuntimeError "Invalid JSON"
    if String.starts_with?(input, ["{", "["]) do
      %{parsed: input, format: "json"}  # simulación
    else
      raise RuntimeError, "Invalid JSON: #{input}"
    end
  end

  # TODO: Implementa parse_csv/1
  # Una línea CSV válida tiene al menos una coma
  defp parse_csv(input) do
    # TODO: si contiene ",", retorna la lista de campos
    # Si no, raise ArgumentError "Not a CSV line"
  end

  # TODO: Implementa parse_integer/1
  defp parse_integer(input) do
    # TODO: usa Integer.parse/1, si falla lanza ArgumentError
  end
end

IO.inspect SafeParser.safe_parse("{\"key\": 1}")
# => {:ok, {:json, %{parsed: "{\"key\": 1}", format: "json"}}}

IO.inspect SafeParser.safe_parse("alice,30,admin")
# => {:ok, {:csv, ["alice", "30", "admin"]}}

IO.inspect SafeParser.safe_parse("42")
# => {:ok, {:integer, 42}}

IO.inspect SafeParser.safe_parse("not parseable !@#")
# => {:error, [{:json, "..."}, {:csv, "..."}, {:integer, "..."}]}
```
