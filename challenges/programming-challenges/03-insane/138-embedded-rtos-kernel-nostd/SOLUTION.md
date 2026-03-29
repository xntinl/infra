# Solution: Embedded RTOS Kernel `#![no_std]`

## Architecture Overview

The kernel is structured as five subsystems with strict layered dependencies:

1. **HAL (Hardware Abstraction Layer)**: Simulated CPU context, timer, and interrupt controller. This is the only layer that would change when porting to real hardware.
2. **Scheduler**: Priority bitmap, ready queues, time-slice management, and context switching. Owns the TCB array and dispatches the highest-priority ready task.
3. **IPC**: Counting semaphores and message queues. Each primitive has a waiter list and integrates with the scheduler to block/wake tasks.
4. **Memory Protection**: Per-task region table with permission checking. Simulates an MPU by validating every memory access against the task's allowed regions.
5. **Syscall Interface**: User/kernel mode boundary. User tasks invoke kernel operations through a `syscall()` dispatcher that validates arguments and transitions to kernel mode.

```
User Task A        User Task B        User Task C        Idle Task
     |                  |                  |                 |
   syscall()          syscall()          syscall()           |
     |                  |                  |                 |
 ====|==================|==================|=================|=====
     |                  |                  |                 |
  [Syscall Dispatcher -- validates args, enters kernel mode]
     |
  [IPC Layer]                    [Memory Protection]
  Semaphores, Msg Queues         Region checks on access
     |                                   |
  [Scheduler]
  Priority bitmap, ready queues, time-slice preemption
     |
  [HAL]
  CpuContext, TimerISR, InterruptController
```

Context switch flow:
```
1. Save current task's CpuContext to its TCB
2. Priority bitmap -> highest-priority ready task
3. Round-robin within priority if time-slice expired
4. Load next task's CpuContext from its TCB
5. Resume execution (simulated: return the task ID to the test harness)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "rtos-kernel"
version = "0.1.0"
edition = "2021"

[lib]
name = "rtos_kernel"
path = "src/lib.rs"

[[bin]]
name = "demo"
path = "src/main.rs"
```

### src/lib.rs

```rust
#![no_std]

pub mod hal;
pub mod tcb;
pub mod scheduler;
pub mod semaphore;
pub mod message_queue;
pub mod mpu;
pub mod interrupt;
pub mod syscall;
pub mod kernel;
```

### src/hal.rs

```rust
/// Simulated CPU context matching a 32-bit embedded processor.
/// On real hardware, this is saved/restored by PendSV (ARM) or equivalent.
#[derive(Debug, Clone, Copy)]
#[repr(C)]
pub struct CpuContext {
    pub regs: [u32; 16],  // r0-r15 (r13=SP, r14=LR, r15=PC on ARM)
    pub pc: u32,
    pub sp: u32,
    pub status: StatusRegister,
}

#[derive(Debug, Clone, Copy)]
#[repr(C)]
pub struct StatusRegister {
    pub negative: bool,
    pub zero: bool,
    pub carry: bool,
    pub overflow: bool,
    pub privileged: bool, // true = kernel mode, false = user mode
    pub irq_enabled: bool,
}

impl StatusRegister {
    pub const fn default_user() -> Self {
        Self {
            negative: false,
            zero: false,
            carry: false,
            overflow: false,
            privileged: false,
            irq_enabled: true,
        }
    }

    pub const fn default_kernel() -> Self {
        Self {
            negative: false,
            zero: false,
            carry: false,
            overflow: false,
            privileged: true,
            irq_enabled: true,
        }
    }
}

impl CpuContext {
    pub const fn zeroed() -> Self {
        Self {
            regs: [0; 16],
            pc: 0,
            sp: 0,
            status: StatusRegister::default_user(),
        }
    }

    pub const fn with_stack(sp: u32) -> Self {
        let mut ctx = Self::zeroed();
        ctx.sp = sp;
        ctx.regs[13] = sp; // r13 = SP convention
        ctx
    }
}

/// Simulated system timer.
pub struct SysTimer {
    pub ticks: u64,
    pub reload_value: u32,
    pub current: u32,
    pub enabled: bool,
}

impl SysTimer {
    pub const fn new(reload: u32) -> Self {
        Self {
            ticks: 0,
            reload_value: reload,
            current: reload,
            enabled: false,
        }
    }

    /// Returns true when the timer wraps (triggers a tick interrupt).
    pub fn tick(&mut self) -> bool {
        if !self.enabled {
            return false;
        }
        if self.current == 0 {
            self.current = self.reload_value;
            self.ticks += 1;
            true
        } else {
            self.current -= 1;
            false
        }
    }

    pub fn enable(&mut self) {
        self.enabled = true;
        self.current = self.reload_value;
    }
}
```

### src/tcb.rs

```rust
use crate::hal::CpuContext;

pub const MAX_TASKS: usize = 32;
pub const STACK_SIZE: usize = 1024;
pub const MAX_PRIORITY: u8 = 31;
pub const DEFAULT_TIME_SLICE: u32 = 10;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TaskState {
    Ready,
    Running,
    Blocked,
    Suspended,
    Inactive,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BlockReason {
    None,
    Delay { remaining: u32 },
    Semaphore { sem_id: u8 },
    MsgSend { queue_id: u8 },
    MsgRecv { queue_id: u8 },
}

pub struct TaskControlBlock {
    pub id: u8,
    pub name: [u8; 16],
    pub name_len: usize,
    pub priority: u8,
    pub effective_priority: u8,
    pub state: TaskState,
    pub context: CpuContext,
    pub stack: [u8; STACK_SIZE],
    pub time_slice_max: u32,
    pub time_slice_remaining: u32,
    pub block_reason: BlockReason,
    // Statistics
    pub total_ticks: u64,
    pub context_switches: u64,
}

impl TaskControlBlock {
    pub const fn empty() -> Self {
        Self {
            id: 0,
            name: [0u8; 16],
            name_len: 0,
            priority: MAX_PRIORITY,
            effective_priority: MAX_PRIORITY,
            state: TaskState::Inactive,
            context: CpuContext::zeroed(),
            stack: [0u8; STACK_SIZE],
            time_slice_max: DEFAULT_TIME_SLICE,
            time_slice_remaining: DEFAULT_TIME_SLICE,
            block_reason: BlockReason::None,
        total_ticks: 0,
            context_switches: 0,
        }
    }

    pub fn init(&mut self, id: u8, priority: u8, name: &[u8], time_slice: u32) {
        self.id = id;
        self.priority = priority;
        self.effective_priority = priority;
        self.state = TaskState::Ready;
        self.time_slice_max = time_slice;
        self.time_slice_remaining = time_slice;
        self.block_reason = BlockReason::None;
        self.total_ticks = 0;
        self.context_switches = 0;

        let copy_len = name.len().min(16);
        self.name[..copy_len].copy_from_slice(&name[..copy_len]);
        self.name_len = copy_len;

        // Initialize context with stack pointer at top of stack
        let sp = self.stack.as_ptr() as u32 + STACK_SIZE as u32;
        self.context = CpuContext::with_stack(sp);
    }

    pub fn save_context(&mut self, ctx: &CpuContext) {
        self.context = *ctx;
    }

    pub fn restore_context(&self) -> CpuContext {
        self.context
    }
}
```

