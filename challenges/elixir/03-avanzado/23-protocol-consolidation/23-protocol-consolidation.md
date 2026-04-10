# 23 — Protocol Consolidation & Dispatch Performance

**Nivel**: Avanzado  
**Tema**: Consolidación de protocolos, dispatch O(1) vs O(n), `@derive`, introspección

---

## Contexto

Los protocolos de Elixir son polimorfismo ad-hoc. Cada tipo que implementa un
protocolo registra su implementación. El mecanismo de **dispatch** (encontrar
la implementación correcta para un valor en runtime) tiene dos modos:

### Sin consolidación — Dynamic dispatch O(n)

En desarrollo (`Mix.env() == :dev`), el dispatch busca la implementación en
tiempo de ejecución consultando una tabla de atoms:

```
Protocol.dispatch(value)
  → typeof(value) = Struct/Integer/BitString/...
  → buscar en lista de módulos que implementan el protocolo
  → O(n) donde n = número de implementaciones
```

El número de módulos se escanea linealmente porque en dev cualquier módulo
puede añadir una implementación en calquier momento (hot code reload).

### Con consolidación — Static dispatch O(1)

`mix compile` en producción (o `Protocol.consolidate/2` manualmente) genera
un módulo optimizado donde el dispatch es una única llamada a `apply/3` con
pattern matching compilado:

```
Protocol.dispatch(value)
  → typeof(value) → direct call via case/cond compilado
  → O(1) — el compilador genera un dispatch table estático
```

La diferencia en benchmarks puede ser **10-50x** para protocolos con muchas
implementaciones.

### `@derive`

```elixir
defmodule Point do
  @derive [Inspect, MyProtocol]
  defstruct [:x, :y]
end
```

`@derive` genera automáticamente la implementación de un protocolo para una
struct usando la estrategia que el protocolo define en su cláusula `@for Any`.

### Introspección con `Protocol.impl_for/1`

```elixir
Protocol.impl_for(42)          # => Integer
Protocol.impl_for("hello")     # => BitString
Protocol.impl_for(%MyStruct{}) # => MyStruct (si implementa el protocolo)
Protocol.impl_for(nil)         # => nil (si no implementa y no hay Any)
```

---

## Ejercicio 1 — Benchmark: Consolidado vs No Consolidado

Escribe un benchmark que mida la diferencia de performance entre dispatch
consolidado y no consolidado para un protocolo con múltiples implementaciones.

### Setup del protocolo

```elixir
defprotocol Serializable do
  @doc "Convierte un valor a su representación en string"
  def serialize(value)
end

# Implementar para al menos 15 tipos distintos:
# Integer, Float, BitString, Atom, List, Map, Tuple,
# y 8+ structs propias (Point, Color, Line, Circle, Rect, etc.)
```

### Tarea

1. Implementar el protocolo para los 15+ tipos
2. Escribir una función `run_benchmark/0` que:
   - Ejecute 100_000 llamadas a `Serializable.serialize/1` con valores variados
   - Mida el tiempo con `:timer.tc/1`
   - Fuerze la consolidación con `Protocol.consolidate/2` y repita
   - Imprima la comparación en microsegundos

3. Escribir `compare_dispatch/1` que reciba un valor y retorne:
   ```elixir
   %{
     consolidated: boolean(),  # ¿está el protocolo consolidado?
     impl_module: module(),     # qué módulo maneja este tipo
     dispatch_info: String.t()  # descripción del mecanismo
   }
   ```

### Hints

<details>
<summary>Hint 1 — ¿Cómo consolidar manualmente?</summary>

```elixir
# Lista de módulos que implementan el protocolo
impls = [
  Serializable.Integer,
  Serializable.BitString,
  Serializable.Float,
  # ... etc
]

# Consolidar
{:ok, consolidated_module} = Protocol.consolidate(Serializable, impls)

# Verificar si está consolidado
Protocol.consolidated?(Serializable)  # => true/false
```

Nota: en entorno de desarrollo, `Protocol.consolidate/2` puede no tener efecto
porque Mix recarga el protocolo. Para forzarlo en tests, usa `:code.purge/1`
y `:code.load_binary/3` con el módulo generado.
</details>

