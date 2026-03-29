# Solution: Epoll/Kqueue Event Loop Reactor

## Architecture Overview

The reactor is structured in three layers:

1. **Poller layer**: Platform-specific abstraction over `epoll` (Linux) or `kqueue` (macOS). Exposes `add`, `modify`, `remove`, and `wait` operations. All interaction with OS syscalls happens here.

2. **Reactor layer**: Owns the poller, the callback registry, and the event loop. Maps file descriptors to handlers, dispatches events, manages timers. Single-threaded -- no synchronization needed.

3. **Application layer**: Echo server built on top of the reactor. Manages per-connection state (read/write buffers), handles accept, read, write, and disconnect.

```
                    Event Loop
                    +--------------------+
                    |     Reactor        |
                    |  +--------------+  |
                    |  | Poller       |  |
                    |  | (epoll/kq)   |  |
                    |  +--------------+  |
                    |  | Handlers     |  |
                    |  | HashMap<Fd,  |  |
                    |  |   Callback>  |  |
                    |  +--------------+  |
                    |  | Timers       |  |
                    |  +--------------+  |
                    +--------------------+
                           |
              +------------+------------+
              |            |            |
         Listener     Connection    Timer
         (accept)    (read/write)  (periodic)
```

## Rust Solution (macOS -- kqueue)

This solution targets macOS using kqueue. A Linux epoll variant is included after as an alternative implementation of the `Poller` trait.

```rust
// src/poller.rs -- Platform abstraction

use std::os::unix::io::RawFd;
use std::io;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Interest {
    Read,
    Write,
    ReadWrite,
}

#[derive(Debug, Clone, Copy)]
pub struct Event {
    pub fd: RawFd,
    pub readable: bool,
    pub writable: bool,
    pub error: bool,
}

pub trait Poller {
    fn new() -> io::Result<Self> where Self: Sized;
    fn add(&mut self, fd: RawFd, interest: Interest) -> io::Result<()>;
    fn modify(&mut self, fd: RawFd, interest: Interest) -> io::Result<()>;
    fn remove(&mut self, fd: RawFd) -> io::Result<()>;
    fn wait(&mut self, events: &mut Vec<Event>, timeout_ms: Option<i32>) -> io::Result<usize>;
    fn add_timer(&mut self, id: usize, interval_ms: u64, oneshot: bool) -> io::Result<()>;
    fn remove_timer(&mut self, id: usize) -> io::Result<()>;
}
```

