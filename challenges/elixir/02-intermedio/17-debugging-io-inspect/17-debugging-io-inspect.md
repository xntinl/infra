# 17. Debugging con IO.inspect y dbg

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de pipelines con `|>`
- Estructuras de datos: mapas, listas, tuplas
- Procesos básicos con `self()` y `Process`

## Learning Objectives
After completing this exercise, you will be able to:
- Usar `IO.inspect/2` para inspeccionar valores en medio de un pipeline sin interrumpirlo
- Aplicar `dbg/1` de Elixir 1.14+ para debugging interactivo con contexto completo
- Configurar opciones de `IO.inspect` para estructuras grandes o profundas
- Inspeccionar procesos en runtime con `Process.info/2` y `Process.list/0`
- Leer e interpretar stack traces de Elixir para localizar errores

## Concepts

### IO.inspect/2: El Debugging No Destructivo

`IO.inspect/2` retorna su primer argumento sin modificarlo, lo que permite insertarlo en cualquier punto de un pipeline para observar el valor sin romper el flujo de datos. Es la herramienta de debugging más usada en el día a día.

```elixir
# Sin IO.inspect — no sabes qué valor tiene en cada etapa
result =
  [1, 2, 3, 4, 5]
  |> Enum.filter(&(rem(&1, 2) == 0))
  |> Enum.map(&(&1 * 10))
  |> Enum.sum()

# Con IO.inspect — observas cada etapa sin cambiar el resultado
result =
  [1, 2, 3, 4, 5]
  |> IO.inspect(label: "input")
  |> Enum.filter(&(rem(&1, 2) == 0))
  |> IO.inspect(label: "after filter")
  |> Enum.map(&(&1 * 10))
  |> IO.inspect(label: "after map")
  |> Enum.sum()

# Output en consola:
# input: [1, 2, 3, 4, 5]
# after filter: [2, 4]
# after map: [20, 40]
# result sigue siendo 60
```

La clave es que `IO.inspect` retorna el valor inalterado — no rompe el pipeline.

### Opciones de IO.inspect

`IO.inspect/2` acepta un keyword list de opciones para controlar cómo se muestra el valor:

```elixir
# label: Prefijo descriptivo para identificar el punto de inspección
IO.inspect(value, label: "My Value")
# My Value: [1, 2, 3]

# limit: Máximo de elementos a mostrar en listas/maps (default: 50)
IO.inspect(long_list, limit: 5)
# [1, 2, 3, 4, 5, ...]

# pretty: true para formato multilínea legible
IO.inspect(nested_map, pretty: true)

# width: caracteres por línea en modo pretty
IO.inspect(value, pretty: true, width: 40)

# structs: false para ver el mapa subyacente de un struct
IO.inspect(%MyStruct{}, structs: false)

# Combinación típica para debugging de estructuras complejas
IO.inspect(complex_data, label: "API response", pretty: true, limit: 10)
```

### dbg/1: Debugging Interactivo (Elixir 1.14+)

`dbg/1` es una macro de debugging más poderosa que `IO.inspect`. Muestra no solo el valor, sino también la expresión que lo produjo, el archivo, y la línea. Cuando se usa con `IEx`, permite inspección interactiva.

```elixir
# dbg muestra la expresión Y el valor
x = 42
dbg(x)
# [lib/my_module.ex:10: MyModule.my_function/1]
# x #=> 42

# dbg en pipeline — muestra cada paso
result =
  [1, 2, 3]
  |> dbg()
  |> Enum.map(&(&1 * 2))
  |> dbg()
  |> Enum.sum()

# Output:
# [lib/my_module.ex:3: MyModule.run/0]
# [1, 2, 3] |> Enum.map(&(&1 * 2)) #=> [2, 4, 6]
# [2, 4, 6] |> Enum.sum() #=> 12
```

En IEx, `dbg` abre un sub-shell donde puedes inspeccionar variables locales antes de continuar.

### Process.info: Inspección de Procesos

En sistemas concurrentes, a veces necesitas inspeccionar el estado interno de un proceso en runtime.

