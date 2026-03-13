# 42. Async Channels and Actor Model

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 02 (message passing with `std::sync::mpsc`)
- Completed: exercise 03 (shared state concurrency with `Arc`, `Mutex`)
- Understanding of enums, pattern matching, and ownership semantics

## Learning Objectives

- Use `tokio::sync::mpsc`, `oneshot`, `broadcast`, and `watch` channels for inter-task communication
- Implement the actor pattern: encapsulate state behind a message-processing loop
- Build request-response patterns using `oneshot` channels embedded in message enums
- Design supervision trees where actors monitor and restart child actors
- Understand backpressure semantics of bounded channels
- Build an actor-based chat server that demonstrates all channel types

## Concepts

### Tokio Channel Types

Tokio provides four channel types, each optimized for a different communication pattern:

| Channel | Pattern | Senders | Receivers | Buffering |
|---------|---------|---------|-----------|-----------|
| `mpsc` | Many-to-one | Multiple (`Sender` is `Clone`) | Single (`Receiver`) | Bounded or unbounded |
| `oneshot` | One-to-one | Single (`Sender`) | Single (`Receiver`) | One value |
| `broadcast` | One-to-many | Single (`Sender`, but `Clone`) | Multiple (via `subscribe()`) | Bounded, lossy |
| `watch` | One-to-many (latest value) | Single (`Sender`) | Multiple (`Receiver` is `Clone`) | Single value, always latest |

### mpsc: The Workhorse Channel

`mpsc` (multi-producer, single-consumer) is the primary channel for actor mailboxes:

```rust
use tokio::sync::mpsc;

#[tokio::main]
async fn main() {
    // Bounded channel: capacity of 32 messages
    let (tx, mut rx) = mpsc::channel::<String>(32);

    // Multiple senders (clone the sender)
    let tx2 = tx.clone();

    tokio::spawn(async move {
        tx.send("hello from task 1".to_string()).await.unwrap();
    });

    tokio::spawn(async move {
        tx2.send("hello from task 2".to_string()).await.unwrap();
    });

    // Receive until all senders are dropped
    while let Some(msg) = rx.recv().await {
        println!("Received: {}", msg);
    }
    println!("All senders dropped, channel closed");
}
```

**Backpressure**: When the channel is full (32 messages buffered), `send().await` blocks until the receiver consumes a message. This prevents fast producers from overwhelming slow consumers.

```rust
// Bounded: send blocks when full (backpressure)
let (tx, rx) = mpsc::channel::<i32>(10);

// Unbounded: send never blocks, memory grows without limit
let (tx, rx) = mpsc::unbounded_channel::<i32>();
// tx.send(value) -- note: NOT async, returns immediately
```

**When to use bounded vs unbounded:**
- Bounded (default choice): provides backpressure, prevents OOM
- Unbounded: only when you know the producer rate is bounded by other means (e.g., it is driven by a bounded channel upstream)

### oneshot: Request-Response

`oneshot` channels carry exactly one value. They are used for request-response patterns where the caller needs to await a result:

```rust
use tokio::sync::oneshot;

async fn compute_in_background(input: u64) -> u64 {
    let (tx, rx) = oneshot::channel();

    tokio::spawn(async move {
        // Simulate expensive computation
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        let result = input * input;
        let _ = tx.send(result); // Send result back
    });

    // Await the result
    rx.await.unwrap()
}
```

The key insight: embed the `oneshot::Sender` inside a message enum so the actor can respond to the caller.

### broadcast: Fan-out

`broadcast` sends every message to all active receivers. Late receivers miss messages sent before they subscribed. Slow receivers that fall behind lose messages (with a `RecvError::Lagged` error).

```rust
use tokio::sync::broadcast;

#[tokio::main]
async fn main() {
    let (tx, _) = broadcast::channel::<String>(100);

    let mut rx1 = tx.subscribe();
    let mut rx2 = tx.subscribe();

    tx.send("hello everyone".to_string()).unwrap();

    println!("rx1: {}", rx1.recv().await.unwrap());
    println!("rx2: {}", rx2.recv().await.unwrap());
}
```

Use cases: event notifications, pub/sub, chat rooms.

### watch: Latest-Value Shared State

`watch` holds a single value. Receivers always see the most recent value. Multiple receivers can subscribe. The sender can update the value, and receivers are notified of changes.

```rust
use tokio::sync::watch;

#[tokio::main]
async fn main() {
    let (tx, mut rx) = watch::channel("initial".to_string());

    tokio::spawn(async move {
        loop {
            // Wait for the value to change
            rx.changed().await.unwrap();
            println!("Config updated: {}", *rx.borrow());
        }
    });

    tx.send("updated value 1".to_string()).unwrap();
    tx.send("updated value 2".to_string()).unwrap();

    tokio::time::sleep(std::time::Duration::from_millis(100)).await;
}
```

Use cases: configuration that changes at runtime, shutdown signals, health status.

### The Actor Pattern

An actor is a task that:
1. Owns its state (no shared memory)
2. Receives messages through a channel (its "mailbox")
3. Processes messages sequentially (no concurrency within the actor)
4. Can send messages to other actors

