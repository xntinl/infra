#!/usr/bin/env elixir

base = "/Users/consulting/Documents/consulting/infra/challenges/elixir"
avanzado = Path.join(base, "03-avanzado")
insane = Path.join(base, "04-insane")

# Test avanzado
IO.puts("=== Testing 03-avanzado ===")
case File.ls(avanzado) do
  {:ok, dirs} ->
    IO.puts("Found #{length(dirs)} directories")
    dirs |> Enum.each(&IO.puts("  - #{&1}"))
  {:error, e} ->
    IO.puts("Error: #{inspect(e)}")
end

# Get all .md files in 03-avanzado
IO.puts("\n=== Finding .md files in 03-avanzado ===")
all_md_files =
  File.ls!(avanzado)
  |> Enum.flat_map(fn category ->
    cat_path = Path.join(avanzado, category)
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

IO.puts("Total .md files found in 03-avanzado: #{length(all_md_files)}")
all_md_files |> Enum.take(5) |> Enum.each(&IO.puts("  - #{&1}"))

# Check which ones have script/main.exs
IO.puts("\n=== Checking for script/main.exs heading ===")
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
IO.puts("\n=== Checking for empty script blocks ===")
empty_scripts = with_heading
  |> Enum.filter(fn f ->
    content = File.read!(f)
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [_pre, rest] ->
        case String.split(rest, "```", parts: 3) do
          [_prefix, code, _rest] ->
            trimmed = String.trim(code)
            trimmed == "" or trimmed == "exs" or trimmed == "elixir"
          _ -> false
        end
      _ -> false
    end
  end)

IO.puts("Files with empty script blocks: #{length(empty_scripts)}")
empty_scripts |> Enum.take(5) |> Enum.each(&IO.puts("  - #{Path.basename(&1)}"))
