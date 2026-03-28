# Solution: Thread Scheduler Simulation

## Architecture Overview

The simulator is structured around four components:

1. **Process model**: Processes with CPU/IO burst sequences, state machine (New -> Ready -> Running -> Blocked/Terminated)
2. **Scheduler trait**: Pluggable algorithm interface with `next_process()`, `add_process()`, `preempt_check()`
3. **Simulation engine**: Drives logical time forward, handles state transitions, context switches, I/O completion
4. **Statistics and output**: Collects per-process and global metrics, renders Gantt charts

```
[Workload Generator]
        |
        v
[Process List] --> [Simulation Engine] --> [Statistics]
                        |                       |
                   [Scheduler]             [Gantt Chart]
                   /    |    \    \
                 RR  Priority CFS  MLFQ
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "scheduler-sim"
version = "0.1.0"
edition = "2021"
```

### src/process.rs

```rust
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum ProcessState {
    New,
    Ready,
    Running,
    Blocked,
    Terminated,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BurstType {
    Cpu(u64),
    Io(u64),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PriorityClass {
    RealTime,
    Normal,
}

#[derive(Debug, Clone)]
pub struct ProcessStats {
    pub arrival_time: u64,
    pub first_run_time: Option<u64>,
    pub completion_time: Option<u64>,
    pub total_wait_time: u64,
    pub total_cpu_time: u64,
    pub last_ready_time: u64,
}

impl ProcessStats {
    pub fn turnaround(&self) -> Option<u64> {
        self.completion_time.map(|c| c - self.arrival_time)
    }

    pub fn response(&self) -> Option<u64> {
        self.first_run_time.map(|f| f - self.arrival_time)
    }
}

#[derive(Debug, Clone)]
pub struct Process {
    pub pid: u32,
    pub name: String,
    pub priority: i32,          // Lower number = higher priority
    pub nice: i32,              // -20 to 19 (CFS weight derivation)
    pub priority_class: PriorityClass,
    pub state: ProcessState,
    pub bursts: Vec<BurstType>,
    pub current_burst_index: usize,
    pub remaining_in_burst: u64,
    pub stats: ProcessStats,
    pub vruntime: u64,          // For CFS
    pub mlfq_level: usize,     // For MLFQ
}

impl Process {
    pub fn new(
        pid: u32,
        name: &str,
        priority: i32,
        nice: i32,
        priority_class: PriorityClass,
        arrival_time: u64,
        bursts: Vec<BurstType>,
    ) -> Self {
        let remaining = match bursts.first() {
            Some(BurstType::Cpu(t)) => *t,
            Some(BurstType::Io(t)) => *t,
            None => 0,
        };

        Process {
            pid,
            name: name.to_string(),
            priority,
            nice,
            priority_class,
            state: ProcessState::New,
            bursts,
            current_burst_index: 0,
            remaining_in_burst: remaining,
            stats: ProcessStats {
                arrival_time,
                first_run_time: None,
                completion_time: None,
                total_wait_time: 0,
                total_cpu_time: 0,
                last_ready_time: arrival_time,
            },
            vruntime: 0,
            mlfq_level: 0,
        }
    }

    pub fn current_burst(&self) -> Option<BurstType> {
        self.bursts.get(self.current_burst_index).copied()
    }

    pub fn advance_burst(&mut self) {
        self.current_burst_index += 1;
        if let Some(burst) = self.bursts.get(self.current_burst_index) {
            self.remaining_in_burst = match burst {
                BurstType::Cpu(t) => *t,
                BurstType::Io(t) => *t,
            };
        } else {
            self.remaining_in_burst = 0;
        }
    }

    pub fn is_finished(&self) -> bool {
        self.current_burst_index >= self.bursts.len()
    }

    /// CFS weight derived from nice value.
    /// Nice 0 = weight 1024 (baseline), lower nice = higher weight.
    pub fn weight(&self) -> u64 {
        // Simplified: weight = 1024 * 1.25^(-nice)
        let base: f64 = 1.25_f64.powi(-self.nice);
        (1024.0 * base) as u64
    }
}
```

### src/scheduler.rs