```
                  +-----------+
  msg1 ----+      |           |
  msg2 ----+----> | Actor     | ----> side effects
  msg3 ----+      | (state)   |       (DB writes, responses, messages to other actors)
                  +-----------+
                  mpsc::Receiver
```

This pattern eliminates shared mutable state entirely. Each actor has exclusive ownership of its data. The only way to interact with it is by sending messages.

### Actor Message Types

Define messages as an enum. For request-response, embed a `oneshot::Sender`:

```rust
use tokio::sync::{mpsc, oneshot};

// --- Messages the actor can receive ---

enum AccountMessage {
    // Fire-and-forget: no response needed
    Deposit {
        amount: u64,
    },
    // Request-response: caller awaits the reply
    GetBalance {
        respond_to: oneshot::Sender<u64>,
    },
    // Request-response with Result
    Withdraw {
        amount: u64,
        respond_to: oneshot::Sender<Result<u64, String>>,
    },
}

// --- The actor ---

struct AccountActor {
    receiver: mpsc::Receiver<AccountMessage>,
    balance: u64,
    account_id: String,
}

impl AccountActor {
    fn new(receiver: mpsc::Receiver<AccountMessage>, account_id: String) -> Self {
        Self {
            receiver,
            balance: 0,
            account_id,
        }
    }

    // The actor loop: process messages one at a time
    async fn run(mut self) {
        println!("[{}] Actor started", self.account_id);

        while let Some(msg) = self.receiver.recv().await {
            self.handle_message(msg);
        }

        println!("[{}] Actor stopped (all senders dropped)", self.account_id);
    }

    fn handle_message(&mut self, msg: AccountMessage) {
        match msg {
            AccountMessage::Deposit { amount } => {
                self.balance += amount;
                println!("[{}] Deposited {}. Balance: {}", self.account_id, amount, self.balance);
            }
            AccountMessage::GetBalance { respond_to } => {
                let _ = respond_to.send(self.balance);
            }
            AccountMessage::Withdraw { amount, respond_to } => {
                if amount > self.balance {
                    let _ = respond_to.send(Err(format!(
                        "insufficient funds: have {}, need {}",
                        self.balance, amount
                    )));
                } else {
                    self.balance -= amount;
                    let _ = respond_to.send(Ok(self.balance));
                }
            }
        }
    }
}

// --- The actor handle (client-side API) ---

#[derive(Clone)]
struct AccountHandle {
    sender: mpsc::Sender<AccountMessage>,
}

impl AccountHandle {
    fn new(account_id: String) -> Self {
        let (sender, receiver) = mpsc::channel(64);
        let actor = AccountActor::new(receiver, account_id);
        tokio::spawn(actor.run());
        Self { sender }
    }

    async fn deposit(&self, amount: u64) {
        let _ = self.sender.send(AccountMessage::Deposit { amount }).await;
    }

    async fn get_balance(&self) -> u64 {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .sender
            .send(AccountMessage::GetBalance { respond_to: tx })
            .await;
        rx.await.unwrap()
    }

    async fn withdraw(&self, amount: u64) -> Result<u64, String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .sender
            .send(AccountMessage::Withdraw {
                amount,
                respond_to: tx,
            })
            .await;
        rx.await.unwrap()
    }
}

#[tokio::main]
async fn main() {
    let account = AccountHandle::new("ACC-001".to_string());

    account.deposit(1000).await;
    account.deposit(500).await;

    let balance = account.get_balance().await;
    println!("Balance: {}", balance); // 1500

    match account.withdraw(200).await {
        Ok(new_balance) => println!("Withdrew 200. New balance: {}", new_balance),
        Err(e) => println!("Withdraw failed: {}", e),
    }

    match account.withdraw(5000).await {
        Ok(new_balance) => println!("Withdrew 5000. New balance: {}", new_balance),
        Err(e) => println!("Withdraw failed: {}", e),
    }
}
```

The `AccountHandle` pattern is crucial: it hides the channel mechanics behind a clean async API. Callers never see `mpsc::Sender` or `oneshot::channel` -- they just call `account.deposit(100).await`.

### Actor Supervision

In production, actors can fail. A supervisor actor monitors children and restarts them:

```rust
use tokio::sync::mpsc;
use std::time::Duration;

struct SupervisorMessage {
    child_id: String,
    error: String,
}

struct Supervisor {
    rx: mpsc::Receiver<SupervisorMessage>,
    children: std::collections::HashMap<String, tokio::task::JoinHandle<()>>,
    restart_counts: std::collections::HashMap<String, u32>,
    max_restarts: u32,
}

impl Supervisor {
    fn new(rx: mpsc::Receiver<SupervisorMessage>) -> Self {
        Self {
            rx,
            children: std::collections::HashMap::new(),
            restart_counts: std::collections::HashMap::new(),
            max_restarts: 3,
        }
    }

    async fn run(mut self) {
        while let Some(msg) = self.rx.recv().await {
            let count = self
                .restart_counts
                .entry(msg.child_id.clone())
                .or_insert(0);
            *count += 1;

            if *count > self.max_restarts {
                println!(
                    "[SUPERVISOR] Child {} exceeded max restarts ({}), giving up",
                    msg.child_id, self.max_restarts
                );
                self.children.remove(&msg.child_id);
            } else {
                println!(
                    "[SUPERVISOR] Restarting child {} (attempt {}/{}): {}",
                    msg.child_id, count, self.max_restarts, msg.error
                );
                // In a real system, restart the child actor here
                // self.spawn_child(&msg.child_id);
            }
        }
    }
}
```

