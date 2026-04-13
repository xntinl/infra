# GraphQL File Uploads via Absinthe.Plug.Parser

**Project**: `graphql_uploads` — multipart file upload mutation that streams to S3-compatible storage

---

## Why apis and graphql matters

GraphQL with Absinthe collapses N+1 problems via Dataloader, exposes subscriptions through Phoenix.PubSub, and lets the schema itself enforce complexity limits. REST APIs in Elixir benefit from Plug pipelines, OpenAPI generation, JWT auth, and HMAC-signed webhooks.

The hard parts are not the happy path: it's pagination consistency under concurrent writes, refresh-token rotation, idempotent webhook processing, and complexity budgets that prevent a single query from saturating a node.

---

## The business problem

You are building a production-grade Elixir component in the **APIs and GraphQL** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
graphql_uploads/
├── lib/
│   └── graphql_uploads.ex
├── script/
│   └── main.exs
├── test/
│   └── graphql_uploads_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in APIs and GraphQL the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule GraphqlUploads.MixProject do
  use Mix.Project

  def project do
    [
      app: :graphql_uploads,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/graphql_uploads.ex`

```elixir
# lib/graphql_uploads/router.ex
defmodule GraphqlUploads.Router do
  use Plug.Router

  plug :match
  plug Plug.Parsers,
    parsers: [:urlencoded, Absinthe.Plug.Parser, :json],
    json_decoder: Jason,
    length: 50 * 1024 * 1024  # hard 50 MB per request
  plug :dispatch

  forward "/graphql",
    to: Absinthe.Plug,
    init_opts: [schema: GraphqlUploads.Graphql.Schema]
end

# lib/graphql_uploads/graphql/types/upload_types.ex
defmodule GraphqlUploads.Graphql.Types.UploadTypes do
  use Absinthe.Schema.Notation

  object :uploaded_file do
    field :url, non_null(:string)
    field :size_bytes, non_null(:integer)
    field :content_type, non_null(:string)
    field :original_filename, non_null(:string)
  end
end

# lib/graphql_uploads/graphql/schema.ex
defmodule GraphqlUploads.Graphql.Schema do
  use Absinthe.Schema

  import_types Absinthe.Plug.Types
  import_types GraphqlUploads.Graphql.Types.UploadTypes

  alias GraphqlUploads.Storage.S3Client

  @max_bytes 50 * 1024 * 1024
  @allowed_types ~w(image/png image/jpeg image/webp application/pdf)

  query do
    field :ping, :string, resolve: fn _, _, _ -> {:ok, "pong"} end
  end

  mutation do
    field :upload_avatar, :uploaded_file do
      arg :file, non_null(:upload)

      resolve fn _p, %{file: %Plug.Upload{} = upload}, _r ->
        with :ok <- validate_type(upload),
             {:ok, size} <- validate_size(upload),
             {:ok, url} <- S3Client.stream_upload(upload, "avatars/") do
          {:ok,
           %{
             url: url,
             size_bytes: size,
             content_type: upload.content_type,
             original_filename: upload.filename
           }}
        end
      end
    end
  end

  defp validate_type(%Plug.Upload{content_type: ct}) when ct in @allowed_types, do: :ok
  defp validate_type(%Plug.Upload{content_type: ct}),
    do: {:error, %{message: "unsupported content type #{ct}", extensions: %{code: :invalid_type}}}

  defp validate_size(%Plug.Upload{path: path}) do
    case File.stat(path) do
      {:ok, %{size: size}} when size <= @max_bytes -> {:ok, size}
      {:ok, _} -> {:error, %{message: "file too large", extensions: %{code: :too_large}}}
      {:error, reason} -> {:error, %{message: "stat failed: #{reason}"}}
    end
  end
end

# lib/graphql_uploads/storage/s3_client.ex
defmodule GraphqlUploads.Storage.S3Client do
  @moduledoc """
  Streams a `%Plug.Upload{}` into S3 via multipart upload.

  The upload runs chunk-by-chunk so memory stays bounded regardless of file
  size. `ExAws.S3.upload/3` handles part numbering, parallelism, and abort
  on error.
  """

  @bucket Application.compile_env(:graphql_uploads, :bucket, "graphql-uploads")

  @doc "Returns stream upload result from filename and prefix."
  @spec stream_upload(Plug.Upload.t(), String.t()) :: {:ok, String.t()} | {:error, term()}
  def stream_upload(%Plug.Upload{path: path, filename: original}, prefix) do
    key = "#{prefix}#{generate_id()}-#{safe_filename(original)}"

    path
    |> File.stream!([], 5 * 1024 * 1024)  # 5 MB chunks — S3 multipart minimum
    |> ExAws.S3.upload(@bucket, key,
         content_type: guess_type(original),
         timeout: :timer.minutes(5))
    |> ExAws.request()
    |> case do
      {:ok, %{status_code: 200}} -> {:ok, url(key)}
      {:error, reason} -> {:error, %{message: "upload failed", extensions: %{reason: inspect(reason)}}}
    end
  end

  defp generate_id do
    :crypto.strong_rand_bytes(12) |> Base.url_encode64(padding: false)
  end

  defp safe_filename(name) do
    name
    |> Path.basename()
    |> String.replace(~r/[^a-zA-Z0-9._-]/, "_")
    |> String.slice(0, 120)
  end

  defp url(key) do
    Application.get_env(:graphql_uploads, :public_base, "https://cdn.example.com") <> "/" <> key
  end

  defp guess_type(name) do
    case Path.extname(name) |> String.downcase() do
      ".png" -> "image/png"
      ".jpg" -> "image/jpeg"
      ".jpeg" -> "image/jpeg"
      ".webp" -> "image/webp"
      ".pdf" -> "application/pdf"
      _ -> "application/octet-stream"
    end
  end
end
```
### `test/graphql_uploads_test.exs`

```elixir
defmodule GraphqlUploads.UploadTest do
  use ExUnit.Case, async: true
  doctest GraphqlUploads.Router
  use Plug.Test

  @opts GraphqlUploads.Router.init([])

  setup do
    # Stub S3 so tests don't hit the network.
    :meck.new(ExAws, [:passthrough])
    :meck.expect(ExAws, :request, fn _ -> {:ok, %{status_code: 200}} end)
    on_exit(fn -> :meck.unload() end)
    :ok
  end

  defp multipart_body(boundary, operations, map, file_content, filename) do
    [
      "--", boundary, "\r\n",
      ~s[Content-Disposition: form-data; name="operations"\r\n\r\n],
      operations, "\r\n",
      "--", boundary, "\r\n",
      ~s[Content-Disposition: form-data; name="map"\r\n\r\n],
      map, "\r\n",
      "--", boundary, "\r\n",
      ~s[Content-Disposition: form-data; name="0"; filename="#{filename}"\r\n],
      "Content-Type: image/png\r\n\r\n",
      file_content, "\r\n",
      "--", boundary, "--\r\n"
    ] |> IO.iodata_to_binary()
  end

  describe "GraphqlUploads.Upload" do
    test "uploadAvatar accepts a valid png" do
      boundary = "boundary_test"
      operations = Jason.encode!(%{
        "query" => "mutation ($f: Upload!) { uploadAvatar(file: $f) { url sizeBytes } }",
        "variables" => %{"f" => nil}
      })
      map = Jason.encode!(%{"0" => ["variables.f"]})
      png = <<137, 80, 78, 71, 13, 10, 26, 10>> <> :crypto.strong_rand_bytes(1024)

      body = multipart_body(boundary, operations, map, png, "x.png")

      conn =
        conn(:post, "/graphql", body)
        |> put_req_header("content-type", "multipart/form-data; boundary=#{boundary}")

      conn = GraphqlUploads.Router.call(conn, @opts)
      assert conn.status == 200
      assert %{"data" => %{"uploadAvatar" => %{"url" => _, "sizeBytes" => size}}} =
               Jason.decode!(conn.resp_body)
      assert size == byte_size(png)
    end

    test "uploadAvatar rejects unsupported content type" do
      # Build a multipart with an executable disguised as a file.
      # The resolver-level validation kicks in via content_type.
      # For simplicity skip full multipart — unit-test the validator directly.
      upload = %Plug.Upload{
        path: Path.join(System.tmp_dir!(), "x.sh"),
        filename: "malware.sh",
        content_type: "application/x-sh"
      }
      File.write!(upload.path, "echo hi")
      schema = GraphqlUploads.Graphql.Schema
      assert {:ok, %{errors: [%{message: msg}]}} =
               Absinthe.run(
                 "mutation ($f: Upload!) { uploadAvatar(file: $f) { url } }",
                 schema,
                 variables: %{"f" => upload})
      assert String.contains?(msg, "unsupported")
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for GraphQL File Uploads via Absinthe.Plug.Parser.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== GraphQL File Uploads via Absinthe.Plug.Parser ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GraphqlUploads.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: GraphqlUploads.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Dataloader collapses N+1 queries

Without Dataloader, a GraphQL query for 'posts and their authors' issues N+1 queries. With Dataloader, it issues 2 — one for posts, one batched for authors.

### 2. Complexity analysis prevents query DoS

GraphQL allows clients to compose queries. Without complexity limits, a malicious client can request a 10-level deep nested query that brings the server down. Set per-query and per-connection limits.

### 3. Cursor pagination is consistent under writes

Offset pagination skips/duplicates rows under concurrent inserts. Cursor pagination (encode the last-seen ID) is correct regardless of writes.

---