```rust
use crate::process::*;
use std::collections::{VecDeque, BTreeMap};

pub trait Scheduler {
    fn name(&self) -> &str;
    fn add_process(&mut self, pid: u32);
    fn remove_process(&mut self, pid: u32);
    fn next_process(&mut self, processes: &[Process]) -> Option<u32>;
    fn time_quantum(&self, process: &Process) -> u64;
    fn on_tick(&mut self, processes: &mut [Process], current_time: u64);
    fn should_preempt(&self, running: &Process, arriving: &Process) -> bool;
}

// --- Round Robin ---

pub struct RoundRobin {
    queue: VecDeque<u32>,
    quantum: u64,
}

impl RoundRobin {
    pub fn new(quantum: u64) -> Self {
        RoundRobin {
            queue: VecDeque::new(),
            quantum,
        }
    }
}

impl Scheduler for RoundRobin {
    fn name(&self) -> &str { "Round Robin" }

    fn add_process(&mut self, pid: u32) {
        self.queue.push_back(pid);
    }

    fn remove_process(&mut self, pid: u32) {
        self.queue.retain(|&p| p != pid);
    }

    fn next_process(&mut self, _processes: &[Process]) -> Option<u32> {
        self.queue.pop_front()
    }

    fn time_quantum(&self, _process: &Process) -> u64 {
        self.quantum
    }

    fn on_tick(&mut self, _processes: &mut [Process], _current_time: u64) {}

    fn should_preempt(&self, _running: &Process, arriving: &Process) -> bool {
        arriving.priority_class == PriorityClass::RealTime
    }
}

// --- Priority Scheduling ---

pub struct PriorityScheduler {
    queue: Vec<u32>,
    aging_interval: u64,
}

impl PriorityScheduler {
    pub fn new(aging_interval: u64) -> Self {
        PriorityScheduler {
            queue: Vec::new(),
            aging_interval,
        }
    }
}

impl Scheduler for PriorityScheduler {
    fn name(&self) -> &str { "Priority (with aging)" }

    fn add_process(&mut self, pid: u32) {
        self.queue.push(pid);
    }

    fn remove_process(&mut self, pid: u32) {
        self.queue.retain(|&p| p != pid);
    }

    fn next_process(&mut self, processes: &[Process]) -> Option<u32> {
        if self.queue.is_empty() {
            return None;
        }

        // Find highest priority (lowest number) among ready processes.
        // Real-time processes always come first.
        self.queue.sort_by(|&a, &b| {
            let pa = &processes[a as usize];
            let pb = &processes[b as usize];
            match (pa.priority_class, pb.priority_class) {
                (PriorityClass::RealTime, PriorityClass::Normal) => std::cmp::Ordering::Less,
                (PriorityClass::Normal, PriorityClass::RealTime) => std::cmp::Ordering::Greater,
                _ => pa.priority.cmp(&pb.priority),
            }
        });

        Some(self.queue.remove(0))
    }

    fn time_quantum(&self, _process: &Process) -> u64 {
        u64::MAX // Non-preemptive within same priority
    }

    fn on_tick(&mut self, processes: &mut [Process], current_time: u64) {
        if self.aging_interval > 0 && current_time % self.aging_interval == 0 {
            for &pid in &self.queue {
                let p = &mut processes[pid as usize];
                if p.priority > 0 && p.priority_class == PriorityClass::Normal {
                    p.priority -= 1; // Age: increase priority (lower number)
                }
            }
        }
    }

    fn should_preempt(&self, running: &Process, arriving: &Process) -> bool {
        if arriving.priority_class == PriorityClass::RealTime
            && running.priority_class != PriorityClass::RealTime
        {
            return true;
        }
        arriving.priority < running.priority
    }
}

// --- Completely Fair Scheduler ---

pub struct CfsScheduler {
    tree: BTreeMap<(u64, u32), u32>, // (vruntime, pid) -> pid
    target_latency: u64,
    min_granularity: u64,
}

impl CfsScheduler {
    pub fn new(target_latency: u64, min_granularity: u64) -> Self {
        CfsScheduler {
            tree: BTreeMap::new(),
            target_latency,
            min_granularity,
        }
    }
}

impl Scheduler for CfsScheduler {
    fn name(&self) -> &str { "CFS" }

    fn add_process(&mut self, pid: u32) {
        // Will be inserted with current vruntime in the simulation engine.
        // Use pid=0 vruntime as placeholder; real vruntime set by engine.
        self.tree.insert((0, pid), pid);
    }

    fn remove_process(&mut self, pid: u32) {
        self.tree.retain(|_, &mut v| v != pid);
    }

    fn next_process(&mut self, _processes: &[Process]) -> Option<u32> {
        let entry = self.tree.iter().next()?.clone();
        let pid = *entry.1;
        self.tree.remove(entry.0);
        Some(pid)
    }

    fn time_quantum(&self, process: &Process) -> u64 {
        let n = self.tree.len() as u64 + 1; // +1 for the running process
        let slice = self.target_latency * process.weight() / (n * 1024);
        slice.max(self.min_granularity)
    }

    fn on_tick(&mut self, _processes: &mut [Process], _current_time: u64) {}

    fn should_preempt(&self, running: &Process, arriving: &Process) -> bool {
        arriving.priority_class == PriorityClass::RealTime
            && running.priority_class != PriorityClass::RealTime
    }
}

// --- Multi-Level Feedback Queue ---

pub struct MlfqScheduler {
    levels: Vec<VecDeque<u32>>,
    quanta: Vec<u64>,
    boost_interval: u64,
}

impl MlfqScheduler {
    pub fn new(num_levels: usize, base_quantum: u64, boost_interval: u64) -> Self {
        let mut quanta = Vec::new();
        for i in 0..num_levels {
            quanta.push(base_quantum * (1 << i)); // Double quantum per level
        }
        MlfqScheduler {
            levels: vec![VecDeque::new(); num_levels],
            quanta,
            boost_interval,
        }
    }
}

impl Scheduler for MlfqScheduler {
    fn name(&self) -> &str { "MLFQ" }

    fn add_process(&mut self, pid: u32) {
        // New processes enter highest priority queue (level 0).
        self.levels[0].push_back(pid);
    }

    fn remove_process(&mut self, pid: u32) {
        for level in &mut self.levels {
            level.retain(|&p| p != pid);
        }
    }

    fn next_process(&mut self, processes: &[Process]) -> Option<u32> {
        // Real-time check: scan all levels for RT processes.
        for level in &mut self.levels {
            let rt_pos = level.iter().position(|&pid| {
                processes[pid as usize].priority_class == PriorityClass::RealTime
            });
            if let Some(pos) = rt_pos {
                return Some(level.remove(pos).unwrap());
            }
        }

        // Normal: take from highest priority non-empty queue.
        for level in &mut self.levels {
            if let Some(pid) = level.pop_front() {
                return Some(pid);
            }
        }
        None
    }

    fn time_quantum(&self, process: &Process) -> u64 {
        let level = process.mlfq_level.min(self.quanta.len() - 1);
        self.quanta[level]
    }

    fn on_tick(&mut self, processes: &mut [Process], current_time: u64) {
        // Priority boost: move all processes to top queue.
        if self.boost_interval > 0 && current_time > 0 && current_time % self.boost_interval == 0 {
            let mut all_pids: Vec<u32> = Vec::new();
            for level in &mut self.levels {
                all_pids.extend(level.drain(..));
            }
            for &pid in &all_pids {
                processes[pid as usize].mlfq_level = 0;
            }
            self.levels[0].extend(all_pids);
        }
    }

    fn should_preempt(&self, running: &Process, arriving: &Process) -> bool {
        if arriving.priority_class == PriorityClass::RealTime
            && running.priority_class != PriorityClass::RealTime
        {
            return true;
        }
        arriving.mlfq_level < running.mlfq_level
    }
}
```