<details>
<summary>Hint 2 — Estructura del benchmark</summary>

```elixir
def run_benchmark do
  values = [
    42, 3.14, "hello", :atom, [1, 2, 3], %{a: 1},
    {1, 2}, %Point{x: 1, y: 2}, %Color{r: 255, g: 0, b: 0},
    # ... más valores
  ]

  # Repetir muchas veces para obtener muestra estable
  iterations = 100_000
  sample = Enum.take(Stream.cycle(values), iterations)

  {time_us, _} = :timer.tc(fn ->
    Enum.each(sample, &Serializable.serialize/1)
  end)

  IO.puts("Tiempo total: #{time_us} µs")
  IO.puts("Por llamada: #{time_us / iterations} µs")
end
```
</details>

<details>
<summary>Hint 3 — Introspección con impl_for</summary>

```elixir
def compare_dispatch(value) do
  impl = Serializable.impl_for(value)
  consolidated = Protocol.consolidated?(Serializable)

  %{
    consolidated: consolidated,
    impl_module: impl,
    dispatch_info:
      if consolidated do
        "Static dispatch (O(1)) — módulo consolidado cargado"
      else
        "Dynamic dispatch (O(n)) — buscando entre implementaciones registradas"
      end
  }
end
```
</details>

---

## Ejercicio 2 — `@derive` y derivación automática con el protocolo `Hashable`

Implementa un protocolo `Hashable` que compute un hash determinístico de
cualquier valor Elixir, y soporte derivación automática via `@derive`.

### Definición del protocolo

```elixir
defprotocol Hashable do
  @doc """
  Retorna un hash entero de 64 bits del valor.
  Debe ser determinístico: mismo input → mismo output.
  """
  def hash(value)
end
```

### Implementaciones requeridas

```elixir
# Tipos primitivos
Hashable.hash(42)         # => algún entero
Hashable.hash("hello")    # => algún entero
Hashable.hash(:atom)      # => algún entero
Hashable.hash(3.14)       # => algún entero
Hashable.hash(nil)        # => algún entero

# Tipos compuestos — hash debe combinar los hashes de los elementos
Hashable.hash([1, 2, 3])  # => hash que depende de [hash(1), hash(2), hash(3)]
Hashable.hash({:a, :b})   # => hash de la tupla

# Structs con @derive
defmodule Point do
  @derive Hashable
  defstruct [:x, :y]
end

Hashable.hash(%Point{x: 1, y: 2})  # => hash basado en sus campos
```

### Requisitos de la derivación automática

Cuando se usa `@derive Hashable` en una struct:
1. La implementación generada debe hashear cada campo de la struct en orden
2. Combinar los hashes con una función de mezcla (e.g., XOR con rotación)
3. Incluir el nombre del módulo de la struct en el hash (para evitar colisiones
   entre `%Point{x: 1, y: 2}` y `%Color{x: 1, y: 2}`)

### Implementar derivación con `defimpl ... for Any` + `@derive`

```elixir
defimpl Hashable, for: Any do
  defmacro __deriving__(module, struct, _opts) do
    fields = Map.keys(struct) |> Enum.reject(&(&1 == :__struct__))
    quote do
      defimpl Hashable, for: unquote(module) do
        def hash(value) do
          module_hash = Hashable.hash(unquote(module))
          field_hashes =
            unquote(fields)
            |> Enum.map(fn field -> Hashable.hash(Map.get(value, field)) end)
          Enum.reduce(field_hashes, module_hash, &mix_hash/2)
        end

        defp mix_hash(a, b), do: Bitwise.bxor(a, b * 0x9e3779b9 + (b <<< 6) + (b >>> 2))
      end
    end
  end

  def hash(value) do
    raise Protocol.UndefinedError, protocol: Hashable, value: value
  end
end
```

### Hints

<details>
<summary>Hint 1 — Hash para primitivos</summary>

Usa `:erlang.phash2/1` o implementa un hash simple:

