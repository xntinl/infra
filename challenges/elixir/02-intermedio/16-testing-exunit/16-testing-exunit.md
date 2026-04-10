# 16. Testing Robusto con ExUnit

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de módulos y funciones en Elixir
- Pattern matching y estructuras de datos
- Familiaridad básica con Mix y proyectos Elixir

## Learning Objectives
After completing this exercise, you will be able to:
- Escribir tests básicos con `test` y `assert`/`refute`
- Organizar tests en `describe` blocks para mayor claridad
- Usar `setup` y `setup_all` para preparar datos de prueba
- Verificar excepciones con `assert_raise`
- Integrar doctests directamente en la documentación del módulo

## Concepts

### ExUnit: El Framework de Testing de Elixir

ExUnit es el framework de testing incluido en la librería estándar de Elixir. No necesitas instalar nada extra — está disponible en todo proyecto Mix. La convención es un archivo `_test.exs` por cada módulo, ubicado en `test/`.

```elixir
# test/calculator_test.exs
defmodule CalculatorTest do
  use ExUnit.Case  # Importa macros de testing

  # Cada test es una función nombrada con la macro test/2
  test "adds two positive numbers" do
    result = Calculator.add(2, 3)
    assert result == 5
  end

  test "handles negative numbers" do
    assert Calculator.add(-1, 1) == 0
  end
end
```

```bash
$ mix test
..
Finished in 0.05 seconds (0.05s on load, 0.00s async, 0.00s sync)
2 tests, 0 failures
```

### assert y refute

`assert` falla el test si la expresión es falsy. `refute` falla si la expresión es truthy. Son inversos.

```elixir
# assert — falla si es false o nil
assert 1 + 1 == 2          # ok
assert "hello" =~ "ell"    # ok — regex match
assert {:ok, _} = result   # ok — pattern match con assert
assert is_list([1, 2, 3])  # ok — verifica tipo

# refute — falla si es true
refute 1 == 2              # ok
refute is_nil("value")     # ok
refute Enum.empty?([1])    # ok

# assert con mensaje personalizado
assert length(list) > 0, "Expected non-empty list, got: #{inspect(list)}"

# assert_in_delta para floats (evita problemas de precisión)
assert_in_delta 3.14, :math.pi(), 0.01
```

### describe: Agrupando Tests Relacionados

`describe` agrupa tests que comparten contexto o prueban el mismo comportamiento desde diferentes ángulos. Mejora la legibilidad y el output del runner.

```elixir
defmodule UserTest do
  use ExUnit.Case

  describe "create_user/3" do
    test "creates user with valid data" do
      {:ok, user} = User.create("Alice", 30, "alice@example.com")
      assert user.name == "Alice"
    end

    test "returns error with empty name" do
      assert {:error, _} = User.create("", 30, "alice@example.com")
    end

    test "returns error with invalid age" do
      assert {:error, _} = User.create("Alice", -1, "alice@example.com")
    end
  end

  describe "format_user/1" do
    test "formats user as string" do
      user = %{name: "Bob", age: 25}
      assert User.format(user) == "Bob (25)"
    end
  end
end
```

### setup y setup_all: Preparación de Datos

`setup` se ejecuta antes de CADA test. `setup_all` se ejecuta UNA VEZ antes de todos los tests del módulo. Retornan un mapa que se convierte en el contexto del test.

```elixir
defmodule OrderTest do
  use ExUnit.Case

  # setup_all: datos que no cambian entre tests
  setup_all do
    config = %{tax_rate: 0.19, currency: "USD"}
    {:ok, config: config}  # disponible en context como context.config
  end

  # setup: datos frescos para cada test
  setup do
    order = %{items: [], total: 0, status: :pending}
    {:ok, order: order}
  end

  # El segundo argumento de test es el contexto
  test "adds item to order", %{order: order} do
    updated = Order.add_item(order, %{name: "Widget", price: 9.99})
    assert length(updated.items) == 1
  end

  test "calculates total with tax", %{order: order, config: config} do
    order = Order.add_item(order, %{name: "Widget", price: 100.0})
    total = Order.total_with_tax(order, config.tax_rate)
    assert_in_delta total, 119.0, 0.01
  end
end
```