```rust
// src/kqueue_poller.rs -- macOS implementation

use crate::poller::*;
use std::os::unix::io::RawFd;
use std::io;

pub struct KqueuePoller {
    kq: RawFd,
}

impl Poller for KqueuePoller {
    fn new() -> io::Result<Self> {
        // SAFETY: kqueue() creates a new kernel event queue. Returns -1 on error.
        let kq = unsafe { libc::kqueue() };
        if kq < 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(KqueuePoller { kq })
    }

    fn add(&mut self, fd: RawFd, interest: Interest) -> io::Result<()> {
        let mut changes = Vec::new();

        if matches!(interest, Interest::Read | Interest::ReadWrite) {
            changes.push(make_kevent(
                fd as usize,
                libc::EVFILT_READ,
                libc::EV_ADD | libc::EV_ENABLE,
            ));
        }
        if matches!(interest, Interest::Write | Interest::ReadWrite) {
            changes.push(make_kevent(
                fd as usize,
                libc::EVFILT_WRITE,
                libc::EV_ADD | libc::EV_ENABLE,
            ));
        }

        kevent_apply(self.kq, &changes)
    }

    fn modify(&mut self, fd: RawFd, interest: Interest) -> io::Result<()> {
        // Remove both, then add desired.
        let remove_both = vec![
            make_kevent(fd as usize, libc::EVFILT_READ, libc::EV_DELETE),
            make_kevent(fd as usize, libc::EVFILT_WRITE, libc::EV_DELETE),
        ];
        // Ignore errors on delete (filter may not be registered).
        let _ = kevent_apply(self.kq, &remove_both);
        self.add(fd, interest)
    }

    fn remove(&mut self, fd: RawFd) -> io::Result<()> {
        let changes = vec![
            make_kevent(fd as usize, libc::EVFILT_READ, libc::EV_DELETE),
            make_kevent(fd as usize, libc::EVFILT_WRITE, libc::EV_DELETE),
        ];
        let _ = kevent_apply(self.kq, &changes);
        Ok(())
    }

    fn wait(&mut self, events: &mut Vec<Event>, timeout_ms: Option<i32>) -> io::Result<usize> {
        let mut raw_events: [libc::kevent; 256] = unsafe { std::mem::zeroed() };

        let timeout = timeout_ms.map(|ms| libc::timespec {
            tv_sec: (ms / 1000) as libc::time_t,
            tv_nsec: ((ms % 1000) * 1_000_000) as libc::c_long,
        });
        let timeout_ptr = timeout
            .as_ref()
            .map(|t| t as *const _)
            .unwrap_or(std::ptr::null());

        // SAFETY: kq is a valid kqueue fd, raw_events is a valid buffer.
        let n = unsafe {
            libc::kevent(
                self.kq,
                std::ptr::null(),
                0,
                raw_events.as_mut_ptr(),
                raw_events.len() as i32,
                timeout_ptr,
            )
        };

        if n < 0 {
            let err = io::Error::last_os_error();
            if err.kind() == io::ErrorKind::Interrupted {
                return Ok(0);
            }
            return Err(err);
        }

        events.clear();
        for i in 0..n as usize {
            let ev = &raw_events[i];
            let is_error = (ev.flags & libc::EV_ERROR) != 0 || (ev.flags & libc::EV_EOF) != 0;

            if ev.filter == libc::EVFILT_TIMER {
                events.push(Event {
                    fd: -(ev.ident as i32), // Negative to distinguish timers
                    readable: true,
                    writable: false,
                    error: false,
                });
                continue;
            }

            events.push(Event {
                fd: ev.ident as RawFd,
                readable: ev.filter == libc::EVFILT_READ,
                writable: ev.filter == libc::EVFILT_WRITE,
                error: is_error,
            });
        }

        Ok(n as usize)
    }

    fn add_timer(&mut self, id: usize, interval_ms: u64, oneshot: bool) -> io::Result<()> {
        let mut flags = libc::EV_ADD | libc::EV_ENABLE;
        if oneshot {
            flags |= libc::EV_ONESHOT;
        }
        let mut ev = make_kevent(id, libc::EVFILT_TIMER, flags);
        ev.data = interval_ms as i64;
        kevent_apply(self.kq, &[ev])
    }

    fn remove_timer(&mut self, id: usize) -> io::Result<()> {
        let ev = make_kevent(id, libc::EVFILT_TIMER, libc::EV_DELETE);
        kevent_apply(self.kq, &[ev])
    }
}

impl Drop for KqueuePoller {
    fn drop(&mut self) {
        // SAFETY: self.kq is a valid file descriptor.
        unsafe { libc::close(self.kq) };
    }
}

fn make_kevent(ident: usize, filter: i16, flags: u16) -> libc::kevent {
    libc::kevent {
        ident,
        filter,
        flags,
        fflags: 0,
        data: 0,
        udata: std::ptr::null_mut(),
    }
}

fn kevent_apply(kq: RawFd, changes: &[libc::kevent]) -> io::Result<()> {
    // SAFETY: kq is valid, changes is a valid slice of kevent structs.
    let ret = unsafe {
        libc::kevent(
            kq,
            changes.as_ptr(),
            changes.len() as i32,
            std::ptr::null_mut(),
            0,
            std::ptr::null(),
        )
    };
    if ret < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}
```