```elixir
defimpl Hashable, for: Integer do
  def hash(n), do: :erlang.phash2(n, 0xFFFFFFFFFFFFFFFF)
end

defimpl Hashable, for: BitString do
  def hash(s) do
    s
    |> :erlang.md5()
    |> :binary.bin_to_list()
    |> Enum.take(8)
    |> Enum.reduce(0, fn byte, acc -> (acc <<< 8) + byte end)
  end
end
```
</details>

<details>
<summary>Hint 2 — Hash compuesto para listas</summary>

```elixir
defimpl Hashable, for: List do
  def hash([]), do: 0
  def hash(list) do
    list
    |> Enum.map(&Hashable.hash/1)
    |> Enum.reduce(fn h, acc ->
      # FNV-1a mix
      Bitwise.bxor(acc, h) |> Kernel.*(0x100000001b3) |> Bitwise.band(0xFFFFFFFFFFFFFFFF)
    end)
  end
end
```
</details>

---

## Ejercicio 3 — Protocol Introspection

Escribe un módulo `ProtocolInspector` con funciones para introspección profunda
de protocolos en el sistema.

### API requerida

```elixir
# ¿Qué módulos implementan un protocolo?
ProtocolInspector.implementations(Enumerable)
# => [List, Map, Range, Stream, MapSet, File.Stream, ...]

# ¿Implementa un valor dado el protocolo?
ProtocolInspector.implements?(42, String.Chars)
# => true

# ¿Está consolidado?
ProtocolInspector.consolidated?(Enumerable)
# => true (en producción)

# Info completa del protocolo
ProtocolInspector.info(Serializable)
# => %{
#   name: Serializable,
#   consolidated: boolean(),
#   callbacks: [:serialize],
#   implementations: [module(), ...],
#   fallback_to_any: boolean()
# }

# Comparar dos protocolos
ProtocolInspector.compare(Enumerable, Collectable)
# => %{
#   only_in_first: [List, ...],
#   only_in_second: [MapSet, ...],
#   in_both: [...]
# }
```

### Hints

<details>
<summary>Hint 1 — Obtener implementaciones via __protocol__</summary>

Los protocolos exponen metainformación vía `__protocol__/1`:

```elixir
Enumerable.__protocol__(:impls)
# En producción (consolidado): {:consolidated, [List, Map, Range, ...]}
# En desarrollo: :not_consolidated

Enumerable.__protocol__(:callbacks)
# => [count: 1, member?: 2, reduce: 3, slice: 1]

Enumerable.__protocol__(:consolidated?)
# => true/false
```
</details>

<details>
<summary>Hint 2 — Listar implementaciones con :not_consolidated</summary>

```elixir
def implementations(protocol) do
  case protocol.__protocol__(:impls) do
    {:consolidated, modules} ->
      modules

    :not_consolidated ->
      # En dev, buscar módulos cargados que implementen el protocolo
      :code.all_loaded()
      |> Enum.map(fn {mod, _} -> mod end)
      |> Enum.filter(fn mod ->
        Protocol.impl_for(struct_or_value_for(mod)) == mod
      end)
  end
end
```

Alternativamente, construye un valor de cada tipo y verifica con `Protocol.impl_for/1`.
</details>

<details>
<summary>Hint 3 — Fallback to Any</summary>

```elixir
def fallback_to_any?(protocol) do
  # Los protocolos definen `@fallback_to_any true` cuando quieren
  # que tipos sin implementación explícita caigan a `for: Any`
  function_exported?(protocol, :__protocol__, 1) and
    protocol.__protocol__(:fallback_to_any)
end
```
</details>

---

## Trade-offs a considerar

### ¿Cuándo importa la consolidación?

En aplicaciones con pocas implementaciones de protocolo (< 10), la diferencia
entre O(n) y O(1) es prácticamente imperceptible. La consolidación importa cuando:
- Tienes 20+ implementaciones de un protocolo
- El protocolo se llama en hot paths (e.g., serialización de requests HTTP)
- El protocolo está en el critical path de latencia

### `@derive` vs implementación explícita

`@derive` es conveniente para structs simples, pero pierde el control fino.
Si necesitas lógica custom (e.g., ignorar ciertos campos, transformar valores),
implementa explícitamente. La regla: `@derive` para comportamiento default,
`defimpl` para comportamiento específico.

