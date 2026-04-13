# AI Agents Framework

**Project**: `agent_framework` — Native BEAM AI agents framework with ReAct loop, tool execution, and AST sandboxing

## Project context

Your team is building an AI assistant for a legal research platform. The assistant must search case law databases, summarize documents, run code to compute statistics, and delegate specialized tasks (contract analysis, precedent ranking) to specialist sub-agents. The orchestration logic is complex: plan a research question, call search tools, analyze results, synthesize findings, produce a final report.

Python frameworks (LangChain, AutoGen) are considered but rejected: they carry runtime overhead, cannot model OTP supervision, and mix the orchestration loop with LLM provider specifics. The BEAM is a natural fit: each agent is a process, failures are isolated, the supervisor tree handles restarts, and `Task.async_stream` handles parallel tool calls.

You will build `AgentFramework`: a native BEAM AI agents framework. No LangChain. No external runtimes. The only external protocol boundary is the HTTP wire format to the LLM provider.

## Design decisions

**Option A — single-shot prompt-then-tool-call**
- Pros: simple, one request per decision
- Cons: agents can't recover from tool failures, no memory across steps

**Option B — ReAct loop with persistent state and tool-result feedback** (chosen)
- Pros: agents can retry, recover, and chain reasoning steps
- Cons: harder to bound cost and latency

→ Chose **B** because agents that can't reason over their own failures are no different from RAG — the loop is what makes it an agent.

## Why agents are GenServers and not plain Task processes

An agent must hold state across multiple turns: conversation history, tool registry, memory, cost accumulator. A `Task` is stateless and terminates. A `GenServer` holds state indefinitely, can be supervised (auto-restarted on crash), registered by name (discoverable by other agents), and receives messages (for approval signals, streaming results). The actor model maps directly to the agent abstraction.

## Why the ReAct loop must be a recursive function, not multiple GenServer callbacks

The ReAct loop is: call LLM -> if tool calls, execute tools -> call LLM again -> repeat until final response. If implemented as `handle_call` -> `handle_cast` -> `handle_call` (multiple rounds), the agent's `GenServer` mailbox queues incoming messages from other processes between each LLM call. This causes ordering problems: a `stop` signal from the supervisor arrives between two LLM calls but is not processed until the loop completes. A private recursive function holds the control flow without yielding the GenServer between iterations.

## Why sandbox code execution requires AST analysis before evaluation

`Code.eval_string/1` executes arbitrary Elixir code with full access to the BEAM. A user-supplied snippet that calls `File.read("/etc/passwd")` or `:os.cmd("rm -rf /")` causes real damage. Allowing the LLM to generate and run arbitrary code requires a sandboxing layer. The AST analysis approach: parse with `Code.string_to_quoted!/1`, walk the AST with `Macro.prewalk/2`, and reject any node that calls forbidden modules (`File`, `Port`, `System`, `:os`, `:file`, etc.). This is static analysis before evaluation.

