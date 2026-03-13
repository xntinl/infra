# 32. Multi-Language Scripts

<!--
difficulty: advanced
concepts: [shebang-recipes, script-attribute, polyglot-pipelines, data-processing, cross-language-invocation]
tools: [just]
estimated_time: 1h
bloom_level: create
prerequisites: [shebang-recipes, recipe-parameters, environment-variables]
-->

## Prerequisites

- just >= 1.38.0
- bash, python3, node (v18+), ruby (2.7+)
- curl and jq

## Learning Objectives

- **Create** a polyglot data pipeline where each stage uses the optimal language for its task
- **Evaluate** the trade-offs between `#!/usr/bin/env` shebang syntax and the `[script()]` attribute
- **Design** cross-language communication patterns using stdout piping and temporary files

## Why Multi-Language Scripts

Real infrastructure work spans languages constantly: a bash script provisions resources, a Python script transforms configuration data, a Node script calls an API, and a Ruby script generates a report. Justfiles let you keep all of these in a single orchestration file without wrapper scripts. Each recipe can use the language best suited to its task while just handles argument passing, dependencies, and error propagation.

## The Challenge

Build a justfile that implements a data processing pipeline across four languages. The pipeline fetches JSON data, transforms it with Python, generates an HTML report with Node, and summarizes it with Ruby. Each stage communicates via files in a `.pipeline/` directory. Include a health-check recipe with robust curl error handling and a setup recipe that verifies all required interpreters are available.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]

pipeline_dir := ".pipeline"

# ─── Setup & Verification ──────────────────────────────────

[group('setup')]
[doc('Verify all required interpreters are available')]
check-interpreters:
    #!/usr/bin/env bash
    set -euo pipefail
    missing=0
    for cmd in bash python3 node ruby curl jq; do
        if command -v "$cmd" >/dev/null 2>&1; then
            version=$("$cmd" --version 2>&1 | head -1)
            printf "  \033[32m✓\033[0m %-10s %s\n" "$cmd" "$version"
        else
            printf "  \033[31m✗\033[0m %-10s NOT FOUND\n" "$cmd"
            missing=$((missing + 1))
        fi
    done
    if [ "$missing" -gt 0 ]; then
        echo ""
        echo "ERROR: $missing required tool(s) missing"
        exit 1
    fi
    echo ""
    echo "All interpreters available."

[group('setup')]
[doc('Initialize the pipeline working directory')]
init:
    @mkdir -p {{ pipeline_dir }}
    @echo "Pipeline directory ready: {{ pipeline_dir }}/"

# ─── Pipeline Stages ───────────────────────────────────────

[group('pipeline')]
[doc('Run the complete data pipeline: fetch -> transform -> report -> summary')]
pipeline: init fetch transform report summary
    @echo ""
    @echo "Pipeline complete. Artifacts in {{ pipeline_dir }}/"

[group('pipeline')]
[doc('Stage 1: Fetch raw JSON data (bash + curl)')]
fetch: init
    #!/usr/bin/env bash
    set -euo pipefail
    echo "==> Stage 1: Fetching data..."
    # Generate sample data simulating an API response
    cat > {{ pipeline_dir }}/raw.json <<'JSONEOF'
    {
      "services": [
        {"name": "api-gateway", "region": "us-east-1", "cpu": 45.2, "memory": 72.1, "requests": 15230, "errors": 12, "status": "healthy"},
        {"name": "auth-service", "region": "us-east-1", "cpu": 23.8, "memory": 55.4, "requests": 8910, "errors": 3, "status": "healthy"},
        {"name": "data-processor", "region": "eu-west-1", "cpu": 89.1, "memory": 91.3, "requests": 4520, "errors": 87, "status": "degraded"},
        {"name": "cache-layer", "region": "us-east-1", "cpu": 12.4, "memory": 38.7, "requests": 52100, "errors": 0, "status": "healthy"},
        {"name": "notification-svc", "region": "ap-southeast-1", "cpu": 67.3, "memory": 44.9, "requests": 2340, "errors": 156, "status": "unhealthy"},
        {"name": "billing-engine", "region": "us-east-1", "cpu": 34.5, "memory": 62.8, "requests": 1890, "errors": 1, "status": "healthy"},
        {"name": "search-index", "region": "eu-west-1", "cpu": 78.9, "memory": 85.2, "requests": 6780, "errors": 23, "status": "degraded"},
        {"name": "media-service", "region": "ap-southeast-1", "cpu": 55.1, "memory": 70.6, "requests": 3210, "errors": 8, "status": "healthy"}
      ],
      "timestamp": "2026-03-09T12:00:00Z"
    }
    JSONEOF
    record_count=$(jq '.services | length' {{ pipeline_dir }}/raw.json)
    echo "  Fetched $record_count service records -> {{ pipeline_dir }}/raw.json"

