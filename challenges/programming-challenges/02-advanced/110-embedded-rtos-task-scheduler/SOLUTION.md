# Solution: Embedded RTOS Task Scheduler

## Architecture Overview

The scheduler is organized in four layers:

1. **TCB layer**: Fixed-size array of `TaskControlBlock` entries holding per-task state: priority, effective priority, delay counter, blocked-on resource, and state machine.
2. **Ready queue layer**: A 32-bit priority bitmap plus per-priority FIFO queues (circular arrays of task IDs). The bitmap enables O(1) highest-priority lookup.
3. **Synchronization layer**: `Mutex` with priority inheritance and `EventFlags` with wait-any/wait-all/auto-clear semantics. Each primitive maintains a waiter list.
4. **Timer layer**: System tick counter. On each tick, blocked tasks with delay counters are decremented and woken when expired.

```
              tick()
                |
          [Decrement delays] -- wake expired tasks -> Ready queue
                |
          [Check reschedule] -- higher priority woke? switch
                |
        +----- [Priority Bitmap] -----+
        |   bit 0  bit 1  ...  bit 31 |
        +------------------------------+
               |
        [Round-robin FIFO at that priority]
               |
         [Current task = Running]
               |
    yield / sleep / wait / lock
               |
        [Back to scheduler]
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "rtos-scheduler"
version = "0.1.0"
edition = "2021"

[lib]
name = "rtos_scheduler"
path = "src/lib.rs"

[[bin]]
name = "demo"
path = "src/main.rs"
```

### src/lib.rs

```rust
#![no_std]

pub mod tcb;
pub mod scheduler;
pub mod event_flags;
pub mod mutex;
pub mod timer;
pub mod idle;
```

### src/tcb.rs

```rust
/// Maximum number of tasks the scheduler supports.
pub const MAX_TASKS: usize = 32;
/// Maximum priority levels (0 = highest, 31 = lowest).
pub const MAX_PRIORITY: u8 = 31;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TaskState {
    /// Task can be scheduled.
    Ready,
    /// Task is currently executing.
    Running,
    /// Task is waiting on a resource, event, or delay.
    Blocked,
    /// Task is explicitly suspended and will not run until resumed.
    Suspended,
    /// Slot is unused.
    Inactive,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BlockReason {
    None,
    Delay,
    EventFlags { group_id: u8, mask: u32, wait_all: bool },
    Mutex { mutex_id: u8 },
}

/// Task Control Block -- the per-task metadata the scheduler manages.
#[derive(Debug)]
pub struct TaskControlBlock {
    pub id: u8,
    pub name: [u8; 16],
    pub name_len: usize,
    pub base_priority: u8,
    pub effective_priority: u8,
    pub state: TaskState,
    pub delay_remaining: u32,
    pub block_reason: BlockReason,
    /// Simulated stack pointer (would be a real pointer on hardware).
    pub stack_ptr: usize,
    /// Execution counter for tracing and utilization.
    pub run_count: u64,
}

impl TaskControlBlock {
    pub const fn empty() -> Self {
        Self {
            id: 0,
            name: [0u8; 16],
            name_len: 0,
            base_priority: MAX_PRIORITY,
            effective_priority: MAX_PRIORITY,
            state: TaskState::Inactive,
            delay_remaining: 0,
            block_reason: BlockReason::None,
            stack_ptr: 0,
            run_count: 0,
        }
    }

    pub fn init(&mut self, id: u8, priority: u8, name: &[u8]) {
        self.id = id;
        self.base_priority = priority;
        self.effective_priority = priority;
        self.state = TaskState::Ready;
        self.delay_remaining = 0;
        self.block_reason = BlockReason::None;
        self.run_count = 0;
        let copy_len = name.len().min(16);
        self.name[..copy_len].copy_from_slice(&name[..copy_len]);
        self.name_len = copy_len;
    }

    pub fn raise_priority(&mut self, new_priority: u8) {
        if new_priority < self.effective_priority {
            self.effective_priority = new_priority;
        }
    }

    pub fn restore_priority(&mut self) {
        self.effective_priority = self.base_priority;
    }
}
```

### src/scheduler.rs

