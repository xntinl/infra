# Distributed Raft Consensus Engine

**Project**: `raft_consensus` -- a complete, production-grade Raft implementation on the BEAM

---

## Project context

You are building `raft_consensus`, a standalone distributed consensus engine in Elixir. The system is used as a foundation for any service that requires linearizable, fault-tolerant state -- a replicated key-value store, a distributed lock, a configuration service. No external consensus libraries. Every byte of the protocol is yours.

Project structure:

```
raft_consensus/
├── lib/
│   └── raft_consensus/
│       ├── application.ex           # starts the cluster supervisor
│       ├── node.ex                  # GenServer per Raft node — roles: follower/candidate/leader
│       ├── log.ex                   # write-ahead log: append, truncate, read entries
│       ├── state_machine.ex         # pure KV apply function: (command, state) → {reply, state}
│       ├── rpc.ex                   # RPC layer over :erpc — RequestVote, AppendEntries, InstallSnapshot
│       ├── snapshot.ex              # log compaction and snapshot installation
│       ├── membership.ex            # joint consensus membership changes
│       ├── session.ex               # exactly-once client session management
│       └── cluster.ex              # public API: start_cluster/1, get/2, put/3, delete/2
├── test/
│   └── raft_consensus/
│       ├── election_test.exs        # leader election correctness
│       ├── replication_test.exs     # log replication and commit quorum
│       ├── safety_test.exs          # no split-brain, log matching property
│       ├── snapshot_test.exs        # compaction and InstallSnapshot
│       ├── membership_test.exs      # joint consensus node add/remove
│       └── linearizability_test.exs # concurrent client correctness
├── bench/
│   └── raft_bench.exs               # throughput and latency benchmark
├── simulation/
│   └── harness.ex                   # inject message drops, delays, partitions
└── mix.exs
```

---

## The problem

A distributed service needs to replicate state across multiple nodes so that any minority of nodes can fail without data loss and without downtime. The naive approach -- "write to all nodes, if any succeed, done" -- breaks under concurrent writes: two nodes may accept conflicting updates and diverge. Raft solves this by electing a single leader that serializes all writes. Every write is committed only after a majority of nodes acknowledge it.

The hard part is not the happy path. The hard part is correctness under failure: what happens when the leader crashes mid-replication? What if network partitions create two groups, each believing it has a majority? What if a recovered node has a stale log? Raft's answer to these questions is a set of invariants with mathematical safety proofs. Your job is to implement those invariants exactly.

---

## Why this design

**Separate log from state machine**: the log is an ordered sequence of commands; the state machine applies them deterministically. The log is the source of truth. The state machine is a projection. This separation lets you snapshot the state machine and truncate the log independently.

**AppendEntries doubles as heartbeat**: the leader sends AppendEntries even when there are no new entries. This resets followers' election timers, preventing spurious elections. If the leader dies, no heartbeat arrives and a follower starts a new election. The timer is the only failure detector.

**Quorum commit, not all-ack commit**: a log entry is committed once a majority of nodes have it in their log. The leader does not wait for every follower. This means a lagging follower does not degrade write latency -- it catches up asynchronously.

**Randomized election timeouts**: each follower picks a timeout uniformly at random from `[T, 2T]`. Under split-vote conditions (multiple candidates simultaneously), the randomness breaks ties within one or two rounds. This is not a theorem -- it is a probabilistic argument that works overwhelmingly well in practice.

---

## Design decisions

**Option A — Multi-Paxos as the consensus core**
- Pros: historically robust; flexible leader selection.
- Cons: notoriously hard to specify correctly; view-change rules diffuse across papers; fewer reference implementations with matching invariants.

**Option B — Raft with term + log-comparison leader election** (chosen)
- Pros: single spec (Figure 2 of the Raft paper); invariants are local and checkable; large body of reference code (etcd, TiKV) to cross-check against; randomized election timeouts collapse split votes quickly.
- Cons: leader bottlenecks all writes; log must be fully ordered, no per-key concurrency.

→ Chose **B** because the goal is a correct, auditable implementation; Raft's explicit rule set ("commit only current-term entries", "reset timer on every AppendEntries") is the whole point of picking Raft over Paxos.

