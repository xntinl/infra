#!/usr/bin/env elixir
#
# fix_tests_quality.exs (Phase 5 — Test Quality)
#
# Detecta y corrige 5 problemas comunes de calidad en tests ExUnit.
#
# P1: Tests con assertions triviales (siempre pasan)
# P2: def innecesarios en módulos de test
# P3: assert/refute con orden incorrecta
# P4: Tests sin describe (sugerencia)
# P5: Tests vacíos o stubs

require Logger

defmodule TestQualityFixer do
  @doc """
  Procesa un archivo .md buscando bloques ```elixir con tests.
  """
  def fix_file(file_path) do
    content = File.read!(file_path)
    new_content = process_markdown(content)

    if new_content != content do
      File.write!(file_path, new_content)
      Logger.info("    ✓ #{Path.relative_to_cwd(file_path)}")
      true
    else
      false
    end
  rescue
    e ->
      Logger.error("    ✗ #{Path.relative_to_cwd(file_path)}: #{inspect(e)}")
      false
  end

  @doc """
  Procesa markdown encontrando bloques ```elixir ... ``` que contienen tests.
  Solo procesa bloques que contienen: defmodule ...Test
  """
  defp process_markdown(content) do
    # Usar regex con flag 's' (DOTALL) para que . coincida con newlines
    # NO usar flag 'U' (UNGREEDY) porque hace el regex demasiado restrictivo
    # (.*?) es NON-GREEDY por defecto en Elixir
    Regex.replace(
      ~r/```elixir(.*?)```/s,
      content,
      fn _full_match, code ->
        # IMPORTANTE: Solo procesar bloques que contienen DEFMODULE ...Test
        if contains_test_module?(code) do
          fixed = apply_all_fixes(code)
          # Asegurar que el output tiene el formato correcto
          # El código capturado incluye leading \n, así que preservamos eso
          "```elixir#{fixed}```"
        else
          # No cambiar bloques que no son de test — devolver match sin cambios
          # (esto preserva los espacios originales)
          "```elixir#{code}```"
        end
      end
    )
  end

  defp contains_test_module?(code) do
    # Buscar específicamente: defmodule NOMBR ETest
    # Y asegurar que el cierre 'end' también está en el bloque
    has_test_module = String.match?(code, ~r/defmodule\s+\w+Test\b/)
    has_test_module
  end

  defp apply_all_fixes(code) do
    # Importante: solo aplicar fixes dentro del contexto de un módulo de test
    # ORDEN IMPORTANTE: aplicar P1 antes de P5 (que remueve líneas)
    code
    |> fix_p1_trivial_assertions()
    |> fix_p2_remove_def()
    |> fix_p3_assert_order()
    |> fix_p5_remove_empty_tests()
  end

  # ========================================================================
  # P2: Remover def innecesarios en módulo de test
  # ========================================================================

  defp fix_p2_remove_def(code) do
    lines = String.split(code, "\n")

    result =
      lines
      |> Enum.reduce([], fn line, acc ->
        trimmed = String.trim(line)

        case Regex.run(~r/^def\s+(\w+)/, trimmed) do
          [_, func_name] ->
            if is_allowed_test_function(func_name) do
              acc ++ [line]
            else
              # Remover (no es un callback ni helper)
              acc
            end

          _ ->
            acc ++ [line]
        end
      end)

    Enum.join(result, "\n")
  end

  defp is_allowed_test_function(func_name) do
    setup_callbacks = ["setup", "setup_all", "setup_module"]

    func_name in setup_callbacks ||
      String.starts_with?(func_name, "helper_") ||
      String.ends_with?(func_name, "_helper")
  end

  # ========================================================================
  # P3: Corregir orden de assert/refute
  # ========================================================================

  defp fix_p3_assert_order(code) do
    lines = String.split(code, "\n")

    fixed_lines =
      lines
      |> Enum.map(fn line ->
        if String.contains?(line, "assert ") or String.contains?(line, "refute ") do
          fix_single_assertion(line)
        else
          line
        end
      end)

    Enum.join(fixed_lines, "\n")
  end

  defp fix_single_assertion(line) do
    case Regex.run(~r/^(\s*)(assert|refute)\s+(true|false)\s*==\s*(.+)$/, line) do
      [_full, indent, "assert", "true", expr] ->
        indent <> "assert " <> String.trim(expr) <> " == true"

      [_full, indent, "assert", "false", expr] ->
        indent <> "refute " <> String.trim(expr)

      [_full, indent, "refute", "false", expr] ->
        indent <> "assert " <> String.trim(expr) <> " == true"

      _ ->
        line
    end
  end

  # ========================================================================
  # P5: Remover tests vacíos o stubs
  # ========================================================================

  defp fix_p5_remove_empty_tests(code) do
    lines = String.split(code, "\n")

    {fixed, _} =
      Enum.reduce(lines, {[], nil}, fn line, {acc, test_block} ->
        trimmed = String.trim(line)

        cond do
          String.starts_with?(trimmed, "test ") && String.contains?(trimmed, "do") ->
            if is_inline_empty_test(trimmed) do
              # Inline empty test: remover directamente
              {acc, test_block}
            else
              # Iniciar tracking del test
              # NO incluir line en acc aún — lo haremos cuando cerremos el test
              {acc, {:in_test, [line]}}
            end

          test_block && trimmed == "end" ->
            case test_block do
              {:in_test, test_lines} ->
                if is_empty_test_body(test_lines) do
                  # Test vacío — remover completamente
                  {acc, nil}
                else
                  # Test no vacío — incluir todo el bloque
                  {acc ++ test_lines ++ [line], nil}
                end

              _ ->
                {acc ++ [line], nil}
            end

          test_block ->
            case test_block do
              {:in_test, test_lines} ->
                {acc, {:in_test, test_lines ++ [line]}}

              _ ->
                {acc ++ [line], test_block}
            end

          true ->
            {acc ++ [line], test_block}
        end
      end)

    Enum.join(fixed, "\n")
  end

  defp is_inline_empty_test(line) do
    String.match?(line, ~r/test\s+"[^"]*"\s+(do|,\s*do:)\s*(:ok)?\s*end/)
  end

  defp is_empty_test_body(lines) do
    content_lines =
      lines
      |> Enum.drop(1)
      |> Enum.map(&String.trim/1)
      |> Enum.filter(fn l ->
        l != "" && !String.starts_with?(l, "#")
      end)

    case content_lines do
      [] -> true
      [":ok"] -> true
      _ -> false
    end
  end

  # ========================================================================
  # P1: Detectar y marcar assertions triviales
  # ========================================================================

  defp fix_p1_trivial_assertions(code) do
    lines = String.split(code, "\n")

    fixed_lines =
      lines
      |> Enum.with_index()
      |> Enum.reduce([], fn {line, idx}, acc ->
        trimmed = String.trim(line)

        if is_trivial_assertion(trimmed, lines, idx) do
          indent = get_indent(line)
          acc ++ [indent <> "# TRIVIAL: " <> trimmed]
        else
          acc ++ [line]
        end
      end)

    Enum.join(fixed_lines, "\n")
  end

  defp is_trivial_assertion(assertion, lines, idx) do
    case Regex.run(~r/^assert\s+(\w+)\s*==\s*(@\w+)/, assertion) do
      [_, var, _attr] ->
        check_trivial_assignment(var, lines, idx)

      _ ->
        case Regex.run(~r/^assert\s+(@\w+)\s*==\s*(\w+)/, assertion) do
          [_, _attr, var] ->
            check_trivial_assignment(var, lines, idx)

          _ ->
            false
        end
    end
  end

  defp check_trivial_assignment(var, lines, idx) do
    lines
    |> Enum.take(idx)
    |> Enum.reverse()
    |> Enum.take(10)
    |> Enum.any?(fn line ->
      String.match?(line, ~r/#{var}\s*=\s*@\w+/)
    end)
  end

  defp get_indent(line) do
    case Regex.run(~r/^(\s*)/, line) do
      [_full, indent] -> indent
      _ -> ""
    end
  end
end

# ========================================================================
# Main
# ========================================================================

defmodule Main do
  require Logger

  def run do
    root = "/Users/consulting/Documents/consulting/infra/challenges/elixir"

    Logger.info("Starting test quality fixes (Phase 5/5)...")
    Logger.info("Root: #{root}\n")

    levels = ["01-basico", "02-intermedio", "03-avanzado", "04-insane"]

    results =
      Enum.map(levels, fn level ->
        level_path = Path.join(root, level)

        if !File.exists?(level_path) do
          {0, 0}
        else
          process_level(level_path, level)
        end
      end)

    {fixed, skipped} = Enum.reduce(results, {0, 0}, fn {f, s}, {af, as} -> {af + f, as + s} end)

    Logger.info("\n========== SUMMARY ==========")
    Logger.info("✓ Fixed:   #{fixed}")
    Logger.info("→ Skipped: #{skipped}")
    Logger.info("Total:    #{fixed + skipped}")
  end

  defp process_level(level_path, level_name) do
    Logger.info("\n=== Level: #{level_name} ===")

    all_md_files = Path.wildcard(Path.join([level_path, "**", "*.md"]))

    fixed = Enum.count(all_md_files, &TestQualityFixer.fix_file/1)
    skipped = Enum.count(all_md_files) - fixed

    {fixed, skipped}
  end
end

Main.run()