```rust
// src/reactor.rs

use crate::poller::*;
use std::collections::{HashMap, VecDeque};
use std::os::unix::io::RawFd;
use std::io;

type EventCallback = Box<dyn FnMut(&mut ReactorInner, RawFd)>;
type TimerCallback = Box<dyn FnMut(&mut ReactorInner)>;

pub struct ReactorInner {
    pub actions: Vec<ReactorAction>,
}

pub enum ReactorAction {
    Register(RawFd, Interest, EventCallback),
    Modify(RawFd, Interest),
    Deregister(RawFd),
    Stop,
}

impl ReactorInner {
    pub fn register(&mut self, fd: RawFd, interest: Interest, cb: EventCallback) {
        self.actions.push(ReactorAction::Register(fd, interest, cb));
    }

    pub fn modify(&mut self, fd: RawFd, interest: Interest) {
        self.actions.push(ReactorAction::Modify(fd, interest));
    }

    pub fn deregister(&mut self, fd: RawFd) {
        self.actions.push(ReactorAction::Deregister(fd));
    }

    pub fn stop(&mut self) {
        self.actions.push(ReactorAction::Stop);
    }
}

pub struct Reactor<P: Poller> {
    poller: P,
    handlers: HashMap<RawFd, EventCallback>,
    timer_handlers: HashMap<usize, TimerCallback>,
    running: bool,
}

impl<P: Poller> Reactor<P> {
    pub fn new() -> io::Result<Self> {
        Ok(Reactor {
            poller: P::new()?,
            handlers: HashMap::new(),
            timer_handlers: HashMap::new(),
            running: false,
        })
    }

    pub fn register(
        &mut self,
        fd: RawFd,
        interest: Interest,
        callback: EventCallback,
    ) -> io::Result<()> {
        set_nonblocking(fd)?;
        self.poller.add(fd, interest)?;
        self.handlers.insert(fd, callback);
        Ok(())
    }

    pub fn deregister(&mut self, fd: RawFd) -> io::Result<()> {
        self.poller.remove(fd)?;
        self.handlers.remove(&fd);
        Ok(())
    }

    pub fn modify(&mut self, fd: RawFd, interest: Interest) -> io::Result<()> {
        self.poller.modify(fd, interest)
    }

    pub fn add_timer(
        &mut self,
        id: usize,
        interval_ms: u64,
        oneshot: bool,
        callback: TimerCallback,
    ) -> io::Result<()> {
        self.poller.add_timer(id, interval_ms, oneshot)?;
        self.timer_handlers.insert(id, callback);
        Ok(())
    }

    pub fn run(&mut self) -> io::Result<()> {
        self.running = true;
        let mut events = Vec::with_capacity(256);

        while self.running && !self.handlers.is_empty() {
            self.poller.wait(&mut events, Some(1000))?;

            for event in events.drain(..) {
                if event.fd < 0 {
                    // Timer event (encoded as negative fd)
                    let timer_id = (-event.fd) as usize;
                    if let Some(mut cb) = self.timer_handlers.remove(&timer_id) {
                        let mut inner = ReactorInner { actions: Vec::new() };
                        cb(&mut inner);
                        self.timer_handlers.insert(timer_id, cb);
                        self.process_actions(inner)?;
                    }
                    continue;
                }

                if let Some(mut cb) = self.handlers.remove(&event.fd) {
                    let mut inner = ReactorInner { actions: Vec::new() };
                    cb(&mut inner, event.fd);

                    // Process deferred actions to avoid borrow conflicts.
                    let should_reinsert = self.process_actions_check_fd(
                        &inner.actions, event.fd,
                    );
                    if should_reinsert {
                        self.handlers.insert(event.fd, cb);
                    }
                    self.process_actions(inner)?;
                }
            }
        }
        Ok(())
    }

    fn process_actions_check_fd(&self, actions: &[ReactorAction], fd: RawFd) -> bool {
        !actions.iter().any(|a| matches!(a, ReactorAction::Deregister(f) if *f == fd))
    }

    fn process_actions(&mut self, inner: ReactorInner) -> io::Result<()> {
        for action in inner.actions {
            match action {
                ReactorAction::Register(fd, interest, cb) => {
                    self.register(fd, interest, cb)?;
                }
                ReactorAction::Modify(fd, interest) => {
                    self.modify(fd, interest)?;
                }
                ReactorAction::Deregister(fd) => {
                    self.deregister(fd)?;
                }
                ReactorAction::Stop => {
                    self.running = false;
                }
            }
        }
        Ok(())
    }
}

fn set_nonblocking(fd: RawFd) -> io::Result<()> {
    // SAFETY: fd is a valid file descriptor.
    let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
    if flags < 0 {
        return Err(io::Error::last_os_error());
    }
    let ret = unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) };
    if ret < 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(())
}
```

