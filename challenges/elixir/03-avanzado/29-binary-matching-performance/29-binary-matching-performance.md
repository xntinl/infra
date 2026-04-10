# 29. Performance de Pattern Matching en Binarios

**Difficulty**: Avanzado

---

## Prerequisites

### Mastered
- Binary syntax: `<<>>`, pattern matching en binaries, tipos (utf8, integer, binary-size)
- String: módulo completo, `String.split`, `String.slice`
- Regex: `~r//`, `Regex.run`, `Regex.scan`

### Familiarity with
- Benchee (ejercicio 28) para comparar implementaciones
- BEAM internals: heaps, ref-counted binaries (ejercicio 26)
- NIFs: concepto básico de extensiones nativas

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

- **Analizar** cómo el match context de BEAM elimina copias innecesarias en traversals de binarios
- **Diseñar** parsers de binarios que exploten la reutilización de match context
- **Evaluar** cuándo usar binary matching nativo vs Regex vs NIF para parsing de protocolos
- **Implementar** un parser de protocolo binario que procese streams sin acumular en memoria

---

## Concepts

### Binary Match Context: La Optimización Central

Cuando BEAM procesa un binary con pattern matching secuencial, puede reutilizar un "match context" — un cursor interno que avanza sin copiar el binary subyacente. Esta optimización es automática pero tiene condiciones específicas:

```elixir
# CASO 1: Match context reutilizado (óptimo)
# BEAM crea un cursor y lo avanza — sin copias del binary original
defmodule Parser do
  def parse(<<0x01, rest::binary>>), do: {:type_a, parse_body(rest)}
  def parse(<<0x02, rest::binary>>), do: {:type_b, parse_body(rest)}
  def parse(<<>>), do: :empty

  defp parse_body(<<len::8, data::binary-size(len), rest::binary>>) do
    {data, rest}
  end
end

# CASO 2: Match context destruido (ineficiente)
# Llamar a otra función ANTES de hacer el match rompe el contexto
defmodule BadParser do
  def parse(binary) do
    binary = normalize(binary)  # ← llama a función externa ANTES del match
    case binary do
      <<0x01, rest::binary>> -> {:type_a, rest}  # ← nuevo context, sin reutilizar
      _ -> :error
    end
  end
end
```

### Cuándo se Reutiliza el Match Context

```
Reutilización ocurre CUANDO:
├── La función es tail-recursive con el mismo binary
├── El match está en la cabeza de la función (clause head)
└── No hay transformaciones del binary entre calls recursivas

Reutilización NO ocurre CUANDO:
├── Se pasa el binary a otra función y se recibe de vuelta
├── Se construye un nuevo binary con el original como parte
├── El binary pasa por una variable intermedia con operación
└── Se usa :binary.part/3 o String.slice/2 para extraer
```

### Sub-Binaries vs Copias

```elixir
# Sub-binary: referencia al original, sin copia
original = :crypto.strong_rand_bytes(1_000_000)

# <<_::binary-size(100), slice::binary>> crea un SUB-BINARY
# slice apunta al offset 100 de original — O(1), sin copia
<<_::binary-size(100), slice::binary-size(50), _::binary>> = original

# :binary.copy/1 fuerza una copia real
real_copy = :binary.copy(slice)  # ahora es independiente

# ⚠️ Trampa: mantener un sub-binary mantiene VIVO el binary original completo
# Si original es 1MB y guardas un slice de 4 bytes, tienes 1MB en memoria
store_somewhere(slice)  # el 1MB no puede ser GC'd
```

### Regex vs Binary Matching: Trade-offs

| Aspecto | Regex | Binary Matching |
|---------|-------|----------------|
| **Legibilidad** | Alta para patterns simples | Baja para casos complejos |
| **Compilación** | `~r//` compila en tiempo de carga | Compilado en beamcode |
| **Performance (simple)** | Buena (PCRE nativo) | Muy buena |
| **Performance (estructural)** | Pobre — no entiende estructura | Excelente — tipo-aware |
| **Streaming** | No soportado | Nativo con recursión |
| **Binarios no-UTF8** | No aplica | Primera clase |
| **Mantenimiento** | Fácil para texto | Requiere conocer el protocolo |

```elixir
# Regex: bueno para texto con patterns irregulares
~r/(\w+)\s*=\s*(\w+)/

# Binary matching: mejor para protocolos con campos de longitud fija
<<type::8, version::8, length::16-big, body::binary-size(length)>> = packet
```

### NIF Comparison: ¿Cuándo Vale la Pena?

