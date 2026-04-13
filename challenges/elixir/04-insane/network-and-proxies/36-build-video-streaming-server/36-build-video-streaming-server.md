# Adaptive Bitrate Video Streaming Server

**Project**: `hls_server` — an HLS-compatible adaptive bitrate streaming server

---

## Project context

You are building `hls_server`, a streaming backend the media team will use for internal video delivery. It must segment video files into HLS-compatible chunks, generate M3U8 playlists, serve segments with proper range request support, simulate a CDN distribution layer, and expose real-time metrics. Clients adapt their quality automatically based on measured download throughput.

Project structure:

```
hls_server/
├── lib/
│   └── hls_server/
│       ├── application.ex
│       ├── segmenter.ex           # ← binary splitting into .ts segments
│       ├── playlist/
│       │   ├── master.ex          # ← master.m3u8 with variant streams
│       │   └── media.ex           # ← media.m3u8 per variant + live sliding window
│       ├── segment_store.ex       # ← ETS-backed segment registry + TTL cleanup
│       ├── range_handler.ex       # ← HTTP/1.1 range requests (RFC 7233)
│       ├── live_producer.ex       # ← periodic segment generation for live mode
│       ├── cdn/
│       │   ├── node.ex            # ← CDN node GenServer (cache + origin fetch)
│       │   └── router.ex          # ← weighted node selection + failover
│       ├── abr_client.ex          # ← simulated adaptive bitrate client
│       └── metrics.ex             # ← ETS counters + /metrics endpoint
├── test/
│   └── hls_server/
│       ├── segmenter_test.exs
│       ├── playlist_test.exs
│       ├── range_handler_test.exs
│       ├── live_producer_test.exs
│       └── cdn_test.exs
├── bench/
│   └── segment_serve_bench.exs
└── mix.exs
```

---

## Why pre-segmented storage over on-the-fly segmentation and not live slicing from MP4 on request

pre-segmenting amortizes I/O and makes every segment independently cacheable at the CDN edge. On-the-fly slicing means every request touches the origin disk and can't be CDN-cached.

## Design decisions

**Option A — serving whole files from disk on every request**
- Pros: simplest implementation
- Cons: no adaptive bitrate, high bandwidth waste on mobile

**Option B — HLS segment-based delivery with adaptive bitrate manifests** (chosen)
- Pros: mobile-friendly, CDN-cacheable, supports seek/pause cheaply
- Cons: requires transcoding pipeline, manifest generation

→ Chose **B** because adaptive bitrate is the baseline user expectation in 2025; anything else is unshippable.

## The business problem

The media team streams training videos to employees in three office locations. Each location has different available bandwidth. A single fixed-bitrate stream means the high-bandwidth office gets poor quality and the low-bandwidth office buffers constantly. Adaptive bitrate streaming solves this: each client measures its download speed and switches to the appropriate quality variant automatically.

Two design constraints shape the implementation:

1. **Zero unnecessary binary copies** — video segments can be hundreds of megabytes. Every copy doubles memory usage. Elixir's binary sharing means sub-binaries reference the parent — use `:binary.part/3` instead of `String.slice`.
2. **Metrics via atomic counters** — the metrics endpoint must not be a bottleneck. Every segment served goes through an ETS `update_counter` — no GenServer, no lock.

---

## Why HLS uses fixed-duration segments

HLS segments are designed around one invariant: a client can switch quality variants at any segment boundary. If segments have variable durations, calculating the next segment's URL for a different variant becomes ambiguous. Fixed durations mean variant playlists are always in sync: segment 5 in the 720p variant covers exactly the same time range as segment 5 in the 360p variant.

The `#EXT-X-TARGETDURATION` playlist tag must equal the maximum segment duration in the playlist, rounded up to the nearest integer second. A player uses this to decide how long to buffer before starting playback.

---

## Why the sliding window matters for live streaming