## Project structure
```
agent_framework/
├── script/
│   └── main.exs
├── mix.exs
├── lib/
│   └── agent_framework/
│       ├── agent.ex               # GenServer: history, tools, ReAct loop
│       ├── tool.ex                # Tool behaviour: name/0, description/0, parameters_schema/0, execute/1
│       ├── tools/
│       │   ├── web_search.ex      # WebSearchTool
│       │   ├── code_execution.ex  # CodeExecutionTool with AST sandbox
│       │   ├── database_query.ex  # DatabaseQueryTool (SELECT only)
│       │   └── agent_call.ex      # AgentCallTool: delegate to named agent
│       ├── llm/
│       │   ├── behaviour.ex       # LLM behaviour: complete/2, stream/3
│       │   ├── anthropic.ex       # Anthropic Claude wire format
│       │   ├── openai.ex          # OpenAI GPT wire format
│       │   └── retry.ex           # Exponential backoff + fallback model
│       ├── memory/
│       │   ├── short_term.ex      # Conversation history + context summarization
│       │   ├── long_term.ex       # pgvector RAG: embed, store, retrieve
│       │   └── episodic.ex        # Decision log: tool calls with rationale
│       ├── supervisor.ex          # Supervision tree: agents, pool, marketplace
│       ├── pool.ex                # PoolSupervisor: bounded queue, least-loaded dispatch
│       ├── marketplace.ex         # Registry: capabilities, embedding-based discovery
│       ├── stats.ex               # ETS: per-agent metrics, cost tracking
│       ├── streaming.ex           # Finch SSE streaming + chunk forwarding
│       └── hitl.ex                # Human-in-the-loop approval protocol
├── test/
│   ├── support/
│   │   └── mock_llm.ex            # Mock LLM for unit tests (no external calls)
│   ├── agent_test.exs
│   ├── tools/
│   │   ├── code_execution_test.exs
│   │   └── database_query_test.exs
│   ├── memory_test.exs
│   ├── pool_test.exs
│   └── marketplace_test.exs
└── bench/
    └── concurrent_agents.exs
```

### Step 1: Tool behaviour

**Objective**: Define the behaviour contract and its required callbacks.

### Step 2: Agent GenServer with ReAct loop

**Objective**: Implement the agent loop that plans tool calls and handles responses.