```elixir
# Información del proceso actual
Process.info(self())
# Retorna una keyword list con: status, memory, message_queue_len, etc.

# Solo una clave específica (más eficiente)
Process.info(self(), :message_queue_len)
# {:message_queue_len, 0}

# Información de memoria del proceso
Process.info(self(), :memory)
# {:memory, 2840}   # bytes

# Ver mensajes en la queue sin consumirlos
Process.info(self(), :messages)
# {:messages, []}

# Listar todos los procesos vivos en el sistema
Process.list()
# [#PID<0.0.0>, #PID<0.1.0>, ...]
# Devuelve cientos de PIDs — usar con cuidado en producción

# Número de procesos activos
length(Process.list())
```

### Leyendo Stack Traces

Cuando un proceso crashea, el stack trace muestra el camino exacto que llevó al error. Saber leerlo es fundamental.

```
** (ArithmeticError) bad argument in arithmetic expression
    (my_app 0.1.0) lib/calculator.ex:15: Calculator.divide/2
    (my_app 0.1.0) lib/calculator.ex:8: Calculator.process/1
    (my_app 0.1.0) lib/my_app.ex:42: MyApp.run/0
    (elixir 1.15.0) lib/task.ex:330: Task.await/2
```

Cómo leer cada línea:
- `(ArithmeticError)` — tipo de excepción
- `bad argument in arithmetic expression` — mensaje del error
- `(my_app 0.1.0) lib/calculator.ex:15` — aplicación, archivo y línea
- `Calculator.divide/2` — módulo, función y aridad (número de argumentos)

El error ocurrió en `Calculator.divide/2` línea 15, que fue llamada desde `Calculator.process/1` línea 8.

## Exercises

### Exercise 1: IO.inspect con label en Pipeline

Inserta `IO.inspect` con labels descriptivos en cada paso del pipeline para ver la transformación de datos.

```elixir
defmodule DataTransformer do
  def process_sales(raw_data) do
    raw_data
    # TODO: Agrega IO.inspect con label: "1. raw input"
    |> Enum.filter(fn sale -> sale.amount > 0 end)
    # TODO: Agrega IO.inspect con label: "2. after filter negative"
    |> Enum.map(fn sale -> %{sale | amount: sale.amount * 1.19} end)
    # TODO: Agrega IO.inspect con label: "3. after tax"
    |> Enum.sort_by(& &1.amount, :desc)
    # TODO: Agrega IO.inspect con label: "4. sorted"
    |> Enum.take(3)
    # TODO: Agrega IO.inspect con label: "5. top 3"
  end
end

# Datos de prueba para ejecutar en IEx:
sales = [
  %{id: 1, product: "Widget", amount: 50.0},
  %{id: 2, product: "Gadget", amount: -10.0},   # Venta inválida
  %{id: 3, product: "Donut", amount: 120.0},
  %{id: 4, product: "Cable", amount: 15.0},
  %{id: 5, product: "Monitor", amount: 350.0},
  %{id: 6, product: "Mouse", amount: 25.0}
]

DataTransformer.process_sales(sales)
```

Expected output:
```
1. raw input: [%{amount: 50.0, id: 1, product: "Widget"}, ...]
2. after filter negative: [%{amount: 50.0, id: 1, ...}, ...]  # Sin el -10.0
3. after tax: [%{amount: 59.5, id: 1, ...}, ...]
4. sorted: [%{amount: 416.5, id: 5, ...}, ...]
5. top 3: [%{amount: 416.5, ...}, %{amount: 142.8, ...}, %{amount: 59.5, ...}]
```

---

### Exercise 2: dbg en Pipeline

Reemplaza los `IO.inspect` anteriores con `dbg/1` y observa la diferencia en el output — dbg muestra el contexto de la expresión.