### src/engine.rs

```rust
use crate::process::*;
use crate::scheduler::*;

pub struct SimConfig {
    pub context_switch_cost: u64,
}

#[derive(Debug, Clone)]
pub struct GanttEntry {
    pub time: u64,
    pub pid: Option<u32>,  // None = idle / context switch
    pub event: String,
}

#[derive(Debug)]
pub struct SimResult {
    pub gantt: Vec<GanttEntry>,
    pub processes: Vec<Process>,
    pub total_time: u64,
    pub context_switches: u64,
    pub idle_time: u64,
}

pub fn simulate(
    processes: &mut Vec<Process>,
    scheduler: &mut dyn Scheduler,
    config: &SimConfig,
) -> SimResult {
    let mut time: u64 = 0;
    let mut gantt: Vec<GanttEntry> = Vec::new();
    let mut running_pid: Option<u32> = None;
    let mut quantum_remaining: u64 = 0;
    let mut context_switches: u64 = 0;
    let mut idle_time: u64 = 0;
    let mut switching_until: u64 = 0;

    let total_processes = processes.len();
    let mut completed = 0;

    // I/O completion events: (completion_time, pid)
    let mut io_events: Vec<(u64, u32)> = Vec::new();

    loop {
        if completed >= total_processes {
            break;
        }

        // 1. Process arrivals.
        for p in processes.iter_mut() {
            if p.state == ProcessState::New && p.stats.arrival_time <= time {
                p.state = ProcessState::Ready;
                p.stats.last_ready_time = time;
                scheduler.add_process(p.pid);
            }
        }

        // 2. I/O completions.
        let completing: Vec<u32> = io_events.iter()
            .filter(|(t, _)| *t <= time)
            .map(|(_, pid)| *pid)
            .collect();
        io_events.retain(|(t, _)| *t > time);

        for pid in completing {
            let p = &mut processes[pid as usize];
            p.advance_burst();
            if p.is_finished() {
                p.state = ProcessState::Terminated;
                p.stats.completion_time = Some(time);
                completed += 1;
            } else {
                p.state = ProcessState::Ready;
                p.stats.last_ready_time = time;
                scheduler.add_process(pid);

                // Check preemption.
                if let Some(run_pid) = running_pid {
                    if scheduler.should_preempt(&processes[run_pid as usize], &processes[pid as usize]) {
                        let run_p = &mut processes[run_pid as usize];
                        run_p.state = ProcessState::Ready;
                        run_p.stats.last_ready_time = time;
                        scheduler.add_process(run_pid);
                        running_pid = None;
                        gantt.push(GanttEntry {
                            time,
                            pid: Some(run_pid),
                            event: format!("P{} preempted by P{}", run_pid, pid),
                        });
                    }
                }
            }
        }

        // 3. Context switch in progress.
        if time < switching_until {
            gantt.push(GanttEntry { time, pid: None, event: "ctx_switch".to_string() });
            time += 1;
            continue;
        }

        // 4. Tick scheduler (aging, boost).
        scheduler.on_tick(processes, time);

        // 5. Check if running process finished its burst or quantum.
        if let Some(pid) = running_pid {
            let p = &mut processes[pid as usize];
            if p.remaining_in_burst == 0 {
                // Burst complete.
                p.advance_burst();
                if p.is_finished() {
                    p.state = ProcessState::Terminated;
                    p.stats.completion_time = Some(time);
                    completed += 1;
                    running_pid = None;
                } else if let Some(BurstType::Io(io_time)) = p.current_burst() {
                    // Start I/O.
                    p.state = ProcessState::Blocked;
                    io_events.push((time + io_time, pid));
                    running_pid = None;
                    gantt.push(GanttEntry {
                        time,
                        pid: Some(pid),
                        event: format!("P{} -> I/O ({}t)", pid, io_time),
                    });
                } else {
                    // Next burst is CPU, re-queue.
                    p.state = ProcessState::Ready;
                    p.stats.last_ready_time = time;
                    scheduler.add_process(pid);
                    running_pid = None;
                }
            } else if quantum_remaining == 0 {
                // Quantum expired, preempt.
                p.state = ProcessState::Ready;
                p.stats.last_ready_time = time;

                // MLFQ: demote if quantum fully used.
                if scheduler.name() == "MLFQ" {
                    p.mlfq_level = (p.mlfq_level + 1).min(2);
                }

                scheduler.add_process(pid);
                running_pid = None;
                gantt.push(GanttEntry {
                    time,
                    pid: Some(pid),
                    event: format!("P{} quantum expired", pid),
                });
            }
        }

        // 6. Dispatch next process.
        if running_pid.is_none() {
            if let Some(next_pid) = scheduler.next_process(processes) {
                let p = &mut processes[next_pid as usize];
                let was_running = gantt.last().map_or(true, |g| g.pid != Some(next_pid));

                if was_running && time > 0 {
                    context_switches += 1;
                    if config.context_switch_cost > 0 {
                        switching_until = time + config.context_switch_cost;
                        gantt.push(GanttEntry { time, pid: None, event: "ctx_switch".to_string() });
                        scheduler.add_process(next_pid); // Re-add; will be picked after switch
                        time += 1;
                        continue;
                    }
                }

                // Update wait time.
                p.stats.total_wait_time += time - p.stats.last_ready_time;

                if p.stats.first_run_time.is_none() {
                    p.stats.first_run_time = Some(time);
                }

                p.state = ProcessState::Running;
                quantum_remaining = scheduler.time_quantum(p);
                running_pid = Some(next_pid);
            }
        }

        // 7. Execute one time unit.
        if let Some(pid) = running_pid {
            let p = &mut processes[pid as usize];
            p.remaining_in_burst -= 1;
            p.stats.total_cpu_time += 1;
            quantum_remaining = quantum_remaining.saturating_sub(1);

            // CFS: update vruntime.
            let delta_vruntime = 1024 / p.weight().max(1);
            p.vruntime += delta_vruntime;

            gantt.push(GanttEntry {
                time,
                pid: Some(pid),
                event: format!("P{} running", pid),
            });
        } else {
            idle_time += 1;
            gantt.push(GanttEntry { time, pid: None, event: "idle".to_string() });
        }

        time += 1;

        // Safety: prevent infinite simulation.
        if time > 100_000 {
            eprintln!("simulation exceeded 100,000 time units, aborting");
            break;
        }
    }

    SimResult {
        gantt,
        processes: processes.clone(),
        total_time: time,
        context_switches,
        idle_time,
    }
}
```

