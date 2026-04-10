# 24. Bitstrings y Binarios Avanzados

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–23
- Understanding of Elixir strings as binaries
- Familiarity with pattern matching
- Basic knowledge of binary numbering (optional but helpful)

## Learning Objectives
After completing this exercise, you will be able to:
- Construir binarios con la sintaxis `<<>>` y sus especificadores de tipo y tamaño
- Hacer pattern matching sobre binarios para extraer bytes y segmentos
- Entender la diferencia entre `integer`, `binary`, `utf8`, y otros specifiers
- Procesar strings a nivel de bytes y codepoints UTF-8 correctamente
- Construir headers de protocolos binarios simples
- Distinguir cuándo usar `String.graphemes/1` vs acceso byte a byte

## Concepts

### Binarios y bitstrings: la base de los strings en Elixir

En Elixir, los strings son binarios — secuencias de bytes codificados en UTF-8. La sintaxis `<<>>` permite construir y hacer pattern matching sobre binarios y bitstrings (secuencias de bits que no necesariamente son múltiplos de 8).

```elixir
# Un binario es una secuencia de bytes
<<72, 101, 108, 108, 111>>   # => "Hello" (bytes ASCII)

# Los strings son azúcar sintáctica sobre binarios
"Hello" == <<72, 101, 108, 108, 111>>   # => true

# is_binary verifica que sea un binario (múltiplo de 8 bits)
is_binary("Elixir")   # => true
is_binary(<<1, 2, 3>>)   # => true

# bit_size y byte_size
bit_size("Hello")    # => 40  (5 bytes × 8 bits)
byte_size("Hello")   # => 5
```

### Especificadores de tipo

Los especificadores controlan cómo se interpretan los datos dentro de `<<>>`:

| Specifier | Significado | Ejemplo |
|-----------|-------------|---------|
| `integer` | entero (default) | `<<255::integer>>` |
| `float` | punto flotante | `<<3.14::float-32>>` |
| `binary` | binario/string | `<<"hello"::binary>>` |
| `utf8` | codepoint UTF-8 | `<<0x00E9::utf8>>` → `"é"` |
| `size(n)` | tamaño en bits | `<<1::size(16)>>` |
| `unit(n)` | unidad multiplicadora | `<<data::binary-unit(8)>>` |
| `big`/`little` | byte order (endianness) | `<<1000::big-16>>` |

```elixir
# Construir con especificadores explícitos
<<1::16>>             # => <<0, 1>> (big-endian 16 bits)
<<1::little-16>>      # => <<1, 0>> (little-endian 16 bits)
<<3.14::float-32>>    # => binario de 4 bytes para float

# Combinar múltiples campos
<<version::8, flags::4, length::20>> = <<1, 0b0001::4, 1024::20>>
```

### Pattern matching sobre binarios

El pattern matching sobre binarios funciona igual que sobre cualquier otro término, pero con la sintaxis de especificadores:

```elixir
# Extraer el primer byte de un binario
<<first::8, rest::binary>> = "Hello"
first   # => 72 (código ASCII de 'H')
rest    # => "ello"

# Extraer dos bytes específicos
<<r::8, g::8, b::8>> = <<255, 128, 0>>
{r, g, b}   # => {255, 128, 0}

# Extraer un segmento de tamaño conocido
<<header::binary-size(4), body::binary>> = "MAGICDATA"
header   # => "MAGI"
body     # => "CDATA"
```

### UTF-8 y codepoints

Los strings en Elixir son UTF-8. Un codepoint puede ocupar 1 a 4 bytes. El specifier `utf8` permite trabajar correctamente con caracteres multibyte:

```elixir
# "é" es el codepoint U+00E9, que en UTF-8 ocupa 2 bytes: <<195, 169>>
byte_size("é")      # => 2
String.length("é")  # => 1 (un codepoint)

# Pattern matching con utf8 extrae el codepoint entero
<<head::utf8, tail::binary>> = "café"
head   # => 99 (código de 'c', que es ASCII y también UTF-8 de 1 byte)
tail   # => "afé"

# Para el carácter "é":
<<head::utf8, tail::binary>> = "élan"
head   # => 233 (U+00E9 = 0xE9 = 233 decimal)
tail   # => "lan"

# String.graphemes vs bytes
String.graphemes("café")   # => ["c", "a", "f", "é"]
:binary.bin_to_list("café") # => [99, 97, 102, 195, 169] (5 bytes)
```

### Construcción de protocolos binarios

Los binarios y sus especificadores permiten construir e interpretar protocolos de comunicación binarios de forma declarativa:

