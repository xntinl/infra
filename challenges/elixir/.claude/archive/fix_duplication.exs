defmodule FixDuplication do
  @moduledoc """
  Removes module duplication from Executable Example sections.

  Finds cases where the main module appears twice:
  1. In Implementation (### `lib/module.ex`)
  2. In Executable Example (should only have Main)

  Keeps only the Main block, removes the duplicated module definition.
  """

  def main do
    IO.puts("=== Removing Module Duplication ===\n")

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
      acc
    else
      md_files = find_md_files(dir_path)

      Enum.reduce(md_files, acc, fn file_path, acc_inner ->
        case fix_file(file_path) do
          {:fixed, _} ->
            %{acc_inner | fixed: acc_inner.fixed + 1}

          {:skipped, _} ->
            %{acc_inner | skipped: acc_inner.skipped + 1}

          {:error, _} ->
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
        {:skipped, "no app name"}

      app_name ->
        new_content = remove_duplicated_module(content, app_name)

        if new_content != content do
          File.write!(file_path, new_content)
          {:fixed, app_name}
        else
          {:skipped, "already clean"}
        end
    end
  rescue
    _e ->
      {:skipped, "error"}
  end

  defp extract_app_name(content) do
    case Regex.run(~r/app:\s*:([a-z_]+)/m, content) do
      [_full, app_name] -> app_name
      nil -> nil
    end
  end

  # Remove duplicated module from ### `script/main.exs` section
  # Keep only the Main block
  defp remove_duplicated_module(content, app_name) do
    # Pattern: find ### `script/main.exs` section
    # Then find the first defmodule (the duplicate main module)
    # Remove it, keep only defmodule Main do ... end

    case Regex.run(~r/(### `script\/main\.exs`.*?\n```elixir\n)(defmodule\s+\w+\s+do\s+(?:(?!^end\s*$).)*^end\s*\n\n)(defmodule\s+Main\s+do.*?^end\s*\n\n)/ms, content) do
      [full_match, heading_part, _dup_module, main_part] ->
        # Keep heading and Main, remove the duplicate module
        replacement = heading_part <> main_part
        String.replace(content, full_match, replacement, global: false)

      nil ->
        # Try simpler pattern: just remove any module that's not Main before Main
        case Regex.run(~r/(### `script\/main\.exs`.*?```elixir\n)(defmodule (?!Main)[A-Z]\w* do.*?end\s*\n\n)+(defmodule Main do)/s, content) do
          [full_match, before_main, _duplicates, main_start] ->
            replacement = before_main <> main_start
            String.replace(content, full_match, replacement, global: false)

          nil ->
            content
        end
    end
  end
end

FixDuplication.main()