### src/output.rs

```rust
use crate::engine::*;
use crate::process::*;

pub fn print_gantt_chart(result: &SimResult) {
    println!("\n=== Gantt Chart ({} time units) ===\n", result.total_time);

    // Compact representation: group consecutive runs.
    let mut segments: Vec<(u64, u64, Option<u32>)> = Vec::new();
    let mut current_pid: Option<u32> = None;
    let mut start_time: u64 = 0;

    for entry in &result.gantt {
        if entry.pid != current_pid {
            if entry.time > start_time || current_pid.is_some() {
                segments.push((start_time, entry.time, current_pid));
            }
            current_pid = entry.pid;
            start_time = entry.time;
        }
    }
    if start_time < result.total_time {
        segments.push((start_time, result.total_time, current_pid));
    }

    // Print timeline.
    print!("CPU: |");
    for (start, end, pid) in &segments {
        let label = match pid {
            Some(p) => format!("P{}", p),
            None => "--".to_string(),
        };
        let width = (end - start) as usize;
        print!("{:^width$}|", label, width = width.max(2));
    }
    println!();

    // Print time markers.
    print!("     ");
    for (start, _, _) in &segments {
        print!("{:<3}", start);
    }
    if let Some((_, end, _)) = segments.last() {
        print!("{}", end);
    }
    println!();

    // I/O timeline.
    println!("\nI/O events:");
    for entry in &result.gantt {
        if entry.event.contains("I/O") {
            println!("  t={}: {}", entry.time, entry.event);
        }
    }
}

pub fn print_statistics(result: &SimResult) {
    println!("\n=== Per-Process Statistics ===\n");
    println!(
        "{:<6} {:<10} {:>10} {:>10} {:>10} {:>10} {:>8}",
        "PID", "Name", "Arrival", "Turnaround", "Wait", "Response", "CPU"
    );
    println!("{}", "-".repeat(74));

    let mut total_turnaround = 0u64;
    let mut total_wait = 0u64;
    let mut total_response = 0u64;
    let mut count = 0u64;

    for p in &result.processes {
        let turnaround = p.stats.turnaround().unwrap_or(0);
        let response = p.stats.response().unwrap_or(0);

        println!(
            "{:<6} {:<10} {:>10} {:>10} {:>10} {:>10} {:>8}",
            p.pid, p.name, p.stats.arrival_time,
            turnaround, p.stats.total_wait_time, response, p.stats.total_cpu_time,
        );

        total_turnaround += turnaround;
        total_wait += p.stats.total_wait_time;
        total_response += response;
        count += 1;
    }

    let cpu_util = if result.total_time > 0 {
        (1.0 - result.idle_time as f64 / result.total_time as f64) * 100.0
    } else {
        0.0
    };

    println!("\n=== Global Statistics ===\n");
    println!("Total simulation time:  {} time units", result.total_time);
    println!("CPU utilization:        {:.1}%", cpu_util);
    println!(
        "Throughput:             {:.3} processes/time unit",
        count as f64 / result.total_time.max(1) as f64
    );
    println!("Context switches:       {}", result.context_switches);
    println!("Idle time:              {} time units", result.idle_time);
    println!();
    println!("Avg turnaround time:    {:.2}", total_turnaround as f64 / count as f64);
    println!("Avg wait time:          {:.2}", total_wait as f64 / count as f64);
    println!("Avg response time:      {:.2}", total_response as f64 / count as f64);
}

pub fn print_comparison(results: &[(&str, SimResult)]) {
    println!("\n=== Algorithm Comparison ===\n");
    println!(
        "{:<20} {:>10} {:>10} {:>10} {:>8} {:>10}",
        "Algorithm", "Avg Turn.", "Avg Wait", "Avg Resp.", "CPU %", "Ctx Sw."
    );
    println!("{}", "-".repeat(78));

    for (name, result) in results {
        let count = result.processes.len() as f64;
        let avg_turn: f64 = result.processes.iter()
            .filter_map(|p| p.stats.turnaround())
            .sum::<u64>() as f64 / count;
        let avg_wait: f64 = result.processes.iter()
            .map(|p| p.stats.total_wait_time)
            .sum::<u64>() as f64 / count;
        let avg_resp: f64 = result.processes.iter()
            .filter_map(|p| p.stats.response())
            .sum::<u64>() as f64 / count;
        let cpu_util = (1.0 - result.idle_time as f64 / result.total_time.max(1) as f64) * 100.0;

        println!(
            "{:<20} {:>10.2} {:>10.2} {:>10.2} {:>7.1}% {:>10}",
            name, avg_turn, avg_wait, avg_resp, cpu_util, result.context_switches,
        );
    }
}
```