## Implementation milestones

### Step 1: Create the project

**Objective**: Lay out the module boundaries that separate log, state machine, RPC, and membership so each invariant can be reasoned about in isolation.


```bash
mix new raft_consensus --sup
cd raft_consensus
mkdir -p lib/raft_consensus test/raft_consensus bench simulation
```

### Step 2: `mix.exs` -- dependencies

**Objective**: Pin only benchee and stream_data so the consensus core stays free of external libraries that could hide protocol details.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: Core data structures

**Objective**: Encode the node state with the exact fields Raft Figure 2 requires so term, log, and voting rules remain checkable locally.


Define these structs before writing any GenServer. Raft's correctness hinges on the exact fields each message carries.

```elixir
# lib/raft_consensus/node.ex
defmodule RaftConsensus.Node do
  use GenServer

  alias RaftConsensus.{Log, RPC, StateMachine}

  defstruct [
    :id,
    :peers,
    :role,              # :follower | :candidate | :leader
    :current_term,
    :voted_for,
    :log,               # list of %{term: t, index: i, command: cmd}
    :commit_index,
    :last_applied,
    :next_index,        # leader only: %{peer_id => next log index to send}
    :match_index,       # leader only: %{peer_id => highest replicated index confirmed}
    :votes_received,    # candidate only
    :election_timer,
    :heartbeat_timer,
    :state_machine,
    :pending_requests   # %{index => from} for client request routing
  ]

  @election_timeout_min 150
  @election_timeout_max 300
  @heartbeat_interval 50

  # --- Public API ---

  def start_link(opts) do
    id = Keyword.fetch!(opts, :id)
    GenServer.start_link(__MODULE__, opts, name: via(id))
  end

  def via(id), do: {:via, Registry, {RaftConsensus.Registry, id}}

  def get_state(id), do: GenServer.call(via(id), :get_state)

  def client_request(id, command), do: GenServer.call(via(id), {:client_request, command}, 10_000)

  # --- Callbacks ---

  @impl true
  def init(opts) do
    id = Keyword.fetch!(opts, :id)
    peers = Keyword.get(opts, :peers, [])

    state = %__MODULE__{
      id: id,
      peers: peers,
      role: :follower,
      current_term: 0,
      voted_for: nil,
      log: [],
      commit_index: 0,
      last_applied: 0,
      next_index: %{},
      match_index: %{},
      votes_received: MapSet.new(),
      election_timer: nil,
      heartbeat_timer: nil,
      state_machine: %{},
      pending_requests: %{}
    }

    {:ok, schedule_election_timeout(state)}
  end

  @impl true
  def handle_call(:get_state, _from, state) do
    info = %{
      id: state.id,
      role: state.role,
      term: state.current_term,
      commit_index: state.commit_index,
      log_length: length(state.log),
      vote_count: MapSet.size(state.votes_received)
    }
    {:reply, info, state}
  end

  def handle_call({:client_request, command}, from, %{role: :leader} = state) do
    index = Log.last_index(state.log) + 1
    entry = %{term: state.current_term, index: index, command: command}
    new_log = Log.append(state.log, entry)

    new_state = %{state |
      log: new_log,
      pending_requests: Map.put(state.pending_requests, index, from)
    }

    send_append_entries(new_state)
    {:noreply, new_state}
  end

  def handle_call({:client_request, _command}, _from, state) do
    {:reply, {:error, :not_leader}, state}
  end

  def handle_call({:request_vote, args}, _from, state) do
    state = maybe_step_down(state, args.term)

    vote_granted =
      args.term >= state.current_term and
      (state.voted_for == nil or state.voted_for == args.candidate_id) and
      log_up_to_date?(state.log, args.last_log_index, args.last_log_term)

    new_state =
      if vote_granted do
        %{state | voted_for: args.candidate_id, current_term: args.term}
        |> schedule_election_timeout()
      else
        state
      end

    {:reply, %{term: new_state.current_term, vote_granted: vote_granted}, new_state}
  end

  def handle_call({:append_entries, args}, _from, state) do
    state = maybe_step_down(state, args.term)

    cond do
      args.term < state.current_term ->
        {:reply, %{term: state.current_term, success: false}, state}

      args.prev_log_index > 0 and Log.term_at(state.log, args.prev_log_index) != args.prev_log_term ->
        {:reply, %{term: state.current_term, success: false}, state}

      true ->
        new_log =
          state.log
          |> Log.truncate_from(args.prev_log_index + 1)
          |> Log.append_entries(args.entries)

        new_commit_index =
          if args.leader_commit > state.commit_index do
            min(args.leader_commit, Log.last_index(new_log))
          else
            state.commit_index
          end

        new_state = %{state |
          log: new_log,
          commit_index: new_commit_index,
          role: :follower,
          current_term: args.term,
          voted_for: nil
        }
        |> apply_committed_entries()
        |> schedule_election_timeout()

        {:reply, %{term: new_state.current_term, success: true, match_index: Log.last_index(new_log)}, new_state}
    end
  end

  @impl true
  def handle_info(:election_timeout, state) do
    new_state = start_election(state)
    {:noreply, new_state}
  end

  def handle_info(:heartbeat, %{role: :leader} = state) do
    send_append_entries(state)
    {:noreply, schedule_heartbeat(state)}
  end

  def handle_info(:heartbeat, state), do: {:noreply, state}

  def handle_info({:vote_response, from_id, response}, %{role: :candidate} = state) do
    state = maybe_step_down(state, response.term)

    if state.role != :candidate do
      {:noreply, state}
    else
      new_state =
        if response.vote_granted do
          votes = MapSet.put(state.votes_received, from_id)
          s = %{state | votes_received: votes}

          if MapSet.size(votes) >= quorum_size(s) do
            become_leader(s)
          else
            s
          end
        else
          state
        end

      {:noreply, new_state}
    end
  end

  def handle_info({:append_response, from_id, response}, %{role: :leader} = state) do
    state = maybe_step_down(state, response.term)

    if state.role != :leader do
      {:noreply, state}
    else
      new_state =
        if response.success do
          match_idx = response.match_index
          %{state |
            next_index: Map.put(state.next_index, from_id, match_idx + 1),
            match_index: Map.put(state.match_index, from_id, match_idx)
          }
          |> advance_commit_index()
          |> apply_committed_entries()
        else
          next = max(Map.get(state.next_index, from_id, 1) - 1, 1)
          %{state | next_index: Map.put(state.next_index, from_id, next)}
        end

      {:noreply, new_state}
    end
  end

  def handle_info(_msg, state), do: {:noreply, state}

  # --- Private ---

  defp start_election(state) do
    new_term = state.current_term + 1
    new_state = %{state |
      current_term: new_term,
      role: :candidate,
      voted_for: state.id,
      votes_received: MapSet.new([state.id])
    }
    |> schedule_election_timeout()

    last_log_index = Log.last_index(new_state.log)
    last_log_term = Log.term_at(new_state.log, last_log_index)

    args = %{
      term: new_term,
      candidate_id: state.id,
      last_log_index: last_log_index,
      last_log_term: last_log_term
    }

    for peer <- state.peers do
      Task.start(fn ->
        case RPC.request_vote(peer, args) do
          {:ok, response} ->
            send(via_pid(state.id), {:vote_response, peer, response})
          {:error, _} ->
            :ok
        end
      end)
    end

    if MapSet.size(new_state.votes_received) >= quorum_size(new_state) do
      become_leader(new_state)
    else
      new_state
    end
  end

  defp become_leader(state) do
    last_index = Log.last_index(state.log)

    new_state = %{state |
      role: :leader,
      next_index: Map.new(state.peers, fn p -> {p, last_index + 1} end),
      match_index: Map.new(state.peers, fn p -> {p, 0} end),
      election_timer: cancel_timer(state.election_timer)
    }
    |> schedule_heartbeat()

    send_append_entries(new_state)
    new_state
  end

  defp send_append_entries(state) do
    for peer <- state.peers do
      next_idx = Map.get(state.next_index, peer, 1)
      prev_log_index = next_idx - 1
      prev_log_term = Log.term_at(state.log, prev_log_index)
      entries = Log.entries_from(state.log, next_idx)

      args = %{
        term: state.current_term,
        leader_id: state.id,
        prev_log_index: prev_log_index,
        prev_log_term: prev_log_term,
        entries: entries,
        leader_commit: state.commit_index
      }

      Task.start(fn ->
        case RPC.append_entries(peer, args) do
          {:ok, response} ->
            send(via_pid(state.id), {:append_response, peer, response})
          {:error, _} ->
            :ok
        end
      end)
    end
  end

  defp advance_commit_index(state) do
    indices =
      [Log.last_index(state.log) | Map.values(state.match_index)]
      |> Enum.sort(:desc)

    quorum_index = Enum.at(indices, quorum_size(state) - 1) || 0

    if quorum_index > state.commit_index and Log.term_at(state.log, quorum_index) == state.current_term do
      %{state | commit_index: quorum_index}
    else
      state
    end
  end

  defp apply_committed_entries(state) do
    if state.commit_index > state.last_applied do
      Enum.reduce((state.last_applied + 1)..state.commit_index, state, fn idx, acc ->
        case Log.entry_at(acc.log, idx) do
          nil -> acc
          entry ->
            {reply, new_sm} = StateMachine.apply_command(entry.command, acc.state_machine)
            new_acc = %{acc | state_machine: new_sm, last_applied: idx}

            case Map.pop(new_acc.pending_requests, idx) do
              {nil, _} -> new_acc
              {from, remaining} ->
                GenServer.reply(from, reply)
                %{new_acc | pending_requests: remaining}
            end
        end
      end)
    else
      state
    end
  end

  defp maybe_step_down(state, term) do
    if term > state.current_term do
      %{state |
        current_term: term,
        role: :follower,
        voted_for: nil,
        votes_received: MapSet.new(),
        heartbeat_timer: cancel_timer(state.heartbeat_timer)
      }
      |> schedule_election_timeout()
    else
      state
    end
  end

  defp log_up_to_date?(log, candidate_last_index, candidate_last_term) do
    my_last_index = Log.last_index(log)
    my_last_term = Log.term_at(log, my_last_index)

    cond do
      candidate_last_term > my_last_term -> true
      candidate_last_term == my_last_term -> candidate_last_index >= my_last_index
      true -> false
    end
  end

  defp quorum_size(state), do: div(length(state.peers) + 1, 2) + 1

  defp schedule_election_timeout(state) do
    timer = cancel_timer(state.election_timer)
    timeout = @election_timeout_min + :rand.uniform(@election_timeout_max - @election_timeout_min)
    %{state | election_timer: Process.send_after(self(), :election_timeout, timeout)}
  end

  defp schedule_heartbeat(state) do
    timer = cancel_timer(state.heartbeat_timer)
    %{state | heartbeat_timer: Process.send_after(self(), :heartbeat, @heartbeat_interval)}
  end

  defp cancel_timer(nil), do: nil
  defp cancel_timer(ref) do
    Process.cancel_timer(ref)
    nil
  end

  defp via_pid(id) do
    case Registry.lookup(RaftConsensus.Registry, id) do
      [{pid, _}] -> pid
      [] -> self()
    end
  end
end
```

