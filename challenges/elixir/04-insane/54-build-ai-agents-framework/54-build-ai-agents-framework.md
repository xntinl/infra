# 54. Build an AI Agents Framework
**Difficulty**: Insane

## Prerequisites
- Mastered: GenServer and OTP supervision trees, HTTP client (Finch/Req), streaming HTTP responses (SSE), JSON encoding/decoding, vector similarity search concepts, ETS, PubSub, Phoenix Channels or LiveView for streaming UI
- Study first: OpenAI function calling specification, Anthropic tool use documentation, LangChain architecture (Python source for concepts), "Building LLM Powered Applications" (Ozdemir), Retrieval Augmented Generation survey paper, pgvector PostgreSQL extension docs

## Problem Statement
Build a production-grade AI agents framework in Elixir where each agent is an OTP process that calls LLM APIs, maintains conversation context, invokes external tools, coordinates with other agents, streams responses in real time, and operates reliably under failures — without depending on Python or any non-BEAM runtime.

1. Implement the Agent as a GenServer: it holds the full conversation history (list of `{role, content}` messages), a tool registry, and a system prompt; expose `Agent.run(pid, user_message)` which appends the message, calls the LLM, and returns the response; the context window is managed automatically — when it approaches the model's limit, summarize older messages via a second LLM call and replace them with the summary.
2. Implement the Tool system: a tool is a module implementing a `@behaviour AgentTool` with `name/0`, `description/0`, `parameters_schema/0` (JSON Schema), and `call/1`; the framework serializes all registered tools into the LLM request's `tools` field; when the LLM response contains a `tool_use` block, the framework invokes the correct tool, appends the result to context, and continues the agentic loop until the LLM produces a final text response.
3. Build concrete tool implementations: `WebSearchTool` (calls a search API and returns top 5 results), `CodeExecutionTool` (runs Elixir code in a sandboxed `Task` with a 5-second timeout and captures stdout), `DatabaseQueryTool` (executes read-only SQL against a configured Ecto repo and returns results as JSON), and `AgentCallTool` (spawns or calls another agent by name — enabling multi-agent coordination).
4. Implement memory via Retrieval Augmented Generation: store all agent conversation turns as vector embeddings in PostgreSQL via pgvector; before each LLM call, retrieve the 5 most semantically similar past turns using cosine similarity (`<=>` operator); inject retrieved context into the system prompt as "relevant past context"; support `Memory.clear(agent_id)` to wipe an agent's history.
5. Build multi-agent coordination: an Orchestrator agent breaks a task into subtasks and dispatches them to Specialist agents (e.g., `ResearchAgent`, `CodeAgent`, `ReviewAgent`) via `AgentCallTool`; the Orchestrator collects specialist results and synthesizes a final answer; specialists run concurrently via `Task.async_stream` with a configurable timeout per specialist.
6. Implement LLM response streaming: use Finch's streaming API to receive SSE chunks from the LLM; forward each chunk to the caller via `GenServer.reply` extended with `send(caller_pid, {:chunk, text})`; expose a `Agent.stream(pid, message, callback_fn)` API that calls `callback_fn.(chunk)` for each streamed token; for Phoenix LiveView callers, push chunks via `send(liveview_pid, {:chunk, text})`.
7. Build observability: emit a `:telemetry` event for every LLM call containing `{model, prompt_tokens, completion_tokens, latency_ms, tool_calls_made, cost_usd}`; aggregate per-agent stats in ETS (total calls, total cost, average latency); expose `Observability.report(agent_id)` returning a summary map; log every tool call with input, output, and duration at `Logger.info` level with structured metadata.
8. Implement retry and fallback: if an LLM API call fails (network error, rate limit 429, server error 5xx), retry with exponential backoff up to 3 times; if all retries fail, attempt the same call on a configured fallback model (e.g., primary `claude-opus-4-5`, fallback `claude-haiku-4-5`); if the fallback also fails, return `{:error, :llm_unavailable}` to the caller; never silently swallow errors.

