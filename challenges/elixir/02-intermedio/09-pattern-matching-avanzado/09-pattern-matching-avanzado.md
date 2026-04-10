# 09. Pattern Matching Avanzado

**Difficulty**: Intermedio

---

## Prerequisites

- Pattern matching básico (literales, variables, `_`)
- Structs y Maps en Elixir
- Funciones multi-cláusula
- Ejercicio 08: Protocols

---

## Learning Objectives

1. Aplicar guards `when` para refinar el matching más allá del tipo/estructura
2. Ordenar correctamente cláusulas multi-clause (de más a menos específica)
3. Usar el operador pin `^` para comparar contra valores ya enlazados
4. Aprovechar el underscore `_` y sus variantes para ignorar partes irrelevantes
5. Hacer nested pattern matching en maps, structs y listas anidadas
6. Diseñar state machines con funciones multi-cláusula y guards

---

## Concepts

### Guards `when`

Los guards amplían el pattern matching con predicados booleanos. Se evalúan solo si el patrón ya coincidió:

```elixir
defmodule Clasificador do
  def clasificar(n) when is_integer(n) and n > 0, do: :positivo
  def clasificar(n) when is_integer(n) and n < 0, do: :negativo
  def clasificar(0),                               do: :cero
  def clasificar(_),                               do: :no_es_entero
end

Clasificador.clasificar(5)    # :positivo
Clasificador.clasificar(-3)   # :negativo
Clasificador.clasificar(0)    # :cero
Clasificador.clasificar("x")  # :no_es_entero
```

**Funciones permitidas en guards** (subconjunto seguro sin efectos secundarios):
`is_integer/1`, `is_float/1`, `is_atom/1`, `is_binary/1`, `is_list/1`, `is_map/1`,
`is_nil/1`, `is_pid/1`, `length/1`, `map_size/1`, `div/2`, `rem/2`, `abs/1`,
operadores aritméticos, comparación, booleanos `and/or/not`.

```elixir
# Guards compuestos
def procesar(lista) when is_list(lista) and length(lista) > 0 do
  hd(lista)
end

def procesar([]), do: {:error, :lista_vacia}

# Guard con múltiples tipos (or)
def es_numero?(v) when is_integer(v) or is_float(v), do: true
def es_numero?(_), do: false
```

### Operador Pin `^`

El operador `^` fija una variable a su valor actual en lugar de reasignarla:

```elixir
esperado = :ok
resultado = :ok

# Sin pin — reasigna resultado (siempre coincide)
case resultado do
  esperado -> "siempre llega aquí"
end

# Con pin — compara contra el valor de esperado
case resultado do
  ^esperado -> "coincide con :ok"
  _         -> "otro valor"
end
```

Casos de uso frecuentes:

```elixir
# En receive — esperar respuesta de un PID específico
pid = self()
receive do
  {^pid, mensaje} -> mensaje  # Solo del PID correcto
  _               -> :ignorado
end

# En funciones — verificar que algo no cambió
def sin_cambios?([h | t], ^h), do: true
def sin_cambios?(_, _),        do: false
```

### Underscore y variantes

```elixir
# _ descarta cualquier valor
{:ok, _} = {:ok, "ignorado"}

# _nombre documenta qué es pero no lo usa (evita warning del compilador)
def log({:error, _razon} = evento), do: IO.inspect(evento)

# Múltiples _ en el mismo patrón — cada uno es independiente
{_, _, _} = {1, 2, 3}
```

### Nested Pattern Matching

El matching puede ir tan profundo como la estructura requiera:

```elixir
# Maps anidados
%{usuario: %{nombre: nombre, rol: :admin}} = datos
# Solo coincide si datos.usuario.rol es :admin

# Listas con patrones internos
[%{id: id1}, %{id: id2} | _resto] = registros
# Desempaqueta los primeros dos ids

# Structs anidados
defmodule Pedido do
  defstruct [:id, :cliente, :lineas]
end

defmodule Cliente do
  defstruct [:nombre, :tipo]  # tipo: :vip | :normal
end

def descuento(%Pedido{cliente: %Cliente{tipo: :vip}, lineas: l})
    when length(l) > 5 do
  0.20  # 20% para VIP con más de 5 líneas
end

def descuento(%Pedido{cliente: %Cliente{tipo: :vip}}) do
  0.10  # 10% para VIP con pocas líneas
end

def descuento(_), do: 0.0  # Sin descuento por defecto
```