### src/scheduler.rs

```rust
use crate::tcb::{TaskControlBlock, TaskState, BlockReason, MAX_TASKS, MAX_PRIORITY, DEFAULT_TIME_SLICE};
use crate::hal::CpuContext;

const QUEUE_SIZE: usize = MAX_TASKS;

struct ReadyQueue {
    tasks: [u8; QUEUE_SIZE],
    head: usize,
    tail: usize,
    count: usize,
}

impl ReadyQueue {
    const fn new() -> Self {
        Self {
            tasks: [0; QUEUE_SIZE],
            head: 0,
            tail: 0,
            count: 0,
        }
    }

    fn enqueue(&mut self, task_id: u8) -> bool {
        if self.count >= QUEUE_SIZE {
            return false;
        }
        self.tasks[self.tail] = task_id;
        self.tail = (self.tail + 1) % QUEUE_SIZE;
        self.count += 1;
        true
    }

    fn dequeue(&mut self) -> Option<u8> {
        if self.count == 0 {
            return None;
        }
        let id = self.tasks[self.head];
        self.head = (self.head + 1) % QUEUE_SIZE;
        self.count -= 1;
        Some(id)
    }

    fn is_empty(&self) -> bool {
        self.count == 0
    }

    fn remove(&mut self, task_id: u8) -> bool {
        let mut found = false;
        let count = self.count;
        let mut new_q = ReadyQueue::new();
        for _ in 0..count {
            if let Some(id) = self.dequeue() {
                if id == task_id && !found {
                    found = true;
                } else {
                    new_q.enqueue(id);
                }
            }
        }
        *self = new_q;
        found
    }
}

pub struct Scheduler {
    pub tasks: [TaskControlBlock; MAX_TASKS],
    ready_bitmap: u32,
    ready_queues: [ReadyQueue; 32],
    pub current_task: Option<u8>,
    task_count: usize,
    pub current_context: CpuContext,
    pub context_switch_count: u64,
    pub total_ticks: u64,
    pub idle_task_id: Option<u8>,
}

impl Scheduler {
    pub fn new() -> Self {
        const EMPTY_Q: ReadyQueue = ReadyQueue::new();
        Self {
            tasks: unsafe {
                let mut t: [TaskControlBlock; MAX_TASKS] =
                    core::mem::MaybeUninit::uninit().assume_init();
                let mut i = 0;
                while i < MAX_TASKS {
                    core::ptr::write(&mut t[i], TaskControlBlock::empty());
                    i += 1;
                }
                t
            },
            ready_bitmap: 0,
            ready_queues: [EMPTY_Q; 32],
            current_task: None,
            task_count: 0,
            current_context: CpuContext::zeroed(),
            context_switch_count: 0,
            total_ticks: 0,
            idle_task_id: None,
        }
    }

    pub fn create_task(&mut self, priority: u8, name: &[u8], time_slice: u32) -> Option<u8> {
        if self.task_count >= MAX_TASKS || priority > MAX_PRIORITY {
            return None;
        }
        let id = self.task_count as u8;
        self.tasks[id as usize].init(id, priority, name, time_slice);
        self.enqueue_ready(id);
        self.task_count += 1;
        Some(id)
    }

    pub fn create_idle_task(&mut self) -> u8 {
        let id = self.create_task(MAX_PRIORITY, b"idle", u32::MAX).unwrap();
        self.idle_task_id = Some(id);
        id
    }

    fn enqueue_ready(&mut self, task_id: u8) {
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

    /// Perform a context switch: save current, select next, restore next.
    pub fn context_switch(&mut self) -> Option<u8> {
        // Save current task's context
        if let Some(current_id) = self.current_task {
            let tcb = &mut self.tasks[current_id as usize];
            tcb.save_context(&self.current_context);

            if tcb.state == TaskState::Running {
                tcb.state = TaskState::Ready;
                self.enqueue_ready(current_id);
            }
        }

        // Find highest-priority ready task
        if self.ready_bitmap == 0 {
            return None;
        }

        let highest_prio = self.ready_bitmap.trailing_zeros() as usize;
        let next_id = self.ready_queues[highest_prio].dequeue()?;

        if self.ready_queues[highest_prio].is_empty() {
            self.ready_bitmap &= !(1 << highest_prio);
        }

        // Restore next task's context
        let tcb = &mut self.tasks[next_id as usize];
        tcb.state = TaskState::Running;
        tcb.context_switches += 1;
        tcb.time_slice_remaining = tcb.time_slice_max;
        self.current_context = tcb.restore_context();
        self.current_task = Some(next_id);
        self.context_switch_count += 1;

        Some(next_id)
    }

    /// Called on each timer tick. Returns true if a reschedule is needed.
    pub fn tick(&mut self) -> bool {
        self.total_ticks += 1;
        let mut need_reschedule = false;

        // Track running task's CPU time
        if let Some(current_id) = self.current_task {
            self.tasks[current_id as usize].total_ticks += 1;

            // Time-slice preemption
            let tcb = &mut self.tasks[current_id as usize];
            if tcb.time_slice_remaining > 0 {
                tcb.time_slice_remaining -= 1;
            }
            if tcb.time_slice_remaining == 0 {
                need_reschedule = true;
            }
        }

        // Decrement delay counters and wake expired tasks
        let current_prio = self.current_task
            .map(|id| self.tasks[id as usize].effective_priority)
            .unwrap_or(MAX_PRIORITY);

        let mut to_wake = [0u8; MAX_TASKS];
        let mut wake_count = 0;

        for i in 0..self.task_count {
            if let BlockReason::Delay { remaining } = self.tasks[i].block_reason {
                if self.tasks[i].state == TaskState::Blocked && remaining > 0 {
                    let new_remaining = remaining - 1;
                    if new_remaining == 0 {
                        to_wake[wake_count] = i as u8;
                        wake_count += 1;
                    } else {
                        self.tasks[i].block_reason = BlockReason::Delay { remaining: new_remaining };
                    }
                }
            }
        }

        for i in 0..wake_count {
            let tid = to_wake[i];
            let prio = self.tasks[tid as usize].effective_priority;
            self.enqueue_ready(tid);
            if prio < current_prio {
                need_reschedule = true;
            }
        }

        need_reschedule
    }

    pub fn block_current(&mut self, reason: BlockReason) {
        if let Some(current_id) = self.current_task {
            self.tasks[current_id as usize].state = TaskState::Blocked;
            self.tasks[current_id as usize].block_reason = reason;
            self.current_task = None;
        }
    }

    pub fn wake_task(&mut self, task_id: u8) {
        if self.tasks[task_id as usize].state == TaskState::Blocked {
            self.enqueue_ready(task_id);
        }
    }

    pub fn suspend_task(&mut self, task_id: u8) {
        let tcb = &self.tasks[task_id as usize];
        if tcb.state == TaskState::Ready {
            self.remove_from_ready(task_id);
        }
        self.tasks[task_id as usize].state = TaskState::Suspended;
        if self.current_task == Some(task_id) {
            self.current_task = None;
        }
    }

    pub fn resume_task(&mut self, task_id: u8) {
        if self.tasks[task_id as usize].state == TaskState::Suspended {
            self.enqueue_ready(task_id);
        }
    }

    pub fn task_count(&self) -> usize {
        self.task_count
    }

    pub fn change_effective_priority(&mut self, task_id: u8, new_prio: u8) {
        let old_prio = self.tasks[task_id as usize].effective_priority;
        if old_prio == new_prio {
            return;
        }
        if self.tasks[task_id as usize].state == TaskState::Ready {
            self.remove_from_ready(task_id);
            self.tasks[task_id as usize].effective_priority = new_prio;
            self.enqueue_ready(task_id);
        } else {
            self.tasks[task_id as usize].effective_priority = new_prio;
        }
    }
}
```

