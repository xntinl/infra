#!/usr/bin/env elixir
# Script mejorado v2: Usa un enfoque de "balance tokens" más confiable
# Algoritmo:
# 1. Lee cada archivo .md en 03-avanzado/
# 2. Busca sección "## Executable Example"
# 3. Extrae el bloque ```elixir
# 4. Intenta Code.string_to_quoted/2
# 5. Si falla con "unexpected end", cuenta cuántos `end` hay después del error
# 6. Remueve `end` extras del final hasta que pase la validación
# Uso: elixir fix_extra_end_v2.exs <directorio_base>

defmodule FixExtraEndV2 do
  require Logger

  @executable_example_regex ~r/## Executable Example\n(.*?)(?=^## |\Z)/ms
  @elixir_block_regex ~r/```elixir\n(.*?)\n```/s

  def main(args) do
    case args do
      [] ->
        IO.puts(:stderr, "Uso: elixir fix_extra_end_v2.exs <directorio_base>")
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

    case Regex.run(@executable_example_regex, content, capture: :all_but_first) do
      nil ->
        {:no_example, file_path, 0, []}

      [section] ->
        case Regex.run(@elixir_block_regex, section, capture: :all_but_first) do
          nil ->
            {:no_code_block, file_path, 0, []}

          [code_block] ->
            case fix_code_block(code_block, file_path) do
              {:fixed, new_code} ->
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

  defp fix_code_block(code, _file_path) do
    # Primero, valida el código tal como está
    case Code.string_to_quoted(code, []) do
      {:ok, _ast} ->
        {:already_valid, code}

      {:error, _error_info} ->
        # Intenta remover `end` extras progresivamente
        case try_remove_ends(code, 1, 10) do
          {:ok, fixed_code} ->
            {:fixed, fixed_code}

          :max_attempts ->
            {:error, "Código no válido tras 10 intentos de remoción de 'end'"}
        end
    end
  end

  defp try_remove_ends(code, attempt, max_attempts) when attempt > max_attempts do
    :max_attempts
  end

  defp try_remove_ends(code, attempt, max_attempts) do
    fixed_code = remove_one_end(code)

    case Code.string_to_quoted(fixed_code, []) do
      {:ok, _ast} ->
        {:ok, fixed_code}

      {:error, _error_info} ->
        # Código sigue siendo inválido, intenta remover otro `end`
        if code == fixed_code do
          # No se pudo remover más `end`, falló
          :max_attempts
        else
          try_remove_ends(fixed_code, attempt + 1, max_attempts)
        end
    end
  end

  defp remove_one_end(code) do
    lines = String.split(code, "\n")
    last_idx = Enum.count(lines) - 1

    if last_idx > 0 do
      # Busca la última línea que sea `end`
      last_idx_with_end =
        last_idx..0
        |> Enum.find(fn idx ->
          String.trim(Enum.at(lines, idx)) == "end"
        end)

      if last_idx_with_end do
        List.delete_at(lines, last_idx_with_end) |> Enum.join("\n")
      else
        code
      end
    else
      code
    end
  end

  defp report_results(results, base_path) do
    modified = Enum.filter(results, fn r -> elem(r, 0) == :modified end)
    with_errors = Enum.filter(results, fn r -> elem(r, 0) == :error or Enum.count(elem(r, 3)) > 0 end)
    no_examples = Enum.count(Enum.filter(results, fn r -> elem(r, 0) == :no_example end))
    no_blocks = Enum.count(Enum.filter(results, fn r -> elem(r, 0) == :no_code_block end))
    total_fixed = Enum.sum(Enum.map(modified, fn r -> elem(r, 2) end))

    IO.puts("\n" <> String.duplicate("=", 80))
    IO.puts("REPORTE FINAL - FIX EXTRA END v2")
    IO.puts(String.duplicate("=", 80))
    IO.puts("Directorio procesado: #{base_path}")
    IO.puts("Total archivos .md encontrados: #{Enum.count(results)}")
    IO.puts("Archivos sin sección '## Executable Example': #{no_examples}")
    IO.puts("Archivos sin bloque ```elixir: #{no_blocks}")
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
      if Enum.count(modified) == 0 do
        IO.puts("\n✓ Todos los bloques ya estaban válidos.")
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

args = System.argv()
FixExtraEndV2.main(args)