### Multi-Clause Ordering

Las cláusulas se evalúan de arriba a abajo. Las más específicas deben ir primero:

```elixir
# MAL — la cláusula genérica captura todo
def f(_),          do: :cualquiera    # siempre llega aquí
def f(0),          do: :cero          # nunca se alcanza

# BIEN — de específico a general
def f(0),          do: :cero
def f(n) when n > 0, do: :positivo
def f(_),          do: :negativo
```

---

## Exercises

### Exercise 1: Parser de Tokens con Pattern Matching

Implementa un lexer/tokenizador que convierte strings en tokens tipados usando pattern matching multi-cláusula.

```elixir
# Archivo: lib/lexer.ex

defmodule Lexer do
  @moduledoc """
  Tokenizador simple para una expresión aritmética.
  Tokens posibles:
    {:number, integer}
    {:op, :plus | :minus | :mult | :div}
    {:lparen}
    {:rparen}
    {:whitespace}
    {:unknown, char}
  """

  @doc """
  Tokeniza un string completo, devolviendo lista de tokens.
  Filtra los whitespace del resultado final.
  """
  def tokenize(input) when is_binary(input) do
    input
    |> String.graphemes()
    |> Enum.map(&tokenize_char/1)
    |> Enum.reject(&match?({:whitespace}, &1))
  end

  # TODO: Implementar tokenize_char/1 con pattern matching
  # Debe manejar los siguientes casos:
  # - Dígitos "0".."9" -> {:number, integer}   (Pista: String.to_integer/1)
  # - "+" -> {:op, :plus}
  # - "-" -> {:op, :minus}
  # - "*" -> {:op, :mult}
  # - "/" -> {:op, :div}
  # - "(" -> {:lparen}
  # - ")" -> {:rparen}
  # - " " o "\t" -> {:whitespace}
  # - Cualquier otro -> {:unknown, char}
  defp tokenize_char(char) do
  end

  @doc """
  Agrupa tokens consecutivos de tipo :number en un solo número multi-dígito.
  Ej: [{:number,1},{:number,2},{:op,:plus}] -> [{:number,12},{:op,:plus}]
  """
  def merge_numbers(tokens) do
    # TODO: Implementar con Enum.reduce/3
    # Acumula dígitos consecutivos {:number, n} en un buffer
    # Cuando aparece un token no-number, vuelca el buffer como {:number, N}
    # Pista: el acumulador puede ser {lista_resultado, buffer_digitos}
    # donde buffer_digitos es una lista de dígitos pendientes de unir
  end

  @doc """
  Valida que los paréntesis estén balanceados.
  Devuelve :ok | {:error, :unclosed} | {:error, :unopened}
  """
  def validate_parens(tokens) do
    # TODO: Usar Enum.reduce_while/3
    # Lleva un contador: +1 en :lparen, -1 en :rparen
    # Si el contador baja de 0 -> {:halt, {:error, :unopened}}
    # Al final: 0 -> :ok, >0 -> {:error, :unclosed}
  end
end
```

**Verificación esperada:**

```elixir
Lexer.tokenize("3 + 42 * (2 - 1)")
# [
#   {:number, 3}, {:op, :plus}, {:number, 4}, {:number, 2},
#   {:op, :mult}, {:lparen}, {:number, 2}, {:op, :minus}, {:number, 1}, {:rparen}
# ]

tokens = Lexer.tokenize("3 + 42 * (2 - 1)")
Lexer.merge_numbers(tokens)
# [{:number, 3}, {:op, :plus}, {:number, 42}, {:op, :mult},
#  {:lparen}, {:number, 2}, {:op, :minus}, {:number, 1}, {:rparen}]

Lexer.validate_parens(Lexer.merge_numbers(tokens))   # :ok
Lexer.validate_parens([{:lparen}, {:number, 1}])     # {:error, :unclosed}
Lexer.validate_parens([{:rparen}, {:number, 1}])     # {:error, :unopened}
```

