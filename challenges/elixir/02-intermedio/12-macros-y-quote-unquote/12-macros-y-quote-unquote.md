# 12 — Macros y Quote/Unquote

## Prerequisites

- Módulos y funciones en Elixir (def, defmodule)
- Pattern matching y estructuras de datos
- Comprensión básica del proceso de compilación
- Ejercicio 09: Pattern Matching Avanzado

---

## Learning Objectives

Al terminar este ejercicio serás capaz de:

1. Entender que el compilador de Elixir opera sobre AST (Abstract Syntax Tree)
2. Usar `quote/1` para capturar código como estructura de datos
3. Usar `unquote/1` para inyectar valores dentro de un bloque `quote`
4. Escribir macros con `defmacro` que expanden código en tiempo de compilación
5. Aplicar hygiene de macros para evitar colisiones de variables
6. Inspeccionar y depurar expansiones con `Macro.expand/2` y `Macro.to_string/1`

---

## Concepts

### El AST de Elixir

Todo código Elixir es transformado a una estructura de datos antes de compilarse. Esa estructura es el AST (Abstract Syntax Tree), representado como tuplas de 3 elementos:

```elixir
# Forma general del AST:
# {nombre_atom, metadata, argumentos}

# Puedes ver el AST de cualquier expresión con quote/1:
iex> quote do: 1 + 2
{:+, [context: Elixir, imports: [{1, Kernel}, {2, Kernel}]], [1, 2]}

iex> quote do: x = foo(42)
{:=, [],
 [{:x, [], Elixir},
  {:foo, [], [42]}]}

iex> quote do: if true, do: :yes, else: :no
{:if, [context: Elixir, imports: [{2, Kernel}]],
 [true, [do: :yes, else: :no]]}
```

Los literales (números, strings, átomos, listas) se representan a sí mismos:

```elixir
iex> quote do: 42
42

iex> quote do: :hello
:hello

iex> quote do: [1, 2, 3]
[1, 2, 3]
```

### unquote/1 — Inyectar valores en el AST

Dentro de un bloque `quote`, todo permanece sin evaluar. `unquote/1` abre un "escape hatch" para inyectar un valor calculado en tiempo de compilación:

```elixir
defmodule Demo do
  def build_ast(n) do
    quote do
      # Sin unquote, 'n' sería una variable del AST, no su valor
      IO.puts("El valor es: #{unquote(n)}")
    end
  end
end

# El AST generado embebe el valor 42, no la variable n
Demo.build_ast(42)
# => {:__block__, [], [{:., [], [IO, :puts]}, [], ["El valor es: 42"]]}
```

### defmacro — Macros como transformaciones de AST

Una macro recibe AST como argumentos y devuelve AST. El compilador reemplaza la llamada a la macro con el AST devuelto:

```elixir
defmodule MyMacros do
  # 'condition' llega como AST, no como valor evaluado
  defmacro my_if(condition, do: then_block, else: else_block) do
    quote do
      case unquote(condition) do
        true  -> unquote(then_block)
        false -> unquote(else_block)
        _     -> unquote(else_block)
      end
    end
  end
end

defmodule Usage do
  require MyMacros

  def demo do
    MyMacros.my_if 1 == 1 do
      IO.puts("verdadero")
    else
      IO.puts("falso")
    end
  end
end
```

### Hygiene de macros

Las macros en Elixir son higiénicas por defecto: las variables introducidas dentro de un `quote` no colisionan con las del contexto que llama a la macro.

```elixir
defmodule Hygienic do
  defmacro set_x do
    quote do
      # Esta 'x' vive en el contexto de la macro, no del caller
      x = 999
    end
  end
end

defmodule Caller do
  require Hygienic

  def demo do
    x = 1
    Hygienic.set_x()
    # x sigue siendo 1 — la hygiene protege el contexto del caller
    IO.puts(x)
  end
end
```

Para escapar la hygiene intencionalmente, usa `var!/2`:

```elixir
defmacro set_caller_x(value) do
  quote do
    # var! inyecta en el contexto del CALLER
    var!(x) = unquote(value)
  end
end
```