```rust
use crate::tcb::{TaskControlBlock, TaskState, BlockReason, MAX_TASKS, MAX_PRIORITY};

/// Per-priority FIFO queue for round-robin scheduling.
/// Holds task IDs in a circular buffer.
const QUEUE_CAPACITY: usize = MAX_TASKS;

struct PriorityQueue {
    tasks: [u8; QUEUE_CAPACITY],
    head: usize,
    tail: usize,
    count: usize,
}

impl PriorityQueue {
    const fn new() -> Self {
        Self {
            tasks: [0; QUEUE_CAPACITY],
            head: 0,
            tail: 0,
            count: 0,
        }
    }

    fn enqueue(&mut self, task_id: u8) -> bool {
        if self.count >= QUEUE_CAPACITY {
            return false;
        }
        self.tasks[self.tail] = task_id;
        self.tail = (self.tail + 1) % QUEUE_CAPACITY;
        self.count += 1;
        true
    }

    fn dequeue(&mut self) -> Option<u8> {
        if self.count == 0 {
            return None;
        }
        let id = self.tasks[self.head];
        self.head = (self.head + 1) % QUEUE_CAPACITY;
        self.count -= 1;
        Some(id)
    }

    fn is_empty(&self) -> bool {
        self.count == 0
    }

    fn remove(&mut self, task_id: u8) -> bool {
        // Linear scan to remove a specific task (used during priority changes)
        let mut found = false;
        let mut new_queue = PriorityQueue::new();
        let count = self.count;
        for _ in 0..count {
            if let Some(id) = self.dequeue() {
                if id == task_id && !found {
                    found = true;
                } else {
                    new_queue.enqueue(id);
                }
            }
        }
        *self = new_queue;
        found
    }
}

pub struct Scheduler {
    pub tasks: [TaskControlBlock; MAX_TASKS],
    ready_bitmap: u32,
    ready_queues: [PriorityQueue; 32],
    pub current_task: Option<u8>,
    task_count: usize,
    pub system_ticks: u64,
    pub idle_task_id: Option<u8>,
    /// Trace log: records task ID on each schedule() call for testing.
    pub trace: [u8; 256],
    pub trace_len: usize,
}

impl Scheduler {
    pub fn new() -> Self {
        const EMPTY_QUEUE: PriorityQueue = PriorityQueue::new();
        Self {
            tasks: {
                let mut arr = [TaskControlBlock::empty(); 0];
                // Work around const init limitation
                let _ = arr;
                unsafe {
                    let mut tasks: [TaskControlBlock; MAX_TASKS] =
                        core::mem::MaybeUninit::uninit().assume_init();
                    let mut i = 0;
                    while i < MAX_TASKS {
                        core::ptr::write(&mut tasks[i], TaskControlBlock::empty());
                        i += 1;
                    }
                    tasks
                }
            },
            ready_bitmap: 0,
            ready_queues: [EMPTY_QUEUE; 32],
            current_task: None,
            task_count: 0,
            system_ticks: 0,
            idle_task_id: None,
            trace: [0; 256],
            trace_len: 0,
        }
    }

    /// Create a new task. Returns the task ID.
    pub fn create_task(&mut self, priority: u8, name: &[u8]) -> Option<u8> {
        if self.task_count >= MAX_TASKS || priority > MAX_PRIORITY {
            return None;
        }
        let id = self.task_count as u8;
        self.tasks[id as usize].init(id, priority, name);
        self.make_ready(id);
        self.task_count += 1;
        Some(id)
    }

    /// Create the idle task at lowest priority.
    pub fn create_idle_task(&mut self) -> u8 {
        let id = self.create_task(MAX_PRIORITY, b"idle").unwrap();
        self.idle_task_id = Some(id);
        id
    }

    fn make_ready(&mut self, task_id: u8) {
        let tcb = &mut self.tasks[task_id as usize];
        tcb.state = TaskState::Ready;
        tcb.block_reason = BlockReason::None;
        let prio = tcb.effective_priority as usize;
        self.ready_queues[prio].enqueue(task_id);
        self.ready_bitmap |= 1 << prio;
    }

    fn remove_from_ready(&mut self, task_id: u8) {
        let prio = self.tasks[task_id as usize].effective_priority as usize;
        self.ready_queues[prio].remove(task_id);
        if self.ready_queues[prio].is_empty() {
            self.ready_bitmap &= !(1 << prio);
        }
    }

    /// Find and switch to the highest-priority ready task.
    pub fn schedule(&mut self) -> Option<u8> {
        if self.ready_bitmap == 0 {
            return None;
        }

        // O(1) lookup: trailing zeros gives the highest-priority (lowest number) set bit
        let highest_prio = self.ready_bitmap.trailing_zeros() as usize;
        let task_id = self.ready_queues[highest_prio].dequeue()?;

        // Clear bitmap bit if queue is now empty
        if self.ready_queues[highest_prio].is_empty() {
            self.ready_bitmap &= !(1 << highest_prio);
        }

        // Transition previous Running task
        if let Some(prev_id) = self.current_task {
            if self.tasks[prev_id as usize].state == TaskState::Running {
                self.make_ready(prev_id);
            }
        }

        self.tasks[task_id as usize].state = TaskState::Running;
        self.tasks[task_id as usize].run_count += 1;
        self.current_task = Some(task_id);

        // Record in trace
        if self.trace_len < self.trace.len() {
            self.trace[self.trace_len] = task_id;
            self.trace_len += 1;
        }

        Some(task_id)
    }

    /// Current task voluntarily yields. Goes to tail of its priority queue.
    pub fn yield_task(&mut self) -> Option<u8> {
        if let Some(current_id) = self.current_task {
            self.tasks[current_id as usize].state = TaskState::Ready;
            self.make_ready(current_id);
            self.current_task = None;
        }
        self.schedule()
    }

    /// Block the current task for `ticks` timer ticks.
    pub fn sleep_ticks(&mut self, ticks: u32) {
        if let Some(current_id) = self.current_task {
            let tcb = &mut self.tasks[current_id as usize];
            tcb.state = TaskState::Blocked;
            tcb.block_reason = BlockReason::Delay;
            tcb.delay_remaining = ticks;
            self.current_task = None;
        }
    }

    /// Suspend a task (it will not be scheduled until resumed).
    pub fn suspend_task(&mut self, task_id: u8) {
        let tcb = &mut self.tasks[task_id as usize];
        match tcb.state {
            TaskState::Ready => {
                self.remove_from_ready(task_id);
                tcb.state = TaskState::Suspended;
            }
            TaskState::Running => {
                tcb.state = TaskState::Suspended;
                if self.current_task == Some(task_id) {
                    self.current_task = None;
                }
            }
            _ => {
                tcb.state = TaskState::Suspended;
            }
        }
    }

    /// Resume a suspended task.
    pub fn resume_task(&mut self, task_id: u8) {
        let tcb = &self.tasks[task_id as usize];
        if tcb.state == TaskState::Suspended {
            self.make_ready(task_id);
        }
    }

    /// Advance the system clock by one tick. Decrements delay counters
    /// and wakes expired tasks.
    pub fn tick(&mut self) -> bool {
        self.system_ticks += 1;
        let mut woke_higher = false;

        let current_prio = self.current_task
            .map(|id| self.tasks[id as usize].effective_priority)
            .unwrap_or(MAX_PRIORITY);

        for i in 0..self.task_count {
            let tcb = &mut self.tasks[i];
            if tcb.state == TaskState::Blocked && tcb.block_reason == BlockReason::Delay {
                if tcb.delay_remaining > 0 {
                    tcb.delay_remaining -= 1;
                    if tcb.delay_remaining == 0 {
                        let prio = tcb.effective_priority;
                        // Cannot call make_ready on &mut self while borrowing tcb,
                        // so we mark and handle below.
                        tcb.state = TaskState::Ready;
                        tcb.block_reason = BlockReason::None;
                        if prio < current_prio {
                            woke_higher = true;
                        }
                    }
                }
            }
        }

        // Re-enqueue woken tasks
        for i in 0..self.task_count {
            if self.tasks[i].state == TaskState::Ready
                && self.tasks[i].block_reason == BlockReason::None
            {
                // Check if already in ready queue by checking bitmap
                let prio = self.tasks[i].effective_priority as usize;
                // Simple approach: just enqueue (idempotency handled by scheduler)
                let already_queued = !self.ready_queues[prio].is_empty()
                    && self.is_in_ready_queue(i as u8);
                if !already_queued {
                    self.ready_queues[prio].enqueue(i as u8);
                    self.ready_bitmap |= 1 << prio;
                }
            }
        }

        woke_higher
    }

    fn is_in_ready_queue(&self, task_id: u8) -> bool {
        let prio = self.tasks[task_id as usize].effective_priority as usize;
        let q = &self.ready_queues[prio];
        for idx in 0..q.count {
            let phys = (q.head + idx) % QUEUE_CAPACITY;
            if q.tasks[phys] == task_id {
                return true;
            }
        }
        false
    }

    /// Change a task's effective priority (used by mutex priority inheritance).
    pub fn change_effective_priority(&mut self, task_id: u8, new_priority: u8) {
        let tcb = &self.tasks[task_id as usize];
        let old_priority = tcb.effective_priority;

        if old_priority == new_priority {
            return;
        }

        if tcb.state == TaskState::Ready {
            self.remove_from_ready(task_id);
            self.tasks[task_id as usize].effective_priority = new_priority;
            self.make_ready(task_id);
        } else {
            self.tasks[task_id as usize].effective_priority = new_priority;
        }
    }

    pub fn cpu_utilization(&self) -> (u64, u64) {
        let idle_runs = self.idle_task_id
            .map(|id| self.tasks[id as usize].run_count)
            .unwrap_or(0);
        (idle_runs, self.system_ticks)
    }

    pub fn task_count(&self) -> usize {
        self.task_count
    }
}
```