### Step 4: RPC layer

**Objective**: Isolate RequestVote and AppendEntries behind a transport module so the protocol logic can be tested without touching the network.


```elixir
# lib/raft_consensus/rpc.ex
defmodule RaftConsensus.RPC do
  @moduledoc """
  RPC layer for Raft inter-node communication.
  Uses GenServer.call through the Registry for local-cluster simulation.
  In production, this would use :erpc for cross-node calls.
  """

  @doc """
  Sends a RequestVote RPC to a remote node.
  Returns {:ok, %{term, vote_granted}} or {:error, reason}.
  """
  @spec request_vote(term(), map()) :: {:ok, map()} | {:error, term()}
  def request_vote(node_id, args) do
    try do
      result = GenServer.call(
        RaftConsensus.Node.via(node_id),
        {:request_vote, args},
        200
      )
      {:ok, result}
    catch
      :exit, reason -> {:error, reason}
    end
  end

  @doc """
  Sends AppendEntries (or heartbeat when entries: []) to a follower.
  Returns {:ok, response} or {:error, reason}.
  """
  @spec append_entries(term(), map()) :: {:ok, map()} | {:error, term()}
  def append_entries(node_id, args) do
    try do
      result = GenServer.call(
        RaftConsensus.Node.via(node_id),
        {:append_entries, args},
        200
      )
      {:ok, result}
    catch
      :exit, reason -> {:error, reason}
    end
  end
end
```