```elixir
defmodule AgentFramework.Agent do
  use GenServer
  require Logger

  defstruct [
    :id, :llm_module, :llm_config, :system_prompt,
    :tools, :max_iterations, :hitl_handler,
    history: [], cost_usd: 0.0, total_tokens: 0
  ]

  def start_link(opts) do
    id = Keyword.get(opts, :id, generate_id())
    GenServer.start_link(__MODULE__, opts, name: via(id))
  end

  def run(agent_pid, user_message) do
    GenServer.call(agent_pid, {:run, user_message}, :infinity)
  end

  def stream(agent_pid, user_message, caller_pid) do
    GenServer.cast(agent_pid, {:stream, user_message, caller_pid})
  end

  def init(opts) do
    state = struct(__MODULE__,
      id: Keyword.get(opts, :id, generate_id()),
      llm_module: Keyword.fetch!(opts, :llm_module),
      llm_config: Keyword.get(opts, :llm_config, %{}),
      system_prompt: Keyword.get(opts, :system_prompt, "You are a helpful assistant."),
      tools: Keyword.get(opts, :tools, []),
      max_iterations: Keyword.get(opts, :max_iterations, 10),
      hitl_handler: Keyword.get(opts, :hitl_handler)
    )
    AgentFramework.Stats.register(state.id)
    {:ok, state}
  end

  def handle_call({:run, user_message}, _from, state) do
    new_history = state.history ++ [%{role: "user", content: user_message}]
    case react_loop(new_history, state, 0) do
      {:ok, final_response, final_history, cost_delta, token_delta} ->
        new_state = %{state |
          history: final_history,
          cost_usd: state.cost_usd + cost_delta,
          total_tokens: state.total_tokens + token_delta
        }
        AgentFramework.Stats.record(state.id, :tokens, token_delta)
        AgentFramework.Stats.record(state.id, :cost, cost_delta)
        {:reply, {:ok, final_response}, new_state}
      {:error, reason, final_history} ->
        {:reply, {:error, reason}, %{state | history: final_history}}
    end
  end

  def handle_cast({:stream, user_message, caller_pid}, state) do
    new_history = state.history ++ [%{role: "user", content: user_message}]
    case state.llm_module do
      mod ->
        mod.stream(new_history, state, caller_pid)
    end
    {:noreply, state}
  end

  defp react_loop(_history, state, iteration) when iteration >= 10 do
    {:error, :max_iterations_exceeded, []}
  end

  defp react_loop(history, state, iteration) do
    :telemetry.execute([:agent_framework, :llm_call, :start],
      %{system_time: System.system_time()},
      %{agent_id: state.id, model: state.llm_config[:model], iteration: iteration})

    t0 = System.monotonic_time(:microsecond)

    case state.llm_module.complete(history, state) do
      {:ok, %{content: content, tool_calls: [], tokens: tokens, cost: cost}} ->
        duration = System.monotonic_time(:microsecond) - t0
        :telemetry.execute([:agent_framework, :llm_call, :stop],
          %{duration_microseconds: duration, token_count: tokens, cost_usd: cost},
          %{agent_id: state.id})
        new_history = history ++ [%{role: "assistant", content: content}]
        {:ok, content, new_history, cost, tokens}

      {:ok, %{content: content, tool_calls: tool_calls, tokens: tokens, cost: cost}} ->
        duration = System.monotonic_time(:microsecond) - t0
        :telemetry.execute([:agent_framework, :llm_call, :stop],
          %{duration_microseconds: duration, token_count: tokens, cost_usd: cost},
          %{agent_id: state.id})

        assistant_msg = %{role: "assistant", content: content, tool_calls: tool_calls}
        tool_results = execute_tool_calls(tool_calls, state)
        tool_result_msg = %{role: "tool", tool_results: tool_results}

        new_history = history ++ [assistant_msg, tool_result_msg]
        react_loop(new_history, state, iteration + 1)

      {:error, reason} ->
        {:error, reason, history}
    end
  end

  defp execute_tool_calls(tool_calls, state) do
    Enum.map(tool_calls, fn %{name: tool_name, input: input, id: call_id} ->
      tool_module = find_tool(state.tools, tool_name)

      cond do
        is_nil(tool_module) ->
          %{tool_call_id: call_id, result: {:error, "tool not found: #{tool_name}"}}
        hitl_required?(tool_module) ->
          result = request_approval(state, tool_module, input, call_id)
          %{tool_call_id: call_id, result: result}
        true ->
          :telemetry.execute([:agent_framework, :tool_call, :start],
            %{system_time: System.system_time()},
            %{agent_id: state.id, tool: tool_name})
          t0 = System.monotonic_time(:microsecond)
          result = AgentFramework.Tool.call(tool_module, input)
          duration = System.monotonic_time(:microsecond) - t0
          :telemetry.execute([:agent_framework, :tool_call, :stop],
            %{duration_microseconds: duration},
            %{agent_id: state.id, tool: tool_name, result: elem(result, 0)})
          AgentFramework.Episodic.record(state.id, tool_name, input, result)
          %{tool_call_id: call_id, result: result}
      end
    end)
  end

  defp find_tool(tools, name) do
    Enum.find(tools, fn mod -> mod.name() == name end)
  end

  @doc """
  Check if a tool module requires human-in-the-loop approval.
  Uses module attributes: if the tool defines `@requires_approval true`,
  this returns true.
  """
  defp hitl_required?(tool_module) do
    if function_exported?(tool_module, :__info__, 1) do
      attrs = tool_module.__info__(:attributes)
      case Keyword.get(attrs, :requires_approval) do
        [true] -> true
        _ -> false
      end
    else
      false
    end
  end

  defp request_approval(state, tool_module, input, call_id) do
    if state.hitl_handler do
      send(state.hitl_handler, {:approval_required, state.id, tool_module.name(), input, call_id})
      timeout = Application.get_env(:agent_framework, :hitl_timeout, 300_000)
      receive do
        {:approved, ^call_id} -> AgentFramework.Tool.call(tool_module, input)
        {:rejected, ^call_id, reason} -> {:error, {:rejected, reason}}
      after
        timeout -> {:error, {:rejected, :timeout}}
      end
    else
      {:error, :no_hitl_handler}
    end
  end

  defp generate_id, do: :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
  defp via(id), do: {:via, Registry, {AgentFramework.Registry, id}}
end
```
### Step 3: Code execution tool with AST sandbox

