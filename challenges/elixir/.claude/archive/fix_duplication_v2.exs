#!/usr/bin/env elixir
# fix_duplication_v2.exs
# Remover módulos duplicados en la sección ### `script/main.exs`

defmodule FixDuplication do
  def run do
    directories = [
      "/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico",
      "/Users/consulting/Documents/consulting/infra/challenges/elixir/02-intermedio",
      "/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado",
      "/Users/consulting/Documents/consulting/infra/challenges/elixir/04-insane"
    ]

    {fixed, skipped, _errors} =
      directories
      |> Enum.reduce({0, 0, 0}, fn dir, {f, s, e} ->
        {dir_fixed, dir_skipped} = process_directory(dir)
        IO.puts("✓ #{Path.relative_to(dir, "/Users/consulting/Documents/consulting/infra/challenges/elixir")}: Fixed #{dir_fixed}, Skipped #{dir_skipped}")
        {f + dir_fixed, s + dir_skipped, e}
      end)

    IO.puts("\n=== SUMMARY ===")
    IO.puts("Fixed: #{fixed}, Skipped: #{skipped}")
  end

  defp process_directory(dir) do
    md_files = find_all_md_files(dir)

    Enum.reduce(md_files, {0, 0}, fn path, {f, s} ->
      case process_file(path) do
        :fixed -> {f + 1, s}
        :skipped -> {f, s + 1}
        :error -> {f, s}
      end
    end)
  end

  defp find_all_md_files(dir) do
    case File.ls(dir) do
      {:ok, items} ->
        items
        |> Enum.flat_map(fn item ->
          path = Path.join(dir, item)

          case File.dir?(path) do
            true -> find_all_md_files(path)
            false -> if String.ends_with?(item, ".md"), do: [path], else: []
          end
        end)

      {:error, _reason} ->
        []
    end
  end

  defp process_file(path) do
    case File.read(path) do
      {:ok, content} ->
        case fix_content(content) do
          {:modified, new_content} ->
            File.write(path, new_content)
            :fixed

          :unchanged ->
            :skipped

          :error ->
            :error
        end

      {:error, _reason} ->
        :error
    end
  end

  defp fix_content(content) do
    # Buscar la sección ### `script/main.exs`
    case String.split(content, "### `script/main.exs`") do
      [_before, section_rest] ->
        # Encontrada la sección
        # Ahora buscar el siguiente heading (###, ##, #) o fin
        case split_section_and_rest(section_rest) do
          {section, rest} ->
            case fix_section(section) do
              {:modified, new_section} ->
                new_content = content_before_section(content) <> "### `script/main.exs`" <> new_section <> rest
                {:modified, new_content}

              :unchanged ->
                :unchanged

              :error ->
                :error
            end

          :error ->
            :error
        end

      [_] ->
        # No encontrada la sección
        :unchanged

      _ ->
        :error
    end
  end

  defp content_before_section(content) do
    case String.split(content, "### `script/main.exs`") do
      [before, _] -> before
      _ -> ""
    end
  end

  defp split_section_and_rest(section_with_rest) do
    # Buscar el siguiente heading (^##, ^###, etc.)
    case Regex.split(~r/\n(?=^#+\s)/m, section_with_rest, parts: 2) do
      [section] -> {section, ""}
      [section, rest] -> {section, "\n" <> rest}
      _ -> :error
    end
  end

  defp fix_section(section) do
    # Buscar bloque de código ```elixir ... ```
    case find_code_block(section) do
      {before_code, code_block, after_code} ->
        # Procesar el bloque de código
        case fix_code_block(code_block) do
          {:modified, new_code_block} ->
            new_section = before_code <> new_code_block <> after_code
            {:modified, new_section}

          :unchanged ->
            :unchanged

          :error ->
            :error
        end

      :not_found ->
        :unchanged
    end
  end

  defp find_code_block(section) do
    # Buscar ```elixir y ```
    case Regex.run(~r/([\s\S]*?)(```(?:elixir)?\s*\n)([\s\S]*?)\n(```)([\s\S]*)/m, section) do
      [_full, before, opening, code_content, closing, after_code] ->
        {before <> opening, code_content <> "\n" <> closing, after_code}

      nil ->
        :not_found
    end
  end

  defp fix_code_block(code_block) do
    # Separar el código en líneas
    lines = String.split(code_block, "\n")

    # Eliminar la última línea vacía si existe (viene de "\n```")
    lines = if Enum.any?(lines, &(String.trim(&1) == "")), do: Enum.filter(lines, &(String.trim(&1) != "")), else: lines

    # Contar defmodules
    defmodule_count = Enum.count(lines, &String.match?(&1, ~r/^\s*defmodule\s+/))

    if defmodule_count < 2 do
      :unchanged
    else
      case remove_duplicates(lines) do
        {:fixed, new_lines} ->
          new_code = Enum.join(new_lines, "\n")
          {:modified, new_code}

        :error ->
          :error
      end
    end
  end

  defp remove_duplicates(lines) do
    # Encontrar índice de "defmodule Main do"
    main_idx = find_line_index(lines, ~r/^\s*defmodule\s+Main\s+do\s*$/)

    if is_nil(main_idx) do
      :error
    else
      # Encontrar el "end" que cierra el defmodule
      case find_closing_end(lines, main_idx) do
        nil ->
          :error

        end_idx ->
          # Extraer Main module y Main.main()
          main_lines = Enum.slice(lines, main_idx..end_idx)

          # Buscar Main.main() después
          remaining = Enum.slice(lines, (end_idx + 1)..-1//1)
          main_call_idx = find_line_index(remaining, ~r/^Main\.main\(\)\s*$/)

          if is_nil(main_call_idx) do
            # Main.main() no existe, añadirlo
            new_lines = main_lines ++ ["Main.main()"]
            {:fixed, new_lines}
          else
            main_call = [Enum.at(remaining, main_call_idx)]
            new_lines = main_lines ++ main_call
            {:fixed, new_lines}
          end
      end
    end
  end

  defp find_line_index(lines, pattern) do
    Enum.find_index(lines, &String.match?(&1, pattern))
  end

  defp find_closing_end(lines, defmodule_idx) do
    # Buscar el "end" al nivel de indentación 0 que cierra el defmodule
    search_end(lines, defmodule_idx + 1)
  end

  defp search_end(lines, idx) do
    if idx >= Enum.count(lines) do
      nil
    else
      line = Enum.at(lines, idx)
      trimmed = String.trim_leading(line)
      indent = String.length(line) - String.length(trimmed)

      if String.starts_with?(trimmed, "end") and indent == 0 do
        idx
      else
        search_end(lines, idx + 1)
      end
    end
  end
end

# Ejecutar
FixDuplication.run()
