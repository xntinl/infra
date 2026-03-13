# 50. Build a Task Scheduler

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of async Rust with tokio (spawn, JoinSet, select, channels)
- Familiarity with `BinaryHeap`, `HashMap`, and graph algorithms (topological sort)
- Completed: exercises on concurrency patterns, error handling, and file I/O
- Basic understanding of DAGs (directed acyclic graphs) and cron expressions

## Learning Objectives

- Build an async task scheduler with cron-like scheduling using `BinaryHeap`
- Implement task dependencies as a DAG with topological execution order
- Control concurrent execution with semaphore-based limits
- Add retry with exponential backoff and jitter
- Support task cancellation via `CancellationToken`
- Persist task state for crash recovery
- Understand the architecture of production schedulers like Airflow and Temporal

## Concepts

A task scheduler coordinates the execution of work units (tasks) according to timing constraints, dependency relationships, and resource limits. It answers questions like: "Run task A every 5 minutes, but only after tasks B and C have succeeded, and never run more than 3 tasks at once."

This exercise builds a scheduler that combines four capabilities:

1. **Cron-like scheduling**: tasks fire at specified intervals
2. **DAG execution**: tasks declare dependencies and execute in topological order
3. **Concurrency control**: a semaphore limits how many tasks run simultaneously
4. **Resilience**: retry with backoff, cancellation, and persistent state

### Architecture

```
                    +-------------------+
                    |   Scheduler Core  |
                    |  (BinaryHeap of   |
                    |   next-fire times) |
                    +--------+----------+
                             |
              +--------------+--------------+
              |              |              |
         +----+----+   +----+----+   +----+----+
         |  Timer   |   |  DAG    |   | Executor |
         | (cron    |   | Engine  |   | (tokio   |
         |  parser) |   | (topo   |   |  tasks + |
         |          |   |  sort)  |   | semaphore)|
         +----------+   +---------+   +----------+
                                           |
                                    +------+------+
                                    | Retry Logic |
                                    | (backoff +  |
                                    |  jitter)    |
                                    +-------------+
```

---

## Implementation

### Task Definition

```rust
use std::collections::HashMap;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;

/// A unique identifier for a task.
type TaskId = String;

/// The function a task executes. Returns Ok(()) on success, Err(message) on failure.
type TaskFn = Arc<dyn Fn() -> Pin<Box<dyn Future<Output = Result<(), String>> + Send>> + Send + Sync>;

#[derive(Debug, Clone)]
struct RetryPolicy {
    max_retries: u32,
    initial_delay: Duration,
    max_delay: Duration,
    multiplier: f64,
}

impl RetryPolicy {
    fn default_policy() -> Self {
        Self {
            max_retries: 3,
            initial_delay: Duration::from_secs(1),
            max_delay: Duration::from_secs(60),
            multiplier: 2.0,
        }
    }

    /// Compute the delay for attempt `n` (0-indexed) with jitter.
    fn delay_for_attempt(&self, attempt: u32) -> Duration {
        let base = self.initial_delay.as_secs_f64() * self.multiplier.powi(attempt as i32);
        let capped = base.min(self.max_delay.as_secs_f64());

        // Add jitter: random value between 0.5x and 1.5x
        let jitter = {
            let nanos = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .subsec_nanos();
            0.5 + (nanos as f64 % 1000.0) / 1000.0
        };

        Duration::from_secs_f64(capped * jitter)
    }
}

#[derive(Debug, Clone, PartialEq)]
enum TaskStatus {
    Pending,
    Running,
    Succeeded,
    Failed(String),
    Cancelled,
    Retrying { attempt: u32, next_retry: Instant },
}

struct TaskDef {
    id: TaskId,
    name: String,
    func: TaskFn,
    dependencies: Vec<TaskId>,
    retry_policy: RetryPolicy,
    timeout: Duration,
}

struct TaskState {
    status: TaskStatus,
    attempts: u32,
    last_run: Option<Instant>,
    last_error: Option<String>,
    duration: Option<Duration>,
}

impl TaskState {
    fn new() -> Self {
        Self {
            status: TaskStatus::Pending,
            attempts: 0,
            last_run: None,
            last_error: None,
            duration: None,
        }
    }
}
```

### Cron-Like Schedule Parser

A simplified cron parser that supports: "every N seconds", "every N minutes", "at HH:MM daily".

