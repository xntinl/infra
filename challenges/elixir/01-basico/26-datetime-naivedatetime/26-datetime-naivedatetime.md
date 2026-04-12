# Date, Time, DateTime and Timezones: Building a Meeting Scheduler

**Project**: `meet_sched` — detects overlapping meetings across attendees in different timezones

**Difficulty**: ★★☆☆☆
**Estimated time**: 2-3 hours

---

## Why date/time handling matters for a senior developer

Timezones are where good systems quietly rot. Elixir splits the problem
into four distinct types so you can express intent precisely:

- **`Date`**: a calendar date, no time, no zone. Birthdays, billing cycles.
- **`Time`**: a wall-clock time with no date and no zone. "Daily 09:00".
- **`NaiveDateTime`**: a date and time without a timezone. Safe for "local
  to wherever you interpret this later" — e.g. form input, DB timestamps
  in a single-region system.
- **`DateTime`**: a point in time on the Earth's timeline. Has a timezone
  and a UTC offset. This is the only type you can safely compare across
  regions.

Elixir does not ship a full timezone database by default — it only knows
`Etc/UTC`. For real-world zones (`America/New_York`, `Europe/Madrid`),
you add the `tzdata` dependency and configure it as the time zone database.

Get these distinctions right and daylight-savings bugs disappear. Get them
wrong and every October/March you will have incident calls.

---

## The business problem

You are building an internal scheduling tool. Team members are spread
across continents. You need to:

1. Store each meeting as a DateTime in the organiser's timezone
2. Convert to every attendee's timezone for display
3. Detect overlaps between meetings per attendee, accounting for DST
4. Find free slots of at least N minutes in a working-hours window
5. Produce human-friendly output ("3:00 PM – 4:30 PM CET")

All comparisons happen in UTC internally. Conversions to local time
only happen at the presentation boundary.

---

## Project structure

```
meet_sched/
├── lib/
│   └── meet_sched/
│       ├── meeting.ex
│       ├── overlap.ex
│       └── formatter.ex
├── test/
│   └── meet_sched/
│       ├── meeting_test.exs
│       ├── overlap_test.exs
│       └── formatter_test.exs
├── config/
│   └── config.exs
├── .formatter.exs
└── mix.exs
```

---

## Implementation

### Step 1: Create the project

```bash
mix new meet_sched
cd meet_sched
mkdir -p config
```

### Step 2: `mix.exs`

```elixir
defmodule MeetSched.MixProject do
  use Mix.Project

  def project do
    [
      app: :meet_sched,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    # tzdata must be started so its ETS tables are populated before
    # DateTime.shift_zone/2 is called.
    [extra_applications: [:logger, :tzdata]]
  end

  defp deps do
    [
      {:tzdata, "~> 1.1"}
    ]
  end
end
```

### Step 3: `config/config.exs`

```elixir
import Config

# Register tzdata as the global time-zone database. Without this, any
# DateTime.shift_zone/2 call outside Etc/UTC raises.
config :elixir, :time_zone_database, Tzdata.TimeZoneDatabase
```

### Step 4: `.formatter.exs`

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### Step 5: `lib/meet_sched/meeting.ex`