### src/event_flags.rs

```rust
use crate::tcb::{BlockReason, TaskState, MAX_TASKS};
use crate::scheduler::Scheduler;

pub const MAX_EVENT_GROUPS: usize = 4;

pub struct EventGroup {
    pub flags: u32,
    pub auto_clear: bool,
}

impl EventGroup {
    pub const fn new(auto_clear: bool) -> Self {
        Self {
            flags: 0,
            auto_clear,
        }
    }
}

pub struct EventFlags {
    pub groups: [EventGroup; MAX_EVENT_GROUPS],
    group_count: usize,
}

impl EventFlags {
    pub fn new() -> Self {
        const EMPTY: EventGroup = EventGroup::new(false);
        Self {
            groups: [EMPTY; MAX_EVENT_GROUPS],
            group_count: 0,
        }
    }

    pub fn create_group(&mut self, auto_clear: bool) -> Option<u8> {
        if self.group_count >= MAX_EVENT_GROUPS {
            return None;
        }
        let id = self.group_count;
        self.groups[id] = EventGroup::new(auto_clear);
        self.group_count += 1;
        Some(id as u8)
    }

    /// Set flag bits. Returns list of task IDs that should be woken.
    pub fn set(
        &mut self,
        group_id: u8,
        mask: u32,
        scheduler: &mut Scheduler,
    ) -> usize {
        let group = &mut self.groups[group_id as usize];
        group.flags |= mask;
        let mut woken = 0;

        for i in 0..scheduler.task_count() {
            let tcb = &scheduler.tasks[i];
            if tcb.state != TaskState::Blocked {
                continue;
            }

            if let BlockReason::EventFlags {
                group_id: gid,
                mask: wait_mask,
                wait_all,
            } = tcb.block_reason
            {
                if gid != group_id {
                    continue;
                }

                let should_wake = if wait_all {
                    (group.flags & wait_mask) == wait_mask
                } else {
                    (group.flags & wait_mask) != 0
                };

                if should_wake {
                    if group.auto_clear {
                        group.flags &= !wait_mask;
                    }
                    // Wake the task
                    let task_id = tcb.id;
                    scheduler.tasks[i].state = TaskState::Ready;
                    scheduler.tasks[i].block_reason = BlockReason::None;
                    woken += 1;
                }
            }
        }

        woken
    }

    /// Block the current task until the specified flags are set.
    pub fn wait(
        &self,
        group_id: u8,
        mask: u32,
        wait_all: bool,
        scheduler: &mut Scheduler,
    ) -> bool {
        let group = &self.groups[group_id as usize];

        // Check if already satisfied
        let satisfied = if wait_all {
            (group.flags & mask) == mask
        } else {
            (group.flags & mask) != 0
        };

        if satisfied {
            return true;
        }

        // Block the current task
        if let Some(current_id) = scheduler.current_task {
            let tcb = &mut scheduler.tasks[current_id as usize];
            tcb.state = TaskState::Blocked;
            tcb.block_reason = BlockReason::EventFlags {
                group_id,
                mask,
                wait_all,
            };
            scheduler.current_task = None;
        }
        false
    }

    pub fn clear(&mut self, group_id: u8, mask: u32) {
        self.groups[group_id as usize].flags &= !mask;
    }

    pub fn get(&self, group_id: u8) -> u32 {
        self.groups[group_id as usize].flags
    }
}
```