```elixir
defmodule TextProcessor do
  def analyze(text) do
    text
    # TODO: Inserta dbg() aquí para ver el valor inicial
    |> String.downcase()
    # TODO: Inserta dbg() aquí para ver después de downcase
    |> String.split(~r/\W+/, trim: true)
    # TODO: Inserta dbg() aquí para ver las palabras
    |> Enum.frequencies()
    # TODO: Inserta dbg() aquí para ver las frecuencias
    |> Enum.sort_by(fn {_word, count} -> count end, :desc)
    |> Enum.take(5)
  end
end

# Ejecutar en IEx:
# TextProcessor.analyze("the quick brown fox jumps over the lazy dog the fox")
```

Expected output (fragmento — dbg muestra la expresión y el resultado):
```
[lib/text_processor.ex:5: TextProcessor.analyze/1]
"the quick brown fox jumps over the lazy dog the fox" |> String.downcase() #=> "the quick..."

[lib/text_processor.ex:7: TextProcessor.analyze/1]
... |> String.split(~r/\W+/, trim: true) #=> ["the", "quick", "brown", ...]
```

---

### Exercise 3: IO.inspect con Opciones para Datos Grandes

Aprende a configurar `IO.inspect` para estructuras grandes o profundamente anidadas.

```elixir
defmodule DataInspector do
  # Genera datos de prueba con estructura compleja
  def sample_data do
    Enum.map(1..100, fn i ->
      %{
        id: i,
        name: "Product #{i}",
        tags: Enum.take(["electronics", "books", "food", "sports", "toys"], rem(i, 5) + 1),
        metadata: %{
          created_at: "2024-01-#{rem(i, 28) + 1}",
          weight: i * 0.5,
          dimensions: %{width: i, height: i * 2, depth: i * 0.5}
        }
      }
    end)
  end

  def process(data) do
    data
    # TODO: Agrega IO.inspect con limit: 3 para ver solo los primeros 3 elementos
    # Esto evita inundar la consola con 100 items
    |> Enum.filter(fn p -> p.id <= 10 end)
    # TODO: Agrega IO.inspect con pretty: true y limit: 5
    # Verás la estructura anidada formateada correctamente
    |> Enum.map(fn p -> Map.take(p, [:id, :name]) end)
    # TODO: Agrega IO.inspect con label: "simplified" para el resultado final
  end
end

# También practica inspeccionar con opciones directamente en IEx:
# data = DataInspector.sample_data()
# TODO: Inspecciona data con limit: 2 para ver solo 2 elementos
# TODO: Inspecciona el primer elemento con pretty: true para ver la estructura anidada
```

Expected output:
```elixir
# Con limit: 3
[
  %{id: 1, name: "Product 1", tags: [...], metadata: %{...}},
  %{id: 2, name: "Product 2", tags: [...], metadata: %{...}},
  %{id: 3, name: "Product 3", tags: [...], metadata: %{...}},
  ...
]
# Con pretty: true — indentación multilínea legible
%{
  dimensions: %{depth: 0.5, height: 2, width: 1},
  ...
}
```

---

### Exercise 4: Process.info para Inspección de Procesos

Usa las funciones de `Process` para inspeccionar el estado de procesos en runtime.

```elixir
defmodule ProcessInspector do
  def inspect_self do
    pid = self()

    # TODO: Usa Process.info/1 para obtener toda la info del proceso actual
    # Asigna el resultado a una variable e imprimelo con IO.inspect

    # TODO: Usa Process.info/2 para obtener solo :message_queue_len del proceso actual
    # ¿Cuántos mensajes hay en la queue? (debería ser 0)

    # TODO: Usa Process.info/2 para obtener :memory del proceso actual
    # ¿Cuánta memoria usa este proceso en bytes?

    # TODO: Usa Process.list() para obtener todos los PIDs activos
    # ¿Cuántos procesos hay? Usa length/1 para contarlos

    # TODO: Envíate 3 mensajes a ti mismo con send(self(), :test_message)
    # Luego vuelve a consultar :message_queue_len
    # ¿Cambió el número?
    :ok
  end

  def send_and_inspect do
    pid = self()

    # Envía mensajes sin consumirlos
    send(pid, {:message, 1})
    send(pid, {:message, 2})
    send(pid, {:message, 3})

    # TODO: Consulta Process.info(self(), :messages) para ver los mensajes
    # sin consumirlos — ¿aparecen los 3?

    # TODO: Consulta Process.info(self(), :message_queue_len)
    # El resultado debe ser {:message_queue_len, 3}
  end
end
```