## Acceptance Criteria
- [ ] Agent lifecycle: `Agent.run/2` completes a full agentic loop (user message → LLM call → tool call → LLM call → final response) in a single call; the conversation history is correctly maintained across multiple `run/2` calls on the same agent; context summarization triggers automatically when token count exceeds the configured threshold
- [ ] Tool calling: a tool registered with the framework is automatically included in every LLM request; the framework correctly parses a `tool_use` response block, calls the tool, and continues the loop; a tool that raises an exception is caught, the error is appended to context as a `tool_result` with `is_error: true`, and the agent continues
- [ ] Concrete tools: `WebSearchTool` returns structured results; `CodeExecutionTool` returns stdout for correct code and a timeout error for code that runs longer than 5 seconds; `DatabaseQueryTool` rejects any non-`SELECT` statement with a clear error; `AgentCallTool` successfully delegates a subtask to another running agent
- [ ] RAG memory: retrieved context appears in the system prompt for each LLM call; similarity search returns results with cosine similarity > 0.7 for semantically related past turns; `Memory.clear/1` removes all embeddings for the agent and subsequent calls receive no retrieved context
- [ ] Multi-agent: an Orchestrator that coordinates 3 Specialists completes a task where no single specialist has all the required information; specialists run concurrently and the Orchestrator collects all results before synthesizing; a specialist that times out is reported as failed without blocking the others
- [ ] Streaming: `Agent.stream/3` delivers the first chunk within 500 ms of the LLM beginning its response; all chunks arrive in order; the callback receives the complete response when assembled from all chunks — verified by comparing streamed vs. non-streamed responses for the same prompt
- [ ] Observability: every LLM call emits a telemetry event; `Observability.report/1` returns accurate token counts (within ±5% of the LLM's reported usage); cost is computed correctly from token counts and published model pricing
- [ ] Retry/fallback: a simulated 429 response triggers exponential backoff and retries; a simulated consecutive failure on the primary model triggers a fallback model call; three consecutive failures on both models return `{:error, :llm_unavailable}` without crashing the agent process

## What You Will Learn
- Agentic loop design: why the LLM → tool → LLM cycle is not a simple request/response and how to model it as a stateful process without blocking the BEAM scheduler
- Tool calling protocol: how OpenAI and Anthropic structure `tools`, `tool_use`, and `tool_result` message formats, and how to build an abstraction layer that works with both
- Vector embeddings in PostgreSQL: how pgvector stores and queries high-dimensional vectors, what cosine similarity means operationally, and the latency/accuracy trade-offs of approximate nearest-neighbor search
- SSE streaming over HTTP in Elixir: how Finch's streaming mode works, how to forward chunks without buffering them in the GenServer's mailbox, and the back-pressure implications
- Multi-agent orchestration: the difference between an orchestrator-worker pattern and a peer-to-peer agent network, and when each is appropriate
- Cost-aware design: why token counting and cost attribution matter in production AI systems and how to implement them without calling the LLM's tokenization API

## Hints (research topics, NO tutorials)
- Model the agentic loop as a recursive private function `loop(state, messages)` inside the GenServer that tail-calls itself on `tool_use` and returns on a text response — keep the GenServer's `handle_call` thin
- For streaming: Finch supports a `receive_timeout` and a streaming callback; study `Finch.stream/5` and how to forward chunks to an external pid without buffering
- Tool parameter validation: use `ExJsonSchema` to validate tool inputs against the `parameters_schema` before calling the tool — fail fast with a clear error message rather than passing malformed data to the tool
- pgvector + Ecto: use a custom Ecto type `Pgvector.Ecto.Vector` to store and retrieve embedding arrays; the `<=>` operator translates to a fragment in Ecto queries
- Context window management: count tokens approximately (4 chars ≈ 1 token) or use the model's `/tokenize` endpoint if available; trigger summarization at 80% of the limit to leave room for the summary itself
- For sandboxed code execution: spawn a `Task` under a `DynamicSupervisor` with a timeout; use `Code.eval_string/2` with restricted bindings; never expose the tool to the file system or network without explicit allow-listing

## Reference Material
- OpenAI function calling docs: https://platform.openai.com/docs/guides/function-calling
- Anthropic tool use docs: https://docs.anthropic.com/en/docs/tool-use
- "Building LLM Powered Applications" — Valentina Alto (Ozdemir), Packt Publishing
- Lewis et al. (2020). "Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks" — https://arxiv.org/abs/2005.11401
- pgvector: https://github.com/pgvector/pgvector
- Finch streaming: https://hexdocs.pm/finch — `Finch.stream/5` documentation
- LangChain architecture overview: https://python.langchain.com/docs/concepts (for conceptual reference only — implement in Elixir)

## Difficulty Rating ★★★★★★☆
The framework itself is composable from known BEAM primitives, but correctness requires deep understanding of the agentic loop (tool → continue vs. tool → stop decisions), the streaming protocol mechanics, and the vector retrieval pipeline. The multi-agent coordination adds distributed state management on top.

## Estimated Time
100–180 hours