```
Binary matching nativo de BEAM:
└── ~100-500 ns por operación simple
    Limitación: todo corre en el scheduler normal → preemptible

NIF (C/Rust extension):
└── ~10-50 ns para operaciones equivalentes (código nativo)
    Limitación: bloquea el scheduler → usar dirty NIFs para >1ms
    Ejemplos reales: Jason (JSON), :ssl, protobuf parsers

Regla práctica:
├── < 1μs target: considera NIF si binary matching no alcanza
├── 1-10μs target: binary matching optimizado es suficiente
└── > 10μs target: Regex o binary matching — performance no es el problema
```

### Patterns de Binary Streaming

```elixir
# Pattern 1: acumular hasta tener suficientes bytes para parsear un frame
defmodule FrameParser do
  def parse(buffer, acc \\ [])

  # Tenemos suficientes bytes para el header (4 bytes)
  def parse(<<length::32-big, body::binary-size(length), rest::binary>>, acc) do
    parse(rest, [body | acc])  # ← match context reutilizado aquí
  end

  # Buffer incompleto — esperar más datos
  def parse(incomplete, acc) do
    {Enum.reverse(acc), incomplete}
  end
end

# Pattern 2: procesar sin acumular (streaming real)
defmodule StreamProcessor do
  def process_stream(binary, handler_fn) do
    do_process(binary, handler_fn)
  end

  defp do_process(<<frame_type::8, len::16, data::binary-size(len), rest::binary>>, handler_fn) do
    handler_fn.({frame_type, data})
    do_process(rest, handler_fn)  # ← tail call, match context reutilizado
  end

  defp do_process(<<>>, _handler_fn), do: :done
  defp do_process(incomplete, _handler_fn), do: {:need_more, incomplete}
end
```

---

## Exercises

### Exercise 1: HTTP Request Line Parser — Binary Matching vs Regex

**Problem**

Implementa un parser para la request line de HTTP/1.1: `GET /path/to/resource?query=1 HTTP/1.1`

El parser debe extraer: `{method, path, query_string, version}`.

Implementa **tres versiones**:

1. **Regex version**: usando `~r/^(\w+)\s+([^?]+)(?:\?([^\s]*))?\s+HTTP\/(\d+\.\d+)$/`
2. **String.split version**: dividir por espacios y luego procesar partes
3. **Binary matching version**: usando `<<>>` pattern matching directamente

Luego benchmarkea con Benchee las tres versiones con:
- 1000 requests lines distintas (variedad de métodos, paths, query strings)
- Mide tiempo y memoria

```elixir
# Input de ejemplo:
"GET /users/123/profile?include=avatar&format=json HTTP/1.1"

# Output esperado:
{:ok, %{method: "GET", path: "/users/123/profile", query: "include=avatar&format=json", version: "1.1"}}
```

**Hints**

- Para la versión de binary matching: usa `<<method_byte::8, _::binary>>` para detectar el método por su primer byte (G=GET, P=POST/PUT/PATCH, D=DELETE, H=HEAD)
- El espacio en ASCII es `0x20` — úsalo como delimitador: `<<byte, rest::binary>> when byte == 0x20`
- Para extraer hasta el próximo `?` o espacio: función recursiva que acumula bytes hasta encontrar el delimitador
- Compila el Regex fuera del benchmark con `@regex ~r/.../` para no incluir compilación en la medición
- `before_scenario` en Benchee para generar las 1000 request lines variadas

**One possible solution**

