# Map Pattern Matching: A Webhook Event Router

**Project**: `webhook_event_router` — routes incoming Stripe/GitHub-style webhook events to handlers via partial map matching

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Project structure

```
webhook_event_router/
├── lib/
│   └── webhook_event_router.ex
├── test/
│   └── webhook_event_router_test.exs
└── mix.exs
```

---

## Core concepts

Map patterns in Elixir are **partial by default**. The pattern
`%{type: "payment"}` matches ANY map that contains a `:type` key equal to
`"payment"`, regardless of what other keys exist. This is the opposite of
tuples and lists, which must match exactly.

This makes maps the right choice for payloads where extra fields should be
ignored (webhooks, API responses, config blobs). Coming from TypeScript,
think of it as structural typing with "extra properties allowed".

Two consequences:

1. **Order does not matter**. `%{a: 1, b: 2}` matches the same set of maps as
   `%{b: 2, a: 1}`.
2. **Required keys are those in the pattern**. A key absent from both the
   pattern and the input is simply not checked. Missing required keys in the
   input cause the match to fail.

Atom keys (`%{type: "x"}`) and string keys (`%{"type" => "x"}`) are different.
External JSON arrives with string keys; pick one convention and normalize
at the boundary.

---

## The business problem

A webhook endpoint receives JSON events from multiple providers. Each provider
uses a different field name for "event type":

- Stripe: `"type"` (e.g., `"charge.succeeded"`)
- GitHub: `"action"` + `"repository"` fields
- Our internal format: `:event` atom

The router must dispatch events to the right handler based on shape, ignoring
unrelated fields (timestamps, IDs, metadata) that vary wildly.

---

## Implementation

### `lib/webhook_event_router.ex`

```elixir
defmodule WebhookEventRouter do
  @moduledoc """
  Routes webhook payloads to handlers via partial map pattern matching.

  Each handler clause matches on only the keys it cares about. Extra fields
  (timestamps, IDs, metadata) are ignored without explicit wildcards.
  """

  @type result :: {:ok, map()} | {:error, :unknown_event}

  @doc """
  Dispatches a payload to the correct handler.

  Order of clauses matters — more specific patterns come first. Once a
  clause matches, the rest are skipped.
  """
  @spec route(map()) :: result()
  def route(payload) when is_map(payload) do
    case payload do
      # Stripe: string key "type" with dot-notation value.
      %{"type" => "charge.succeeded", "data" => %{"object" => charge}} ->
        handle_payment_success(charge)

      %{"type" => "charge.failed", "data" => %{"object" => charge}} ->
        handle_payment_failure(charge)

      # GitHub: action + repository. We don't care about sender, installation, etc.
      %{"action" => "opened", "pull_request" => pr, "repository" => repo} ->
        handle_pr_opened(pr, repo)

      %{"action" => "closed", "pull_request" => %{"merged" => true} = pr,
        "repository" => repo} ->
        handle_pr_merged(pr, repo)

      # Internal: atom key. Note the mix of atom and string keys in different
      # clauses is normal — you match the shape your input actually has.
      %{event: :user_signup, user_id: id} when is_binary(id) ->
        handle_signup(id)

      _ ->
        {:error, :unknown_event}
    end
  end

  @doc """
  Demonstrates that pattern order is irrelevant for map keys.

  Both patterns match the same set of inputs. The first to textually match
  in a case/function is chosen.
  """
  @spec has_user_and_action?(map()) :: boolean()
  def has_user_and_action?(%{user_id: _, action: _}), do: true
  def has_user_and_action?(%{action: _, user_id: _}), do: true
  def has_user_and_action?(_), do: false

  @doc """
  Extracts a nested value, requiring a specific shape at every level.

  If any key is missing or any value doesn't match, `:error` is returned.
  This is safer than chained `Map.get/2` calls with nil bubbling.
  """
  @spec extract_charge_id(map()) :: {:ok, String.t()} | :error
  def extract_charge_id(payload) do
    case payload do
      %{"data" => %{"object" => %{"id" => id}}} when is_binary(id) -> {:ok, id}
      _ -> :error
    end
  end

  # --- Handlers ---

  defp handle_payment_success(%{"id" => id, "amount" => amount}) do
    {:ok, %{kind: :payment_success, id: id, amount: amount}}
  end

  defp handle_payment_failure(%{"id" => id, "failure_code" => reason}) do
    {:ok, %{kind: :payment_failure, id: id, reason: reason}}
  end

  defp handle_pr_opened(%{"number" => number, "title" => title}, %{"full_name" => repo}) do
    {:ok, %{kind: :pr_opened, repo: repo, number: number, title: title}}
  end

  defp handle_pr_merged(%{"number" => number}, %{"full_name" => repo}) do
    {:ok, %{kind: :pr_merged, repo: repo, number: number}}
  end

  defp handle_signup(user_id) do
    {:ok, %{kind: :user_signup, user_id: user_id}}
  end
end
```

### `test/webhook_event_router_test.exs`