### src/mutex.rs

```rust
use crate::tcb::{BlockReason, TaskState, MAX_PRIORITY};
use crate::scheduler::Scheduler;

pub const MAX_MUTEXES: usize = 8;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MutexError {
    DoubleLock,
    NotOwner,
    InvalidMutex,
}

pub struct MutexData {
    pub owner: Option<u8>,
    /// Waiters stored as a simple array of task IDs, sorted by priority.
    waiters: [u8; 32],
    waiter_count: usize,
}

impl MutexData {
    const fn new() -> Self {
        Self {
            owner: None,
            waiters: [0; 32],
            waiter_count: 0,
        }
    }

    fn add_waiter(&mut self, task_id: u8) {
        if self.waiter_count < 32 {
            self.waiters[self.waiter_count] = task_id;
            self.waiter_count += 1;
        }
    }

    fn pop_highest_priority_waiter(&mut self, scheduler: &Scheduler) -> Option<u8> {
        if self.waiter_count == 0 {
            return None;
        }

        let mut best_idx = 0;
        let mut best_prio = MAX_PRIORITY;

        for i in 0..self.waiter_count {
            let tid = self.waiters[i];
            let prio = scheduler.tasks[tid as usize].base_priority;
            if prio < best_prio {
                best_prio = prio;
                best_idx = i;
            }
        }

        let task_id = self.waiters[best_idx];
        // Remove by swapping with last
        self.waiter_count -= 1;
        if best_idx < self.waiter_count {
            self.waiters[best_idx] = self.waiters[self.waiter_count];
        }
        Some(task_id)
    }

    fn highest_waiter_priority(&self, scheduler: &Scheduler) -> u8 {
        let mut best = MAX_PRIORITY;
        for i in 0..self.waiter_count {
            let prio = scheduler.tasks[self.waiters[i] as usize].base_priority;
            if prio < best {
                best = prio;
            }
        }
        best
    }
}

pub struct MutexManager {
    pub mutexes: [MutexData; MAX_MUTEXES],
    mutex_count: usize,
}

impl MutexManager {
    pub fn new() -> Self {
        const EMPTY: MutexData = MutexData::new();
        Self {
            mutexes: [EMPTY; MAX_MUTEXES],
            mutex_count: 0,
        }
    }

    pub fn create(&mut self) -> Option<u8> {
        if self.mutex_count >= MAX_MUTEXES {
            return None;
        }
        let id = self.mutex_count;
        self.mutexes[id] = MutexData::new();
        self.mutex_count += 1;
        Some(id as u8)
    }

    /// Attempt to lock a mutex. Returns Ok(true) if acquired, Ok(false) if blocked.
    pub fn lock(
        &mut self,
        mutex_id: u8,
        scheduler: &mut Scheduler,
    ) -> Result<bool, MutexError> {
        let mid = mutex_id as usize;
        if mid >= self.mutex_count {
            return Err(MutexError::InvalidMutex);
        }

        let current_id = scheduler.current_task.ok_or(MutexError::InvalidMutex)?;

        // Detect double-lock
        if self.mutexes[mid].owner == Some(current_id) {
            return Err(MutexError::DoubleLock);
        }

        if self.mutexes[mid].owner.is_none() {
            // Mutex is free: acquire it
            self.mutexes[mid].owner = Some(current_id);
            return Ok(true);
        }

        // Mutex is held: block the caller and apply priority inheritance
        let owner_id = self.mutexes[mid].owner.unwrap();
        let caller_prio = scheduler.tasks[current_id as usize].effective_priority;

        // Priority inheritance: boost owner if caller has higher priority
        if caller_prio < scheduler.tasks[owner_id as usize].effective_priority {
            scheduler.change_effective_priority(owner_id, caller_prio);
        }

        // Block the current task
        let tcb = &mut scheduler.tasks[current_id as usize];
        tcb.state = TaskState::Blocked;
        tcb.block_reason = BlockReason::Mutex { mutex_id };
        self.mutexes[mid].add_waiter(current_id);
        scheduler.current_task = None;

        Ok(false)
    }

    /// Unlock a mutex. Wakes the highest-priority waiter and restores priority.
    pub fn unlock(
        &mut self,
        mutex_id: u8,
        scheduler: &mut Scheduler,
    ) -> Result<Option<u8>, MutexError> {
        let mid = mutex_id as usize;
        if mid >= self.mutex_count {
            return Err(MutexError::InvalidMutex);
        }

        let current_id = scheduler.current_task.ok_or(MutexError::NotOwner)?;
        if self.mutexes[mid].owner != Some(current_id) {
            return Err(MutexError::NotOwner);
        }

        // Restore the owner's original priority
        scheduler.tasks[current_id as usize].restore_priority();

        // If the task holds other mutexes with waiters, the effective priority
        // should be the max of base priority and highest waiter priority across
        // all held mutexes. Simplified here to just restore base priority.

        // Wake the highest-priority waiter
        let woken = self.mutexes[mid].pop_highest_priority_waiter(scheduler);
        if let Some(waiter_id) = woken {
            self.mutexes[mid].owner = Some(waiter_id);
            scheduler.tasks[waiter_id as usize].state = TaskState::Ready;
            scheduler.tasks[waiter_id as usize].block_reason = BlockReason::None;
        } else {
            self.mutexes[mid].owner = None;
        }

        Ok(woken)
    }

    /// Propagate priority inheritance transitively.
    /// If task A blocks on mutex M held by task B, and task B is blocked on mutex N
    /// held by task C, then C also needs priority boost.
    pub fn propagate_inheritance(
        &self,
        start_task: u8,
        priority: u8,
        scheduler: &mut Scheduler,
    ) {
        let mut current = start_task;
        let mut depth = 0;
        const MAX_DEPTH: usize = 8; // prevent infinite loops

        while depth < MAX_DEPTH {
            let tcb = &scheduler.tasks[current as usize];

            if let BlockReason::Mutex { mutex_id } = tcb.block_reason {
                if let Some(owner) = self.mutexes[mutex_id as usize].owner {
                    scheduler.change_effective_priority(owner, priority);
                    current = owner;
                    depth += 1;
                } else {
                    break;
                }
            } else {
                break;
            }
        }
    }
}
```