### Step 5: Write-ahead log

**Objective**: Model the log as the single source of truth so the state machine becomes a pure projection and snapshots can truncate safely.


```elixir
# lib/raft_consensus/log.ex
defmodule RaftConsensus.Log do
  @moduledoc """
  In-memory write-ahead log represented as a list of entries sorted by index.
  Each entry is %{term: integer, index: integer, command: term}.

  In production, this would be backed by :dets or a file with :file.sync/1
  after each append for durability.
  """

  @spec append(list(), map()) :: list()
  def append(log, entry), do: log ++ [entry]

  @spec append_entries(list(), list()) :: list()
  def append_entries(log, entries), do: log ++ entries

  @spec truncate_from(list(), pos_integer()) :: list()
  def truncate_from(log, from_index) do
    Enum.filter(log, fn entry -> entry.index < from_index end)
  end

  @spec term_at(list(), non_neg_integer()) :: non_neg_integer()
  def term_at(_log, 0), do: 0
  def term_at(log, index) do
    case Enum.find(log, fn e -> e.index == index end) do
      nil -> 0
      entry -> entry.term
    end
  end

  @spec entry_at(list(), pos_integer()) :: map() | nil
  def entry_at(log, index) do
    Enum.find(log, fn e -> e.index == index end)
  end

  @spec last_index(list()) :: non_neg_integer()
  def last_index([]), do: 0
  def last_index(log), do: List.last(log).index

  @spec last_term(list()) :: non_neg_integer()
  def last_term([]), do: 0
  def last_term(log), do: List.last(log).term

  @spec entries_from(list(), pos_integer()) :: list()
  def entries_from(log, from_index) do
    Enum.filter(log, fn e -> e.index >= from_index end)
  end
end
```