### assert_raise: Verificando Excepciones

Cuando el comportamiento esperado es una excepción, usa `assert_raise`. Es más expresivo que un rescue manual.

```elixir
defmodule SafeMathTest do
  use ExUnit.Case

  test "raises on division by zero" do
    # assert_raise ExceptionType, fn -> expresión_que_lanza end
    assert_raise ArithmeticError, fn ->
      SafeMath.divide(10, 0)
    end
  end

  test "raises with specific message" do
    # También puede verificar el mensaje del error
    assert_raise ArgumentError, ~r/cannot be negative/, fn ->
      SafeMath.sqrt(-1)
    end
  end

  # catch_error para inspeccionar la excepción
  test "error contains useful context" do
    error = assert_raise RuntimeError, fn ->
      SafeMath.process(nil)
    end
    assert error.message =~ "nil is not supported"
  end
end
```

### Doctests: Tests en la Documentación

Los doctests convierten los ejemplos en `@doc` en tests ejecutables. Esto garantiza que la documentación siempre esté sincronizada con el comportamiento real.

```elixir
defmodule StringUtils do
  @doc """
  Capitaliza la primera letra de cada palabra.

  ## Examples

      iex> StringUtils.title_case("hello world")
      "Hello World"

      iex> StringUtils.title_case("")
      ""

      iex> StringUtils.title_case("already CAPS")
      "Already Caps"

  """
  def title_case(str) do
    str
    |> String.split()
    |> Enum.map(&String.capitalize/1)
    |> Enum.join(" ")
  end
end

# En el test file:
defmodule StringUtilsTest do
  use ExUnit.Case, async: true
  doctest StringUtils  # Ejecuta todos los iex> ejemplos como tests
end
```

## Exercises

### Exercise 1: Basic Tests con assert y refute

Completa los tests vacíos para el módulo `StringHelper`. Los TODO indican qué debes escribir.

```elixir
# lib/string_helper.ex
defmodule StringHelper do
  def palindrome?(str), do: str == String.reverse(str)
  def word_count(str), do: str |> String.split() |> length()
  def truncate(str, max) when byte_size(str) <= max, do: str
  def truncate(str, max), do: String.slice(str, 0, max) <> "..."
end

# test/string_helper_test.exs
defmodule StringHelperTest do
  use ExUnit.Case

  # TODO: Escribe un test que verifique que "racecar" es palíndromo
  # Nombre sugerido: "racecar is a palindrome"

  # TODO: Escribe un test que verifique que "hello" NO es palíndromo
  # Usa refute en lugar de assert

  # TODO: Escribe un test para word_count/1 con "hello world elixir"
  # El resultado esperado es 3

  # TODO: Escribe un test para truncate/2 con string corto que no se trunca
  # "hi" con max 10 debe retornar "hi" sin cambios

  # TODO: Escribe un test para truncate/2 que sí trunca
  # "hello world" con max 5 debe retornar "hello..."
end
```

Expected output:
```bash
$ mix test test/string_helper_test.exs
.....
Finished in 0.02 seconds
5 tests, 0 failures
```

---

### Exercise 2: describe Blocks — Organización

Organiza los tests de `BankAccount` en `describe` blocks lógicos.