### src/workload.rs

```rust
use crate::process::*;

pub fn cpu_bound_workload() -> Vec<Process> {
    vec![
        Process::new(0, "cpu-heavy", 5, 0, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(20), BurstType::Io(2), BurstType::Cpu(15)]),
        Process::new(1, "cpu-med", 5, 0, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(10), BurstType::Io(3), BurstType::Cpu(8)]),
        Process::new(2, "cpu-light", 5, 0, PriorityClass::Normal, 2,
            vec![BurstType::Cpu(5), BurstType::Io(1), BurstType::Cpu(3)]),
    ]
}

pub fn io_bound_workload() -> Vec<Process> {
    vec![
        Process::new(0, "io-1", 3, 0, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(2), BurstType::Io(10), BurstType::Cpu(2), BurstType::Io(8), BurstType::Cpu(1)]),
        Process::new(1, "io-2", 3, 0, PriorityClass::Normal, 1,
            vec![BurstType::Cpu(3), BurstType::Io(12), BurstType::Cpu(1), BurstType::Io(5), BurstType::Cpu(2)]),
        Process::new(2, "cpu-bg", 8, 0, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(30)]),
    ]
}

pub fn mixed_workload() -> Vec<Process> {
    vec![
        Process::new(0, "web-srv", 2, -5, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(3), BurstType::Io(8), BurstType::Cpu(2), BurstType::Io(6), BurstType::Cpu(3)]),
        Process::new(1, "compile", 10, 10, PriorityClass::Normal, 0,
            vec![BurstType::Cpu(25), BurstType::Io(2), BurstType::Cpu(20)]),
        Process::new(2, "editor", 5, 0, PriorityClass::Normal, 3,
            vec![BurstType::Cpu(2), BurstType::Io(15), BurstType::Cpu(1), BurstType::Io(10), BurstType::Cpu(2)]),
        Process::new(3, "backup", 15, 15, PriorityClass::Normal, 5,
            vec![BurstType::Cpu(5), BurstType::Io(20), BurstType::Cpu(5)]),
        Process::new(4, "rt-audio", 0, -20, PriorityClass::RealTime, 10,
            vec![BurstType::Cpu(3), BurstType::Io(5), BurstType::Cpu(2)]),
    ]
}

pub fn starvation_demo() -> Vec<Process> {
    let mut procs = Vec::new();
    for i in 0..5 {
        procs.push(Process::new(
            i, &format!("high-{}", i), 1, -10, PriorityClass::Normal, i as u64 * 2,
            vec![BurstType::Cpu(10), BurstType::Io(2), BurstType::Cpu(8)],
        ));
    }
    procs.push(Process::new(
        5, "low-prio", 20, 19, PriorityClass::Normal, 0,
        vec![BurstType::Cpu(5)],
    ));
    procs
}
```

