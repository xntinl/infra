#!/usr/bin/env elixir
# Script para agregar @moduledoc y @doc automáticamente a archivos .md

defmodule DocAdder do
  @moduledoc "Automates adding @moduledoc and @doc to Elixir code blocks in markdown files"

  def run(root_dir) do
    md_files = find_md_files(root_dir)

    stats = Enum.reduce(md_files, %{files: 0, moduledocs: 0, docs: 0}, fn file, acc ->
      process_file(file, acc)
    end)

    print_summary(stats)
    save_log(stats, root_dir)
    stats
  end

  defp find_md_files(root_dir) do
    root_dir
    |> Path.expand()
    |> Stream.unfold(&next_file/1)
    |> Enum.filter(&String.ends_with?(&1, ".md"))
  end

  defp next_file(dir) do
    case File.ls(dir) do
      {:ok, files} ->
        {dir, files}
        |> expand_paths()
        |> Enum.split_with(&File.dir?/1)
        |> case do
          {dirs, files} ->
            files_with_path = Enum.map(files, &Path.join(dir, &1))
            remaining = dirs ++ []
            {files_with_path, remaining}
        end
      {:error, _} ->
        nil
    end
  end

  defp expand_paths({dir, files}) do
    Enum.map(files, &Path.join(dir, &1))
  end

  defp process_file(file, acc) do
    case File.read(file) do
      {:ok, content} ->
        {new_content, moduledocs, docs} = add_docs_to_content(content)

        if moduledocs > 0 or docs > 0 do
          File.write(file, new_content)
          IO.puts("✓ #{file}: +#{moduledocs} @moduledoc, +#{docs} @doc")
          %{acc | files: acc.files + 1, moduledocs: acc.moduledocs + moduledocs, docs: acc.docs + docs}
        else
          acc
        end

      {:error, _} ->
        acc
    end
  end

  defp add_docs_to_content(content) do
    lines = String.split(content, "\n")
    {new_lines, moduledocs, docs} = process_lines(lines, [], false, 0, 0)
    {Enum.join(new_lines, "\n"), moduledocs, docs}
  end

  defp process_lines([], acc, _in_code, moduledocs, docs) do
    {Enum.reverse(acc), moduledocs, docs}
  end

  defp process_lines([line | rest], acc, in_code, moduledocs, docs) do
    cond do
      # Detectar inicio de bloque de código Elixir
      String.starts_with?(line, "```elixir") ->
        process_lines(rest, [line | acc], true, moduledocs, docs)

      # Detectar fin de bloque de código
      String.starts_with?(line, "```") and in_code ->
        process_lines(rest, [line | acc], false, moduledocs, docs)

      # Procesar líneas dentro de bloques de código
      in_code ->
        trimmed = String.trim_start(line)
        cond do
          # Detectar defmodule sin @moduledoc
          String.starts_with?(trimmed, "defmodule ") ->
            # Buscar si la siguiente línea no es @moduledoc
            case rest do
              [next | _] ->
                next_trimmed = String.trim_start(next)
                if String.starts_with?(next_trimmed, "@moduledoc") or String.starts_with?(next_trimmed, "@doc") do
                  process_lines(rest, [line | acc], in_code, moduledocs, docs)
                else
                  # Agregar @moduledoc
                  indent = get_indent(line)
                  module_name = extract_module_name(line)
                  moduledoc_line = "#{indent}@moduledoc \"Implements #{format_name(module_name)}\""
                  process_lines(rest, [moduledoc_line, line | acc], in_code, moduledocs + 1, docs)
                end
              [] ->
                process_lines(rest, [line | acc], in_code, moduledocs, docs)
            end

          # Detectar def sin @doc (pero no handle_call, handle_cast, init)
          String.match?(trimmed, ~r/^def\s+\w+/) ->
            callback? = String.match?(trimmed, ~r/^def\s+(handle_call|handle_cast|init|handle_info|code_change|terminate)/)
            if callback? do
              process_lines(rest, [line | acc], in_code, moduledocs, docs)
            else
              case acc do
                ["@doc" <> _ | _] ->
                  # Ya tiene @doc
                  process_lines(rest, [line | acc], in_code, moduledocs, docs)
                _ ->
                  # Agregar @doc
                  indent = get_indent(line)
                  func_name = extract_function_name(line)
                  doc_line = "#{indent}@doc \"#{format_name(func_name)}\""
                  process_lines(rest, [line, doc_line | acc], in_code, moduledocs, docs + 1)
              end
            end

          true ->
            process_lines(rest, [line | acc], in_code, moduledocs, docs)
        end

      true ->
        process_lines(rest, [line | acc], in_code, moduledocs, docs)
    end
  end

  defp get_indent(line) do
    case Regex.scan(~r/^(\s*)/, line) do
      [[indent, _]] -> indent
      _ -> ""
    end
  end

  defp extract_module_name(line) do
    case Regex.run(~r/defmodule\s+(\w+)/, line) do
      [_, name] -> name
      _ -> "Module"
    end
  end

  defp extract_function_name(line) do
    case Regex.run(~r/def\s+(\w+)/, line) do
      [_, name] -> name
      _ -> "function"
    end
  end

  defp format_name(name) do
    name
    |> String.split(~r/(?=[A-Z])/)
    |> Enum.join(" ")
    |> String.downcase()
    |> String.capitalize()
  end

  defp print_summary(%{files: files, moduledocs: moduledocs, docs: docs}) do
    IO.puts("\n" <> String.duplicate("=", 60))
    IO.puts("Code Quality (Docs): #{files} archivos procesados")
    IO.puts("#{moduledocs} @moduledoc agregados")
    IO.puts("#{docs} @doc agregados")
    IO.puts(String.duplicate("=", 60) <> "\n")
  end

  defp save_log(%{files: files, moduledocs: moduledocs, docs: docs}, root_dir) do
    log_file = Path.join(root_dir, ".claude/code-quality-docs.log")
    timestamp = DateTime.utc_now() |> DateTime.to_string()

    log_content = """
    Code Quality Documentation Automation Log
    ========================================
    Timestamp: #{timestamp}

    Summary:
    - Files processed: #{files}
    - @moduledoc added: #{moduledocs}
    - @doc added: #{docs}

    Status: COMPLETED
    """

    File.mkdir_p(Path.dirname(log_file))
    File.write(log_file, log_content)
    IO.puts("Log saved to: #{log_file}")
  end
end

# Ejecutar
root = "/Users/consulting/Documents/consulting/infra/challenges/elixir"
DocAdder.run(root)
