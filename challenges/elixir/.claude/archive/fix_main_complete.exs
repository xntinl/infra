#!/usr/bin/env elixir
# fix_main_complete.exs
# Script para generar demostraciones reales en Main blocks placeholder
# Uso: elixir fix_main_complete.exs [directorio opcional]

defmodule FixMainComplete do
  require Logger

  def run(args) do
    base_dir =
      case args do
        [] -> "/Users/consulting/Documents/consulting/infra/challenges/elixir"
        [dir] -> dir
        _ -> raise "Uso: elixir fix_main_complete.exs [directorio]"
      end

    Logger.configure(level: :info)

    fixed = fix_all_mds(base_dir)

    IO.puts("\n=== RESUMEN ===")
    IO.puts("Fixed: #{fixed.fixed}")
    IO.puts("Skipped: #{fixed.skipped}")
    IO.puts("Errors: #{fixed.errors}")
  end

  defp fix_all_mds(base_dir) do
    pattern = Path.join(base_dir, "**/*.md")
    files = Path.wildcard(pattern)

    files
    |> Enum.reject(fn f -> String.contains?(f, "deps/") end)
    |> Enum.reduce(%{fixed: 0, skipped: 0, errors: 0}, fn file, acc ->
      case process_file(file) do
        :fixed -> %{acc | fixed: acc.fixed + 1}
        :skipped -> %{acc | skipped: acc.skipped + 1}
        :error -> %{acc | errors: acc.errors + 1}
      end
    end)
  end

  defp process_file(filepath) do
    try do
      content = File.read!(filepath)

      case detect_and_fix_placeholder(content) do
        {:ok, new_content} ->
          File.write!(filepath, new_content)
          Logger.info("Fixed: #{filepath}")
          :fixed

        :skip ->
          :skipped

        {:error, reason} ->
          Logger.warning("Error in #{filepath}: #{reason}")
          :error
      end
    rescue
      e ->
        Logger.error("Exception in #{filepath}: #{inspect(e)}")
        :error
    end
  end

  defp detect_and_fix_placeholder(content) do
    # Estrategia: detectar un Main block trivial (sin código real)
    # Primero verificar que el archivo tiene script/main.exs

    if not String.contains?(content, "script/main.exs") do
      :skip
    else
      do_fix_placeholder(content)
    end
  end

  defp do_fix_placeholder(content) do

    case extract_main_section(content) do
      nil ->
        :skip

      main_section ->
        # Verificar que es trivial (solo I/O.puts, comentarios, strings)
        if is_trivial_main?(main_section) do
          # Extraer información del módulo
          case extract_module_info(content) do
            {:ok, module_name, functions} ->
              generated_main = generate_main_block(module_name, functions)
              new_content = replace_main_section(content, generated_main)
              {:ok, new_content}

            {:error, reason} ->
              # Si no hay lib, pero detectamos script/main.exs trivial, es un error real
              {:error, reason}
          end
        else
          # Main block ya tiene código real, saltarlo
          :skip
        end
    end
  end

  defp extract_main_section(content) do
    case Regex.run(
           ~r/defmodule\s+Main\s+do\s+def\s+main\s+do\s+(.*?)\s+end\s+end/msu,
           content,
           capture: [1]
         ) do
      [section] -> section
      _ -> nil
    end
  end

  defp is_trivial_main?(section) do
    trimmed = String.trim(section)

    # Si contiene "# Demo:" es código generado, no es trivial
    if String.contains?(trimmed, "# Demo:") do
      false
    else
      lines = String.split(trimmed, "\n")

      # Líneas consideradas triviales
      non_trivial_lines =
        lines
        |> Enum.reject(fn line ->
          s = String.trim(line)
          # Trivial: comentarios, IO.puts, strings, asignaciones a variables literales
          is_trivial_line(s)
        end)

      Enum.empty?(non_trivial_lines)
    end
  end

  defp is_trivial_line(line) do
    s = String.trim(line)

    cond do
      # Comentarios
      String.starts_with?(s, "#") -> true
      # IO.puts
      String.starts_with?(s, "IO.puts") -> true
      # Líneas vacías
      s == "" -> true
      # Asignación simple a variable (var = "..." or var = [...] or var = 123)
      String.match?(s, ~r/^[a-z_]\w*\s*=\s*["'\[\{]/) -> true
      # Asignación simple sin estructura compleja
      String.match?(s, ~r/^[a-z_]\w*\s*=\s*[0-9]+/) -> true
      # Asignación simple sin estructura compleja
      String.match?(s, ~r/^[a-z_]\w*\s*=\s*:[a-z_]/) -> true
      # Llamadas a funciones que retornan valores (análisis básico)
      # Si contiene .() es una llamada de método/función
      String.match?(s, ~r/^[a-z_]\w*\s*=\s*/) and not String.contains?(s, "\.") and not String.contains?(s, "if ") and not String.contains?(s, "case ") -> true
      true -> false
    end
  end

  defp extract_module_info(content) do
    # Estrategia: buscar la PRIMERA sección con ### que contenga `lib/.../*.ex`
    # Extraer el código del bloque elixir más cercano

    # Buscar todas las líneas que son secciones con lib
    case find_first_lib_section(content) do
      {:ok, lib_path, after_section} ->
        # Extraer el bloque de código (```elixir ... ```)
        case extract_code_block(after_section) do
          {:ok, code_section} ->
            # Parsear functions
            functions = parse_functions(code_section)

            # Extraer nombre del módulo del defmodule
            module_name = extract_module_name(code_section) || derive_module_name(lib_path)

            {:ok, module_name, functions}

          :error ->
            {:error, "No code block found in lib section"}
        end

      :error ->
        {:error, "No lib section found"}
    end
  end

  defp find_first_lib_section(content) do
    # Buscar la primera línea con ### que contenga `lib/`
    case Regex.run(~r/###.*?`lib\/([^`]+)\.ex`.*?\n(.*?)(?=^###|\Z)/msu, content, capture: [1, 2]) do
      [lib_path, section] ->
        {:ok, lib_path, section}

      _ ->
        :error
    end
  end

  defp extract_module_name(code) do
    # Buscar "defmodule ModuleName do"
    case Regex.run(~r/defmodule\s+([A-Z]\w*(?:\.[A-Z]\w*)*)\s+do/m, code, capture: [1]) do
      [name] -> name
      _ -> nil
    end
  end

  defp derive_module_name(lib_path) do
    # Convertir lib_path (ej: "api_client" o "page_stream/paginator") a módulo
    # Último componente: "paginator" -> "Paginator"
    lib_path
    |> String.split("/")
    |> List.last()
    |> String.split("_")
    |> Enum.map(&String.capitalize/1)
    |> Enum.join("")
  end

  defp extract_code_block(text) do
    case Regex.run(~r/```elixir\s*(.*?)\s*```/msu, text, capture: [1]) do
      [code] -> {:ok, code}
      _ -> :error
    end
  end

  defp parse_functions(code) do
    # Estrategia más flexible: buscar @spec y luego def
    # Patrón 1: @spec name/arity :: type
    # Patrón 2: @spec name(...) :: type

    specs_with_slash = parse_specs_with_slash(code)
    specs_with_args = parse_specs_with_args(code)

    # Combinar y deduplicar por nombre
    (specs_with_slash ++ specs_with_args)
    |> Enum.uniq_by(fn f -> f.name end)
    |> Enum.sort_by(fn f -> f.arity end)
  end

  defp parse_specs_with_slash(code) do
    # @spec name/arity :: return_type
    spec_pattern = ~r/@spec\s+(\w+)\/([\d]+)\s*::\s*(.+?)(?=\n\s*(?:@|def|$))/msu

    Regex.scan(spec_pattern, code, capture: [1, 2, 3])
    |> Enum.map(fn [name, arity_str, spec] ->
      arity = String.to_integer(arity_str)
      args = parse_spec_args(spec)

      %{
        name: name,
        arity: arity,
        args: args,
        spec: String.trim(spec)
      }
    end)
  end

  defp parse_specs_with_args(code) do
    # @spec name(arg1, arg2, ...) :: return_type
    # Estrategia simple: extraer líneas con @spec, luego parsear manualmente

    code
    |> String.split("\n")
    |> Enum.reduce([], fn line, acc ->
      case Regex.run(~r/@spec\s+(\w+)\s*\(/, line, capture: [1]) do
        [name] ->
          # Buscar la sección entre paréntesis
          # Usar un enfoque simple: contar la posición del @spec y luego buscar el ::
          case extract_args_from_spec_line(line, name) do
            {:ok, args, return_type} ->
              acc ++
                [
                  %{
                    name: name,
                    arity: length(args),
                    args: args,
                    spec: String.trim(return_type)
                  }
                ]

            :error ->
              acc
          end

        _ ->
          acc
      end
    end)
  end

  defp extract_args_from_spec_line(line, name) do
    # Formato: @spec name(arg1, arg2, ...) :: return
    # Encontrar la sección entre el ( y ::

    case Regex.run(~r/@spec\s+#{name}\s*\((.*)\)\s*::\s*(.+?)$/m, line, capture: [1, 2]) do
      [args_str, return_type] ->
        # Parsear argumentos de forma inteligente
        # Los argumentos están separados por comas, pero pueden contener parens
        # Contar comas a nivel superior (sin parens anidados)

        args =
          parse_top_level_args(args_str)
          |> Enum.map(&String.trim/1)
          |> Enum.filter(fn a -> a != "" end)

        {:ok, args, return_type}

      _ ->
        :error
    end
  end

  defp parse_top_level_args(args_str) do
    # Dividir por comas, pero respetando parens/brackets anidados
    # Usar enfoque de prepend y luego reverso
    args_str
    |> String.split(",")
    |> Enum.reduce({[], 0}, fn part, {acc, depth} ->
      # Contar parens abiertos/cerrados
      open_count = String.count(part, "(") + String.count(part, "[") + String.count(part, "{")
      close_count = String.count(part, ")") + String.count(part, "]") + String.count(part, "}")
      new_depth = depth + open_count - close_count

      if depth == 0 and open_count == close_count do
        # Es un nuevo argumento separado
        {[part | acc], 0}
      else
        # Fusionar con el anterior (caso de anidación)
        case acc do
          [last | rest] ->
            {["#{last},#{part}" | rest], new_depth}

          [] ->
            {[part], new_depth}
        end
      end
    end)
    |> elem(0)
    |> Enum.reverse()
  end

  defp parse_spec_args(spec_str) do
    # Extraer argumentos antes del return type (::)
    # Formato: arg1, arg2, ... :: return_type
    spec_trimmed = String.trim(spec_str)

    case String.split(spec_trimmed, "::", parts: 2) do
      [args_part, _return] ->
        # Dividir argumentos por comas, siendo cuidadoso con tipos complejos
        args_part
        |> String.trim()
        |> String.split(",")
        |> Enum.map(&String.trim/1)
        |> Enum.filter(fn arg -> arg != "" end)

      _ ->
        []
    end
  end

  defp generate_main_block(module_name, functions) do
    # Seleccionar funciones demostrables
    demostrables =
      functions
      |> Enum.filter(fn f -> f.arity <= 2 and f.name != "main" end)
      |> Enum.take(5)

    if Enum.empty?(demostrables) do
      generate_main_empty(module_name)
    else
      generate_main_with_demos(module_name, demostrables)
    end
  end

  defp generate_main_empty(module_name) do
    """
    defmodule Main do
      def main do
        IO.puts("=== #{module_name} ===\\n")
        # No public functions to demonstrate
      end
    end

    Main.main()
    """
  end

  defp generate_main_with_demos(module_name, functions) do
    demos =
      functions
      |> Enum.map(&generate_function_demo/1)
      |> Enum.filter(fn d -> d != "" end)
      |> Enum.map(fn demo ->
        # Re-indentar cada línea del demo a 8 espacios
        demo
        |> String.split("\n")
        |> Enum.map(fn line -> "      #{line}" end)
        |> Enum.join("\n")
      end)
      |> Enum.join("\n\n")

    indent = if demos != "" do
      "\n#{demos}\n    "
    else
      ""
    end

    """
    defmodule Main do
      def main do
        IO.puts("=== #{module_name} ===\\n")
        #{indent}
      end
    end

    Main.main()
    """
  end

  defp generate_function_demo(%{name: name, arity: 0}) do
    "# Demo: #{name}/0\nresult = #{name}()\nIO.puts(inspect(result))"
  end

  defp generate_function_demo(%{name: name, arity: 1, args: args}) do
    sample = generate_sample_arg(Enum.at(args, 0, "term"))
    "# Demo: #{name}/1\nresult = #{name}(#{sample})\nIO.puts(inspect(result))"
  end

  defp generate_function_demo(%{name: name, arity: 2, args: args}) do
    sample1 = generate_sample_arg(Enum.at(args, 0, "term"))
    sample2 = generate_sample_arg(Enum.at(args, 1, "term"))
    "# Demo: #{name}/2\nresult = #{name}(#{sample1}, #{sample2})\nIO.puts(inspect(result))"
  end

  defp generate_function_demo(func) do
    "# #{func.name}/#{func.arity} — skipped (arity > 2, complex to demo)"
  end

  defp generate_sample_arg(nil), do: "nil"
  defp generate_sample_arg(""), do: "\"\""

  defp generate_sample_arg(arg_type) do
    arg = String.trim(arg_type)

    cond do
      arg == "" -> "nil"
      String.contains?(arg, "String.t") -> "\"sample\""
      String.contains?(arg, "String") -> "\"sample\""
      String.contains?(arg, "integer") -> "42"
      String.contains?(arg, "pos_integer") -> "1"
      String.contains?(arg, "non_neg_integer") -> "0"
      String.contains?(arg, "float") -> "3.14"
      String.contains?(arg, "boolean") -> "true"
      String.contains?(arg, "atom") -> ":ok"
      String.contains?(arg, "list") or String.contains?(arg, "[") -> "[1, 2, 3]"
      String.contains?(arg, "map") or String.contains?(arg, "%{") -> "%{a: 1, b: 2}"
      String.contains?(arg, "tuple") or String.contains?(arg, "{") -> "{:ok, 1}"
      String.contains?(arg, "keyword") -> "[a: 1, b: 2]"
      true -> "nil"
    end
  end

  defp replace_main_section(content, new_main) do
    # Estrategia: split + reconstrucción en lugar de regex global
    # Evita problemas con variaciones en whitespace/formato

    case String.split(content, "### `script/main.exs`") do
      [before, after_marker] ->
        # Encontrar el bloque elixir (```elixir ... ```)
        case extract_and_replace_code_block(after_marker, new_main) do
          {:ok, new_after} -> before <> "### `script/main.exs`" <> new_after
          :error -> content  # Fallback: devolver original si falla
        end

      _ ->
        # No encontró el marker, devolver original
        content
    end
  end

  defp extract_and_replace_code_block(text, new_main) do
    # Buscar el primer ``` que inicia el bloque elixir
    case String.split(text, "```elixir") do
      [before_fence, after_open] ->
        # Buscar el closing ```
        case String.split(after_open, "```", parts: 2) do
          [_old_code, after_fence] ->
            # Reconstruir: antes + ```elixir\n + nuevo código + \n```
            new_block = before_fence <> "```elixir\n" <> String.trim(new_main) <> "\n```" <> after_fence
            {:ok, new_block}

          _ ->
            :error
        end

      _ ->
        :error
    end
  end
end

FixMainComplete.run(System.argv())
