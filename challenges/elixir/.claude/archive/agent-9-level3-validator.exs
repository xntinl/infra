#!/usr/bin/env elixir
# Agent 9 Level 3: Syntax Validation and Fix
# Purpose: Validate Elixir code blocks in all .md files and auto-fix syntax errors

defmodule Agent9Level3 do
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

    state = %Agent9Level3{
      files_processed: 0,
      syntax_errors_fixed: 0,
      requires_review: 0,
      errors_log: [],
      base_path: base_path
    }

    # Find all .md files
    md_files = find_markdown_files(base_path)
    IO.puts("Found #{length(md_files)} markdown files")

    # Process each file
    final_state =
      md_files
      |> Enum.reduce(state, fn file, acc ->
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
    |> find_files_recursive("*.md")
  end

  defp find_files_recursive(dir, pattern) do
    case File.ls(dir) do
      {:ok, items} ->
        items
        |> Enum.flat_map(fn item ->
          path = Path.join(dir, item)

          cond do
            File.dir?(path) and not String.starts_with?(item, ".") ->
              find_files_recursive(path, pattern)

            String.match?(item, Regex.compile!(pattern |> String.replace("*", ".*"))) ->
              [path]

            true ->
              []
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
              IO.puts("✓ Fixed: #{file_path}")

              %{
                file_state
                | files_processed: file_state.files_processed + 1
              }

            {:error, reason} ->
              IO.puts("✗ Error writing: #{file_path} - #{inspect(reason)}")

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

      {:error, reason} ->
        IO.puts("✗ Cannot read: #{file_path} - #{inspect(reason)}")

        %{
          state
          | files_processed: state.files_processed + 1,
            errors_log: [{file_path, "Read error: #{inspect(reason)}"} | state.errors_log]
        }
    end
  end

  defp process_content(content, state, file_path) do
    # Extract all Elixir code blocks
    code_blocks = extract_code_blocks(content)

    if Enum.empty?(code_blocks) do
      {content, state}
    else
      # Process each block
      {new_content, block_state} =
        code_blocks
        |> Enum.reduce({content, state}, fn block, {acc_content, acc_state} ->
          process_code_block(block, acc_content, acc_state, file_path)
        end)

      {new_content, block_state}
    end
  end

  defp extract_code_blocks(content) do
    # Find all ```elixir ... ``` blocks
    Regex.scan(~r/```elixir\n(.*?)\n```/s, content)
    |> Enum.map(&Enum.at(&1, 1))
    |> Enum.filter(& &1)
  end

  defp process_code_block(code_block, content, state, file_path) do
    case validate_and_fix_block(code_block) do
      {:ok, fixed_code} ->
        if fixed_code != code_block do
          # Replace in content
          new_content =
            String.replace(content, "```elixir\n#{code_block}\n```",
              "```elixir\n#{fixed_code}\n```",
              global: false
            )

          {new_content,
           %{
             state
             | syntax_errors_fixed: state.syntax_errors_fixed + 1
           }}
        else
          {content, state}
        end

      {:error, reason} ->
        # Log for review
        new_log = [
          {file_path, String.slice(code_block, 0, 100), reason}
          | state.errors_log
        ]

        {content,
         %{
           state
           | requires_review: state.requires_review + 1,
             errors_log: new_log
         }}
    end
  end

  defp validate_and_fix_block(code_block) do
    # First try: validate as-is
    case Code.string_to_quoted(code_block) do
      {:ok, _ast} ->
        {:ok, code_block}

      {:error, _reason1} ->
        # Try to fix by removing trailing `end` if present
        trimmed = String.trim(code_block)

        case String.split(trimmed, "\n") do
          [] ->
            {:error, "Empty block"}

          lines ->
            last_line = List.last(lines)

            if String.trim(last_line) == "end" do
              fixed =
                lines
                |> Enum.drop(-1)
                |> Enum.join("\n")
                |> String.trim()

              case Code.string_to_quoted(fixed) do
                {:ok, _ast} ->
                  {:ok, fixed}

                {:error, reason} ->
                  {:error,
                   "Still invalid after removing trailing end: #{inspect(reason)}"}
              end
            else
              {:error, "Invalid syntax"}
            end
        end
    end
  end

  defp print_summary(state, elapsed) do
    IO.puts("\n" <> String.duplicate("=", 70))
    IO.puts("Agent-9 (NIVEL 3 - Syntax): #{state.files_processed} archivos procesados")
    IO.puts("  Syntax errors fixed: #{state.syntax_errors_fixed}")
    IO.puts("  Requires manual review: #{state.requires_review}")
    IO.puts("  Elapsed time: #{Float.round(elapsed, 2)}s")
    IO.puts(String.duplicate("=", 70) <> "\n")
  end

  defp save_logs(state, base_path) do
    log_dir = Path.join(base_path, ".claude")
    File.mkdir_p!(log_dir)

    # Save main log
    log_file = Path.join(log_dir, "agent-9-level3.log")

    log_content = """
    Agent-9 Level 3 Syntax Validation Log
    Generated: #{DateTime.utc_now()}

    Summary:
    - Files processed: #{state.files_processed}
    - Syntax errors fixed: #{state.syntax_errors_fixed}
    - Requires review: #{state.requires_review}

    Errors requiring manual review:
    #{format_errors(state.errors_log)}
    """

    File.write!(log_file, log_content)
    IO.puts("Log saved to: #{log_file}")

    # Save detailed errors file if there are any
    if state.requires_review > 0 do
      errors_file = Path.join(log_dir, "syntax-errors.md")

      errors_content = """
      # Syntax Errors Requiring Manual Review

      Generated: #{DateTime.utc_now()}

      Total requiring review: #{state.requires_review}

      #{format_detailed_errors(state.errors_log)}
      """

      File.write!(errors_file, errors_content)
      IO.puts("Detailed errors saved to: #{errors_file}")
    end
  end

  defp format_errors(errors) do
    errors
    |> Enum.reverse()
    |> Enum.map(fn
      {file, preview, reason} ->
        "  - #{file}\n    Preview: #{preview}\n    Reason: #{reason}"

      {file, reason} ->
        "  - #{file}: #{reason}"
    end)
    |> Enum.join("\n")
  end

  defp format_detailed_errors(errors) do
    errors
    |> Enum.reverse()
    |> Enum.map(fn
      {file, preview, reason} ->
        """
        ## #{file}

        **Preview:** `#{preview}`

        **Reason:** #{reason}
        """

      {file, reason} ->
        """
        ## #{file}

        **Reason:** #{reason}
        """
    end)
    |> Enum.join("\n\n")
  end
end

# Run the agent
Agent9Level3.run(".")