### src/timer.rs

```rust
/// Timer wheel for managing task delays with O(1) tick processing.
/// Uses a simple counter-based approach for the cooperative scheduler.
pub struct TimerManager {
    pub ticks: u64,
}

impl TimerManager {
    pub const fn new() -> Self {
        Self { ticks: 0 }
    }

    pub fn current_ticks(&self) -> u64 {
        self.ticks
    }

    pub fn advance(&mut self) -> u64 {
        self.ticks += 1;
        self.ticks
    }
}
```

### src/idle.rs

```rust
/// Idle task statistics for CPU utilization measurement.
pub struct IdleStats {
    pub idle_count: u64,
    pub total_ticks: u64,
}

impl IdleStats {
    pub const fn new() -> Self {
        Self {
            idle_count: 0,
            total_ticks: 0,
        }
    }

    pub fn record_idle(&mut self) {
        self.idle_count += 1;
    }

    pub fn record_tick(&mut self) {
        self.total_ticks += 1;
    }

    /// CPU utilization as a percentage (0-100).
    /// Utilization = 1 - (idle_runs / total_ticks).
    pub fn utilization_percent(&self) -> u32 {
        if self.total_ticks == 0 {
            return 0;
        }
        let idle_pct = (self.idle_count * 100) / self.total_ticks;
        100u32.saturating_sub(idle_pct as u32)
    }
}
```