### Combining Channel Types

A realistic actor often uses multiple channel types:

```rust
use tokio::sync::{mpsc, broadcast, watch, oneshot};

struct ChatRoom {
    // Incoming commands from clients
    commands: mpsc::Receiver<ChatCommand>,
    // Broadcast messages to all clients
    broadcast_tx: broadcast::Sender<ChatEvent>,
    // Current room status (clients can poll)
    status_tx: watch::Sender<RoomStatus>,
    // Room state
    members: Vec<String>,
}

enum ChatCommand {
    Join {
        username: String,
        respond_to: oneshot::Sender<broadcast::Receiver<ChatEvent>>,
    },
    Leave {
        username: String,
    },
    SendMessage {
        from: String,
        text: String,
    },
    GetStatus {
        respond_to: oneshot::Sender<RoomStatus>,
    },
}

#[derive(Clone, Debug)]
enum ChatEvent {
    UserJoined(String),
    UserLeft(String),
    Message { from: String, text: String },
}

#[derive(Clone, Debug)]
struct RoomStatus {
    member_count: usize,
    members: Vec<String>,
}
```

### Performance Characteristics

| Operation | Approximate cost |
|---|---|
| `mpsc::send` (bounded, space available) | ~50 ns |
| `mpsc::send` (bounded, full) | Suspends until space |
| `oneshot::send` | ~20 ns |
| `broadcast::send` (N receivers) | ~50 ns + ~20 ns per receiver |
| `watch::send` | ~50 ns (single atomic write) |
| `mpsc::recv` (message waiting) | ~30 ns |
| `mpsc::recv` (empty) | Suspends until message |

Actors with `mpsc` mailboxes can handle ~10M messages/sec on a single core. The bottleneck is usually the work done *inside* the actor, not the channel overhead.

---

## Exercises

### Exercise 1: Bank Account Actor System

Build a multi-account banking system using actors:

1. Each account is an actor with its own `mpsc` mailbox
2. An `AccountManager` actor creates and looks up accounts
3. Support: `Deposit`, `Withdraw`, `GetBalance`, `Transfer` (between accounts)
4. `Transfer` must be atomic: debit source and credit destination, rolling back on failure

**Cargo.toml:**

```toml
[package]
name = "actor-bank"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
```

**Hints:**
- The `Transfer` message on the source account can internally send a `Deposit` to the target account
- Use `oneshot` channels for request-response (GetBalance, Withdraw, Transfer)
- The `AccountManager` holds a `HashMap<String, AccountHandle>`
- For atomicity: withdraw first; if successful, deposit to target; there is no rollback needed because withdraw is checked

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;
use tokio::sync::{mpsc, oneshot};

// --- Account Actor ---

enum AccountMsg {
    Deposit {
        amount: u64,
    },
    Withdraw {
        amount: u64,
        respond_to: oneshot::Sender<Result<u64, String>>,
    },
    GetBalance {
        respond_to: oneshot::Sender<u64>,
    },
}

struct AccountActor {
    id: String,
    balance: u64,
    rx: mpsc::Receiver<AccountMsg>,
}

impl AccountActor {
    async fn run(mut self) {
        while let Some(msg) = self.rx.recv().await {
            match msg {
                AccountMsg::Deposit { amount } => {
                    self.balance += amount;
                }
                AccountMsg::Withdraw { amount, respond_to } => {
                    if amount > self.balance {
                        let _ = respond_to.send(Err(format!(
                            "insufficient funds in {}: have {}, need {}",
                            self.id, self.balance, amount
                        )));
                    } else {
                        self.balance -= amount;
                        let _ = respond_to.send(Ok(self.balance));
                    }
                }
                AccountMsg::GetBalance { respond_to } => {
                    let _ = respond_to.send(self.balance);
                }
            }
        }
    }
}

#[derive(Clone)]
struct AccountHandle {
    tx: mpsc::Sender<AccountMsg>,
}

impl AccountHandle {
    fn new(id: String, initial_balance: u64) -> Self {
        let (tx, rx) = mpsc::channel(64);
        let actor = AccountActor {
            id,
            balance: initial_balance,
            rx,
        };
        tokio::spawn(actor.run());
        Self { tx }
    }

    async fn deposit(&self, amount: u64) {
        let _ = self.tx.send(AccountMsg::Deposit { amount }).await;
    }

    async fn withdraw(&self, amount: u64) -> Result<u64, String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(AccountMsg::Withdraw { amount, respond_to: tx })
            .await;
        rx.await.map_err(|_| "actor died".to_string())?
    }

    async fn get_balance(&self) -> u64 {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(AccountMsg::GetBalance { respond_to: tx })
            .await;
        rx.await.unwrap_or(0)
    }
}

// --- Account Manager Actor ---