```elixir
defmodule HttpParser do
  # Version 1: Regex
  @request_regex ~r/^(\w+)\s+([^?\s]+)(?:\?([^\s]*))?\s+HTTP\/(\d+\.\d+)$/

  def parse_regex(line) do
    case Regex.run(@request_regex, line, capture: :all_but_first) do
      [method, path, query, version] ->
        {:ok, %{method: method, path: path, query: query, version: version}}
      [method, path, version] ->
        {:ok, %{method: method, path: path, query: "", version: version}}
      nil ->
        {:error, :invalid_request_line}
    end
  end

  # Version 2: String.split
  def parse_split(line) do
    case String.split(line, " ", parts: 3) do
      [method, path_query, <<"HTTP/", version::binary>>] ->
        {path, query} = split_path_query(path_query)
        {:ok, %{method: method, path: path, query: query, version: String.trim(version)}}
      _ ->
        {:error, :invalid_request_line}
    end
  end

  defp split_path_query(path_query) do
    case String.split(path_query, "?", parts: 2) do
      [path, query] -> {path, query}
      [path] -> {path, ""}
    end
  end

  # Version 3: Binary matching
  def parse_binary(line) do
    # TODO: implementar usando pattern matching en <<>>
    # Pista: extraer method hasta el primer espacio (0x20)
    # luego path hasta ? o espacio, luego query hasta espacio
    parse_method(line, [])
  end

  defp parse_method(<<" ", rest::binary>>, acc) do
    method = acc |> Enum.reverse() |> :erlang.list_to_binary()
    parse_path(rest, [], method)
  end

  defp parse_method(<<byte, rest::binary>>, acc) do
    parse_method(rest, [byte | acc])
  end

  defp parse_method(<<>>, _acc), do: {:error, :invalid_request_line}

  defp parse_path(<<"?", rest::binary>>, acc, method) do
    path = acc |> Enum.reverse() |> :erlang.list_to_binary()
    parse_query(rest, [], method, path)
  end

  defp parse_path(<<" ", rest::binary>>, acc, method) do
    path = acc |> Enum.reverse() |> :erlang.list_to_binary()
    parse_version(rest, method, path, "")
  end

  defp parse_path(<<byte, rest::binary>>, acc, method) do
    parse_path(rest, [byte | acc], method)
  end

  defp parse_path(<<>>, _acc, _method), do: {:error, :invalid_request_line}

  defp parse_query(<<" ", rest::binary>>, acc, method, path) do
    query = acc |> Enum.reverse() |> :erlang.list_to_binary()
    parse_version(rest, method, path, query)
  end

  defp parse_query(<<byte, rest::binary>>, acc, method, path) do
    parse_query(rest, [byte | acc], method, path)
  end

  defp parse_query(<<>>, _acc, _method, _path), do: {:error, :invalid_request_line}

  defp parse_version(<<"HTTP/", version::binary>>, method, path, query) do
    {:ok, %{method: method, path: path, query: query, version: String.trim(version)}}
  end

  defp parse_version(_, _, _, _), do: {:error, :invalid_request_line}
end
```

---

### Exercise 2: Binary Streaming — Procesar sin Acumular en Memoria

**Problem**

Dado un stream de "frames" en formato binario, implementa un parser que los procese de forma incremental sin acumular todos los frames en memoria.

El formato del frame:
```
| type (1 byte) | flags (1 byte) | length (4 bytes, big-endian) | payload (length bytes) |
```

Implementa:

1. `FrameParser.parse_stream/2` — recibe un binary completo, llama a `callback_fn` por cada frame, retorna `{frames_count, bytes_processed}`
2. `FrameParser.parse_incremental/3` — versión para datos que llegan en chunks: `parse_incremental(chunk, buffer, callback_fn)` retorna el buffer restante
3. Un generador `FrameGenerator.generate/2` que crea binarios de prueba
4. Benchmark de `parse_stream` vs `Enum.map` + `Enum.reduce` sobre los mismos datos

El indicador de éxito es que puedas procesar un binary de 100MB sin que la memoria del proceso supere los 10MB.

**Hints**

- El match context se reutiliza cuando la función recursiva es tail-call con el mismo binary
- Para `parse_incremental`: si el buffer no tiene suficientes bytes para el header, retornar `{:need_more_data, buffer}`; si tiene el header pero no el payload completo, retornar `{:need_more_data, buffer}`
- Usa `:erlang.process_info(self(), :total_heap_size)` antes y después para verificar que no hay acumulación
- `FrameGenerator.generate/1` debe crear binarios con frames de tamaño variable para hacer el test más real

**One possible solution**

```elixir
defmodule FrameParser do
  # Header: type(1) + flags(1) + length(4) = 6 bytes
  @header_size 6

  def parse_stream(binary, callback_fn) when is_binary(binary) do
    do_parse(binary, 0, 0, callback_fn)
  end

  # Frame completo disponible
  defp do_parse(
    <<type::8, flags::8, length::32-big, payload::binary-size(length), rest::binary>>,
    frame_count,
    bytes,
    callback_fn
  ) do
    # El match context se reutiliza en la llamada recursiva — no hay copia de `rest`
    callback_fn.(%{type: type, flags: flags, payload: payload})
    consumed = @header_size + length
    do_parse(rest, frame_count + 1, bytes + consumed, callback_fn)
  end

  # Binary terminado
  defp do_parse(<<>>, frame_count, bytes, _callback_fn) do
    {:ok, frame_count, bytes}
  end

  # Datos insuficientes para el frame actual
  defp do_parse(incomplete, frame_count, bytes, _callback_fn) do
    {:partial, frame_count, bytes, incomplete}
  end

  def parse_incremental(chunk, buffer, callback_fn) do
    # Combinar buffer anterior con nuevo chunk
    combined = buffer <> chunk
    case do_parse(combined, 0, 0, callback_fn) do
      {:ok, _, _} -> {:done, <<>>}
      {:partial, _, _, remaining} -> {:need_more_data, remaining}
    end
  end
end

defmodule FrameGenerator do
  def generate(n_frames, payload_size_range \\ 10..1000) do
    Enum.reduce(1..n_frames, <<>>, fn i, acc ->
      size = :rand.uniform(elem(payload_size_range, 1) - elem(payload_size_range, 0)) +
             elem(payload_size_range, 0)
      payload = :crypto.strong_rand_bytes(size)
      frame = <<rem(i, 256)::8, 0::8, size::32-big, payload::binary>>
      <<acc::binary, frame::binary>>
    end)
  end
end
```