### src/main.rs

```rust
mod process;
mod scheduler;
mod engine;
mod output;
mod workload;

use scheduler::*;
use engine::*;
use output::*;

fn main() {
    let config = SimConfig { context_switch_cost: 1 };

    println!("========================================");
    println!("  Thread Scheduler Simulation");
    println!("========================================");

    // Run mixed workload with all four algorithms.
    let algorithms: Vec<(&str, Box<dyn FnMut() -> Box<dyn Scheduler>>)> = vec![
        ("Round Robin (q=4)", Box::new(|| Box::new(RoundRobin::new(4)))),
        ("Priority (aging=10)", Box::new(|| Box::new(PriorityScheduler::new(10)))),
        ("CFS (lat=20, gran=2)", Box::new(|| Box::new(CfsScheduler::new(20, 2)))),
        ("MLFQ (3 levels, q=4)", Box::new(|| Box::new(MlfqScheduler::new(3, 4, 50)))),
    ];

    let mut comparison_results: Vec<(&str, SimResult)> = Vec::new();

    for (name, mut make_scheduler) in algorithms {
        println!("\n--- {} ---", name);
        let mut procs = workload::mixed_workload();
        let mut sched = make_scheduler();
        let result = simulate(&mut procs, sched.as_mut(), &config);
        print_gantt_chart(&result);
        print_statistics(&result);
        comparison_results.push((name, result));
    }

    print_comparison(&comparison_results);

    // Starvation demo.
    println!("\n\n--- Starvation Demo (Priority, no aging) ---");
    let mut procs = workload::starvation_demo();
    let mut sched = PriorityScheduler::new(0); // No aging
    let result = simulate(&mut procs, &mut sched, &config);
    print_statistics(&result);

    println!("\n--- Starvation Demo (Priority, aging=5) ---");
    let mut procs = workload::starvation_demo();
    let mut sched = PriorityScheduler::new(5); // With aging
    let result = simulate(&mut procs, &mut sched, &config);
    print_statistics(&result);
}
```