enum ManagerMsg {
    CreateAccount {
        id: String,
        initial_balance: u64,
        respond_to: oneshot::Sender<Result<(), String>>,
    },
    GetAccount {
        id: String,
        respond_to: oneshot::Sender<Option<AccountHandle>>,
    },
    Transfer {
        from: String,
        to: String,
        amount: u64,
        respond_to: oneshot::Sender<Result<(), String>>,
    },
    ListAccounts {
        respond_to: oneshot::Sender<Vec<String>>,
    },
}

struct ManagerActor {
    rx: mpsc::Receiver<ManagerMsg>,
    accounts: HashMap<String, AccountHandle>,
}

impl ManagerActor {
    async fn run(mut self) {
        while let Some(msg) = self.rx.recv().await {
            match msg {
                ManagerMsg::CreateAccount {
                    id,
                    initial_balance,
                    respond_to,
                } => {
                    if self.accounts.contains_key(&id) {
                        let _ = respond_to.send(Err(format!("account {} already exists", id)));
                    } else {
                        let handle = AccountHandle::new(id.clone(), initial_balance);
                        self.accounts.insert(id, handle);
                        let _ = respond_to.send(Ok(()));
                    }
                }
                ManagerMsg::GetAccount { id, respond_to } => {
                    let _ = respond_to.send(self.accounts.get(&id).cloned());
                }
                ManagerMsg::Transfer {
                    from,
                    to,
                    amount,
                    respond_to,
                } => {
                    let result = self.do_transfer(&from, &to, amount).await;
                    let _ = respond_to.send(result);
                }
                ManagerMsg::ListAccounts { respond_to } => {
                    let ids: Vec<String> = self.accounts.keys().cloned().collect();
                    let _ = respond_to.send(ids);
                }
            }
        }
    }

    async fn do_transfer(&self, from: &str, to: &str, amount: u64) -> Result<(), String> {
        let from_handle = self
            .accounts
            .get(from)
            .ok_or_else(|| format!("source account {} not found", from))?;
        let to_handle = self
            .accounts
            .get(to)
            .ok_or_else(|| format!("destination account {} not found", to))?;

        // Step 1: Withdraw from source
        from_handle.withdraw(amount).await?;

        // Step 2: Deposit to destination (cannot fail)
        to_handle.deposit(amount).await;

        Ok(())
    }
}

#[derive(Clone)]
struct ManagerHandle {
    tx: mpsc::Sender<ManagerMsg>,
}

impl ManagerHandle {
    fn new() -> Self {
        let (tx, rx) = mpsc::channel(64);
        let actor = ManagerActor {
            rx,
            accounts: HashMap::new(),
        };
        tokio::spawn(actor.run());
        Self { tx }
    }

    async fn create_account(&self, id: &str, initial_balance: u64) -> Result<(), String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::CreateAccount {
                id: id.to_string(),
                initial_balance,
                respond_to: tx,
            })
            .await;
        rx.await.map_err(|_| "manager died".to_string())?
    }

    async fn get_account(&self, id: &str) -> Option<AccountHandle> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::GetAccount {
                id: id.to_string(),
                respond_to: tx,
            })
            .await;
        rx.await.ok()?
    }

    async fn transfer(&self, from: &str, to: &str, amount: u64) -> Result<(), String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::Transfer {
                from: from.to_string(),
                to: to.to_string(),
                amount,
                respond_to: tx,
            })
            .await;
        rx.await.map_err(|_| "manager died".to_string())?
    }

    async fn list_accounts(&self) -> Vec<String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::ListAccounts { respond_to: tx })
            .await;
        rx.await.unwrap_or_default()
    }
}