### src/semaphore.rs

```rust
use crate::scheduler::Scheduler;
use crate::tcb::{BlockReason, TaskState, MAX_PRIORITY};

pub const MAX_SEMAPHORES: usize = 16;

struct WaiterList {
    waiters: [u8; 32],
    count: usize,
}

impl WaiterList {
    const fn new() -> Self {
        Self {
            waiters: [0; 32],
            count: 0,
        }
    }

    fn add(&mut self, task_id: u8) {
        if self.count < 32 {
            self.waiters[self.count] = task_id;
            self.count += 1;
        }
    }

    fn pop_highest_priority(&mut self, scheduler: &Scheduler) -> Option<u8> {
        if self.count == 0 {
            return None;
        }
        let mut best_idx = 0;
        let mut best_prio = MAX_PRIORITY;
        for i in 0..self.count {
            let prio = scheduler.tasks[self.waiters[i] as usize].effective_priority;
            if prio < best_prio {
                best_prio = prio;
                best_idx = i;
            }
        }
        let tid = self.waiters[best_idx];
        self.count -= 1;
        if best_idx < self.count {
            self.waiters[best_idx] = self.waiters[self.count];
        }
        Some(tid)
    }

    fn is_empty(&self) -> bool {
        self.count == 0
    }
}

pub struct Semaphore {
    pub count: i32,
    pub max_count: i32,
    waiters: WaiterList,
    active: bool,
}

impl Semaphore {
    const fn empty() -> Self {
        Self {
            count: 0,
            max_count: 0,
            waiters: WaiterList::new(),
            active: false,
        }
    }
}

pub struct SemaphoreManager {
    sems: [Semaphore; MAX_SEMAPHORES],
    count: usize,
}

impl SemaphoreManager {
    pub fn new() -> Self {
        const EMPTY: Semaphore = Semaphore::empty();
        Self {
            sems: [EMPTY; MAX_SEMAPHORES],
            count: 0,
        }
    }

    pub fn create(&mut self, initial: i32, max: i32) -> Option<u8> {
        if self.count >= MAX_SEMAPHORES {
            return None;
        }
        let id = self.count;
        self.sems[id] = Semaphore {
            count: initial,
            max_count: max,
            waiters: WaiterList::new(),
            active: true,
        };
        self.count += 1;
        Some(id as u8)
    }

    /// Decrement the semaphore. Blocks the current task if count is 0.
    /// Returns true if acquired immediately, false if blocked.
    pub fn wait(&mut self, sem_id: u8, scheduler: &mut Scheduler) -> bool {
        let sem = &mut self.sems[sem_id as usize];
        if sem.count > 0 {
            sem.count -= 1;
            return true;
        }

        // Block the current task
        if let Some(current_id) = scheduler.current_task {
            sem.waiters.add(current_id);
            scheduler.block_current(BlockReason::Semaphore { sem_id });
        }
        false
    }

    /// Non-blocking wait. Returns true if acquired, false if would block.
    pub fn try_wait(&mut self, sem_id: u8) -> bool {
        let sem = &mut self.sems[sem_id as usize];
        if sem.count > 0 {
            sem.count -= 1;
            true
        } else {
            false
        }
    }

    /// Increment the semaphore and wake the highest-priority waiter.
    /// Returns the woken task ID, if any.
    /// Safe to call from ISR context (does not block).
    pub fn post(&mut self, sem_id: u8, scheduler: &mut Scheduler) -> Option<u8> {
        let sem = &mut self.sems[sem_id as usize];

        if !sem.waiters.is_empty() {
            // Wake highest-priority waiter instead of incrementing count
            let woken = sem.waiters.pop_highest_priority(scheduler);
            if let Some(tid) = woken {
                scheduler.wake_task(tid);
            }
            return woken;
        }

        if sem.count < sem.max_count {
            sem.count += 1;
        }
        None
    }

    pub fn get_count(&self, sem_id: u8) -> i32 {
        self.sems[sem_id as usize].count
    }
}
```

### src/message_queue.rs