```elixir
# Construir un frame de protocolo simple:
# | magic (4 bytes) | version (1 byte) | length (2 bytes) | payload |
defmodule Protocol do
  def encode(payload) when is_binary(payload) do
    magic = "MYPROT"
    version = 1
    length = byte_size(payload)
    <<magic::binary, version::8, length::16, payload::binary>>
  end

  def decode(<<magic::binary-size(6), version::8, length::16, payload::binary-size(length)>>) do
    {magic, version, length, payload}
  end
end
```

---

## Exercises

### Exercise 1: Binary basics

```elixir
defmodule BinaryBasics do
  def run do
    # TODO 1: Crea un binario con los bytes [1, 2, 3, 255] usando la sintaxis <<>>
    # Almacena en `bytes`
    bytes = # TODO
    IO.inspect(bytes)    # => <<1, 2, 3, 255>>

    # TODO 2: Verifica que bytes es un binario con is_binary/1
    # Almacena el resultado en `is_bin`
    is_bin = # TODO
    IO.inspect(is_bin)   # => true

    # TODO 3: Obtén el tamaño en bytes de "Elixir Programming" con byte_size/1
    size = # TODO
    IO.inspect(size)     # => 18

    # TODO 4: Verifica que el string "hello" es igual al binario
    # construido con sus bytes ASCII: h=104, e=101, l=108, l=108, o=111
    are_equal = "hello" == # TODO (construye el binario con <<>>)
    IO.inspect(are_equal)   # => true

    # TODO 5: Construye el binario que representa el número 1000
    # como un entero de 16 bits big-endian
    # PISTA: <<1000::16>> o <<1000::big-16>>
    uint16 = # TODO
    IO.inspect(uint16)   # => <<3, 232>> (3*256 + 232 = 1000)
    IO.inspect(byte_size(uint16))   # => 2
  end
end

BinaryBasics.run()
```

---

### Exercise 2: Pattern matching sobre binarios

```elixir
defmodule BinaryPatternMatch do
  def run do
    message = "Hello, World!"

    # TODO 1: Haz pattern match para extraer el primer byte y el resto
    # PISTA: <<first::8, rest::binary>> = message
    # Almacena en `first_byte` y `rest_bytes`
    # TODO

    IO.inspect(first_byte)    # => 72 (ASCII de 'H')
    IO.inspect(rest_bytes)    # => "ello, World!"

    # TODO 2: Extrae los primeros 5 bytes como un segmento binario y el resto
    # PISTA: usa binary-size(5) para el primer segmento
    # Almacena en `header` y `body`
    # TODO

    IO.inspect(header)   # => "Hello"
    IO.inspect(body)     # => ", World!"

    # TODO 3: Dado este binario de color RGB, extrae los 3 componentes
    color = <<255, 128, 64>>
    # PISTA: <<r::8, g::8, b::8>> = color
    # TODO

    IO.inspect({r, g, b})   # => {255, 128, 64}

    # TODO 4: Implementa first_byte/1 que retorna el valor del primer byte de un binario
    IO.inspect(first_byte_of("ABC"))   # => 65

    # TODO 5: Implementa split_at/2 que divide un binario en posición n
    # PISTA: usa binary-size(n) en el pattern
    IO.inspect(split_at("HelloWorld", 5))   # => {"Hello", "World"}
  end

  # TODO: Implementa estas funciones
  def first_byte_of(binary) do
    # TODO (pattern match con <<byte::8, _::binary>>)
  end

  def split_at(binary, n) do
    # TODO
  end
end

BinaryPatternMatch.run()
```

---

### Exercise 3: Especificadores de tipo y endianness

```elixir
defmodule BinarySpecifiers do
  def run do
    # TODO 1: Interpreta <<1, 0>> como un entero big-endian de 16 bits
    # ¿Cuánto vale? 1*256 + 0 = 256
    # PISTA: <<value::integer-big-16>> = <<1, 0>>
    # TODO
    IO.inspect(value_big)      # => 256

    # TODO 2: Interpreta <<1, 0>> como un entero little-endian de 16 bits
    # ¿Cuánto vale? 1 + 0*256 = 1
    # PISTA: <<value::integer-little-16>> = <<1, 0>>
    # TODO
    IO.inspect(value_little)   # => 1

    # TODO 3: Construye el número 300 como big-endian 16 bits
    # 300 = 1*256 + 44, así que debe ser <<1, 44>>
    encoded = # TODO
    IO.inspect(encoded)   # => <<1, 44>>

    # TODO 4: Interpreta 4 bytes como un float de 32 bits
    # <<63, 192, 0, 0>> es 1.5 en IEEE 754 single precision
    <<float_val::float-32>> = <<63, 192, 0, 0>>
    IO.inspect(float_val)   # => 1.5

    # TODO 5: Construye un binario que contenga:
    # - El byte 0xAA (170 en decimal)
    # - El número 1024 como big-endian de 16 bits
    # - El byte 0xFF (255 en decimal)
    # El resultado debe ser un binario de 4 bytes
    frame = # TODO: <<0xAA::8, 1024::big-16, 0xFF::8>>
    IO.inspect(frame)        # => <<170, 4, 0, 255>>
    IO.inspect(byte_size(frame))   # => 4
  end
end

BinarySpecifiers.run()
```

