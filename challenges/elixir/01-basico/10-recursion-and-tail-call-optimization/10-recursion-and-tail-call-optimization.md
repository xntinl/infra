# 10. Recursion and Tail Call Optimization

**Difficulty**: Basico

## Prerequisites

- Haber completado los ejercicios 01–09
- Conocimiento de pattern matching (05), listas y `[H|T]` (06), funciones y arity (08)
- Un proyecto Mix con `mix new` o IEx para pruebas interactivas

## Learning Objectives

- Entender la estructura de toda función recursiva: base case + recursive case
- Reconocer por qué la recursión naive puede causar stack overflow en listas grandes
- Escribir funciones con Tail Call Optimization (TCO) usando el patrón acumulador
- Identificar cuándo una llamada es tail call y cuándo no lo es
- Entender por qué Elixir no tiene loops `for`/`while` clásicos (y qué usa en su lugar)

## Concepts

### Recursión: base case + recursive case

Toda función recursiva tiene dos partes obligatorias:

```elixir
defmodule MyList do
  # Base case: la lista está vacía — retorna 0 directamente
  def sum([]), do: 0

  # Recursive case: tomar el head, sumar al resultado de la cola
  def sum([head | tail]), do: head + sum(tail)
end

MyList.sum([1, 2, 3])
# sum([1, 2, 3])
# = 1 + sum([2, 3])
# = 1 + 2 + sum([3])
# = 1 + 2 + 3 + sum([])
# = 1 + 2 + 3 + 0
# = 6
```

Sin base case, la recursión nunca termina y el proceso muere por stack overflow.

### Por qué hay stack overflow: la pila crece

En recursión naive, **cada llamada espera que la anterior retorne** para completar su cálculo. Esto apila frames en el call stack:

```
sum([1, 2, 3])                    <- frame 1, esperando sum([2,3])
  1 + sum([2, 3])                 <- frame 2, esperando sum([3])
      2 + sum([3])                <- frame 3, esperando sum([])
          3 + sum([])             <- frame 4 — ya puede retornar
```

Con una lista de 1.000.000 de elementos, habría 1.000.000 de frames apilados. Esto causa:

```
** (SystemLimitError) a system limit has been reached
```

### Tail Call Optimization (TCO): la VM de Erlang/BEAM lo resuelve

Una llamada es **tail call** cuando es la **última operación** de la función — no hay nada más que calcular después. La BEAM puede reusar el frame del stack actual en lugar de crear uno nuevo.

```elixir
# NOT tail call: después de la llamada recursiva, aún hay que hacer + head
def sum([head | tail]), do: head + sum(tail)
#                              ^^^^^^^^^^^^^
#                          La suma ocurre DESPUÉS — no es tail call

# Tail call: la llamada recursiva ES la última operación
def sum_tail([], acc), do: acc
def sum_tail([head | tail], acc), do: sum_tail(tail, head + acc)
#                                     ^^^^^^^^^^^^^^^^^^^^^^^^^^^
#                                 Esta es la ÚLTIMA operación — es tail call
```

### El patrón Acumulador

Para convertir recursión naive en TCO, se agrega un argumento extra que acumula el resultado:

```elixir
defmodule Recursive do
  # Versión pública con interfaz limpia — default arg
  def sum(list, acc \\ 0)

  # Base case: retornar el acumulador
  def sum([], acc), do: acc

  # Recursive case: sumar head al acumulador, continuar con tail
  def sum([head | tail], acc), do: sum(tail, head + acc)
end

Recursive.sum([1, 2, 3])
# sum([1,2,3], 0)
# -> sum([2,3], 1)   -- acc = 0 + 1 = 1
# -> sum([3], 3)     -- acc = 1 + 2 = 3
# -> sum([], 6)      -- acc = 3 + 3 = 6
# -> 6
```

Cada llamada no espera a la anterior — la VM puede reutilizar el mismo frame. **Sin límite de profundidad**.

### Por qué Elixir no tiene loops clásicos

Elixir no tiene `for x in list: ...` ni `while condition: ...`. La razón: los procesos de Erlang no tienen estado mutable. Una variable asignada no puede reasignarse en un loop.

En su lugar:
- **`Enum.map/filter/reduce`**: para transformaciones de colecciones (el 95% de los casos)
- **Recursión**: para lógica compleja que no encaja en Enum
- **`for` comprehension**: para transformaciones con filtros y generadores (no es un loop clásico)