Expected output:
```elixir
iex> ProcessInspector.inspect_self()
# Total process count: ~150 (varía según el sistema)
# message_queue_len antes: {:message_queue_len, 0}
# memory: {:memory, 2840}
# message_queue_len después de 3 sends: {:message_queue_len, 3}
:ok
```

---

### Exercise 5: Leer e Interpretar Stack Traces

Provoca errores intencionalmente y practica leer el stack trace para identificar la causa raíz.

```elixir
defmodule BuggyModule do
  # Esta función tiene un bug intencional
  def process_order(order) do
    order
    |> validate_order()
    |> apply_discount()
    |> calculate_total()
  end

  defp validate_order(order) do
    # TODO: Agrega IO.inspect(order, label: "validate_order input") aquí
    order
  end

  defp apply_discount(order) do
    # TODO: Agrega IO.inspect(order, label: "apply_discount input") aquí
    # Este bug: si order.discount es nil, falla con ArithmeticError
    %{order | total: order.total * (1 - order.discount)}
  end

  defp calculate_total(order) do
    # TODO: Agrega IO.inspect(order, label: "calculate_total input") aquí
    order.total + order.shipping
  end
end

# Ejecuta en IEx y observa el stack trace:
# BuggyModule.process_order(%{total: 100.0, discount: nil, shipping: 10.0})

# Ejercicio:
# 1. Ejecuta el código y lee el stack trace completo
# 2. Identifica: ¿en qué función ocurrió el error?
# 3. Identifica: ¿en qué línea?
# 4. ¿Cuál es la causa raíz del error?
# 5. TODO: Corrige el bug en apply_discount/1 para manejar discount: nil
#    (usa || 0 o un guard para defaultear a 0 cuando discount es nil)
```

Expected output (stack trace de ejemplo):
```
** (ArithmeticError) bad argument in arithmetic expression: 1 - nil
    (my_app 0.1.0) lib/buggy_module.ex:16: BuggyModule.apply_discount/1
    (my_app 0.1.0) lib/buggy_module.ex:5: BuggyModule.process_order/1
```

---

## Try It Yourself

Debuggea un pipeline de procesamiento de datos usando `IO.inspect` en cada etapa para encontrar dónde se introduce un bug. Sin solución incluida.

```elixir
defmodule ReportGenerator do
  @moduledoc """
  Genera un reporte de ventas. Hay un bug en algún paso del pipeline.
  Usa IO.inspect en cada etapa para encontrar dónde los datos se corrompen.
  """

  def generate_report(transactions) do
    transactions
    |> filter_valid()
    |> group_by_category()
    |> calculate_category_totals()
    |> sort_by_revenue()
    |> format_report()
  end

  defp filter_valid(txns), do: Enum.filter(txns, &valid?/1)
  defp valid?(%{amount: a}) when is_number(a) and a > 0, do: true
  defp valid?(_), do: false

  defp group_by_category(txns), do: Enum.group_by(txns, & &1.category)

  # BUG: Esta función hace algo incorrecto con los totales
  defp calculate_category_totals(grouped) do
    Enum.map(grouped, fn {category, txns} ->
      # Hay un bug aquí — ¿lo encuentras con IO.inspect?
      {category, Enum.count(txns)}  # Debería ser Enum.sum con el amount
    end)
  end

  defp sort_by_revenue(categories), do: Enum.sort_by(categories, &elem(&1, 1), :desc)

  defp format_report(categories) do
    Enum.map(categories, fn {cat, total} ->
      "#{cat}: $#{Float.round(total * 1.0, 2)}"
    end)
  end
end

# Datos de prueba:
transactions = [
  %{id: 1, category: "electronics", amount: 250.0},
  %{id: 2, category: "books", amount: 15.0},
  %{id: 3, category: "electronics", amount: 180.0},
  %{id: 4, category: "books", amount: -5.0},   # Inválida
  %{id: 5, category: "food", amount: 45.0},
  %{id: 6, category: "electronics", amount: 320.0}
]

# 1. Ejecuta ReportGenerator.generate_report(transactions)
# 2. Agrega IO.inspect después de cada paso del pipeline
# 3. Encuentra el bug (pista: el total de electronics debería ser 750.0, no 3)
# 4. Corrige la función calculate_category_totals/1
```