A live stream generates segments indefinitely. Without a window, the media.m3u8 grows unbounded — a client that joins late would need to download all past segments to seek to the live edge. HLS specifies that a live playlist should keep only the last N segments (typically 3-5). Older segments are removed from the playlist and their storage is freed.

The `#EXT-X-MEDIA-SEQUENCE` tag tells clients which sequence number the first segment in the current playlist has. This prevents clients from re-requesting segments they have already seen.

---

## Implementation

### Step 1: Create the project

**Objective**: Use `--sup` so the playlist agent and Cowboy listener boot together and restart cleanly on a crash.


```bash
mix new hls_server --sup
cd hls_server
mkdir -p lib/hls_server/{playlist,cdn}
mkdir -p test/hls_server bench
```

### Step 2: `mix.exs`

**Objective**: Pick Cowboy via `plug_cowboy` — streaming HLS needs chunked responses, not a blocking request/response facade.


```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/hls_server/segmenter.ex`

**Objective**: Slice with `:binary.part/3` to share memory — a 1 GB video in 100 segments must not cost 2 GB of RAM.


The segmenter splits a video binary into fixed-byte-size segments. In real HLS, segments must start on keyframes; this implementation uses fixed byte boundaries as a simulation.

The critical design choice is using `:binary.part/3` for segment extraction. This function returns a sub-binary that shares memory with the parent binary -- no data is copied. A 1GB video split into 100 segments uses ~1GB total, not ~2GB. If you instead used binary pattern matching like `<<chunk::binary-size(n), _rest::binary>>`, each `chunk` would be a new allocation because the compiler cannot prove the reference is safe to share.

The `segment_duration/2` function correctly handles the last segment, which is almost always shorter than the configured duration. Reporting the wrong duration in `#EXTINF` causes players to stall waiting for bytes that do not exist.

```elixir
defmodule HLSServer.Segmenter do
  @moduledoc """
  Splits a video binary into fixed-size segments.

  In real HLS, segments must start on keyframes. This implementation uses
  fixed byte boundaries as a simulation -- treating each chunk as if it
  starts on a keyframe, which is valid for our testing purposes.

  Memory design: store the entire video binary in an Agent (or :persistent_term).
  Sub-segments are created with :binary.part/3, which returns a sub-binary that
  SHARES the parent binary's memory. The original binary is not copied.

  If you use binary slice operations that trigger a copy (e.g., pattern matching
  that extracts into a new variable), you pay 2x memory. Measure with
  :erlang.memory(:binary) before and after segmentation.
  """

  @type t :: %__MODULE__{}

  defstruct [:video_binary, :segment_size_bytes, :num_segments, :duration_s]

  @doc """
  Loads a video binary and computes segment metadata.
  segment_duration_s: target duration of each segment in seconds
  total_duration_s: total video duration (used to calculate byte-rate)
  """
  @spec load(binary(), pos_integer(), pos_integer()) :: t()
  def load(video_binary, segment_duration_s, total_duration_s) do
    bytes_per_second = div(byte_size(video_binary), total_duration_s)
    segment_size = bytes_per_second * segment_duration_s
    num_segments = ceil(byte_size(video_binary) / segment_size)

    %__MODULE__{
      video_binary: video_binary,
      segment_size_bytes: segment_size,
      num_segments: num_segments,
      duration_s: segment_duration_s
    }
  end

  @doc """
  Returns segment N as a binary slice (no copy).
  Returns {:ok, binary} or {:error, :out_of_range}.
  """
  @spec get_segment(t(), non_neg_integer()) :: {:ok, binary()} | {:error, :out_of_range}
  def get_segment(%__MODULE__{} = seg, index) when index >= 0 do
    offset = index * seg.segment_size_bytes
    total_size = byte_size(seg.video_binary)

    if offset >= total_size do
      {:error, :out_of_range}
    else
      length = min(seg.segment_size_bytes, total_size - offset)
      # :binary.part/3 returns a sub-binary referencing the same underlying
      # memory as the parent. This is the zero-copy operation that keeps
      # memory usage at O(video_size) instead of O(video_size * num_segments).
      {:ok, :binary.part(seg.video_binary, offset, length)}
    end
  end

  @doc """
  Returns the actual duration of segment N in seconds.
  All segments except the last have duration = segment_duration_s.
  The last segment may be shorter.
  """
  @spec segment_duration(t(), non_neg_integer()) :: float()
  def segment_duration(%__MODULE__{} = seg, index) do
    if index < seg.num_segments - 1 do
      # All segments except the last have the full configured duration
      seg.duration_s * 1.0
    else
      # The last segment covers whatever bytes remain. Its duration is
      # proportional to its byte size relative to a full segment.
      total_size = byte_size(seg.video_binary)
      last_offset = index * seg.segment_size_bytes
      last_length = total_size - last_offset
      # Duration scales linearly with byte count (constant bitrate assumption)
      last_length / seg.segment_size_bytes * seg.duration_s * 1.0
    end
  end
end
```