### Inspeccionar expansiones

```elixir
# Ver el AST expandido de una macro
ast = quote do
  unless false, do: :ok
end
Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts()

# En iex, también puedes usar:
# expand-once vs expand completo
Macro.expand_once(ast, __ENV__)
```

### unquote_splicing/1 — Inyectar listas

```elixir
defmacro call_with(fun, args) do
  quote do
    # unquote_splicing "aplana" la lista de args como argumentos individuales
    unquote(fun)(unquote_splicing(args))
  end
end
```

---

## Exercises

### Ejercicio 1 — Macro `unless` desde cero

Implementa la macro `unless/2` que ejecuta el bloque solo cuando la condición es **falsa**. Elixir ya tiene `unless` en el kernel, así que la tuya vivirá en su propio módulo.

```elixir
# Archivo: lib/my_control.ex

defmodule MyControl do
  @moduledoc """
  Estructuras de control personalizadas implementadas como macros.
  """

  @doc """
  Ejecuta `do_block` solo si `condition` evalúa a false o nil.

  ## Ejemplo

      iex> require MyControl
      iex> MyControl.unless 1 == 2, do: "ejecutado"
      "ejecutado"

      iex> MyControl.unless 1 == 1, do: "no ejecutado"
      nil
  """
  defmacro unless(condition, do: do_block) do
    # TODO: Implementar usando quote/unquote
    # Pista: if !condition, do: do_block
    # Debes retornar un AST que evalúe la condición negada
    #
    # Recuerda: condition llega como AST. Usa unquote(condition) para inyectarlo.
  end

  @doc """
  Versión de unless con rama else.

      iex> require MyControl
      iex> MyControl.unless false, do: :no_es_falso, else: :es_falso
      :no_es_falso
  """
  defmacro unless(condition, do: do_block, else: else_block) do
    # TODO: Implementar la versión con else
    # Cuando condition es falsa -> do_block
    # Cuando condition es verdadera -> else_block
  end
end
```

```elixir
# Archivo: test/my_control_test.exs

defmodule MyControlTest do
  use ExUnit.Case
  require MyControl

  describe "unless/2" do
    test "ejecuta el bloque cuando la condicion es false" do
      resultado = MyControl.unless false, do: :ejecutado
      assert resultado == :ejecutado
    end

    test "ejecuta el bloque cuando la condicion es nil" do
      resultado = MyControl.unless nil, do: :ejecutado
      assert resultado == :ejecutado
    end

    test "retorna nil cuando la condicion es verdadera" do
      resultado = MyControl.unless true, do: :no_ejecutado
      assert resultado == nil
    end

    test "la rama else se ejecuta cuando la condicion es verdadera" do
      resultado = MyControl.unless true, do: :falso, else: :verdadero
      assert resultado == :verdadero
    end

    test "funciona con expresiones complejas" do
      x = 5
      resultado = MyControl.unless x > 10, do: :menor, else: :mayor
      assert resultado == :menor
    end

    test "hygiene: no contamina variables del caller" do
      condition = false
      MyControl.unless condition, do: :ok
      # condition debe seguir siendo false en este scope
      assert condition == false
    end
  end
end
```

---

### Ejercicio 2 — Macro `assert_equal` con inspección del AST

Implementa `assert_equal/2`, una macro que compara dos valores y, cuando fallan, muestra no solo los valores sino también el **código fuente** de cada expresión. Esto es lo que hace ExUnit internamente.