---

### Exercise 2: State Machine con Guards

Implementa una máquina de estados para un semáforo de tráfico con transiciones controladas por guards y multi-clause.

```elixir
# Archivo: lib/semaforo.ex

defmodule Semaforo do
  @moduledoc """
  Máquina de estados para un semáforo.

  Estados: :rojo | :verde | :amarillo
  Eventos: :tick (avance normal) | :emergencia | :reset

  Transiciones normales (tick):
    :rojo     -> :verde
    :verde    -> :amarillo
    :amarillo -> :rojo

  Transiciones especiales:
    :emergencia desde cualquier estado -> :rojo
    :reset      desde cualquier estado -> :rojo
  """

  defstruct [:estado, :ciclos, :en_emergencia]
  # ciclos: cuántas veces ha completado rojo->verde->amarillo->rojo
  # en_emergencia: boolean

  def nuevo do
    %Semaforo{estado: :rojo, ciclos: 0, en_emergencia: false}
  end

  @doc """
  Aplica un evento al semáforo y devuelve el nuevo estado.
  """
  # TODO: Implementar transition/2 con múltiples cláusulas
  # Casos a cubrir:
  # 1. Evento :emergencia -> pone en_emergencia: true, estado: :rojo (sin contar ciclo)
  # 2. Evento :reset      -> vuelve a estado inicial (ciclos no se resetean)
  # 3. :tick desde :rojo  -> :verde
  # 4. :tick desde :verde -> :amarillo
  # 5. :tick desde :amarillo -> :rojo, incrementa ciclos en 1
  # 6. Cualquier otro evento -> devuelve el semáforo sin cambios
  def transition(%Semaforo{} = sem, evento) do
  end

  @doc """
  Devuelve el color como string para mostrar.
  """
  # TODO: Usar pattern matching con guard para cada estado
  # :rojo     -> "🔴 STOP"
  # :verde    -> "🟢 GO"
  # :amarillo -> "🟡 SLOW"
  def mostrar(%Semaforo{estado: estado, en_emergencia: true}) do
    # TODO: Si está en emergencia, prefija con "⚠️  EMERGENCIA — "
  end

  def mostrar(%Semaforo{estado: estado}) do
  end

  @doc """
  Simula N ciclos del semáforo, devolviendo el log de estados.
  """
  def simular(semaforo, n_ticks) when is_integer(n_ticks) and n_ticks > 0 do
    # TODO: Usar Enum.reduce/3 sobre 1..n_ticks
    # Acumula {semaforo_actual, lista_de_logs}
    # Cada tick registra: {tick_numero, estado, ciclos}
    Enum.reduce(1..n_ticks, {semaforo, []}, fn tick, {sem, log} ->
    end)
    |> then(fn {sem_final, log} -> {sem_final, Enum.reverse(log)} end)
  end
end
```

**Verificación esperada:**

```elixir
sem = Semaforo.nuevo()
Semaforo.mostrar(sem)  # "🔴 STOP"

sem = Semaforo.transition(sem, :tick)
Semaforo.mostrar(sem)  # "🟢 GO"

sem = Semaforo.transition(sem, :emergencia)
Semaforo.mostrar(sem)  # "⚠️  EMERGENCIA — 🔴 STOP"

sem = Semaforo.transition(sem, :reset)
sem.en_emergencia  # false
sem.estado         # :rojo

{sem_final, log} = Semaforo.simular(Semaforo.nuevo(), 9)
# log tiene 9 entradas: 3 ciclos completos
sem_final.ciclos   # 3
```

---

### Exercise 3: Matching en Estructuras Anidadas

Implementa un procesador de órdenes de compra que extrae información usando nested pattern matching.