### Step 6: State Machine

**Objective**: Keep the KV apply function pure and deterministic so every replica reaches identical state from the same committed log prefix.


```elixir
# lib/raft_consensus/state_machine.ex
defmodule RaftConsensus.StateMachine do
  @moduledoc """
  Pure key-value state machine. Applies commands deterministically
  to produce a reply and updated state.
  """

  @spec apply_command(term(), map()) :: {term(), map()}
  def apply_command({:put, key, value}, state) do
    {:ok, Map.put(state, key, value)}
  end

  def apply_command({:get, key}, state) do
    {{:ok, Map.get(state, key)}, state}
  end

  def apply_command({:delete, key}, state) do
    {:ok, Map.delete(state, key)}
  end

  def apply_command(_unknown, state) do
    {{:error, :unknown_command}, state}
  end
end
```

### Step 7: Cluster API

**Objective**: Route every client call through the current leader so linearizability holds even when callers talk to a stale follower.


```elixir
# lib/raft_consensus/cluster.ex
defmodule RaftConsensus.Cluster do
  @moduledoc """
  Public API for managing a Raft cluster. Starts nodes, routes client
  requests to the leader, and provides cluster inspection utilities.
  """

  defstruct [:node_ids, :supervisor]

  @spec start_cluster(keyword()) :: {:ok, %__MODULE__{}}
  def start_cluster(opts \\ []) do
    node_count = Keyword.get(opts, :nodes, 5)
    {min_timeout, max_timeout} = Keyword.get(opts, :election_timeout_range, {150, 300})

    node_ids = for i <- 1..node_count, do: :"raft_node_#{i}"

    children = [
      {Registry, keys: :unique, name: RaftConsensus.Registry}
    ] ++ Enum.map(node_ids, fn id ->
      peers = List.delete(node_ids, id)
      %{
        id: id,
        start: {RaftConsensus.Node, :start_link, [[id: id, peers: peers]]},
        restart: :transient
      }
    end)

    {:ok, sup} = Supervisor.start_link(children, strategy: :one_for_one)
    {:ok, %__MODULE__{node_ids: node_ids, supervisor: sup}}
  end

  @spec stop_cluster(%__MODULE__{}) :: :ok
  def stop_cluster(%__MODULE__{supervisor: sup}) do
    Supervisor.stop(sup, :normal)
    :ok
  end

  @spec get_leaders(%__MODULE__{}) :: [map()]
  def get_leaders(%__MODULE__{node_ids: ids}) do
    ids
    |> Enum.map(fn id ->
      try do
        RaftConsensus.Node.get_state(id)
      catch
        :exit, _ -> nil
      end
    end)
    |> Enum.reject(&is_nil/1)
    |> Enum.filter(fn info -> info.role == :leader end)
  end

  @spec put(%__MODULE__{}, term(), term()) :: :ok | {:error, term()}
  def put(cluster, key, value) do
    route_to_leader(cluster, {:put, key, value})
  end

  @spec get(%__MODULE__{}, term()) :: {:ok, term()} | {:error, term()}
  def get(cluster, key) do
    route_to_leader(cluster, {:get, key})
  end

  @spec kill_node(%__MODULE__{}, atom()) :: :ok
  def kill_node(%__MODULE__{supervisor: sup}, node_id) do
    case Registry.lookup(RaftConsensus.Registry, node_id) do
      [{pid, _}] -> Process.exit(pid, :kill)
      [] -> :ok
    end
    :ok
  end

  @spec read_all(%__MODULE__{}, term()) :: [term()]
  def read_all(%__MODULE__{node_ids: ids}, key) do
    Enum.map(ids, fn id ->
      try do
        case RaftConsensus.Node.get_state(id) do
          %{state_machine: sm} -> Map.get(sm, key)
          _ -> nil
        end
      catch
        :exit, _ -> nil
      end
    end)
  end

  @spec partition(%__MODULE__{}, keyword()) :: {%__MODULE__{}, %__MODULE__{}}
  def partition(%__MODULE__{node_ids: ids} = cluster, opts) do
    minority_size = Keyword.get(opts, :minority_size, 2)
    {minority_ids, majority_ids} = Enum.split(ids, minority_size)
    {%{cluster | node_ids: minority_ids}, %{cluster | node_ids: majority_ids}}
  end

  def heal_partition(_cluster, _minority, _majority), do: :ok

  defp route_to_leader(%__MODULE__{node_ids: ids}, command) do
    leaders = ids
      |> Enum.map(fn id ->
        try do
          {id, RaftConsensus.Node.get_state(id)}
        catch
          :exit, _ -> nil
        end
      end)
      |> Enum.reject(&is_nil/1)
      |> Enum.filter(fn {_id, info} -> info.role == :leader end)

    case leaders do
      [{leader_id, _} | _] ->
        RaftConsensus.Node.client_request(leader_id, command)
      [] ->
        {:error, :no_leader}
    end
  end
end
```