```elixir
defmodule MeetSched.Meeting do
  @moduledoc """
  A meeting has an organiser, a start DateTime, and a duration.

  Internally everything is stored as a UTC DateTime. The organiser
  timezone is kept only so we can round-trip display to the user.
  Comparisons and overlap math happen in UTC where one minute is
  always one minute (DST does not apply).
  """

  @enforce_keys [:id, :title, :starts_at_utc, :duration_minutes, :organiser_tz]
  defstruct [:id, :title, :starts_at_utc, :duration_minutes, :organiser_tz, attendees: []]

  @type t :: %__MODULE__{
          id: String.t(),
          title: String.t(),
          starts_at_utc: DateTime.t(),
          duration_minutes: pos_integer(),
          organiser_tz: Calendar.time_zone(),
          attendees: [String.t()]
        }

  @doc """
  Build a meeting from a NaiveDateTime in a given timezone.

  We accept naive input because users type "2026-05-04 14:00" without
  a zone; the zone comes from a separate field in the UI. We convert
  to UTC immediately so the struct is self-describing.
  """
  @spec new(
          id :: String.t(),
          title :: String.t(),
          starts_at_naive :: NaiveDateTime.t(),
          tz :: Calendar.time_zone(),
          duration_minutes :: pos_integer(),
          attendees :: [String.t()]
        ) :: {:ok, t()} | {:error, term()}
  def new(id, title, starts_at_naive, tz, duration_minutes, attendees \\ []) do
    # from_naive/2 resolves the wall-clock time against the zone's
    # DST rules. It returns {:ambiguous, _, _} for times that occur
    # twice (autumn fall-back) and {:gap, _, _} for times that do not
    # exist (spring forward). Both are real calendar edge cases and
    # must be surfaced to the caller, not swallowed.
    case DateTime.from_naive(starts_at_naive, tz) do
      {:ok, local_dt} ->
        utc = DateTime.shift_zone!(local_dt, "Etc/UTC")

        {:ok,
         %__MODULE__{
           id: id,
           title: title,
           starts_at_utc: utc,
           duration_minutes: duration_minutes,
           organiser_tz: tz,
           attendees: attendees
         }}

      {:ambiguous, _first, _second} = err ->
        {:error, err}

      {:gap, _before, _after} = err ->
        {:error, err}

      {:error, _} = err ->
        err
    end
  end

  @doc """
  End of the meeting in UTC. Computed, not stored, so updates to
  duration always stay consistent.
  """
  @spec ends_at_utc(t()) :: DateTime.t()
  def ends_at_utc(%__MODULE__{starts_at_utc: start, duration_minutes: min}) do
    DateTime.add(start, min * 60, :second)
  end

  @doc """
  Returns the start DateTime rendered in the requested timezone.
  """
  @spec starts_at_in(t(), Calendar.time_zone()) :: DateTime.t()
  def starts_at_in(%__MODULE__{starts_at_utc: utc}, tz) do
    DateTime.shift_zone!(utc, tz)
  end
end
```

**Why this works:**

- Storing UTC makes every comparison an integer subtraction. If we stored
  local times, the 25-hour day that happens every autumn would let two
  meetings "not overlap" even though their UTC ranges do.
- `DateTime.from_naive/2` returns four possible tags. Senior code handles
  all of them explicitly rather than calling `from_naive!/2` and crashing
  on the two edge cases per year.
- `ends_at_utc/1` is computed. Storing both start and end would let them
  drift apart on updates.

### Step 6: `lib/meet_sched/overlap.ex`