```rust
// src/echo_server.rs

use crate::poller::*;
use crate::reactor::*;
use std::collections::HashMap;
use std::io::{self, Read, Write};
use std::net::TcpListener;
use std::os::unix::io::{AsRawFd, FromRawFd, RawFd};
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

struct ConnectionState {
    write_buf: VecDeque<u8>,
}

pub fn run_echo_server<P: Poller + 'static>(addr: &str) -> io::Result<()> {
    let listener = TcpListener::bind(addr)?;
    listener.set_nonblocking(true)?;
    let listener_fd = listener.as_raw_fd();

    let connections: Arc<Mutex<HashMap<RawFd, ConnectionState>>> =
        Arc::new(Mutex::new(HashMap::new()));

    let mut reactor = Reactor::<P>::new()?;

    let conns = connections.clone();
    let accept_cb: Box<dyn FnMut(&mut ReactorInner, RawFd)> = Box::new(move |inner, _fd| {
        // Accept all pending connections (non-blocking accept loop).
        loop {
            // SAFETY: listener_fd is a valid listening socket.
            let client_fd = unsafe {
                libc::accept(listener_fd, std::ptr::null_mut(), std::ptr::null_mut())
            };
            if client_fd < 0 {
                let err = io::Error::last_os_error();
                if err.kind() == io::ErrorKind::WouldBlock {
                    break;
                }
                eprintln!("accept error: {}", err);
                break;
            }

            let conns_inner = conns.clone();
            conns_inner.lock().unwrap().insert(
                client_fd,
                ConnectionState { write_buf: VecDeque::new() },
            );

            let conns_rw = conns_inner.clone();
            let read_cb: Box<dyn FnMut(&mut ReactorInner, RawFd)> =
                Box::new(move |inner, fd| {
                    let mut buf = [0u8; 4096];
                    loop {
                        // SAFETY: fd is valid, buf is a valid write target.
                        let n = unsafe {
                            libc::read(fd, buf.as_mut_ptr() as *mut _, buf.len())
                        };

                        if n < 0 {
                            let err = io::Error::last_os_error();
                            if err.kind() == io::ErrorKind::WouldBlock {
                                break;
                            }
                            close_connection(inner, fd, &conns_rw);
                            return;
                        }
                        if n == 0 {
                            close_connection(inner, fd, &conns_rw);
                            return;
                        }

                        let data = &buf[..n as usize];
                        let mut conns_lock = conns_rw.lock().unwrap();
                        if let Some(state) = conns_lock.get_mut(&fd) {
                            state.write_buf.extend(data);
                        }
                        drop(conns_lock);

                        try_flush(fd, &conns_rw);

                        let conns_lock = conns_rw.lock().unwrap();
                        let has_pending = conns_lock
                            .get(&fd)
                            .map(|s| !s.write_buf.is_empty())
                            .unwrap_or(false);
                        drop(conns_lock);

                        if has_pending {
                            inner.modify(fd, Interest::ReadWrite);
                        }
                    }
                });

            inner.register(client_fd, Interest::Read, read_cb);
        }
    });

    reactor.register(listener_fd, Interest::Read, accept_cb)?;

    println!("Echo server listening on {}", addr);
    reactor.run()?;

    // Prevent listener from being closed by both the TcpListener and reactor.
    std::mem::forget(listener);
    Ok(())
}

fn try_flush(fd: RawFd, conns: &Arc<Mutex<HashMap<RawFd, ConnectionState>>>) {
    let mut conns_lock = conns.lock().unwrap();
    if let Some(state) = conns_lock.get_mut(&fd) {
        while !state.write_buf.is_empty() {
            let (first, _) = state.write_buf.as_slices();
            if first.is_empty() {
                break;
            }
            // SAFETY: fd is valid, first is a valid read source.
            let n = unsafe { libc::write(fd, first.as_ptr() as *const _, first.len()) };
            if n < 0 {
                let err = io::Error::last_os_error();
                if err.kind() == io::ErrorKind::WouldBlock {
                    break;
                }
                return;
            }
            let written = n as usize;
            state.write_buf.drain(..written);
        }
    }
}

fn close_connection(
    inner: &mut ReactorInner,
    fd: RawFd,
    conns: &Arc<Mutex<HashMap<RawFd, ConnectionState>>>,
) {
    inner.deregister(fd);
    conns.lock().unwrap().remove(&fd);
    // SAFETY: fd is a valid file descriptor that we own.
    unsafe { libc::close(fd) };
}
```

