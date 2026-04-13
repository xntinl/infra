#!/usr/bin/env elixir

base = "/Users/consulting/Documents/consulting/infra/challenges/elixir"
insane = Path.join(base, "04-insane")

# Get all .md files in 04-insane
all_md_files =
  File.ls!(insane)
  |> Enum.flat_map(fn category ->
    cat_path = Path.join(insane, category)
    case File.ls(cat_path) do
      {:ok, subdirs} ->
        subdirs
        |> Enum.flat_map(fn subdir ->
          dir_path = Path.join(cat_path, subdir)
          case File.ls(dir_path) do
            {:ok, files} ->
              files
              |> Enum.filter(&String.ends_with?(&1, ".md"))
              |> Enum.map(&Path.join(dir_path, &1))
            {:error, _} -> []
          end
        end)
      {:error, _} -> []
    end
  end)

IO.puts("Total .md files in 04-insane: #{length(all_md_files)}")

# Check which ones have script/main.exs
with_heading = all_md_files
  |> Enum.filter(fn f ->
    case File.read(f) do
      {:ok, content} ->
        String.contains?(content, "### `script/main.exs`")
      {:error, _} -> false
    end
  end)

IO.puts("Files with script/main.exs heading: #{length(with_heading)}")
with_heading |> Enum.take(5) |> Enum.each(&IO.puts("  - #{Path.basename(&1)}"))

# Check which ones have empty script blocks
IO.puts("\n=== Checking for empty/placeholder script blocks ===")
empty_scripts = with_heading
  |> Enum.filter(fn f ->
    content = File.read!(f)
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [_pre, rest] ->
        case String.split(rest, "```", parts: 3) do
          [_prefix, code, _rest] ->
            trimmed = String.trim(code)
            # Check if it's empty or only contains language specifier or placeholder text
            is_empty = trimmed == "" or
                       trimmed == "exs" or
                       trimmed == "elixir" or
                       String.trim_leading(trimmed) == "exs" or
                       String.trim_leading(trimmed) == "elixir" or
                       String.starts_with?(trimmed, "# TODO") or
                       String.starts_with?(trimmed, "# Placeholder")
            if is_empty do
              IO.puts("  EMPTY/PLACEHOLDER: #{Path.basename(f)}")
            end
            is_empty
          [_prefix, code] ->
            # No closing ```
            trimmed = String.trim(code)
            is_empty = trimmed == "" or trimmed == "exs" or trimmed == "elixir"
            if is_empty do
              IO.puts("  EMPTY (no closing ```): #{Path.basename(f)}")
            end
            is_empty
          _ -> false
        end
      _ -> false
    end
  end)

IO.puts("\n=== Summary ===")
IO.puts("Total files: #{length(all_md_files)}")
IO.puts("Files with heading: #{length(with_heading)}")
IO.puts("Files with EMPTY/PLACEHOLDER blocks: #{length(empty_scripts)}")

if empty_scripts != [] do
  IO.puts("\nEmpty scripts to process:")
  empty_scripts |> Enum.each(&IO.puts("  - #{&1}"))
end