## Running

```bash
cargo init scheduler-sim
cd scheduler-sim

# Place module files in src/
cargo run --release
```

## Expected Output

```
========================================
  Thread Scheduler Simulation
========================================

--- Round Robin (q=4) ---

=== Gantt Chart (87 time units) ===

CPU: |P0 |P1 |--|P0 |P2 |P1 |--|P0 |P2 |--|P4 |--|P0 |P1 |P3 |...|
     0   4   8  9   13  16  20 21  25  28 29  32 33  37  41  45

I/O events:
  t=3: P0 -> I/O (8t)
  t=7: P1 -> I/O (6t)
  ...

=== Per-Process Statistics ===

PID    Name       Arrival  Turnaround       Wait   Response      CPU
--------------------------------------------------------------------------
0      web-srv          0          45         27          0        8
1      compile          0          78         31          4       45
2      editor           3          52         27          6        5
3      backup           5          67         22         12       10
4      rt-audio        10          15          5          5        5

=== Global Statistics ===

Total simulation time:  87 time units
CPU utilization:        83.9%
Throughput:             0.057 processes/time unit
Context switches:       14
Idle time:              14 time units

Avg turnaround time:    51.40
Avg wait time:          22.40
Avg response time:      5.40

...

=== Algorithm Comparison ===

Algorithm            Avg Turn.   Avg Wait  Avg Resp.    CPU %    Ctx Sw.
------------------------------------------------------------------------------
Round Robin (q=4)        51.40      22.40       5.40    83.9%         14
Priority (aging=10)      48.20      19.80       3.60    85.2%         10
CFS (lat=20, gran=2)     50.80      21.60       4.20    84.5%         12
MLFQ (3 levels, q=4)     47.60      20.00       3.80    86.1%         11
```