### Protocolos vs Behaviours

| Criterio | Protocol | Behaviour |
|---|---|---|
| Polimorfismo sobre | Tipo del valor | Módulo implementador |
| Dispatch | Dinámico por tipo | Estático (módulo conocido) |
| Uso típico | Serialización, pretty-print, comparación | Workers, adaptadores, plugins |
| `impl_for` nil | Posible (sin Any) | N/A — siempre existe |
| Performance consolidado | O(1) | N/A |

### `Protocol.consolidate/2` en tests

Los tests en `mix test` corren en `:test` env, que como `:dev`, no consolida
automáticamente. Si testeas comportamiento dependiente de consolidación, necesitas
consolidar manualmente en el test setup y revertir después.

---

## One possible solution

<details>
<summary>Ver solución parcial — Hashable con @derive (spoiler)</summary>

```elixir
defprotocol Hashable do
  def hash(value)
end

defimpl Hashable, for: Integer do
  def hash(n), do: :erlang.phash2(n, 0xFFFFFFFFFFFFFFFF)
end

defimpl Hashable, for: Atom do
  def hash(a), do: Hashable.hash(Atom.to_string(a))
end

defimpl Hashable, for: BitString do
  def hash(s), do: :erlang.phash2(s, 0xFFFFFFFFFFFFFFFF)
end

defimpl Hashable, for: Float do
  def hash(f), do: :erlang.phash2(f, 0xFFFFFFFFFFFFFFFF)
end

defimpl Hashable, for: List do
  import Bitwise

  def hash([]), do: 0
  def hash(list) do
    list
    |> Enum.map(&Hashable.hash/1)
    |> Enum.reduce(0, fn h, acc -> bxor(acc, h + 0x9e3779b9 + (acc <<< 6) + (acc >>> 2)) end)
  end
end

defimpl Hashable, for: Map do
  def hash(map) do
    map
    |> Enum.sort_by(fn {k, _} -> Hashable.hash(k) end)
    |> Enum.flat_map(fn {k, v} -> [Hashable.hash(k), Hashable.hash(v)] end)
    |> Hashable.hash()
  end
end

defimpl Hashable, for: Any do
  defmacro __deriving__(module, struct, _opts) do
    fields = struct |> Map.keys() |> Enum.reject(&(&1 == :__struct__)) |> Enum.sort()

    quote do
      defimpl Hashable, for: unquote(module) do
        import Bitwise

        def hash(value) do
          module_hash = Hashable.hash(unquote(to_string(module)))
          field_hashes = Enum.map(unquote(fields), &Hashable.hash(Map.get(value, &1)))
          Enum.reduce(field_hashes, module_hash, fn h, acc ->
            bxor(acc, h + 0x9e3779b9 + (acc <<< 6) + (acc >>> 2))
          end)
        end
      end
    end
  end

  def hash(value), do: raise Protocol.UndefinedError, protocol: Hashable, value: value
end

defmodule ProtocolInspector do
  def consolidated?(protocol), do: Protocol.consolidated?(protocol)

  def implementations(protocol) do
    case protocol.__protocol__(:impls) do
      {:consolidated, mods} -> mods
      :not_consolidated -> []
    end
  end

  def implements?(value, protocol) do
    Protocol.impl_for(value) != nil
  end

  def info(protocol) do
    %{
      name: protocol,
      consolidated: consolidated?(protocol),
      callbacks: protocol.__protocol__(:callbacks) |> Keyword.keys(),
      implementations: implementations(protocol),
      fallback_to_any: function_exported?(protocol, :__protocol__, 1) &&
                       protocol.__protocol__(:fallback_to_any)
    }
  end

  def compare(p1, p2) do
    i1 = MapSet.new(implementations(p1))
    i2 = MapSet.new(implementations(p2))

    %{
      only_in_first:  MapSet.difference(i1, i2) |> MapSet.to_list(),
      only_in_second: MapSet.difference(i2, i1) |> MapSet.to_list(),
      in_both:        MapSet.intersection(i1, i2) |> MapSet.to_list()
    }
  end
end
```

</details>