```elixir
# lib/bank_account.ex
defmodule BankAccount do
  def new(owner), do: %{owner: owner, balance: 0.0}
  def deposit(%{balance: b} = acc, amount) when amount > 0, do: %{acc | balance: b + amount}
  def deposit(_, amount), do: {:error, "Amount must be positive, got: #{amount}"}
  def withdraw(%{balance: b} = acc, amount) when amount > 0 and amount <= b, do: %{acc | balance: b - amount}
  def withdraw(%{balance: b}, amount) when amount > b, do: {:error, :insufficient_funds}
  def withdraw(_, amount), do: {:error, "Invalid amount: #{amount}"}
  def balance(%{balance: b}), do: b
end

# test/bank_account_test.exs
defmodule BankAccountTest do
  use ExUnit.Case

  # TODO: Crea un describe block para "new/1" con estos tests:
  # - "creates account with owner name"
  # - "starts with zero balance"

  # TODO: Crea un describe block para "deposit/2" con estos tests:
  # - "increases balance by deposited amount"
  # - "returns error for negative amount"
  # - "returns error for zero amount"

  # TODO: Crea un describe block para "withdraw/2" con estos tests:
  # - "decreases balance by withdrawn amount"
  # - "returns error when insufficient funds"
  # - "returns error for negative amount"
end
```

Expected output:
```bash
$ mix test test/bank_account_test.exs --trace
BankAccountTest
  new/1
    creates account with owner name (5ms)
    starts with zero balance (0ms)
  deposit/2
    increases balance by deposited amount (0ms)
    returns error for negative amount (0ms)
    returns error for zero amount (0ms)
  withdraw/2
    decreases balance by withdrawn amount (0ms)
    returns error when insufficient funds (0ms)
    returns error for negative amount (0ms)

Finished in 0.02 seconds
8 tests, 0 failures
```

---

### Exercise 3: setup — Datos Compartidos entre Tests

Usa `setup` para crear los datos de prueba una sola vez por test, y `setup_all` para datos inmutables compartidos.

```elixir
# lib/product_catalog.ex
defmodule ProductCatalog do
  def new(), do: []
  def add(catalog, product), do: [product | catalog]
  def find_by_id(catalog, id), do: Enum.find(catalog, &(&1.id == id))
  def filter_by_category(catalog, category), do: Enum.filter(catalog, &(&1.category == category))
  def count(catalog), do: length(catalog)
end

# test/product_catalog_test.exs
defmodule ProductCatalogTest do
  use ExUnit.Case

  # TODO: Agrega setup_all que defina una lista de 3 productos de muestra:
  # - %{id: 1, name: "Widget", category: :electronics, price: 29.99}
  # - %{id: 2, name: "Gadget", category: :electronics, price: 49.99}
  # - %{id: 3, name: "Book", category: :books, price: 14.99}
  # Retorna {:ok, products: productos} para que estén en el contexto

  # TODO: Agrega setup que cree un catálogo fresco con los productos del setup_all
  # Usa context.products para acceder a los productos definidos en setup_all
  # Retorna {:ok, catalog: catalog} para que el catálogo esté en el contexto

  # TODO: Escribe test "finds product by id" usando %{catalog: catalog} del contexto
  # find_by_id con id: 1 debe retornar el Widget

  # TODO: Escribe test "returns nil for missing product" usando el contexto
  # find_by_id con id: 999 debe retornar nil

  # TODO: Escribe test "filters by category" usando el contexto
  # filter_by_category con :electronics debe retornar 2 productos

  # TODO: Escribe test "counts total products" usando el contexto
  # El catálogo con 3 productos debe tener count == 3
end
```

Expected output:
```bash
$ mix test test/product_catalog_test.exs
....
Finished in 0.02 seconds
4 tests, 0 failures
```

---

### Exercise 4: assert_raise — Verificando Excepciones

Prueba que las funciones lanzan las excepciones correctas en condiciones inválidas.

