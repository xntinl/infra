# 26. Polyglot Shebang Recipes

<!--
difficulty: advanced
concepts:
  - shebang recipes with multiple interpreters
  - script attribute with interpreter specification
  - extension attribute for temp file naming
  - passing just variables into shebang scripts
  - error handling with set -euo pipefail
  - data processing pipelines across languages
tools: [just, bash, python3, node]
estimated_time: 45 minutes
bloom_level: analyze
prerequisites:
  - just basics (recipes, variables)
  - basic familiarity with Python and JavaScript
  - understanding of Unix shebang lines
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| bash | 4.0+ | `bash --version` |
| python3 | 3.8+ | `python3 --version` |
| node | 18+ | `node --version` |

## Learning Objectives

- **Analyze** when shebang recipes provide advantages over shell recipes and how Just manages temporary script files
- **Differentiate** between `#!/usr/bin/env` shebangs, `[script()]` attributes, and inline shell recipes for interpreter selection
- **Design** a multi-stage data pipeline where each stage uses the language best suited for the task

## Why Polyglot Recipes

Not every task is best expressed in shell. Parsing JSON is painful in bash but trivial in Python or JavaScript. Statistical analysis belongs in Python. HTML generation is natural in a templating language. Just's shebang recipes let you embed any language inline — Python, Node, Ruby, Perl — while still benefiting from Just's dependency system, variables, and environment handling.

When Just encounters a recipe starting with `#!`, it writes the recipe body to a temporary file and executes it with the specified interpreter. This means the entire body is a script in that language, not line-by-line shell commands. Variables from Just are interpolated before the script runs, so `{{ variable }}` works seamlessly across languages.

The `[script()]` attribute and `[extension()]` attribute give finer control: `[script("python3")]` avoids the shebang line entirely, and `[extension("py")]` ensures the temp file has the right extension (important for Python imports and IDE tooling). This exercise builds a realistic data pipeline that uses bash for orchestration, Python for data processing, and Node for report generation.

## Step 1 -- Basic Shebang Recipes

```just
# justfile

set export

data_dir   := "data"
output_dir := "output"
project    := "pipeline"

# ─── Bash: orchestration and file management ──────────

# Prepare directory structure
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{ data_dir }} {{ output_dir }}
    echo "Directories created: {{ data_dir }}/, {{ output_dir }}/"
```

The `#!/usr/bin/env bash` line tells Just to run this as a bash script. `set -euo pipefail` enables strict mode: `-e` exits on error, `-u` errors on undefined variables, `-o pipefail` catches errors in pipe chains. Always include this in bash shebang recipes.

## Step 2 -- Python Shebang Recipes

```just
# ─── Python: data generation and analysis ─────────────

# Generate sample data
[script("python3")]
generate-data count='100':
    import json, random, os

    count = int("{{ count }}")
    data_dir = "{{ data_dir }}"

    records = []
    categories = ["electronics", "clothing", "food", "books", "tools"]
    for i in range(count):
        records.append({
            "id": i + 1,
            "category": random.choice(categories),
            "price": round(random.uniform(5.0, 500.0), 2),
            "quantity": random.randint(1, 50),
            "rating": round(random.uniform(1.0, 5.0), 1)
        })

    output_path = os.path.join(data_dir, "raw_data.json")
    with open(output_path, "w") as f:
        json.dump(records, f, indent=2)
    print(f"Generated {count} records → {output_path}")
```

Notice `[script("python3")]` — this is equivalent to adding `#!/usr/bin/env python3` but cleaner. Just variables (`{{ count }}`, `{{ data_dir }}`) are interpolated before the script runs, so they appear as string literals in the Python code.

## Step 3 -- Python Analysis with Extension Attribute