---

### Exercise 4: String matching con UTF-8

```elixir
defmodule Utf8Matching do
  def run do
    # TODO 1: Extrae el primer codepoint UTF-8 de "café"
    # PISTA: <<head::utf8, tail::binary>> = "café"
    # TODO
    IO.inspect(head_codepoint)   # => 99 (código de 'c')
    IO.inspect(tail_string)      # => "afé"

    # TODO 2: Extrae el primer codepoint de "élite" (empieza con carácter multibyte)
    # "é" = U+00E9 = 233 decimal
    # TODO
    IO.inspect(first_cp)   # => 233
    IO.inspect(rest_str)   # => "lite"

    # TODO 3: Implementa count_codepoints/1 que cuenta codepoints (no bytes)
    # Usa recursión con pattern matching utf8
    # PISTA: coincide con <<_::utf8, rest::binary>> y cuenta
    IO.inspect(count_codepoints("café"))   # => 4 (no 5 bytes)
    IO.inspect(count_codepoints("hello"))  # => 5

    # TODO 4: ¿Cuántos bytes ocupa "naïve"? ¿Y cuántos codepoints?
    word = "naïve"
    IO.inspect(byte_size(word))      # => 6 (ï ocupa 2 bytes)
    IO.inspect(count_codepoints(word))  # => 5

    # TODO 5: Demuestra la diferencia entre String.graphemes y byte_size
    emoji = "hello 🌍"
    IO.inspect(String.length(emoji))      # => 7 (graphemes)
    IO.inspect(byte_size(emoji))          # => 10 (emoji = 4 bytes)
    IO.inspect(String.graphemes(emoji))   # => ["h","e","l","l","o"," ","🌍"]
  end

  # TODO: Implementa usando recursión + pattern matching
  def count_codepoints(<<>>), do: 0
  def count_codepoints(binary) when is_binary(binary) do
    # TODO: pattern match en el primer codepoint utf8, recursa en el resto
    # PISTA: <<_::utf8, rest::binary>> = binary
  end
end

Utf8Matching.run()
```

---

### Exercise 5: Construir un header de protocolo binario

```elixir
defmodule SimpleProtocol do
  @moduledoc """
  Protocolo binario simple:

  Frame format:
  | magic (2 bytes) | type (1 byte) | seq (2 bytes) | length (2 bytes) | payload |

  Types:
  - 0x01 = PING
  - 0x02 = PONG
  - 0x03 = DATA
  - 0xFF = ERROR
  """

  @magic <<0xDE, 0xAD>>

  # TODO 1: Implementa encode/3 que construye un frame binario completo
  # - magic: @magic (2 bytes fijos)
  # - type: 1 byte (el átomo se convierte a byte: :ping => 0x01, etc.)
  # - seq: número de secuencia, 2 bytes big-endian
  # - length: tamaño del payload, 2 bytes big-endian (calcula con byte_size)
  # - payload: el binario del payload
  def encode(type, seq, payload) when is_binary(payload) do
    type_byte = type_to_byte(type)
    length = byte_size(payload)
    # TODO: construye el frame con <<>>
    # @magic <> <<type_byte, seq::16, length::16>> <> payload
    # O también: <<@magic::binary, type_byte::8, seq::big-16, length::big-16, payload::binary>>
  end

  # TODO 2: Implementa decode/1 que hace pattern matching sobre el binario
  # y retorna {:ok, %{type: type, seq: seq, payload: payload}}
  # o {:error, :invalid_frame} si no coincide el magic
  def decode(frame) do
    case frame do
      <<0xDE, 0xAD, type_byte::8, seq::big-16, length::big-16, payload::binary-size(length)>> ->
        # TODO: convierte type_byte a átomo con byte_to_type/1
        # retorna {:ok, %{type: ..., seq: seq, payload: payload}}
        # TODO
      _ ->
        {:error, :invalid_frame}
    end
  end

  # TODO 3: Implementa type_to_byte/1 convirtiendo átomos a bytes
  defp type_to_byte(:ping),  do: # TODO 0x01
  defp type_to_byte(:pong),  do: # TODO 0x02
  defp type_to_byte(:data),  do: # TODO 0x03
  defp type_to_byte(:error), do: # TODO 0xFF

  # TODO 4: Implementa byte_to_type/1 convirtiendo bytes a átomos
  defp byte_to_type(0x01), do: # TODO :ping
  defp byte_to_type(0x02), do: # TODO :pong
  defp byte_to_type(0x03), do: # TODO :data
  defp byte_to_type(0xFF), do: # TODO :error
  defp byte_to_type(_),    do: :unknown
end

# Tests
frame = SimpleProtocol.encode(:data, 42, "Hello, Server!")
IO.inspect(frame)
# => <<222, 173, 3, 0, 42, 0, 14, 72, 101, 108, 108, 111, ...>>

{:ok, decoded} = SimpleProtocol.decode(frame)
IO.inspect(decoded)
# => %{type: :data, seq: 42, payload: "Hello, Server!"}

# Frame inválido
IO.inspect(SimpleProtocol.decode(<<0, 0, 1>>))
# => {:error, :invalid_frame}
```

