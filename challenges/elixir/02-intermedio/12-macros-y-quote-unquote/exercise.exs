# =============================================================================
# Ejercicio 12: Macros y Quote/Unquote — Metaprogramming Básico
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Funciones y módulos en Elixir
# - if/unless básicos
# - Comprensión básica de AST (Abstract Syntax Tree)
# - Cómo Elixir compila código (compilación en dos fases)

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Definir macros simples con defmacro
# 2. Usar quote do...end para capturar código como AST
# 3. Usar unquote/1 para "escapar" e inyectar valores en el AST
# 4. Entender la hygiene de macros en Elixir
# 5. Inspeccionar el AST de expresiones simples

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# ¿QUÉ ES UNA MACRO?
# Una macro es una función que opera sobre código (AST) en tiempo de
# compilación y retorna nuevo código (AST) para ser inyectado.
#
# Diferencia con funciones:
# - Función: recibe VALORES, retorna VALORES, se ejecuta en runtime
# - Macro:   recibe AST,    retorna AST,    se ejecuta en compile time
#
# AST EN ELIXIR:
# Todo el código Elixir es representado como tuplas anidadas:
#   {función, metadata, argumentos}
#
#   1 + 2   →  {:+, [line: 1], [1, 2]}
#   if x do  →  {:if, [], [{:x, [], nil}, [do: ...]]}
#
# QUOTE:
#   quote do
#     1 + 2
#   end
#   # => {:+, [context: Elixir, imports: [...]], [1, 2]}
#
# `quote do` captura el código como AST sin ejecutarlo.
#
# UNQUOTE:
# Dentro de un bloque quote, unquote/1 "escapa" para inyectar un valor:
#
#   x = 42
#   quote do
#     IO.puts(unquote(x))   # inyecta el valor 42 en el AST
#     IO.puts(x)            # inyecta el AST del símbolo :x (variable)
#   end
#
# HYGIENE:
# Las macros de Elixir son "higiénicas" por defecto: las variables
# definidas DENTRO de la macro no "contaminan" el scope del caller.
#
#   defmacro my_macro do
#     quote do
#       x = 99    # esta `x` no afecta a la `x` del código que llama la macro
#     end
#   end
#
# Para "romper" la hygiene (raro), usa: var!(x)
#
# DEFINIR UNA MACRO:
#
#   defmodule MyMacros do
#     defmacro my_macro(arg) do
#       quote do
#         # código que se inyecta en el call site
#         IO.puts("called with #{inspect(unquote(arg))}")
#       end
#     end
#   end
#
#   require MyMacros   # OBLIGATORIO antes de usar macros de otro módulo
#   MyMacros.my_macro(:hello)

# =============================================================================
# Exercise 1: unless macro — inverso de if
# =============================================================================
#
# El `unless` ya existe en Elixir, pero lo vamos a reimplementar como ejercicio.
# Define la macro `my_unless` en el módulo MyMacros:
#
#   my_unless(condition, do: body)
#
# Que se expande a:
#
#   if !condition do
#     body
#   end
#
# Ejemplo de uso:
#   require MyMacros
#   my_unless(false, do: IO.puts("esto se imprime"))  # imprime
#   my_unless(true,  do: IO.puts("esto NO se imprime"))  # no imprime
#
# Tip: en quote, usa `if !unquote(condition), do: unquote(body)`