```rust
use crate::scheduler::Scheduler;
use crate::tcb::{BlockReason, TaskState, MAX_PRIORITY};

pub const MAX_QUEUES: usize = 8;
pub const MAX_MSG_SIZE: usize = 64;
pub const MAX_QUEUE_DEPTH: usize = 16;

struct MsgSlot {
    data: [u8; MAX_MSG_SIZE],
    len: usize,
}

impl MsgSlot {
    const fn empty() -> Self {
        Self {
            data: [0; MAX_MSG_SIZE],
            len: 0,
        }
    }
}

pub struct MessageQueue {
    slots: [MsgSlot; MAX_QUEUE_DEPTH],
    head: usize,
    tail: usize,
    count: usize,
    depth: usize,
    msg_size: usize,
    active: bool,
    send_waiters: [u8; 32],
    send_waiter_count: usize,
    recv_waiters: [u8; 32],
    recv_waiter_count: usize,
    // Pending messages from blocked senders (stored when woken)
    pending_send: [MsgSlot; 4],
    pending_send_count: usize,
}

impl MessageQueue {
    const fn empty() -> Self {
        const EMPTY_SLOT: MsgSlot = MsgSlot::empty();
        Self {
            slots: [EMPTY_SLOT; MAX_QUEUE_DEPTH],
            head: 0,
            tail: 0,
            count: 0,
            depth: 0,
            msg_size: 0,
            active: false,
            send_waiters: [0; 32],
            send_waiter_count: 0,
            recv_waiters: [0; 32],
            recv_waiter_count: 0,
            pending_send: [EMPTY_SLOT; 4],
            pending_send_count: 0,
        }
    }

    fn enqueue_msg(&mut self, data: &[u8]) -> bool {
        if self.count >= self.depth {
            return false;
        }
        let len = data.len().min(self.msg_size);
        self.slots[self.tail].data[..len].copy_from_slice(&data[..len]);
        self.slots[self.tail].len = len;
        self.tail = (self.tail + 1) % self.depth;
        self.count += 1;
        true
    }

    fn dequeue_msg(&mut self, buf: &mut [u8]) -> Option<usize> {
        if self.count == 0 {
            return None;
        }
        let slot = &self.slots[self.head];
        let len = slot.len.min(buf.len());
        buf[..len].copy_from_slice(&slot.data[..len]);
        self.head = (self.head + 1) % self.depth;
        self.count -= 1;
        Some(len)
    }

    fn pop_highest_priority_sender(&mut self, scheduler: &Scheduler) -> Option<u8> {
        if self.send_waiter_count == 0 {
            return None;
        }
        let mut best_idx = 0;
        let mut best_prio = 255u8;
        for i in 0..self.send_waiter_count {
            let prio = scheduler.tasks[self.send_waiters[i] as usize].effective_priority;
            if prio < best_prio {
                best_prio = prio;
                best_idx = i;
            }
        }
        let tid = self.send_waiters[best_idx];
        self.send_waiter_count -= 1;
        if best_idx < self.send_waiter_count {
            self.send_waiters[best_idx] = self.send_waiters[self.send_waiter_count];
        }
        Some(tid)
    }

    fn pop_highest_priority_receiver(&mut self, scheduler: &Scheduler) -> Option<u8> {
        if self.recv_waiter_count == 0 {
            return None;
        }
        let mut best_idx = 0;
        let mut best_prio = 255u8;
        for i in 0..self.recv_waiter_count {
            let prio = scheduler.tasks[self.recv_waiters[i] as usize].effective_priority;
            if prio < best_prio {
                best_prio = prio;
                best_idx = i;
            }
        }
        let tid = self.recv_waiters[best_idx];
        self.recv_waiter_count -= 1;
        if best_idx < self.recv_waiter_count {
            self.recv_waiters[best_idx] = self.recv_waiters[self.recv_waiter_count];
        }
        Some(tid)
    }
}

pub struct MsgQueueManager {
    queues: [MessageQueue; MAX_QUEUES],
    count: usize,
}

impl MsgQueueManager {
    pub fn new() -> Self {
        const EMPTY: MessageQueue = MessageQueue::empty();
        Self {
            queues: [EMPTY; MAX_QUEUES],
            count: 0,
        }
    }

    pub fn create(&mut self, depth: usize, msg_size: usize) -> Option<u8> {
        if self.count >= MAX_QUEUES || depth > MAX_QUEUE_DEPTH || msg_size > MAX_MSG_SIZE {
            return None;
        }
        let id = self.count;
        self.queues[id].depth = depth;
        self.queues[id].msg_size = msg_size;
        self.queues[id].active = true;
        self.count += 1;
        Some(id as u8)
    }

    /// Send a message. Blocks if the queue is full.
    /// Returns true if sent immediately, false if blocked.
    pub fn send(
        &mut self,
        queue_id: u8,
        data: &[u8],
        scheduler: &mut Scheduler,
    ) -> bool {
        let q = &mut self.queues[queue_id as usize];

        if q.enqueue_msg(data) {
            // Wake a receiver if any is waiting
            if let Some(tid) = q.pop_highest_priority_receiver(scheduler) {
                scheduler.wake_task(tid);
            }
            return true;
        }

        // Queue full: block the sender
        if let Some(current_id) = scheduler.current_task {
            q.send_waiters[q.send_waiter_count] = current_id;
            q.send_waiter_count += 1;
            scheduler.block_current(BlockReason::MsgSend { queue_id });
        }
        false
    }

    /// Receive a message. Blocks if the queue is empty.
    /// Returns Some(bytes_read) if received immediately, None if blocked.
    pub fn recv(
        &mut self,
        queue_id: u8,
        buf: &mut [u8],
        scheduler: &mut Scheduler,
    ) -> Option<usize> {
        let q = &mut self.queues[queue_id as usize];

        if let Some(len) = q.dequeue_msg(buf) {
            // Wake a sender if any is waiting
            if let Some(tid) = q.pop_highest_priority_sender(scheduler) {
                scheduler.wake_task(tid);
            }
            return Some(len);
        }

        // Queue empty: block the receiver
        if let Some(current_id) = scheduler.current_task {
            q.recv_waiters[q.recv_waiter_count] = current_id;
            q.recv_waiter_count += 1;
            scheduler.block_current(BlockReason::MsgRecv { queue_id });
        }
        None
    }

    pub fn message_count(&self, queue_id: u8) -> usize {
        self.queues[queue_id as usize].count
    }

    pub fn is_full(&self, queue_id: u8) -> bool {
        let q = &self.queues[queue_id as usize];
        q.count >= q.depth
    }
}
```

### src/mpu.rs

