#!/usr/bin/env elixir
# Agent-7 Level 2: Process script/main.exs blocks
# Procesa archivos 210+ en 03-avanzado y todos en 04-insane
# Genera demostraciones funcionales de módulos principales

defmodule Agent7Processor do
  @base_path "/Users/consulting/Documents/consulting/infra/challenges/elixir"
  @log_path "/Users/consulting/.claude/agent-7-level2.log"

  def run do
    File.mkdir_p!(Path.dirname(@log_path))
    log_init()

    # Obtener archivos 210+ de 03-avanzado
    avanzado_files = get_avanzado_files()

    # Obtener todos los archivos de 04-insane
    insane_files = get_insane_files()

    all_files = avanzado_files ++ insane_files

    log("Iniciando procesamiento")
    log("Archivos a procesar: #{length(all_files)}")

    # Procesar cada archivo
    results = Enum.map(all_files, &process_file/1)

    # Contar resultados
    processed = Enum.count(results, & &1 == :processed)
    skipped = Enum.count(results, & &1 == :skipped)
    added = Enum.count(results, & &1 == :added)

    final_message = "Agent-7 (03-avanzado 210+ + 04-insane): #{processed} archivos procesados, #{added} script blocks agregados"
    log(final_message)
    IO.puts(final_message)
  end

  defp get_avanzado_files do
    path = Path.join(@base_path, "03-avanzado")
    path
    |> File.ls!()
    |> Enum.flat_map(&get_category_files("#{path}/#{&1}"))
    |> Enum.filter(&should_process_avanzado?/1)
    |> Enum.sort()
  end

  defp get_category_files(category_path) do
    case File.dir?(category_path) do
      true ->
        category_path
        |> File.ls!()
        |> Enum.map(&Path.join(category_path, &1))
        |> Enum.filter(&String.ends_with?(&1, ".md"))
      false ->
        []
    end
  end

  defp should_process_avanzado?(file) do
    # Extrae el número de la estructura "XXX-nombre"
    case Path.basename(Path.dirname(file)) |> String.split("-") do
      [num | _] ->
        case Integer.parse(num) do
          {n, _} -> n >= 210
          :error -> false
        end
      _ -> false
    end
  end

  defp get_insane_files do
    path = Path.join(@base_path, "04-insane")
    path
    |> File.ls!()
    |> Enum.flat_map(&get_category_files("#{path}/#{&1}"))
    |> Enum.sort()
  end

  defp process_file(file_path) do
    try do
      content = File.read!(file_path)

      # Verificar si tiene heading ### `script/main.exs`
      case String.contains?(content, "### `script/main.exs`") do
        false ->
          :skipped

        true ->
          # Verificar si el bloque está vacío
          case extract_script_block(content) do
            {_pre, ""} ->
              # Bloque vacío, generar script
              case generate_script(file_path, content) do
                nil -> :skipped
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
      end
    rescue
      e ->
        log("ERROR en #{file_path}: #{inspect(e)}")
        :skipped
    end
  end

  defp extract_script_block(content) do
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [pre, rest] ->
        # Buscar el siguiente bloque de código (``` exs)
        case String.split(rest, "```", parts: 3) do
          [_prefix, code, _rest] ->
            # El código está entre los delimitadores ```
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

    # Encontrar dónde termina el bloque de código actual (o donde debería estar)
    case String.split(rest, "```", parts: 3) do
      [prefix, _old_code, suffix] ->
        # Reemplazar entre los ```
        "#{pre}### `script/main.exs`\n\n```exs\n#{new_script}\n```#{suffix}"

      _ ->
        # No hay delimitadores, agregar nuevos
        "#{pre}### `script/main.exs`\n\n```exs\n#{new_script}\n```\n\n#{rest}"
    end
  end

  defp generate_script(file_path, content) do
    # Extraer app_name desde "app: :([a-z_]+)"
    app_name = extract_app_name(content)

    case app_name do
      nil -> nil
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
    # Buscar el bloque ### `lib/<app>.ex`
    module_block_pattern = "### `lib/#{app_name}.ex`"

    case String.contains?(content, module_block_pattern) do
      false ->
        # Buscar con variaciones
        find_module_functions(content, app_name)

      true ->
        [_pre, module_section] = String.split(content, module_block_pattern, parts: 2)

        # Extraer código del bloque ```exs
        case String.split(module_section, "```exs", parts: 2) do
          [_prefix, code_block] ->
            case String.split(code_block, "```", parts: 2) do
              [code, _rest] ->
                extract_functions_from_code(code)

              _ ->
                []
            end

          _ ->
            []
        end
    end
  end

  defp find_module_functions(content, app_name) do
    # Buscar el módulo en cualquier bloque de código
    module_pattern = "defmodule #{Macro.camelize(app_name)} do"

    case String.contains?(content, module_pattern) do
      true ->
        case String.split(content, module_pattern, parts: 2) do
          [_pre, code_section] ->
            # Extraer hasta el cierre del módulo
            extract_functions_from_code(code_section)

          _ ->
            []
        end

      false ->
        []
    end
  end

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
    "IO.puts \"Demo of #{app_name}\"\n"
  end

  defp generate_full_script(app_name, functions) do
    module_name = Macro.camelize(app_name)

    lines = [
      "alias #{module_name}",
      "",
      "# Demostraciones de funciones públicas"
    ]

    # Agregar llamadas a funciones (máximo 4)
    demo_lines = functions
      |> Enum.take(4)
      |> Enum.map(&generate_demo_call(&1))

    (lines ++ demo_lines)
    |> Enum.join("\n")
  end

  defp generate_demo_call(func_name) do
    "IO.puts \"Calling #{func_name}...\"\nIO.inspect #{func_name}()"
  end

  defp log(message) do
    timestamp = DateTime.utc_now() |> DateTime.to_string()
    line = "[#{timestamp}] #{message}\n"
    File.write!(@log_path, line, [:append])
  end

  defp log_init do
    File.write!(@log_path, "=== Agent-7 Level 2 Processing ===\n", [:append])
  end
end

Agent7Processor.run()