---

### Exercise 3: Match Context Optimization — Demostrar la Diferencia

**Problem**

Implementa dos versiones de un traversal de binary y demuestra empíricamente (con Benchee) que una versión preserva el match context y la otra no.

**Versión A (preserva el match context)**:
```elixir
# Recursión directa sobre el binary — BEAM reutiliza el cursor
def count_bytes(<<byte, rest::binary>>, acc) when byte > 127, do: count_bytes(rest, acc + 1)
def count_bytes(<<_byte, rest::binary>>, acc), do: count_bytes(rest, acc)
def count_bytes(<<>>, acc), do: acc
```

**Versión B (rompe el match context)**:
```elixir
# Indirección a través de función auxiliar — BEAM NO reutiliza el cursor
def count_bytes_indirect(binary, acc) do
  case extract_first(binary) do  # ← llamada a función externa rompe el contexto
    {byte, rest} when byte > 127 -> count_bytes_indirect(rest, acc + 1)
    {_byte, rest} -> count_bytes_indirect(rest, acc)
    :empty -> acc
  end
end

defp extract_first(<<byte, rest::binary>>), do: {byte, rest}
defp extract_first(<<>>), do: :empty
```

**Versión C (usando Enum/String — para comparación)**:
```elixir
def count_bytes_enum(binary) do
  binary
  |> :binary.bin_to_list()
  |> Enum.count(&(&1 > 127))
end
```

Benchmarkea las tres versiones con binarios de 10KB, 100KB, y 1MB. El objetivo es ver la diferencia entre A y B en escala.

**Hints**

- La diferencia entre A y B es más visible con binarios grandes (1MB+)
- Para generar bytes aleatorios con distribución conocida: `:crypto.strong_rand_bytes/1`
- Usa `memory_time: 2` en Benchee — B generará más garbage que A
- Si la diferencia es pequeña, es porque el JIT compiler de OTP 25+ puede optimizar B en algunos casos — menciona esta posibilidad en tus observaciones
- Añade una versión D usando `:binary.foldl/3` (función Erlang nativa) para otra referencia

**One possible solution**

```elixir
defmodule MatchContextDemo do
  # Versión A: preserva match context — patrón óptimo
  def count_a(<<byte, rest::binary>>, acc) when byte > 127,
    do: count_a(rest, acc + 1)

  def count_a(<<_byte, rest::binary>>, acc),
    do: count_a(rest, acc)

  def count_a(<<>>, acc), do: acc

  # Versión B: rompe match context por indirección
  def count_b(binary, acc \\ 0) do
    case extract_first(binary) do
      {byte, rest} when byte > 127 -> count_b(rest, acc + 1)
      {_byte, rest} -> count_b(rest, acc)
      :empty -> acc
    end
  end

  defp extract_first(<<byte, rest::binary>>), do: {byte, rest}
  defp extract_first(<<>>), do: :empty

  # Versión C: Enum (para referencia)
  def count_c(binary) do
    binary
    |> :binary.bin_to_list()
    |> Enum.count(&(&1 > 127))
  end

  # Versión D: :binary.foldl nativo de Erlang
  def count_d(binary) do
    :binary.foldl(fn byte, acc -> if byte > 127, do: acc + 1, else: acc end, 0, binary)
  end

  def run_benchmark do
    Benchee.run(
      %{
        "A: match context preservado" => fn binary -> count_a(binary, 0) end,
        "B: match context roto" => fn binary -> count_b(binary) end,
        "C: Enum/list" => fn binary -> count_c(binary) end,
        "D: :binary.foldl" => fn binary -> count_d(binary) end
      },
      inputs: %{
        "10 KB"  => :crypto.strong_rand_bytes(10 * 1024),
        "100 KB" => :crypto.strong_rand_bytes(100 * 1024),
        "1 MB"   => :crypto.strong_rand_bytes(1024 * 1024)
      },
      warmup: 2,
      time: 5,
      memory_time: 2
    )
  end
end
```