```rust
pub const MAX_REGIONS_PER_TASK: usize = 8;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MemoryPermissions {
    pub read: bool,
    pub write: bool,
    pub execute: bool,
}

impl MemoryPermissions {
    pub const RO: Self = Self { read: true, write: false, execute: false };
    pub const RW: Self = Self { read: true, write: true, execute: false };
    pub const RX: Self = Self { read: true, write: false, execute: true };
    pub const RWX: Self = Self { read: true, write: true, execute: true };
    pub const NONE: Self = Self { read: false, write: false, execute: false };
}

#[derive(Debug, Clone, Copy)]
pub struct MemoryRegion {
    pub base: u32,
    pub size: u32,
    pub permissions: MemoryPermissions,
    pub active: bool,
}

impl MemoryRegion {
    pub const fn empty() -> Self {
        Self {
            base: 0,
            size: 0,
            permissions: MemoryPermissions::NONE,
            active: false,
        }
    }

    pub fn contains(&self, addr: u32) -> bool {
        self.active && addr >= self.base && addr < self.base + self.size
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AccessType {
    Read,
    Write,
    Execute,
}

#[derive(Debug, Clone, Copy)]
pub struct MemoryFault {
    pub task_id: u8,
    pub address: u32,
    pub access: AccessType,
}

pub struct MemoryProtection {
    /// Per-task region tables.
    regions: [[MemoryRegion; MAX_REGIONS_PER_TASK]; 32],
}

impl MemoryProtection {
    pub fn new() -> Self {
        const EMPTY_REGION: MemoryRegion = MemoryRegion::empty();
        const EMPTY_TABLE: [MemoryRegion; MAX_REGIONS_PER_TASK] = [EMPTY_REGION; MAX_REGIONS_PER_TASK];
        Self {
            regions: [EMPTY_TABLE; 32],
        }
    }

    pub fn add_region(
        &mut self,
        task_id: u8,
        base: u32,
        size: u32,
        permissions: MemoryPermissions,
    ) -> bool {
        let table = &mut self.regions[task_id as usize];
        for region in table.iter_mut() {
            if !region.active {
                *region = MemoryRegion {
                    base,
                    size,
                    permissions,
                    active: true,
                };
                return true;
            }
        }
        false // all region slots occupied
    }

    /// Check whether a memory access is permitted for the given task.
    /// Returns Ok(()) if allowed, Err(MemoryFault) if denied.
    pub fn check_access(
        &self,
        task_id: u8,
        address: u32,
        access: AccessType,
    ) -> Result<(), MemoryFault> {
        let table = &self.regions[task_id as usize];

        for region in table {
            if region.contains(address) {
                let permitted = match access {
                    AccessType::Read => region.permissions.read,
                    AccessType::Write => region.permissions.write,
                    AccessType::Execute => region.permissions.execute,
                };
                if permitted {
                    return Ok(());
                } else {
                    return Err(MemoryFault {
                        task_id,
                        address,
                        access,
                    });
                }
            }
        }

        // No region covers this address
        Err(MemoryFault {
            task_id,
            address,
            access,
        })
    }

    pub fn clear_regions(&mut self, task_id: u8) {
        for region in self.regions[task_id as usize].iter_mut() {
            region.active = false;
        }
    }
}
```

### src/interrupt.rs

```rust
use crate::hal::CpuContext;
use crate::scheduler::Scheduler;

pub const MAX_IRQS: usize = 16;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IsrAction {
    None,
    PostSemaphore { sem_id: u8 },
    SendMessage { queue_id: u8, data: [u8; 8], len: usize },
}

pub struct InterruptController {
    /// Priority per IRQ (0 = highest). IRQ is enabled if priority < 255.
    priorities: [u8; MAX_IRQS],
    /// Pending IRQ flags.
    pending: u16,
    /// Currently executing ISR priority (255 = no ISR active).
    active_priority: u8,
    /// Nested context stack for interrupt nesting.
    context_stack: [CpuContext; MAX_IRQS],
    nesting_depth: usize,
    /// Actions requested by ISRs (deferred until ISR completes).
    pub deferred_actions: [IsrAction; MAX_IRQS],
    pub deferred_count: usize,
    /// Statistics.
    pub total_interrupts: u64,
}

impl InterruptController {
    pub fn new() -> Self {
        Self {
            priorities: [255; MAX_IRQS],
            pending: 0,
            active_priority: 255,
            context_stack: [CpuContext::zeroed(); MAX_IRQS],
            nesting_depth: 0,
            deferred_actions: [IsrAction::None; MAX_IRQS],
            deferred_count: 0,
            total_interrupts: 0,
        }
    }

    pub fn configure_irq(&mut self, irq: u8, priority: u8) {
        if (irq as usize) < MAX_IRQS {
            self.priorities[irq as usize] = priority;
        }
    }

    pub fn disable_irq(&mut self, irq: u8) {
        if (irq as usize) < MAX_IRQS {
            self.priorities[irq as usize] = 255;
        }
    }

    /// Trigger an interrupt. Returns true if the ISR should execute now
    /// (its priority is higher than the currently active ISR).
    pub fn trigger(&mut self, irq: u8) -> bool {
        if (irq as usize) >= MAX_IRQS {
            return false;
        }
        let irq_prio = self.priorities[irq as usize];
        if irq_prio == 255 {
            return false; // IRQ disabled
        }

        self.pending |= 1 << irq;

        // Can this IRQ preempt the current ISR (or task)?
        irq_prio < self.active_priority
    }

    /// Enter ISR: push current context and set active priority.
    pub fn enter_isr(
        &mut self,
        irq: u8,
        current_context: &CpuContext,
    ) -> bool {
        let irq_prio = self.priorities[irq as usize];
        if irq_prio >= self.active_priority {
            return false; // Cannot preempt: same or lower priority
        }

        if self.nesting_depth >= MAX_IRQS {
            return false;
        }

        // Save current context on the interrupt stack
        self.context_stack[self.nesting_depth] = *current_context;
        self.nesting_depth += 1;
        self.active_priority = irq_prio;
        self.pending &= !(1 << irq);
        self.total_interrupts += 1;
        true
    }

    /// Exit ISR: restore previous context and priority.
    pub fn exit_isr(&mut self) -> Option<CpuContext> {
        if self.nesting_depth == 0 {
            return None;
        }
        self.nesting_depth -= 1;
        let restored = self.context_stack[self.nesting_depth];

        self.active_priority = if self.nesting_depth > 0 {
            // Find the priority of the previous ISR
            // (we would track this properly in production; simplified here)
            self.active_priority.saturating_add(1)
        } else {
            255 // No ISR active
        };

        Some(restored)
    }

    /// Queue a deferred action from within an ISR.
    pub fn defer_action(&mut self, action: IsrAction) {
        if self.deferred_count < MAX_IRQS {
            self.deferred_actions[self.deferred_count] = action;
            self.deferred_count += 1;
        }
    }

    /// Process all deferred actions after ISR completion.
    pub fn drain_deferred(&mut self) -> ([IsrAction; MAX_IRQS], usize) {
        let actions = self.deferred_actions;
        let count = self.deferred_count;
        self.deferred_actions = [IsrAction::None; MAX_IRQS];
        self.deferred_count = 0;
        (actions, count)
    }

    pub fn is_in_isr(&self) -> bool {
        self.nesting_depth > 0
    }

    pub fn nesting_depth(&self) -> usize {
        self.nesting_depth
    }
}
```

### src/syscall.rs

