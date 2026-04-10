# 54. Build an AI Agents Framework

**Difficulty**: Insane

**Estimated time**: 120+ hours

## Overview

Large Language Models become genuinely useful when they can plan, act, and recover — not just respond. The "agent loop" is the architectural primitive that makes this possible: a stateful process that receives a goal, decides which tools to call, observes the results, and iterates until the goal is satisfied or a failure condition is reached. Python ecosystems (LangChain, AutoGen, CrewAI) have explored this space, but they carry runtime overhead, lack native concurrency primitives, and cannot model the supervision and fault isolation that production systems demand.

Elixir and OTP are a near-perfect substrate for agents: each agent is a process, failures are isolated, supervision trees handle restarts, and the actor model maps directly to message-passing between agents. This challenge asks you to build that framework from first principles — not a wrapper around a Python library, but a native BEAM implementation where OTP patterns are the architecture, not an afterthought.

## The Challenge

Design and implement a production-grade AI Agents Framework in Elixir where agents are OTP processes that can plan, call tools, maintain memory, coordinate with other agents, stream responses, and operate correctly under partial failures. The framework must be usable as a library: a developer imports it, defines tools and agents, and builds multi-agent systems without understanding the internals.

The framework must support both the ReAct pattern (Reason + Act interleaved) and a planner-executor pattern (decompose goal into sub-tasks, assign to specialists). It must handle LLM providers at the protocol level, not the library level — the only dependency on any specific LLM is the HTTP wire format.

## Core Requirements

### 1. Agent Primitive

Each agent is a `GenServer` that holds: a system prompt, a conversation history as a list of `%Message{role, content, tool_calls, tool_results}` structs, a registered tool set, a planning mode (`:react` or `:planner`), and a cost accumulator. The agent runs an agentic loop: send the current history to the LLM, parse the response, if the response contains tool calls then execute each tool, append the results to history, and recurse — terminating when the LLM produces a final text response with no tool calls, or when a configurable `max_iterations` limit is reached. The loop must be implemented as a private recursive function, not as multiple `handle_call` round-trips.

### 2. Tool System

A tool is a module implementing the `AgentFramework.Tool` behaviour with four callbacks: `name/0` returning a string identifier, `description/0` returning a human-readable string for the LLM, `parameters_schema/0` returning a JSON Schema map that the framework serializes into the LLM request, and `execute/1` receiving the validated parameter map and returning `{:ok, result}` or `{:error, reason}`. The framework validates tool inputs against the schema before calling `execute/1` — malformed inputs are returned to the LLM as a structured error without calling the tool. Tool execution timeout is configurable per tool, defaulting to 30 seconds. A tool that times out returns `{:error, :timeout}` to the loop without crashing the agent.

### 3. Built-in Tool Implementations

Implement four concrete tools that the framework ships:

- `WebSearchTool`: accepts `%{query: string, num_results: integer}`, calls a configurable search API, returns the top N results as structured maps with title, url, and snippet.
- `CodeExecutionTool`: accepts `%{code: string, language: "elixir"}`, evaluates Elixir code in a sandboxed `Task` under a `DynamicSupervisor` with a 5-second wall-clock timeout, captures stdout and the return value, and rejects any code containing file system or network calls by static analysis of the AST before execution.
- `DatabaseQueryTool`: accepts `%{sql: string, repo: atom}`, executes the query against a configured Ecto repo, enforces that the statement is a `SELECT` (parse and reject DDL, DML, and stored procedure calls), and returns results as a list of maps.
- `AgentCallTool`: accepts `%{agent_name: string, goal: string}`, looks up the named agent in the agent registry, delegates the goal via `GenServer.call/3` with a configurable timeout, and returns the agent's response. This tool enables multi-agent delegation without the orchestrator knowing the sub-agent's implementation.

### 4. Memory Architecture

Implement three memory tiers that agents use automatically:

Short-term memory is the conversation history held in the `GenServer` state. When the estimated token count of the history exceeds 80% of the configured context window limit, the agent automatically calls the LLM with a summarization prompt on the oldest half of the history, replaces those messages with a single summary message, and continues. Token estimation uses the heuristic of 4 characters per token unless the model reports exact counts in its response metadata.

