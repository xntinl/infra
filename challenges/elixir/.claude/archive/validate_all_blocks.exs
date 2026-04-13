#!/usr/bin/env elixir
# Script para validar todos los bloques de código Elixir en archivos Markdown
# Uso: elixir validate_all_blocks.exs <directorio_base>

defmodule ValidateAllBlocks do
  @moduledoc """
  Valida la sintaxis de todos los bloques ```elixir ... ``` en archivos Markdown
  usando el compilador Elixir nativo (Code.string_to_quoted/2).
  """

  require Logger

  @elixir_block_regex ~r/```elixir\n(.*?)\n```/s

  def main(args) do
    case args do
      [] ->
        IO.puts(:stderr, "Uso: elixir validate_all_blocks.exs <directorio_base>")
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
    Logger.info("Iniciando validación en: #{base_path}")

    md_files = find_md_files(base_path)

    if Enum.empty?(md_files) do
      IO.puts(:stderr, "Advertencia: No se encontraron archivos .md en #{base_path}")
      System.halt(1)
    end

    results =
      md_files
      |> Enum.with_index(1)
      |> Enum.map(fn {file, index} ->
        IO.write(
          "\r[#{index}/#{Enum.count(md_files)}] Validando: #{Path.relative_to(file, base_path)} "
        )

        validate_file(file)
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

  defp validate_file(file_path) do
    content = File.read!(file_path)
    matches = Regex.scan(@elixir_block_regex, content, capture: :all_but_first)

    if Enum.empty?(matches) do
      {:no_blocks, file_path, 0, []}
    else
      {valid_count, error_list} =
        Enum.reduce(matches, {0, []}, fn [code_block], {valid, errors} ->
          case Code.string_to_quoted(code_block, []) do
            {:ok, _ast} ->
              {valid + 1, errors}

            {:error, error_info} ->
              error_msg = format_error(error_info)
              line = extract_line_from_error(error_info)
              {valid, errors ++ [{line, error_msg}]}
          end
        end)

      total_blocks = Enum.count(matches)

      if Enum.empty?(error_list) do
        {:valid, file_path, total_blocks, []}
      else
        {:errors, file_path, valid_count, error_list}
      end
    end
  end

  defp format_error({location, reason, _token}) when is_list(location) do
    "#{inspect(reason)}"
  end

  defp format_error({line, reason, _token}) when is_integer(line) do
    "#{reason}"
  end

  defp format_error(error_info) do
    inspect(error_info)
  end

  defp extract_line_from_error({location, _reason, _token}) when is_list(location) do
    Keyword.get(location, :line, 0)
  end

  defp extract_line_from_error({line, _reason, _token}) when is_integer(line) do
    line
  end

  defp extract_line_from_error(_), do: 0

  defp report_results(results, base_path) do
    valid_files = Enum.filter(results, fn r -> elem(r, 0) == :valid end)
    error_files = Enum.filter(results, fn r -> elem(r, 0) == :errors end)
    no_block_files = Enum.filter(results, fn r -> elem(r, 0) == :no_blocks end)

    total_files = Enum.count(results)
    total_blocks = Enum.sum(Enum.map(valid_files, fn r -> elem(r, 2) end))
    total_errors = Enum.sum(Enum.map(error_files, fn r -> Enum.count(elem(r, 3)) end))

    IO.puts("\n" <> String.duplicate("=", 80))
    IO.puts("REPORTE DE VALIDACIÓN")
    IO.puts(String.duplicate("=", 80))
    IO.puts("Directorio validado: #{base_path}")
    IO.puts("Total archivos .md encontrados: #{total_files}")
    IO.puts("Archivos sin bloques elixir: #{Enum.count(no_block_files)}")
    IO.puts("Archivos con bloques válidos: #{Enum.count(valid_files)}")
    IO.puts("Total bloques válidos: #{total_blocks}")

    if !Enum.empty?(error_files) do
      IO.puts("\n✗ Archivos con ERRORES DE SINTAXIS: #{Enum.count(error_files)}")
      IO.puts("Total errores encontrados: #{total_errors}\n")

      error_files
      |> Enum.each(fn result ->
        file = elem(result, 1)
        errors = elem(result, 3)
        valid = elem(result, 2)
        total = valid + Enum.count(errors)

        IO.puts("  • #{Path.relative_to(file, base_path)} [#{valid}/#{total} válidos]")

        Enum.each(errors, fn {line, msg} ->
          IO.puts("      └─ Línea #{line}: #{msg}")
        end)
      end)
    else
      IO.puts("\n✓ ¡Todos los archivos tienen bloques válidos!")
    end

    IO.puts(String.duplicate("=", 80))

    # Retorna estado
    if Enum.empty?(error_files) do
      IO.puts("✓ Validación completada: SIN ERRORES")
      :ok
    else
      IO.puts("✗ Validación completada: #{Enum.count(error_files)} archivos con errores")
      :errors
    end
  end
end

# Obtén los argumentos de línea de comandos
args = System.argv()
ValidateAllBlocks.main(args)