### Step 4: `lib/hls_server/playlist/media.ex`

**Objective**: Evict segments past the sliding window and bump `EXT-X-MEDIA-SEQUENCE` — otherwise live playlists grow unbounded.


The media playlist generates HLS-compliant `.m3u8` output for a single quality variant. It supports both VOD (video on demand, with `#EXT-X-ENDLIST`) and live (sliding window, no endlist) modes.

For live playlists, `add_segment/3` appends a new segment and evicts the oldest when the window size is exceeded. The `media_sequence` counter increments with each eviction, telling clients that earlier segments are no longer available. This is how HLS handles unbounded live streams without unbounded playlists.

```elixir
defmodule HLSServer.Playlist.Media do
  @moduledoc """
  Generates and updates HLS media playlists (media.m3u8).

  Playlist format:
    #EXTM3U
    #EXT-X-VERSION:3
    #EXT-X-TARGETDURATION:6
    #EXT-X-MEDIA-SEQUENCE:0

    #EXTINF:6.000,
    /segments/720p/0.ts
    #EXTINF:6.000,
    /segments/720p/1.ts
    ...
    #EXT-X-ENDLIST    (only for VOD, not live)

  For live streams:
  - no #EXT-X-ENDLIST tag
  - #EXT-X-MEDIA-SEQUENCE increments as old segments are evicted
  - only the last K segments appear in the playlist
  """

  @type t :: %__MODULE__{}

  defstruct [
    :variant_name,
    :segment_duration_s,
    :window_size,      # max segments in live playlist (nil for VOD)
    segments: [],      # [{sequence_number, duration_s, url}]
    media_sequence: 0
  ]

  @doc "Creates a VOD playlist from a list of {sequence, duration, url} tuples."
  def new_vod(variant_name, segments, segment_duration_s) do
    %__MODULE__{
      variant_name: variant_name,
      segment_duration_s: segment_duration_s,
      window_size: nil,
      segments: segments,
      media_sequence: 0
    }
  end

  @doc "Creates a live playlist. new_segment/2 adds segments and evicts old ones."
  def new_live(variant_name, segment_duration_s, window_size \\ 5) do
    %__MODULE__{
      variant_name: variant_name,
      segment_duration_s: segment_duration_s,
      window_size: window_size,
      segments: [],
      media_sequence: 0
    }
  end

  @doc "Appends a segment to a live playlist, evicting the oldest if window is full."
  @spec add_segment(t(), float(), String.t()) :: t()
  def add_segment(%__MODULE__{window_size: w} = playlist, duration_s, url) do
    next_seq = playlist.media_sequence + length(playlist.segments)
    new_seg = {next_seq, duration_s, url}
    new_segments = playlist.segments ++ [new_seg]

    if w != nil and length(new_segments) > w do
      # Evict the oldest segment by dropping the head of the list.
      # Incrementing media_sequence signals to clients that the evicted
      # segment is no longer available, preventing them from requesting it.
      %{playlist | segments: tl(new_segments), media_sequence: playlist.media_sequence + 1}
    else
      %{playlist | segments: new_segments}
    end
  end

  @doc "Renders the playlist to a string."
  @spec render(t()) :: String.t()
  def render(%__MODULE__{} = playlist) do
    target_duration = playlist.segment_duration_s |> ceil() |> trunc()

    header = """
    #EXTM3U
    #EXT-X-VERSION:3
    #EXT-X-TARGETDURATION:#{target_duration}
    #EXT-X-MEDIA-SEQUENCE:#{playlist.media_sequence}
    """

    segments_text =
      Enum.map_join(playlist.segments, "\n", fn {_seq, duration, url} ->
        "#EXTINF:#{:erlang.float_to_binary(duration, decimals: 3)},\n#{url}"
      end)

    endlist = if playlist.window_size == nil, do: "\n#EXT-X-ENDLIST\n", else: "\n"

    header <> "\n" <> segments_text <> endlist
  end
end
```