```elixir
# Archivo: lib/my_assert.ex

defmodule MyAssert do
  @moduledoc """
  Utilidades de aserción con información de contexto enriquecida.
  """

  @doc """
  Compara left y right. Si no son iguales, lanza un error mostrando
  el código fuente de cada expresión y sus valores evaluados.

  ## Ejemplo de error esperado:

      Assertion failed!
      left:  1 + 1          => 2
      right: 3              => 3

  """
  defmacro assert_equal(left, right) do
    # TODO: Capturar el código fuente de left y right como strings
    # Pista: Macro.to_string(left) convierte el AST a string legible
    #        Recuerda que `left` y `right` son AST en este punto
    #
    # TODO: Generar código que:
    #   1. Evalúe left_value = <expresion left>
    #   2. Evalúe right_value = <expresion right>
    #   3. Si left_value != right_value, llame a __raise_error__/4
    #      pasando left_str, right_str, left_value, right_value
    #
    # Estructura esperada del quote:
    #
    # quote do
    #   left_val = unquote(left)
    #   right_val = unquote(right)
    #   if left_val != right_val do
    #     MyAssert.__raise_error__(
    #       unquote(left_str),
    #       unquote(right_str),
    #       left_val,
    #       right_val
    #     )
    #   end
    # end
  end

  @doc false
  # Función pública solo para ser llamada desde el AST expandido
  def __raise_error__(left_str, right_str, left_val, right_val) do
    # TODO: Construir un mensaje de error descriptivo con el formato:
    #
    #   Assertion failed!
    #   left:  #{left_str} => #{inspect(left_val)}
    #   right: #{right_str} => #{inspect(right_val)}
    #
    # Lanzar con raise/1 (RuntimeError)
  end
end
```

```elixir
# Archivo: test/my_assert_test.exs

defmodule MyAssertTest do
  use ExUnit.Case
  require MyAssert

  describe "assert_equal/2" do
    test "no lanza cuando los valores son iguales" do
      assert MyAssert.assert_equal(1 + 1, 2) == nil
    end

    test "no lanza con strings iguales" do
      assert MyAssert.assert_equal("hello", "hello") == nil
    end

    test "lanza RuntimeError cuando los valores difieren" do
      assert_raise RuntimeError, fn ->
        MyAssert.assert_equal(1 + 1, 3)
      end
    end

    test "el mensaje de error incluye el codigo fuente de left" do
      error =
        assert_raise RuntimeError, fn ->
          MyAssert.assert_equal(1 + 1, 3)
        end

      assert error.message =~ "1 + 1"
    end

    test "el mensaje de error incluye el codigo fuente de right" do
      error =
        assert_raise RuntimeError, fn ->
          MyAssert.assert_equal(1 + 1, 3)
        end

      assert error.message =~ "3"
    end

    test "el mensaje de error incluye los valores evaluados" do
      error =
        assert_raise RuntimeError, fn ->
          MyAssert.assert_equal(1 + 1, 3)
        end

      assert error.message =~ "2"
    end

    test "funciona con expresiones de variable" do
      x = 10
      assert_raise RuntimeError, fn ->
        MyAssert.assert_equal(x * 2, 25)
      end
    end
  end
end
```

---

### Ejercicio 3 — Macro `retry` con backoff exponencial

Implementa `retry/2`, una macro que reintenta un bloque de código cuando lanza una excepción, con espera exponencial entre intentos.

```elixir
# Archivo: lib/retry.ex

defmodule Retry do
  @moduledoc """
  Macro para reintentar operaciones que pueden fallar transitoriamente.
  """

  @doc """
  Reintenta `do_block` hasta `max_attempts` veces.
  Entre intentos espera `base_delay_ms * 2^intento` milisegundos.

  Opciones:
  - `max_attempts:` (requerido) — número máximo de intentos
  - `base_delay_ms:` (opcional, default 100) — delay base en ms

  ## Ejemplo de uso

      result = Retry.retry max_attempts: 3, base_delay_ms: 50 do
        # Operación que puede fallar
        HTTPoison.get!("https://api.example.com/data")
      end

  Si todos los intentos fallan, relanza la última excepción.
  """
  defmacro retry(opts, do: block) do
    # TODO: Extraer max_attempts y base_delay_ms de opts en tiempo de compilación
    # Pista: Keyword.fetch!(opts, :max_attempts) — esto ocurre en la macro
    #        Keyword.get(opts, :base_delay_ms, 100)
    #
    # TODO: Generar código equivalente a:
    #
    #   Retry.__do_retry__(
    #     fn -> <block> end,
    #     <max_attempts>,
    #     <base_delay_ms>
    #   )
    #
    # El bloque debe envolverse en un fn -> ... end para ser pasado
    # como función anónima a __do_retry__/3
  end

  @doc false
  def __do_retry__(fun, max_attempts, base_delay_ms, attempt \\ 1) do
    # TODO: Implementar la lógica de reintento:
    #
    # 1. Llamar fun.()
    # 2. Si tiene éxito, retornar el resultado
    # 3. Si lanza una excepción:
    #    a. Si attempt >= max_attempts, relanzar la excepción
    #    b. Si no, calcular delay = base_delay_ms * 2^(attempt - 1)
    #       esperar con Process.sleep(delay)
    #       y llamar recursivamente con attempt + 1
    #
    # Usa try/rescue para capturar cualquier excepción
    # y `reraise exception, __STACKTRACE__` para relanzar
  end
end
```

