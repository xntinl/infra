#!/usr/bin/env elixir
# Script mejorado para automatizar la remoción de MÚLTIPLES `end` EXTRA en bloques Elixir
# Implementa el algoritmo solicitado:
# 1. Busca "## Executable Example"
# 2. Extrae bloque ```elixir
# 3. Cuenta do/end imbalance
# 4. Remueve los `end` extras del final
# 5. Re-valida con Code.string_to_quoted/2
# Uso: elixir fix_extra_end_advanced.exs <directorio_base>

defmodule FixExtraEndAdvanced do
  @moduledoc """
  Detecta y corrige bloques de código Elixir con múltiples `end` sueltos
  en secciones "## Executable Example" de archivos Markdown.

  Patrón a arreglar:
  ## Executable Example
  ```elixir
  defmodule Example do
    # ... código ...
  end
  end    ← ESTOS EXTRAS se eliminan
  end
  ```
  """

  require Logger

  @executable_example_regex ~r/## Executable Example\n(.*?)(?=^## |\Z)/ms
  @elixir_block_regex ~r/```elixir\n(.*?)\n```/s

  def main(args) do
    case args do
      [] ->
        IO.puts(:stderr, "Uso: elixir fix_extra_end_advanced.exs <directorio_base>")
        System.halt(1)

      [base_path | _] ->
        base_path
        |> Path.expand()
        |> validate_path()
        |> process_directory()
    end
  end

  defp validate_path(path) do
    if File.dir?(path) do
      path
    else
      IO.puts(:stderr, "Error: #{path} no es un directorio válido")
      System.halt(1)
    end
  end

  defp process_directory(base_path) do
    Logger.info("Iniciando búsqueda en: #{base_path}")

    md_files = find_md_files(base_path)

    if Enum.empty?(md_files) do
      IO.puts(:stderr, "Advertencia: No se encontraron archivos .md en #{base_path}")
      System.halt(1)
    end

    results =
      md_files
      |> Enum.with_index(1)
      |> Enum.map(fn {file, index} ->
        IO.write("\r[#{index}/#{Enum.count(md_files)}] Procesando: #{Path.relative_to(file, base_path)} ")
        process_file(file)
      end)

    IO.write("\r")
    IO.puts("")

    report_results(results, base_path)
  end

  defp find_md_files(base_path) do
    base_path
    |> Path.join("**/*.md")
    |> Path.wildcard()
    |> Enum.sort()
  end

  defp process_file(file_path) do
    content = File.read!(file_path)

    # Busca la sección "## Executable Example"
    case Regex.run(@executable_example_regex, content, capture: :all_but_first) do
      nil ->
        {:no_executable_example, file_path, 0, []}

      [section] ->
        # Extrae el bloque ```elixir
        case Regex.run(@elixir_block_regex, section, capture: :all_but_first) do
          nil ->
            {:no_code_block, file_path, 0, []}

          [code_block] ->
            case fix_code_block(code_block, file_path) do
              {:fixed, new_code} ->
                # Reemplaza el bloque en el contenido original
                old_pattern = "```elixir\n#{code_block}\n```"
                new_pattern = "```elixir\n#{new_code}\n```"
                new_content = String.replace(content, old_pattern, new_pattern, global: false)
                File.write!(file_path, new_content)

                Logger.info("✓ Bloque corregido en #{Path.basename(file_path)}")
                {:modified, file_path, 1, []}

              {:already_valid, _} ->
                {:no_changes, file_path, 0, []}

              {:error, reason} ->
                {:error, file_path, 0, [{code_block, reason}]}
            end
        end
    end
  end

  defp fix_code_block(code, file_path) do
    # Primero, valida el código tal como está
    case Code.string_to_quoted(code, []) do
      {:ok, _ast} ->
        {:already_valid, code}

      {:error, _error_info} ->
        # Intenta remover `end` extras basado en count
        case count_imbalance(code) do
          {:imbalanced, extra_ends} when extra_ends > 0 ->
            fixed = remove_extra_ends(code, extra_ends)

            case Code.string_to_quoted(fixed, []) do
              {:ok, _ast} ->
                {:fixed, fixed}

              {:error, error_info} ->
                msg = format_code_error(error_info)
                Logger.warning("Error persiste tras quitar #{extra_ends} `end`: #{msg} en #{Path.basename(file_path)}")
                {:error, msg}
            end

          {:imbalanced, _} ->
            {:error, "Más bloques abiertos que cerrados"}

          {:balanced, _} ->
            # Código no es válido pero está balanceado - hay otro problema
            {:error, "Código inválido pero balanceado (revisar sintaxis)"}
        end
    end
  end

  defp count_imbalance(code) do
    # Cuenta bloques que abren y cierran
    open_patterns = [
      ~r/\bdo\b/,      # do/end
      ~r/\bdefmodule\b/, # defmodule/end
      ~r/\bdefmacro\b/,  # defmacro/end
      ~r/\bdef\b/,      # def/end
      ~r/\bcase\b/,     # case/end
      ~r/\bif\b/,       # if/end (puede no tener end si es inline)
      ~r/\bcond\b/,     # cond/end
      ~r/\bwith\b/,     # with/end
      ~r/\btry\b/,      # try/end (catch/after/end)
      ~r/\bunless\b/,   # unless/end
      ~r/\bfn\b/        # fn/end (anonymous functions)
    ]

    # Solo contamos `end` explícitamente
    end_count = Regex.scan(~r/\bend\b/, code) |> Enum.count()

    # Contamos patrones de apertura
    open_count =
      open_patterns
      |> Enum.reduce(0, fn pattern, acc ->
        count = Regex.scan(pattern, code) |> Enum.count()
        acc + count
      end)

    imbalance = end_count - open_count

    if imbalance == 0 do
      {:balanced, 0}
    else
      {:imbalanced, imbalance}
    end
  end

  defp remove_extra_ends(code, extra_count) when extra_count > 0 do
    lines = String.split(code, "\n")
    total_lines = Enum.count(lines)

    # Remueve desde el final, contando cuántos `end` sueltos hay
    {remaining_lines, _} =
      Enum.reduce((total_lines - 1)..0, {lines, extra_count}, fn idx, {acc_lines, remaining} ->
        if remaining > 0 do
          line = String.trim(Enum.at(acc_lines, idx))

          if line == "end" do
            new_lines = List.delete_at(acc_lines, idx)
            {new_lines, remaining - 1}
          else
            {acc_lines, remaining}
          end
        else
          {acc_lines, remaining}
        end
      end)

    Enum.join(remaining_lines, "\n")
  end

  defp remove_extra_ends(code, _), do: code

  defp format_code_error({location, message, _token}) when is_list(location) and is_tuple(message) do
    line = Keyword.get(location, :line, "?")
    {msg_text, _} = message
    "Línea #{line}: #{msg_text}"
  end

  defp format_code_error({location, message, _token}) when is_list(location) do
    line = Keyword.get(location, :line, "?")
    msg = if is_binary(message), do: message, else: inspect(message)
    "Línea #{line}: #{msg}"
  end

  defp format_code_error({line, message, _token}) when is_integer(line) do
    msg = if is_binary(message), do: message, else: inspect(message)
    "Línea #{line}: #{msg}"
  end

  defp format_code_error(other) do
    inspect(other)
  end

  defp report_results(results, base_path) do
    modified = Enum.filter(results, fn r -> elem(r, 0) == :modified end)
    with_errors = Enum.filter(results, fn r -> elem(r, 0) == :error or Enum.count(elem(r, 3)) > 0 end)
    no_examples = Enum.filter(results, fn r -> elem(r, 0) == :no_executable_example end)
    total_fixed = Enum.sum(Enum.map(modified, fn r -> elem(r, 2) end))

    IO.puts("\n" <> String.duplicate("=", 80))
    IO.puts("REPORTE FINAL - FIX EXTRA END ADVANCED")
    IO.puts(String.duplicate("=", 80))
    IO.puts("Directorio procesado: #{base_path}")
    IO.puts("Total archivos .md encontrados: #{Enum.count(results)}")
    IO.puts("Archivos sin sección '## Executable Example': #{Enum.count(no_examples)}")
    IO.puts("Archivos modificados: #{Enum.count(modified)}")
    IO.puts("Bloques corregidos: #{total_fixed}")

    if !Enum.empty?(with_errors) do
      IO.puts("\n⚠ Archivos con errores (requieren revisión manual):")

      with_errors
      |> Enum.each(fn result ->
        file = elem(result, 1)
        errors = elem(result, 3)

        IO.puts("  • #{Path.relative_to(file, base_path)}")

        Enum.each(errors, fn {_code, reason} ->
          IO.puts("    - #{reason}")
        end)
      end)
    else
      if Enum.empty?(modified) do
        IO.puts("\n✓ Todos los bloques ya estaban válidos o no requerían corrección.")
      else
        IO.puts("\n✓ Todos los bloques fueron validados correctamente.")
      end
    end

    IO.puts(String.duplicate("=", 80))

    if Enum.empty?(with_errors) do
      IO.puts("✓ Proceso completado sin errores.")
      :ok
    else
      IO.puts("⚠ Algunos archivos requieren revisión manual.")
      :warnings
    end
  end
end

# Obtén los argumentos de línea de comandos
args = System.argv()
FixExtraEndAdvanced.main(args)