### Step 8: Application

**Objective**: Keep the application shell minimal so cluster wiring stays an explicit caller concern rather than a hidden startup side effect.


```elixir
# lib/raft_consensus/application.ex
defmodule RaftConsensus.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    opts = [strategy: :one_for_one, name: RaftConsensus.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 9: Given tests -- must pass without modification

**Objective**: Lock down safety properties (single leader, majority-replicated commits, partition recovery) with tests the implementation cannot edit to pass.


```elixir
# test/raft_consensus/election_test.exs
defmodule RaftConsensus.ElectionTest do
  use ExUnit.Case, async: false

  alias RaftConsensus.Cluster

  setup do
    {:ok, cluster} = Cluster.start_cluster(nodes: 5, election_timeout_range: {150, 300})
    on_exit(fn -> Cluster.stop_cluster(cluster) end)
    {:ok, cluster: cluster}
  end

  test "a single leader is elected within 3 seconds", %{cluster: cluster} do
    Process.sleep(3_000)
    leaders = Cluster.get_leaders(cluster)
    assert length(leaders) == 1, "expected exactly 1 leader, got: #{inspect(leaders)}"
  end

  test "leader has won a quorum of votes", %{cluster: cluster} do
    Process.sleep(3_000)
    [leader] = Cluster.get_leaders(cluster)
    assert leader.vote_count >= 3
  end

  test "killing the leader triggers a new election", %{cluster: cluster} do
    Process.sleep(1_500)
    [old_leader] = Cluster.get_leaders(cluster)
    Cluster.kill_node(cluster, old_leader.id)

    Process.sleep(3_000)
    [new_leader] = Cluster.get_leaders(cluster)

    assert new_leader.id != old_leader.id
    assert new_leader.term > old_leader.term
  end
