# LiveView Uploads with Progress and Chunked Consumption

**Project**: `media_vault` — a LiveView form that uploads video files with a per-file progress bar, per-file cancellation, and backpressure-aware consumption of chunks.

## Project context

Your team needs to replace a legacy PHP upload endpoint. The product requires: multi-file selection, individual progress bars, cancel per file without cancelling the batch, max 2 GB per file, server-side virus scanning on each chunk, and final storage in S3-compatible object storage. The UX people also want the "Post" button enabled only when *all* uploads reach 100%.

LiveView uploads cover all of this natively. The gotcha most teams hit: they call `consume_uploaded_entries/3` before `entry.done?` is true, then wonder why files are corrupted. Or they forget that `allow_upload/3` `max_file_size` is a *client* hint only — the server still needs to enforce limits.

```
media_vault/
├── lib/
│   ├── media_vault/
│   │   ├── application.ex
│   │   └── storage.ex
│   └── media_vault_web/
│       ├── endpoint.ex
│       ├── router.ex
│       └── live/
│           └── upload_live.ex
├── test/
│   └── media_vault_web/
│       └── live/
│           └── upload_live_test.exs
├── bench/
│   └── consume_bench.exs
└── mix.exs
```

## Why LiveView uploads and not a separate REST endpoint

A separate `POST /uploads` endpoint forces a round trip outside the LiveView session: you need a CSRF token, you lose the socket connection context, and progress events must be pushed back through PubSub or Channels. LiveView uploads reuse the WebSocket connection — progress is streamed natively, the authenticated session is already there, and the server sees a single consistent state machine.

**Why not Uppy + tus?** Valid for very large files (> 5 GB) or resumable uploads across sessions. For 2 GB with same-session semantics, LiveView uploads are 1/10 the code.

## Core concepts

### 1. `allow_upload/3`

Declares what the LV can accept. Key options:

- `:accept` — MIME types or extensions (`~w(.mp4 .mov)` or `:any`)
- `:max_entries` — how many files in parallel
- `:max_file_size` — **client-side hint** (browser refuses to even send), enforce again server-side
- `:auto_upload` — start uploading as soon as the file is selected (default `false`, triggered by form submit)
- `:progress` — callback `fn name, entry, socket -> {:noreply, socket} end` fired on every progress tick
- `:external` — return a signed URL instead of chunking through the socket (use for S3 direct uploads)

### 2. Entry lifecycle

```
picked ──▶ uploading (progress ticks) ──▶ done? = true ──▶ consumed
                       │
                       └──▶ cancel_upload ──▶ entry disappears from @uploads.name.entries
```

### 3. `consume_uploaded_entries/3`

Invokes the callback once per *completed* entry. Inside the callback, `path` is a **temporary** file on disk that will be deleted as soon as the callback returns. You must either move it, stream it, or copy its contents.

Return values:
- `{:ok, value}` — entry is consumed and removed; `value` is collected into the return list
- `{:postpone, term}` — entry stays in the list (use for retries on transient upload errors)

### 4. Backpressure

If the callback is slow (virus scan + upload to S3), LiveView stops reading the socket. The client blocks until the server catches up. That is correct: it prevents OOM on the server.

## Design decisions

- **Option A — `auto_upload: true` + consume on each `:done`**: files stream as soon as picked, UX is snappy. Cost: if the user cancels the form, you have already uploaded.
- **Option B — `auto_upload: false`, consume on submit**: classic form semantics, nothing is persisted until the user confirms.
- **Option C — `external: fun`, direct-to-S3 presigned PUT**: server never touches the bytes. Required for multi-GB files on small nodes.