```rust
// src/main.rs

mod poller;
#[cfg(target_os = "macos")]
mod kqueue_poller;
mod reactor;
mod echo_server;

#[cfg(target_os = "macos")]
use kqueue_poller::KqueuePoller;

fn main() -> std::io::Result<()> {
    #[cfg(target_os = "macos")]
    echo_server::run_echo_server::<KqueuePoller>("127.0.0.1:9000")?;

    #[cfg(target_os = "linux")]
    echo_server::run_echo_server::<epoll_poller::EpollPoller>("127.0.0.1:9000")?;

    Ok(())
}
```

## Linux Epoll Variant (Poller trait implementation)

```rust
// src/epoll_poller.rs -- Linux implementation

use crate::poller::*;
use std::os::unix::io::RawFd;
use std::io;
use std::collections::HashMap;

pub struct EpollPoller {
    epfd: RawFd,
    timer_fds: HashMap<usize, RawFd>,
}

impl Poller for EpollPoller {
    fn new() -> io::Result<Self> {
        // SAFETY: epoll_create1(0) creates a new epoll instance.
        let epfd = unsafe { libc::epoll_create1(0) };
        if epfd < 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(EpollPoller { epfd, timer_fds: HashMap::new() })
    }

    fn add(&mut self, fd: RawFd, interest: Interest) -> io::Result<()> {
        let events = interest_to_epoll(interest);
        let mut ev = libc::epoll_event { events, u64: fd as u64 };
        // SAFETY: epfd is valid, fd is a valid file descriptor.
        let ret = unsafe { libc::epoll_ctl(self.epfd, libc::EPOLL_CTL_ADD, fd, &mut ev) };
        if ret < 0 { Err(io::Error::last_os_error()) } else { Ok(()) }
    }

    fn modify(&mut self, fd: RawFd, interest: Interest) -> io::Result<()> {
        let events = interest_to_epoll(interest);
        let mut ev = libc::epoll_event { events, u64: fd as u64 };
        let ret = unsafe { libc::epoll_ctl(self.epfd, libc::EPOLL_CTL_MOD, fd, &mut ev) };
        if ret < 0 { Err(io::Error::last_os_error()) } else { Ok(()) }
    }

    fn remove(&mut self, fd: RawFd) -> io::Result<()> {
        let ret = unsafe {
            libc::epoll_ctl(self.epfd, libc::EPOLL_CTL_DEL, fd, std::ptr::null_mut())
        };
        if ret < 0 { Err(io::Error::last_os_error()) } else { Ok(()) }
    }

    fn wait(&mut self, events: &mut Vec<Event>, timeout_ms: Option<i32>) -> io::Result<usize> {
        let mut raw: [libc::epoll_event; 256] = unsafe { std::mem::zeroed() };
        let timeout = timeout_ms.unwrap_or(-1);

        let n = unsafe { libc::epoll_wait(self.epfd, raw.as_mut_ptr(), 256, timeout) };
        if n < 0 {
            let err = io::Error::last_os_error();
            if err.kind() == io::ErrorKind::Interrupted { return Ok(0); }
            return Err(err);
        }

        events.clear();
        for i in 0..n as usize {
            let ev = &raw[i];
            let fd = ev.u64 as RawFd;

            // Check if this is a timer fd
            let is_timer = self.timer_fds.values().any(|&tfd| tfd == fd);
            if is_timer {
                // Read timerfd to acknowledge
                let mut buf = [0u8; 8];
                unsafe { libc::read(fd, buf.as_mut_ptr() as *mut _, 8) };
                let timer_id = self.timer_fds.iter()
                    .find(|(_, &tfd)| tfd == fd)
                    .map(|(&id, _)| id)
                    .unwrap();
                events.push(Event {
                    fd: -(timer_id as i32),
                    readable: true, writable: false, error: false,
                });
                continue;
            }

            events.push(Event {
                fd,
                readable: (ev.events & libc::EPOLLIN as u32) != 0,
                writable: (ev.events & libc::EPOLLOUT as u32) != 0,
                error: (ev.events & (libc::EPOLLERR | libc::EPOLLHUP) as u32) != 0,
            });
        }
        Ok(n as usize)
    }

    fn add_timer(&mut self, id: usize, interval_ms: u64, oneshot: bool) -> io::Result<()> {
        // SAFETY: timerfd_create creates a timer file descriptor.
        let tfd = unsafe {
            libc::timerfd_create(libc::CLOCK_MONOTONIC, libc::TFD_NONBLOCK)
        };
        if tfd < 0 { return Err(io::Error::last_os_error()); }

        let secs = (interval_ms / 1000) as i64;
        let nsecs = ((interval_ms % 1000) * 1_000_000) as i64;

        let it_value = libc::timespec { tv_sec: secs, tv_nsec: nsecs };
        let it_interval = if oneshot {
            libc::timespec { tv_sec: 0, tv_nsec: 0 }
        } else {
            libc::timespec { tv_sec: secs, tv_nsec: nsecs }
        };

        let spec = libc::itimerspec { it_interval, it_value };
        let ret = unsafe {
            libc::timerfd_settime(tfd, 0, &spec, std::ptr::null_mut())
        };
        if ret < 0 {
            unsafe { libc::close(tfd) };
            return Err(io::Error::last_os_error());
        }

        self.timer_fds.insert(id, tfd);
        self.add(tfd, Interest::Read)
    }

    fn remove_timer(&mut self, id: usize) -> io::Result<()> {
        if let Some(tfd) = self.timer_fds.remove(&id) {
            self.remove(tfd)?;
            unsafe { libc::close(tfd) };
        }
        Ok(())
    }
}

impl Drop for EpollPoller {
    fn drop(&mut self) {
        for (_, tfd) in &self.timer_fds {
            unsafe { libc::close(*tfd) };
        }
        unsafe { libc::close(self.epfd) };
    }
}

fn interest_to_epoll(interest: Interest) -> u32 {
    match interest {
        Interest::Read => libc::EPOLLIN as u32,
        Interest::Write => libc::EPOLLOUT as u32,
        Interest::ReadWrite => (libc::EPOLLIN | libc::EPOLLOUT) as u32,
    }
}
```