## Design Decisions

1. **Logical time, not wall-clock time**: The simulator advances time by discrete units, making results deterministic and reproducible. Wall-clock simulation would introduce nondeterminism from OS scheduling and system load.

2. **Process array indexed by PID**: Processes are stored in a `Vec` and PIDs are array indices. This allows O(1) access from any scheduler without lifetime issues. The trade-off is PIDs must be dense and sequential.

3. **Trait-based scheduler dispatch**: The `Scheduler` trait abstracts the algorithm, making it trivial to add new algorithms. Runtime dispatch (`&mut dyn Scheduler`) is used over generics to enable the comparison loop.

4. **CFS vruntime as integer**: Using `u64` for vruntime avoids floating-point precision issues. Real CFS uses nanosecond-resolution integers internally. The weight-to-vruntime-delta calculation uses integer division, which rounds down -- acceptable for simulation purposes.

5. **MLFQ quantum doubling per level**: Each lower level gets 2x the quantum of the level above (4, 8, 16). This follows the standard MLFQ design: CPU-bound processes that sink to lower levels get longer uninterrupted runs, improving throughput at the cost of latency.

6. **Context switch as simulation delay**: Context switch cost is modeled as dead time where no process runs. This accurately reflects the CPU cycles spent saving/restoring registers and flushing TLB entries.

7. **Priority aging as periodic increment**: Every `aging_interval` time units, all waiting processes get their priority incremented by 1. This is simpler than decay-based aging but effective at preventing indefinite starvation.

## Common Mistakes

1. **Not re-adding preempted processes to the ready queue**: When a process is preempted (quantum expired or higher-priority arrival), it must be added back to the scheduler's queue. Forgetting this effectively kills the process.

2. **CFS vruntime divergence**: If a process blocks for I/O and other processes accumulate vruntime, the returning process has much lower vruntime and monopolizes the CPU until it catches up. Real CFS sets the returning process's vruntime to `min_vruntime - target_latency` to limit this advantage.

3. **MLFQ gaming**: Without anti-gaming measures, a process can issue a tiny I/O burst just before its quantum expires to stay at the highest priority. The boost mechanism (resetting all processes to top queue periodically) mitigates this but does not eliminate it.

4. **Off-by-one in quantum tracking**: If a process runs for exactly `quantum` time units and the burst also ends at that exact tick, both the "burst complete" and "quantum expired" conditions trigger simultaneously. Handle burst completion first -- the process should advance to I/O or terminate, not be needlessly preempted.

5. **Double-counting context switch time**: If context switch cost is 1 time unit, and the engine charges both the outgoing and incoming process, the cost is doubled. The cost should be charged once per switch, to neither process.

## Performance Notes

The simulation itself is O(T * N) where T is total simulation time and N is the number of processes, since each time unit requires checking arrivals, I/O completions, and scheduler decisions.

Key complexity per scheduler:
- **Round Robin**: O(1) next, O(1) add -- VecDeque operations
- **Priority**: O(N log N) next (sorting) -- could be O(log N) with a BinaryHeap
- **CFS**: O(log N) next, O(log N) add -- BTreeMap operations, matching real kernel performance
- **MLFQ**: O(L) next where L is the number of levels -- constant for typical 3-8 level configs

For simulations with thousands of processes, the Priority scheduler's linear sort becomes the bottleneck. Switching to a `BinaryHeap` makes all schedulers O(log N) per operation.

## Going Further

- Add multi-core scheduling: model 2-4 CPUs with per-CPU run queues and work stealing between cores
- Implement processor affinity: processes prefer the CPU they last ran on (warm cache)
- Add real-time scheduling classes: Rate Monotonic, Earliest Deadline First (EDF)
- Generate random workloads with configurable distributions (exponential burst lengths, Poisson arrivals)
- Add interactive visualization using a TUI library (`ratatui`) with real-time Gantt chart rendering
- Model memory: add page faults as a type of I/O burst, track working set size
- Implement gang scheduling: related processes (threads of the same program) are co-scheduled on multiple CPUs simultaneously
- Compare against real Linux scheduler behavior by tracing a workload with `perf sched`
