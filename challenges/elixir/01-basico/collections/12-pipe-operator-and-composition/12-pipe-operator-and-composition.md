# Pipe Operator and Composition: Building an ETL Pipeline

**Project**: `etl` — an Extract-Transform-Load pipeline that reads CSV, transforms data, and outputs JSON

---

## Why the pipe operator changes how you design functions

The pipe operator `|>` passes the result of the left expression as the first
argument to the function on the right. This is not syntactic sugar — it is a
design constraint that shapes how you write functions in Elixir.

```elixir
# Without pipes — read inside-out
String.trim(String.downcase(String.replace(input, "\t", " ")))

# With pipes — read top-to-bottom
input
|> String.replace("\t", " ")
|> String.downcase()
|> String.trim()
```

The pipe operator enforces a convention: the primary data being transformed is
always the first argument. This is why every Elixir standard library function
puts the "subject" first: `Enum.map(list, fun)`, `String.split(string, sep)`,
`Map.put(map, key, value)`.

When you design your own functions, following this convention makes them
pipe-friendly. Breaking it makes them awkward to use in pipelines.

---

## The business problem

Build an ETL system that:

1. Reads raw CSV text
2. Parses it into maps (rows with headers as keys)
3. Transforms values (type casting, normalization, enrichment)
4. Filters invalid records
5. Outputs the result as a JSON-serializable structure

The entire flow should be a single pipe chain from input to output.

---

## Project structure

```
etl/
├── lib/
│   └── etl.ex
├── test/
│   └── etl_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — named intermediate bindings between steps**
```elixir
parsed = parse(input)
typed = cast_types(parsed)
valid = filter_valid(typed)
encode_json(valid)
```
- Pros: easy to inspect each stage in a debugger; each binding reads like a noun that names the intermediate shape; no clever syntax.
- Cons: verbose; invites variable-name drift (`typed`, `typed2`, `typed_final`); visually obscures the one-directional flow.

**Option B — single pipe chain from input to output** (chosen)
- Pros: reads top-to-bottom like a flowchart; no throwaway variable names; swapping/adding a stage is one line; the shape of the data at each step is implicit in the function used.
- Cons: debugging a mid-pipe failure means inserting `IO.inspect/2` or splitting temporarily; long pipes (>8 stages) become harder to read; requires functions designed with subject-first signatures.

Chose **B** because ETL is by nature a linear transformation; the pipe reflects the domain. When a stage gets complicated, extract it to a named function — but keep the top-level shape as a pipe.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
  ]
end
```


### `lib/etl.ex`