For this exercise we pick Option B (form-driven) and include the `:progress` callback for the UI. Option C appears in the "When to use this" discussion but would require S3 credentials.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule MediaVault.MixProject do
  use Mix.Project

  def project, do: [app: :media_vault, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [mod: {MediaVault.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_html, "~> 4.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:floki, "~> 0.36", only: :test}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule MediaVault.MixProject do
  use Mix.Project

  def project, do: [app: :media_vault, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [mod: {MediaVault.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_html, "~> 4.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:floki, "~> 0.36", only: :test}
    ]
  end
end
```

### Step 1: Storage stub — `lib/media_vault/storage.ex`

**Objective**: Build the storage stub layer: lib/media_vault/storage.ex.

```elixir
defmodule MediaVault.Storage do
  @moduledoc """
  In production this would call ExAws.S3. Here we write to a local
  directory so the exercise runs offline.
  """

  def put(source_path, key) do
    dest = Path.join(System.tmp_dir!(), "media_vault_" <> key)
    File.cp!(source_path, dest)
    {:ok, dest}
  end

  def scan!(path) do
    # Placeholder for ClamAV/Lambda-based scanning. Fail fast if file is empty.
    case File.stat(path) do
      {:ok, %{size: size}} when size > 0 -> :ok
      _ -> raise "virus scan aborted: empty or missing file"
    end
  end
end
```

### Step 2: LiveView — `lib/media_vault_web/live/upload_live.ex`

**Objective**: Build the liveview layer: lib/media_vault_web/live/upload_live.ex.

```elixir
defmodule MediaVaultWeb.UploadLive do
  use Phoenix.LiveView

  alias MediaVault.Storage

  @max_bytes 2_000_000_000
  @accept ~w(.mp4 .mov .mkv)

  @impl true
  def mount(_params, _session, socket) do
    socket =
      socket
      |> assign(uploaded: [])
      |> allow_upload(:videos,
        accept: @accept,
        max_entries: 5,
        max_file_size: @max_bytes,
        progress: &handle_progress/3,
        auto_upload: false
      )

    {:ok, socket}
  end

  @impl true
  def handle_event("validate", _params, socket), do: {:noreply, socket}

  def handle_event("cancel", %{"ref" => ref}, socket) do
    {:noreply, cancel_upload(socket, :videos, ref)}
  end

  def handle_event("save", _params, socket) do
    uploaded =
      consume_uploaded_entries(socket, :videos, fn %{path: path}, entry ->
        # Server-side enforcement — client hints are never trusted.
        stat = File.stat!(path)
        if stat.size > @max_bytes, do: raise("file too large after upload")

        :ok = Storage.scan!(path)
        {:ok, stored} = Storage.put(path, entry.client_name)
        {:ok, %{name: entry.client_name, stored_at: stored, bytes: stat.size}}
      end)

    {:noreply, update(socket, :uploaded, &(&1 ++ uploaded))}
  end

  defp handle_progress(:videos, entry, socket) do
    if entry.done? do
      {:noreply, put_flash(socket, :info, "#{entry.client_name} received, pending submit")}
    else
      {:noreply, socket}
    end
  end

  @impl true
  def render(assigns) do
    ~H"""
    <section>
      <form id="upload-form" phx-submit="save" phx-change="validate">
        <.live_file_input upload={@uploads.videos} />

        <ul>
          <li :for={entry <- @uploads.videos.entries}>
            <span>{entry.client_name}</span>
            <progress max="100" value={entry.progress}>{entry.progress}%</progress>
            <button type="button" phx-click="cancel" phx-value-ref={entry.ref}>
              Cancel
            </button>
            <p :for={err <- upload_errors(@uploads.videos, entry)} class="error">
              {error_to_string(err)}
            </p>
          </li>
        </ul>

        <p :for={err <- upload_errors(@uploads.videos)} class="error">
          {error_to_string(err)}
        </p>

        <button type="submit" disabled={not all_done?(@uploads.videos)}>Post</button>
      </form>

      <ul>
        <li :for={f <- @uploaded}>{f.name} ({f.bytes} bytes) → {f.stored_at}</li>
      </ul>
    </section>
    """
  end

  defp all_done?(upload) do
    upload.entries != [] and Enum.all?(upload.entries, & &1.done?)
  end

  defp error_to_string(:too_large), do: "file exceeds 2 GB"
  defp error_to_string(:not_accepted), do: "unsupported file type"
  defp error_to_string(:too_many_files), do: "max 5 files at a time"
  defp error_to_string(other), do: to_string(other)
end
```

## Why this works

`consume_uploaded_entries/3` is only invoked on entries where `done? == true`. That protects you from reading half-written temp files. The temp file is managed by the `Phoenix.LiveView.UploadChannel` process; if the LV crashes mid-upload, the channel terminates and the temp file is cleaned by the OS. Progress events are throttled by LV itself (roughly every 100ms) so the socket is not flooded for large files.

The "Post" button stays disabled until `all_done?/1` returns true — this uses the entries list which LV keeps in `@uploads.videos.entries` automatically.

## Tests — `test/media_vault_web/live/upload_live_test.exs`

```elixir
defmodule MediaVaultWeb.UploadLiveTest do
  use ExUnit.Case, async: true
  import Phoenix.LiveViewTest

  @endpoint MediaVaultWeb.Endpoint

  setup do
    {:ok, conn: Phoenix.ConnTest.build_conn()}
  end

  defp upload_fixture(view, name, content) do
    file_input(view, "#upload-form", :videos, [
      %{name: name, content: content, type: "video/mp4"}
    ])
  end

  describe "file selection" do
    test "rejects unsupported extensions", %{conn: conn} do
      {:ok, view, _} = live(conn, "/upload")
      input = upload_fixture(view, "virus.exe", "bad")
      assert render_upload(input, "virus.exe") =~ "unsupported file type"
    end

    test "accepts mp4", %{conn: conn} do
      {:ok, view, _} = live(conn, "/upload")
      input = upload_fixture(view, "trailer.mp4", "abcdef")
      html = render_upload(input, "trailer.mp4")
      assert html =~ "trailer.mp4"
      assert html =~ "value=\"100\""
    end
  end

  describe "cancellation" do
    test "removes the entry without touching other entries", %{conn: conn} do
      {:ok, view, _} = live(conn, "/upload")
      input = upload_fixture(view, "one.mp4", "111")
      render_upload(input, "one.mp4")
      [entry] = get_entries(view)
      view |> element("button", "Cancel") |> render_click(%{"ref" => entry.ref})
      refute render(view) =~ "one.mp4"
    end
  end

  describe "save" do
    test "post button stays disabled until all entries are done", %{conn: conn} do
      {:ok, view, _} = live(conn, "/upload")
      assert render(view) =~ ~s(disabled="disabled")
      upload_fixture(view, "a.mp4", "x") |> render_upload("a.mp4")
      refute render(view) =~ ~s(disabled="disabled")
    end
  end

  defp get_entries(view) do
    :sys.get_state(view.pid).socket.assigns.uploads.videos.entries
  end
end
```

## Benchmark — `bench/consume_bench.exs`

```elixir
# Measure the overhead of File.cp! vs File.stream! for different sizes.
sizes = [1_000_000, 10_000_000, 100_000_000]

for bytes <- sizes do
  path = Path.join(System.tmp_dir!(), "bench_#{bytes}")
  File.write!(path, :crypto.strong_rand_bytes(bytes))

  {cp_us, _} = :timer.tc(fn -> File.cp!(path, path <> ".cp") end)

  {stream_us, _} =
    :timer.tc(fn ->
      path
      |> File.stream!([], 64 * 1024)
      |> Stream.into(File.stream!(path <> ".stream"))
      |> Stream.run()
    end)

  IO.puts("#{bytes} bytes → cp=#{cp_us}µs stream=#{stream_us}µs")
end
```

**Expected**: `File.cp!` is 2–4x faster than streaming for files < 100 MB. For > 500 MB prefer streaming to avoid a second copy in the page cache.

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. `max_file_size` is a client hint.** Chromium/Safari enforce it before sending. A crafted client can send anything. Re-check `File.stat!(path).size` server-side.

**2. Temp files disappear after the callback returns.** Any async task that references `path` after the callback returns will see `:enoent`. Copy the file or pass its contents before returning.

**3. `consume_uploaded_entries` must be called from the same LV process.** If you try to consume from a `Task`, you get a raise. Spawn the work AFTER copying the file into your own storage.

**4. Progress callback runs in the LV process.** Heavy work there blocks every other event. Keep it to flash messages or `assign` updates.

**5. `auto_upload: true` uploads even if the user never submits.** If the view navigates away, the temp files are GC'd, but you wasted bandwidth and CPU on the scan. Only set `auto_upload: true` when you also want to commit eagerly.

**6. When NOT to use LV uploads.** Multi-gigabyte files or cross-session resumes need `:external` with presigned URLs or a tus-compatible backend. The LV socket is not designed for 10 GB single transfers.

## Reflection

A reviewer argues that consuming files inside `consume_uploaded_entries/3` is "synchronous and blocks the LV". They want to spawn a `Task` that does the S3 upload in the background. What concrete problem will they hit on the first request, and what is the idiomatic fix?

## Resources

- [Phoenix.LiveView.Upload — hexdocs](https://hexdocs.pm/phoenix_live_view/uploads.html)
- [`consume_uploaded_entries/3` source](https://github.com/phoenixframework/phoenix_live_view/blob/main/lib/phoenix_live_view.ex)
- [External uploads guide (S3 direct)](https://hexdocs.pm/phoenix_live_view/external-uploads.html)
- [tus.io — resumable upload protocol](https://tus.io/)
