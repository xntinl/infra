defmodule FixCoherence do
  def main do
    IO.puts("=== Fixing Coherence in Challenge .md Files ===\n")

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
        test_file = "#{app_name}_test.exs"

        # Apply transformations
        new_content = content
        new_content = fix_project_structure(new_content, app_name)
        new_content = fix_headings(new_content, app_name, test_file)
        new_content = fix_main_call(new_content)

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

  # Fix Project structure: lib/<app>/<app>/core.ex → lib/<app>.ex
  defp fix_project_structure(content, app_name) do
    result = content

    # Replace the nested lib structure with flat structure
    # Pattern: lib/\n│   └── <app>/\n│       ├── core.ex
    old_lib = "lib/\n│   └── #{app_name}/\n│       ├── core.ex          (pure functions, no I/O)\n│       └── helpers.ex       (if needed)"
    new_lib = "lib/\n│   └── #{app_name}.ex"

    String.replace(result, old_lib, new_lib, global: true)
  end

  # Normalize headings
  defp fix_headings(content, app_name, test_file) do
    result = content

    # Fix heading formats
    result = String.replace(result, "### Dependencies (mix.exs)", "### `mix.exs`", global: true)
    result = String.replace(result, "### Tests", "### `test/#{test_file}`", global: true)
    result = String.replace(result, "### `lib/#{app_name}/core.ex`", "### `lib/#{app_name}.ex`", global: true)

    result
  end

  # Fix main() → Main.main() (but not if already Main.main or Main.Main.main)
  defp fix_main_call(content) do
    # First, fix any doubled Main.Main.main() → Main.main()
    content = String.replace(content, ~r/Main\.Main\.main\(\)/, "Main.main()", global: true)

    # Then fix remaining standalone main() → Main.main()
    # But be careful not to replace if it's already prefixed
    String.replace(content, ~r/(?<!Main\.)(?<!\.)main\(\)/, "Main.main()", global: true)
  end
end

FixCoherence.main()