[group('pipeline')]
[doc('Stage 2: Transform and enrich data (Python)')]
transform: fetch
    #!/usr/bin/env python3
    import json
    import os

    pipeline_dir = "{{ pipeline_dir }}"

    with open(f"{pipeline_dir}/raw.json") as f:
        data = json.load(f)

    print("==> Stage 2: Transforming data...")

    for svc in data["services"]:
        svc["error_rate"] = round((svc["errors"] / max(svc["requests"], 1)) * 100, 3)
        svc["health_score"] = round(
            100
            - (svc["cpu"] * 0.3)
            - (svc["memory"] * 0.2)
            - (svc["error_rate"] * 10),
            1
        )
        svc["health_score"] = max(0, svc["health_score"])
        if svc["health_score"] >= 70:
            svc["risk_level"] = "low"
        elif svc["health_score"] >= 40:
            svc["risk_level"] = "medium"
        else:
            svc["risk_level"] = "high"

    data["services"].sort(key=lambda s: s["health_score"])

    by_region = {}
    for svc in data["services"]:
        region = svc["region"]
        by_region.setdefault(region, []).append(svc["name"])
    data["by_region"] = by_region

    with open(f"{pipeline_dir}/transformed.json", "w") as f:
        json.dump(data, f, indent=2)

    high_risk = [s["name"] for s in data["services"] if s["risk_level"] == "high"]
    print(f"  Enriched {len(data['services'])} records")
    if high_risk:
        print(f"  WARNING: High-risk services: {', '.join(high_risk)}")
    print(f"  Output -> {pipeline_dir}/transformed.json")

[group('pipeline')]
[doc('Stage 3: Generate HTML dashboard report (Node.js)')]
[script("node")]
report:
    const fs = require('fs');
    const path = '{{ pipeline_dir }}';

    console.log('==> Stage 3: Generating HTML report...');

    const data = JSON.parse(fs.readFileSync(`${path}/transformed.json`, 'utf8'));

    const riskColor = { low: '#22c55e', medium: '#f59e0b', high: '#ef4444' };
    const statusIcon = { healthy: '●', degraded: '◐', unhealthy: '○' };

    const rows = data.services.map(s => `
        <tr>
          <td>${s.name}</td>
          <td>${s.region}</td>
          <td>${statusIcon[s.status] || '?'} ${s.status}</td>
          <td>${s.cpu}%</td>
          <td>${s.memory}%</td>
          <td>${s.error_rate}%</td>
          <td style="color: ${riskColor[s.risk_level]}">${s.health_score}</td>
          <td><span style="background: ${riskColor[s.risk_level]}; color: white; padding: 2px 8px; border-radius: 4px">${s.risk_level}</span></td>
        </tr>`).join('\n');

    const html = `<!DOCTYPE html>
    <html lang="en">
    <head>
      <meta charset="utf-8">
      <title>Service Health Dashboard</title>
      <style>
        body { font-family: -apple-system, sans-serif; margin: 2rem; background: #0f172a; color: #e2e8f0; }
        h1 { color: #38bdf8; }
        table { border-collapse: collapse; width: 100%; margin-top: 1rem; }
        th, td { padding: 0.75rem 1rem; text-align: left; border-bottom: 1px solid #334155; }
        th { background: #1e293b; color: #94a3b8; text-transform: uppercase; font-size: 0.75rem; }
        tr:hover { background: #1e293b; }
        .meta { color: #64748b; font-size: 0.85rem; margin-top: 0.5rem; }
      </style>
    </head>
    <body>
      <h1>Service Health Dashboard</h1>
      <p class="meta">Generated: ${new Date().toISOString()} | Source timestamp: ${data.timestamp}</p>
      <table>
        <thead>
          <tr><th>Service</th><th>Region</th><th>Status</th><th>CPU</th><th>Memory</th><th>Error Rate</th><th>Health Score</th><th>Risk</th></tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </body>
    </html>`;

    fs.writeFileSync(`${path}/report.html`, html);
    console.log(`  Generated dashboard -> ${path}/report.html`);