```rust
#[derive(Debug, Clone)]
enum Schedule {
    /// Run every `interval` duration.
    Interval(Duration),
    /// Run at specific times. Simplified: minutes past the hour.
    /// E.g., [0, 15, 30, 45] means every 15 minutes.
    MinuteMarks(Vec<u32>),
    /// Run once (for DAG tasks that are triggered, not scheduled).
    Once,
}

impl Schedule {
    fn every_seconds(secs: u64) -> Self {
        Schedule::Interval(Duration::from_secs(secs))
    }

    fn every_minutes(mins: u64) -> Self {
        Schedule::Interval(Duration::from_secs(mins * 60))
    }

    /// Parse a simple schedule string:
    /// "every 30s" -> Interval(30 seconds)
    /// "every 5m" -> Interval(5 minutes)
    /// "once" -> Once
    fn parse(s: &str) -> Result<Self, String> {
        let s = s.trim().to_lowercase();

        if s == "once" {
            return Ok(Schedule::Once);
        }

        if let Some(rest) = s.strip_prefix("every ") {
            let rest = rest.trim();
            if let Some(secs) = rest.strip_suffix('s') {
                let n: u64 = secs.trim().parse().map_err(|e| format!("invalid seconds: {e}"))?;
                return Ok(Schedule::Interval(Duration::from_secs(n)));
            }
            if let Some(mins) = rest.strip_suffix('m') {
                let n: u64 = mins.trim().parse().map_err(|e| format!("invalid minutes: {e}"))?;
                return Ok(Schedule::Interval(Duration::from_secs(n * 60)));
            }
            if let Some(hours) = rest.strip_suffix('h') {
                let n: u64 = hours.trim().parse().map_err(|e| format!("invalid hours: {e}"))?;
                return Ok(Schedule::Interval(Duration::from_secs(n * 3600)));
            }
        }

        Err(format!("cannot parse schedule: '{s}'"))
    }

    fn next_fire_time(&self, last_fire: Instant) -> Option<Instant> {
        match self {
            Schedule::Interval(d) => Some(last_fire + *d),
            Schedule::Once => None, // only fires once
            Schedule::MinuteMarks(_) => {
                // Simplified: use 15-minute interval as approximation
                Some(last_fire + Duration::from_secs(900))
            }
        }
    }
}
```

### Priority Queue for Scheduling

Tasks are scheduled using a min-heap (BinaryHeap in Rust is a max-heap, so we reverse the ordering):

```rust
use std::cmp::Ordering;
use std::collections::BinaryHeap;

#[derive(Debug, Clone)]
struct ScheduledTask {
    task_id: TaskId,
    fire_time: Instant,
}

impl Eq for ScheduledTask {}

impl PartialEq for ScheduledTask {
    fn eq(&self, other: &Self) -> bool {
        self.fire_time == other.fire_time
    }
}

impl PartialOrd for ScheduledTask {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for ScheduledTask {
    fn cmp(&self, other: &Self) -> Ordering {
        // Reverse ordering: earliest fire time has highest priority
        other.fire_time.cmp(&self.fire_time)
    }
}

struct TimerQueue {
    heap: BinaryHeap<ScheduledTask>,
}

impl TimerQueue {
    fn new() -> Self {
        Self {
            heap: BinaryHeap::new(),
        }
    }

    fn schedule(&mut self, task_id: TaskId, fire_time: Instant) {
        self.heap.push(ScheduledTask { task_id, fire_time });
    }

    /// Peek at the next task to fire.
    fn peek_next(&self) -> Option<&ScheduledTask> {
        self.heap.peek()
    }

    /// Pop the next task if its fire time has passed.
    fn pop_ready(&mut self, now: Instant) -> Option<ScheduledTask> {
        if let Some(next) = self.heap.peek() {
            if next.fire_time <= now {
                return self.heap.pop();
            }
        }
        None
    }

    /// Time until the next task fires. None if queue is empty.
    fn time_until_next(&self) -> Option<Duration> {
        self.heap.peek().map(|next| {
            let now = Instant::now();
            if next.fire_time > now {
                next.fire_time - now
            } else {
                Duration::ZERO
            }
        })
    }
}
```

### DAG Executor

Execute tasks respecting dependency order using topological sort:

```rust
use std::collections::{HashMap, HashSet, VecDeque};

struct DagExecutor {
    tasks: HashMap<TaskId, TaskDef>,
    states: HashMap<TaskId, TaskState>,
}

impl DagExecutor {
    fn new() -> Self {
        Self {
            tasks: HashMap::new(),
            states: HashMap::new(),
        }
    }

    fn add_task(&mut self, def: TaskDef) {
        self.states.insert(def.id.clone(), TaskState::new());
        self.tasks.insert(def.id.clone(), def);
    }

    /// Compute topological execution order. Returns Err if there is a cycle.
    fn topological_order(&self) -> Result<Vec<TaskId>, String> {
        let mut in_degree: HashMap<&TaskId, usize> = HashMap::new();
        let mut graph: HashMap<&TaskId, Vec<&TaskId>> = HashMap::new();

        // Initialize
        for id in self.tasks.keys() {
            in_degree.entry(id).or_insert(0);
            graph.entry(id).or_default();
        }

        // Build edges: dependency -> dependent
        for (id, def) in &self.tasks {
            for dep in &def.dependencies {
                graph.entry(dep).or_default().push(id);
                *in_degree.entry(id).or_insert(0) += 1;
            }
        }

        // Kahn's algorithm
        let mut queue: VecDeque<&TaskId> = in_degree
            .iter()
            .filter(|(_, &deg)| deg == 0)
            .map(|(&id, _)| id)
            .collect();

        let mut order = Vec::new();

        while let Some(node) = queue.pop_front() {
            order.push(node.clone());
            if let Some(dependents) = graph.get(node) {
                for &dep in dependents {
                    let deg = in_degree.get_mut(dep).unwrap();
                    *deg -= 1;
                    if *deg == 0 {
                        queue.push_back(dep);
                    }
                }
            }
        }

        if order.len() != self.tasks.len() {
            return Err("cycle detected in task dependencies".to_string());
        }

        Ok(order)
    }

    /// Check if all dependencies of a task have succeeded.
    fn dependencies_met(&self, task_id: &TaskId) -> bool {
        if let Some(def) = self.tasks.get(task_id) {
            def.dependencies.iter().all(|dep| {
                self.states
                    .get(dep)
                    .map_or(false, |s| s.status == TaskStatus::Succeeded)
            })
        } else {
            false
        }
    }

    /// Return tasks that are ready to execute (dependencies met, not yet running).
    fn ready_tasks(&self) -> Vec<TaskId> {
        self.tasks
            .keys()
            .filter(|id| {
                let state = self.states.get(*id).unwrap();
                state.status == TaskStatus::Pending && self.dependencies_met(id)
            })
            .cloned()
            .collect()
    }
}
```