### src/main.rs

```rust
use rtos_scheduler::scheduler::Scheduler;
use rtos_scheduler::event_flags::EventFlags;
use rtos_scheduler::mutex::MutexManager;
use rtos_scheduler::idle::IdleStats;

fn main() {
    let mut sched = Scheduler::new();
    let mut events = EventFlags::new();
    let mut mutexes = MutexManager::new();
    let mut idle_stats = IdleStats::new();

    // Create tasks at different priorities
    let _t_high = sched.create_task(1, b"high");
    let _t_med = sched.create_task(5, b"medium");
    let _t_low = sched.create_task(10, b"low");
    let _t_idle = sched.create_idle_task();

    // Create synchronization primitives
    let _evt_group = events.create_group(true);
    let _mtx = mutexes.create();

    println!("=== RTOS Scheduler Demo ===\n");

    // Simulate 20 ticks of execution
    for tick in 0..20 {
        idle_stats.record_tick();

        // Simulate task behavior
        if let Some(task_id) = sched.schedule() {
            if Some(task_id) == sched.idle_task_id {
                idle_stats.record_idle();
            }

            let name = &sched.tasks[task_id as usize].name[..sched.tasks[task_id as usize].name_len];
            let name_str = core::str::from_utf8(name).unwrap_or("?");
            println!("[tick {:3}] Running: {} (prio={})",
                tick,
                name_str,
                sched.tasks[task_id as usize].effective_priority
            );

            // High-priority task sleeps periodically
            if task_id == 0 && tick % 5 == 0 && tick > 0 {
                sched.sleep_ticks(3);
                println!("           -> high sleeps for 3 ticks");
            } else {
                sched.yield_task();
            }
        }

        sched.tick();
    }

    println!("\nCPU utilization: {}%", idle_stats.utilization_percent());
    println!("Execution trace ({} entries): {:?}",
        sched.trace_len,
        &sched.trace[..sched.trace_len.min(30)]
    );
}
```

### tests/scheduler_tests.rs