```elixir
# En otros lenguajes
result = []
for x in [1, 2, 3]:
    result.append(x * 2)

# En Elixir — usar Enum.map
result = Enum.map([1, 2, 3], fn x -> x * 2 end)
# [2, 4, 6]
```

## Exercises

### Exercise 1: Suma recursiva simple

```elixir
defmodule MyMath do
  # Base case
  def sum([]), do: 0

  # Recursive case: head + sum del resto
  def sum([head | tail]) do
    head + sum(tail)
  end
end

MyMath.sum([])          # 0
MyMath.sum([5])         # 5
MyMath.sum([1, 2, 3])   # 6
MyMath.sum([10, 20, 30, 40])  # 100
```

**Expected output:**

```
iex> MyMath.sum([])
0
iex> MyMath.sum([5])
5
iex> MyMath.sum([1, 2, 3])
6
iex> MyMath.sum([10, 20, 30, 40])
100
```

**Traza mental:**
```
sum([1, 2, 3])
= 1 + sum([2, 3])
= 1 + (2 + sum([3]))
= 1 + (2 + (3 + sum([])))
= 1 + (2 + (3 + 0))
= 6
```

---

### Exercise 2: Longitud recursiva

```elixir
defmodule MyList do
  # Base case: lista vacía tiene longitud 0
  def my_length([]), do: 0

  # Recursive case: 1 (por el head) + longitud del tail
  def my_length([_ | tail]) do
    1 + my_length(tail)
  end
end

MyList.my_length([])           # 0
MyList.my_length([1])          # 1
MyList.my_length([1, 2, 3])    # 3
MyList.my_length([:a, :b, :c, :d, :e])  # 5

# Verificar contra la función estándar
length([1, 2, 3]) == MyList.my_length([1, 2, 3])  # true
```

**Expected output:**

```
iex> MyList.my_length([])
0
iex> MyList.my_length([1, 2, 3])
3
iex> MyList.my_length([:a, :b, :c, :d, :e])
5
iex> length([1, 2, 3]) == MyList.my_length([1, 2, 3])
true
```

---

### Exercise 3: Factorial naive (NO tail-call)

```elixir
defmodule NaiveMath do
  # Base case
  def factorial(0), do: 1

  # Recursive case: n * factorial(n-1)
  # NOT tail call: la multiplicación ocurre DESPUÉS de que factorial retorna
  def factorial(n) when n > 0 do
    n * factorial(n - 1)
  end
end

NaiveMath.factorial(0)    # 1
NaiveMath.factorial(1)    # 1
NaiveMath.factorial(5)    # 120
NaiveMath.factorial(10)   # 3628800

# Con N muy grande eventualmente causaría stack overflow
# (Para factorials, el número crece tan rápido que el problema de precisión llega antes)
```

**Expected output:**

```
iex> NaiveMath.factorial(0)
1
iex> NaiveMath.factorial(1)
1
iex> NaiveMath.factorial(5)
120
iex> NaiveMath.factorial(10)
3628800
```

**Por qué NO es tail call:**
```
factorial(3)
= 3 * factorial(2)     <- espera que factorial(2) retorne para multiplicar
    = 2 * factorial(1) <- espera que factorial(1) retorne para multiplicar
        = 1 * factorial(0) <- espera que factorial(0) retorne
            = 1
```

---

### Exercise 4: Factorial con TCO — patrón acumulador

```elixir
defmodule TCOMath do
  # Interfaz pública limpia — el acumulador es un detalle de implementación
  def factorial(n), do: factorial(n, 1)

  # Base case: cuando n llega a 0, el acumulador tiene el resultado
  defp factorial(0, acc), do: acc

  # Tail call: la ÚLTIMA operación es la llamada recursiva — no hay nada después
  defp factorial(n, acc) when n > 0 do
    factorial(n - 1, n * acc)
  end
end

TCOMath.factorial(0)    # 1
TCOMath.factorial(5)    # 120
TCOMath.factorial(10)   # 3628800
TCOMath.factorial(20)   # 2432902008176640000
```

**Expected output:**

```
iex> TCOMath.factorial(0)
1
iex> TCOMath.factorial(5)
120
iex> TCOMath.factorial(10)
3628800
iex> TCOMath.factorial(20)
2432902008176640000
```

**Traza con acumulador — sin crecimiento de pila:**
```
factorial(3, 1)    -- acc = 1
factorial(2, 3)    -- acc = 1 * 3 = 3
factorial(1, 6)    -- acc = 3 * 2 = 6
factorial(0, 6)    -- base case: retorna 6
```

---

### Exercise 5: Suma con TCO — patrón acumulador

