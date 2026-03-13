# Exercise 08: CSV Reading and Writing

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

CSV (Comma-Separated Values) is everywhere: data exports, spreadsheet interchange, log files, configuration tables. Go's `encoding/csv` package provides a streaming reader and writer that handle quoting, escaping, and custom delimiters correctly.

## Prerequisites

- File I/O basics (Exercise 01)
- `io.Reader` / `io.Writer`
- Slices of slices ([][]string)

## Key APIs

```go
// Reading
r := csv.NewReader(file)
r.Comma = ';'              // custom delimiter
r.Comment = '#'            // skip comment lines
r.FieldsPerRecord = -1     // allow variable field count
record, err := r.Read()    // one row at a time
records, err := r.ReadAll() // all rows at once

// Writing
w := csv.NewWriter(file)
w.Comma = '\t'             // tab-separated
w.Write([]string{"a", "b", "c"})
w.WriteAll(records)
w.Flush()
w.Error()                  // check for write errors
```

## Task

### Part 1: Read and Process CSV

Given this CSV data (create it as a string or file):

```csv
name,department,salary,start_date
Alice Johnson,Engineering,95000,2020-03-15
Bob Smith,Marketing,72000,2019-07-01
Charlie Brown,Engineering,88000,2021-01-10
Diana Prince,Marketing,78000,2020-11-20
Eve Wilson,Engineering,102000,2018-05-03
Frank Castle,Sales,65000,2022-02-14
Grace Lee,Engineering,91000,2021-06-30
```

Parse the CSV and:
1. Skip the header row
2. Calculate the average salary per department
3. Find the employee with the highest salary
4. Print results formatted as a table

### Part 2: Write CSV with Custom Formatting

Generate a CSV report from the processed data:
1. Write a header row
2. Write one row per department with: department name, employee count, average salary, total salary
3. Use tab as the delimiter
4. Write to both a file and stdout (using `io.MultiWriter`)

### Part 3: Handle Messy CSV

Process CSV with edge cases:

```csv
# This is a comment
name,bio,score
"Alice ""The Great""",works at "Acme, Inc.",95
Bob,"likes
newlines",88
Charlie,,75
```

Configure the reader to handle:
- Comment lines (starting with `#`)
- Quoted fields with embedded commas
- Quoted fields with embedded double quotes
- Quoted fields with embedded newlines
- Empty fields

Print each record showing how Go handles these cases.

### Part 4: CSV Struct Mapping

Write helper functions to convert between CSV and structs:

```go
func MarshalCSV(records []Employee) ([]byte, error)
func UnmarshalCSV(data []byte, records *[]Employee) error
```

Use struct field names as the header row. Handle the basic types: string, int, float64.

## Hints

- `csv.NewReader` returns `io.EOF` when done -- check for it in the read loop.
- `r.FieldsPerRecord = 0` (default) enforces that all rows have the same field count as the first row.
- `strconv.Atoi` and `strconv.ParseFloat` convert string fields to numbers.
- CSV fields containing commas, quotes, or newlines are automatically quoted by the writer.
- Double quotes in fields are escaped as `""` by the CSV standard.
- For struct mapping, use `reflect` to iterate struct fields, or hard-code for simplicity.
- `csv.Writer.Flush()` must be called after all writes. Check `w.Error()` afterward.

## Verification

### Part 1:
```
=== Department Averages ===
Engineering:  $94,000.00 (4 employees)
Marketing:    $75,000.00 (2 employees)
Sales:        $65,000.00 (1 employee)

Highest salary: Eve Wilson ($102,000)
```

### Part 2:
```
department	count	avg_salary	total_salary
Engineering	4	94000.00	376000.00
Marketing	2	75000.00	150000.00
Sales	1	65000.00	65000.00
```

### Part 3:
```
Record 1: [Alice "The Great"] [works at "Acme, Inc."] [95]
Record 2: [Bob] [likes\nnewlines] [88]
Record 3: [Charlie] [] [75]
```

## Key Takeaways

- `encoding/csv` handles quoting, escaping, and edge cases automatically
- Use `Read()` in a loop for streaming; `ReadAll()` for small files
- Always call `Flush()` after writing and check `Error()`
- `Comma`, `Comment`, and `FieldsPerRecord` customize parsing behavior
- CSV has no type information -- you must convert strings to numbers explicitly
- Embedded commas, quotes, and newlines in fields are handled correctly by the standard