#[tokio::main]
async fn main() {
    let manager = ManagerHandle::new();

    manager.create_account("alice", 1000).await.unwrap();
    manager.create_account("bob", 500).await.unwrap();

    let alice = manager.get_account("alice").await.unwrap();
    let bob = manager.get_account("bob").await.unwrap();

    println!("Alice balance: {}", alice.get_balance().await);
    println!("Bob balance: {}", bob.get_balance().await);

    // Transfer 300 from Alice to Bob
    manager.transfer("alice", "bob", 300).await.unwrap();

    println!("After transfer:");
    println!("Alice balance: {}", alice.get_balance().await);
    println!("Bob balance: {}", bob.get_balance().await);

    // Try to overdraw
    match manager.transfer("bob", "alice", 2000).await {
        Ok(()) => println!("Transfer succeeded"),
        Err(e) => println!("Transfer failed: {}", e),
    }

    println!("Accounts: {:?}", manager.list_accounts().await);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_deposit_and_balance() {
        let handle = AccountHandle::new("test".into(), 0);
        handle.deposit(100).await;
        handle.deposit(50).await;
        assert_eq!(handle.get_balance().await, 150);
    }

    #[tokio::test]
    async fn test_withdraw_success() {
        let handle = AccountHandle::new("test".into(), 100);
        let result = handle.withdraw(30).await;
        assert_eq!(result, Ok(70));
    }

    #[tokio::test]
    async fn test_withdraw_insufficient() {
        let handle = AccountHandle::new("test".into(), 50);
        let result = handle.withdraw(100).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_transfer() {
        let manager = ManagerHandle::new();
        manager.create_account("a", 1000).await.unwrap();
        manager.create_account("b", 0).await.unwrap();

        manager.transfer("a", "b", 400).await.unwrap();

        let a = manager.get_account("a").await.unwrap();
        let b = manager.get_account("b").await.unwrap();
        assert_eq!(a.get_balance().await, 600);
        assert_eq!(b.get_balance().await, 400);
    }

    #[tokio::test]
    async fn test_transfer_insufficient_funds() {
        let manager = ManagerHandle::new();
        manager.create_account("a", 100).await.unwrap();
        manager.create_account("b", 0).await.unwrap();

        let result = manager.transfer("a", "b", 200).await;
        assert!(result.is_err());

        // Source balance should be unchanged
        let a = manager.get_account("a").await.unwrap();
        assert_eq!(a.get_balance().await, 100);
    }

    #[tokio::test]
    async fn test_duplicate_account() {
        let manager = ManagerHandle::new();
        manager.create_account("x", 0).await.unwrap();
        let result = manager.create_account("x", 0).await;
        assert!(result.is_err());
    }
}
```

</details>

### Exercise 2: Actor-Based Chat Server

Build a chat server with these actors:

1. **RoomManager** -- creates and looks up chat rooms
2. **Room** -- manages members and broadcasts messages
3. **Client** -- represents a connected user, receives messages via `broadcast`

Features:
- Join/leave rooms
- Send messages to a room (broadcast to all members)
- List rooms and members
- Room status via `watch` channel (member count updates)

**Hints:**
- Each Room actor holds a `broadcast::Sender<ChatEvent>` for fan-out
- When a client joins, the Room returns a `broadcast::Receiver` via a `oneshot`
- Use `watch` for room status (member count) so clients can poll without asking the actor
- `broadcast::Receiver::recv()` returns `Lagged` when the receiver falls behind -- handle it

<details>
<summary>Solution</summary>

```rust
use std::collections::{HashMap, HashSet};
use tokio::sync::{broadcast, mpsc, oneshot, watch};

// --- Events broadcast to room members ---

#[derive(Clone, Debug)]
enum ChatEvent {
    Message { from: String, text: String },
    UserJoined(String),
    UserLeft(String),
    RoomClosed,
}

#[derive(Clone, Debug)]
struct RoomStatus {
    name: String,
    member_count: usize,
}

// --- Room Actor ---

enum RoomMsg {
    Join {
        username: String,
        respond_to: oneshot::Sender<(broadcast::Receiver<ChatEvent>, watch::Receiver<RoomStatus>)>,
    },
    Leave {
        username: String,
    },
    SendMessage {
        from: String,
        text: String,
    },
    GetMembers {
        respond_to: oneshot::Sender<Vec<String>>,
    },
}

struct RoomActor {
    name: String,
    rx: mpsc::Receiver<RoomMsg>,
    members: HashSet<String>,
    broadcast_tx: broadcast::Sender<ChatEvent>,
    status_tx: watch::Sender<RoomStatus>,
}

impl RoomActor {
    fn new(name: String, rx: mpsc::Receiver<RoomMsg>) -> Self {
        let (broadcast_tx, _) = broadcast::channel(256);
        let (status_tx, _) = watch::channel(RoomStatus {
            name: name.clone(),
            member_count: 0,
        });

        Self {
            name,
            rx,
            members: HashSet::new(),
            broadcast_tx,
            status_tx,
        }
    }

    async fn run(mut self) {
        while let Some(msg) = self.rx.recv().await {
            match msg {
                RoomMsg::Join { username, respond_to } => {
                    self.members.insert(username.clone());
                    self.update_status();
                    let _ = self.broadcast_tx.send(ChatEvent::UserJoined(username));
                    let rx = self.broadcast_tx.subscribe();
                    let status_rx = self.status_tx.subscribe();
                    let _ = respond_to.send((rx, status_rx));
                }
                RoomMsg::Leave { username } => {
                    self.members.remove(&username);
                    self.update_status();
                    let _ = self.broadcast_tx.send(ChatEvent::UserLeft(username));
                }
                RoomMsg::SendMessage { from, text } => {
                    let _ = self.broadcast_tx.send(ChatEvent::Message { from, text });
                }
                RoomMsg::GetMembers { respond_to } => {
                    let members: Vec<String> = self.members.iter().cloned().collect();
                    let _ = respond_to.send(members);
                }
            }
        }
        // Actor shutting down
        let _ = self.broadcast_tx.send(ChatEvent::RoomClosed);
    }

    fn update_status(&self) {
        let _ = self.status_tx.send(RoomStatus {
            name: self.name.clone(),
            member_count: self.members.len(),
        });
    }
}

#[derive(Clone)]
struct RoomHandle {
    tx: mpsc::Sender<RoomMsg>,
}

impl RoomHandle {
    fn new(name: String) -> Self {
        let (tx, rx) = mpsc::channel(64);
        let actor = RoomActor::new(name, rx);
        tokio::spawn(actor.run());
        Self { tx }
    }

    async fn join(
        &self,
        username: &str,
    ) -> Option<(broadcast::Receiver<ChatEvent>, watch::Receiver<RoomStatus>)> {
        let (tx, rx) = oneshot::channel();
        self.tx
            .send(RoomMsg::Join {
                username: username.to_string(),
                respond_to: tx,
            })
            .await
            .ok()?;
        rx.await.ok()
    }

    async fn leave(&self, username: &str) {
        let _ = self
            .tx
            .send(RoomMsg::Leave {
                username: username.to_string(),
            })
            .await;
    }

    async fn send_message(&self, from: &str, text: &str) {
        let _ = self
            .tx
            .send(RoomMsg::SendMessage {
                from: from.to_string(),
                text: text.to_string(),
            })
            .await;
    }

    async fn get_members(&self) -> Vec<String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(RoomMsg::GetMembers { respond_to: tx })
            .await;
        rx.await.unwrap_or_default()
    }
}

