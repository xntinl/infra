#!/usr/bin/env elixir
# Agent 9 Level 3: Final Syntax Validation and Fix
# Purpose: Detect and fix orphaned `end` statements in Elixir code blocks
# Strategy:
#   1. Extract all elixir blocks
#   2. Try to parse them
#   3. If parse fails BUT succeeds after removing trailing `end`, fix it
#   4. Report anything else that fails

defmodule Agent9Final do
  require Logger

  defstruct [
    :files_processed,
    :syntax_errors_fixed,
    :requires_review,
    :errors_log,
    :base_path
  ]

  def run(base_path \\ ".") do
    start_time = DateTime.utc_now()

    state = %Agent9Final{
      files_processed: 0,
      syntax_errors_fixed: 0,
      requires_review: 0,
      errors_log: [],
      base_path: base_path
    }

    # Find all .md files
    md_files = find_markdown_files(base_path)
    IO.puts("Found #{length(md_files)} markdown files to process")

    # Process each file
    final_state =
      md_files
      |> Enum.with_index()
      |> Enum.reduce(state, fn {file, idx}, acc ->
        if rem(idx, 50) == 0 do
          IO.puts("  Progress: #{idx}/#{length(md_files)}")
        end

        process_file(file, acc)
      end)

    # Generate reports
    end_time = DateTime.utc_now()
    elapsed = DateTime.diff(end_time, start_time, :millisecond) / 1000

    print_summary(final_state, elapsed)
    save_logs(final_state, base_path)

    final_state
  end

  defp find_markdown_files(base_path) do
    base_path
    |> Path.expand()
    |> find_files_recursive()
    |> Enum.filter(&String.ends_with?(&1, ".md"))
    |> Enum.sort()
  end

  defp find_files_recursive(dir) do
    case File.ls(dir) do
      {:ok, items} ->
        items
        |> Enum.flat_map(fn item ->
          path = Path.join(dir, item)

          cond do
            File.dir?(path) and not String.starts_with?(item, ".") ->
              find_files_recursive(path)

            true ->
              [path]
          end
        end)

      {:error, _} ->
        []
    end
  end

  defp process_file(file_path, state) do
    case File.read(file_path) do
      {:ok, content} ->
        {new_content, file_state} = process_content(content, state, file_path)

        if new_content != content do
          case File.write(file_path, new_content) do
            :ok ->
              IO.puts("✓ Fixed: #{String.slice(file_path, -60..-1)}")

              %{
                file_state
                | files_processed: file_state.files_processed + 1
              }

            {:error, reason} ->
              IO.puts("✗ Write error: #{file_path}")

              %{
                file_state
                | files_processed: file_state.files_processed + 1,
                  errors_log: [
                    {file_path, "Write error: #{inspect(reason)}"}
                    | file_state.errors_log
                  ]
              }
          end
        else
          %{file_state | files_processed: file_state.files_processed + 1}
        end

      {:error, _reason} ->
        %{
          state
          | files_processed: state.files_processed + 1
        }
    end
  end

  defp process_content(content, state, file_path) do
    # Find all ```elixir...``` blocks
    # Use a regex that captures the block content
    regex = ~r/```elixir\n(.*?)\n```/s

    matches = Regex.scan(regex, content, return: :index)

    if Enum.empty?(matches) do
      {content, state}
    else
      # Process matches in reverse order to maintain string positions
      {new_content, new_state} =
        matches
        |> Enum.reverse()
        |> Enum.reduce({content, state}, fn match, {acc_content, acc_state} ->
          process_match(match, acc_content, acc_state, file_path)
        end)

      {new_content, new_state}
    end
  end

  defp process_match(match, content, state, file_path) do
    case match do
      [{start, full_len}, {code_start, code_len}] ->
        code = String.slice(content, code_start, code_len)

        case validate_and_fix(code) do
          {:ok, fixed_code} ->
            if fixed_code != code do
              # Replace the code portion
              before = String.slice(content, 0, code_start)
              end_pos = code_start + code_len

              rest =
                if end_pos < String.length(content) do
                  String.slice(content, end_pos..-1)
                else
                  ""
                end

              new_content = before <> fixed_code <> rest

              {new_content,
               %{
                 state
                 | syntax_errors_fixed: state.syntax_errors_fixed + 1
               }}
            else
              {content, state}
            end

          {:error, reason} ->
            {content,
             %{
               state
               | requires_review: state.requires_review + 1,
                 errors_log: [
                   {file_path, String.slice(code, 0, 60), reason}
                   | state.errors_log
                 ]
             }}
        end

      _ ->
        {content, state}
    end
  end

  defp validate_and_fix(code) do
    # Try parsing as-is
    case Code.string_to_quoted(code) do
      {:ok, _ast} ->
        # Already valid
        {:ok, code}

      {:error, _error1} ->
        # Parsing failed. Try removing trailing `end`
        case remove_trailing_end(code) do
          {:ok, fixed_code} ->
            # Check if it parses now
            case Code.string_to_quoted(fixed_code) do
              {:ok, _ast} ->
                # Success! It was an orphaned `end`
                {:ok, fixed_code}

              {:error, _error2} ->
                # Still doesn't parse, not a simple orphaned `end` issue
                {:error, "Invalid syntax (not just orphaned end)"}
            end

          :no_change ->
            {:error, "Invalid syntax"}
        end
    end
  end

  defp remove_trailing_end(code) do
    lines = String.split(code, "\n")

    case Enum.reverse(lines) do
      [] ->
        :no_change

      [last | rest] ->
        if String.trim(last) == "end" do
          fixed = rest |> Enum.reverse() |> Enum.join("\n") |> String.trim()

          if String.fixed != [] do
            {:ok, fixed}
          else
            :no_change
          end
        else
          :no_change
        end
    end
  end

  defp print_summary(state, elapsed) do
    IO.puts("\n" <> String.duplicate("=", 70))
    IO.puts("Agent-9 (NIVEL 3 - Syntax Validation and Fix)")
    IO.puts("Files processed: #{state.files_processed}")
    IO.puts("Syntax errors fixed: #{state.syntax_errors_fixed}")
    IO.puts("Requires manual review: #{state.requires_review}")
    IO.puts("Elapsed time: #{Float.round(elapsed, 2)}s")
    IO.puts(String.duplicate("=", 70) <> "\n")
  end

  defp save_logs(state, base_path) do
    log_dir = Path.join(base_path, ".claude")
    File.mkdir_p!(log_dir)

    log_file = Path.join(log_dir, "agent-9-level3.log")

    log_content = """
    Agent-9 Level 3 - Syntax Validation and Fix
    Generated: #{DateTime.utc_now()}

    SUMMARY:
    --------
    Files processed: #{state.files_processed}
    Syntax errors fixed: #{state.syntax_errors_fixed}
    Requires manual review: #{state.requires_review}

    Errors requiring manual review (#{min(100, state.requires_review)} of #{state.requires_review}}):
    #{format_errors(state.errors_log)}
    """

    File.write!(log_file, log_content)
    IO.puts("Log saved to: #{log_file}")

    if state.requires_review > 0 do
      errors_file = Path.join(log_dir, "syntax-errors.md")

      errors_md = """
      # Syntax Errors Requiring Manual Review

      Generated: #{DateTime.utc_now()}

      **Total issues:** #{state.requires_review}

      #{format_errors_markdown(state.errors_log)}
      """

      File.write!(errors_file, errors_md)
      IO.puts("Errors file saved to: #{errors_file}")
    end
  end

  defp format_errors(errors) do
    errors
    |> Enum.reverse()
    |> Enum.take(100)
    |> Enum.map(fn {file, preview, reason} ->
      "- #{file}\n  Preview: #{preview}\n  Reason: #{reason}"
    end)
    |> Enum.join("\n\n")
  end

  defp format_errors_markdown(errors) do
    errors
    |> Enum.reverse()
    |> Enum.take(500)
    |> Enum.map(fn {file, preview, reason} ->
      "## #{file}\n**Preview:** `#{preview}`\n**Reason:** #{reason}"
    end)
    |> Enum.join("\n\n")
  end
end

# Execute
Agent9Final.run(".")
