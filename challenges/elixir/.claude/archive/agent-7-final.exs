#!/usr/bin/env elixir
# Agent-7 Level 2: Add script/main.exs blocks to 04-insane files
# and update any empty blocks in 03-avanzado (210+)

defmodule Agent7Final do
  @base_path "/Users/consulting/Documents/consulting/infra/challenges/elixir"
  @log_path "/Users/consulting/.claude/agent-7-level2.log"

  def run do
    File.mkdir_p!(Path.dirname(@log_path))
    log_init()

    # Get all files from 03-avanzado (210+) and 04-insane
    avanzado_files = get_avanzado_files_210_plus()
    insane_files = get_insane_files()

    all_files = avanzado_files ++ insane_files

    log("Total files to process: #{length(all_files)}")
    log("  - From 03-avanzado (210+): #{length(avanzado_files)}")
    log("  - From 04-insane: #{length(insane_files)}")

    # Process each file
    results = Enum.map(all_files, &process_file/1)

    # Count results
    processed = Enum.count(results, & &1 == :processed)
    added = Enum.count(results, & &1 == :added)
    _skipped = Enum.count(results, & &1 == :skipped)

    final_message = "Agent-7 (03-avanzado 210+ + 04-insane): #{processed} archivos procesados, #{added} script blocks agregados"
    log(final_message)
    IO.puts(final_message)
  end

  defp get_avanzado_files_210_plus do
    base = Path.join(@base_path, "03-avanzado")
    base
    |> File.ls!()
    |> Enum.flat_map(&find_md_files("#{base}/#{&1}"))
    |> Enum.filter(fn f ->
      case extract_number(f) do
        nil -> false
        n -> n >= 210
      end
    end)
    |> Enum.sort()
  end

  defp get_insane_files do
    base = Path.join(@base_path, "04-insane")
    base
    |> File.ls!()
    |> Enum.flat_map(&find_md_files("#{base}/#{&1}"))
    |> Enum.sort()
  end

  defp find_md_files(path) do
    case File.dir?(path) do
      true ->
        case File.ls(path) do
          {:ok, entries} ->
            entries
            |> Enum.filter(&String.ends_with?(&1, ".md"))
            |> Enum.map(&Path.join(path, &1))
          {:error, _} ->
            []
        end
      false ->
        []
    end
  end

  defp extract_number(path) do
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

      # Check if file already has the heading
      if String.contains?(content, "### `script/main.exs`") do
        # File has heading, check if block is empty
        case extract_script_block(content) do
          {_pre, ""} ->
            # Empty block, generate and replace
            case generate_script(content) do
              nil -> :skipped
              new_script ->
                new_content = replace_script_block(content, new_script)
                File.write!(file_path, new_content)
                log("UPDATED (empty block): #{file_path}")
                :added
            end

          {_pre, _existing} ->
            # Has content, skip
            :skipped
        end
      else
        # No heading yet, add it with generated script
        case generate_script(content) do
          nil -> :skipped
          new_script ->
            new_content = append_script_section(content, new_script)
            File.write!(file_path, new_content)
            log("ADDED (new section): #{file_path}")
            :added
        end
      end
    rescue
      e ->
        log("ERROR in #{file_path}: #{inspect(e)}")
        :skipped
    else
      result -> result
    end
  end

  defp extract_script_block(content) do
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [pre, rest] ->
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

    case String.split(rest, "```", parts: 3) do
      [prefix, _old_code, suffix] ->
        "#{pre}### `script/main.exs`\n\n```elixir\n#{new_script}\n```#{suffix}"

      _ ->
        "#{pre}### `script/main.exs`\n\n```elixir\n#{new_script}\n```\n\n#{rest}"
    end
  end

  defp append_script_section(content, new_script) do
    section = """

    ### `script/main.exs`

    ```elixir
    #{new_script}
    ```
    """
    content <> section
  end

  defp generate_script(content) do
    # Extract app name
    app_name = extract_app_name(content)

    case app_name do
      nil ->
        nil
      name ->
        # Try to find public functions
        functions = extract_public_functions(content, name)

        case functions do
          [] ->
            generate_minimal_script(name)

          funcs ->
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
    module_name = Macro.camelize(app_name)

    case String.split(content, "defmodule #{module_name} do", parts: 2) do
      [_pre, code_section] ->
        code_section
        |> String.split("\n")
        |> Enum.filter(&String.match?(&1, ~r/^\s*def\s+\w+/))
        |> Enum.map(&extract_fn_name/1)
        |> Enum.filter(& &1 != nil)
        |> Enum.uniq()

      _ ->
        []
    end
  end

  defp extract_fn_name(line) do
    case Regex.run(~r/def\s+(\w+)/, line, capture: :all_but_first) do
      [name] -> name
      _ -> nil
    end
  end

  defp generate_minimal_script(app_name) do
    """
    IO.puts("=== #{Macro.camelize(app_name)} Demo ===")
    IO.puts("Module demo. Adjust the code below to call your module's public functions.")
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

    # Generate demos for up to 4 functions
    demo_lines = functions
      |> Enum.take(4)
      |> Enum.flat_map(&gen_demo/1)

    (lines ++ demo_lines)
    |> Enum.join("\n")
  end

  defp gen_demo(func_name) do
    [
      "# Demo: #{func_name}",
      "# Adjust arguments as needed",
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
    File.write!(@log_path, "=== Agent-7 Level 2 Processing ===\n")
  end
end

Agent7Final.run()
