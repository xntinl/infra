#!/usr/bin/env elixir

base = "/Users/consulting/Documents/consulting/infra/challenges/elixir"
avanzado = Path.join(base, "03-avanzado")

# Get all .md files in 03-avanzado
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
  |> Enum.filter(fn f ->
    # Only 210+
    case Regex.run(~r/\/(\d+)-/, f) do
      [_match, num_str] ->
        case Integer.parse(num_str) do
          {n, _} -> n >= 210
          :error -> false
        end
      _ -> false
    end
  end)

IO.puts("Files >= 210 in 03-avanzado: #{length(all_md_files)}")

# For each file >= 210, check if it has empty script block
empty_scripts = all_md_files
  |> Enum.filter(fn f ->
    content = File.read!(f)
    case String.split(content, "### `script/main.exs`", parts: 2) do
      [_pre, rest] ->
        case String.split(rest, "```", parts: 3) do
          [_prefix, code, _rest] ->
            trimmed = String.trim(code)
            # Check if it's empty or only contains language specifier
            is_empty = trimmed == "" or trimmed == "exs" or trimmed == "elixir" or
                       String.trim_leading(trimmed) == "exs" or
                       String.trim_leading(trimmed) == "elixir"
            if is_empty do
              IO.puts("EMPTY: #{Path.basename(f)}")
            end
            is_empty
          _ -> false
        end
      _ -> false
    end
  end)

IO.puts("\n=== Summary ===")
IO.puts("Files >= 210: #{length(all_md_files)}")
IO.puts("Files with EMPTY script blocks: #{length(empty_scripts)}")