```elixir
defmodule TCOList do
  # Interfaz pública con default arg — acc es invisible para el usuario
  def sum(list, acc \\ 0)

  # Base case: lista vacía — retornar el acumulador
  def sum([], acc), do: acc

  # Tail call: mover head al acumulador, continuar con tail
  # La última operación es sum/2 — TCO aplica
  def sum([head | tail], acc), do: sum(tail, head + acc)
end

TCOList.sum([])              # 0
TCOList.sum([1, 2, 3])       # 6
TCOList.sum(Enum.to_list(1..1_000_000))  # 500000500000 — sin stack overflow
```

**Expected output:**

```
iex> TCOList.sum([])
0
iex> TCOList.sum([1, 2, 3])
6
iex> TCOList.sum(Enum.to_list(1..100))
5050
iex> TCOList.sum(Enum.to_list(1..1_000_000))
500000500000
```

**Diferencia clave:**
```
# Naive: crece la pila
sum([1,2,3]) = 1 + (2 + (3 + 0))  <- 4 frames en pila

# TCO: la pila no crece
sum([1,2,3], 0)
-> sum([2,3], 1)    <- reusa el frame
-> sum([3], 3)      <- reusa el frame
-> sum([], 6)       <- reusa el frame
-> 6
```

---

### Exercise 6: Map recursivo — construir lista con prepend + reverse

```elixir
defmodule MyEnum do
  # Base case: lista vacía — retorna lista vacía
  def my_map([], _func), do: []

  # Recursive case: aplicar func al head, prepend al resultado de la cola
  def my_map([head | tail], func) do
    [func.(head) | my_map(tail, func)]
  end

  # Versión con TCO — patrón acumulador + reverse al final
  def my_map_tco(list, func), do: my_map_tco(list, func, [])

  defp my_map_tco([], _func, acc), do: Enum.reverse(acc)
  defp my_map_tco([head | tail], func, acc) do
    my_map_tco(tail, func, [func.(head) | acc])
  end
end

MyEnum.my_map([1, 2, 3], fn x -> x * 2 end)
# [2, 4, 6]

MyEnum.my_map(["hello", "world"], &String.upcase/1)
# ["HELLO", "WORLD"]

MyEnum.my_map_tco([1, 2, 3], fn x -> x * x end)
# [1, 4, 9]

# Verificar que el resultado es el mismo que Enum.map
Enum.map([1, 2, 3], fn x -> x * 2 end) == MyEnum.my_map([1, 2, 3], fn x -> x * 2 end)
# true
```

**Expected output:**

```
iex> MyEnum.my_map([1, 2, 3], fn x -> x * 2 end)
[2, 4, 6]
iex> MyEnum.my_map(["hello", "world"], &String.upcase/1)
["HELLO", "WORLD"]
iex> MyEnum.my_map_tco([1, 2, 3], fn x -> x * x end)
[1, 4, 9]
iex> Enum.map([1, 2, 3], fn x -> x * 2 end) == MyEnum.my_map([1, 2, 3], fn x -> x * 2 end)
true
```

**Por qué prepend + reverse y no append:**
```
# Append en cada paso: O(n²) — muy lento
acc ++ [new_elem]  # recorre acc completo en cada iteración

# Prepend + reverse al final: O(n) — eficiente
[new_elem | acc]   # O(1) cada vez
Enum.reverse(acc)  # O(n) una sola vez al final
```

## Common Mistakes

### Error 1: Recursión sin base case — bucle infinito

```elixir
# WRONG: sin base case, recurre para siempre hasta crash
defmodule BadRecursion do
  def count(n) do
    count(n - 1)  # nunca termina
  end
end

# FIX: siempre definir el base case primero
defmodule GoodRecursion do
  def count(0), do: 0  # base case
  def count(n) when n > 0, do: 1 + count(n - 1)
end
```

### Error 2: El acumulador no es el último argumento — TCO no aplica

```elixir
# WRONG: la llamada recursiva NO es la última operación
# n * factorial(n-1, acc) calcula factorial primero y luego multiplica
defmodule WrongTCO do
  def factorial(0, acc), do: acc
  def factorial(n, acc), do: n * factorial(n - 1, acc)  # NO es tail call
  #                          ^^^
  #                  multiplicación ocurre DESPUÉS — no es la última op
end

# FIX: mover el cálculo AL argumento, no al retorno
defmodule CorrectTCO do
  def factorial(0, acc), do: acc
  def factorial(n, acc), do: factorial(n - 1, n * acc)  # sí es tail call
  #                          ^^^^^^^^^^^^^^^^^^^^^^^^^^
  #                   factorial es LA ÚLTIMA operación
end
```