### Step 5: `lib/hls_server/range_handler.ex`

**Objective**: Honor RFC 7233 Range headers exactly — a wrong Content-Range breaks resume and seek on every major player.


The range handler implements RFC 7233 (HTTP Range Requests) for video segments. It parses the `Range` header, validates the requested byte range against the content length, and returns either the full content (200) or a partial content slice (206).

Three range formats are supported:
- `bytes=N-M` : specific byte range [N, M] inclusive
- `bytes=N-` : from offset N to end of file
- `bytes=-N` : last N bytes of file

Out-of-range requests return 416 (Range Not Satisfiable). The `Content-Range` header in the 206 response tells the client exactly which bytes it received and the total content length, enabling the client to request subsequent ranges for resumable downloads.

```elixir
defmodule HLSServer.RangeHandler do
  @moduledoc """
  Handles HTTP Range requests (RFC 7233) for video segments.

  Range header format: "Range: bytes=0-1023"
  Response: HTTP 206 Partial Content with Content-Range header.

  This enables:
  1. Seeking: a player can jump to position X without downloading from the start.
  2. Resumable downloads: interrupted transfers can resume from the last byte.
  3. Parallel chunk downloads: multiple range requests for different parts.
  """

  @doc """
  Processes a Range header and returns the appropriate response map.
  Returns {:ok, %{status: 206, body: binary, headers: [...]}}
       or {:ok, %{status: 200, body: binary, headers: [...]}} if no Range header
       or {:error, 416} for unsatisfiable ranges.
  """
  @spec handle(binary(), String.t() | nil) :: {:ok, map()} | {:error, 416}
  def handle(full_content, range_header) when is_nil(range_header) do
    {:ok, %{
      status: 200,
      body: full_content,
      headers: [{"content-length", to_string(byte_size(full_content))}]
    }}
  end

  def handle(full_content, "bytes=" <> range_spec) do
    total = byte_size(full_content)

    with {:ok, {first, last}} <- parse_range(range_spec, total),
         true <- first <= last and last < total do
      length = last - first + 1
      body = :binary.part(full_content, first, length)

      {:ok, %{
        status: 206,
        body: body,
        headers: [
          {"content-range", "bytes #{first}-#{last}/#{total}"},
          {"content-length", to_string(length)},
          {"accept-ranges", "bytes"}
        ]
      }}
    else
      _ -> {:error, 416}
    end
  end

  defp parse_range(spec, total) do
    cond do
      String.contains?(spec, "-") ->
        case String.split(spec, "-") do
          ["", suffix] ->
            n = String.to_integer(suffix)
            {:ok, {total - n, total - 1}}

          [prefix, ""] ->
            first = String.to_integer(prefix)
            {:ok, {first, total - 1}}

          [prefix, suffix] ->
            {:ok, {String.to_integer(prefix), String.to_integer(suffix)}}
        end

      true ->
        {:error, :invalid_range}
    end
  end
end
```