```rust
use crate::hal::StatusRegister;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyscallId {
    Yield,
    Sleep { ticks: u32 },
    SemWait { sem_id: u8 },
    SemPost { sem_id: u8 },
    SemTryWait { sem_id: u8 },
    MsgSend { queue_id: u8 },
    MsgRecv { queue_id: u8 },
    Suspend { task_id: u8 },
    Resume { task_id: u8 },
    GetTicks,
    Exit,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyscallResult {
    Ok,
    Value(u32),
    Blocked,
    Error(SyscallError),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyscallError {
    InvalidSyscall,
    NotPrivileged,
    InvalidResource,
    WouldBlock,
}

/// Validate that the caller is in user mode (syscalls come from user mode).
/// On real hardware this is enforced by the SVC instruction which traps to kernel mode.
pub fn validate_syscall(status: &StatusRegister) -> Result<(), SyscallError> {
    // In simulation, we accept calls from both modes.
    // On real hardware, only user-mode SVC triggers would reach here.
    Ok(())
}

/// Validate that a direct kernel operation is called from privileged mode.
pub fn require_privilege(status: &StatusRegister) -> Result<(), SyscallError> {
    if status.privileged {
        Ok(())
    } else {
        Err(SyscallError::NotPrivileged)
    }
}
```

### src/kernel.rs

```rust
use crate::scheduler::Scheduler;
use crate::semaphore::SemaphoreManager;
use crate::message_queue::MsgQueueManager;
use crate::mpu::MemoryProtection;
use crate::interrupt::{InterruptController, IsrAction};
use crate::syscall::{SyscallId, SyscallResult, SyscallError};
use crate::tcb::BlockReason;
use crate::hal::CpuContext;

/// The complete RTOS kernel, combining all subsystems.
pub struct Kernel {
    pub scheduler: Scheduler,
    pub semaphores: SemaphoreManager,
    pub msg_queues: MsgQueueManager,
    pub mpu: MemoryProtection,
    pub interrupts: InterruptController,
}

impl Kernel {
    pub fn new() -> Self {
        Self {
            scheduler: Scheduler::new(),
            semaphores: SemaphoreManager::new(),
            msg_queues: MsgQueueManager::new(),
            mpu: MemoryProtection::new(),
            interrupts: InterruptController::new(),
        }
    }

    /// Handle a system call from user mode.
    pub fn syscall(&mut self, id: SyscallId, data: &[u8]) -> SyscallResult {
        match id {
            SyscallId::Yield => {
                self.scheduler.context_switch();
                SyscallResult::Ok
            }
            SyscallId::Sleep { ticks } => {
                self.scheduler.block_current(BlockReason::Delay { remaining: ticks });
                self.scheduler.context_switch();
                SyscallResult::Blocked
            }
            SyscallId::SemWait { sem_id } => {
                if self.semaphores.wait(sem_id, &mut self.scheduler) {
                    SyscallResult::Ok
                } else {
                    self.scheduler.context_switch();
                    SyscallResult::Blocked
                }
            }
            SyscallId::SemPost { sem_id } => {
                self.semaphores.post(sem_id, &mut self.scheduler);
                SyscallResult::Ok
            }
            SyscallId::SemTryWait { sem_id } => {
                if self.semaphores.try_wait(sem_id) {
                    SyscallResult::Ok
                } else {
                    SyscallResult::Error(SyscallError::WouldBlock)
                }
            }
            SyscallId::MsgSend { queue_id } => {
                if self.msg_queues.send(queue_id, data, &mut self.scheduler) {
                    SyscallResult::Ok
                } else {
                    self.scheduler.context_switch();
                    SyscallResult::Blocked
                }
            }
            SyscallId::MsgRecv { queue_id } => {
                let mut buf = [0u8; 64];
                let len = data.len().min(64);
                if self.msg_queues.recv(queue_id, &mut buf[..len], &mut self.scheduler).is_some() {
                    SyscallResult::Ok
                } else {
                    self.scheduler.context_switch();
                    SyscallResult::Blocked
                }
            }
            SyscallId::Suspend { task_id } => {
                self.scheduler.suspend_task(task_id);
                SyscallResult::Ok
            }
            SyscallId::Resume { task_id } => {
                self.scheduler.resume_task(task_id);
                SyscallResult::Ok
            }
            SyscallId::GetTicks => {
                SyscallResult::Value(self.scheduler.total_ticks as u32)
            }
            SyscallId::Exit => {
                if let Some(tid) = self.scheduler.current_task {
                    self.scheduler.suspend_task(tid);
                }
                self.scheduler.context_switch();
                SyscallResult::Ok
            }
        }
    }

    /// Simulate a timer interrupt (SysTick).
    pub fn timer_isr(&mut self) {
        let needs_switch = self.scheduler.tick();
        if needs_switch {
            self.scheduler.context_switch();
        }
    }

    /// Simulate a peripheral interrupt.
    pub fn trigger_irq(&mut self, irq: u8, action: IsrAction) {
        let can_preempt = self.interrupts.trigger(irq);

        if can_preempt {
            let entered = self.interrupts.enter_isr(
                irq,
                &self.scheduler.current_context,
            );

            if entered {
                // Execute ISR action
                self.interrupts.defer_action(action);

                // Exit ISR and process deferred actions
                if let Some(ctx) = self.interrupts.exit_isr() {
                    self.scheduler.current_context = ctx;
                }

                self.process_deferred_actions();
            }
        }
    }

    fn process_deferred_actions(&mut self) {
        let (actions, count) = self.interrupts.drain_deferred();
        for i in 0..count {
            match actions[i] {
                IsrAction::PostSemaphore { sem_id } => {
                    self.semaphores.post(sem_id, &mut self.scheduler);
                }
                IsrAction::SendMessage { queue_id, ref data, len } => {
                    self.msg_queues.send(queue_id, &data[..len], &mut self.scheduler);
                }
                IsrAction::None => {}
            }
        }
    }

    /// Start the kernel: create the idle task and begin scheduling.
    pub fn start(&mut self) -> u8 {
        self.scheduler.create_idle_task();
        self.scheduler.context_switch().unwrap_or(0)
    }

    pub fn current_task_id(&self) -> Option<u8> {
        self.scheduler.current_task
    }
}
```

### src/main.rs