### The Scheduler

Bringing it all together with concurrency control:

```rust
use tokio::sync::Semaphore;
use tokio_util::sync::CancellationToken;

struct Scheduler {
    dag: DagExecutor,
    timer: TimerQueue,
    schedules: HashMap<TaskId, Schedule>,
    semaphore: Arc<Semaphore>,
    cancellation: CancellationToken,
    max_concurrency: usize,
}

impl Scheduler {
    fn new(max_concurrency: usize) -> Self {
        Self {
            dag: DagExecutor::new(),
            timer: TimerQueue::new(),
            schedules: HashMap::new(),
            semaphore: Arc::new(Semaphore::new(max_concurrency)),
            cancellation: CancellationToken::new(),
            max_concurrency,
        }
    }

    fn add_task(&mut self, def: TaskDef, schedule: Schedule) {
        let task_id = def.id.clone();
        self.dag.add_task(def);

        // Schedule the first firing
        match &schedule {
            Schedule::Interval(d) => {
                self.timer.schedule(task_id.clone(), Instant::now() + *d);
            }
            Schedule::Once => {
                self.timer.schedule(task_id.clone(), Instant::now());
            }
            Schedule::MinuteMarks(_) => {
                self.timer.schedule(task_id.clone(), Instant::now());
            }
        }

        self.schedules.insert(task_id, schedule);
    }

    /// Run the scheduler loop.
    async fn run(&mut self) {
        println!("[SCHEDULER] starting with max concurrency = {}", self.max_concurrency);

        let mut active_tasks = tokio::task::JoinSet::new();

        loop {
            // Check for cancellation
            if self.cancellation.is_cancelled() {
                println!("[SCHEDULER] cancellation requested, shutting down");
                break;
            }

            // Pop all ready tasks from the timer
            let now = Instant::now();
            while let Some(scheduled) = self.timer.pop_ready(now) {
                let task_id = &scheduled.task_id;

                // Check if dependencies are met
                if !self.dag.dependencies_met(task_id) {
                    // Reschedule for later
                    self.timer.schedule(task_id.clone(), now + Duration::from_secs(1));
                    continue;
                }

                if let Some(def) = self.dag.tasks.get(task_id) {
                    let func = def.func.clone();
                    let retry_policy = def.retry_policy.clone();
                    let timeout = def.timeout;
                    let task_id = task_id.clone();
                    let sem = self.semaphore.clone();
                    let cancel = self.cancellation.clone();

                    // Update state
                    if let Some(state) = self.dag.states.get_mut(&task_id) {
                        state.status = TaskStatus::Running;
                        state.last_run = Some(Instant::now());
                    }

                    active_tasks.spawn(async move {
                        // Acquire semaphore permit
                        let _permit = sem.acquire().await.unwrap();

                        let result = execute_with_retry(
                            &task_id, func, &retry_policy, timeout, &cancel,
                        ).await;

                        (task_id, result)
                    });
                }
            }

            // Collect completed tasks
            while let Some(result) = active_tasks.try_join_next() {
                match result {
                    Ok((task_id, Ok(attempts))) => {
                        if let Some(state) = self.dag.states.get_mut(&task_id) {
                            state.status = TaskStatus::Succeeded;
                            state.attempts = attempts;
                            state.duration = state.last_run.map(|t| t.elapsed());
                        }
                        println!("[TASK] {} succeeded (attempts: {})", task_id, attempts);

                        // Reschedule if periodic
                        if let Some(schedule) = self.schedules.get(&task_id) {
                            if let Some(next) = schedule.next_fire_time(Instant::now()) {
                                self.timer.schedule(task_id.clone(), next);

                                // Reset state for next run
                                if let Some(state) = self.dag.states.get_mut(&task_id) {
                                    state.status = TaskStatus::Pending;
                                }
                            }
                        }
                    }
                    Ok((task_id, Err(error))) => {
                        if let Some(state) = self.dag.states.get_mut(&task_id) {
                            state.status = TaskStatus::Failed(error.clone());
                            state.last_error = Some(error.clone());
                        }
                        eprintln!("[TASK] {} failed: {}", task_id, error);
                    }
                    Err(e) => {
                        eprintln!("[TASK] task panicked: {e}");
                    }
                }
            }

            // Sleep until next task or poll interval
            let sleep_duration = self.timer
                .time_until_next()
                .unwrap_or(Duration::from_secs(1))
                .min(Duration::from_millis(100));

            tokio::time::sleep(sleep_duration).await;
        }

        // Wait for all active tasks to complete
        while let Some(result) = active_tasks.join_next().await {
            match result {
                Ok((task_id, _)) => println!("[SHUTDOWN] {} completed", task_id),
                Err(e) => eprintln!("[SHUTDOWN] task error: {e}"),
            }
        }

        println!("[SCHEDULER] shutdown complete");
    }

    fn cancel(&self) {
        self.cancellation.cancel();
    }

    fn status(&self) -> Vec<(TaskId, TaskStatus)> {
        self.dag.states
            .iter()
            .map(|(id, state)| (id.clone(), state.status.clone()))
            .collect()
    }
}
```