Long-term memory is a vector store integration. After each completed agent turn (one full user request to final response), the turn is embedded using a configured embedding model and stored with `pgvector`. Before each LLM call, retrieve the 5 most semantically similar past turns using cosine similarity (threshold 0.7) and inject them into the system prompt as structured context. The embedding and retrieval operations must not block the agent loop — execute them asynchronously and incorporate results only when available within a 500ms budget.

Episodic memory is a structured log of agent decisions: every time the agent chooses to call a tool, record `{timestamp, agent_id, tool_name, input, output, decision_rationale}` where the rationale is extracted from the LLM's reasoning text preceding the tool call. This log is queryable by agent, time range, and tool name.

### 5. Multi-agent Coordination

A supervisor agent receives a complex goal and decomposes it into an ordered list of sub-tasks using a planning LLM call. Each sub-task is assigned to a specialist agent registered in the agent marketplace. Sub-tasks with no dependencies between them are dispatched concurrently via `Task.async_stream`. Sub-tasks with declared dependencies are dispatched sequentially, with the output of one task injected into the context of the next. The supervisor collects all specialist results and makes a final synthesis LLM call to produce the aggregated answer.

A specialist that exceeds its timeout is reported as `{:error, :specialist_timeout, agent_name}` and included in the synthesis context — the supervisor must not block on a timed-out specialist. A specialist that crashes is restarted by the OTP supervision tree and the orchestrator retries the subtask once before marking it failed.

### 6. LLM Response Streaming

Implement streaming at the HTTP level using `Finch.stream/5`. As SSE chunks arrive, parse them incrementally and forward each decoded text token to the caller via `Process.send(caller_pid, {:agent_chunk, token})`. The agent loop itself runs on streamed responses: tool call detection is applied to the accumulated streamed content, not to a complete response. Expose `AgentFramework.stream/3` accepting an agent pid, a user message, and a caller pid — the function returns immediately and the caller receives `{:agent_chunk, token}` messages followed by `{:agent_done, final_response}` or `{:agent_error, reason}`.

### 7. Retry and Fallback

LLM API calls are wrapped with a retry policy: on HTTP 429 or 5xx responses, retry with exponential backoff starting at 1 second, doubling each attempt, capped at 60 seconds, for a maximum of 3 retries. If all retries fail on the primary model, attempt the same call on a configured fallback model. If the fallback also fails after its own retry policy, the agent returns `{:error, :llm_unavailable}` to the caller without crashing. The agent process remains alive and accepts new requests after an `llm_unavailable` failure. Log every retry attempt with structured metadata: `[model, attempt, status_code, backoff_ms, error]`.

### 8. Agent Observability

Emit `:telemetry` events for every significant operation: `[:agent_framework, :llm_call, :start]`, `[:agent_framework, :llm_call, :stop]`, `[:agent_framework, :tool_call, :start]`, `[:agent_framework, :tool_call, :stop]`, `[:agent_framework, :memory_retrieval, :stop]`. Each event carries a measurements map and a metadata map. The stop events include `duration_microseconds`, `token_count`, and `cost_usd` where applicable. Cost is computed from token counts using configurable per-model pricing tables stored as application config.

Aggregate per-agent statistics in ETS: total LLM calls, total tokens consumed, total cost in USD, number of tool calls, number of failed tool calls, average loop iterations to completion. Expose `AgentFramework.Stats.report(agent_id)` returning a structured map, and `AgentFramework.Stats.report_all()` returning stats for every registered agent.

### 9. Human-in-the-Loop

Agents support a `:hitl` mode where certain tool calls require human approval before execution. A tool is marked as requiring approval by setting `@requires_approval true` in its module. When the agent loop reaches a tool call for an approval-required tool, it pauses by sending `{:approval_required, agent_id, tool_name, input}` to a registered approval handler pid, then blocks with a `receive` with a configurable timeout (default 5 minutes). The human can approve with `AgentFramework.approve(agent_id, tool_call_id)` or reject with `AgentFramework.reject(agent_id, tool_call_id, reason)`. A rejection appends a `tool_result` with the rejection reason to the history and the agent loop continues. A timeout in waiting for approval is treated identically to a rejection.

### 10. Agent Marketplace