```elixir
# Archivo: lib/order_processor.ex

defmodule Direccion do
  defstruct [:calle, :ciudad, :pais, :codigo_postal]
end

defmodule LineaPedido do
  defstruct [:producto_id, :nombre, :cantidad, :precio_unitario]
end

defmodule Cliente do
  defstruct [:id, :nombre, :tipo, :direccion]
  # tipo: :vip | :corporativo | :normal
end

defmodule Pedido do
  defstruct [:id, :cliente, :lineas, :estado, :metodo_pago]
  # estado: :pendiente | :confirmado | :enviado | :cancelado
  # metodo_pago: :tarjeta | :transferencia | :contrarrembolso
end

defmodule OrderProcessor do
  @doc """
  Calcula el total de un pedido aplicando descuentos según tipo de cliente.
  Descuentos: VIP -> 15%, corporativo -> 10%, normal -> 0%
  """
  # TODO: Usar nested pattern matching para extraer cliente.tipo y lineas
  # Calcular subtotal: sum(cantidad * precio_unitario por linea)
  # Aplicar descuento según tipo de cliente
  def calcular_total(%Pedido{
        cliente: %Cliente{tipo: tipo},
        lineas: lineas
      }) do
  end

  @doc """
  Filtra pedidos por país del cliente y estado.
  """
  # TODO: Usar Enum.filter/2 con nested pattern matching en el predicado
  # Predicado: pedido.cliente.direccion.pais == pais AND pedido.estado == estado
  def filtrar_por_pais_y_estado(pedidos, pais, estado) do
  end

  @doc """
  Extrae los IDs de producto de todos los pedidos confirmados de clientes VIP
  en España, sin duplicados.
  """
  # TODO: Combinar Enum.flat_map + pattern matching en cláusula + MapSet para uniq
  def productos_vip_espana(pedidos) do
    pedidos
    |> Enum.filter(fn
      # TODO: Pattern match aquí — solo pedidos confirmados de VIP en España
    end)
    |> Enum.flat_map(fn %Pedido{lineas: lineas} ->
      # TODO: Extraer producto_id de cada línea
    end)
    |> Enum.uniq()
  end

  @doc """
  Clasifica un pedido como :rentable, :neutro, o :deficitario.
  Un pedido es rentable si total > 500, neutro si 100..500, deficitario si < 100.
  Además, los pedidos de contrarrembolso tienen un cargo fijo de +10.
  """
  # TODO: Usar multiple cláusulas con guards
  # Pista: calcular_total primero, luego ajustar por metodo_pago, luego clasificar
  def clasificar(%Pedido{metodo_pago: :contrarrembolso} = pedido) do
  end

  def clasificar(%Pedido{} = pedido) do
  end

  @doc """
  Agrupa pedidos por ciudad del cliente.
  Devuelve %{ciudad => [pedidos]}
  """
  # TODO: Usar Enum.group_by/2 con nested pattern matching
  def agrupar_por_ciudad(pedidos) do
  end
end
```

**Verificación esperada:**

```elixir
dir_es = %Direccion{calle: "Gran Vía 1", ciudad: "Madrid", pais: "España", codigo_postal: "28001"}
dir_fr = %Direccion{calle: "Rue de la Paix", ciudad: "París", pais: "Francia", codigo_postal: "75001"}

cliente_vip  = %Cliente{id: 1, nombre: "Ana VIP",  tipo: :vip,        direccion: dir_es}
cliente_corp = %Cliente{id: 2, nombre: "Corp SL",  tipo: :corporativo, direccion: dir_es}
cliente_norm = %Cliente{id: 3, nombre: "Bob Normal", tipo: :normal,    direccion: dir_fr}

lineas_a = [
  %LineaPedido{producto_id: "P1", nombre: "Silla", cantidad: 2, precio_unitario: 150.0},
  %LineaPedido{producto_id: "P2", nombre: "Mesa",  cantidad: 1, precio_unitario: 300.0}
]
# Subtotal: 600.0

pedido_vip = %Pedido{
  id: "O1", cliente: cliente_vip, lineas: lineas_a,
  estado: :confirmado, metodo_pago: :tarjeta
}

OrderProcessor.calcular_total(pedido_vip)
# 600.0 * (1 - 0.15) = 510.0

OrderProcessor.clasificar(pedido_vip)
# :rentable  (510 > 500)

pedidos = [pedido_vip, ...]
OrderProcessor.filtrar_por_pais_y_estado(pedidos, "España", :confirmado)
# Solo pedidos de clientes en España con estado :confirmado

OrderProcessor.productos_vip_espana(pedidos)
# ["P1", "P2"]  <- únicos, sin duplicar
```

