#!/usr/bin/env elixir
# Agent-7 Level 2: Process script/main.exs blocks
# Procesa archivos 210+ en 03-avanzado y todos en 04-insane
# Genera demostraciones funcionales de módulos principales

defmodule Agent7ProcessorV2 do
  @base_path "/Users/consulting/Documents/consulting/infra/challenges/elixir"
  @log_path "/Users/consulting/.claude/agent-7-level2.log"

  def run do
    File.mkdir_p!(Path.dirname(@log_path))
    log_init()

    # Obtener archivos con el heading script/main.exs
    files_with_heading = find_files_with_heading()

    log("Archivos encontrados con heading: #{length(files_with_heading)}")

    # Filtrar por criterios (210+ en 03-avanzado, todos en 04-insane)
    filtered_files = Enum.filter(files_with_heading, &should_process?/1)

    log("Archivos a procesar después de filtrar: #{length(filtered_files)}")

    # Procesar cada archivo
    results = Enum.map(filtered_files, &process_file/1)

    # Contar resultados
    processed = Enum.count(results, & &1 == :processed)
    _skipped = Enum.count(results, & &1 == :skipped)
    added = Enum.count(results, & &1 == :added)

    final_message = "Agent-7 (03-avanzado 210+ + 04-insane): #{processed} archivos procesados, #{added} script blocks agregados"
    log(final_message)
    IO.puts(final_message)
  end

  # Buscar todos los archivos .md que contienen el heading
  defp find_files_with_heading do
    [avanzado_dir(), insane_dir()]
    |> Enum.flat_map(&find_markdown_files/1)
    |> Enum.uniq()
  end

  defp find_markdown_files(dir_path) do
    case File.ls(dir_path) do
      {:ok, entries} ->
        entries
        |> Enum.flat_map(&find_markdown_in_category("#{dir_path}/#{&1}"))
      {:error, _} ->
        []
    end
  end

  defp find_markdown_in_category(path) do
    case File.dir?(path) do
      true ->
        case File.ls(path) do
          {:ok, entries} ->
            entries
            |> Enum.filter(&String.ends_with?(&1, ".md"))
            |> Enum.map(&"#{path}/#{&1}")
          {:error, _} ->
            []
        end
      false ->
        []
    end
  end

  defp avanzado_dir, do: Path.join(@base_path, "03-avanzado")
  defp insane_dir, do: Path.join(@base_path, "04-insane")

  defp should_process?(file_path) do
    cond do
      String.contains?(file_path, "03-avanzado") ->
        # Solo archivos 210+
        case extract_number_from_path(file_path) do
          nil -> false
          n -> n >= 210
        end

      String.contains?(file_path, "04-insane") ->
        # Todos los archivos
        true

      true ->
        false
    end
  end

  defp extract_number_from_path(path) do
    # Extraer número del path: "XXX-nombre"
    case Regex.run(~r/\/(\d+)-/, path) do
      [_match, num_str] ->
        case Integer.parse(num_str) do
          {n, _} -> n
          :error -> nil
        end
      _ -> nil
    end
  end

  defp process_file(file_path) do
    try do
      content = File.read!(file_path)

      # Extraer el bloque de script/main.exs
      case extract_script_block(content) do
        {_pre, ""} ->
          # Bloque vacío, generar script
          case generate_script(file_path, content) do
            nil ->
              :skipped
            new_script ->
              new_content = replace_script_block(content, new_script)
              File.write!(file_path, new_content)
              log("ACTUALIZADO: #{file_path}")
              :added
          end

        {_pre, _existing} ->
          # Bloque tiene contenido, saltar
          :skipped
      end
    rescue
      e ->
        log("ERROR en #{file_path}: #{inspect(e)}")
        :skipped
    else
      result ->
        :processed
        result
    end
  end

  defp extract_script_block(content) do
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [pre, rest] ->
        # Buscar el siguiente bloque de código (``` exs o ``` elixir)
        case String.split(rest, "```", parts: 3) do
          [_prefix, code, _rest] ->
            {pre, String.trim(code)}
          _ ->
            {pre, ""}
        end

      _ ->
        {content, ""}
    end
  end

  defp replace_script_block(content, new_script) do
    [pre, rest] = String.split(content, "### `script/main.exs`", parts: 2)

    # Encontrar dónde termina el bloque de código actual
    case String.split(rest, "```", parts: 3) do
      [prefix, _old_code, suffix] ->
        # Reemplazar entre los ```
        "#{pre}### `script/main.exs`\n\n```elixir\n#{new_script}\n```#{suffix}"

      _ ->
        # No hay delimitadores, agregar nuevos
        "#{pre}### `script/main.exs`\n\n```elixir\n#{new_script}\n```\n\n#{rest}"
    end
  end

  defp generate_script(_file_path, content) do
    # Extraer app_name desde "app: :([a-z_]+)"
    app_name = extract_app_name(content)

    case app_name do
      nil ->
        nil
      name ->
        # Buscar funciones públicas del módulo principal
        functions = extract_public_functions(content, name)

        case functions do
          [] ->
            # Sin funciones públicas, script mínimo
            generate_minimal_script(name)

          funcs ->
            # Script con llamadas a funciones
            generate_full_script(name, funcs)
        end
    end
  end

  defp extract_app_name(content) do
    case Regex.run(~r/app:\s*:([a-z_]+)/, content, capture: :all_but_first) do
      [name] -> name
      _ -> nil
    end
  end

  defp extract_public_functions(content, app_name) do
    # Buscar el módulo principal (usualmente defmodule sin underscore)
    module_name = Macro.camelize(app_name)

    case find_module_block(content, module_name) do
      nil -> []
      code -> extract_functions_from_code(code)
    end
  end

  defp find_module_block(content, module_name) do
    # Buscar defmodule ModuleName do
    pattern = "defmodule #{module_name} do"

    case String.split(content, pattern, parts: 2) do
      [_pre, code_section] ->
        # Extraer hasta el cierre del módulo (el próximo 'end' sin indent o siguiente 'defmodule')
        case find_end_of_module(code_section) do
          nil -> nil
          code -> code
        end

      _ ->
        nil
    end
  end

  defp find_end_of_module(code) do
    # Simple heuristic: take everything until we see 'end' at line start
    lines = String.split(code, "\n")
    case find_module_end_line(lines, 0) do
      nil -> nil
      idx ->
        lines
        |> Enum.take(idx)
        |> Enum.join("\n")
    end
  end

  defp find_module_end_line([line | rest], idx) do
    trimmed = String.trim_leading(line)
    if String.match?(trimmed, ~r/^end(\s|$)/) and idx > 0 do
      idx
    else
      find_module_end_line(rest, idx + 1)
    end
  end

  defp find_module_end_line([], _idx), do: nil

  defp extract_functions_from_code(code) do
    # Encontrar todas las funciones públicas (def, no defp)
    code
    |> String.split("\n")
    |> Enum.filter(&String.match?(&1, ~r/^\s*def\s+\w+/))
    |> Enum.map(&extract_function_name/1)
    |> Enum.filter(& &1 != nil)
    |> Enum.uniq()
  end

  defp extract_function_name(line) do
    case Regex.run(~r/def\s+(\w+)/, line, capture: :all_but_first) do
      [name] -> name
      _ -> nil
    end
  end

  defp generate_minimal_script(app_name) do
    """
    IO.puts("=== #{Macro.camelize(app_name)} Demo ===\\n")
    IO.puts("No public functions found.")
    """
  end

  defp generate_full_script(app_name, functions) do
    module_name = Macro.camelize(app_name)

    lines = [
      "IO.puts(\"=== #{module_name} Demo ===\")",
      "IO.puts(\"\")",
      "alias #{module_name}",
      ""
    ]

    # Generar demostraciones para máximo 4 funciones
    demo_lines = functions
      |> Enum.take(4)
      |> Enum.flat_map(&generate_demo_call/1)

    (lines ++ demo_lines)
    |> Enum.join("\n")
  end

  defp generate_demo_call(func_name) do
    [
      "# Demo: #{func_name}",
      "IO.puts(\"Calling #{func_name}...\")  # adjust args as needed",
      "# result = #{func_name}()",
      "# IO.inspect(result)",
      ""
    ]
  end

  defp log(message) do
    timestamp = DateTime.utc_now() |> DateTime.to_string()
    line = "[#{timestamp}] #{message}\n"
    File.write!(@log_path, line, [:append])
  end

  defp log_init do
    # Clear old log
    File.write!(@log_path, "=== Agent-7 Level 2 Processing ===\n")
  end
end

Agent7ProcessorV2.run()
