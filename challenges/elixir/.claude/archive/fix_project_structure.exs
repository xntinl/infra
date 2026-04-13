defmodule FixProjectStructure do
  @moduledoc """
  Corrects project structure naming inconsistencies in Elixir challenge .md files.
  """

  def main do
    IO.puts("=== Fixing Project Structure in Challenge .md Files ===\n")

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
      IO.puts("Processing #{dir_name}: #{length(md_files)} files found")

      Enum.reduce(md_files, acc, fn file_path, acc_inner ->
        case fix_file(file_path) do
          {:fixed, app_name} ->
            rel_path = Path.relative_to(file_path, base_path)
            IO.puts("  ✓ #{rel_path} → #{app_name}")
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
        module_name = Macro.camelize(app_name)

        case find_project_structure_section(content) do
          nil ->
            {:skipped, "no project structure section"}

          _section ->
            case extract_old_challenge_name(content, file_path) do
              nil ->
                {:skipped, "could not extract old challenge name"}

              old_name ->
                if app_name == old_name do
                  {:skipped, "already coherent"}
                else
                  new_content = fix_project_structure_section(
                    content,
                    old_name,
                    app_name,
                    module_name
                  )

                  new_content = fix_executable_example_section(
                    new_content,
                    old_name,
                    app_name,
                    module_name
                  )

                  if new_content != content do
                    File.write!(file_path, new_content)
                    {:fixed, app_name}
                  else
                    {:skipped, "no changes needed"}
                  end
                end
            end
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

  defp find_project_structure_section(content) do
    case Regex.run(~r/## Project structure/, content) do
      [_] -> true
      nil -> nil
    end
  end

  defp extract_old_challenge_name(content, file_path) do
    # Try to extract from the project structure section's first folder line
    case Regex.run(~r/## Project structure\s*\n+```\s*\n([a-z_]+)\//, content) do
      [_full, name] ->
        name

      nil ->
        # Fallback: extract from the folder structure in the path
        parts = Path.split(file_path)
        reverse_parts = Enum.reverse(parts)

        case reverse_parts do
          [file | dir_parts] ->
            if String.ends_with?(file, ".md") do
              case dir_parts do
                [dir | _rest] ->
                  String.replace(dir, "-", "_")

                _ ->
                  nil
              end
            else
              nil
            end

          _ ->
            nil
        end
    end
  end

  defp fix_project_structure_section(content, old_name, app_name, module_name) do
    # Replace root folder
    result = String.replace(content, "#{old_name}/", "#{app_name}/", global: true)

    # Replace lib path
    lib_old_core = "lib/#{old_name}/"
    lib_new = "lib/#{app_name}/"
    result = String.replace(result, lib_old_core, lib_new, global: true)

    # Replace test file reference
    test_old = "#{old_name}_test.exs"
    test_new = "#{app_name}_test.exs"
    result = String.replace(result, test_old, test_new, global: true)

    # Replace module references in structure
    old_module = Macro.camelize(old_name)
    if old_module != module_name do
      result = String.replace(result, old_module, module_name, global: true)
    end

    result
  end

  defp fix_executable_example_section(content, old_name, app_name, module_name) do
    case Regex.run(~r/## Executable Example\s*\n+```elixir(.*?)```/s, content) do
      nil ->
        content

      [full_match, code_section] ->
        new_code = code_section
        new_code = String.replace(new_code, "#{old_name}/", "#{app_name}/", global: true)
        new_code = String.replace(new_code, "lib/#{old_name}/", "lib/#{app_name}/", global: true)
        new_code = String.replace(new_code, "#{old_name}_test.exs", "#{app_name}_test.exs", global: true)

        old_module = Macro.camelize(old_name)
        new_code = if old_module != module_name do
          String.replace(new_code, old_module, module_name, global: true)
        else
          new_code
        end

        if new_code != code_section do
          new_full = String.replace(full_match, code_section, new_code)
          String.replace(content, full_match, new_full)
        else
          content
        end
    end
  end
end

FixProjectStructure.main()