Implement a registry where agents advertise their capabilities. An agent registers itself with a name, a description, a list of capability tags, and an input/output schema. Implement `AgentFramework.Marketplace.discover(query)` that returns matching agents ranked by relevance — relevance is computed by embedding the query and performing cosine similarity against stored agent description embeddings. Marketplace entries are stored in ETS with periodic backup to PostgreSQL. On node restart, the marketplace reloads from PostgreSQL and agents re-register on startup via their `Application` supervision tree entry.

### 11. Backpressure and Concurrency Control

Agent pools are managed by a `PoolSupervisor` that maintains a configurable number of agent worker processes. Incoming requests to a pool are queued in a bounded ETS queue. When the queue reaches its capacity limit, new requests receive `{:error, :pool_full}` immediately without blocking the caller. The pool tracks in-flight requests per worker and distributes new work to the worker with the fewest active tasks. Implement `AgentFramework.Pool.checkout/2` and `AgentFramework.Pool.checkin/2` for explicit pool management when the automatic dispatch is not appropriate.

## Acceptance Criteria

- An agent configured with `WebSearchTool` and `CodeExecutionTool` receives the goal `"Find the top 3 Elixir web frameworks and write a benchmarking script that measures their hello-world throughput"`, completes the full ReAct loop (search → reason → code → reason → final response) within 120 seconds, and the final response contains both a ranked list and syntactically valid Elixir code — verified by `Code.string_to_quoted/1`.

- Conversation history persists correctly: calling `run/2` three times on the same agent pid with successive follow-up questions produces responses that demonstrate awareness of the prior turns; the history accumulates correctly across all three calls.

- Context summarization triggers automatically when a synthetic history of 200 messages is injected into agent state: the next `run/2` call triggers a summarization LLM call, replaces the old messages with a summary, and the subsequent LLM call receives the shortened history — verified by intercepting LLM call counts and message counts.

- A tool registered with `parameters_schema` requiring `%{query: :string}` rejects a call with `%{query: 42}` without invoking `execute/1` and appends a structured error to the history; the agent loop continues and the LLM corrects its call on the next iteration.

- `CodeExecutionTool` returns stdout and return value for `IO.puts("hello"); 42`; returns `{:error, :timeout}` for `Process.sleep(:infinity)`; returns `{:error, :unsafe_code}` for `File.read("/etc/passwd")` — all without crashing the agent process.

- `DatabaseQueryTool` returns rows as maps for a valid `SELECT` statement; returns `{:error, :non_select_statement}` for `DROP TABLE users` without executing the query — verified by asserting no database mutation occurred.

- An orchestrator agent with three registered specialists (`ResearchAgent`, `SynthesisAgent`, `ReviewAgent`) completes a research task where each specialist contributes distinct information; the specialists run concurrently and the orchestrator's synthesis response references content from all three.

- Killing one specialist mid-task via `Process.exit(pid, :kill)` does not crash the orchestrator; the OTP supervisor restarts the specialist; the orchestrator receives `{:error, :specialist_timeout, "ResearchAgent"}` for that subtask and proceeds with synthesis using the available results.

- `AgentFramework.stream/3` delivers the first `{:agent_chunk, _}` message within 1 second of the LLM beginning its response; the caller receives chunks in correct order; the full response assembled from all chunks equals the response from the non-streaming `run/2` call for the same prompt.

- Simulating HTTP 429 from the LLM API causes 3 retry attempts with exponential delays (>=1s, >=2s, >=4s between attempts); after exhausting retries, the fallback model is attempted; exhausting fallback retries returns `{:error, :llm_unavailable}` and the agent remains alive and accepts the next request.

- A tool marked `@requires_approval true` pauses the agent loop; calling `AgentFramework.approve/2` with the correct agent id resumes execution; calling `AgentFramework.reject/2` appends the rejection as a tool result and continues the loop without executing the tool.

- `AgentFramework.Stats.report/1` returns token counts within +-5% of the values reported by the LLM provider in its response metadata; cost is computed correctly for at least two configured model pricing tiers.

- `AgentFramework.Marketplace.discover("agent that can write and execute code")` returns the `CodeAgent` as the top result when the marketplace contains at least 10 registered agents with varied descriptions — verified with cosine similarity > 0.8 between the query embedding and the top result's description embedding.

- A pool of 5 workers with queue capacity 10 accepts 10 queued requests without error when all workers are busy; the 11th concurrent request receives `{:error, :pool_full}` immediately; workers pick up queued requests as they become available and all 10 complete correctly.