**Objetivo**: Usar únicamente `IO.inspect` (sin leer el código en detalle) para diagnosticar y confirmar el bug. Luego corregirlo.

---

## Common Mistakes

### Mistake 1: Olvidar que IO.inspect retorna el valor — el pipeline no se rompe

**Wrong (creencia incorrecta):**
```elixir
# Muchos desarrolladores piensan que IO.inspect "interrumpe" el pipeline
result =
  [1, 2, 3]
  |> IO.inspect()   # ¿Esto rompe el pipe?
  |> Enum.sum()
```
**Aclaración:** `IO.inspect` retorna su primer argumento sin modificar. El pipeline funciona exactamente igual con o sin él. Es seguro agregarlo y quitarlo sin alterar el comportamiento.

### Mistake 2: Usar IO.puts en lugar de IO.inspect para debugging

**Wrong:**
```elixir
IO.puts(complex_data)  # Convierte a string con to_string/1 — pierde estructura
```
**Why:** `IO.puts` llama a `String.Chars.to_string/1`, que para estructuras complejas puede fallar o perder información. `IO.inspect` siempre muestra la representación Elixir completa.
**Fix:**
```elixir
IO.inspect(complex_data, label: "debug")  # Siempre muestra la estructura real
```

### Mistake 3: Dejar IO.inspect en código de producción

**Wrong:**
```elixir
# lib/my_module.ex — código que va a producción
def process(data) do
  data
  |> IO.inspect(label: "DEBUG")   # Olvidado en producción
  |> transform()
end
```
**Why:** `IO.inspect` escribe a stdout en producción, lo que puede saturar logs, exponer datos sensibles, o degradar performance.
**Fix:** Usa un buscador de texto para encontrar `IO.inspect` antes de cada PR/deploy:
```bash
$ grep -r "IO.inspect" lib/   # Debe retornar vacío antes de merge
```

---

## Verification

```elixir
# En IEx, verificar que IO.inspect no rompe pipelines
iex> [1, 2, 3] |> IO.inspect(label: "test") |> Enum.sum()
# test: [1, 2, 3]
# 6

# Verificar dbg disponible (requiere Elixir 1.14+)
iex> elixir_version = System.version()
iex> dbg(elixir_version)
# [iex:2: (top level)]
# elixir_version #=> "1.15.7"

# Verificar Process.info
iex> Process.info(self(), :message_queue_len)
{:message_queue_len, 0}
```

## Summary
- **Key concepts**: `IO.inspect/2`, `label:`, `limit:`, `pretty:`, `dbg/1`, `Process.info/2`, stack traces
- **What you practiced**: Inspeccionar pipelines sin interrumpirlos, configurar output para datos complejos, inspeccionar procesos en runtime, localizar errores con stack traces
- **Important to remember**: `IO.inspect` retorna el valor — nunca rompe el pipeline. `dbg` muestra la expresión + valor. Elimina todos los `IO.inspect` antes de mergear código.

## What's Next
En el siguiente ejercicio **18-mix-tasks-personalizadas** aprenderás a crear tus propias Mix tasks para automatizar tareas repetitivas del proyecto.

## Resources
- [IO.inspect Documentation](https://hexdocs.pm/elixir/IO.html#inspect/2)
- [Kernel.dbg/2 Documentation](https://hexdocs.pm/elixir/Kernel.html#dbg/2)
- [Process Module](https://hexdocs.pm/elixir/Process.html)
- [Debugging Guide — Elixir](https://elixir-lang.org/getting-started/debugging.html)