```elixir
# lib/strict_math.ex
defmodule StrictMath do
  def divide(_, 0), do: raise ArithmeticError, "Cannot divide by zero"
  def divide(a, b), do: a / b

  def sqrt(n) when n < 0, do: raise ArgumentError, "Cannot compute sqrt of negative: #{n}"
  def sqrt(n), do: :math.sqrt(n)

  def factorial(n) when n < 0, do: raise ArgumentError, "Factorial undefined for negative: #{n}"
  def factorial(0), do: 1
  def factorial(n), do: n * factorial(n - 1)

  def parse_positive!(str) do
    case Integer.parse(str) do
      {n, ""} when n > 0 -> n
      {n, ""} -> raise ArgumentError, "Expected positive integer, got: #{n}"
      _ -> raise ArgumentError, "Cannot parse '#{str}' as integer"
    end
  end
end

# test/strict_math_test.exs
defmodule StrictMathTest do
  use ExUnit.Case

  # TODO: Escribe test que verifique que divide/2 con divisor 0 lanza ArithmeticError

  # TODO: Escribe test que verifique que sqrt/1 con número negativo lanza ArgumentError

  # TODO: Escribe test que capture el error de factorial(-1) y verifique
  # que el mensaje del error contiene "negative"
  # Hint: error = assert_raise ArgumentError, fn -> ... end
  #        assert error.message =~ "negative"

  # TODO: Escribe test que verifique que parse_positive!/1 con "abc" lanza ArgumentError

  # TODO: Escribe test positivo (sin excepción) que verifique que divide/2
  # con números válidos retorna el resultado correcto
end
```

Expected output:
```bash
$ mix test test/strict_math_test.exs
.....
Finished in 0.01 seconds
5 tests, 0 failures
```

---

### Exercise 5: Doctests — Tests en la Documentación

Agrega ejemplos `iex>` al `@doc` de cada función y habilita los doctests en el test file.

```elixir
# lib/temperature.ex
defmodule Temperature do
  @moduledoc "Conversiones de temperatura entre unidades."

  @doc """
  Convierte Celsius a Fahrenheit.

  ## Examples

      # TODO: Agrega ejemplo iex> para Temperature.celsius_to_fahrenheit(0)
      # El resultado es 32.0

      # TODO: Agrega ejemplo iex> para Temperature.celsius_to_fahrenheit(100)
      # El resultado es 212.0

  """
  def celsius_to_fahrenheit(c), do: c * 9 / 5 + 32

  @doc """
  Convierte Fahrenheit a Celsius.

  ## Examples

      # TODO: Agrega ejemplo iex> para Temperature.fahrenheit_to_celsius(32)
      # El resultado es 0.0

      # TODO: Agrega ejemplo iex> para Temperature.fahrenheit_to_celsius(212)
      # El resultado es 100.0

  """
  def fahrenheit_to_celsius(f), do: (f - 32) * 5 / 9

  @doc """
  Clasifica la temperatura en palabras.

  ## Examples

      # TODO: Agrega 3 ejemplos: freezing (< 0), comfortable (15-25), hot (> 30)

  """
  def classify(c) when c < 0, do: :freezing
  def classify(c) when c < 10, do: :cold
  def classify(c) when c < 20, do: :cool
  def classify(c) when c <= 25, do: :comfortable
  def classify(c) when c <= 35, do: :warm
  def classify(_), do: :hot
end

# test/temperature_test.exs
defmodule TemperatureTest do
  use ExUnit.Case

  # TODO: Agrega doctest Temperature para ejecutar todos los iex> ejemplos
  # Hint: doctest NombreDelModulo

  # Puedes agregar tests adicionales aquí si quieres
end
```

Expected output:
```bash
$ mix test test/temperature_test.exs
.......
Finished in 0.02 seconds
7 doctests, 0 failures
```

---

## Try It Yourself

Escribe una test suite completa para una calculadora. Sin solución incluida — diseña los casos de prueba tú mismo.

