defmodule FixStructureAndHeadings do
  @moduledoc """
  Fixes remaining structural issues:
  1. Add script/main.exs to project trees (was missing)
  2. Change "### Tests" → "### `test/<app>_test.exs`"
  3. Fix incomplete mix.exs blocks
  """

  def main do
    IO.puts("=== Fixing Structure and Headings ===\n")

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
            rel_path = Path.relative_to(file_path, base_path)
            IO.puts("  ✓ #{rel_path}")
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
        new_content = content
        new_content = add_script_to_tree(new_content)
        new_content = fix_test_heading(new_content, app_name)
        new_content = fix_mix_exs_block(new_content)

        if new_content != content do
          File.write!(file_path, new_content)
          {:fixed, app_name}
        else
          {:skipped, "already fixed"}
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

  # Add script/main.exs to project tree if missing
  defp add_script_to_tree(content) do
    if String.contains?(content, "script/") and String.contains?(content, "main.exs") do
      content
    else
      String.replace(
        content,
        ~r/(\│   └── test\/.*?\n)(└── mix\.exs)/m,
        "\\1├── script/\n│   └── main.exs\n\\2"
      )
    end
  end

  # Change "### Tests" → "### `test/<app>_test.exs`"
  defp fix_test_heading(content, app_name) do
    test_file = "test/#{app_name}_test.exs"
    String.replace(content, "### Tests", "### `#{test_file}`")
  end

  # Fix incomplete mix.exs blocks
  defp fix_mix_exs_block(content) do
    if String.contains?(content, "defmodule") and String.contains?(content, "defp deps do") do
      # Already has complete mix.exs
      content
    else
      # Look for incomplete mix.exs (just defp deps without module wrapper)
      incomplete_pattern = ~r/(### `mix\.exs`\s*```elixir\s*)\n(defp deps do\s*\[)/m

      if String.match?(content, incomplete_pattern) do
        # Extract app name and build complete module
        case Regex.run(~r/app:\s*:([a-z_]+)/m, content) do
          [_full, app_name] ->
            module_name = Macro.camelize(app_name)

            complete_block = """
            defmodule #{module_name}.MixProject do
              use Mix.Project

              def project do
                [
                  app: :#{app_name},
                  version: "0.1.0",
                  elixir: "~> 1.14",
                  start_permanent: Mix.env() == :prod,
                  deps: deps()
                ]
              end

              def application do
                [
                  extra_applications: [:logger]
                ]
              end

              defp deps do
                [
                  # Standard library: no external dependencies required
                ]
              end
            end
            """

            String.replace(
              content,
              incomplete_pattern,
              "\\1\n#{complete_block}\n"
            )

          nil ->
            content
        end
      else
        content
      end
    end
  end
end

FixStructureAndHeadings.main()
