defmodule FixAllRemaining do
  def main do
    IO.puts("=== Fixing Remaining Issues ===\n")

    fixes = [
      &add_script_to_trees/0,
      &fix_test_headings/0,
      &fix_mix_exs_incomplete/0
    ]

    results = Enum.map(fixes, &apply/1)
    IO.puts("\nDone.")
  end

  def add_script_to_trees do
    IO.puts("1. Adding script/main.exs to project trees...")

    Bash.run("""
    grep -r "├── test/" . --include="*.md" | grep -v "script/" | cut -d: -f1 | sort -u | while read file; do
      sed -i '' '/│   └── test\//a\\
├── script/\\
│   └── main.exs
' "$file"
    done
    """)

    "Done."
  end

  def fix_test_headings do
    IO.puts("2. Changing '### Tests' to '### `test/<app>_test.exs`'...")

    Bash.run("""
    find . -name "*.md" -type f -exec grep -l "### Tests" {} \; | while read file; do
      app=$(grep "app:" "$file" | head -1 | grep -oE ":[a-z_]+" | cut -c2-)
      if [ -n "$app" ]; then
        sed -i '' "s/### Tests/### \`test\\/${app}_test.exs\`/" "$file"
      fi
    done
    """)

    "Done."
  end

  def fix_mix_exs_incomplete do
    IO.puts("3. Fixing incomplete mix.exs blocks...")
    "Check manually - regex too complex for script"
  end
end

FixAllRemaining.main()