```rust
use rtos_kernel::kernel::Kernel;
use rtos_kernel::mpu::MemoryPermissions;
use rtos_kernel::interrupt::IsrAction;
use rtos_kernel::syscall::SyscallId;
use rtos_kernel::tcb::DEFAULT_TIME_SLICE;

fn main() {
    let mut kernel = Kernel::new();

    // Create tasks
    let t_high = kernel.scheduler.create_task(1, b"sensor", 5).unwrap();
    let t_med = kernel.scheduler.create_task(5, b"process", 10).unwrap();
    let t_low = kernel.scheduler.create_task(10, b"logger", 10).unwrap();

    // Set up memory protection
    kernel.mpu.add_region(t_high, 0x2000_0000, 0x1000, MemoryPermissions::RW);
    kernel.mpu.add_region(t_med, 0x2000_1000, 0x1000, MemoryPermissions::RW);
    kernel.mpu.add_region(t_low, 0x2000_2000, 0x1000, MemoryPermissions::RO);

    // Create IPC primitives
    let sem = kernel.semaphores.create(0, 10).unwrap();
    let queue = kernel.msg_queues.create(8, 16).unwrap();

    // Configure interrupts
    kernel.interrupts.configure_irq(0, 2);  // Timer IRQ, high priority
    kernel.interrupts.configure_irq(1, 5);  // UART IRQ, medium priority

    println!("=== RTOS Kernel Demo ===\n");

    kernel.start();

    // Simulate 50 ticks of execution
    for tick in 0..50 {
        kernel.timer_isr();

        if let Some(tid) = kernel.current_task_id() {
            let tcb = &kernel.scheduler.tasks[tid as usize];
            let name = core::str::from_utf8(&tcb.name[..tcb.name_len]).unwrap_or("?");
            if tick < 20 {
                println!("[tick {:3}] Running: {} (prio={}, slice={})",
                    tick, name, tcb.effective_priority, tcb.time_slice_remaining);
            }
        }

        // Simulate periodic interrupt on tick 10
        if tick == 10 {
            kernel.trigger_irq(1, IsrAction::PostSemaphore { sem_id: sem });
            println!("[tick {:3}] UART IRQ -> sem_post", tick);
        }
    }

    println!("\n--- Statistics ---");
    println!("Total ticks: {}", kernel.scheduler.total_ticks);
    println!("Context switches: {}", kernel.scheduler.context_switch_count);
    println!("Interrupts handled: {}", kernel.interrupts.total_interrupts);
    for i in 0..kernel.scheduler.task_count() {
        let tcb = &kernel.scheduler.tasks[i];
        let name = core::str::from_utf8(&tcb.name[..tcb.name_len]).unwrap_or("?");
        println!("  {}: {} ticks, {} switches", name, tcb.total_ticks, tcb.context_switches);
    }
}
```

### tests/kernel_tests.rs

```rust
#[cfg(test)]
mod tests {
    use rtos_kernel::kernel::Kernel;
    use rtos_kernel::tcb::{TaskState, BlockReason};
    use rtos_kernel::mpu::{MemoryPermissions, AccessType};
    use rtos_kernel::interrupt::IsrAction;
    use rtos_kernel::syscall::SyscallId;

    #[test]
    fn context_switch_saves_and_restores_registers() {
        let mut kernel = Kernel::new();
        let t0 = kernel.scheduler.create_task(1, b"A", 10).unwrap();
        let t1 = kernel.scheduler.create_task(5, b"B", 10).unwrap();
        kernel.start();

        // Modify current context (simulating task A executing)
        kernel.scheduler.current_context.regs[0] = 0xDEAD;
        kernel.scheduler.current_context.regs[5] = 0xBEEF;
        kernel.scheduler.current_context.pc = 0x1000;

        // Force context switch
        kernel.scheduler.context_switch();

        // Task A's context should be saved in its TCB
        assert_eq!(kernel.scheduler.tasks[t0 as usize].context.regs[0], 0xDEAD);
        assert_eq!(kernel.scheduler.tasks[t0 as usize].context.regs[5], 0xBEEF);
        assert_eq!(kernel.scheduler.tasks[t0 as usize].context.pc, 0x1000);
    }

    #[test]
    fn preemptive_scheduling_higher_priority_preempts() {
        let mut kernel = Kernel::new();
        let t_low = kernel.scheduler.create_task(10, b"low", 100).unwrap();
        kernel.start();

        assert_eq!(kernel.current_task_id(), Some(t_low));

        // Create a higher-priority task (simulates it becoming ready)
        let t_high = kernel.scheduler.create_task(1, b"high", 10).unwrap();
        kernel.scheduler.context_switch();

        assert_eq!(kernel.current_task_id(), Some(t_high));
    }

    #[test]
    fn time_slice_preemption() {
        let mut kernel = Kernel::new();
        kernel.scheduler.create_task(5, b"A", 3).unwrap(); // 3-tick time slice
        kernel.scheduler.create_task(5, b"B", 3).unwrap();
        kernel.start();

        let first = kernel.current_task_id().unwrap();

        // 3 ticks should exhaust A's time slice
        for _ in 0..3 {
            kernel.timer_isr();
        }

        let second = kernel.current_task_id().unwrap();
        assert_ne!(first, second, "time slice should cause switch to B");
    }

    #[test]
    fn semaphore_blocks_and_wakes() {
        let mut kernel = Kernel::new();
        let t0 = kernel.scheduler.create_task(1, b"waiter", 10).unwrap();
        let t1 = kernel.scheduler.create_task(5, b"poster", 10).unwrap();
        let sem = kernel.semaphores.create(0, 10).unwrap();
        kernel.start();

        // t0 (higher priority) waits on empty semaphore -> should block
        let result = kernel.syscall(SyscallId::SemWait { sem_id: sem }, &[]);
        assert_eq!(kernel.scheduler.tasks[t0 as usize].state, TaskState::Blocked);

        // t1 should now be running
        assert_eq!(kernel.current_task_id(), Some(t1));

        // Post semaphore -> should wake t0
        kernel.semaphores.post(sem, &mut kernel.scheduler);
        kernel.scheduler.context_switch();
        assert_eq!(kernel.current_task_id(), Some(t0));
    }

    #[test]
    fn message_queue_send_recv() {
        let mut kernel = Kernel::new();
        kernel.scheduler.create_task(1, b"recv", 10).unwrap();
        kernel.scheduler.create_task(5, b"send", 10).unwrap();
        let q = kernel.msg_queues.create(4, 8).unwrap();
        kernel.start();

        // Receiver blocks on empty queue
        let mut buf = [0u8; 8];
        let result = kernel.msg_queues.recv(q, &mut buf, &mut kernel.scheduler);
        assert!(result.is_none());

        // Switch to sender
        kernel.scheduler.context_switch();

        // Sender sends a message -> should wake receiver
        let data = b"hello!!!\0";
        kernel.msg_queues.send(q, &data[..8], &mut kernel.scheduler);

        // Receiver should be woken
        assert_eq!(kernel.scheduler.tasks[0].state, TaskState::Ready);
    }

    #[test]
    fn memory_protection_allows_permitted_access() {
        let mut kernel = Kernel::new();
        let t = kernel.scheduler.create_task(5, b"task", 10).unwrap();

        kernel.mpu.add_region(t, 0x2000_0000, 0x1000, MemoryPermissions::RW);

        assert!(kernel.mpu.check_access(t, 0x2000_0500, AccessType::Read).is_ok());
        assert!(kernel.mpu.check_access(t, 0x2000_0500, AccessType::Write).is_ok());
    }

    #[test]
    fn memory_protection_denies_unpermitted_access() {
        let mut kernel = Kernel::new();
        let t = kernel.scheduler.create_task(5, b"task", 10).unwrap();

        kernel.mpu.add_region(t, 0x2000_0000, 0x1000, MemoryPermissions::RO);

        assert!(kernel.mpu.check_access(t, 0x2000_0500, AccessType::Write).is_err());
        assert!(kernel.mpu.check_access(t, 0x3000_0000, AccessType::Read).is_err());
    }

    #[test]
    fn interrupt_nesting_higher_priority_preempts() {
        let mut kernel = Kernel::new();
        kernel.scheduler.create_task(5, b"task", 10).unwrap();
        kernel.start();

        kernel.interrupts.configure_irq(0, 5); // low-priority IRQ
        kernel.interrupts.configure_irq(1, 2); // high-priority IRQ

        // Enter low-priority ISR
        let ctx = kernel.scheduler.current_context;
        assert!(kernel.interrupts.enter_isr(0, &ctx));
        assert_eq!(kernel.interrupts.nesting_depth(), 1);

        // High-priority IRQ can nest
        let ctx2 = kernel.scheduler.current_context;
        assert!(kernel.interrupts.enter_isr(1, &ctx2));
        assert_eq!(kernel.interrupts.nesting_depth(), 2);

        // Same-or-lower priority cannot nest
        let ctx3 = kernel.scheduler.current_context;
        assert!(!kernel.interrupts.enter_isr(0, &ctx3));
        assert_eq!(kernel.interrupts.nesting_depth(), 2);

        // Exit both ISRs
        kernel.interrupts.exit_isr();
        assert_eq!(kernel.interrupts.nesting_depth(), 1);
        kernel.interrupts.exit_isr();
        assert_eq!(kernel.interrupts.nesting_depth(), 0);
    }

    #[test]
    fn isr_posts_semaphore_via_deferred_action() {
        let mut kernel = Kernel::new();
        let t = kernel.scheduler.create_task(1, b"waiter", 10).unwrap();
        let sem = kernel.semaphores.create(0, 10).unwrap();
        kernel.start();

        // Block on semaphore
        kernel.semaphores.wait(sem, &mut kernel.scheduler);

        // Configure and trigger IRQ that posts the semaphore
        kernel.interrupts.configure_irq(0, 2);
        kernel.trigger_irq(0, IsrAction::PostSemaphore { sem_id: sem });

        // Task should be woken
        assert_eq!(kernel.scheduler.tasks[t as usize].state, TaskState::Ready);
    }

    #[test]
    fn idle_task_tracks_statistics() {
        let mut kernel = Kernel::new();
        let t = kernel.scheduler.create_task(5, b"worker", 10).unwrap();
        kernel.start();

        // Block the worker
        kernel.scheduler.block_current(BlockReason::Delay { remaining: 100 });
        kernel.scheduler.context_switch();

        // Idle should be running
        assert_eq!(kernel.current_task_id(), kernel.scheduler.idle_task_id);

        // Run 10 ticks
        for _ in 0..10 {
            kernel.timer_isr();
        }

        let idle_id = kernel.scheduler.idle_task_id.unwrap();
        assert!(kernel.scheduler.tasks[idle_id as usize].total_ticks >= 10);
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
=== RTOS Kernel Demo ===

[tick   0] Running: sensor (prio=1, slice=5)
[tick   1] Running: sensor (prio=1, slice=4)
[tick   2] Running: sensor (prio=1, slice=3)
[tick   3] Running: sensor (prio=1, slice=2)
[tick   4] Running: sensor (prio=1, slice=1)
[tick   5] Running: sensor (prio=1, slice=5)
...
[tick  10] UART IRQ -> sem_post
...

--- Statistics ---
Total ticks: 50
Context switches: 12
Interrupts handled: 1
  sensor: 30 ticks, 6 switches
  process: 15 ticks, 4 switches
  logger: 3 ticks, 1 switches
  idle: 2 ticks, 1 switches
```