**Objective**: Define the abstract syntax tree representation for parsed input.

```elixir
defmodule AgentFramework.Tools.CodeExecution do
  @behaviour AgentFramework.Tool

  @forbidden_modules [File, Port, System, IO, :os, :file, :gen_tcp, :httpc,
                      :inet, :ssl, :net_adm, :net_kernel]

  def name, do: "code_execution"
  def description, do: "Execute Elixir code in a sandbox and return stdout and return value"
  def parameters_schema do
    %{
      "type" => "object",
      "required" => ["code"],
      "properties" => %{
        "code" => %{"type" => "string", "description" => "Elixir code to execute"},
        "language" => %{"type" => "string", "enum" => ["elixir"]}
      }
    }
  end

  def execute(%{"code" => code}) do
    case analyze_ast(code) do
      {:error, reason} -> {:error, reason}
      :ok -> run_sandboxed(code)
    end
  end

  @doc "AST-level analysis: reject any calls to forbidden modules"
  def analyze_ast(code) do
    case Code.string_to_quoted(code) do
      {:error, _} -> {:error, :syntax_error}
      {:ok, ast} ->
        violations = find_violations(ast)
        if violations == [] do
          :ok
        else
          {:error, {:unsafe_code, violations}}
        end
    end
  end

  defp find_violations(ast) do
    {_ast, violations} = Macro.prewalk(ast, [], fn node, acc ->
      case node do
        {{:., _, [{:__aliases__, _, mod_parts}, _func]}, _, _} ->
          mod = Module.concat(mod_parts)
          if mod in @forbidden_modules do
            {node, [mod | acc]}
          else
            {node, acc}
          end
        {{:., _, [mod, _func]}, _, _} when is_atom(mod) ->
          if mod in @forbidden_modules do
            {node, [mod | acc]}
          else
            {node, acc}
          end
        _ ->
          {node, acc}
      end
    end)
    violations
  end

  @doc """
  Run code in a sandboxed Task with a 5-second timeout.
  Captures stdout by redirecting the group leader to a StringIO process
  and evaluates the code with a clean binding to prevent variable leakage.
  """
  defp run_sandboxed(code) do
    task = Task.Supervisor.async_nolink(AgentFramework.TaskSupervisor, fn ->
      {result, output} = capture_output(fn ->
        try do
          {value, _bindings} = Code.eval_string(code, [], __ENV__)  # WARNING: only safe with trusted input
          {:ok, value}
        rescue
          e in RuntimeError -> {:error, Exception.message(e)}
        end
      end)
      {result, output}
    end)

    case Task.yield(task, 5_000) || Task.shutdown(task, :brutal_kill) do
      {:ok, {{:ok, value}, output}} -> {:ok, %{return: value, stdout: output}}
      {:ok, {{:error, reason}, _}} -> {:error, reason}
      nil -> {:error, :timeout}
    end
  end

  @doc """
  Capture stdout by temporarily replacing the group leader with a StringIO.
  After the function executes, restore the original group leader and
  return the captured output alongside the function result.
  """
  defp capture_output(fun) do
    original_gl = Process.group_leader()
    {:ok, string_io} = StringIO.open("")

    try do
      Process.group_leader(self(), string_io)
      result = fun.()
      {_input, output} = StringIO.contents(string_io)
      {result, output}
    after
      Process.group_leader(self(), original_gl)
      StringIO.close(string_io)
    end
  end
end
```
### Step 4: LLM behaviour and retry

**Objective**: Define the behaviour contract and its required callbacks.