### Step 6: Given tests — must pass without modification

**Objective**: Tests lock the HLS and Range contracts — any drift here is a player-compatibility bug, not a test bug.


```elixir
# test/hls_server/segmenter_test.exs
defmodule HLSServer.SegmenterTest do
  use ExUnit.Case, async: true

  alias HLSServer.Segmenter

  @video_size 1_000_000  # 1MB simulated video

  setup do
    video = :crypto.strong_rand_bytes(@video_size)
    seg = Segmenter.load(video, 6, 60)
    %{seg: seg, video: video}
  end


  describe "Segmenter" do

  test "produces correct number of segments", %{seg: seg} do
    # 1MB at ~16.6KB/s over 60s, split into 6s chunks → ~10 segments
    assert seg.num_segments > 0
  end

  test "all segments reassemble to original video", %{seg: seg, video: video} do
    reassembled =
      for i <- 0..(seg.num_segments - 1), reduce: <<>> do
        acc ->
          {:ok, chunk} = Segmenter.get_segment(seg, i)
          acc <> chunk
      end

    assert reassembled == video
  end

  test "out of range returns error", %{seg: seg} do
    assert {:error, :out_of_range} = Segmenter.get_segment(seg, seg.num_segments + 100)
  end

  test "segment is a sub-binary (no copy)", %{seg: seg} do
    {:ok, seg0} = Segmenter.get_segment(seg, 0)
    # If :binary.part is used, the sub-binary shares memory with the parent.
    # We can't test this directly, but we can verify the size is correct.
    assert byte_size(seg0) == seg.segment_size_bytes
  end


  end
end
```

```elixir
# test/hls_server/playlist_test.exs
defmodule HLSServer.PlaylistTest do
  use ExUnit.Case, async: true

  alias HLSServer.Playlist.Media


  describe "Playlist" do

  test "VOD playlist contains #EXT-X-ENDLIST" do
    segments = for i <- 0..4, do: {i, 6.0, "/segments/720p/#{i}.ts"}
    playlist = Media.new_vod("720p", segments, 6)
    rendered = Media.render(playlist)
    assert String.contains?(rendered, "#EXT-X-ENDLIST")
  end

  test "live playlist does not contain #EXT-X-ENDLIST" do
    playlist = Media.new_live("720p", 6, 3)
    playlist = Media.add_segment(playlist, 6.0, "/segments/720p/0.ts")
    rendered = Media.render(playlist)
    refute String.contains?(rendered, "#EXT-X-ENDLIST")
  end

  test "live playlist evicts old segments when window exceeded" do
    playlist =
      Enum.reduce(0..4, Media.new_live("720p", 6, 3), fn i, p ->
        Media.add_segment(p, 6.0, "/segments/720p/#{i}.ts")
      end)

    # Window of 3: segments 0 and 1 should be evicted
    assert length(playlist.segments) == 3
    assert playlist.media_sequence == 2
  end

  test "#EXT-X-MEDIA-SEQUENCE increments with evictions" do
    playlist =
      Enum.reduce(0..9, Media.new_live("720p", 6, 5), fn i, p ->
        Media.add_segment(p, 6.0, "/seg/#{i}.ts")
      end)

    rendered = Media.render(playlist)
    assert String.contains?(rendered, "#EXT-X-MEDIA-SEQUENCE:5")
  end


  end
end
```