[group('pipeline')]
[doc('Stage 4: Generate text summary (Ruby)')]
summary:
    #!/usr/bin/env ruby
    require 'json'

    pipeline_dir = "{{ pipeline_dir }}"

    puts "==> Stage 4: Generating summary..."

    data = JSON.parse(File.read("#{pipeline_dir}/transformed.json"))
    services = data["services"]

    total = services.length
    by_risk = services.group_by { |s| s["risk_level"] }
    by_status = services.group_by { |s| s["status"] }
    avg_health = (services.sum { |s| s["health_score"] } / total.to_f).round(1)
    avg_cpu = (services.sum { |s| s["cpu"] } / total.to_f).round(1)
    avg_memory = (services.sum { |s| s["memory"] } / total.to_f).round(1)

    summary = []
    summary << "=" * 50
    summary << "  SERVICE HEALTH SUMMARY"
    summary << "=" * 50
    summary << ""
    summary << "  Total services: #{total}"
    summary << "  Average health score: #{avg_health}/100"
    summary << "  Average CPU: #{avg_cpu}%"
    summary << "  Average memory: #{avg_memory}%"
    summary << ""
    summary << "  Risk Distribution:"
    %w[low medium high].each do |level|
        count = (by_risk[level] || []).length
        bar = "#" * (count * 4)
        summary << "    #{level.ljust(8)} #{bar} (#{count})"
    end
    summary << ""
    summary << "  Status Breakdown:"
    %w[healthy degraded unhealthy].each do |status|
        names = (by_status[status] || []).map { |s| s["name"] }
        summary << "    #{status.ljust(12)} #{names.join(', ')}" unless names.empty?
    end
    summary << ""

    high_risk = (by_risk["high"] || [])
    unless high_risk.empty?
        summary << "  ACTION REQUIRED:"
        high_risk.each do |s|
            summary << "    - #{s['name']}: health=#{s['health_score']}, errors=#{s['error_rate']}%"
        end
        summary << ""
    end
    summary << "=" * 50

    report = summary.join("\n")
    File.write("#{pipeline_dir}/summary.txt", report)
    puts report
    puts ""
    puts "  Summary written -> #{pipeline_dir}/summary.txt"

# ─── Health Check ───────────────────────────────────────────

[group('ops')]
[doc('Health check an HTTP endpoint with retries and detailed diagnostics')]
health-check url retries="3" interval="2":
    #!/usr/bin/env bash
    set -euo pipefail
    url="{{ url }}"
    max={{ retries }}
    interval={{ interval }}
    attempt=1

    printf "\033[36mHealth-checking %s (max %d attempts, %ds interval)\033[0m\n" "$url" "$max" "$interval"

    while [ "$attempt" -le "$max" ]; do
        printf "  Attempt %d/%d... " "$attempt" "$max"
        http_code=$(curl -s -o /tmp/health_body -w "%{http_code}" --connect-timeout 5 --max-time 10 "$url" 2>/dev/null) || http_code="000"

        if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
            printf "\033[32m%s OK\033[0m\n" "$http_code"
            if command -v jq >/dev/null 2>&1 && jq . /tmp/health_body >/dev/null 2>&1; then
                jq . /tmp/health_body
            else
                cat /tmp/health_body
            fi
            rm -f /tmp/health_body
            exit 0
        else
            printf "\033[31m%s FAIL\033[0m\n" "$http_code"
        fi

        if [ "$attempt" -lt "$max" ]; then
            sleep "$interval"
        fi
        attempt=$((attempt + 1))
    done

    printf "\033[31mHealth check failed after %d attempts\033[0m\n" "$max"
    rm -f /tmp/health_body
    exit 1

# ─── Cleanup ───────────────────────────────────────────────

[group('setup')]
[doc('Remove all pipeline artifacts')]
clean:
    rm -rf {{ pipeline_dir }}
    @echo "Cleaned {{ pipeline_dir }}/"
```

## Verify What You Learned

```bash
# Verify all interpreters are available
just check-interpreters

# Run the full pipeline end-to-end
just pipeline

# Confirm all artifacts were created
ls -la .pipeline/

# Inspect the transformed JSON to verify Python enrichment
python3 -c "import json; d=json.load(open('.pipeline/transformed.json')); print(json.dumps(d['services'][0], indent=2))"

# View the text summary
cat .pipeline/summary.txt
```

## What's Next

Continue to [Exercise 33: Robust Error Handling](../03-robust-error-handling/03-robust-error-handling.md) to build deploy pipelines with rollback logic, pre-flight checks, and graceful degradation.

## Summary

- Shebang recipes (`#!/usr/bin/env python3`) embed full scripts in any language inside a justfile
- The `[script("interpreter")]` attribute is a cleaner alternative for supported interpreters
- Cross-language communication works through files in a shared pipeline directory
- Each language handles what it does best: bash for I/O, Python for data, Node for templating, Ruby for text processing
- Just handles dependency ordering, argument passing, and error propagation across languages

## Reference

- [Shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [Script attribute](https://just.systems/man/en/script-attribute.html)
- [Shell settings](https://just.systems/man/en/settings.html)

## Additional Resources

- [Polyglot automation with just](https://github.com/casey/just#recipe-lines)
- [just cookbook examples](https://github.com/casey/just/tree/master/examples)
