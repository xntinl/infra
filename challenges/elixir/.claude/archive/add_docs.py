#!/usr/bin/env python3
"""
Script to add @moduledoc and @doc to Elixir code blocks in markdown files
"""

import os
import re
import sys
from pathlib import Path

class DocAdder:
    def __init__(self, root_dir):
        self.root_dir = Path(root_dir)
        self.stats = {
            'files_processed': 0,
            'moduledocs_added': 0,
            'docs_added': 0
        }

    def run(self):
        """Process all markdown files"""
        md_files = list(self.root_dir.rglob('*.md'))

        for md_file in md_files:
            # Skip dependency files
            if 'deps' in str(md_file):
                continue

            self.process_file(md_file)

        self.print_summary()
        self.save_log()

    def process_file(self, file_path):
        """Process a single markdown file"""
        try:
            with open(file_path, 'r', encoding='utf-8') as f:
                content = f.read()
        except Exception as e:
            print(f"Error reading {file_path}: {e}")
            return

        new_content, moduledocs, docs = self.add_docs_to_content(content)

        if moduledocs > 0 or docs > 0:
            try:
                with open(file_path, 'w', encoding='utf-8') as f:
                    f.write(new_content)
                print(f"✓ {file_path}: +{moduledocs} @moduledoc, +{docs} @doc")
                self.stats['files_processed'] += 1
                self.stats['moduledocs_added'] += moduledocs
                self.stats['docs_added'] += docs
            except Exception as e:
                print(f"Error writing {file_path}: {e}")

    def add_docs_to_content(self, content):
        """Add @moduledoc and @doc to code blocks"""
        lines = content.split('\n')
        new_lines = []
        in_code_block = False
        moduledocs_count = 0
        docs_count = 0
        i = 0

        while i < len(lines):
            line = lines[i]

            # Check for elixir code block start
            if line.strip().startswith('```elixir'):
                in_code_block = True
                new_lines.append(line)
                i += 1
                continue

            # Check for code block end
            if line.strip().startswith('```') and in_code_block:
                in_code_block = False
                new_lines.append(line)
                i += 1
                continue

            # Process lines inside code blocks
            if in_code_block:
                stripped = line.lstrip()
                indent = line[:len(line) - len(stripped)]

                # Handle defmodule - check if next line is @moduledoc
                if re.match(r'^defmodule\s+\w+', stripped):
                    # Look ahead to see if @moduledoc exists
                    has_moduledoc = False
                    if i + 1 < len(lines):
                        next_line = lines[i + 1].lstrip()
                        if next_line.startswith('@moduledoc') or next_line.startswith('@doc'):
                            has_moduledoc = True

                    new_lines.append(line)

                    if not has_moduledoc:
                        # Extract module name for better doc
                        match = re.match(r'^defmodule\s+(\w+)', stripped)
                        module_name = match.group(1) if match else 'Module'
                        formatted_name = self.format_name(module_name)
                        moduledoc_line = f'{indent}@moduledoc "{formatted_name}"'
                        new_lines.append(moduledoc_line)
                        moduledocs_count += 1

                    i += 1
                    continue

                # Handle def - check if it's a public function
                if re.match(r'^def\s+\w+', stripped) and not re.match(r'^def\s+(handle_call|handle_cast|init|handle_info|code_change|terminate)', stripped):
                    # Check if previous line is @doc
                    has_doc = False
                    if new_lines and new_lines[-1].lstrip().startswith('@doc'):
                        has_doc = True

                    if not has_doc:
                        # Extract function name
                        match = re.match(r'^def\s+(\w+)', stripped)
                        func_name = match.group(1) if match else 'function'
                        formatted_name = self.format_name(func_name)
                        doc_line = f'{indent}@doc "{formatted_name}"'
                        new_lines.append(doc_line)
                        docs_count += 1

                    new_lines.append(line)
                    i += 1
                    continue

            new_lines.append(line)
            i += 1

        return '\n'.join(new_lines), moduledocs_count, docs_count

    def format_name(self, name):
        """Convert camelCase to readable name"""
        # Insert space before uppercase letters
        spaced = re.sub(r'(?<!^)(?=[A-Z])', ' ', name)
        return spaced.lower()

    def print_summary(self):
        """Print processing summary"""
        stats = self.stats
        print("\n" + "=" * 60)
        print(f"Code Quality (Docs): {stats['files_processed']} archivos procesados")
        print(f"{stats['moduledocs_added']} @moduledoc agregados")
        print(f"{stats['docs_added']} @doc agregados")
        print("=" * 60 + "\n")

    def save_log(self):
        """Save processing log"""
        log_dir = self.root_dir / '.claude'
        log_dir.mkdir(exist_ok=True)
        log_file = log_dir / 'code-quality-docs.log'

        import datetime
        timestamp = datetime.datetime.utcnow().isoformat()

        log_content = f"""Code Quality Documentation Automation Log
========================================
Timestamp: {timestamp}

Summary:
- Files processed: {self.stats['files_processed']}
- @moduledoc added: {self.stats['moduledocs_added']}
- @doc added: {self.stats['docs_added']}

Status: COMPLETED
"""

        with open(log_file, 'w', encoding='utf-8') as f:
            f.write(log_content)

        print(f"Log saved to: {log_file}")

if __name__ == '__main__':
    root = "/Users/consulting/Documents/consulting/infra/challenges/elixir"
    adder = DocAdder(root)
    adder.run()