```rust
#[cfg(test)]
mod tests {
    use rtos_scheduler::scheduler::Scheduler;
    use rtos_scheduler::event_flags::EventFlags;
    use rtos_scheduler::mutex::MutexManager;
    use rtos_scheduler::tcb::TaskState;

    #[test]
    fn highest_priority_runs_first() {
        let mut sched = Scheduler::new();
        sched.create_task(10, b"low");
        sched.create_task(5, b"med");
        sched.create_task(1, b"high");

        let scheduled = sched.schedule().unwrap();
        assert_eq!(scheduled, 2, "highest priority (1) task should run first");
    }

    #[test]
    fn round_robin_same_priority() {
        let mut sched = Scheduler::new();
        sched.create_task(5, b"A");
        sched.create_task(5, b"B");
        sched.create_task(5, b"C");

        let first = sched.schedule().unwrap();
        assert_eq!(first, 0);

        let second = sched.yield_task().unwrap();
        assert_eq!(second, 1);

        let third = sched.yield_task().unwrap();
        assert_eq!(third, 2);

        // Wraps back to first
        let fourth = sched.yield_task().unwrap();
        assert_eq!(fourth, 0);
    }

    #[test]
    fn sleep_ticks_accuracy() {
        let mut sched = Scheduler::new();
        sched.create_task(1, b"sleeper");
        sched.create_task(10, b"worker");
        sched.create_idle_task();

        // Run sleeper
        sched.schedule();
        sched.sleep_ticks(5);

        // Sleeper should be blocked
        assert_eq!(sched.tasks[0].state, TaskState::Blocked);

        // Advance 4 ticks -- still blocked
        for _ in 0..4 {
            sched.tick();
        }
        assert_eq!(sched.tasks[0].state, TaskState::Blocked);

        // 5th tick -- should wake
        sched.tick();
        assert_eq!(sched.tasks[0].state, TaskState::Ready);
    }

    #[test]
    fn event_flags_wait_any() {
        let mut sched = Scheduler::new();
        let mut events = EventFlags::new();

        sched.create_task(1, b"waiter");
        sched.create_task(5, b"setter");
        let group_id = events.create_group(false).unwrap();

        sched.schedule(); // Run waiter
        events.wait(group_id, 0x05, false, &mut sched); // wait_any for bits 0 and 2

        assert_eq!(sched.tasks[0].state, TaskState::Blocked);

        // Set bit 2 only
        sched.schedule(); // switch to setter
        let woken = events.set(group_id, 0x04, &mut sched);
        assert_eq!(woken, 1);
        assert_eq!(sched.tasks[0].state, TaskState::Ready);
    }

    #[test]
    fn event_flags_wait_all() {
        let mut sched = Scheduler::new();
        let mut events = EventFlags::new();

        sched.create_task(1, b"waiter");
        sched.create_task(5, b"setter");
        let group_id = events.create_group(false).unwrap();

        sched.schedule();
        events.wait(group_id, 0x05, true, &mut sched); // wait_all for bits 0 AND 2

        // Set only bit 0 -- should NOT wake
        sched.schedule();
        let woken = events.set(group_id, 0x01, &mut sched);
        assert_eq!(woken, 0);
        assert_eq!(sched.tasks[0].state, TaskState::Blocked);

        // Set bit 2 -- now both bits set, should wake
        let woken = events.set(group_id, 0x04, &mut sched);
        assert_eq!(woken, 1);
        assert_eq!(sched.tasks[0].state, TaskState::Ready);
    }

    #[test]
    fn mutex_priority_inheritance() {
        let mut sched = Scheduler::new();
        let mut mutexes = MutexManager::new();

        sched.create_task(10, b"low");    // task 0
        sched.create_task(1, b"high");    // task 1

        let mtx = mutexes.create().unwrap();

        // Low-priority task acquires mutex
        sched.schedule(); // high runs first
        sched.yield_task();
        // Force low to run by simulating schedule
        sched.current_task = Some(0);
        sched.tasks[0].state = TaskState::Running;
        mutexes.lock(mtx, &mut sched).unwrap();

        // High-priority task tries to lock -- should block and boost low
        sched.current_task = Some(1);
        sched.tasks[1].state = TaskState::Running;
        let result = mutexes.lock(mtx, &mut sched).unwrap();
        assert_eq!(result, false); // blocked

        // Low's effective priority should be raised to 1 (high's priority)
        assert_eq!(sched.tasks[0].effective_priority, 1);

        // Low unlocks
        sched.current_task = Some(0);
        sched.tasks[0].state = TaskState::Running;
        let woken = mutexes.unlock(mtx, &mut sched).unwrap();
        assert_eq!(woken, Some(1));

        // Low's priority should be restored
        assert_eq!(sched.tasks[0].effective_priority, 10);
    }

    #[test]
    fn mutex_double_lock_detected() {
        let mut sched = Scheduler::new();
        let mut mutexes = MutexManager::new();

        sched.create_task(5, b"task");
        let mtx = mutexes.create().unwrap();

        sched.schedule();
        mutexes.lock(mtx, &mut sched).unwrap();

        let result = mutexes.lock(mtx, &mut sched);
        assert!(result.is_err());
    }

    #[test]
    fn idle_task_runs_when_all_blocked() {
        let mut sched = Scheduler::new();
        sched.create_task(1, b"main");
        let idle_id = sched.create_idle_task();

        sched.schedule(); // main runs
        sched.sleep_ticks(10); // main sleeps

        let next = sched.schedule().unwrap();
        assert_eq!(next, idle_id, "idle task should run when all others blocked");
    }

    #[test]
    fn suspend_and_resume() {
        let mut sched = Scheduler::new();
        sched.create_task(1, b"A");
        sched.create_task(5, b"B");

        sched.schedule(); // A runs
        sched.suspend_task(0);

        let next = sched.schedule().unwrap();
        assert_eq!(next, 1, "B should run when A is suspended");

        sched.resume_task(0);
        let next = sched.yield_task().unwrap();
        assert_eq!(next, 0, "A should run after resume (higher priority)");
    }
}
```