```elixir
defmodule Etl do
  @moduledoc """
  Extract-Transform-Load pipeline using the pipe operator.

  Each step in the ETL process is a function that takes data as its
  first argument and returns transformed data. This makes the entire
  pipeline expressible as a single pipe chain.
  """

  @doc """
  Runs the complete ETL pipeline from raw CSV to structured output.

  ## Examples

      iex> csv = "name,age,email\\nAlice,30,alice@example.com\\nBob,invalid,bob@example.com"
      iex> {:ok, result} = Etl.run(csv)
      iex> length(result.valid_records)
      1
      iex> hd(result.valid_records)["name"]
      "alice"

  """
  @spec run(String.t()) :: {:ok, map()} | {:error, String.t()}
  def run(csv_text) when is_binary(csv_text) do
    csv_text
    |> extract()
    |> transform()
    |> load()
  end

  @doc """
  Extracts data: parses CSV text into a list of maps.

  The first row is used as headers. Each subsequent row becomes a
  map with header keys.

  ## Examples

      iex> Etl.extract("name,age\\nAlice,30\\nBob,25")
      [%{"name" => "Alice", "age" => "30"}, %{"name" => "Bob", "age" => "25"}]

      iex> Etl.extract("")
      []

  """
  @spec extract(String.t()) :: [map()]
  def extract(csv_text) when is_binary(csv_text) do
    csv_text
    |> String.trim()
    |> String.split("\n", trim: true)
    |> parse_csv()
  end

  @doc """
  Transforms data: normalizes values, casts types, and marks validity.

  Each record gets:
    - String fields trimmed and lowercased
    - "age" field cast to integer
    - "email" field validated
    - A "_valid" boolean field

  ## Examples

      iex> records = [%{"name" => " Alice ", "age" => "30", "email" => "a@b.com"}]
      iex> [transformed] = Etl.transform(records)
      iex> transformed["name"]
      "alice"
      iex> transformed["age"]
      30

  """
  @spec transform([map()]) :: [map()]
  def transform(records) when is_list(records) do
    records
    |> Enum.map(&normalize_strings/1)
    |> Enum.map(&cast_age/1)
    |> Enum.map(&validate_email/1)
    |> Enum.map(&mark_validity/1)
  end

  @doc """
  Loads data: separates valid from invalid records and builds output.

  ## Examples

      iex> records = [
      ...>   %{"name" => "alice", "age" => 30, "email" => "a@b.com", "_valid" => true},
      ...>   %{"name" => "bob", "age" => nil, "email" => "b@c.com", "_valid" => false}
      ...> ]
      iex> {:ok, result} = Etl.load(records)
      iex> length(result.valid_records)
      1
      iex> length(result.invalid_records)
      1

  """
  @spec load([map()]) :: {:ok, map()}
  def load(records) when is_list(records) do
    {valid, invalid} =
      records
      |> Enum.split_with(fn record -> record["_valid"] == true end)

    output = %{
      valid_records: Enum.map(valid, &strip_internal_fields/1),
      invalid_records: Enum.map(invalid, &strip_internal_fields/1),
      stats: %{
        total: length(records),
        valid: length(valid),
        invalid: length(invalid)
      }
    }

    {:ok, output}
  end

  @doc """
  Converts the output to a JSON string.

  ## Examples

      iex> data = %{valid_records: [%{"name" => "alice"}], invalid_records: [], stats: %{total: 1, valid: 1, invalid: 0}}
      iex> {:ok, json} = Etl.to_json(data)
      iex> is_binary(json)
      true

  """
  @spec to_json(map()) :: {:ok, String.t()} | {:error, String.t()}
  def to_json(data) when is_map(data) do
    case Jason.encode(data, pretty: true) do
      {:ok, json} -> {:ok, json}
      {:error, reason} -> {:error, "JSON encoding failed: #{inspect(reason)}"}
    end
  end

  # --- Private: CSV parsing ---

  @spec parse_csv([String.t()]) :: [map()]
  defp parse_csv([]), do: []
  defp parse_csv([_header_only]), do: []

  defp parse_csv([header_line | data_lines]) do
    headers =
      header_line
      |> String.split(",")
      |> Enum.map(&String.trim/1)

    data_lines
    |> Enum.map(fn line ->
      values =
        line
        |> String.split(",")
        |> Enum.map(&String.trim/1)

      headers
      |> Enum.zip(values)
      |> Map.new()
    end)
  end

  # --- Private: transformation steps ---

  @spec normalize_strings(map()) :: map()
  defp normalize_strings(record) do
    record
    |> Enum.map(fn
      {key, value} when is_binary(value) ->
        {key, value |> String.trim() |> String.downcase()}

      pair ->
        pair
    end)
    |> Map.new()
  end

  @spec cast_age(map()) :: map()
  defp cast_age(%{"age" => age_str} = record) when is_binary(age_str) do
    case Integer.parse(age_str) do
      {age, ""} when age > 0 and age < 150 -> Map.put(record, "age", age)
      _ -> Map.put(record, "age", nil)
    end
  end

  defp cast_age(record), do: record

  @spec validate_email(map()) :: map()
  defp validate_email(%{"email" => email} = record) when is_binary(email) do
    valid =
      String.contains?(email, "@") and
        String.contains?(email, ".") and
        byte_size(email) > 5

    Map.put(record, "_email_valid", valid)
  end

  defp validate_email(record) do
    Map.put(record, "_email_valid", false)
  end

  @spec mark_validity(map()) :: map()
  defp mark_validity(record) do
    valid =
      record["age"] != nil and
        record["name"] != nil and
        byte_size(record["name"] || "") > 0 and
        record["_email_valid"] == true

    record
    |> Map.put("_valid", valid)
    |> Map.delete("_email_valid")
  end

  @spec strip_internal_fields(map()) :: map()
  defp strip_internal_fields(record) do
    record
    |> Map.reject(fn {key, _val} -> String.starts_with?(key, "_") end)
  end
end
```

**Why this works:**

- `run/1` is a single pipe chain: `extract |> transform |> load`. Each function
  takes the primary data as its first argument and returns transformed data.
- `extract/1` pipes through `String.trim -> String.split -> parse_csv`. Each step
  transforms the data shape: string -> list of strings -> list of maps.
- `transform/1` pipes through four `Enum.map` calls. Each map applies one
  transformation to every record. This is clear and composable, though slightly
  less efficient than a single pass (see trade-offs below).