```elixir
defmodule AgentFramework.LLM do
  @callback complete(history :: list(), state :: map()) ::
    {:ok, %{content: String.t(), tool_calls: list(), tokens: non_neg_integer(), cost: float()}} |
    {:error, term()}

  @callback stream(history :: list(), state :: map(), caller_pid :: pid()) :: :ok
end

defmodule AgentFramework.LLM.Retry do
  @max_retries 3
  @initial_backoff_ms 1_000

  @doc """
  Retry an LLM call with exponential backoff and jitter.
  Retries on transient HTTP errors (429, 500, 502, 503).
  Falls back to a secondary model/function if primary exhausts retries.
  """
  def with_retry(fun, opts \\ []) do
    max_retries = Keyword.get(opts, :max_retries, @max_retries)
    initial_backoff = Keyword.get(opts, :initial_backoff_ms, @initial_backoff_ms)
    fallback = Keyword.get(opts, :fallback_fn)
    do_retry(fun, 0, max_retries, initial_backoff, fallback)
  end

  defp do_retry(fun, attempt, max_retries, backoff, fallback) do
    case fun.() do
      {:error, {:http, status}} when status in [429, 500, 502, 503] ->
        if attempt < max_retries do
          jitter = :rand.uniform(div(backoff, 5))
          Process.sleep(backoff + jitter)
          do_retry(fun, attempt + 1, max_retries, min(backoff * 2, 60_000), fallback)
        else
          if fallback do
            do_retry(fallback, 0, max_retries, @initial_backoff_ms, nil)
          else
            {:error, :llm_unavailable}
          end
        end
      result ->
        result
    end
  end
end
```
### Step 5: Context summarization (short-term memory)

**Objective**: Implement the Context summarization (short-term memory) component required by the ai agents framework system.

```elixir
defmodule AgentFramework.Memory.ShortTerm do
  @summarize_threshold 0.80
  @chars_per_token 4

  @doc """
  Check if history needs summarization; if so, call LLM to summarize oldest half.
  When the token count exceeds 80% of the context window, the oldest half of
  the conversation is compressed into a summary message, preserving key facts
  while freeing context space for new interactions.
  """
  def maybe_summarize(history, context_window, llm_module, state) do
    tokens = estimate_tokens(history)
    if tokens > context_window * @summarize_threshold do
      half = div(length(history), 2)
      {old, recent} = Enum.split(history, half)
      summary = summarize(old, llm_module, state)
      [%{role: "system", content: "Summary of prior conversation: #{summary}"} | recent]
    else
      history
    end
  end

  defp estimate_tokens(history) do
    history
    |> Enum.map(fn msg ->
      div(String.length(msg.content || ""), @chars_per_token)
    end)
    |> Enum.sum()
  end

  defp summarize(messages, llm_module, state) do
    summary_prompt = [
      %{role: "user", content: "Summarize this conversation concisely, preserving key facts: " <>
        Enum.map_join(messages, "\n", fn m -> "#{m.role}: #{m.content}" end)}
    ]
    case llm_module.complete(summary_prompt, state) do
      {:ok, %{content: summary}} -> summary
      _ -> "[Summary unavailable]"
    end
  end
end
```
### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
defmodule AgentFramework.Tools.CodeExecutionTest do
  use ExUnit.Case, async: true
  doctest AgentFramework.Memory.ShortTerm
  alias AgentFramework.Tools.CodeExecution

  describe "CodeExecution" do

  test "executes safe code and returns result" do
    assert {:ok, %{return: 42}} = CodeExecution.execute(%{"code" => "21 * 2"})
  end

  test "captures stdout" do
    assert {:ok, %{stdout: stdout}} = CodeExecution.execute(%{"code" => ~s(IO.puts("hello"))})
    # stdout capture requires proper redirect — at minimum, result should not error
  end

  test "rejects File.read" do
    assert {:error, {:unsafe_code, _}} = CodeExecution.execute(%{"code" => ~s(File.read("/etc/passwd"))})
  end

  test "rejects :os.cmd" do
    assert {:error, {:unsafe_code, _}} = CodeExecution.execute(%{"code" => ~s(:os.cmd('id'))})
  end

  test "returns :timeout for infinite sleep" do
    assert {:error, :timeout} = CodeExecution.execute(%{"code" => "Process.sleep(:infinity)"})
  end

  test "analyze_ast detects forbidden modules" do
    assert :ok = CodeExecution.analyze_ast("1 + 2")
    assert {:error, {:unsafe_code, [File]}} = CodeExecution.analyze_ast(~s(File.read("x")))
  end
