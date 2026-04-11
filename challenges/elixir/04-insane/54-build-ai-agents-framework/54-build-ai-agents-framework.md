# AI Agents Framework

**Project**: `agent_framework` — Native BEAM AI agents framework with ReAct loop, tool execution, and AST sandboxing

## Project context

Your team is building an AI assistant for a legal research platform. The assistant must search case law databases, summarize documents, run code to compute statistics, and delegate specialized tasks (contract analysis, precedent ranking) to specialist sub-agents. The orchestration logic is complex: plan a research question, call search tools, analyze results, synthesize findings, produce a final report.

Python frameworks (LangChain, AutoGen) are considered but rejected: they carry runtime overhead, cannot model OTP supervision, and mix the orchestration loop with LLM provider specifics. The BEAM is a natural fit: each agent is a process, failures are isolated, the supervisor tree handles restarts, and `Task.async_stream` handles parallel tool calls.

You will build `AgentFramework`: a native BEAM AI agents framework. No LangChain. No external runtimes. The only external protocol boundary is the HTTP wire format to the LLM provider.

## Why agents are GenServers and not plain Task processes

An agent must hold state across multiple turns: conversation history, tool registry, memory, cost accumulator. A `Task` is stateless and terminates. A `GenServer` holds state indefinitely, can be supervised (auto-restarted on crash), registered by name (discoverable by other agents), and receives messages (for approval signals, streaming results). The actor model maps directly to the agent abstraction.

## Why the ReAct loop must be a recursive function, not multiple GenServer callbacks

The ReAct loop is: call LLM → if tool calls, execute tools → call LLM again → repeat until final response. If implemented as `handle_call` → `handle_cast` → `handle_call` (multiple rounds), the agent's `GenServer` mailbox queues incoming messages from other processes between each LLM call. This causes ordering problems: a `stop` signal from the supervisor arrives between two LLM calls but is not processed until the loop completes. A private recursive function holds the control flow without yielding the GenServer between iterations.

## Why sandbox code execution requires AST analysis before evaluation

`Code.eval_string/1` executes arbitrary Elixir code with full access to the BEAM. A user-supplied snippet that calls `File.read("/etc/passwd")` or `:os.cmd("rm -rf /")` causes real damage. Allowing the LLM to generate and run arbitrary code requires a sandboxing layer. The AST analysis approach: parse with `Code.string_to_quoted!/1`, walk the AST with `Macro.prewalk/2`, and reject any node that calls forbidden modules (`File`, `Port`, `System`, `:os`, `:file`, etc.). This is static analysis before evaluation.

## Project Structure

```
agent_framework/
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

```elixir
defmodule AgentFramework.Tool do
  @callback name() :: String.t()
  @callback description() :: String.t()
  @callback parameters_schema() :: map()  # JSON Schema
  @callback execute(params :: map()) :: {:ok, term()} | {:error, term()}

  @optional_callbacks []

  @doc "Validate parameters against schema before calling execute/1"
  def call(tool_module, raw_params) do
    schema = tool_module.parameters_schema()
    case validate_schema(raw_params, schema) do
      :ok ->
        timeout = Application.get_env(:agent_framework, :tool_timeout, 30_000)
        task = Task.async(fn -> tool_module.execute(raw_params) end)
        case Task.yield(task, timeout) || Task.shutdown(task) do
          {:ok, result} -> result
          nil -> {:error, :timeout}
        end
      {:error, reason} ->
        {:error, {:schema_violation, reason}}
    end
  end

  defp validate_schema(params, schema) do
    # TODO: validate params against JSON Schema
    # HINT: check required fields, type constraints
    # For simplicity: verify required keys are present and types match
    required = Map.get(schema, "required", [])
    properties = Map.get(schema, "properties", %{})
    missing = Enum.filter(required, fn k -> not Map.has_key?(params, k) end)
    if missing != [] do
      {:error, "missing required fields: #{inspect(missing)}"}
    else
      type_errors = Enum.filter(properties, fn {k, prop} ->
        val = Map.get(params, k)
        val != nil and not type_matches?(val, prop["type"])
      end)
      if type_errors != [] do
        {:error, "type mismatch: #{inspect(Enum.map(type_errors, &elem(&1, 0)))}"}
      else
        :ok
      end
    end
  end

  defp type_matches?(val, "string"), do: is_binary(val)
  defp type_matches?(val, "integer"), do: is_integer(val)
  defp type_matches?(val, "number"), do: is_number(val)
  defp type_matches?(val, "boolean"), do: is_boolean(val)
  defp type_matches?(_val, _type), do: true