### Error 3: Construir lista con ++ en recursión — O(n²)

```elixir
# WRONG: acc ++ [transformed] es O(n) en cada iteración -> O(n²) total
defmodule SlowMap do
  def my_map([], _func, acc), do: acc
  def my_map([h | t], func, acc) do
    my_map(t, func, acc ++ [func.(h)])  # O(n²) total
  end
end

# FIX: prepend O(1) + reverse al final O(n) = O(n) total
defmodule FastMap do
  def my_map(list, func), do: my_map(list, func, [])
  defp my_map([], _func, acc), do: Enum.reverse(acc)
  defp my_map([h | t], func, acc) do
    my_map(t, func, [func.(h) | acc])  # O(1)
  end
end
```

### Error 4: Reinventar Enum innecesariamente en código de producción

```elixir
# WRONG en producción: reimplementar lo que Enum ya hace
defmodule MyApp do
  def process(list) do
    my_map(list, fn x -> x * 2 end)  # implementación propia
  end
end

# FIX: usar Enum para código de producción — más legible, más optimizado
defmodule MyApp do
  def process(list) do
    Enum.map(list, fn x -> x * 2 end)
  end
end

# La recursión manual es valiosa para:
# 1. Aprender el mecanismo
# 2. Lógica que no encaja en Enum.map/filter/reduce
# 3. Estructuras de datos propias (árboles, grafos)
```

## Verification

```bash
# Crear archivo de verificación
cat > /tmp/test_recursion.exs << 'EOF'
defmodule Verify do
  # Suma naive
  def sum([]), do: 0
  def sum([h | t]), do: h + sum(t)

  # Suma TCO
  def sum_tco(list, acc \\ 0)
  def sum_tco([], acc), do: acc
  def sum_tco([h | t], acc), do: sum_tco(t, h + acc)

  # Factorial TCO
  def factorial(n), do: factorial(n, 1)
  defp factorial(0, acc), do: acc
  defp factorial(n, acc) when n > 0, do: factorial(n - 1, n * acc)

  # Map recursivo
  def my_map([], _f), do: []
  def my_map([h | t], f), do: [f.(h) | my_map(t, f)]
end

IO.puts "sum([1..5]): #{Verify.sum([1, 2, 3, 4, 5])}"
IO.puts "sum_tco([1..5]): #{Verify.sum_tco([1, 2, 3, 4, 5])}"
IO.puts "factorial(10): #{Verify.factorial(10)}"
IO.puts "my_map x*2: #{inspect(Verify.my_map([1, 2, 3], fn x -> x * 2 end))}"

# TCO con lista grande — no causa stack overflow
big_sum = Verify.sum_tco(Enum.to_list(1..100_000))
IO.puts "sum(1..100_000): #{big_sum}"
EOF

elixir /tmp/test_recursion.exs
```

**Expected output:**

```
sum([1..5]): 15
sum_tco([1..5]): 15
factorial(10): 3628800
my_map x*2: [2, 4, 6]
sum(1..100_000): 5000050000
```

## Summary

- Toda función recursiva necesita **base case** (termina la recursión) y **recursive case** (avanza hacia el base case)
- La recursión **naive** crea un frame de pila por llamada — con listas grandes causa stack overflow
- **Tail Call Optimization (TCO)**: si la llamada recursiva es la **última operación**, la BEAM reutiliza el frame — sin límite de profundidad
- El **patrón acumulador** convierte recursión naive en TCO: mueve el cálculo al argumento en lugar de al retorno
- Para construir listas con TCO: **prepend `[x | acc]`** + **`Enum.reverse/1` al final** — O(n) total
- En código de producción, prefer `Enum.map/filter/reduce` sobre recursión manual — la recursión es para aprender el mecanismo y para casos que Enum no cubre

## What's Next

- **Módulos y estructuras avanzadas**: `defstruct` para tipos de datos propios
- **Procesos y concurrencia**: la recursión infinita con TCO es la base de los GenServers

## Resources

- [Elixir Getting Started — Recursion](https://elixir-lang.org/getting-started/recursion.html)
- [Elixir School — Recursion](https://elixirschool.com/en/lessons/basics/functions#recursion-6)
- [Erlang Efficiency Guide — Tail Recursion](https://www.erlang.org/doc/efficiency_guide/eff_guide.html)
- [Elixir Docs — Enum](https://hexdocs.pm/elixir/Enum.html)