- Every private function is pipe-friendly: the data being transformed is the first
  (or only) argument.

### Tests

```elixir
# test/etl_test.exs
defmodule EtlTest do
  use ExUnit.Case, async: true

  doctest Etl

  @sample_csv """
  name,age,email
  Alice,30,alice@example.com
  Bob,invalid,bob@example.com
  Charlie,25,charlie@example.com
   Diana , 28 , diana@example.com
  """

  describe "run/1" do
    test "full pipeline produces valid and invalid records" do
      assert {:ok, result} = Etl.run(@sample_csv)
      assert result.stats.total == 4
      assert result.stats.valid == 3
      assert result.stats.invalid == 1
    end

    test "invalid age makes record invalid" do
      csv = "name,age,email\nAlice,xyz,alice@example.com"
      assert {:ok, result} = Etl.run(csv)
      assert result.stats.invalid == 1
    end
  end

  describe "extract/1" do
    test "parses CSV into list of maps" do
      records = Etl.extract("name,age\nAlice,30\nBob,25")
      assert length(records) == 2
      assert hd(records) == %{"name" => "Alice", "age" => "30"}
    end

    test "handles empty input" do
      assert Etl.extract("") == []
    end

    test "handles header-only input" do
      assert Etl.extract("name,age") == []
    end

    test "trims whitespace from values" do
      records = Etl.extract("name,age\n Alice , 30 ")
      assert hd(records)["name"] == "Alice"
      assert hd(records)["age"] == "30"
    end
  end

  describe "transform/1" do
    test "normalizes strings to lowercase" do
      records = [%{"name" => "ALICE", "age" => "30", "email" => "A@B.COM"}]
      [result] = Etl.transform(records)
      assert result["name"] == "alice"
      assert result["email"] == "a@b.com"
    end

    test "casts valid age to integer" do
      records = [%{"name" => "alice", "age" => "30", "email" => "a@b.com"}]
      [result] = Etl.transform(records)
      assert result["age"] == 30
    end

    test "sets nil for invalid age" do
      records = [%{"name" => "alice", "age" => "xyz", "email" => "a@b.com"}]
      [result] = Etl.transform(records)
      assert result["age"] == nil
    end

    test "marks validity" do
      records = [
        %{"name" => "alice", "age" => "30", "email" => "a@b.com"},
        %{"name" => "bob", "age" => "invalid", "email" => "b@c.com"}
      ]

      results = Etl.transform(records)
      assert Enum.at(results, 0)["_valid"] == true
      assert Enum.at(results, 1)["_valid"] == false
    end
  end

  describe "load/1" do
    test "separates valid from invalid" do
      records = [
        %{"name" => "alice", "age" => 30, "_valid" => true},
        %{"name" => "bob", "age" => nil, "_valid" => false}
      ]

      assert {:ok, result} = Etl.load(records)
      assert length(result.valid_records) == 1
      assert length(result.invalid_records) == 1
    end

    test "strips internal fields from output" do
      records = [%{"name" => "alice", "_valid" => true, "_temp" => "x"}]
      {:ok, result} = Etl.load(records)
      record = hd(result.valid_records)
      refute Map.has_key?(record, "_valid")
      refute Map.has_key?(record, "_temp")
    end
  end

  describe "to_json/1" do
    test "converts output to JSON string" do
      data = %{records: [%{"name" => "alice"}]}
      assert {:ok, json} = Etl.to_json(data)
      assert json =~ "alice"
    end
  end

  describe "pipe composition" do
    test "end-to-end with to_json" do
      csv = "name,age,email\nAlice,30,alice@example.com"

      result =
        csv
        |> Etl.run()
        |> then(fn {:ok, data} -> Etl.to_json(data) end)

      assert {:ok, json} = result
      assert json =~ "alice"
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

### Why this works

The pipe is mechanical: `a |> f(b)` is `f(a, b)` — no magic, just a rewrite. Its power comes from the discipline it enforces: every function in the chain must accept its primary input as the first argument. Once you follow that rule, `Enum`, `String`, `Map`, `Jason`, and your own modules all compose uniformly. Stage failures don't corrupt global state because nothing is stateful — each stage takes a value, returns a value, and hands off. For error handling inside a pipe, `with` or tagged-tuple fold patterns replace `try/rescue` without breaking the linear shape.

---



---
## Key Concepts

### 1. The Pipe Operator `|>` Threads Data Left-to-Right

```elixir
[1, 2, 3] |> Enum.map(&(&1 * 2)) |> Enum.filter(&(&1 > 3))
```

This reads top-to-bottom like a Unix pipeline. Without pipes, you nest function calls right-to-left. Pipes make data transformations linear and readable.

### 2. Pipes Transform Imperative-Looking Code

```elixir
user = get_user(id)
user = update_profile(user, new_name)
user = save(user)
```

Becomes:

```elixir
get_user(id) |> update_profile(new_name) |> save()
```

Clearer intent, less variable rebinding.

### 3. Pipe Carefully with Multiple-Argument Functions

`Enum.reduce([1,2,3], 0, fn x, acc -> acc + x end)` does not pipe cleanly. Use anonymous functions or reorder arguments. Pipes shine for single-argument transformations.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    csv =
      ["name,age,email"] ++
        for i <- 1..10_000 do
          "User#{i},#{20 + rem(i, 60)},user#{i}@example.com"
        end

    input = Enum.join(csv, "\n")

    {us, _} = :timer.tc(fn -> ETL.run(input) end)
    IO.puts("ETL.run 10k rows: #{us} µs (#{us / 10_000} µs/row)")
  end
end

Bench.run()
```