// --- Room Manager Actor ---

enum ManagerMsg {
    CreateRoom {
        name: String,
        respond_to: oneshot::Sender<Result<RoomHandle, String>>,
    },
    GetRoom {
        name: String,
        respond_to: oneshot::Sender<Option<RoomHandle>>,
    },
    ListRooms {
        respond_to: oneshot::Sender<Vec<String>>,
    },
}

struct RoomManagerActor {
    rx: mpsc::Receiver<ManagerMsg>,
    rooms: HashMap<String, RoomHandle>,
}

impl RoomManagerActor {
    async fn run(mut self) {
        while let Some(msg) = self.rx.recv().await {
            match msg {
                ManagerMsg::CreateRoom { name, respond_to } => {
                    if self.rooms.contains_key(&name) {
                        let _ = respond_to.send(Err(format!("room '{}' already exists", name)));
                    } else {
                        let handle = RoomHandle::new(name.clone());
                        self.rooms.insert(name, handle.clone());
                        let _ = respond_to.send(Ok(handle));
                    }
                }
                ManagerMsg::GetRoom { name, respond_to } => {
                    let _ = respond_to.send(self.rooms.get(&name).cloned());
                }
                ManagerMsg::ListRooms { respond_to } => {
                    let rooms: Vec<String> = self.rooms.keys().cloned().collect();
                    let _ = respond_to.send(rooms);
                }
            }
        }
    }
}

#[derive(Clone)]
struct RoomManagerHandle {
    tx: mpsc::Sender<ManagerMsg>,
}

impl RoomManagerHandle {
    fn new() -> Self {
        let (tx, rx) = mpsc::channel(64);
        let actor = RoomManagerActor {
            rx,
            rooms: HashMap::new(),
        };
        tokio::spawn(actor.run());
        Self { tx }
    }

    async fn create_room(&self, name: &str) -> Result<RoomHandle, String> {
        let (tx, rx) = oneshot::channel();
        self.tx
            .send(ManagerMsg::CreateRoom {
                name: name.to_string(),
                respond_to: tx,
            })
            .await
            .map_err(|_| "manager died".to_string())?;
        rx.await.map_err(|_| "manager died".to_string())?
    }

    async fn get_room(&self, name: &str) -> Option<RoomHandle> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::GetRoom {
                name: name.to_string(),
                respond_to: tx,
            })
            .await;
        rx.await.ok()?
    }

    async fn list_rooms(&self) -> Vec<String> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .tx
            .send(ManagerMsg::ListRooms { respond_to: tx })
            .await;
        rx.await.unwrap_or_default()
    }
}