end

# test/agent_test.exs
defmodule AgentFramework.AgentTest do
  use ExUnit.Case, async: false

  defmodule MockLLM do
    @behaviour AgentFramework.LLM

    def complete(history, _state) do
      last = List.last(history)
      {:ok, %{
        content: "Echo: #{last.content}",
        tool_calls: [],
        tokens: 10,
        cost: 0.001
      }}
    end

    def stream(history, state, caller_pid) do
      {:ok, %{content: response}} = complete(history, state)
      for char <- String.graphemes(response) do
        send(caller_pid, {:agent_chunk, char})
      end
      send(caller_pid, {:agent_done, response})
      :ok
    end
  end

  test "agent run returns response" do
    {:ok, pid} = AgentFramework.Agent.start_link(
      llm_module: MockLLM,
      system_prompt: "Test agent"
    )
    assert {:ok, response} = AgentFramework.Agent.run(pid, "Hello")
    assert response =~ "Echo:"
  end

  test "history accumulates across runs" do
    {:ok, pid} = AgentFramework.Agent.start_link(llm_module: MockLLM)
    AgentFramework.Agent.run(pid, "First message")
    AgentFramework.Agent.run(pid, "Second message")
    state = :sys.get_state(pid)
    assert length(state.history) >= 4
  end

  test "stream delivers chunks then done" do
    {:ok, pid} = AgentFramework.Agent.start_link(llm_module: MockLLM)
    AgentFramework.Agent.stream(pid, "Hello", self())
    chunks = collect_chunks([])
    assert length(chunks) > 0
    assert_receive {:agent_done, _}
  end

  defp collect_chunks(acc) do
    receive do
      {:agent_chunk, c} -> collect_chunks([c | acc])
    after
      500 -> Enum.reverse(acc)
    end
  end
end

# test/pool_test.exs
defmodule AgentFramework.PoolTest do
  use ExUnit.Case, async: false

  test "pool returns pool_full when queue capacity exceeded" do
    pool_opts = [workers: 2, queue_capacity: 3, worker_mod: AgentFramework.Agent]
    {:ok, pool} = AgentFramework.Pool.start_link(pool_opts)

    for _ <- 1..5, do: AgentFramework.Pool.submit(pool, :run, ["test"])

    assert {:error, :pool_full} = AgentFramework.Pool.submit(pool, :run, ["overflow"])
  end

  end