## Load Test Client

```rust
// src/load_test.rs

use std::io::{Read, Write};
use std::net::TcpStream;
use std::thread;
use std::time::{Duration, Instant};

fn main() {
    let target_connections = 10_000;
    let addr = "127.0.0.1:9000";
    let message = b"Hello from load test client!\n";

    println!("Opening {} connections to {}", target_connections, addr);
    let start = Instant::now();

    let mut streams: Vec<TcpStream> = Vec::with_capacity(target_connections);

    for i in 0..target_connections {
        match TcpStream::connect(addr) {
            Ok(stream) => {
                stream.set_nonblocking(false).unwrap();
                stream.set_read_timeout(Some(Duration::from_secs(5))).unwrap();
                streams.push(stream);
            }
            Err(e) => {
                eprintln!("Connection {} failed: {}. Check ulimit -n.", i, e);
                break;
            }
        }
        if (i + 1) % 1000 == 0 {
            println!("  {} connections established...", i + 1);
        }
    }

    let connected = streams.len();
    let connect_time = start.elapsed();
    println!("{} connections in {:?}", connected, connect_time);

    // Send and verify echo
    let mut success = 0;
    for stream in &mut streams {
        if stream.write_all(message).is_ok() {
            let mut buf = vec![0u8; message.len()];
            if stream.read_exact(&mut buf).is_ok() && buf == message {
                success += 1;
            }
        }
    }

    println!("{}/{} echoed correctly", success, connected);
    println!("Total time: {:?}", start.elapsed());
}
```