```elixir
defmodule MeetSched.Overlap do
  @moduledoc """
  Overlap detection over lists of meetings.

  The algorithm is O(n log n): sort by start, then sweep. This is
  enough for team calendars (thousands of meetings). For interval
  trees over millions of events see the "when NOT to use" note.
  """

  alias MeetSched.Meeting

  @doc """
  True if two meetings overlap in wall-clock time.

  Uses the half-open interval convention: a meeting from 10:00 to
  11:00 does NOT overlap with one starting at 11:00. This matches
  how humans schedule back-to-back calls.
  """
  @spec overlaps?(Meeting.t(), Meeting.t()) :: boolean()
  def overlaps?(%Meeting{} = a, %Meeting{} = b) do
    a_start = a.starts_at_utc
    a_end = Meeting.ends_at_utc(a)
    b_start = b.starts_at_utc
    b_end = Meeting.ends_at_utc(b)

    # Two intervals overlap iff a_start < b_end AND b_start < a_end.
    # DateTime.compare/2 returns :lt | :eq | :gt; we want strict lt.
    DateTime.compare(a_start, b_end) == :lt and DateTime.compare(b_start, a_end) == :lt
  end

  @doc """
  Returns all pairs of meetings that overlap for a given attendee.
  Each pair is returned once (lexicographically by id).
  """
  @spec find_conflicts([Meeting.t()], String.t()) :: [{Meeting.t(), Meeting.t()}]
  def find_conflicts(meetings, attendee) do
    relevant =
      meetings
      |> Enum.filter(&(attendee in &1.attendees))
      |> Enum.sort_by(& &1.starts_at_utc, DateTime)

    # Single-pass sweep: we only need to compare each meeting to the
    # ones currently "open" (those whose end is after the current
    # start). With the list sorted, we can keep the open set tiny.
    sweep(relevant, [], [])
  end

  @doc """
  Free slots of at least `min_minutes` for an attendee inside the
  working window [from_utc, to_utc). Returns a list of {start, end}
  DateTime pairs in UTC.
  """
  @spec free_slots(
          [Meeting.t()],
          String.t(),
          DateTime.t(),
          DateTime.t(),
          pos_integer()
        ) :: [{DateTime.t(), DateTime.t()}]
  def free_slots(meetings, attendee, from_utc, to_utc, min_minutes) do
    busy =
      meetings
      |> Enum.filter(&(attendee in &1.attendees))
      |> Enum.map(fn m -> {m.starts_at_utc, Meeting.ends_at_utc(m)} end)
      |> clamp(from_utc, to_utc)
      |> Enum.sort_by(fn {s, _} -> s end, DateTime)
      |> merge_overlapping()

    compute_gaps(busy, from_utc, to_utc, min_minutes)
  end

  # --- private ---

  defp sweep([], _open, conflicts), do: Enum.reverse(conflicts)

  defp sweep([meeting | rest], open, conflicts) do
    # Drop any open meetings that finish before this one starts.
    still_open =
      Enum.filter(open, fn m ->
        DateTime.compare(Meeting.ends_at_utc(m), meeting.starts_at_utc) == :gt
      end)

    # Everything still open overlaps with the current meeting by
    # construction: they haven't ended yet and they started earlier.
    new_conflicts = Enum.map(still_open, fn m -> {m, meeting} end)

    sweep(rest, [meeting | still_open], new_conflicts ++ conflicts)
  end

  defp clamp(intervals, from, to) do
    Enum.flat_map(intervals, fn {s, e} ->
      cond do
        DateTime.compare(e, from) != :gt -> []
        DateTime.compare(s, to) != :lt -> []
        true -> [{max_dt(s, from), min_dt(e, to)}]
      end
    end)
  end

  defp merge_overlapping([]), do: []

  defp merge_overlapping([first | rest]) do
    Enum.reduce(rest, [first], fn {s, e}, [{ps, pe} | tail] = acc ->
      if DateTime.compare(s, pe) == :gt do
        [{s, e} | acc]
      else
        [{ps, max_dt(pe, e)} | tail]
      end
    end)
    |> Enum.reverse()
  end

  defp compute_gaps(busy, from, to, min_minutes) do
    min_seconds = min_minutes * 60
    cursor = from

    {gaps, final_cursor} =
      Enum.reduce(busy, {[], cursor}, fn {s, e}, {acc, cur} ->
        if DateTime.compare(cur, s) == :lt do
          {[{cur, s} | acc], e}
        else
          {acc, max_dt(cur, e)}
        end
      end)

    final_gaps =
      if DateTime.compare(final_cursor, to) == :lt do
        [{final_cursor, to} | gaps]
      else
        gaps
      end

    final_gaps
    |> Enum.reverse()
    |> Enum.filter(fn {s, e} -> DateTime.diff(e, s, :second) >= min_seconds end)
  end

  defp max_dt(a, b), do: if(DateTime.compare(a, b) == :gt, do: a, else: b)
  defp min_dt(a, b), do: if(DateTime.compare(a, b) == :lt, do: a, else: b)
end
```

### Step 7: `lib/meet_sched/formatter.ex`

```elixir
defmodule MeetSched.Formatter do
  @moduledoc """
  Human-friendly formatting for meeting display.

  Kept separate from Meeting so alternate renderers (HTML, iCal)
  can be added without touching the core type.
  """

  alias MeetSched.Meeting

  @doc """
  Renders a meeting for a specific viewer timezone.
  Example: "Standup — 2026-05-04 09:00-09:30 Europe/Madrid"
  """
  @spec render(Meeting.t(), Calendar.time_zone()) :: String.t()
  def render(%Meeting{} = meeting, viewer_tz) do
    start_local = Meeting.starts_at_in(meeting, viewer_tz)
    end_local = meeting |> Meeting.ends_at_utc() |> DateTime.shift_zone!(viewer_tz)

    "#{meeting.title} — #{format_date(start_local)} " <>
      "#{format_time(start_local)}-#{format_time(end_local)} #{viewer_tz}"
  end

  defp format_date(dt), do: Calendar.strftime(dt, "%Y-%m-%d")
  defp format_time(dt), do: Calendar.strftime(dt, "%H:%M")
end
```

### Step 8: Tests

