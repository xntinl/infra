defmodule FixIncoherence do
  @moduledoc """
  Fixes structural and code coherence issues:
  - Removes module duplication from Executable Example
  - Converts solution.exs references to script/main.exs
  - Removes conflicting Main definitions
  - Fixes inconsistent structure declarations
  """

  def main do
    IO.puts("=== Fixing Code ↔ Structure ↔ Name Coherence ===\n")

    root_dirs = ["01-basico", "02-intermedio", "03-avanzado", "04-insane"]

    stats = Enum.reduce(root_dirs, %{fixed: 0, skipped: 0, error: 0}, fn dir, acc ->
      process_directory(dir, acc)
    end)

    IO.puts("\n=== Summary ===")
    IO.puts("Fixed:   #{stats.fixed}")
    IO.puts("Skipped: #{stats.skipped}")
    IO.puts("Errors:  #{stats.error}")
  end

  defp process_directory(dir_name, acc) do
    base_path = File.cwd!()
    dir_path = Path.join(base_path, dir_name)

    unless File.exists?(dir_path) do
      IO.warn("Directory not found: #{dir_path}")
      acc
    else
      md_files = find_md_files(dir_path)
      IO.puts("Processing #{dir_name}: #{length(md_files)} files")

      Enum.reduce(md_files, acc, fn file_path, acc_inner ->
        case fix_file(file_path) do
          {:fixed, _app_name} ->
            rel_path = Path.relative_to(file_path, base_path)
            IO.puts("  ✓ #{rel_path}")
            %{acc_inner | fixed: acc_inner.fixed + 1}

          {:skipped, _reason} ->
            %{acc_inner | skipped: acc_inner.skipped + 1}

          {:error, reason} ->
            rel_path = Path.relative_to(file_path, base_path)
            IO.warn("  ✗ #{rel_path}: #{reason}")
            %{acc_inner | error: acc_inner.error + 1}
        end
      end)
    end
  end

  defp find_md_files(dir_path) do
    dir_path
    |> File.ls!()
    |> Enum.flat_map(fn item ->
      full_path = Path.join(dir_path, item)

      cond do
        File.dir?(full_path) -> find_md_files(full_path)
        String.ends_with?(item, ".md") -> [full_path]
        true -> []
      end
    end)
  end

  defp fix_file(file_path) do
    content = File.read!(file_path)

    case extract_app_name(content) do
      nil ->
        {:skipped, "no app name in mix.exs"}

      app_name ->
        # Apply transformations
        new_content = content
        new_content = remove_module_duplication(new_content, app_name)
        new_content = fix_solution_exs_reference(new_content)
        new_content = remove_duplicate_structures(new_content)
        new_content = fix_double_main_definitions(new_content)

        if new_content != content do
          File.write!(file_path, new_content)
          {:fixed, app_name}
        else
          {:skipped, "already coherent"}
        end
    end
  rescue
    e ->
      {:error, Exception.message(e)}
  end

  defp extract_app_name(content) do
    case Regex.run(~r/app:\s*:([a-z_]+)/m, content) do
      [_full, app_name] -> app_name
      nil -> nil
    end
  end

  # Remove module duplication: if module appears twice, remove from Executable Example
  defp remove_module_duplication(content, app_name) do
    module_name = Macro.camelize(app_name)

    # Find how many times the main module is defined
    pattern = ~r/defmodule\s+#{Regex.escape(module_name)}\s+do/
    matches = Regex.scan(pattern, content)

    if length(matches) > 1 do
      # Module is defined more than once - likely in Implementation and Executable Example
      # Remove the one from Executable Example
      case Regex.run(~r/(## Executable Example.*?)(```elixir\s+defmodule\s+#{Regex.escape(module_name)}\s+do.*?end\s*```)/s, content) do
        [_full, before, code_block] ->
          # Replace with just main.exs placeholder
          replacement = before <> "\n```elixir\ndefmodule Main do\n  def main do\n    # Demonstration of #{module_name}\n    IO.puts(\"=== #{module_name} Demo ===\")\n  end\nend\n\nMain.main()\n```"
          String.replace(content, _full, replacement, global: false)

        nil ->
          content
      end
    else
      content
    end
  end

  # Change "solution.exs" references to "script/main.exs"
  defp fix_solution_exs_reference(content) do
    result = content

    # Fix instruction text
    result = String.replace(
      result,
      "Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:",
      "Run with: `elixir script/main.exs`"
    )

    # Fix heading if it says "## Executable Example"
    result = String.replace(
      result,
      "## Executable Example",
      "### `script/main.exs`"
    )

    result
  end

  # Remove duplicate structure declarations
  defp remove_duplicate_structures(content) do
    # Look for patterns where "Project structure:" or similar appears multiple times
    case Regex.scan(~r/(Project structure|project structure:)/i, content) |> length() do
      1 ->
        content

      count when count > 1 ->
        # Remove duplicate structure blocks that appear in the body (after first one)
        case Regex.run(~r/(## Project structure.*?```\s*\n.*?```\s*\n\n)(Project structure:.*?```\s*\n.*?```)/s, content) do
          [full_match, first, second] ->
            # Keep first, remove second
            String.replace(content, full_match, first, global: false)

          nil ->
            content
        end

      _ ->
        content
    end
  end

  # Remove conflicting Main definitions
  defp fix_double_main_definitions(content) do
    # Find all defmodule Main occurrences
    main_count = Regex.scan(~r/defmodule\s+Main\s+do/, content) |> length()

    if main_count > 1 do
      # Remove Main from benchmark/illustrative sections
      # Pattern: defmodule Main inside a benchmark/illustration block
      result = String.replace(
        content,
        ~r/defmodule\s+Main\s+do\s+Bench\.run\(\)\s+end/,
        "# Benchmark demonstration (see Bench.run() above)"
      )

      # Also fix any Main.Main.main() calls → Main.main()
      result = String.replace(result, "Main.Main.main()", "Main.main()")

      result
    else
      # Just fix Main.Main.main() if it exists
      String.replace(content, "Main.Main.main()", "Main.main()")
    end
  end
end

FixIncoherence.main()