```elixir
# test/hls_server/range_handler_test.exs
defmodule HLSServer.RangeHandlerTest do
  use ExUnit.Case, async: true

  alias HLSServer.RangeHandler

  @content "0123456789"  # 10 bytes for easy math


  describe "RangeHandler" do

  test "no Range header returns 200 with full content" do
    {:ok, resp} = RangeHandler.handle(@content, nil)
    assert resp.status == 200
    assert resp.body == @content
  end

  test "bytes=0-4 returns first 5 bytes" do
    {:ok, resp} = RangeHandler.handle(@content, "bytes=0-4")
    assert resp.status == 206
    assert resp.body == "01234"
  end

  test "bytes=5- returns from offset to end" do
    {:ok, resp} = RangeHandler.handle(@content, "bytes=5-")
    assert resp.status == 206
    assert resp.body == "56789"
  end

  test "bytes=-3 returns last 3 bytes" do
    {:ok, resp} = RangeHandler.handle(@content, "bytes=-3")
    assert resp.status == 206
    assert resp.body == "789"
  end

  test "out of range returns 416" do
    assert {:error, 416} = RangeHandler.handle(@content, "bytes=100-200")
  end


  end
end
```

### Step 7: Run the tests

**Objective**: Run with `--trace` so segment-eviction order and Range boundary cases surface in sequence on failure.


```bash
mix test test/hls_server/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the video streaming (HLS/DASH) invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.


## Main Entry Point

```elixir
def main do
  IO.puts("======== 36-build-video-streaming-server ========")
  IO.puts("Build video streaming server")
  IO.puts("")
  
  HLSServer.Segmenter.start_link([])
  IO.puts("HLSServer.Segmenter started")
  
  IO.puts("Run: mix test")
end
```



## Benchmark

```elixir
# bench/hls_bench.exs
video = :crypto.strong_rand_bytes(100_000_000)  # 100MB simulated video
seg = HLSServer.Segmenter.load(video, 6, 600)   # 10-min video, 6s segments

Benchee.run(%{
  "segment_extraction_1mb" => fn ->
    {:ok, _} = HLSServer.Segmenter.get_segment(seg, rem(:rand.uniform(1000), seg.num_segments))
  end,
  "range_request_parsing" => fn ->
    HLSServer.RangeHandler.handle(binary_part(video, 0, 1_000_000), "bytes=0-100000")
  end,
  "playlist_rendering_vod" => fn ->
    segments = for i <- 0..99, do: {i, 6.0, "/segments/720p/#{i}.ts"}
    playlist = HLSServer.Playlist.Media.new_vod("720p", segments, 6)
    HLSServer.Playlist.Media.render(playlist)
  end,
  "playlist_eviction_live" => fn ->
    playlist = Enum.reduce(0..99, HLSServer.Playlist.Media.new_live("720p", 6, 10), fn i, p ->
      HLSServer.Playlist.Media.add_segment(p, 6.0, "/segments/720p/#{i}.ts")
    end)
    HLSServer.Playlist.Media.render(playlist)
  end
}, time: 10, warmup: 3)
```

## Quick start

```bash
# Start the application
mix deps.get
mix test