---

## Common Mistakes

### 1. Guard inválido — función con efectos secundarios

```elixir
# MAL — String.contains? no está permitida en guards
def f(s) when String.contains?(s, "hola"), do: :saludo

# BIEN — mover la lógica al cuerpo
def f(s) when is_binary(s) do
  if String.contains?(s, "hola"), do: :saludo, else: :otro
end
```

### 2. Orden incorrecto de cláusulas

```elixir
# MAL — el catch-all captura antes que el caso específico
def describir(lista) when is_list(lista), do: "lista de #{length(lista)}"
def describir([]),                         do: "lista vacía"  # nunca alcanzado

# BIEN — más específico primero
def describir([]),                          do: "lista vacía"
def describir(lista) when is_list(lista),   do: "lista de #{length(lista)}"
```

### 3. Pin operator olvidado

```elixir
esperado = :ok

# MAL — reasigna esperado a :error (siempre coincide)
case :error do
  esperado -> IO.puts("coincide con #{esperado}")  # imprime "coincide con :error"
end

# BIEN — fija el valor con ^
case :error do
  ^esperado -> IO.puts("coincide")
  otro      -> IO.puts("no coincide, es #{otro}")  # llega aquí
end
```

### 4. Nested matching demasiado profundo sin variables intermedias

```elixir
# MAL — difícil de leer y mantener
def procesar(%{a: %{b: %{c: %{d: valor}}}}) when valor > 0, do: valor

# BIEN — usar variables intermedias en el cuerpo cuando sea necesario
def procesar(%{a: seccion_a} = datos) do
  %{b: %{c: %{d: valor}}} = seccion_a
  if valor > 0, do: valor, else: {:error, :valor_negativo}
end
```

---

## Verification

```bash
mix compile
mix test

# En iex:
iex -S mix
```

```elixir
# Smoke tests en iex:

# Exercise 1
Lexer.tokenize("1 + 2")
tokens = Lexer.tokenize("(3 + 4) * 2")
Lexer.merge_numbers(tokens)
Lexer.validate_parens(tokens)

# Exercise 2
sem = Semaforo.nuevo()
sem = Semaforo.transition(sem, :tick)
sem = Semaforo.transition(sem, :emergencia)
{final, log} = Semaforo.simular(Semaforo.nuevo(), 6)
final.ciclos  # 2

# Exercise 3
# (ver verificación del ejercicio arriba)
```

---

## Summary

El pattern matching avanzado en Elixir es mucho más que simple desestructuración: los guards permiten expresar condiciones complejas de forma declarativa, el operador pin `^` habilita comparaciones contra valores ya conocidos, y el matching anidado en mapas, structs y listas permite extraer datos profundamente estructurados en una sola expresión. El orden correcto de las cláusulas multi-función es crítico: siempre de lo más específico a lo más general, dejando los catch-all al final.

## What's Next

**10. Comprehensions Avanzadas** — `for` con múltiples generators, filtros, `:into`, `:uniq`, y map comprehensions.

## Resources

- [Elixir Docs — Pattern Matching](https://elixir-lang.org/getting-started/pattern-matching.html)
- [Elixir Docs — Guards](https://hexdocs.pm/elixir/guards.html)
- [Kernel — guard functions](https://hexdocs.pm/elixir/Kernel.html)
- [Elixir School — Pattern Matching](https://elixirschool.com/en/lessons/basics/pattern_matching)