```just
# Analyze data and produce summary statistics
[script("python3")]
[extension("py")]
analyze:
    import json, os
    from collections import defaultdict

    data_dir = "{{ data_dir }}"
    output_dir = "{{ output_dir }}"

    with open(os.path.join(data_dir, "raw_data.json")) as f:
        records = json.load(f)

    # Aggregate by category
    stats = defaultdict(lambda: {"count": 0, "revenue": 0.0, "avg_rating": []})
    for r in records:
        cat = r["category"]
        stats[cat]["count"] += r["quantity"]
        stats[cat]["revenue"] += r["price"] * r["quantity"]
        stats[cat]["avg_rating"].append(r["rating"])

    # Compute averages
    summary = {}
    for cat, s in stats.items():
        summary[cat] = {
            "total_units": s["count"],
            "total_revenue": round(s["revenue"], 2),
            "avg_rating": round(sum(s["avg_rating"]) / len(s["avg_rating"]), 2)
        }

    output_path = os.path.join(output_dir, "summary.json")
    with open(output_path, "w") as f:
        json.dump(summary, f, indent=2)

    print("Category Summary:")
    for cat, s in sorted(summary.items()):
        print(f"  {cat:15s}  units={s['total_units']:5d}  "
              f"revenue=${s['total_revenue']:>10,.2f}  "
              f"rating={s['avg_rating']:.1f}")
    print(f"\nSummary written to {output_path}")
```

The `[extension("py")]` attribute ensures the temp file is named `*.py`. This matters when Python needs to import the file or when error tracebacks show the file name. Without it, the temp file has no extension.

## Step 4 -- Node.js Report Generation