defmodule MyMacros do
  # TODO: Define my_unless como macro
  #
  # defmacro my_unless(condition, do: body) do
  #   quote do
  #     if !unquote(condition) do
  #       unquote(body)
  #     end
  #   end
  # end

  # ==========================================================================
  # Exercise 2: times macro — ejecutar bloque N veces
  # ==========================================================================
  #
  # Define la macro `times` que ejecuta el bloque dado `n` veces.
  #
  #   times 3 do
  #     IO.puts("hola")
  #   end
  #   # imprime "hola" tres veces
  #
  # Tip: expande a `Enum.each(1..unquote(n), fn _ -> unquote(body) end)`
  #
  # La macro recibe dos argumentos: n y [do: body]

  # TODO: Define la macro times
  #
  # defmacro times(n, do: body) do
  #   quote do
  #     Enum.each(1..unquote(n), fn _ ->
  #       unquote(body)
  #     end)
  #   end
  # end

  # ==========================================================================
  # Exercise 3: log_if_slow macro — medir tiempo de ejecución
  # ==========================================================================
  #
  # Define la macro `log_if_slow(threshold_ms, do: body)`.
  # Mide el tiempo que tarda en ejecutarse `body`.
  # Si tarda más de `threshold_ms` ms, imprime un warning.
  # Siempre retorna el resultado de `body`.
  #
  # Expande a algo como:
  #   {us, result} = :timer.tc(fn -> body end)
  #   ms = us / 1000
  #   if ms > threshold_ms, do: IO.puts("SLOW: #{ms}ms")
  #   result
  #
  # Tip: usa :timer.tc/1 que retorna {microseconds, result}

  # TODO: Define la macro log_if_slow
  #
  # defmacro log_if_slow(threshold_ms, do: body) do
  #   quote do
  #     {us, result} = :timer.tc(fn -> unquote(body) end)
  #     ms = us / 1000
  #     if ms > unquote(threshold_ms) do
  #       IO.puts("⚠️  SLOW: #{ms}ms (límite: #{unquote(threshold_ms)}ms)")
  #     end
  #     result
  #   end
  # end

  # ==========================================================================
  # Exercise 4: Inspeccionar AST — ver la representación interna
  # ==========================================================================
  #
  # Define la función (no macro) `show_ast/1` que recibe código como AST
  # (ya cotizado) y lo imprime con IO.inspect.
  #
  # Esta función te ayudará a entender cómo Elixir representa el código.
  # Úsala con `quote do ... end` como argumento.
  #
  # Tip: simplemente retorna el ast que recibe (quote ya cotizó el código).

  def show_ast(ast) do
    # TODO: IO.inspect(ast, label: "AST") y retornar ast
  end

  # ==========================================================================
  # Exercise 5: defmacro con unquote_splicing — lista de valores
  # ==========================================================================
  #
  # Define la macro `assert_all_equal(values, expected)` que verifica que
  # todos los elementos de `values` sean iguales a `expected`.
  # Expande a una secuencia de assertions individuales.
  #
  # assert_all_equal([1 + 1, 4 - 2, 8 / 4], 2)
  # # expande a algo como:
  # #   if 1 + 1 != 2, do: raise "Falló: expected 2, got #{1+1}"
  # #   if 4 - 2 != 2, do: raise "Falló: expected 2, got #{4-2}"
  # #   if 8 / 4 != 2, do: raise "Falló: expected 2, got #{8/4}"
  #
  # Tip: usa Enum.map sobre los values para generar AST de cada check,
  # luego usa quote do ... end con unquote_splicing para inyectar la lista.

  defmacro assert_all_equal(values, expected) do
    # TODO: Genera los checks individuales con Enum.map
    # checks = Enum.map(values, fn val ->
    #   quote do
    #     result = unquote(val)
    #     if result != unquote(expected) do
    #       raise "Falló: expected #{unquote(expected)}, got #{result}"
    #     end
    #   end
    # end)
    #
    # Luego combínalos:
    # quote do
    #   unquote_splicing(checks)
    # end
  end
end

# =============================================================================
# Demostración de AST — NO modifiques este bloque
# =============================================================================

defmodule ASTExplorer do
  def show_examples do
    IO.puts("\n--- AST de expresiones Elixir ---\n")

    ast1 = quote do 1 + 2 end
    IO.puts("  `1 + 2`  →  #{inspect(ast1)}")

    ast2 = quote do x = 42 end
    IO.puts("  `x = 42` →  #{inspect(ast2)}")

    ast3 = quote do if true, do: :yes, else: :no end
    IO.puts("  `if true, do: :yes, else: :no`")
    IO.puts("           →  #{inspect(ast3)}")

    ast4 = quote do [1, 2, 3] end
    IO.puts("  `[1, 2, 3]` →  #{inspect(ast4)}")

    IO.puts("")
  end
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule MacroTests do
  require MyMacros

  def run do
    IO.puts("\n=== Verificación: Macros y Quote/Unquote ===\n")

    # Ejercicio 1: my_unless
    IO.puts("  Ejercicio 1 — my_unless:")
    result1 = capture_io(fn ->
      MyMacros.my_unless(false, do: IO.puts("unless false: se ejecuta"))
      MyMacros.my_unless(true,  do: IO.puts("unless true: NO se ejecuta"))
    end)
    check("my_unless(false) ejecuta el body",  String.contains?(result1, "unless false: se ejecuta"), true)
    check("my_unless(true) no ejecuta el body", String.contains?(result1, "unless true"), false)

    IO.puts("")

    # Ejercicio 2: times
    IO.puts("  Ejercicio 2 — times:")
    counter = :ets.new(:counter, [:set, :public])
    :ets.insert(counter, {:count, 0})

    MyMacros.times 4 do
      [{:count, n}] = :ets.lookup(counter, :count)
      :ets.insert(counter, {:count, n + 1})
    end

    [{:count, final}] = :ets.lookup(counter, :count)
    :ets.delete(counter)
    check("times 4 ejecuta el bloque exactamente 4 veces", final, 4)

    IO.puts("")

    # Ejercicio 3: log_if_slow
    IO.puts("  Ejercicio 3 — log_if_slow:")
    result3 = MyMacros.log_if_slow(1000) do
      Process.sleep(0)  # sin sleep real para no alargar el test
      :fast_result
    end
    check("log_if_slow retorna el resultado del bloque", result3, :fast_result)

    IO.puts("")

    # Ejercicio 4: show_ast
    IO.puts("  Ejercicio 4 — show_ast:")
    ast = quote do 1 + 2 end
    returned_ast = MyMacros.show_ast(ast)
    check("show_ast retorna el ast sin modificar", returned_ast, ast)

    IO.puts("")

    # Ejercicio 5: assert_all_equal
    IO.puts("  Ejercicio 5 — assert_all_equal:")
    try do
      MyMacros.assert_all_equal([1 + 1, 4 - 2, 8 / 4], 2)
      check("assert_all_equal no lanza para valores correctos", true, true)
    rescue
      e -> check("assert_all_equal no lanza para valores correctos", false, true)
    end

    try do
      MyMacros.assert_all_equal([1 + 1, 1 + 3], 2)
      check("assert_all_equal lanza para valor incorrecto", false, true)
    rescue
      RuntimeError -> check("assert_all_equal lanza para valor incorrecto", true, true)
    end

    IO.puts("")
    ASTExplorer.show_examples()
    IO.puts("=== Verificación completada ===")
  end

  defp capture_io(fun) do
    ExUnit.CaptureIO.capture_io(fun)
  rescue
    _ ->
      # Fallback si no está disponible ExUnit.CaptureIO
      fun.()
      "(captura no disponible fuera de ExUnit)"
  end

  defp check(label, actual, expected) do
    if actual == expected do
      IO.puts("  ✓ #{label}")
    else
      IO.puts("  ✗ #{label}")
      IO.puts("    Esperado: #{inspect(expected)}")
      IO.puts("    Obtenido: #{inspect(actual)}")
    end
  end