```elixir
# Archivo: test/retry_test.exs

defmodule RetryTest do
  use ExUnit.Case
  require Retry

  describe "retry/2" do
    test "retorna el resultado cuando el bloque tiene exito en el primer intento" do
      result = Retry.retry max_attempts: 3 do
        :ok
      end

      assert result == :ok
    end

    test "reintenta y tiene exito en el segundo intento" do
      # Usamos un Agent para rastrear cuantas veces se llamo al bloque
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      result =
        Retry.retry max_attempts: 3, base_delay_ms: 1 do
          count = Agent.get_and_update(counter, fn n -> {n + 1, n + 1} end)

          if count < 2 do
            raise "fallo transitorio"
          else
            :exito_en_intento_2
          end
        end

      assert result == :exito_en_intento_2
      Agent.stop(counter)
    end

    test "relanza la excepcion cuando se agotan los intentos" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      assert_raise RuntimeError, "siempre fallo", fn ->
        Retry.retry max_attempts: 3, base_delay_ms: 1 do
          Agent.update(counter, &(&1 + 1))
          raise "siempre fallo"
        end
      end

      # Verificar que se intentó exactamente 3 veces
      assert Agent.get(counter, & &1) == 3
      Agent.stop(counter)
    end

    test "el bloque se ejecuta exactamente max_attempts veces al fallar siempre" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      assert_raise RuntimeError, fn ->
        Retry.retry max_attempts: 5, base_delay_ms: 1 do
          Agent.update(counter, &(&1 + 1))
          raise "error"
        end
      end

      assert Agent.get(counter, & &1) == 5
      Agent.stop(counter)
    end

    test "funciona con cualquier tipo de excepcion" do
      assert_raise ArgumentError, fn ->
        Retry.retry max_attempts: 2, base_delay_ms: 1 do
          raise ArgumentError, "argumento invalido"
        end
      end
    end
  end
end
```

---

## Common Mistakes

### 1. Olvidar `require` antes de usar una macro

```elixir
# INCORRECTO — las macros se expanden en tiempo de compilación
# y el compilador necesita saber sobre el módulo antes de procesar el archivo
defmodule Usage do
  def demo do
    MyMacros.unless false, do: :ok   # CompileError: cannot invoke unless/2 ...
  end
end

# CORRECTO
defmodule Usage do
  require MyMacros

  def demo do
    MyMacros.unless false, do: :ok
  end
end
```

### 2. Usar valores en lugar de AST como argumentos de macro

```elixir
# INCORRECTO — intentar evaluar el argumento dentro de la macro
defmacro broken(condition, do: block) do
  if condition do   # ERROR: condition es AST, no un valor booleano
    quote do: unquote(block)
  end
end

# CORRECTO — trabajar siempre con AST y dejar la evaluación para el runtime
defmacro correct(condition, do: block) do
  quote do
    if unquote(condition) do
      unquote(block)
    end
  end
end
```

### 3. Colisión de variables por falta de hygiene