```elixir
defmodule WebhookEventRouterTest do
  use ExUnit.Case, async: true

  alias WebhookEventRouter, as: Router

  describe "Stripe events" do
    test "charge.succeeded extracts amount and id" do
      payload = %{
        "type" => "charge.succeeded",
        "id" => "evt_1",
        "created" => 1_700_000_000,
        "data" => %{"object" => %{"id" => "ch_abc", "amount" => 5000}}
      }

      assert {:ok, %{kind: :payment_success, id: "ch_abc", amount: 5000}} =
               Router.route(payload)
    end

    test "charge.failed captures failure_code" do
      payload = %{
        "type" => "charge.failed",
        "data" => %{"object" => %{"id" => "ch_bad", "failure_code" => "card_declined"}}
      }

      assert {:ok, %{kind: :payment_failure, reason: "card_declined"}} =
               Router.route(payload)
    end

    test "extra Stripe fields are ignored" do
      payload = %{
        "type" => "charge.succeeded",
        "id" => "evt_x",
        "livemode" => false,
        "request" => %{"id" => "req_1"},
        "api_version" => "2020-08-27",
        "data" => %{"object" => %{"id" => "ch_x", "amount" => 1000}}
      }

      assert {:ok, _} = Router.route(payload)
    end
  end

  describe "GitHub events" do
    test "pull_request opened" do
      payload = %{
        "action" => "opened",
        "pull_request" => %{"number" => 42, "title" => "Fix bug"},
        "repository" => %{"full_name" => "org/repo"},
        "sender" => %{"login" => "alice"}
      }

      assert {:ok, %{kind: :pr_opened, number: 42, title: "Fix bug", repo: "org/repo"}} =
               Router.route(payload)
    end

    test "pull_request merged (closed + merged=true)" do
      payload = %{
        "action" => "closed",
        "pull_request" => %{"number" => 99, "merged" => true},
        "repository" => %{"full_name" => "org/repo"}
      }

      assert {:ok, %{kind: :pr_merged, number: 99}} = Router.route(payload)
    end

    test "pull_request closed without merge is not routed as merged" do
      payload = %{
        "action" => "closed",
        "pull_request" => %{"number" => 99, "merged" => false},
        "repository" => %{"full_name" => "org/repo"}
      }

      # Not matched by any clause above — falls through to unknown.
      assert {:error, :unknown_event} = Router.route(payload)
    end
  end

  describe "internal atom-keyed events" do
    test "user_signup dispatches" do
      assert {:ok, %{kind: :user_signup, user_id: "u-7"}} =
               Router.route(%{event: :user_signup, user_id: "u-7", ts: 1_700})
    end
  end

  describe "unknown events" do
    test "returns error for unmatched shapes" do
      assert {:error, :unknown_event} = Router.route(%{"random" => "payload"})
    end

    test "returns error for empty map" do
      assert {:error, :unknown_event} = Router.route(%{})
    end
  end

  describe "extract_charge_id/1" do
    test "extracts nested id" do
      payload = %{"data" => %{"object" => %{"id" => "ch_123"}}}
      assert {:ok, "ch_123"} = Router.extract_charge_id(payload)
    end

    test "fails when any level missing" do
      assert :error = Router.extract_charge_id(%{"data" => %{}})
      assert :error = Router.extract_charge_id(%{})
    end
  end

  describe "key order irrelevance" do
    test "pattern matches regardless of source order" do
      assert Router.has_user_and_action?(%{user_id: "a", action: :x})
      assert Router.has_user_and_action?(%{action: :x, user_id: "a"})
      refute Router.has_user_and_action?(%{action: :x})
    end
  end
end
```

### Run it

```bash
mix new webhook_event_router
cd webhook_event_router
mix test
```

---

## Trade-offs and production mistakes

**1. String vs atom keys**
JSON parsers return string keys. Do NOT call `String.to_atom/1` on webhook
field names — the atom table is DoS-prone. Either match string keys directly
(as shown) or normalize via `Jason.decode!(body, keys: :atoms!)` which only
converts to existing atoms.

**2. Empty map pattern matches everything**
`%{}` matches any map, including empty. If you want "has no keys", use a
guard: `when map_size(m) == 0`.

**3. Clause order matters for overlap**
If you write the general clause first, specific clauses never fire. Put
specific patterns first, fallbacks last.

**4. Struct vs plain map**
`%User{id: id}` only matches `%User{}` structs, not plain maps. Webhook
payloads are plain maps; reserve struct patterns for your own domain types.

**5. `=` also does map pattern matching**
`%{data: d} = payload` raises `MatchError` if `:data` is missing. Useful
for fail-fast extraction when the key is guaranteed; dangerous otherwise.

## When NOT to use map patterns

- When you need ALL keys to match exactly (use a struct or compare keyset).
- When the field is optional and its absence is normal (use `Map.get/3`).
- In hot loops where the pattern checks many keys — consider a single guard
  with `Map.has_key?/2` if profiling shows it matters.

---

## Resources

- [Map pattern matching — Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html#maps)
- [Map — HexDocs](https://hexdocs.pm/elixir/Map.html)
- [Jason keys option](https://hexdocs.pm/jason/Jason.html#decode/2-options)
- [Patterns and guards](https://hexdocs.pm/elixir/patterns-and-guards.html)