end

# =============================================================================
# Common Mistakes
# =============================================================================
#
# ERROR 1: Olvidar `require` antes de usar una macro de otro módulo
#
#   MyMacros.my_unless(false, do: :ok)
#   # => (CompileError) you must require MyMacros before invoking...
#
#   Solución: añadir `require MyMacros` antes de usar la macro.
#   Las macros deben estar disponibles en tiempo de compilación.
#
# ERROR 2: Usar unquote fuera de quote
#
#   defmacro bad(x) do
#     IO.puts(unquote(x))   # Error: unquote solo válido dentro de quote
#   end
#
#   Solución: mover el unquote dentro de un bloque quote do ... end
#
# ERROR 3: Confundir el momento de ejecución
#
#   defmacro my_macro(x) do
#     IO.puts("compile time: #{inspect(x)}")  # se ejecuta al compilar
#     quote do
#       IO.puts("runtime: #{inspect(unquote(x))}")  # se ejecuta en runtime
#     end
#   end
#
# ERROR 4: Variable hygiene no esperada
#
#   defmacro set_x do
#     quote do x = 42 end   # esta `x` NO es la del caller scope
#   end
#   set_x()
#   IO.puts(x)  # CompileError: x no definida
#
#   Solución: usar var!(x) dentro de quote para romper hygiene.
#
# ERROR 5: Macros no son funciones de primera clase
#
#   Enum.map([1, 2, 3], &MyMacros.my_unless/2)  # Error — las macros
#   # no se pueden pasar como referencias de función (&)

# =============================================================================
# Summary
# =============================================================================
#
# - defmacro define código que se ejecuta en compile time
# - quote captura código como AST (tuplas anidadas)
# - unquote inyecta un valor/AST dentro de un bloque quote
# - Las macros son higiénicas: no contaminan el scope del caller
# - require es obligatorio antes de usar macros de otro módulo
# - Usa macros solo cuando una función no es suficiente (DRY por convención)

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 13: ETS básico — almacenamiento en memoria compartida
# - Explorar: Macro.expand/2 para ver qué genera una macro
# - Explorar: __using__ y use MyModule para macros de "setup"
# - Explorar: DSLs con macros (como Ecto.Schema, Phoenix.Router)

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/macros.html
# - https://hexdocs.pm/elixir/Macro.html
# - Metaprogramming Elixir (Chris McCord) — el libro definitivo sobre el tema

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Implementa la macro `assert_raises` que verifica que una expresión
# lanza una excepción específica:
#
#   assert_raises(ArgumentError) do
#     String.to_integer("no_es_numero")
#   end
#   # pasa sin error
#
#   assert_raises(ArgumentError) do
#     1 + 1
#   end
#   # lanza RuntimeError: "Expected ArgumentError but nothing was raised"
#
# La macro debe expandirse a un try/rescue que:
# 1. Ejecuta el bloque
# 2. Si lanza la excepción esperada → retorna :ok
# 3. Si NO lanza nada → raise "Expected #{exception_module} but nothing was raised"
# 4. Si lanza una excepción DIFERENTE → re-raise la excepción original
#
# Tip: rescue e in ^ExceptionModule -> :ok  (usa pin en rescue)

MacroTests.run()
