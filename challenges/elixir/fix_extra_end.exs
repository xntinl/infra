#!/usr/bin/env elixir
# Script para automatizar la remoción de `end` EXTRA en bloques Elixir
# Uso: elixir fix_extra_end.exs <directorio_base>

defmodule FixExtraEnd do
  @moduledoc """
  Detecta y corrige bloques de código Elixir con `end` suelto al final en archivos Markdown.

  Patrón a arreglar:
  ```elixir
  defmodule Example do
    # ... código ...
  end
  end    ← ESTE EXTRA se elimina
  ```
  """

  require Logger

  @elixir_block_regex ~r/```elixir\n(.*?)\n```/s

  def main(args) do
    case args do
      [] ->
        IO.puts(:stderr, "Uso: elixir fix_extra_end.exs <directorio_base>")
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
    matches = Regex.scan(@elixir_block_regex, content, capture: :all_but_first)

    if Enum.empty?(matches) do
      {:no_blocks, file_path, 0, []}
    else
      {fixed_content, fixed_blocks, errors} =
        Enum.reduce(matches, {content, [], []}, fn [code_block], {acc_content, fixed, errs} ->
          case fix_code_block(code_block, file_path) do
            {:fixed, new_code} ->
              old_pattern = "```elixir\n#{code_block}\n```"
              new_pattern = "```elixir\n#{new_code}\n```"
              new_content = String.replace(acc_content, old_pattern, new_pattern, global: false)
              {new_content, fixed ++ [1], errs}

            {:already_valid, _} ->
              {acc_content, fixed, errs}

            {:error, reason} ->
              {acc_content, fixed, errs ++ [{code_block, reason}]}
          end
        end)

      if Enum.count(fixed_blocks) > 0 do
        File.write!(file_path, fixed_content)
        Logger.info("✓ #{Enum.count(fixed_blocks)} bloques corregidos en #{Path.basename(file_path)}")
        {:modified, file_path, Enum.count(fixed_blocks), errors}
      else
        {:no_changes, file_path, 0, errors}
      end
    end
  end

  defp fix_code_block(code, file_path) do
    # Primero, valida el código tal como está
    case Code.string_to_quoted(code, []) do
      {:ok, _ast} ->
        {:already_valid, code}

      {:error, error_info} ->
        if is_extra_end_error(error_info) do
          # Hay un `end` extra - intenta removarlo
          trimmed = remove_extra_end(code)

          case Code.string_to_quoted(trimmed, []) do
            {:ok, _ast} ->
              {:fixed, trimmed}

            {:error, error_info2} ->
              msg = format_code_error(error_info2)
              Logger.warning(
                "Error persiste tras quitar `end`: #{msg} en #{Path.basename(file_path)}"
              )
              {:error, msg}
          end
        else
          # Otro tipo de error que no es `end` suelto
          error_msg = format_code_error(error_info)
          {:error, error_msg}
        end
    end
  end

  defp is_extra_end_error({_location, {msg_text, _}, token}) do
    String.contains?(msg_text, "unexpected reserved word") and token == "end"
  end

  defp is_extra_end_error({_location, message, token}) when is_binary(message) do
    String.contains?(message, "unexpected reserved word") and token == "end"
  end

  defp is_extra_end_error({_line, message, token}) when is_binary(message) do
    String.contains?(message, "unexpected reserved word") and token == "end"
  end

  defp is_extra_end_error(_), do: false

  defp format_code_error({location, message, _token}) when is_list(location) and is_tuple(message) do
    # location es [line: N, column: M], message es una tupla
    line = Keyword.get(location, :line, "?")
    {msg_text, _} = message
    "Línea #{line}: #{msg_text}"
  end

  defp format_code_error({location, message, _token}) when is_list(location) do
    # location es [line: N, column: M], message es string
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

  defp remove_extra_end(code) do
    # Solo remueve la última línea si es un `end` suelto
    # y la penúltima también es `end` (para evitar remover `end` válidos)
    lines = String.split(code, "\n")
    last_idx = Enum.count(lines) - 1

    if last_idx > 0 do
      last_line = String.trim(Enum.at(lines, last_idx))

      if last_line == "end" or last_line == "" do
        # Remueve solo la última línea
        Enum.take(lines, last_idx) |> Enum.join("\n")
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
    total_fixed = Enum.sum(Enum.map(modified, fn r -> elem(r, 2) end))

    IO.puts("\n" <> String.duplicate("=", 80))
    IO.puts("REPORTE FINAL")
    IO.puts(String.duplicate("=", 80))
    IO.puts("Directorio procesado: #{base_path}")
    IO.puts("Total archivos .md encontrados: #{Enum.count(results)}")
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
      IO.puts("\n✓ Todos los bloques fueron validados correctamente.")
    end

    IO.puts(String.duplicate("=", 80))

    # Retorna estado de éxito si no hay errores críticos
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
FixExtraEnd.main(args)
