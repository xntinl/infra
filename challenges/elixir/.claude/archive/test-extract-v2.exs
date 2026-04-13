#!/usr/bin/env elixir

test_path = "/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/metaprogramming/388-compile-time-config-validation-on-load/388-compile-time-config-validation-on-load.md"

# Get parent dir
parent_dir = Path.dirname(test_path) |> Path.basename()
IO.puts("Parent dir: #{parent_dir}")

# Extract number
case Regex.run(~r/^(\d+)-/, parent_dir) do
  [match, num_str] ->
    IO.puts("Match: #{match}")
    IO.puts("Num str: #{num_str}")
    case Integer.parse(num_str) do
      {n, _} ->
        IO.puts("Number: #{n}")
        IO.puts("Is >= 210? #{n >= 210}")
      :error ->
        IO.puts("Parse error")
    end
  _ ->
    IO.puts("No match found")
end

# Now test with the whole list
base = "/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado"
files =
  File.ls!(base)
  |> Enum.flat_map(fn category ->
    cat_path = Path.join(base, category)
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

IO.puts("\nTotal files in 03-avanzado: #{length(files)}")

# Extract numbers and filter
files_210_plus = files
  |> Enum.filter(fn f ->
    parent_dir = Path.dirname(f) |> Path.basename()
    case Regex.run(~r/^(\d+)-/, parent_dir) do
      [_match, num_str] ->
        case Integer.parse(num_str) do
          {n, _} -> n >= 210
          :error -> false
        end
      _ -> false
    end
  end)

IO.puts("Files 210+: #{length(files_210_plus)}")
files_210_plus |> Enum.take(5) |> Enum.each(&IO.puts("  - #{Path.basename(&1)}"))