## Running the Solution

```bash
cargo new event-reactor && cd event-reactor

# Add to Cargo.toml:
# [dependencies]
# libc = "0.2"

# Copy source files into src/

# Increase file descriptor limit for 10K test
ulimit -n 65536

# Terminal 1: run the server
cargo run --release --bin server

# Terminal 2: run the load test
cargo run --release --bin load_test
```

### Expected Output (server)

```
Echo server listening on 127.0.0.1:9000
```

### Expected Output (load test)

```
Opening 10000 connections to 127.0.0.1:9000
  1000 connections established...
  2000 connections established...
  ...
  10000 connections established...
10000 connections in 1.23s
10000/10000 echoed correctly
Total time: 3.45s
```

## Design Decisions

1. **Poller trait for platform abstraction**: Rather than `#[cfg]` sprinkled throughout the reactor, the platform-specific code is isolated in the `Poller` trait implementation. The reactor is generic over `P: Poller`.

2. **Callback removal pattern**: When dispatching an event, the callback is temporarily removed from the HashMap. This avoids the problem of the callback holding a mutable reference to the reactor while the reactor's HashMap also needs mutation. After the callback returns, it is re-inserted (unless the fd was deregistered).

3. **ReactorInner indirection**: Callbacks receive `&mut ReactorInner` instead of `&mut Reactor`. ReactorInner queues actions (register, deregister, modify) that the reactor processes after the callback returns. This decouples callback execution from reactor mutation.

4. **Level-triggered mode**: The implementation uses level-triggered notifications (the default for both epoll and kqueue). This is simpler to reason about -- if data is still available, the next `wait` will return the event again. Edge-triggered is more efficient but requires draining all data in a single callback invocation.

5. **Timer encoding**: Timer events are distinguished from fd events by using negative fd values. This avoids adding a separate event type and keeps the dispatch logic simple.

## Common Mistakes

1. **Not accepting all pending connections**: When the listener is readable, there may be multiple pending connections. Calling `accept` once and returning to the event loop means subsequent connections are only accepted on the next `wait` cycle. Always loop until `EAGAIN`.

2. **Forgetting to set non-blocking mode**: If a socket is left blocking, `read` or `write` will block the entire event loop, defeating the purpose. Every accepted socket must be set to `O_NONBLOCK` immediately.

3. **Write buffer not flushed**: After a write completes partially, you must register for write-readiness and resume when notified. Dropping the unsent data silently corrupts the echo.

4. **File descriptor leak**: When a connection errors or the client disconnects, the fd must be removed from the reactor AND closed. Missing either step leaks resources.

5. **Callback borrow conflicts**: If the callback tries to call `reactor.register()` directly while the reactor is iterating events, you get a borrow checker error. The ReactorInner action queue solves this.

## Performance Notes

- **Single-threaded throughput**: A well-implemented reactor on modern hardware handles 100,000+ events/sec on a single core. The bottleneck is usually the `epoll_wait`/`kevent` syscall itself, not the dispatch logic.
- **Event batch size**: The 256-event batch in `wait` is a good balance. Too small means more syscalls; too large wastes stack space. Production servers like Nginx use 512.
- **Memory per connection**: Each connection needs a write buffer (VecDeque) and a callback closure. At 10K connections with 4KB buffers, that is ~40MB -- well within reasonable limits.
- **Syscall overhead**: Each `epoll_ctl`/`kevent` change is a syscall. Batching changes (kevent supports this natively; for epoll, consider `io_uring`) reduces overhead.