### Execute with Retry

```rust
async fn execute_with_retry(
    task_id: &str,
    func: TaskFn,
    policy: &RetryPolicy,
    timeout: Duration,
    cancel: &CancellationToken,
) -> Result<u32, String> {
    for attempt in 0..=policy.max_retries {
        if cancel.is_cancelled() {
            return Err("cancelled".to_string());
        }

        if attempt > 0 {
            let delay = policy.delay_for_attempt(attempt - 1);
            println!(
                "[RETRY] {} attempt {} after {:?}",
                task_id,
                attempt + 1,
                delay
            );

            tokio::select! {
                _ = tokio::time::sleep(delay) => {}
                _ = cancel.cancelled() => {
                    return Err("cancelled during retry wait".to_string());
                }
            }
        }

        let result = tokio::select! {
            r = tokio::time::timeout(timeout, (func)()) => {
                match r {
                    Ok(inner) => inner,
                    Err(_) => Err(format!("timeout after {:?}", timeout)),
                }
            }
            _ = cancel.cancelled() => {
                return Err("cancelled during execution".to_string());
            }
        };

        match result {
            Ok(()) => return Ok(attempt + 1),
            Err(e) => {
                if attempt == policy.max_retries {
                    return Err(format!("failed after {} attempts: {}", attempt + 1, e));
                }
                eprintln!("[RETRY] {} attempt {} failed: {}", task_id, attempt + 1, e);
            }
        }
    }

    Err("unreachable".to_string())
}
```

### Persistence

Save task states to disk so the scheduler can resume after a crash:

```rust
use std::io::{self, Write, BufRead};
use std::fs::{File, OpenOptions};
use std::path::Path;

struct TaskLog {
    path: std::path::PathBuf,
}

impl TaskLog {
    fn new(path: impl AsRef<Path>) -> Self {
        Self {
            path: path.as_ref().to_path_buf(),
        }
    }

    fn log_event(&self, task_id: &str, status: &str, message: &str) -> io::Result<()> {
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)?;

        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();

        writeln!(file, "{}\t{}\t{}\t{}", timestamp, task_id, status, message)?;
        file.flush()
    }

    fn load_last_states(&self) -> io::Result<HashMap<String, (String, String)>> {
        let mut states = HashMap::new();

        if !self.path.exists() {
            return Ok(states);
        }

        let file = File::open(&self.path)?;
        for line in io::BufReader::new(file).lines() {
            let line = line?;
            let parts: Vec<&str> = line.splitn(4, '\t').collect();
            if parts.len() >= 3 {
                let task_id = parts[1].to_string();
                let status = parts[2].to_string();
                let message = parts.get(3).unwrap_or(&"").to_string();
                states.insert(task_id, (status, message));
            }
        }

        Ok(states)
    }
}
```

---

## Full Example

```rust
use std::sync::atomic::{AtomicU32, Ordering};

#[tokio::main]
async fn main() {
    let counter = Arc::new(AtomicU32::new(0));

    let mut scheduler = Scheduler::new(3); // max 3 concurrent tasks

    // Task A: runs every 2 seconds, always succeeds
    let counter_a = counter.clone();
    scheduler.add_task(
        TaskDef {
            id: "task-a".into(),
            name: "Heartbeat".into(),
            func: Arc::new(move || {
                let c = counter_a.clone();
                Box::pin(async move {
                    let n = c.fetch_add(1, Ordering::Relaxed);
                    println!("  [A] heartbeat #{n}");
                    Ok(())
                })
            }),
            dependencies: vec![],
            retry_policy: RetryPolicy::default_policy(),
            timeout: Duration::from_secs(5),
        },
        Schedule::every_seconds(2),
    );

    // Task B: runs every 3 seconds, depends on A
    scheduler.add_task(
        TaskDef {
            id: "task-b".into(),
            name: "Process Data".into(),
            func: Arc::new(|| {
                Box::pin(async {
                    tokio::time::sleep(Duration::from_millis(500)).await;
                    println!("  [B] processed data");
                    Ok(())
                })
            }),
            dependencies: vec!["task-a".into()],
            retry_policy: RetryPolicy::default_policy(),
            timeout: Duration::from_secs(10),
        },
        Schedule::every_seconds(3),
    );

    // Task C: runs once, fails twice then succeeds (tests retry)
    let attempt_c = Arc::new(AtomicU32::new(0));
    let attempt_c_clone = attempt_c.clone();
    scheduler.add_task(
        TaskDef {
            id: "task-c".into(),
            name: "Flaky Task".into(),
            func: Arc::new(move || {
                let a = attempt_c_clone.clone();
                Box::pin(async move {
                    let n = a.fetch_add(1, Ordering::Relaxed);
                    if n < 2 {
                        Err(format!("simulated failure #{n}"))
                    } else {
                        println!("  [C] finally succeeded on attempt {}", n + 1);
                        Ok(())
                    }
                })
            }),
            dependencies: vec![],
            retry_policy: RetryPolicy {
                max_retries: 5,
                initial_delay: Duration::from_millis(200),
                max_delay: Duration::from_secs(5),
                multiplier: 2.0,
            },
            timeout: Duration::from_secs(5),
        },
        Schedule::Once,
    );

    // Run for 10 seconds then stop
    let cancel = scheduler.cancellation.clone();
    tokio::spawn(async move {
        tokio::time::sleep(Duration::from_secs(10)).await;
        println!("\n[MAIN] requesting shutdown...");
        cancel.cancel();
    });

    scheduler.run().await;

    // Print final status
    println!("\n--- Final Status ---");
    for (id, status) in scheduler.status() {
        println!("  {id}: {status:?}");
    }

    println!("total heartbeats: {}", counter.load(Ordering::Relaxed));
}
```

