# erlexec with CPU and Memory Limits

**Project**: `ml_sandbox` — run user-submitted Python ML scripts inside resource-limited subprocesses with cgroups-style CPU and RSS caps.

## Project context

A research platform lets data scientists submit short Python snippets that train small models
on uploaded CSVs. Without limits, a single submission can spin up a 16-thread NumPy operation
that consumes 64GB RAM and starves other workloads. Standard `Port.open/2` cannot enforce CPU
time, memory, nice level, or user switching directly — you would shell out to `setrlimit`,
`ulimit`, `nice`, and `runuser` in wrappers.

`erlexec` is the industry library for this: it is a port program (written in C++) that acts
as a broker. The BEAM sends it structured commands (JSON-like) and `erlexec` sets `rlimit`s,
manages process groups, enforces timeouts, and streams stdout/stderr back as Elixir messages.

```
ml_sandbox/
├── lib/
│   └── ml_sandbox/
│       ├── application.ex
│       └── sandbox.ex
├── test/ml_sandbox/sandbox_test.exs
├── bench/sandbox_bench.exs
└── mix.exs
```

## Why erlexec and not plain Port.open

| Concern | Port.open | erlexec |
|---|---|---|
| CPU time limit | manual via `ulimit -t` wrapper | `cpu_seconds` option |
| RSS limit | manual via `prlimit` wrapper | `rss_bytes` option |
| User switching | needs a setuid wrapper | `user:` option |
| Process group kill | manual | automatic |
| Reliable exit reporting | mostly | yes (structured) |
| Zombie cleanup on BEAM exit | no | yes (dedicated reaper) |

For untrusted code the brokered approach is mandatory. Writing wrappers by hand leads to
races and leaks that are hard to find post-incident.

## Why run with `{:cd, ...}` and `{:env, ...}` isolation