#[tokio::main]
async fn main() {
    let manager = RoomManagerHandle::new();

    // Create rooms
    let general = manager.create_room("general").await.unwrap();
    let _rust = manager.create_room("rust").await.unwrap();

    // Users join
    let (mut alice_rx, alice_status) = general.join("alice").await.unwrap();
    let (mut bob_rx, _bob_status) = general.join("bob").await.unwrap();

    // Check status
    println!("Room status: {:?}", *alice_status.borrow());

    // Send messages
    general.send_message("alice", "Hello everyone!").await;
    general.send_message("bob", "Hi Alice!").await;

    // Read messages (drain broadcast)
    for _ in 0..3 {
        // UserJoined(bob), Message from alice, Message from bob
        if let Ok(event) = bob_rx.try_recv() {
            println!("Bob received: {:?}", event);
        }
    }

    // List members
    println!("Members: {:?}", general.get_members().await);
    println!("Rooms: {:?}", manager.list_rooms().await);

    // Leave
    general.leave("bob").await;
    println!("Members after bob left: {:?}", general.get_members().await);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_room_join_and_message() {
        let room = RoomHandle::new("test".into());

        let (mut rx, status) = room.join("alice").await.unwrap();
        let (mut rx2, _) = room.join("bob").await.unwrap();

        // Status should show 2 members
        assert_eq!(status.borrow().member_count, 2);

        room.send_message("alice", "hello").await;

        // Give a moment for broadcast
        tokio::time::sleep(std::time::Duration::from_millis(10)).await;

        // Both should receive the message (bob also gets UserJoined events first)
        let mut found_message = false;
        while let Ok(event) = rx2.try_recv() {
            if matches!(event, ChatEvent::Message { ref from, .. } if from == "alice") {
                found_message = true;
            }
        }
        assert!(found_message);
    }

    #[tokio::test]
    async fn test_room_manager() {
        let manager = RoomManagerHandle::new();

        manager.create_room("room1").await.unwrap();
        manager.create_room("room2").await.unwrap();

        let rooms = manager.list_rooms().await;
        assert_eq!(rooms.len(), 2);

        // Duplicate should fail
        let result = manager.create_room("room1").await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_leave_updates_status() {
        let room = RoomHandle::new("test".into());

        let (_, mut status) = room.join("alice").await.unwrap();
        room.join("bob").await.unwrap();

        // Wait for status update
        let _ = status.changed().await;
        assert_eq!(status.borrow().member_count, 2);

        room.leave("bob").await;

        // Wait for status update
        let _ = status.changed().await;
        assert_eq!(status.borrow().member_count, 1);
    }
}
```

</details>

### Exercise 3: Actor with Supervision and Graceful Shutdown

Build a supervised worker pool where:

1. A `Supervisor` actor spawns N `Worker` actors
2. Workers process jobs from a shared `mpsc` channel
3. If a worker panics, the supervisor detects it and spawns a replacement
4. A shutdown signal (via `watch` channel) causes all workers to finish current work and exit
5. The supervisor waits for all workers to exit before shutting down itself

**Hints:**
- Use `tokio::task::JoinSet` to track worker tasks and detect panics
- `JoinSet::join_next()` returns the result when any task completes
- A `watch::Receiver<bool>` works well as a shutdown signal
- Workers should `tokio::select!` between receiving jobs and the shutdown signal
- Use `tokio::select!` in the supervisor too: monitor `JoinSet` for exits and listen for shutdown

<details>
<summary>Solution</summary>

```rust
use tokio::sync::{mpsc, watch};
use tokio::task::JoinSet;
use std::time::Duration;

struct Job {
    id: u64,
    should_panic: bool,
}

async fn worker(
    worker_id: u32,
    mut jobs: mpsc::Receiver<Job>,
    mut shutdown: watch::Receiver<bool>,
) {
    println!("[Worker {}] Started", worker_id);

    loop {
        tokio::select! {
            job = jobs.recv() => {
                match job {
                    Some(job) => {
                        if job.should_panic {
                            panic!("[Worker {}] Simulated panic on job {}", worker_id, job.id);
                        }
                        println!("[Worker {}] Processing job {}", worker_id, job.id);
                        tokio::time::sleep(Duration::from_millis(50)).await;
                        println!("[Worker {}] Completed job {}", worker_id, job.id);
                    }
                    None => {
                        println!("[Worker {}] Job channel closed", worker_id);
                        break;
                    }
                }
            }
            _ = shutdown.changed() => {
                if *shutdown.borrow() {
                    println!("[Worker {}] Shutdown signal received", worker_id);
                    break;
                }
            }
        }
    }

    println!("[Worker {}] Exiting", worker_id);
}

struct Supervisor {
    num_workers: u32,
    job_tx: mpsc::Sender<Job>,
    job_rx: Option<mpsc::Receiver<Job>>,
    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,
    next_worker_id: u32,
}

impl Supervisor {
    fn new(num_workers: u32) -> (Self, mpsc::Sender<Job>) {
        let (job_tx, job_rx) = mpsc::channel(100);
        let (shutdown_tx, shutdown_rx) = watch::channel(false);

        let supervisor = Self {
            num_workers,
            job_tx: job_tx.clone(),
            job_rx: Some(job_rx),
            shutdown_tx,
            shutdown_rx,
            next_worker_id: 0,
        };

        (supervisor, job_tx)
    }

    async fn run(mut self) {
        println!("[Supervisor] Starting {} workers", self.num_workers);

        // We need to share the job receiver among workers.
        // Use a shared mpsc pattern: create per-worker channels and a dispatcher.
        let mut join_set = JoinSet::new();
        let mut worker_txs: Vec<(u32, mpsc::Sender<Job>)> = Vec::new();

        // Spawn initial workers
        for _ in 0..self.num_workers {
            let (worker_tx, id) = self.spawn_worker(&mut join_set);
            worker_txs.push((id, worker_tx));
        }

        // Dispatch jobs to workers (round-robin)
        let mut job_rx = self.job_rx.take().unwrap();
        let mut worker_idx = 0;

        loop {
            tokio::select! {
                // Dispatch incoming jobs
                job = job_rx.recv() => {
                    match job {
                        Some(job) => {
                            if !worker_txs.is_empty() {
                                let idx = worker_idx % worker_txs.len();
                                let (wid, ref tx) = worker_txs[idx];
                                if tx.send(job).await.is_err() {
                                    println!("[Supervisor] Worker {} channel closed", wid);
                                }
                                worker_idx += 1;
                            }
                        }
                        None => {
                            println!("[Supervisor] Job source closed, initiating shutdown");
                            let _ = self.shutdown_tx.send(true);
                            break;
                        }
                    }
                }
                // Monitor worker exits
                result = join_set.join_next(), if !join_set.is_empty() => {
                    match result {
                        Some(Ok(worker_id)) => {
                            println!("[Supervisor] Worker {} exited normally", worker_id);
                            worker_txs.retain(|(id, _)| *id != worker_id);
                        }
                        Some(Err(err)) => {
                            if err.is_panic() {
                                println!("[Supervisor] A worker panicked, spawning replacement");
                                let (worker_tx, id) = self.spawn_worker(&mut join_set);
                                worker_txs.push((id, worker_tx));
                            }
                        }
                        None => {}
                    }
                }
            }
        }

        // Wait for all workers to finish
        println!("[Supervisor] Waiting for workers to drain...");
        // Drop worker senders to close their channels
        drop(worker_txs);

        while let Some(result) = join_set.join_next().await {
            match result {
                Ok(id) => println!("[Supervisor] Worker {} shut down", id),
                Err(e) => println!("[Supervisor] Worker join error: {:?}", e),
            }
        }

        println!("[Supervisor] All workers stopped. Supervisor exiting.");
    }

    fn spawn_worker(
        &mut self,
        join_set: &mut JoinSet<u32>,
    ) -> (mpsc::Sender<Job>, u32) {
        let id = self.next_worker_id;
        self.next_worker_id += 1;
        let shutdown_rx = self.shutdown_rx.clone();
        let (tx, rx) = mpsc::channel(16);

        join_set.spawn(async move {
            worker(id, rx, shutdown_rx).await;
            id
        });

        (tx, id)
    }
}

#[tokio::main]
async fn main() {
    let (supervisor, job_tx) = Supervisor::new(3);

    // Spawn supervisor
    let supervisor_handle = tokio::spawn(supervisor.run());

    // Send some jobs
    for i in 0..10 {
        let job = Job {
            id: i,
            should_panic: i == 5, // Job 5 causes a panic
        };
        job_tx.send(job).await.unwrap();
        tokio::time::sleep(Duration::from_millis(20)).await;
    }

    // Drop job sender to trigger shutdown
    drop(job_tx);

    // Wait for supervisor to finish
    supervisor_handle.await.unwrap();
    println!("System shut down cleanly.");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_workers_process_jobs() {
        let (supervisor, job_tx) = Supervisor::new(2);
        let handle = tokio::spawn(supervisor.run());

        for i in 0..5 {
            job_tx
                .send(Job {
                    id: i,
                    should_panic: false,
                })
                .await
                .unwrap();
        }

        drop(job_tx);
        handle.await.unwrap();
    }

    #[tokio::test]
    async fn test_supervisor_restarts_panicked_worker() {
        let (supervisor, job_tx) = Supervisor::new(1);
        let handle = tokio::spawn(supervisor.run());

        // Send a job that causes panic
        job_tx
            .send(Job {
                id: 1,
                should_panic: true,
            })
            .await
            .unwrap();

        tokio::time::sleep(Duration::from_millis(100)).await;

        // Send more jobs -- should still work because supervisor restarted the worker
        job_tx
            .send(Job {
                id: 2,
                should_panic: false,
            })
            .await
            .unwrap();

        drop(job_tx);
        handle.await.unwrap();
    }
}
```

</details>

## Common Mistakes

1. **Using unbounded channels by default.** Unbounded channels grow without limit. A fast producer paired with a slow consumer leads to out-of-memory. Always start with bounded channels and only switch to unbounded when you can prove the producer rate is externally bounded.

2. **Forgetting to drop the `Sender` to close the channel.** The `Receiver::recv()` loop continues until all `Sender` clones are dropped. If you hold a sender in a struct that never gets dropped, the receiver blocks forever.

3. **Ignoring `broadcast::RecvError::Lagged`.** When a broadcast receiver falls behind, it skips messages and returns `Lagged(n)`. Treat this as a degraded state (log a warning), not a fatal error.

4. **Sending large values through channels.** Channels copy values on send. For large payloads, send `Arc<LargeData>` instead of `LargeData` to avoid expensive clones.

5. **Blocking inside an actor loop.** The actor loop runs on a tokio worker thread. Blocking calls (file IO, `std::thread::sleep`, CPU-heavy computation) stall the entire worker. Use `tokio::task::spawn_blocking` for CPU-bound work.

## Verification

```bash
cargo build
cargo run
cargo test
cargo clippy -- -W clippy::all
```

## Summary

Tokio's channel types -- `mpsc`, `oneshot`, `broadcast`, and `watch` -- cover the full spectrum of async communication patterns. The actor model combines `mpsc` mailboxes with `oneshot` request-response to create self-contained units of state with clean async APIs. The `Handle` pattern hides channel mechanics behind ergonomic method calls. Supervision uses `JoinSet` to detect failed actors and restart them. Bounded channels provide backpressure, `broadcast` enables fan-out, and `watch` shares the latest value efficiently. Together, these patterns replace shared mutable state with message passing, making concurrent systems easier to reason about and test.

## Resources

- [tokio::sync module documentation](https://docs.rs/tokio/latest/tokio/sync/index.html)
- [tokio::sync::mpsc](https://docs.rs/tokio/latest/tokio/sync/mpsc/index.html)
- [tokio::sync::broadcast](https://docs.rs/tokio/latest/tokio/sync/broadcast/index.html)
- [tokio::sync::watch](https://docs.rs/tokio/latest/tokio/sync/watch/index.html)
- [Alice Ryhl: Actors with Tokio](https://ryhl.io/blog/actors-with-tokio/)
- [tokio tutorial: channels](https://tokio.rs/tokio/tutorial/channels)