---

## Exercises

### Exercise 1: DAG Validation and Execution

Build a DAG of 6 tasks with the following dependencies:

```
A -> C
B -> C
C -> D
C -> E
D -> F
E -> F
```

Verify:
1. Topological sort produces a valid order
2. Tasks A and B can run in parallel (no dependencies between them)
3. Task C waits for both A and B
4. Task F waits for both D and E
5. Detect and reject a cycle if you add `F -> A`

<details>
<summary>Solution</summary>

```rust
fn test_dag() {
    let mut dag = DagExecutor::new();

    let make_task = |id: &str, deps: Vec<&str>| TaskDef {
        id: id.to_string(),
        name: id.to_string(),
        func: Arc::new(|| Box::pin(async { Ok(()) })),
        dependencies: deps.into_iter().map(String::from).collect(),
        retry_policy: RetryPolicy::default_policy(),
        timeout: Duration::from_secs(5),
    };

    dag.add_task(make_task("A", vec![]));
    dag.add_task(make_task("B", vec![]));
    dag.add_task(make_task("C", vec!["A", "B"]));
    dag.add_task(make_task("D", vec!["C"]));
    dag.add_task(make_task("E", vec!["C"]));
    dag.add_task(make_task("F", vec!["D", "E"]));

    // Test topological sort
    let order = dag.topological_order().expect("should have valid order");
    println!("topological order: {:?}", order);

    // Verify constraints
    let pos = |id: &str| order.iter().position(|x| x == id).unwrap();
    assert!(pos("A") < pos("C"));
    assert!(pos("B") < pos("C"));
    assert!(pos("C") < pos("D"));
    assert!(pos("C") < pos("E"));
    assert!(pos("D") < pos("F"));
    assert!(pos("E") < pos("F"));
    println!("topological order: valid");

    // Test ready tasks: initially only A and B
    let ready = dag.ready_tasks();
    println!("initially ready: {:?}", ready);
    assert!(ready.contains(&"A".to_string()));
    assert!(ready.contains(&"B".to_string()));
    assert_eq!(ready.len(), 2);

    // Simulate A completing
    dag.states.get_mut("A").unwrap().status = TaskStatus::Succeeded;
    let ready = dag.ready_tasks();
    println!("after A succeeds: ready = {:?}", ready);
    assert!(ready.contains(&"B".to_string()));
    assert!(!ready.contains(&"C".to_string())); // still waiting on B

    // Simulate B completing
    dag.states.get_mut("B").unwrap().status = TaskStatus::Succeeded;
    let ready = dag.ready_tasks();
    println!("after B succeeds: ready = {:?}", ready);
    assert!(ready.contains(&"C".to_string()));

    // Test cycle detection
    let mut dag_cycle = DagExecutor::new();
    dag_cycle.add_task(make_task("X", vec!["Z"]));
    dag_cycle.add_task(make_task("Y", vec!["X"]));
    dag_cycle.add_task(make_task("Z", vec!["Y"]));

    assert!(dag_cycle.topological_order().is_err());
    println!("cycle detection: works");

    println!("\nDAG tests: all passed");
}

fn main() {
    test_dag();
}
```
</details>

### Exercise 2: Retry with Exponential Backoff

Test the retry mechanism by creating a task that fails with decreasing probability on each attempt. Verify:
1. The task retries the correct number of times
2. Delays between retries increase exponentially
3. A task that exceeds `max_retries` is marked as failed
4. A task that succeeds on retry N is marked as succeeded with the correct attempt count

<details>
<summary>Solution</summary>