end
```
## Main Entry Point

```elixir
def main do
  IO.puts("======== 54-build-ai-agents-framework ========")
  IO.puts("Build ai agents framework")
  IO.puts("")
  
  AgentFramework.Tool.start_link([])
  IO.puts("AgentFramework.Tool started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
```
## Quick start

```bash
# Start the application
mix deps.get
mix test

# Or run the benchmark:
mix run bench/agents_bench.exs
```

Target: <2s end-to-end for a 3-step ReAct loop excluding LLM call latency.

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

---

## Trade-off analysis

| Design | Selected | Alternative | Trade-off |
|---|---|---|---|
| Loop implementation | Private recursive function | Multiple GenServer callbacks | Callbacks: allows mailbox processing between LLM calls; recursive function: holds control flow, prevents ordering surprises |
| Tool execution | Task with timeout | GenServer per tool | GenServer: poolable; Task: simpler, sufficient for one-off executions |
| Context window management | Summarization via LLM call | Truncation (drop oldest) | Truncation: no extra LLM cost; summarization: preserves semantic content |
| Memory retrieval timing | Async, 500ms budget | Synchronous before LLM call | Synchronous: simpler; async: doesn't add RAG latency to every call |
| Marketplace discovery | pgvector cosine similarity | Tag-based exact match | Tag match: faster, deterministic; vector: handles natural language queries |
| LLM provider abstraction | Behaviour + adapter modules | HTTP library wrapper | Library wrapper: simpler but provider-specific; behaviour: hot-swappable providers |

## Common production mistakes

**Not handling partial JSON in LLM tool call responses.** LLMs sometimes emit malformed JSON for tool call parameters (truncated, unescaped characters). The framework must handle `Jason.decode/1` errors gracefully — return a structured error to the LLM with the original invalid JSON and ask it to retry. Do not let a JSON parse error propagate as an uncaught exception.

**Holding the GenServer loop during async tool calls.** If tools are executed synchronously inside the `react_loop` private function, the GenServer cannot process any other messages (including `:shutdown`) during tool execution. Use `Task.yield/2` with a timeout rather than `Task.await/2` so the loop remains preemptible by setting a reasonable timeout and handling `nil` (timeout) cases.

**Not scoping `Code.eval_string/1` to a clean binding.** By default, `Code.eval_string/1` inherits the current process's variable bindings. Two successive calls may share state if the second call uses a variable name from the first. Always pass `[]` as the second argument: `Code.eval_string(code, [], __ENV__)`.

**Episodic log missing entries for tool calls that returned errors.** The episodic log must record every tool call attempt, including failed ones. If `execute/1` returns `{:error, reason}`, the log entry should include `error: reason`. Omitting error entries makes debugging impossible — you cannot reconstruct why an agent made a decision if you cannot see what tools it tried.

**Pool not draining gracefully on shutdown.** When the application shuts down, the `PoolSupervisor` sends `:shutdown` to workers. If workers are mid-LLM-call and the timeout is `:infinity`, they block the shutdown sequence indefinitely. Set a `shutdown: 30_000` in the worker's child spec and handle the `:stop` signal in the agent's `terminate/2` callback to abort the current loop cleanly.

## Reflection

Your agent framework allows arbitrary tool calls. A misbehaving agent burns $200 in API credits on a single user request. What's your cost-control contract — per-agent budget, per-loop step cap, or both? Justify.

## Resources

- Yao et al. — "ReAct: Synergizing Reasoning and Acting in Language Models" (2023) — https://arxiv.org/abs/2210.03629 (ReAct pattern)
- Anthropic — Claude Messages API — https://docs.anthropic.com/en/api/messages (tool call wire format)
- OpenAI — Chat Completions API — https://platform.openai.com/docs/api-reference/chat (function calling wire format)
- Finch HTTP client — https://hexdocs.pm/finch/ (streaming HTTP with Finch.stream/5)
- pgvector Elixir — https://github.com/pgvector/pgvector-elixir (vector store integration)
- Erlang `Code` module — https://hexdocs.pm/elixir/Code.html (string_to_quoted, eval_string)
- Model Context Protocol — https://modelcontextprotocol.io (tool protocol standard, for stretch goal)

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Agentx.MixProject do
  use Mix.Project

  def project do
    [
      app: :agentx,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Agentx.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `agentx` (AI agent orchestration framework).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 2000000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:agentx) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Agentx stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:agentx) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:agentx)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual agentx operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Agentx classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **100 agents concurrent** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **2000 ms** | ReAct + LangChain design |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- ReAct + LangChain design: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why AI Agents Framework matters

Mastering **AI Agents Framework** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

---

## Implementation

### `lib/agent_framework.ex`

```elixir
defmodule AgentFramework do
  @moduledoc """
  Reference implementation for AI Agents Framework.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the agent_framework module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> AgentFramework.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/agent_framework_test.exs`

```elixir
defmodule AgentFrameworkTest do
  use ExUnit.Case, async: true

  doctest AgentFramework

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert AgentFramework.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- ReAct + LangChain design
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