---

## Common Mistakes

### 1. Construir el binary de salida en la misma función que lo parsea

```elixir
# MAL: construcción y traversal en la misma función → rompe el match context
def transform(<<byte, rest::binary>>, acc) do
  new_byte = byte + 1
  transform(rest, <<acc::binary, new_byte::8>>)  # ← construir binary aquí rompe el contexto
end

# BIEN: separar parsing y construcción
def transform(binary) do
  bytes = collect_bytes(binary, [])  # parsear primero
  Enum.reduce(bytes, <<>>, fn b, acc -> <<acc::binary, b + 1::8>> end)  # construir después
end
```

### 2. Usar `String.split` para binary estructurado no-UTF8

`String.split` opera sobre codepoints UTF-8 y tiene overhead de validación de encoding. Para protocolos binarios que no son texto, usa binary matching directamente.

### 3. Mantener sub-binaries referenciando binarios grandes

```elixir
# MAL: slice mantiene vivo el binary de 1MB completo
def extract_id(<<_::binary-size(1000), id::binary-size(16), _::binary>>), do: id

# BIEN: copiar para liberar la referencia al binary grande
def extract_id(<<_::binary-size(1000), id::binary-size(16), _::binary>>),
  do: :binary.copy(id)
```

### 4. Parsear el mismo binary múltiples veces

```elixir
# MAL: dos traversals completos del mismo binary
header = extract_header(binary)   # primer traversal
payload = extract_payload(binary)  # segundo traversal

# BIEN: un solo traversal que extrae todo
{header, payload} = parse_frame(binary)  # una sola pasada
```

### 5. Usar Regex para binarios con longitudes variables determinadas por campos previos

Regex no puede expresar "match N bytes donde N viene del byte anterior". Para protocolos con campos de longitud variable (length-prefixed), binary matching es la única opción expresiva:

```elixir
# Regex NO puede hacer esto:
<<type::8, length::16-big, data::binary-size(length), _rest::binary>> = binary
# Solo binary matching puede.
```

### 6. No considerar endianness en protocolos de red

Por defecto, `::integer` en BEAM es big-endian. Los protocolos de red (TCP/IP stack) son big-endian. Los formatos de archivo y los protocolos de bajo nivel (x86 registers) son little-endian.

```elixir
# Big-endian (network byte order — defecto):
<<value::32-big>> = <<0x00, 0x00, 0x04, 0xD2>>  # value = 1234

# Little-endian (common in file formats, x86):
<<value::32-little>> = <<0xD2, 0x04, 0x00, 0x00>>  # value = 1234
```

---

## Summary

El pattern matching en binarios de BEAM es una herramienta de primera clase para parsing de protocolos, no solo syntax sugar sobre operaciones de string. Las optimizaciones clave son:

- **Match context reutilizado**: traversals con recursión de cola sobre el mismo binary son O(1) en allocations
- **Sub-binaries**: slices sin copia, pero con trampa — mantienen vivo el binary original
- **Binary matching vs Regex**: matching para estructura fija/length-prefixed, Regex para texto con patterns irregulares
- **Streaming**: el patrón buffer+parse incremental permite procesar GB de datos con memoria constante

El binary matching nativo de BEAM alcanza 100-500ns por operación — un NIF solo vale la pena para operaciones que requieren <10ns o para integrarse con librerías C ya existentes.

---

## What's Next

- Habiendo completado los ejercicios 25-29, tienes una base sólida de BEAM internals
- Próximo territorio: NIFs con Rustler para operaciones que binary matching no alcanza
- Investiga `exprotobuf` o `protox` para ver cómo se implementan parsers de protocolos de producción
- Explora `:prim_inet` y el módulo `:gen_tcp` para ver binary matching en acción en sockets

---

## Resources

- [BEAM Book — Binary matching chapter](https://happi.github.io/theBeamBook/#binary-handling)
- [Erlang efficiency guide — Binaries](https://www.erlang.org/doc/efficiency_guide/binaryhandling.html)
- [:binary module — Erlang docs](https://www.erlang.org/doc/man/binary.html)
- [Björn Gustavsson — "How binary matching is optimized in BEAM"](http://erlang.org/workshop/2003/paper/p36-gustavsson.pdf)
- [Rustler — Safe Rust NIFs for Elixir](https://github.com/rusterlium/rustler)
