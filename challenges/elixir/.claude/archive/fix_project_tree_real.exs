#!/usr/bin/env elixir

# Script para sincronizar árboles de proyecto en archivos Markdown con la estructura real del filesystem
# Uso: elixir fix_project_tree_real.exs [--dry-run] [--verbose]

defmodule ProjectTreeFixer do
  @base_path "/Users/consulting/Documents/consulting/infra/challenges/elixir"
  @section_marker "## Project structure"

  def run(opts) do
    {files, dry_run, verbose} = parse_opts(opts)

    if Enum.empty?(files) do
      find_and_process_files(dry_run, verbose)
    else
      process_specified_files(files, dry_run, verbose)
    end
  end

  defp parse_opts(opts) do
    dry_run = Enum.member?(opts, "--dry-run")
    verbose = Enum.member?(opts, "--verbose")
    files = Enum.filter(opts, &String.ends_with?(&1, ".md"))
    {files, dry_run, verbose}
  end

  defp find_and_process_files(dry_run, verbose) do
    IO.puts("Buscando archivos Markdown...")

    md_files =
      @base_path
      |> Path.join("**/*.md")
      |> Path.wildcard()
      |> Enum.sort()

    IO.puts("Encontrados #{length(md_files)} archivos Markdown")

    fixed_count =
      md_files
      |> Enum.reduce(0, fn file, acc ->
        case process_file(file, dry_run, verbose) do
          :fixed -> acc + 1
          :skipped -> acc
          :error -> acc
        end
      end)

    IO.puts("\nResumen:")
    IO.puts("Archivos procesados: #{length(md_files)}")
    IO.puts("Árboles reparados: #{fixed_count}")

    if dry_run do
      IO.puts("(--dry-run: sin cambios reales)")
    end
  end

  defp process_specified_files(files, dry_run, verbose) do
    files
    |> Enum.map(fn file ->
      full_path =
        if String.starts_with?(file, "/") do
          file
        else
          Path.join(@base_path, file)
        end
      process_file(full_path, dry_run, verbose)
    end)
  end

  defp process_file(file_path, dry_run, verbose) do
    if not File.exists?(file_path) do
      IO.puts("ERROR: Archivo no encontrado: #{file_path}")
      :error
    else
      content = File.read!(file_path)

      case process_markdown(file_path, content, verbose) do
        {:ok, new_content} ->
          if new_content != content do
            unless dry_run do
              File.write!(file_path, new_content)
            end
            if verbose do
              IO.puts("FIXED: #{file_path}")
            end
            :fixed
          else
            if verbose do
              IO.puts("SKIP: #{file_path} (sin cambios necesarios)")
            end
            :skipped
          end
        {:error, reason} ->
          if verbose do
            IO.puts("ERROR: #{file_path} - #{reason}")
          end
          :error
      end
    end
  end

  defp process_markdown(file_path, content, verbose) do
    case find_project_structure_section(content) do
      nil ->
        {:ok, content}

      {before, section_line, after_section} ->
        case extract_app_name(content) do
          nil ->
            {:error, "No app name found in mix.exs block"}

          app_name ->
            # Construir ruta esperada del filesystem
            case build_expected_tree(file_path, app_name, verbose) do
              {:ok, tree} ->
                # Reemplazar solo el árbol ASCII (después del marcador y descripción)
                new_section = build_new_section(section_line, after_section, tree, verbose)
                new_content = before <> new_section
                {:ok, new_content}

              {:error, reason} ->
                {:error, reason}
            end
        end
    end
  end

  defp find_project_structure_section(content) do
    lines = String.split(content, "\n")

    case Enum.find_index(lines, fn line -> String.trim(line) == "## Project structure" end) do
      nil -> nil
      idx ->
        before = (lines |> Enum.take(idx) |> Enum.join("\n")) <> "\n"
        section_line = Enum.at(lines, idx)
        after_section = lines |> Enum.drop(idx + 1) |> Enum.join("\n")
        {before, section_line, after_section}
    end
  end

  defp extract_app_name(content) do
    # Método 1: Buscar app: :app_name en mix.exs
    case Regex.run(~r/app:\s*:([a-z_]+)/, content) do
      [_match, app_name] ->
        app_name

      nil ->
        # Método 2: Buscar **Project**: `app_name` en la primera línea
        case Regex.run(~r/\*\*Project\*\*:\s*`([a-z_]+)`/, content) do
          [_match, app_name] -> app_name
          nil -> nil
        end
    end
  end

  defp build_expected_tree(file_path, app_name, verbose) do
    # Ruta: /Users/.../elixir/<nivel>/<topic>/<folder>/<app_name>/
    # El archivo está en: /Users/.../elixir/<nivel>/<topic>/<folder>/<archivo.md>

    file_dir = Path.dirname(file_path)
    app_dir = Path.join(file_dir, app_name)

    if verbose do
      IO.puts("  Buscando estructura en: #{app_dir}")
    end

    if File.exists?(app_dir) and File.dir?(app_dir) do
      subtree = generate_tree(app_dir, app_name, "")
      # Incluir el nombre de la carpeta raíz
      tree = app_name <> "/\n" <> subtree
      {:ok, tree}
    else
      {:error, "Directorio de app no encontrado: #{app_dir}"}
    end
  end

  defp generate_tree(dir, _dir_name, prefix) do
    try do
      entries =
        File.ls!(dir)
        |> Enum.filter(fn entry -> not String.starts_with?(entry, ".") end)
        |> Enum.sort()

      lines =
        entries
        |> Enum.with_index()
        |> Enum.map(fn {entry, idx} ->
          is_last = idx == length(entries) - 1
          entry_path = Path.join(dir, entry)

          if File.dir?(entry_path) do
            # Carpeta
            connector = if is_last, do: "└── ", else: "├── "
            new_prefix = if is_last, do: prefix <> "    ", else: prefix <> "│   "

            subtree = generate_tree(entry_path, entry, new_prefix)
            prefix <> connector <> entry <> "/\n" <> subtree
          else
            # Archivo
            connector = if is_last, do: "└── ", else: "├── "
            prefix <> connector <> entry <> "\n"
          end
        end)

      lines
      |> Enum.join("")
    rescue
      _e -> ""
    end
  end

  defp build_new_section(section_line, after_section, tree, _verbose) do
    # El after_section puede tener descripción, línea vacía, y luego el árbol viejo
    # Buscamos dónde comienza el árbol viejo (primera línea con "├──", "└──", o nombre de carpeta)

    after_lines = String.split(after_section, "\n")

    # Buscar primer índice que tenga contenido de árbol
    tree_start_idx =
      Enum.find_index(after_lines, fn line ->
        trimmed = String.trim(line)
        trimmed != "" and (
          String.contains?(trimmed, "├") or
          String.contains?(trimmed, "└") or
          String.match?(trimmed, ~r/^\w+\/$/)
        )
      end)

    case tree_start_idx do
      nil ->
        # No hay árbol previo, solo agregar después de descripción
        section_line <> "\n" <> after_section <> tree

      idx ->
        # Preservar líneas antes del árbol (descripción)
        preserved = after_lines |> Enum.take(idx) |> Enum.join("\n")
        section = section_line <> "\n" <> preserved

        # Asegurar salto de línea adecuado
        trailing_newline =
          if preserved != "" and not String.ends_with?(preserved, "\n") do
            "\n"
          else
            ""
          end

        section <> trailing_newline <> tree
    end
  end
end

# Punto de entrada
ProjectTreeFixer.run(System.argv())
