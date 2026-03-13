<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Seccomp Filter Engine

## The Challenge

Build a seccomp (secure computing mode) filter engine in Go that generates and installs BPF (classic Berkeley Packet Filter) programs to restrict the system calls a process can make, implementing a sandboxing system similar to those used in container runtimes, web browsers, and systemd services. Seccomp-BPF allows a process to install a filter that intercepts every system call and decides whether to allow it, deny it with a specific error code, kill the process, or log it. Your engine must support a policy DSL (defined in YAML or Go structs) that describes allowed and denied syscalls with argument-level filtering, compile the policy into BPF bytecode, install it via `prctl(PR_SET_SECCOMP)`, and handle the interactions with Go's multi-threaded runtime.

## Requirements

1. Implement a BPF assembler that generates classic BPF bytecode (not eBPF) for seccomp filters: support `BPF_LD` (load syscall number and arguments from `seccomp_data`), `BPF_JMP` (conditional jumps for comparing syscall numbers and argument values), and `BPF_RET` (return allow/deny/kill/trap/log verdicts).
2. Define a policy DSL using Go structs: `Policy` contains a default action (allow/deny/kill) and a list of `Rule` entries, each specifying a syscall name, an action, and optional argument filters (e.g., "allow `openat` only if the flags argument does not include `O_WRONLY`").
3. Compile the policy into BPF bytecode: generate a linear BPF program that loads the syscall number from `seccomp_data.nr`, compares it against each rule using conditional jumps, and returns the appropriate verdict. Optimize the generated code by grouping syscalls with the same action.
4. Implement argument filtering: for rules with argument conditions, generate BPF instructions that load the argument from `seccomp_data.args[i]` (handling the 64-bit split into two 32-bit loads on 32-bit architectures) and compare against the specified value using the specified operator (==, !=, &, <, >).
5. Install the filter using `prctl(PR_SET_NO_NEW_PRIVS, 1)` followed by `prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog)` where `prog` is a `sock_fprog` struct pointing to the BPF bytecode array.
6. Handle Go's multi-threaded runtime: seccomp filters are per-thread in Linux, but Go goroutines can run on any OS thread. Use `runtime.LockOSThread()` and apply the filter to the current thread, or use `SECCOMP_FILTER_FLAG_TSYNC` to synchronize the filter across all threads.
7. Implement a `SECCOMP_RET_TRACE` mode that uses ptrace to intercept filtered syscalls, allowing a supervisor process to inspect and modify arguments before deciding to allow or deny the call.
8. Provide a YAML-based policy file format and a CLI tool that loads a policy, installs the filter, and execs a target program in the sandbox: `sandbox --policy policy.yaml -- /usr/bin/target`.

## Hints

- The `seccomp_data` struct that the BPF program operates on contains: `nr` (syscall number, 4 bytes at offset 0), `arch` (architecture, 4 bytes at offset 4), and `args[6]` (syscall arguments, 8 bytes each starting at offset 8).
- BPF instructions are 8 bytes each: `struct sock_filter { uint16 code; uint8 jt; uint8 jf; uint32 k; }`.
- Common BPF instruction patterns: `BPF_LD | BPF_W | BPF_ABS` (load word at absolute offset), `BPF_JMP | BPF_JEQ | BPF_K` (jump if equal to constant), `BPF_RET | BPF_K` (return constant verdict).
- Always check the architecture first (`seccomp_data.arch == AUDIT_ARCH_X86_64`) to prevent syscall number confusion across architectures.
- Syscall numbers differ between architectures; use `x/sys/unix` package constants or a mapping table.
- `SECCOMP_FILTER_FLAG_TSYNC` (since Linux 3.17) synchronizes the filter across all threads, which is essential for Go programs.
- Test carefully: an overly restrictive filter will kill the Go runtime itself (it needs syscalls like `futex`, `sigaltstack`, `mmap`, `clone`).
- Start with a permissive default (allow all) and deny specific syscalls, then work toward a restrictive default.

## Success Criteria

1. A filter that denies `unlink`/`unlinkat` prevents the sandboxed process from deleting files (returns EPERM).
2. A filter with argument filtering allows `openat` with `O_RDONLY` but denies `openat` with `O_WRONLY`, verified by attempting both operations.
3. The BPF bytecode generated for a policy with 50 rules is correct and passes the kernel's BPF verifier.
4. `SECCOMP_FILTER_FLAG_TSYNC` correctly applies the filter to all Go runtime threads, preventing any goroutine from bypassing the filter.
5. The sandboxed process can still function (Go runtime syscalls are allowed) while target syscalls are denied.
6. The YAML policy file is correctly parsed and compiled into a working filter.
7. The CLI tool successfully execs a target program under the sandbox and the target cannot perform denied operations.
8. The filter handles both x86_64 and arm64 architectures by checking `seccomp_data.arch`.

## Research Resources

- Linux seccomp man page -- `man 2 seccomp`, `man 2 prctl`
- Linux kernel seccomp documentation -- https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html
- "BPF (Berkeley Packet Filter)" -- `man 7 bpf` (classic BPF, not eBPF)
- Docker default seccomp profile -- https://docs.docker.com/engine/security/seccomp/
- libseccomp -- https://github.com/seccomp/libseccomp -- reference implementation
- "Container Security" (Rice, 2020) -- Chapter 8: Seccomp