```elixir
# test/meet_sched/meeting_test.exs
defmodule MeetSched.MeetingTest do
  use ExUnit.Case, async: true
  alias MeetSched.Meeting

  describe "new/6" do
    test "converts a local naive datetime to UTC" do
      naive = ~N[2026-05-04 09:00:00]
      {:ok, m} = Meeting.new("m1", "Standup", naive, "Europe/Madrid", 30, ["alice"])

      # Madrid in May is CEST (UTC+2), so 09:00 Madrid = 07:00 UTC.
      assert m.starts_at_utc.time_zone == "Etc/UTC"
      assert m.starts_at_utc.hour == 7
    end

    test "computes end time correctly" do
      naive = ~N[2026-05-04 09:00:00]
      {:ok, m} = Meeting.new("m1", "Standup", naive, "Etc/UTC", 45)

      ends = Meeting.ends_at_utc(m)
      assert ends.hour == 9
      assert ends.minute == 45
    end

    test "surfaces ambiguous local times (fall-back DST)" do
      # 2026-10-25 02:30 Europe/Madrid occurs twice (clocks roll back
      # from 03:00 CEST to 02:00 CET).
      naive = ~N[2026-10-25 02:30:00]
      assert {:error, {:ambiguous, _, _}} = Meeting.new("m", "t", naive, "Europe/Madrid", 30)
    end

    test "surfaces gap local times (spring-forward DST)" do
      # 2026-03-29 02:30 Europe/Madrid does not exist (02:00 jumps to
      # 03:00).
      naive = ~N[2026-03-29 02:30:00]
      assert {:error, {:gap, _, _}} = Meeting.new("m", "t", naive, "Europe/Madrid", 30)
    end
  end

  describe "starts_at_in/2" do
    test "renders start time in the requested timezone" do
      {:ok, m} = Meeting.new("m", "t", ~N[2026-05-04 09:00:00], "Etc/UTC", 30)
      ny = Meeting.starts_at_in(m, "America/New_York")
      # NY in May is EDT (UTC-4).
      assert ny.hour == 5
      assert ny.time_zone == "America/New_York"
    end
  end
end
```

```elixir
# test/meet_sched/overlap_test.exs
defmodule MeetSched.OverlapTest do
  use ExUnit.Case, async: true
  alias MeetSched.{Meeting, Overlap}

  defp meeting(id, start_naive, tz, duration, attendees) do
    {:ok, m} = Meeting.new(id, id, start_naive, tz, duration, attendees)
    m
  end

  test "overlaps?/2 true when ranges intersect" do
    a = meeting("a", ~N[2026-05-04 09:00:00], "Etc/UTC", 60, [])
    b = meeting("b", ~N[2026-05-04 09:30:00], "Etc/UTC", 60, [])
    assert Overlap.overlaps?(a, b)
  end

  test "overlaps?/2 false for back-to-back meetings" do
    a = meeting("a", ~N[2026-05-04 09:00:00], "Etc/UTC", 60, [])
    b = meeting("b", ~N[2026-05-04 10:00:00], "Etc/UTC", 30, [])
    refute Overlap.overlaps?(a, b)
  end

  test "overlap works across timezones" do
    # 14:00 Madrid = 12:00 UTC. 13:30 UTC overlaps that meeting.
    a = meeting("a", ~N[2026-05-04 14:00:00], "Europe/Madrid", 60, ["x"])
    b = meeting("b", ~N[2026-05-04 13:30:00], "Etc/UTC", 60, ["x"])
    assert Overlap.overlaps?(a, b)
  end

  test "find_conflicts returns each overlapping pair once" do
    a = meeting("a", ~N[2026-05-04 09:00:00], "Etc/UTC", 60, ["u"])
    b = meeting("b", ~N[2026-05-04 09:30:00], "Etc/UTC", 60, ["u"])
    c = meeting("c", ~N[2026-05-04 11:00:00], "Etc/UTC", 30, ["u"])

    conflicts = Overlap.find_conflicts([a, b, c], "u")
    assert length(conflicts) == 1
    assert {^a, ^b} = hd(conflicts)
  end

  test "find_conflicts ignores meetings where attendee is absent" do
    a = meeting("a", ~N[2026-05-04 09:00:00], "Etc/UTC", 60, ["alice"])
    b = meeting("b", ~N[2026-05-04 09:30:00], "Etc/UTC", 60, ["bob"])
    assert Overlap.find_conflicts([a, b], "alice") == []
  end

  test "free_slots returns gaps inside the working window" do
    a = meeting("a", ~N[2026-05-04 10:00:00], "Etc/UTC", 60, ["alice"])
    b = meeting("b", ~N[2026-05-04 13:00:00], "Etc/UTC", 30, ["alice"])

    from = ~U[2026-05-04 09:00:00Z]
    to = ~U[2026-05-04 17:00:00Z]

    slots = Overlap.free_slots([a, b], "alice", from, to, 30)

    # Expected free: 09:00-10:00, 11:00-13:00, 13:30-17:00.
    assert length(slots) == 3
    {s1, e1} = Enum.at(slots, 0)
    assert s1 == from
    assert e1.hour == 10
  end

  test "free_slots filters out too-short gaps" do
    a = meeting("a", ~N[2026-05-04 09:30:00], "Etc/UTC", 60, ["alice"])
    from = ~U[2026-05-04 09:00:00Z]
    to = ~U[2026-05-04 11:00:00Z]

    # First gap is only 30 min; reject when min is 45.
    slots = Overlap.free_slots([a], "alice", from, to, 45)
    assert length(slots) == 0
  end
end
```