```elixir
# PELIGROSO — si el caller tiene una variable 'result', se sobreescribe
defmacro dangerous(expr) do
  quote do
    result = unquote(expr)   # 'result' no es higiénica con var!
    result * 2
  end
end

# SEGURO — sin var!, Elixir crea un nombre único para 'result' en la macro
defmacro safe(expr) do
  quote do
    result = unquote(expr)   # Esto ES higiénico por defecto
    result * 2
  end
end
# Nota: en el ejemplo safe, result está protegida automáticamente.
# Solo hay problema si usas var!(result) explícitamente.
```

### 4. Llamar a funciones que no existen en tiempo de compilación

```elixir
# Las macros se ejecutan en tiempo de compilación.
# Solo puedes llamar a funciones que ya están compiladas.
defmacro broken do
  result = SomeModule.some_function()  # SomeModule podría no estar compilado aún
  quote do: unquote(result)
end
```

### 5. No distinguir entre `Macro.expand` y `Macro.expand_once`

```elixir
# expand_once expande solo un nivel (útil para depurar paso a paso)
ast = quote do: unless false, do: :ok
Macro.expand_once(ast, __ENV__)
# => {:if, [], [{:!, [], [false]}, [do: :ok]]}

# expand expande recursivamente hasta que no haya más macros
Macro.expand(ast, __ENV__)
# Expansión completa — puede ser muy verbosa
```

---

## Verification

```bash
# Crear estructura del proyecto si no existe
mix new macro_exercises --module MacroExercises
cd macro_exercises

# Copiar los archivos lib/ y test/ según los ejercicios

# Ejecutar todos los tests
mix test

# Ejecutar tests de un ejercicio específico
mix test test/my_control_test.exs
mix test test/my_assert_test.exs
mix test test/retry_test.exs

# Ver el AST expandido de una macro en iex
iex -S mix
iex> require MyControl
iex> ast = quote do: MyControl.unless(false, do: :ok)
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts()

# Inspeccionar el AST de expresiones arbitrarias
iex> quote do: 1 + 2 * 3
iex> quote do: defmodule Foo, do: def bar, do: :baz
```

Salida esperada al pasar todos los tests:

```
Finished in 0.05 seconds
14 tests, 0 failures
```

---

## Summary

Las macros son transformaciones de AST que ocurren en **tiempo de compilación**. Los puntos clave:

| Concepto | Descripción |
|---|---|
| `quote do ... end` | Captura código como estructura de datos (AST) |
| `unquote(expr)` | Inyecta un valor calculado dentro de un bloque quote |
| `defmacro name(args)` | Define una macro que recibe y retorna AST |
| `require Module` | Necesario para usar macros de otro módulo |
| Hygiene | Las variables de la macro no colisionan con las del caller |
| `var!(name)` | Escapa la hygiene para acceder/modificar variables del caller |
| `Macro.expand/2` | Expande macros recursivamente para inspección |
| `Macro.to_string/1` | Convierte AST a código legible |

Las macros son poderosas pero deben usarse con moderación. La regla general: si algo puede implementarse como función, impleméntalo como función. Usa macros solo cuando necesites operar sobre código (AST) en lugar de valores.

---

## What's Next

- **Ejercicio 13 — ETS Básico**: Almacenamiento en memoria compartida entre procesos, base de los sistemas de caché en Elixir.
- **Ejercicio 16 — Testing con ExUnit**: Cómo ExUnit usa macros internamente para `assert`, `refute`, y `describe`.
- Explora `Kernel.use/2` y cómo los macros `__using__/1` inyectan comportamiento en módulos.
- Estudia `Macro.postwalk/2` y `Macro.prewalk/2` para transformar AST de forma estructurada.

---

## Resources

- [Elixir Docs — Macros](https://hexdocs.pm/elixir/macros.html)
- [Elixir Docs — Macro module](https://hexdocs.pm/elixir/Macro.html)
- [Elixir Getting Started — Meta-programming](https://elixir-lang.org/getting-started/meta/macros.html)
- [Metaprogramming Elixir — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) (libro de referencia)
- [Elixir Forum — Macro hygiene explained](https://elixirforum.com/t/understanding-macro-hygiene/1234)