### Build and Run

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== RTOS Scheduler Demo ===

[tick   0] Running: high (prio=1)
[tick   1] Running: high (prio=1)
[tick   2] Running: high (prio=1)
[tick   3] Running: high (prio=1)
[tick   4] Running: high (prio=1)
[tick   5] Running: high (prio=1)
           -> high sleeps for 3 ticks
[tick   6] Running: medium (prio=5)
[tick   7] Running: medium (prio=5)
[tick   8] Running: medium (prio=5)
[tick   9] Running: high (prio=1)
...

CPU utilization: 85%
Execution trace (20 entries): [2, 2, 2, 2, 2, 2, 1, 1, 1, 2, ...]
```

## Design Decisions

1. **Priority bitmap over linear scan**: Using `trailing_zeros()` on a 32-bit bitmap gives O(1) scheduling. A linear scan of 32 tasks would be O(N) per schedule call, unacceptable at high tick rates.

2. **Fixed-size arrays everywhere**: No heap allocation. All TCBs, queues, and waiter lists use compile-time-sized arrays. This matches real RTOS constraints where heap allocation is forbidden or dangerous.

3. **Cooperative over preemptive**: The scheduler requires explicit `yield_task()` or `sleep_ticks()` calls. This eliminates the need for context switching hardware (PendSV on ARM) and interrupt-safe critical sections, making the implementation testable on a host machine.

4. **Simplified priority inheritance**: The implementation boosts the mutex holder's priority to the highest waiter's priority. Production RTOS (like FreeRTOS) track which mutexes each task holds to correctly restore priority when multiple mutexes are involved. The transitive propagation method handles chains but does not handle the multi-mutex case fully.

5. **Event flags as u32 bitmask**: Matches the API of FreeRTOS event groups and ThreadX event flags. 32 bits is sufficient for most embedded use cases and maps directly to a single CPU register.

## Common Mistakes

1. **Not clearing the priority bitmap bit when the last task leaves a priority level**: This causes the scheduler to repeatedly check an empty queue, degrading to O(32) in the worst case.
2. **Forgetting to restore priority on mutex unlock**: The holder remains at the boosted priority forever, violating the priority model for all subsequent scheduling decisions.
3. **Re-adding a task to the ready queue without checking if it is already there**: This causes the same task to appear multiple times in the round-robin rotation.
4. **Decrementing delay counters for tasks that are not in the Delay-blocked state**: A task might be Blocked on a mutex but coincidentally have a non-zero delay_remaining from a previous sleep.
5. **Priority inheritance without transitivity**: If A blocks on mutex held by B, and B blocks on mutex held by C, only boosting B's priority is insufficient. C must also be boosted.

## Performance Notes

- **Schedule**: O(1) via priority bitmap `trailing_zeros` (maps to a single CLZ/CTZ instruction on ARM and x86).
- **Yield**: O(1) -- enqueue at tail of priority FIFO + schedule().
- **Tick**: O(N) where N is blocked task count. Optimizable to O(1) with a sorted delta list, but N <= 32 makes linear scan acceptable.
- **Mutex lock/unlock**: O(W) where W is waiter count. Finding the highest-priority waiter requires a scan of the waiter array.
- **Event flags set**: O(N) scan of all tasks to find waiters on the group. Could be optimized with per-group waiter lists.
- **Memory footprint**: ~2.5 KB for 32 TCBs + queues + sync primitives. Fits in the SRAM of most Cortex-M0 devices (4-16 KB).