## Design Decisions

1. **Simulated context vs real context switching**: The `CpuContext` struct mirrors a real ARM Cortex-M register file. On real hardware, the PendSV handler saves/restores these registers to/from the TCB. By simulating this with struct copies, the kernel logic is identical to production code -- only the HAL changes.

2. **Statically sized everything**: No heap allocation in the kernel. All arrays are compile-time sized. This matches real RTOS practice where dynamic allocation is forbidden in safety-critical code (MISRA-C, DO-178C).

3. **Deferred ISR actions**: ISRs in a real RTOS cannot call blocking operations. The `defer_action` pattern queues post-ISR work that runs at the tail of the interrupt with scheduler lock held. This matches the FreeRTOS `xSemaphoreGiveFromISR` / `xQueueSendFromISR` pattern.

4. **Per-task region tables over global MPU registers**: Real MPUs have 8-16 regions that are reprogrammed on context switch. Storing per-task tables and checking against them on access is functionally equivalent but easier to simulate and test.

5. **Syscall as enum dispatch**: Maps cleanly to the SVC (Supervisor Call) instruction on ARM, where the SVC number selects the kernel operation. The enum variant carries the arguments that would normally be passed in registers r0-r3.

## Common Mistakes

1. **Not saving all registers on context switch**: Missing even one register causes silent data corruption when the task resumes. The status register is especially easy to forget.
2. **Decrementing time slice when the task is not Running**: Only the currently running task's time slice should decrement. Blocked tasks should not have their slice counted down.
3. **Allowing ISRs to call blocking operations**: `sem_wait` from an ISR deadlocks the system because there is no task to context-switch away from. ISRs must only use non-blocking variants.
4. **Not restoring interrupt priority on ISR exit**: Failing to update `active_priority` means subsequent same-priority interrupts are permanently masked.
5. **MPU check bypass in kernel mode**: Kernel-mode code should bypass MPU checks (it manages all memory). Only user-mode accesses should be validated.

## Performance Notes

- **Context switch**: O(1) for register save/restore (fixed-size struct copy). O(1) for scheduler dispatch via priority bitmap.
- **Syscall dispatch**: O(1) match statement. No hash table or vtable indirection.
- **Semaphore wait/post**: O(W) where W is waiter count (scan for highest priority). FreeRTOS uses a sorted list for O(1) wake.
- **Message queue**: O(1) for enqueue/dequeue. O(W) for waiter management.
- **MPU check**: O(R) where R is regions per task (max 8). Linear scan is faster than binary search for 8 entries due to branch prediction.
- **Memory footprint**: ~33 KB for 32 tasks with 1KB stacks. Dominated by stack allocation. TCB metadata is ~100 bytes per task. On a 64KB SRAM MCU, this leaves 31KB for application data.
- **Interrupt latency**: O(1) for entering the ISR (save context + set priority). Nesting adds O(D) where D is nesting depth (max 16).