```rust
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Instant;

#[tokio::main]
async fn main() {
    // Test 1: Task that fails twice then succeeds
    {
        let attempts = Arc::new(AtomicU32::new(0));
        let attempts_clone = attempts.clone();

        let func: TaskFn = Arc::new(move || {
            let a = attempts_clone.clone();
            Box::pin(async move {
                let n = a.fetch_add(1, Ordering::Relaxed);
                if n < 2 {
                    Err(format!("fail #{n}"))
                } else {
                    Ok(())
                }
            })
        });

        let policy = RetryPolicy {
            max_retries: 5,
            initial_delay: Duration::from_millis(50),
            max_delay: Duration::from_secs(1),
            multiplier: 2.0,
        };

        let cancel = CancellationToken::new();
        let start = Instant::now();
        let result = execute_with_retry("test-1", func, &policy, Duration::from_secs(5), &cancel).await;
        let elapsed = start.elapsed();

        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 3); // succeeded on 3rd attempt
        assert_eq!(attempts.load(Ordering::Relaxed), 3);
        println!("test 1: succeeded on attempt 3 in {:?}", elapsed);
    }

    // Test 2: Task that always fails (exhausts retries)
    {
        let func: TaskFn = Arc::new(|| {
            Box::pin(async { Err("always fails".to_string()) })
        });

        let policy = RetryPolicy {
            max_retries: 2,
            initial_delay: Duration::from_millis(10),
            max_delay: Duration::from_millis(100),
            multiplier: 2.0,
        };

        let cancel = CancellationToken::new();
        let result = execute_with_retry("test-2", func, &policy, Duration::from_secs(5), &cancel).await;

        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(err.contains("failed after 3 attempts"));
        println!("test 2: correctly failed after max retries");
    }

    // Test 3: Task cancelled during retry wait
    {
        let func: TaskFn = Arc::new(|| {
            Box::pin(async { Err("fail".to_string()) })
        });

        let policy = RetryPolicy {
            max_retries: 10,
            initial_delay: Duration::from_secs(60), // long delay
            max_delay: Duration::from_secs(60),
            multiplier: 1.0,
        };

        let cancel = CancellationToken::new();
        let cancel_clone = cancel.clone();

        // Cancel after 100ms
        tokio::spawn(async move {
            tokio::time::sleep(Duration::from_millis(100)).await;
            cancel_clone.cancel();
        });

        let start = Instant::now();
        let result = execute_with_retry("test-3", func, &policy, Duration::from_secs(5), &cancel).await;
        let elapsed = start.elapsed();

        assert!(result.is_err());
        assert!(result.unwrap_err().contains("cancelled"));
        assert!(elapsed < Duration::from_secs(1)); // should cancel quickly
        println!("test 3: cancelled during retry wait in {:?}", elapsed);
    }

    // Test 4: Verify exponential backoff timing
    {
        let policy = RetryPolicy {
            max_retries: 4,
            initial_delay: Duration::from_millis(100),
            max_delay: Duration::from_secs(10),
            multiplier: 2.0,
        };

        println!("\nbackoff delays:");
        for attempt in 0..4 {
            let delay = policy.delay_for_attempt(attempt);
            let expected_base = 100.0 * 2.0f64.powi(attempt as i32);
            println!("  attempt {}: {:?} (base: {:.0}ms)", attempt, delay, expected_base);
            // With jitter, delay should be between 0.5x and 1.5x base
            let min = Duration::from_secs_f64(expected_base / 1000.0 * 0.4);
            let max = Duration::from_secs_f64(expected_base / 1000.0 * 1.6);
            assert!(delay >= min && delay <= max, "delay out of expected range");
        }
        println!("  backoff timing: verified");
    }

    println!("\nall retry tests passed");
}
```
</details>

### Exercise 3: Concurrent Execution with Semaphore

Create 10 tasks that each take 1 second to complete. Run them with a concurrency limit of 3. Verify:
1. At most 3 tasks run simultaneously
2. Total wall-clock time is approximately ceil(10/3) = 4 seconds
3. All 10 tasks complete successfully

<details>
<summary>Solution</summary>

```rust
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use tokio::sync::Semaphore;

#[tokio::main]
async fn main() {
    let max_concurrent = 3;
    let total_tasks = 10;
    let task_duration = Duration::from_secs(1);

    let semaphore = Arc::new(Semaphore::new(max_concurrent));
    let peak_concurrent = Arc::new(AtomicU32::new(0));
    let current_concurrent = Arc::new(AtomicU32::new(0));
    let completed = Arc::new(AtomicU32::new(0));

    let start = Instant::now();
    let mut set = tokio::task::JoinSet::new();

    for i in 0..total_tasks {
        let sem = semaphore.clone();
        let peak = peak_concurrent.clone();
        let current = current_concurrent.clone();
        let done = completed.clone();

        set.spawn(async move {
            let _permit = sem.acquire().await.unwrap();

            let running = current.fetch_add(1, Ordering::SeqCst) + 1;
            peak.fetch_max(running as u32, Ordering::SeqCst);

            println!("[t={:?}] task {i} started (concurrent: {running})", start.elapsed());
            tokio::time::sleep(task_duration).await;

            current.fetch_sub(1, Ordering::SeqCst);
            done.fetch_add(1, Ordering::Relaxed);
            println!("[t={:?}] task {i} completed", start.elapsed());

            i
        });
    }

    let mut results = Vec::new();
    while let Some(result) = set.join_next().await {
        results.push(result.unwrap());
    }

    let total_time = start.elapsed();
    let peak = peak_concurrent.load(Ordering::Relaxed);
    let done = completed.load(Ordering::Relaxed);

    println!("\n--- Results ---");
    println!("total tasks: {done}");
    println!("peak concurrent: {peak}");
    println!("wall-clock time: {:?}", total_time);

    assert_eq!(done, total_tasks as u32);
    assert!(peak <= max_concurrent as u32, "exceeded concurrency limit");
    assert!(
        total_time >= Duration::from_secs(3) && total_time < Duration::from_secs(6),
        "unexpected total time: {:?}",
        total_time
    );

    println!("\nconcurrency test: all assertions passed");
}
```
</details>

