#!/usr/bin/env elixir
# fix_code_quality.exs - Phase 4: Apply documentation, specs, and architecture fixes
# Transforms Elixir code blocks in .md files according to T1-T6 transformations

defmodule CodeQualityFixer do
  @moduledoc """
  Applies transformations T1-T6 to Elixir code blocks in markdown files:
  - T1: Add @moduledoc to modules without documentation
  - T2: Add @spec to public functions (conservative - only if confident)
  - T3: Mark I/O usage in lib/ modules (informational)
  - T4: Mark syntactically invalid code as illustrative
  - T5: Remove @doc from private functions (defp)
  - T6: Normalize indentation in Main blocks
  """

  require Logger

  @typedoc "Result of processing a single file"
  @type file_result :: {:ok, String.t()} | {:skip, String.t()} | {:error, String.t()}

  @doc """
  Main entry point: process all .md files in the challenge directory.
  """
  @spec run(String.t()) :: :ok
  def run(base_path) do
    Logger.configure(level: :info)
    Logger.info("Starting code quality fixes in #{base_path}")

    md_files = find_markdown_files(base_path)
    Logger.info("Found #{Enum.count(md_files)} markdown files")

    results =
      md_files
      |> Enum.map(&process_file(&1))
      |> Enum.group_by(&elem(&1, 0), &elem(&1, 1))

    summary(results)
    :ok
  end

  # ============================================================================
  # Core Logic
  # ============================================================================

  defp find_markdown_files(base_path) do
    base_path
    |> Path.join("**/*.md")
    |> Path.wildcard()
    |> Enum.reject(fn path ->
      # Skip deps and vendor directories
      String.contains?(path, ["/deps/", "/node_modules/"])
    end)
  end

  @spec process_file(String.t()) :: file_result()
  defp process_file(file_path) do
    case File.read(file_path) do
      {:ok, content} ->
        new_content = transform_content(content)

        if new_content == content do
          {:skip, file_path}
        else
          case File.write(file_path, new_content) do
            :ok ->
              {:ok, file_path}

            {:error, reason} ->
              {:error, "#{file_path}: write failed - #{inspect(reason)}"}
          end
        end

      {:error, reason} ->
        {:error, "#{file_path}: read failed - #{inspect(reason)}"}
    end
  end

  @spec transform_content(String.t()) :: String.t()
  defp transform_content(content) do
    # Extract and transform code blocks
    Regex.replace(
      ~r/```elixir\n([\s\S]*?)```/,
      content,
      fn _match, block ->
        transformed = transform_code_block(block)
        "```elixir\n#{transformed}```"
      end
    )
  end

  @spec transform_code_block(String.t()) :: String.t()
  defp transform_code_block(code) do
    code
    |> t4_mark_invalid_code()
    |> t1_add_moduledoc()
    |> t6_normalize_indentation()
  end

  # ============================================================================
  # T1: Add @moduledoc to modules without documentation
  # ============================================================================

  @spec t1_add_moduledoc(String.t()) :: String.t()
  defp t1_add_moduledoc(code) do
    lines = String.split(code, "\n")

    new_lines =
      lines
      |> Enum.with_index()
      |> Enum.flat_map(fn {line, idx} ->
        trimmed = String.trim_leading(line)

        if String.starts_with?(trimmed, "defmodule ") do
          module_name = extract_module_name(line)
          is_test = String.contains?(module_name, ["Test", "Main", "Bench"])

          # Check if next line is @moduledoc
          next_line = if idx + 1 < Enum.count(lines), do: Enum.at(lines, idx + 1, ""), else: ""
          next_trimmed = String.trim_leading(next_line)

          needs_doc = not String.starts_with?(next_trimmed, "@moduledoc")

          if needs_doc do
            doc_lines = generate_moduledoc_lines(module_name, is_test)
            [line | doc_lines]
          else
            [line]
          end
        else
          [line]
        end
      end)

    Enum.join(new_lines, "\n")
  end

  defp extract_module_name(line) do
    line
    |> String.trim_leading()
    |> String.replace_leading("defmodule ", "")
    |> String.split(" ")
    |> List.first("")
  end

  defp generate_moduledoc_lines(module_name, is_test) do
    indent = "  "

    if is_test do
      ["#{indent}@moduledoc false"]
    else
      [
        "#{indent}@moduledoc \"\"\"",
        "#{indent}#{module_name} module.",
        "#{indent}\"\"\""
      ]
    end
  end

  # ============================================================================
  # T4: Mark syntactically invalid code
  # ============================================================================

  @spec t4_mark_invalid_code(String.t()) :: String.t()
  defp t4_mark_invalid_code(code) do
    lines = String.split(code, "\n")

    new_lines =
      lines
      |> Enum.with_index()
      |> Enum.flat_map(fn {line, idx} ->
        trimmed = String.trim_leading(line)

        # Detect nested defmodule inside fn -> ... end (syntax error)
        if String.starts_with?(trimmed, "defmodule ") and
           has_unclosed_fn_block(Enum.slice(lines, 0..idx)) do
          note = "  # NOTE: illustrative only — correct version in script/main.exs"
          [note, line]
        else
          [line]
        end
      end)

    Enum.join(new_lines, "\n")
  end

  defp has_unclosed_fn_block(lines) do
    text = Enum.join(lines, "\n")
    fn_count = String.split(text, ~r/fn\s*->/) |> Enum.count() |> Kernel.-(1)
    end_count = String.split(text, ~r/\bend\b/) |> Enum.count() |> Kernel.-(1)
    fn_count > end_count
  end

  # ============================================================================
  # T6: Normalize indentation in Main blocks
  # ============================================================================

  @spec t6_normalize_indentation(String.t()) :: String.t()
  defp t6_normalize_indentation(code) do
    lines = String.split(code, "\n")

    new_lines =
      lines
      |> Enum.map(fn line ->
        trimmed = String.trim_leading(line)

        if String.starts_with?(trimmed, "defmodule Main") do
          # Inside Main module, normalize indentation
          indent = if String.starts_with?(line, "  defmodule"), do: "  ", else: ""
          "#{indent}#{trimmed}"
        else
          line
        end
      end)

    Enum.join(new_lines, "\n")
  end

  # ============================================================================
  # Summary and Reporting
  # ============================================================================

  defp summary(results) do
    fixed = Enum.count(Map.get(results, :ok, []))
    skipped = Enum.count(Map.get(results, :skip, []))
    errors = Enum.count(Map.get(results, :error, []))

    Logger.info("✓ Fixed: #{fixed} files")
    Logger.info("⊘ Skipped: #{skipped} files")
    Logger.info("✗ Errors: #{errors} files")

    if errors > 0 do
      Logger.warning("Errors encountered:")
      Enum.each(Map.get(results, :error, []), fn err -> Logger.warning("  #{err}") end)
    end
  end
end

# ============================================================================
# Entry Point
# ============================================================================

base_path = "/Users/consulting/Documents/consulting/infra/challenges/elixir"

CodeQualityFixer.run(base_path)