# Or run the benchmark:
mix run bench/hls_server_bench.exs
```

Target: sustained >10 Gbps per node with <100ms segment TTFB.

## Key Concepts: Video Streaming Codecs and HLS Manifests

Video streaming must adapt to variable bandwidth. HLS (HTTP Live Streaming) segments video into chunks that clients can fetch independently, allowing quality switching mid-stream.

**Segment size and duration**: HLS segments are fixed-duration (typically 6-10 seconds). Longer segments reduce overhead (fewer HTTP requests, smaller manifest) but increase time-to-switch-quality and buffer latency. Shorter segments provide faster adaptation but increase manifest size. The MPEG DASH standard also uses segment duration as a key parameter for player synchronization.

**Variant playlists and ABR (Adaptive Bitrate)**: A master.m3u8 lists multiple quality variants (360p, 720p, 1080p). Each variant is a separate media.m3u8. Players measure download throughput and switch to the appropriate variant. All variants have the same timeline — segment N in 360p covers the same time as segment N in 1080p, enabling seamless quality switches.

**HTTP Range requests (RFC 7233)**: Clients can request a byte range from a segment to resume interrupted downloads. The server returns 206 (Partial Content) with a Content-Range header. The Content-Range tells the client exactly which bytes arrived, so the client knows what to request next. This enables parallel chunk downloads and seek operations.

**Sliding window for live playlists**: A live stream generates segments indefinitely. Without windowing, the playlist grows unbounded. HLS specifies that live playlists should contain only the last N segments (typically 3-5). The MEDIA-SEQUENCE tag tells clients which segment number the first entry is, allowing clients to resume without re-requesting earlier segments.

**Production insight**: Video streaming seems simple (segment, list, serve) but is unforgiving. Wrong segment durations break player buffers. Missing or wrong Range headers break resumption. CDN nodes returning stale segments after they're evicted from the live window confuse players. Test with real HLS players (hls.js, VLC, Apple TV) and degraded networks (throttle to 1Mbps, packet loss, latency spikes).

---

## Trade-off analysis

| Aspect | HLS (your impl) | MPEG-DASH | WebRTC |
|--------|----------------|-----------|--------|
| Latency | 15-30s (segment duration) | 4-12s | < 500ms |
| CDN compatibility | excellent (plain HTTP) | excellent | limited |
| Adaptive bitrate | yes (variant playlists) | yes | yes |
| Seeking support | segment granularity | byte-range | N/A |
| Player ecosystem | native iOS/macOS, hls.js | dash.js, Shaka | browser native |
| Server complexity | moderate | higher | high (signaling + DTLS) |

Reflection: HLS has an inherent minimum latency equal to the segment duration plus the number of segments a player buffers before starting playback. If you reduce segment duration from 6s to 1s, what happens to the number of playlist entries a client must fetch per minute? What is the trade-off?

---

## Common production mistakes

**1. Using `String.split` on video binary data**
Video segments contain arbitrary bytes including null bytes and non-UTF-8 sequences. `String.split` and string operations assume UTF-8. Use `:binary.part/3` and `binary_part/3` for all segment operations.

**2. Storing segments as GenServer state**
A GenServer holding 10 segments of 500KB each has 5MB in its heap. GC pressure and heap fragmentation grow quickly. Use ETS with the segment binary as the value — it lives off the heap.

**3. Not handling the last segment's duration correctly**
The last segment is almost always shorter than `segment_duration_s`. If you report it as the full duration in `#EXTINF`, players may stall waiting for bytes that don't exist. Always compute and report the actual duration.

**4. CDN node returning stale segments after live window eviction**
A CDN node caches segment 0. The live producer evicts segment 0. A client requests segment 0 from the CDN node — it returns the cached version. But the segment is no longer in the origin's `media.m3u8`. The CDN must respect the live window's eviction and purge cached segments when they are removed from the playlist.

**5. Metrics endpoint as a GenServer.call bottleneck**
If `GET /metrics` calls a GenServer to read counters, it serializes with all counter updates. Use `:ets.update_counter/3` for writes (atomic, no GenServer) and `:ets.lookup` for reads (concurrent, no lock).

---

## Reflection

If 10k users request the same segment within 100ms of each other (a live event), what protects the origin — CDN shielding, a request coalescer in the origin, or both? Sketch the failure mode if you pick only one.

## Resources

- [RFC 8216 — HTTP Live Streaming](https://www.rfc-editor.org/rfc/rfc8216) — the HLS specification; sections 4 (playlist format) and 6 (protocol version compatibility) are essential
- [RFC 7233 — HTTP Range Requests](https://www.rfc-editor.org/rfc/rfc7233) — sections 2 (Range header) and 4 (206 response) define what your range handler must implement
- [Apple HLS Authoring Specification](https://developer.apple.com/documentation/http-live-streaming/hls-authoring-specification-for-apple-devices) — Apple's additional constraints on valid HLS streams
- [MPEG-DASH ISO/IEC 23009-1](https://www.iso.org/standard/79329.html) — the alternative to HLS; useful for understanding why HLS made certain design choices
- [Erlang `:binary` module](https://www.erlang.org/doc/man/binary.html) — study `:binary.part/3` and the memory sharing semantics for large binary operations