Target: under 50 µs per row end-to-end (parse + cast + filter + JSON encode) for typical 3–5 column CSV. Bottleneck is usually JSON encoding if the schema is wide; profile individual stages with `:timer.tc/1` if you fall below target.

---

## Designing pipe-friendly functions

The rule: the primary data being transformed goes in the first argument position.

```elixir
# Pipe-friendly — data is first argument
def normalize(record), do: ...
def cast_type(record, field, type), do: ...
def filter_valid(records), do: ...

# Pipeline reads naturally
records
|> Enum.map(&normalize/1)
|> Enum.map(&cast_type(&1, :age, :integer))
|> filter_valid()

# NOT pipe-friendly — data is last argument
def transform(options, record), do: ...
# Forces awkward usage:
records |> Enum.map(&transform(options, &1))
```

When wrapping third-party functions that do not follow this convention, create
a thin wrapper:

```elixir
# Jason.encode takes data first — pipe-friendly
data |> Jason.encode!()

# But if a library puts options first, wrap it:
def encode(data, opts \\ []), do: ThirdParty.encode(opts, data)
```

---

## When pipes hurt readability

Pipes are not always better. Avoid:

```elixir
# Bad — single-step pipe adds noise
result = input |> String.trim()
# Just write: result = String.trim(input)

# Bad — pipe with anonymous function is awkward
result = input |> (fn x -> x * 2 end).()
# Just write: result = input * 2

# Bad — pipe into a conditional
input
|> validate()
|> case do
  :ok -> process()      # Valid but unusual, prefer `with`
  {:error, _} -> halt()
end
```

Use pipes when you have 2+ transformations that flow naturally from input to output.
Use regular function calls for single operations or conditional logic.

---

## Common production mistakes

**1. Breaking the "data first" convention**
If your function signature is `process(options, data)`, it cannot be piped into
naturally. Always put the subject first.

**2. Side effects in the middle of a pipe**
```elixir
data
|> transform()
|> IO.inspect(label: "debug")  # OK for debugging
|> save_to_database()          # Side effect — harder to test
|> notify_user()               # Another side effect
```
Keep side effects at the end of the pipeline or in a separate step.

**3. Piping into `case` or `if`**
While syntactically valid, piping into `case` or `if` is unusual and confusing.
Use `with` for conditional pipelines.

**4. Too many transformations in one pipe**
A 20-step pipe is hard to debug. Break it into named functions that each represent
a meaningful transformation step.

---

## Reflection

1. An ETL stage now needs to call an external HTTP API and can fail. Do you keep the pipe shape with `with` handling `{:ok, _} | {:error, _}` tuples, introduce a `Result` monad via a library, or break the pipe at the boundary and return an explicit error? Which option scales to 10 stages with partial failures?
2. The ETL runs on a single process today. Events arrive at 500k/sec. What is the smallest architectural change that keeps the pipe abstraction but scales horizontally — `Task.async_stream/3`, `Flow`, `Broadway`, or something else? Why that one and not the others?

---

## Resources

- [Pipe operator — Elixir Getting Started](https://elixir-lang.org/getting-started/enumerables-and-streams.html#the-pipe-operator)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html)
- [Stream — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [Jason — HexDocs](https://hexdocs.pm/jason/Jason.html)