- Every LLM call emits `[:agent_framework, :llm_call, :start]` and `[:agent_framework, :llm_call, :stop]` telemetry events with correct `agent_id`, `model`, and `duration_microseconds` — verified by attaching a test telemetry handler and asserting event receipt for a complete agent run.

## Constraints & Rules

- No Python, no Node.js, no non-BEAM runtimes. All components run on the BEAM.
- No wrapping of LangChain, AutoGen, or any existing agent framework. The HTTP wire format to the LLM provider is the only external protocol boundary.
- LLM provider support must be abstracted behind a `AgentFramework.LLM` behaviour so that Anthropic and OpenAI can be swapped by configuration without changing agent code.
- No agent process may crash due to a tool failure, an LLM API error, or a malformed LLM response. Every failure path must be handled explicitly and result in a structured error appended to history or returned to the caller.
- The framework must compile and its test suite must pass with `mix test` without any external services running (use mocks for LLM calls and vector store in unit tests).
- Sandbox enforcement in `CodeExecutionTool` is non-negotiable: before executing any code, perform AST-level analysis to detect calls to `File`, `Port`, `System`, `:os`, `:file`, `:gen_tcp`, `:httpc`, or any `:erlang` function that performs I/O. Code containing such calls must be rejected with `{:error, :unsafe_code}` without evaluation.
- All public API functions must have `@spec` type annotations and pass `mix dialyzer` without warnings.
- Streaming must not buffer the entire response before forwarding — chunks must be forwarded as they arrive from the HTTP layer.

## Stretch Goals

- Implement a visual tracing dashboard as a Phoenix LiveView that shows a real-time DAG of agent decisions, tool calls, and memory retrievals for any running agent — nodes for each step, edges showing data flow, color-coded by step type.
- Add support for the Model Context Protocol (MCP) as a tool transport: any MCP-compatible tool server can be registered and its tools are automatically imported into the framework's tool registry.
- Implement agent checkpointing: serialize the full agent state (history, memory, cost accumulator, episodic log) to a portable format and restore it in a new process — enabling agent migration across nodes and resumption after intentional shutdown.
- Add a cost circuit breaker: an agent configured with a maximum budget in USD automatically pauses and sends `{:budget_exceeded, agent_id, current_cost}` to a configured pid when accumulated cost reaches the limit, before making the next LLM call.
- Implement agent-to-agent communication over distributed Erlang: two agents on different BEAM nodes can exchange messages and delegate tasks using the same `AgentCallTool` interface, with location transparency.

## Evaluation Criteria

**Correctness of the agentic loop**: The ReAct loop must handle all cases correctly — text-only response (terminate), tool call response (execute and recurse), mixed response (extract all tool calls, execute all, append all results, recurse), and max-iterations exceeded (return partial result with error flag). Any loop that terminates prematurely or hangs indefinitely fails this criterion.

**OTP design quality**: Agents must be supervised, restartable, and isolated. A crash in one agent must not affect others. The supervision tree must be explicitly designed — not a flat list of workers under a single supervisor. Process registration, shutdown sequencing, and restart strategies must be deliberate choices justified by the semantics of each component.

**Tool system extensibility**: A new tool written outside the framework, implementing the `AgentFramework.Tool` behaviour, must be registerable with any agent at runtime without modifying framework code. The framework's serialization of tool schemas to LLM-specific formats must be correct for both Anthropic and OpenAI wire formats.

**Memory correctness**: Context summarization must not lose information that was referenced in the preserved history. RAG retrieval must only include turns above the configured similarity threshold. Episodic log entries must be accurate — no missing entries, no entries for tool calls that were rejected before execution.

**Failure handling completeness**: Every external call (LLM API, tool execution, vector store, embedding model) must have an explicit failure path that results in a structured error, not an uncaught exception. Test this by building a fault-injection harness that randomly fails each external call type and asserting the agent remains alive and returns structured errors.

**Performance under concurrency**: A pool of 10 agents running concurrently on a single BEAM node must process 100 independent requests in under 5 minutes (assuming LLM latency of 2 seconds per call). Agent processes must not be the bottleneck — measure scheduler utilization, process queue lengths, and ETS contention to identify and eliminate hot spots.