end
```

### Step 2: Agent GenServer with ReAct loop

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

  defp react_loop(_history, _state, max_iter) when max_iter >= 0 and max_iter >= 10 do
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
    # Execute all tool calls; concurrent for independent tools
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

  defp hitl_required?(tool_module) do
    # TODO: check if tool_module has @requires_approval true attribute
    false
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
        # Detect Module.function calls
        {{:., _, [{:__aliases__, _, mod_parts}, _func]}, _, _} ->
          mod = Module.concat(mod_parts)
          if mod in @forbidden_modules do
            {node, [mod | acc]}
          else
            {node, acc}
          end
        # Detect :erlang_module.function calls
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

  defp run_sandboxed(code) do
    parent = self()
    task = Task.Supervisor.async_nolink(AgentFramework.TaskSupervisor, fn ->
      # Capture output and return value
      {result, output} = capture_output(fn ->
        try do
          {value, _bindings} = Code.eval_string(code)
          {:ok, value}
        rescue
          e -> {:error, Exception.message(e)}
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

  defp capture_output(fun) do
    # TODO: use ExUnit.CaptureIO or redirect :stdio to capture output
    result = fun.()
    {result, ""}
  end
end
```

### Step 4: LLM behaviour and retry

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
          Process.sleep(backoff + :rand.uniform(div(backoff, 5)))
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

```elixir
defmodule AgentFramework.Memory.ShortTerm do
  @summarize_threshold 0.80
  @chars_per_token 4

  @doc "Check if history needs summarization; if so, call LLM to summarize oldest half"
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

## Given tests

```elixir
# test/tools/code_execution_test.exs
defmodule AgentFramework.Tools.CodeExecutionTest do
  use ExUnit.Case, async: true
  alias AgentFramework.Tools.CodeExecution

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
      # Simple mock: echo the user's message back
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
    assert length(state.history) >= 4  # 2 user + 2 assistant messages
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
    # Pool: 2 workers, queue capacity 3
    pool_opts = [workers: 2, queue_capacity: 3, worker_mod: AgentFramework.Agent]
    {:ok, pool} = AgentFramework.Pool.start_link(pool_opts)

    # Fill all workers and queue
    for _ <- 1..5, do: AgentFramework.Pool.submit(pool, :run, ["test"])

    # 6th should be rejected
    assert {:error, :pool_full} = AgentFramework.Pool.submit(pool, :run, ["overflow"])
  end
end
```

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

## Resources

- Yao et al. — "ReAct: Synergizing Reasoning and Acting in Language Models" (2023) — https://arxiv.org/abs/2210.03629 (ReAct pattern)
- Anthropic — Claude Messages API — https://docs.anthropic.com/en/api/messages (tool call wire format)
- OpenAI — Chat Completions API — https://platform.openai.com/docs/api-reference/chat (function calling wire format)
- Finch HTTP client — https://hexdocs.pm/finch/ (streaming HTTP with Finch.stream/5)
- pgvector Elixir — https://github.com/pgvector/pgvector-elixir (vector store integration)
- Erlang `Code` module — https://hexdocs.pm/elixir/Code.html (string_to_quoted, eval_string)
- Model Context Protocol — https://modelcontextprotocol.io (tool protocol standard, for stretch goal)
