#!/usr/bin/env elixir
# Script para analizar error handling en archivos de 02-intermedio y 03-avanzado
# Busca patrones problemáticos de error handling

defmodule ErrorHandlingAnalyzer do
  @moduledoc """
  Analiza archivos MD para identificar:
  - Funciones que pueden fallar sin manejo
  - Operaciones de I/O sin try/rescue
  - Llamadas a Repo/GenServer/Registry sin case handling
  - Falta de documentación de errores posibles
  """

  def analyze_directory(dir_path) do
    files = find_markdown_files(dir_path)
    IO.puts("Analizando #{length(files)} archivos en #{dir_path}...")

    results = Enum.map(files, &analyze_file/1)
    |> Enum.filter(&not_empty_results/1)

    {files_with_issues, issue_count} =
      results
      |> Enum.reduce({[], 0}, fn {file, issues}, {acc_files, acc_count} ->
        {[file | acc_files], acc_count + length(issues)}
      end)

    IO.puts("\n" <> String.duplicate("=", 80))
    IO.puts("RESUMEN: #{length(files_with_issues)} archivos con problemas potenciales")
    IO.puts("Total de issues detectados: #{issue_count}")
    IO.puts(String.duplicate("=", 80) <> "\n")

    Enum.each(results, fn {file, issues} ->
      unless Enum.empty?(issues) do
        IO.puts("\nArchivo: #{file}")
        Enum.each(issues, fn issue ->
          IO.puts("  - #{issue}")
        end)
      end
    end)

    {length(files_with_issues), issue_count}
  end

  defp find_markdown_files(dir_path) do
    Path.wildcard("#{dir_path}/**/*.md")
  end

  defp not_empty_results({_file, issues}), do: not Enum.empty?(issues)

  defp analyze_file(file_path) do
    case File.read(file_path) do
      {:ok, content} ->
        issues = check_error_patterns(content, file_path)
        {file_path, issues}
      {:error, _} ->
        {file_path, []}
    end
  end

  defp check_error_patterns(content, file_path) do
    issues = []

    # Patrón 1: Llamadas a Repo.insert!/update!/delete! sin case handling
    issues = if String.contains?(content, ["Repo.insert!", "Repo.update!", "Repo.delete!"])
      and not String.contains?(content, ["Repo.insert/", "Repo.update/", "Repo.delete/", "{:ok,", "{:error,"])
    do
      issues ++ ["⚠️  Usa Repo.insert/update/delete! sin error handling"]
    else
      issues
    end

    # Patrón 2: GenServer.call sin try/rescue
    issues = if String.contains?(content, "GenServer.call")
      and not String.contains?(content, ["try do", "rescue", "case"])
    do
      issues ++ ["⚠️  Usa GenServer.call sin manejo de errores"]
    else
      issues
    end

    # Patrón 3: Llamadas a Registry sin verificación
    issues = if String.contains?(content, "Registry.lookup")
      and not String.contains?(content, ["{:ok,", "{:error,", "case"])
    do
      issues ++ ["⚠️  Registry.lookup sin verificación de resultado"]
    else
      issues
    end

    # Patrón 4: File operations sin try/rescue
    issues = if (String.contains?(content, ["File.read", "File.write", "File.read_file"])
      or String.contains?(content, ["File.dir?"]))
      and not String.contains?(content, ["try do", "case"])
    do
      issues ++ ["⚠️  File I/O sin manejo de errores"]
    else
      issues
    end

    # Patrón 5: HTTP calls (Req/HTTPoison) sin case handling
    issues = if String.contains?(content, ["Req.get", "Req.post", "HTTPoison.get", "HTTPoison.post"])
      and not String.contains?(content, ["case", "{:ok,", "{:error,"])
    do
      issues ++ ["⚠️  HTTP requests sin case handling"]
    else
      issues
    end

    # Patrón 6: Stream/Enum operations sin documentación
    issues = if String.contains?(content, ["Stream.resource", "Enum.each"])
      and not String.contains?(content, ["@doc", "can fail", "error"])
    do
      issues ++ ["⚠️  Stream/Enum sin documentación de posibles errores"]
    else
      issues
    end

    # Patrón 7: Falta de documentación de @errors o {:error, reason}
    issues = if String.contains?(content, "def ")
      and not String.contains?(content, ["@doc", "error", "reason", "Error"])
    do
      issues ++ ["⚠️  Funciones sin documentación de errores posibles"]
    else
      issues
    end

    issues
  end
end

# Main
case System.argv() do
  [dir] ->
    {files_count, issues_count} = ErrorHandlingAnalyzer.analyze_directory(dir)
    IO.puts("\n✓ Análisis completado")
    IO.puts("  Archivos con problemas: #{files_count}")
    IO.puts("  Issues totales: #{issues_count}")
  _ ->
    IO.puts("Uso: elixir analyze_error_handling.exs <directory>")
    IO.puts("Ejemplo: elixir analyze_error_handling.exs 02-intermedio")
end