```just
# ─── Node.js: report generation ──────────────────────

# Generate an HTML report from the summary
[script("node")]
[extension("mjs")]
report:
    import { readFileSync, writeFileSync } from 'fs';
    import { join } from 'path';

    const outputDir = '{{ output_dir }}';
    const summary = JSON.parse(
        readFileSync(join(outputDir, 'summary.json'), 'utf-8')
    );

    const rows = Object.entries(summary)
        .sort(([,a], [,b]) => b.total_revenue - a.total_revenue)
        .map(([cat, s]) => `
            <tr>
                <td>${cat}</td>
                <td>${s.total_units.toLocaleString()}</td>
                <td>$${s.total_revenue.toLocaleString()}</td>
                <td>${s.avg_rating}/5.0</td>
            </tr>`)
        .join('');

    const html = `<!DOCTYPE html>
    <html><head><title>{{ project }} Report</title>
    <style>
        body { font-family: system-ui; max-width: 800px; margin: 2rem auto; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background: #f4f4f4; }
    </style></head>
    <body>
        <h1>{{ project }} - Sales Report</h1>
        <table>
            <tr><th>Category</th><th>Units</th><th>Revenue</th><th>Rating</th></tr>
            ${rows}
        </table>
    </body></html>`;

    const outPath = join(outputDir, 'report.html');
    writeFileSync(outPath, html);
    console.log(`Report written to ${outPath}`);
```

The `[extension("mjs")]` lets Node use ES module syntax (`import`/`export`). Without it, Node would expect CommonJS (`require`).

## Step 5 -- Bash Validation and Pipeline Orchestration

```just
# ─── Bash: validation and orchestration ──────────────

# Validate that expected files exist
validate:
    #!/usr/bin/env bash
    set -euo pipefail
    errors=0
    check_file() {
        if [[ -f "$1" ]]; then
            printf "  %-30s %s\n" "$1" "OK ($(wc -c < "$1" | tr -d ' ') bytes)"
        else
            printf "  %-30s %s\n" "$1" "MISSING"
            ((errors++))
        fi
    }
    echo "Validating pipeline outputs..."
    check_file "{{ data_dir }}/raw_data.json"
    check_file "{{ output_dir }}/summary.json"
    check_file "{{ output_dir }}/report.html"

    if (( errors > 0 )); then
        echo "Validation failed: $errors file(s) missing"
        exit 1
    fi
    echo "All files present"

# Run the full pipeline: generate → analyze → report → validate
pipeline count='200': setup (generate-data count) analyze report validate
    #!/usr/bin/env bash
    set -euo pipefail
    echo ""
    echo "Pipeline complete!"
    echo "  Data:    {{ data_dir }}/raw_data.json"
    echo "  Summary: {{ output_dir }}/summary.json"
    echo "  Report:  {{ output_dir }}/report.html"

# Clean all generated files
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf {{ data_dir }} {{ output_dir }}
    echo "Cleaned {{ data_dir }}/ and {{ output_dir }}/"
```

The `pipeline` recipe demonstrates how Just's dependency system works across languages. Python generates data, Python analyzes it, Node creates the report, and bash validates — all coordinated by Just's recipe dependency chain.

## Step 6 -- Passing Complex Data Between Languages

```just
# Demonstrate passing just variables into different interpreters
show-env lang='all':
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Bash ==="
    echo "Project: {{ project }}"
    echo "OS: {{ os() }}, Arch: {{ arch() }}"

# Python can access the same just variables
[script("python3")]
show-env-python:
    import platform
    print("=== Python ===")
    print(f"Project: {{ project }}")
    print(f"Python: {platform.python_version()}")
    print(f"OS (from just): {{ os() }}")
    print(f"OS (from python): {platform.system().lower()}")

# Node can too
[script("node")]
show-env-node:
    console.log("=== Node ===");
    console.log(`Project: {{ project }}`);
    console.log(`Node: ${process.version}`);
    console.log(`OS (from just): {{ os() }}`);
    console.log(`OS (from node): ${process.platform}`);
```

Key insight: `{{ }}` interpolation happens before the interpreter sees the code. The values are baked into the script as string literals. This means you cannot use Just variables dynamically at runtime — they are compile-time constants from the interpreter's perspective.

## Common Mistakes

**Wrong: Forgetting `set -euo pipefail` in bash shebang recipes**
```just
process:
    #!/usr/bin/env bash
    false
    echo "This still runs!"
```
What happens: Without `-e`, bash ignores the failed command and continues executing. The recipe reports success even though an intermediate step failed. This is especially dangerous in pipelines where a failed data processing step produces corrupt output consumed by the next stage.
Fix: Always start bash shebang recipes with `set -euo pipefail`. For Python and Node, errors are raised by default, but consider wrapping in try/except or try/catch for meaningful error messages.

**Wrong: Using `{{ }}` for runtime values in shebang scripts**
```just
[script("python3")]
read-input:
    user_input = input("Enter name: ")
    # This does NOT work — {{ }} is compile-time
    print(f"Hello {{ user_input }}")
```
What happens: `{{ user_input }}` is evaluated by Just before Python runs. Since `user_input` is not a Just variable, it errors. Just interpolation is pre-processing, not runtime.
Fix: Use Just variables for values known at recipe invocation time. Use the language's own variable system for runtime values. If you need to pass Just variables into Python, they become string literals: `name = "{{ project }}"`, then use `name` in Python code.

## Verify What You Learned

```bash
# Run the full pipeline
just pipeline 50
# Expected: generate → analyze → report → validate, all pass

# Check that files were created
ls -la data/ output/
# Expected: raw_data.json, summary.json, report.html

# Show environment from each language
just show-env
just show-env-python
just show-env-node
# Expected: project name and OS info from each interpreter

# Clean and verify
just clean
ls data/ 2>&1
# Expected: "No such file or directory"
```

## What's Next

The next exercise ([27. Multi-Stage CI/CD Pipeline](../07-multi-stage-ci-cd-pipeline/07-multi-stage-ci-cd-pipeline.md)) builds a complete CI/CD pipeline with quality gates, environment-specific deployment, and artifact versioning.

## Summary

- Shebang recipes (`#!/usr/bin/env python3`) run as complete scripts in any language
- `[script("interpreter")]` is a cleaner alternative to shebang lines
- `[extension("py")]` controls the temp file extension (important for imports and ES modules)
- `{{ variable }}` interpolation happens before the interpreter runs — values are compile-time constants
- `set -euo pipefail` is mandatory in bash shebang recipes for proper error handling
- Just's dependency system coordinates recipes across languages seamlessly
- Each language should handle the task it does best: bash for orchestration, Python for data, Node for templating

## Reference

- [Just Shebang Recipes](https://just.systems/man/en/shebang-recipes.html)
- [Just Script Attribute](https://just.systems/man/en/attributes.html)
- [Just Extension Attribute](https://just.systems/man/en/attributes.html)
- [Just String Interpolation](https://just.systems/man/en/string-interpolation.html)

## Additional Resources

- [Bash Strict Mode](http://redsymbol.net/articles/unofficial-bash-strict-mode/)
- [Python JSON Module](https://docs.python.org/3/library/json.html)
- [Node.js ES Modules](https://nodejs.org/api/esm.html)