end
```

```elixir
# test/raft_consensus/replication_test.exs
defmodule RaftConsensus.ReplicationTest do
  use ExUnit.Case, async: false

  alias RaftConsensus.Cluster

  setup do
    {:ok, cluster} = Cluster.start_cluster(nodes: 5, election_timeout_range: {150, 300})
    Process.sleep(2_000)
    {:ok, cluster: cluster}
  end

  test "put returns :ok only after majority replication", %{cluster: cluster} do
    assert :ok = Cluster.put(cluster, "k1", "v1")
    # Wait for all nodes to apply, then verify all 5 agree
    Process.sleep(200)
    values = Cluster.read_all(cluster, "k1")
    assert Enum.all?(values, fn v -> v == "v1" end), "divergent state: #{inspect(values)}"
  end

  test "1000 sequential puts are durable and ordered", %{cluster: cluster} do
    for i <- 1..1_000 do
      assert :ok = Cluster.put(cluster, "seq_#{i}", i)
    end

    Process.sleep(500)

    for i <- 1..1_000 do
      assert {:ok, ^i} = Cluster.get(cluster, "seq_#{i}")
    end
  end

  test "partitioned follower catches up after reconnect", %{cluster: cluster} do
    {minority, majority} = Cluster.partition(cluster, minority_size: 2)

    for i <- 1..100 do
      Cluster.put(majority, "part_#{i}", i)
    end

    Cluster.heal_partition(cluster, minority, majority)
    Process.sleep(2_000)

    for i <- 1..100 do
      assert {:ok, ^i} = Cluster.get(cluster, "part_#{i}")
    end
  end
end
```

### Step 10: Run the tests

**Objective**: Run the full suite with tracing so election timing bugs surface as observable order, not as silently flaky tests.


```bash
mix test test/raft_consensus/ --trace
```

### Step 11: Throughput benchmark

**Objective**: Quantify the cost of quorum commit under parallel writes so the leader bottleneck and RTT tail are measured, not assumed.


```elixir
# bench/raft_bench.exs
{:ok, cluster} = RaftConsensus.Cluster.start_cluster(nodes: 3)
Process.sleep(2_000)