---

## Common Mistakes

### Confundir byte_size con String.length

```elixir
# byte_size cuenta bytes — NO caracteres
byte_size("café")      # => 5 (c=1, a=1, f=1, é=2)
String.length("café")  # => 4 (caracteres/graphemes)

# Para contar caracteres, siempre usa String.length
```

### Pattern match sin especificador usa integer por defecto

```elixir
# Por defecto, un segmento sin especificador es un integer de 8 bits
<<x>> = <<65>>       # x => 65 (no "A")
<<x::binary-size(1)>> = <<65>>   # x => "A" (binario de 1 byte)
```

### Asumir que UTF-8 siempre es 1 byte por carácter

```elixir
# MAL: esto asume 1 byte = 1 carácter
<<first::8, rest::binary>> = "naïve"
# first => 110 ('n', correcto)
# Pero para 'ï': <<195, 175>> (2 bytes) — el siguiente match rompería

# BIEN: usa ::utf8 para respetar la codificación
<<first::utf8, rest::binary>> = "naïve"
# first => 110, rest => "aïve" (correcto)
```

### Construir binarios con concatenación cuando <<>> es más claro

```elixir
# Funciona, pero verbose:
header = <<magic::binary>> <> <<version::8>> <> <<length::16>>

# Más idiomático:
header = <<magic::binary, version::8, length::big-16>>
```

### Olvidar binary-size en pattern matching de payload variable

```elixir
# MAL: el compilador no sabe cuántos bytes tomar para 'payload'
<<_magic::32, length::16, payload::binary>> = frame
# payload puede incluir bytes de un siguiente frame

# BIEN: usa binary-size con el campo length para acotar exactamente
<<_magic::32, length::16, payload::binary-size(length)>> = frame
```

---

## Try It Yourself

Implementa un parser para un formato binario simple de mensajes. Cada mensaje tiene el siguiente formato:

```
| magic (4 bytes: "MSG\0") | version (1 byte) | length (2 bytes) | payload (length bytes) |
```

```elixir
defmodule MessageParser do
  @magic "MSG\0"   # 4 bytes incluyendo null terminator

  @doc """
  Parses a binary message frame.

  ## Examples

      iex> frame = MessageParser.encode(1, "Hello")
      iex> MessageParser.parse(frame)
      {:ok, %{version: 1, payload: "Hello"}}

      iex> MessageParser.parse(<<0, 1, 2>>)
      {:error, :invalid_magic}
  """
  def encode(version, payload) when is_binary(payload) do
    # TODO: construye <<@magic::binary, version::8, byte_size(payload)::big-16, payload::binary>>
  end

  def parse(<<magic::binary-size(4), version::8, length::big-16, payload::binary-size(length)>>) do
    case magic do
      @magic ->
        # TODO: retorna {:ok, %{version: version, payload: payload}}
      _ ->
        {:error, :invalid_magic}
    end
  end

  def parse(_), do: {:error, :invalid_frame}

  @doc """
  Parses multiple concatenated frames from a binary stream.
  Returns a list of decoded messages.
  """
  def parse_stream(binary, acc \\ [])
  def parse_stream(<<>>, acc), do: {:ok, Enum.reverse(acc)}
  def parse_stream(<<"MSG\0", version::8, length::big-16, payload::binary-size(length), rest::binary>>, acc) do
    # TODO: parsea un frame, agrega al acc, recursa con rest
  end
  def parse_stream(_, _acc), do: {:error, :corrupt_stream}
end

# Tests
frame = MessageParser.encode(2, "ping")
IO.inspect(MessageParser.parse(frame))
# => {:ok, %{version: 2, payload: "ping"}}

stream = MessageParser.encode(1, "foo") <> MessageParser.encode(1, "bar")
IO.inspect(MessageParser.parse_stream(stream))
# => {:ok, [%{version: 1, payload: "foo"}, %{version: 1, payload: "bar"}]}
```