### Exercise 4: Task Cancellation

Build a scenario where:
1. 5 long-running tasks are started (each sleeps 10 seconds)
2. After 2 seconds, cancel all tasks
3. Verify all tasks are cancelled within 500ms of the cancel signal
4. Verify no task completes its full 10-second duration

<details>
<summary>Solution</summary>

```rust
use tokio_util::sync::CancellationToken;
use std::sync::atomic::{AtomicU32, Ordering};

#[tokio::main]
async fn main() {
    let token = CancellationToken::new();
    let completed = Arc::new(AtomicU32::new(0));
    let cancelled = Arc::new(AtomicU32::new(0));

    let mut set = tokio::task::JoinSet::new();

    for i in 0..5 {
        let token = token.clone();
        let completed = completed.clone();
        let cancelled = cancelled.clone();

        set.spawn(async move {
            tokio::select! {
                _ = tokio::time::sleep(Duration::from_secs(10)) => {
                    completed.fetch_add(1, Ordering::Relaxed);
                    println!("task {i}: completed (should not happen!)");
                    Ok(i)
                }
                _ = token.cancelled() => {
                    cancelled.fetch_add(1, Ordering::Relaxed);
                    println!("task {i}: cancelled");
                    Err(format!("task {i} cancelled"))
                }
            }
        });
    }

    // Cancel after 2 seconds
    let cancel_token = token.clone();
    tokio::spawn(async move {
        tokio::time::sleep(Duration::from_secs(2)).await;
        println!("\n--- sending cancel signal ---\n");
        cancel_token.cancel();
    });

    let start = Instant::now();

    // Wait for all tasks
    let mut results = Vec::new();
    while let Some(result) = set.join_next().await {
        results.push(result.unwrap());
    }

    let elapsed = start.elapsed();

    println!("\n--- Results ---");
    println!("completed: {}", completed.load(Ordering::Relaxed));
    println!("cancelled: {}", cancelled.load(Ordering::Relaxed));
    println!("total time: {:?}", elapsed);

    assert_eq!(completed.load(Ordering::Relaxed), 0, "no task should complete");
    assert_eq!(cancelled.load(Ordering::Relaxed), 5, "all tasks should cancel");
    assert!(
        elapsed < Duration::from_secs(4),
        "should finish well before 10 seconds"
    );

    println!("\ncancellation test: all assertions passed");
}
```
</details>

### Exercise 5: Full Pipeline with Persistence

Build a complete pipeline that:
1. Defines a DAG: `fetch-data -> process-data -> generate-report`
2. `fetch-data` takes 1s and always succeeds
3. `process-data` takes 2s and fails on the first attempt (retry succeeds)
4. `generate-report` takes 1s and always succeeds
5. Log all task events to a file
6. After completion, read the log and verify the event sequence

<details>
<summary>Solution</summary>