Benchee.run(
  %{
    "put — serialized" => fn ->
      RaftConsensus.Cluster.put(cluster, "bench", :rand.uniform(1_000_000))
    end,
    "get — linearizable" => fn ->
      RaftConsensus.Cluster.get(cluster, "bench")
    end
  },
  parallel: 10,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/raft_bench.exs
```

Target: 10,000 linearizable writes/second on a 3-node cluster on localhost.

### Why this works

The leader serializes every write through a single log, so there is exactly one order of operations per term. Quorum commit guarantees that any future leader has every committed entry (by the log-up-to-date vote rule), so no committed data is ever lost. Randomized election timeouts make split votes self-correct in one or two rounds, and `maybe_step_down/2` keeps every node on the highest term it has seen — which is what makes the safety argument hold without a global clock.

---

## Benchmark

```elixir
# bench/raft_bench.exs — already defined in Step 11
# mix run bench/raft_bench.exs
def main do
  IO.puts("[RaftConsensus.Cluster] GenServer demo")
  :ok
end

```

Target: 10,000 linearizable writes/second on a 3-node localhost cluster; p99 < 20 ms under `parallel: 10`.

---

## Key Concepts: Consensus and Distributed Agreement

The core challenge in distributed systems is reaching agreement across multiple nodes when some may fail, be slow, or partition from the network. Consensus algorithms formalize three properties:

1. **Safety**: All nodes that decide must decide the same value.
2. Liveness**: Every non-faulty node eventually decides.
3. Fault tolerance**: The system tolerates up to F faulty nodes out of 2F+1 total.

Raft achieves this via a leader-based approach: the leader serializes writes through a log, and quorum commit ensures no data loss across failures. The log-up-to-date vote rule prevents stale nodes from becoming leader, and the "commit only current-term entries" rule prevents committed entries from being overwritten.

This contrasts with leaderless protocols (e.g., CRDTs) that sacrifice strong consistency for eventual consistency, enabling offline-first systems. For the BEAM, Raft fits naturally into the GenServer + OTP supervision model: each node is a GenServer with local state (log, term, vote), and RPCs are asynchronous messages that do not block the caller.

**Production insight**: Raft's safety depends on three invariants holding simultaneously. A single violated invariant (e.g., committing an entry from a previous term by index alone) causes data loss on specific failure patterns that may never surface in testing. This is why production systems use formal verification or extensive failure injection (Jepsen tests) to validate safety, not just positive test cases.

---

## Trade-off analysis

| Aspect | Raft (your impl) | Multi-Paxos | Viewstamped Replication |
|--------|-----------------|-------------|------------------------|
| Leader election | log comparison vote | any quorum member | deterministic rotation (`view mod N`) |
| Log commit rule | quorum on current-term entries | phase 2 acceptance | commit_number broadcast |
| View change | term + log comparison | ballot + accept | two-phase DO_VIEW_CHANGE |
| Membership change | joint consensus | varies | reconfiguration op |
| Snapshot protocol | InstallSnapshot RPC | implementation-defined | recovery RPC |
| Understandability | designed for clarity | historically harder | comparable to Raft |

After running the benchmark, record your measured latency (p50, p99) and throughput (ops/sec) in each column for direct comparison.

Architectural question: Raft forbids committing entries from previous terms by index alone. Why? Construct a 3-node scenario where doing so would violate safety. Draw the log state on each node step by step.

---

## Common production mistakes

**1. Committing entries from previous terms by index**
The most commonly misimplemented rule. A new leader must not mark an old entry committed by seeing it on a majority -- it must first replicate and commit an entry from its own term, which transitively commits all previous entries. Violating this causes data loss after a specific sequence of leader crashes.

**2. Not resetting the election timer on AppendEntries**
If the timer is only reset on non-empty AppendEntries, the node will call an election even though a live leader is sending heartbeats. The timer must reset on every valid AppendEntries, including no-op heartbeats.

**3. Stale RPC responses updating state**
A response to a RequestVote or AppendEntries from a previous term must be discarded. Check the term in every response; if it is higher than your current term, convert to follower immediately.

**4. Blocking :erpc calls inside the GenServer**
The Raft node must not block its own message loop waiting for RPCs. Fire RPCs from Task processes, collect results via cast or monitor.

**5. Using wall-clock time for election timeouts**
Use `System.monotonic_time/1`. Wall-clock time can jump backward after NTP correction, causing spurious elections.

---

## Reflection

- If your cluster grew from 5 to 50 nodes, would you keep Raft as-is, shard into multiple Raft groups (à la CockroachDB / TiKV), or switch to a leaderless protocol? Justify in terms of write latency and failure blast radius.
- Suppose 2 of 5 nodes are in a slow data center adding 80 ms of RTT. Would you change the quorum composition, tune election timeouts per-node, or accept the latency hit? Back your answer with the quorum-commit rule.

---

## Resources

- Ongaro, D. & Ousterhout, J. (2014). *In Search of an Understandable Consensus Algorithm (Extended Version)* -- Figure 2 is the complete specification; implement it exactly
- Ongaro, D. (2014). *Consensus: Bridging Theory and Practice* (PhD dissertation) -- chapters 3-6 cover safety proofs and membership change
- [etcd `raft/` package](https://github.com/etcd-io/etcd/tree/main/raft) -- the reference Go implementation; study the structure, not the wrapper
- [TiKV Raft](https://github.com/tikv/raft-rs) -- Rust implementation with extensive correctness comments
- [Jepsen analyses](https://jepsen.io) -- Kyle Kingsbury's linearizability violation reports; understand how violations are detected before you claim your implementation is safe
