#!/usr/bin/env elixir

test_path = "/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/metaprogramming/388-compile-time-config-validation-on-load/388-compile-time-config-validation-on-load.md"

case Regex.run(~r/\/(\d+)-/, test_path) do
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
    IO.puts("No match")
end