```elixir
# test/meet_sched/formatter_test.exs
defmodule MeetSched.FormatterTest do
  use ExUnit.Case, async: true
  alias MeetSched.{Meeting, Formatter}

  test "render shows local time for the viewer" do
    {:ok, m} = Meeting.new("m", "Standup", ~N[2026-05-04 09:00:00], "Etc/UTC", 30)
    output = Formatter.render(m, "Europe/Madrid")
    assert output =~ "Standup"
    assert output =~ "11:00-11:30"
    assert output =~ "Europe/Madrid"
  end
end
```

### Step 9: Run and verify

```bash
mix deps.get
mix compile --warnings-as-errors
mix test --trace
mix format
```

---

## Trade-off analysis

| Type | Store | Compare | Display |
|------|-------|---------|---------|
| `Date` | Yes for calendar-only data | Yes | Yes |
| `Time` | Rarely alone | Yes | Yes |
| `NaiveDateTime` | Yes in single-region systems | Avoid across zones | Never for multi-region |
| `DateTime` | Best for cross-zone data | Yes, always | After zone conversion |

| Option | When |
|--------|------|
| `tzdata` (current) | Real timezone support, auto-updates |
| `Tz` (alternative lib) | Same API, different release cadence |
| No database | UTC-only systems (rare in reality) |

---

## Common production mistakes

**1. Using `NaiveDateTime.compare/2` on data from different zones**
Two naive datetimes can look equal but represent instants hours apart.
Always convert to `DateTime` first when attendees cross timezones.

**2. Calling `DateTime.from_naive!/2`**
The bang version crashes on `:ambiguous` and `:gap` — both of which
happen in production twice a year during DST transitions. Use the
non-bang version and handle the edge cases explicitly.

**3. Forgetting to configure `:time_zone_database`**
Without `Tzdata.TimeZoneDatabase` registered, `DateTime.shift_zone/2`
silently fails for anything except `Etc/UTC`. Configure it once in
`config.exs` and add `:tzdata` to `extra_applications`.

**4. Storing offsets instead of zone names**
A meeting tagged "UTC+1" breaks when DST kicks in — it becomes UTC+2.
Store the IANA zone name (`Europe/Madrid`) so future arithmetic stays
correct across DST boundaries.

**5. Using `Date.add/2` for "one month later"**
`Date.add/2` only adds days. For calendar arithmetic (`+1 month`,
`+1 year`), use `Date.shift/2` (Elixir 1.17+) which handles month-end
edge cases like Jan 31 + 1 month.

**6. Comparing `DateTime` with `==`**
Two equal instants in different zones are NOT `==` in Elixir because
the structs differ. Use `DateTime.compare/2` and check for `:eq`, or
normalise to UTC first.

---

## When NOT to use DateTime

- **Pure calendar math** (invoicing cycles, birthdays): use `Date`.
  Attaching times and zones adds DST traps you do not need.
- **Recurring daily times** ("remind me at 08:00 local"): store `Time`
  plus a zone, resolve to a concrete `DateTime` per occurrence.
- **Durations**: use integer seconds (or `Duration` in 1.17+). A
  `DateTime` difference should produce a duration, not another datetime.

---

## Resources

- [Calendar, Date, Time, NaiveDateTime, DateTime — HexDocs](https://hexdocs.pm/elixir/Calendar.html)
- [DateTime — HexDocs](https://hexdocs.pm/elixir/DateTime.html)
- [tzdata — time-zone database](https://hexdocs.pm/tzdata/Tzdata.html)
- [Calendar.strftime/2 — formatting](https://hexdocs.pm/elixir/Calendar.html#strftime/3)
- [IANA time-zone database](https://www.iana.org/time-zones)