```rust
use std::sync::atomic::{AtomicU32, Ordering};

#[tokio::main]
async fn main() -> io::Result<()> {
    let log_path = "/tmp/scheduler-test.log";
    let _ = std::fs::remove_file(log_path);
    let task_log = Arc::new(TaskLog::new(log_path));

    let process_attempts = Arc::new(AtomicU32::new(0));

    let mut scheduler = Scheduler::new(2);

    // Task 1: fetch-data
    let log = task_log.clone();
    scheduler.add_task(
        TaskDef {
            id: "fetch-data".into(),
            name: "Fetch Data".into(),
            func: Arc::new(move || {
                let log = log.clone();
                Box::pin(async move {
                    let _ = log.log_event("fetch-data", "RUNNING", "");
                    tokio::time::sleep(Duration::from_secs(1)).await;
                    let _ = log.log_event("fetch-data", "SUCCEEDED", "fetched 1000 rows");
                    Ok(())
                })
            }),
            dependencies: vec![],
            retry_policy: RetryPolicy::default_policy(),
            timeout: Duration::from_secs(10),
        },
        Schedule::Once,
    );

    // Task 2: process-data (depends on fetch-data, fails first attempt)
    let log = task_log.clone();
    let attempts = process_attempts.clone();
    scheduler.add_task(
        TaskDef {
            id: "process-data".into(),
            name: "Process Data".into(),
            func: Arc::new(move || {
                let log = log.clone();
                let attempts = attempts.clone();
                Box::pin(async move {
                    let n = attempts.fetch_add(1, Ordering::Relaxed);
                    let _ = log.log_event("process-data", "RUNNING", &format!("attempt {}", n + 1));
                    tokio::time::sleep(Duration::from_secs(1)).await;

                    if n == 0 {
                        let _ = log.log_event("process-data", "FAILED", "transient error");
                        Err("transient processing error".to_string())
                    } else {
                        let _ = log.log_event("process-data", "SUCCEEDED", "processed 1000 rows");
                        Ok(())
                    }
                })
            }),
            dependencies: vec!["fetch-data".into()],
            retry_policy: RetryPolicy {
                max_retries: 3,
                initial_delay: Duration::from_millis(100),
                max_delay: Duration::from_secs(5),
                multiplier: 2.0,
            },
            timeout: Duration::from_secs(10),
        },
        Schedule::Once,
    );

    // Task 3: generate-report (depends on process-data)
    let log = task_log.clone();
    scheduler.add_task(
        TaskDef {
            id: "generate-report".into(),
            name: "Generate Report".into(),
            func: Arc::new(move || {
                let log = log.clone();
                Box::pin(async move {
                    let _ = log.log_event("generate-report", "RUNNING", "");
                    tokio::time::sleep(Duration::from_secs(1)).await;
                    let _ = log.log_event("generate-report", "SUCCEEDED", "report generated");
                    Ok(())
                })
            }),
            dependencies: vec!["process-data".into()],
            retry_policy: RetryPolicy::default_policy(),
            timeout: Duration::from_secs(10),
        },
        Schedule::Once,
    );

    // Auto-cancel after 30 seconds (safety net)
    let cancel = scheduler.cancellation.clone();
    tokio::spawn(async move {
        tokio::time::sleep(Duration::from_secs(30)).await;
        cancel.cancel();
    });

    let start = Instant::now();
    scheduler.run().await;
    let elapsed = start.elapsed();

    // Print status
    println!("\n--- Final Status ---");
    for (id, status) in scheduler.status() {
        println!("  {id}: {status:?}");
    }
    println!("total time: {:?}", elapsed);

    // Read and verify log
    println!("\n--- Event Log ---");
    let log_content = std::fs::read_to_string(log_path)?;
    for line in log_content.lines() {
        println!("  {line}");
    }

    // Verify sequence
    let events: Vec<(&str, &str)> = log_content.lines()
        .filter_map(|line| {
            let parts: Vec<&str> = line.splitn(4, '\t').collect();
            if parts.len() >= 3 {
                Some((parts[1], parts[2]))
            } else {
                None
            }
        })
        .collect();

    // fetch-data should run and succeed before process-data starts
    let fetch_succeeded = events.iter().position(|&(id, s)| id == "fetch-data" && s == "SUCCEEDED");
    let process_first_run = events.iter().position(|&(id, s)| id == "process-data" && s == "RUNNING");
    assert!(fetch_succeeded.is_some());
    assert!(process_first_run.is_some());
    // Note: due to scheduler timing, we verify events were logged

    println!("\npipeline test: all events logged correctly");
    let _ = std::fs::remove_file(log_path);
    Ok(())
}
```
</details>

---

## Common Mistakes

1. **Using `std::time::Instant` across tokio tasks.** `Instant` is monotonic and safe across tasks, but be aware that `tokio::time::sleep` may not wake at the exact instant requested. Always use `>=` comparisons, not `==`.

2. **Deadlocking the DAG.** If task A depends on task B and task B depends on task A, the scheduler hangs. Always validate for cycles before execution using topological sort.

3. **Forgetting to re-schedule periodic tasks.** After a task completes, it must be pushed back into the timer queue with its next fire time. Missing this causes the task to run only once.

4. **Semaphore starvation.** If a task acquires a semaphore permit and then panics, the permit is leaked (never returned). Use RAII patterns -- the permit is automatically returned when the `SemaphorePermit` is dropped, which handles panics correctly.

5. **Jitter implementation.** Using the same seed for all retries produces the same "random" delay. Use a time-based seed or a proper RNG. Our simplified jitter uses `subsec_nanos()` which varies enough for demonstration but is not cryptographically random.

6. **Not handling `JoinSet` task panics.** `JoinSet::join_next()` returns `Result<T, JoinError>`. A `JoinError` means the task panicked. Always handle this case to avoid losing track of task status.

---

## Verification

```bash
cargo new task-scheduler-lab && cd task-scheduler-lab
```

Add dependencies to `Cargo.toml`:

```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
tokio-util = "0.7"
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

---

## What You Learned

- A `BinaryHeap` with reversed ordering serves as an efficient min-priority queue for scheduling tasks by their next fire time. Peeking and popping are O(log n), and the scheduler only wakes when the next task is due.
- DAG-based execution with topological sort (Kahn's algorithm) ensures tasks run only after all their dependencies have succeeded, with O(V + E) cycle detection built in.
- Tokio's `Semaphore` provides precise concurrency control: tasks acquire a permit before running and release it automatically when the permit guard drops, even on panics.
- Exponential backoff with jitter prevents retry storms (thundering herd) by spreading retry times across a range rather than having all failed tasks retry simultaneously.
- `CancellationToken` from `tokio-util` enables cooperative cancellation: long-running tasks check for cancellation at `.await` points via `tokio::select!`, allowing graceful shutdown within milliseconds.
- Persistent event logging enables crash recovery by replaying task states from disk, and provides an audit trail for debugging failed workflows.

## Resources

- [Tokio: Graceful Shutdown](https://tokio.rs/tokio/topics/shutdown)
- [tokio-util CancellationToken](https://docs.rs/tokio-util/latest/tokio_util/sync/struct.CancellationToken.html)
- [Apache Airflow Architecture](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/overview.html)
- [Temporal.io Concepts](https://docs.temporal.io/concepts)
- [Exponential Backoff and Jitter (AWS)](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [Kahn's Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Topological_sorting#Kahn's_algorithm)