```elixir
# lib/calculator.ex
defmodule Calculator do
  def add(a, b), do: a + b
  def subtract(a, b), do: a - b
  def multiply(a, b), do: a * b
  def divide(_, 0), do: {:error, :division_by_zero}
  def divide(a, b), do: {:ok, a / b}
  def power(base, 0), do: 1
  def power(base, exp) when exp > 0, do: base * power(base, exp - 1)
  def power(_, exp), do: {:error, "Negative exponent not supported: #{exp}"}
end

# test/calculator_test.exs
defmodule CalculatorTest do
  use ExUnit.Case

  # Diseña la test suite completa incluyendo:
  # - describe blocks para cada operación
  # - setup con valores de prueba comunes
  # - Tests de casos normales (happy path)
  # - Tests de edge cases (cero, negativos, flotantes)
  # - assert_raise o manejo de {:error, _} para división por cero
  # - Al menos 15 tests en total
  # - doctest Calculator si agregas @doc con ejemplos en el módulo
end
```

**Objetivo**: La suite debe cubrir todos los casos de `Calculator`. Ejecuta `mix test` con `--cover` para ver el porcentaje de cobertura.

---

## Common Mistakes

### Mistake 1: No usar describe para agrupar tests relacionados

**Wrong:**
```elixir
test "add positive" do ...end
test "add negative" do ...end
test "add zero" do ...end
test "subtract positive" do ...end
```
**Why:** Sin `describe`, el output del runner no agrupa visualmente los tests. En suites grandes, es difícil ver qué función tiene failures.
**Fix:**
```elixir
describe "add/2" do
  test "with positive numbers" do ...end
  test "with negative numbers" do ...end
  test "with zero" do ...end
end
```

### Mistake 2: setup retorna datos sin el wrapper {:ok, ...}

**Wrong:**
```elixir
setup do
  user = %{name: "Alice"}
  user  # Retorna el mapa directamente — ExUnit lo ignora
end

test "uses user", %{user: user} do  # user es nil aquí
  assert user.name == "Alice"
end
```
**Error:** `** (KeyError) key :user not found in context`
**Fix:**
```elixir
setup do
  user = %{name: "Alice"}
  {:ok, user: user}  # Siempre retornar {:ok, keyword_list}
end
```

### Mistake 3: Doctests con output incorrecto

**Wrong:**
```elixir
@doc """
## Examples

    iex> MyModule.greet("Alice")
    Hello, Alice!        # Sin comillas — incorrecto
"""
```
**Error:** `doctest failed: expected Hello, Alice! but got "Hello, Alice!"`
**Fix:**
```elixir
@doc """
## Examples

    iex> MyModule.greet("Alice")
    "Hello, Alice!"      # El output debe ser la representación Elixir exacta
"""
```

---

## Verification

```bash
# Correr todos los tests del proyecto
$ mix test

# Correr con output verbose
$ mix test --trace

# Ver cobertura de código
$ mix test --cover

# Correr solo un archivo específico
$ mix test test/calculator_test.exs

# Correr un test específico por línea
$ mix test test/calculator_test.exs:15
```

## Summary
- **Key concepts**: `test`, `describe`, `setup`, `setup_all`, `assert`, `refute`, `assert_raise`, `doctest`
- **What you practiced**: Escribir tests con contexto compartido, organizar tests en grupos lógicos, verificar excepciones, sincronizar documentación con tests
- **Important to remember**: `setup` retorna `{:ok, keyword_list}`. Los doctests usan la representación exacta de Elixir en el output. `assert_raise` es más expresivo que un rescue manual.

## What's Next
En el siguiente ejercicio **17-debugging-io-inspect** aprenderás técnicas de debugging para desarrollo y producción — desde `IO.inspect` hasta `dbg/1` y Observer.

## Resources
- [ExUnit Documentation](https://hexdocs.pm/ex_unit/ExUnit.html)
- [ExUnit.Case](https://hexdocs.pm/ex_unit/ExUnit.Case.html)
- [Doctests Guide](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [mix test Reference](https://hexdocs.pm/mix/Mix.Tasks.Test.html)