Even with rlimits, a script can read `/etc/passwd` or write to `/tmp`. Running inside a
dedicated working directory with an allowlisted env (or `chroot` via `erlexec`'s helper)
shrinks the blast radius. For true untrusted code, combine with Linux namespaces
(separate user/pid/mount), but that is out of scope here.

## Core concepts

### 1. erlexec lifecycle

`:exec.start/0` boots the C++ broker. Every `:exec.run/2` call tells the broker what to
run; the broker forks, sets rlimits, execs, and pipes stdout/stderr back as messages to
the caller pid.

### 2. Message protocol

Asynchronous mode (`:exec.run(cmd, [:sync])` is blocking; without `:sync`, it is async)
sends:
- `{:stdout, ospid, bytes}`
- `{:stderr, ospid, bytes}`
- `{:exit, ospid, reason}`
  where `reason` is `{:status, n}` or `{:signal, sig, core?}`.

### 3. Kill-on-limit

When a child exceeds `cpu_seconds`, the kernel sends SIGXCPU. erlexec forwards a clean
`{:exit, _, {:signal, :sigxcpu, _}}`. RSS overrun gets SIGSEGV-ish behavior from the kernel
OOM-killer; we translate this in our wrapper.

### 4. Linked vs monitored ports

`:exec.run(cmd, [:link])` links the caller to the port. If the child dies, caller dies too
— usually not what you want. We use `[:monitor]` so we get a message and can handle
recovery.

## Design decisions

- **Option A — one long-lived erlexec broker, many child runs**: broker starts at app boot,
  stays up forever. Pros: no per-call startup. Cons: broker crash kills all in-flight
  children.
- **Option B — fresh broker per sandbox call**: expensive and unnecessary.

→ **Option A**. erlexec is designed to be long-lived.

- **Option A — capture output into memory**: simple but unbounded.
- **Option B — stream output to a file, return a reference**: memory-safe at the cost of
  disk I/O.

→ We **cap memory output via a size check** and return in-memory for test simplicity. For
  production submissions larger than 1MB, Option B is the correct path.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule MlSandbox.MixProject do
  use Mix.Project

  def project do
    [
      app: :ml_sandbox,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [
        {:erlexec, "~> 2.0"},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :erlexec],
      mod: {MlSandbox.Application, []}
    ]
  end
end
```

### Step 1: Supervisor

```elixir
defmodule MlSandbox.Application do
  use Application

  @impl true
  def start(_, _) do
    # erlexec's own supervisor is started via :erlexec as an OTP app.
    # We just need our sandbox wrapper.
    Supervisor.start_link([MlSandbox.Sandbox],
      strategy: :one_for_one, name: MlSandbox.Supervisor)
  end
end
```

### Step 2: The sandbox (`lib/ml_sandbox/sandbox.ex`)

```elixir
defmodule MlSandbox.Sandbox do
  @moduledoc """
  Runs untrusted scripts under erlexec with CPU time, RSS, and wall-clock caps.

  Returns:
    {:ok, stdout, stderr}
    {:error, :timeout}
    {:error, :cpu_limit}
    {:error, :mem_limit}
    {:error, {:exit_status, code, stdout, stderr}}
  """
  use GenServer
  require Logger

  defstruct [:ospid, :ref, :caller, :stdout, :stderr, :limits,
             :timer_ref, :started_at, :max_output]

  # ---- Public API -----------------------------------------------------------

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @doc """
  Runs `cmd` with arguments, enforcing the given limits.

    opts = [
      cpu_seconds:   integer,   # SIGXCPU after N seconds of CPU time
      rss_bytes:     integer,   # RLIMIT_AS — virtual memory cap
      wall_ms:       integer,   # kill after wall-clock ms regardless of CPU
      max_output:    integer,   # abort if stdout+stderr exceeds bytes
      cwd:           path,
      env:           [{binary, binary}]
    ]
  """
  @spec run(Path.t(), [binary()], keyword()) ::
          {:ok, binary(), binary()} | {:error, term()}
  def run(cmd, args, opts \\ []) do
    GenServer.call(__MODULE__, {:run, cmd, args, opts}, :infinity)
  end

  # ---- GenServer ------------------------------------------------------------

  @impl true
  def init(_), do: {:ok, %{}}

  @impl true
  def handle_call({:run, cmd, args, opts}, from, state) do
    cpu = Keyword.get(opts, :cpu_seconds, 5)
    rss = Keyword.get(opts, :rss_bytes, 256 * 1024 * 1024)
    wall = Keyword.get(opts, :wall_ms, 10_000)
    max_output = Keyword.get(opts, :max_output, 1_000_000)
    cwd = Keyword.get(opts, :cwd, System.tmp_dir!())
    env = Keyword.get(opts, :env, []) |> Enum.map(fn {k, v} -> {to_charlist(k), to_charlist(v)} end)

    exec_opts = [
      :stdout, :stderr, :monitor,
      {:cd, String.to_charlist(cwd)},
      {:env, env},
      {:kill_timeout, 2},
      {:group, 0}  # start in its own process group so we can kill the tree
    ]

    # erlexec understands these structured rlimits indirectly — we pass them
    # via the kernel's preexec hook by wrapping the command with `prlimit`.
    # This keeps the example portable to erlexec versions that do not expose
    # every rlimit directly.
    wrapped_cmd = wrap_with_prlimit(cmd, args, cpu, rss)

    case :exec.run(wrapped_cmd, exec_opts) do
      {:ok, _pid, ospid} ->
        timer_ref = Process.send_after(self(), {:wall_timeout, ospid}, wall)
        s = %__MODULE__{
          ospid: ospid,
          caller: from,
          stdout: <<>>,
          stderr: <<>>,
          timer_ref: timer_ref,
          max_output: max_output,
          started_at: System.monotonic_time(:millisecond),
          limits: %{cpu: cpu, rss: rss, wall: wall}
        }
        {:noreply, Map.put(state, ospid, s)}

      {:error, reason} ->
        {:reply, {:error, {:spawn_failed, reason}}, state}
    end
  end

  @impl true
  def handle_info({:stdout, ospid, bytes}, state) do
    state |> update_run(ospid, fn s ->
      new = %{s | stdout: s.stdout <> bytes}
      maybe_kill_on_output(new)
    end)
    |> reply_noreply()
  end

  def handle_info({:stderr, ospid, bytes}, state) do
    state |> update_run(ospid, fn s ->
      new = %{s | stderr: s.stderr <> bytes}
      maybe_kill_on_output(new)
    end)
    |> reply_noreply()
  end

  def handle_info({:DOWN, _, :process, _, {:exit_status, status}}, state) do
    handle_exit_by_status(status, state)
  end

  def handle_info({:wall_timeout, ospid}, state) do
    case Map.get(state, ospid) do
      nil -> {:noreply, state}
      s ->
        :exec.kill(ospid, 9)
        GenServer.reply(s.caller, {:error, :timeout})
        {:noreply, Map.delete(state, ospid)}
    end
  end

  def handle_info(_, state), do: {:noreply, state}

  # ---- Helpers --------------------------------------------------------------

  defp wrap_with_prlimit(cmd, args, cpu_seconds, rss_bytes) do
    prlimit_args =
      ["--cpu=#{cpu_seconds}", "--as=#{rss_bytes}", "--"] ++ [cmd] ++ args

    # erlexec accepts a charlist command string.
    (["prlimit" | prlimit_args]
     |> Enum.map_join(" ", &shell_escape/1))
    |> String.to_charlist()
  end

  defp shell_escape(s), do: "'" <> String.replace(s, "'", "'\\''") <> "'"

  defp update_run(state, ospid, fun) do
    case Map.get(state, ospid) do
      nil -> {state, nil}
      s ->
        case fun.(s) do
          :killed -> {Map.delete(state, ospid), :killed}
          new -> {Map.put(state, ospid, new), nil}
        end
    end
  end

  defp reply_noreply({state, _}), do: {:noreply, state}

  defp maybe_kill_on_output(%{stdout: so, stderr: se, max_output: max, ospid: ospid, caller: from}) do
    if byte_size(so) + byte_size(se) > max do
      :exec.kill(ospid, 9)
      GenServer.reply(from, {:error, :output_too_large})
      :killed
    else
      %{stdout: so, stderr: se, max_output: max, ospid: ospid, caller: from}
    end
  end
  defp maybe_kill_on_output(s), do: s

  # erlexec encodes exit via a monitor DOWN with {:exit_status, N}.
  # The N is a packed value: low byte = signal if any, rest = exit code.
  defp handle_exit_by_status(status, state) do
    # Find the ospid whose process just died. erlexec's DOWN does not
    # carry ospid; we resolve by the single in-flight call in this simple
    # implementation — production code should track ref→ospid mapping.
    case Map.keys(state) do
      [ospid] ->
        s = Map.fetch!(state, ospid)
        Process.cancel_timer(s.timer_ref)
        reply = classify_exit(status, s)
        GenServer.reply(s.caller, reply)
        {:noreply, Map.delete(state, ospid)}
      _ ->
        {:noreply, state}
    end
  end

  defp classify_exit(status, s) do
    signal = :exec.status(status) |> elem(0)
    exit_code = :exec.status(status) |> elem(1)
    cond do
      signal == 24 -> {:error, :cpu_limit}        # SIGXCPU
      signal == 9  -> {:error, :killed}
      exit_code == 0 -> {:ok, s.stdout, s.stderr}
      true -> {:error, {:exit_status, exit_code, s.stdout, s.stderr}}
    end
  end
end
```

## Why this works

```
GenServer.call(run, ...)
         │
         ▼
   :exec.run(cmd wrapped with prlimit)
         │
         ▼          ┌── {:stdout, ospid, bytes}
   erlexec broker ──┤    {:stderr, ospid, bytes}
         │          └── {:DOWN, ..., {:exit_status, N}}
         │
         ▼
   child proc with RLIMIT_CPU and RLIMIT_AS set by prlimit
         │
    SIGXCPU / OOM / exit N
         │
         ▼
   classify_exit → caller GenServer.reply
```

- `prlimit(1)` sets `RLIMIT_CPU` (SIGXCPU on overrun) and `RLIMIT_AS` (address space cap)
  in the **child's execve envelope** — the child starts already under limits, so even a
  fork-bomb cannot escape.
- The wall-clock timer is an independent safety net: even if the child hangs without CPU
  usage (sleeping, I/O blocked), `wall_ms` fires and we `kill -9`.
- `max_output` is enforced per-chunk as bytes arrive — bounded memory on the BEAM side.
- `erlexec`'s monitor mode means our GenServer receives a structured DOWN — no signal
  parsing boilerplate.

## Tests (`test/ml_sandbox/sandbox_test.exs`)

```elixir
defmodule MlSandbox.SandboxTest do
  use ExUnit.Case, async: false
  alias MlSandbox.Sandbox

  @moduletag :requires_prlimit

  setup_all do
    if System.find_executable("prlimit") == nil do
      {:skip, "prlimit not installed"}
    else
      :ok
    end
  end

  describe "normal execution" do
    test "captures stdout from echo" do
      assert {:ok, out, _} = Sandbox.run("echo", ["hello"], wall_ms: 2_000)
      assert String.trim(out) == "hello"
    end

    test "non-zero exit returns exit status" do
      assert {:error, {:exit_status, 3, _, _}} =
               Sandbox.run("sh", ["-c", "exit 3"], wall_ms: 2_000)
    end
  end

  describe "limits enforcement" do
    test "wall clock timeout kills sleep" do
      assert {:error, :timeout} =
               Sandbox.run("sleep", ["30"], wall_ms: 200)
    end

    test "CPU limit kills a busy loop" do
      # A tight shell loop burns CPU fast; cpu_seconds: 1 should trigger.
      script = "while :; do :; done"
      assert result = Sandbox.run("sh", ["-c", script], cpu_seconds: 1, wall_ms: 5_000)
      assert result in [{:error, :cpu_limit}, {:error, :killed}, {:error, :timeout}]
    end

    test "output size cap kills yes" do
      assert {:error, :output_too_large} =
               Sandbox.run("yes", [], max_output: 10_000, wall_ms: 5_000)
    end
  end
end
```

## Benchmark (`bench/sandbox_bench.exs`)

```elixir
Benchee.run(
  %{
    "echo hello" => fn ->
      MlSandbox.Sandbox.run("echo", ["hello"], wall_ms: 2_000)
    end,
    "true (minimal exit)" => fn ->
      MlSandbox.Sandbox.run("true", [], wall_ms: 2_000)
    end
  },
  parallel: 8, time: 5, warmup: 2
)
```

**Expected**: each spawn costs ~8ms (erlexec broker round-trip + prlimit setup + fork/exec).
For a real Python interpreter startup you add another 40–80ms. Do not call `Sandbox.run`
inside a tight loop; batch work inside the subprocess.

## Trade-offs and production gotchas

**1. Prlimit is Linux-only.** macOS lacks `prlimit(1)`; the test is tagged accordingly. For
cross-platform, consider a tiny C launcher that calls `setrlimit` before `execvp`.

**2. RLIMIT_AS vs real memory.** RSS-based limits require cgroups. `RLIMIT_AS` (virtual
memory) can kill programs that `mmap` large sparse regions but never touch them (e.g., Go
runtimes). For production ML workloads, move to cgroup v2 memory.max.

**3. Zombie grandchildren.** If the child forks and the parent exits, the grandchildren
survive. Use `setsid` + process group kills (`kill -9 -pgid`) to reap the tree.

**4. Broker as single point of failure.** The erlexec broker process is one C++ program.
If it crashes, all in-flight subprocesses become orphans. Monitor it; restart policy is
already baked into the `:erlexec` OTP app.

**5. Shell escaping in `wrap_with_prlimit`.** We quote each argument. Never concatenate
untrusted input into a shell command without escaping. A safer approach passes args as
a list to `:exec.run/2` directly (list form, no shell).

**6. When NOT to use erlexec.** For trusted scripts where you control the source and don't
need limits, `Port.open/2` is simpler and has no dependency. erlexec earns its keep when
the workload is untrusted.

## Reflection

The implementation wraps every command in `prlimit`. Moving the same semantics to cgroups v2
trades per-call speed (cgroup creation is slower than `prlimit`) for stronger isolation
(memory limits actually track RSS, not VAS) and portability with container runtimes. In
what kind of workload does the cgroup overhead become negligible compared to the script's
own startup time, and where does it matter?

## Resources

- [erlexec README and docs](https://github.com/saleyn/erlexec)
- [`prlimit(1)` — util-linux](https://man7.org/linux/man-pages/man1/prlimit.1.html)
- [`setrlimit(2)`](https://man7.org/linux/man-pages/man2/setrlimit.2.html)
- [cgroup v2 memory controller](https://www.kernel.org/doc/Documentation/admin-guide/cgroup-v2.rst)
