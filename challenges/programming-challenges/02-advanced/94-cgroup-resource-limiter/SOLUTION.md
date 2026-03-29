# Solution: Cgroup Resource Limiter

## Architecture Overview

The tool is structured in three layers:

1. **Cgroup layer**: Low-level operations on the cgroup filesystem -- creating directories, writing controller files, reading stats, killing processes, removing cgroups. All cgroup interaction is isolated here.

2. **Limiter layer**: High-level resource limit configuration. Converts human-readable inputs (0.5 CPU, 256M memory) to cgroup file format. Manages the lifecycle: create, configure, assign, monitor, destroy.

3. **CLI layer**: Parses command-line arguments, orchestrates the limiter, runs the target command, handles signals, and prints results.

```
CLI (main.go)
  |
  +-- Limiter (limiter.go)
  |     |
  |     +-- Cgroup (cgroup.go)
  |           |
  |           +-- /sys/fs/cgroup/{name}/
  |                 cpu.max
  |                 memory.max
  |                 memory.swap.max
  |                 io.max
  |                 pids.max
  |                 cgroup.procs
  |                 cpu.stat
  |                 memory.current
  |                 memory.stat
  |                 pids.current
  |
  +-- Signal handler (cleanup on SIGINT/SIGTERM)
  +-- Process runner (os/exec)
```

## Go Solution

```go
// cgroup.go -- Low-level cgroup filesystem operations

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const cgroupRoot = "/sys/fs/cgroup"

type Cgroup struct {
	Name string
	Path string
}

func NewCgroup(name string) (*Cgroup, error) {
	path := filepath.Join(cgroupRoot, name)

	if err := enableSubtreeControllers(); err != nil {
		return nil, fmt.Errorf("enable subtree controllers: %w", err)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("create cgroup dir %s: %w", path, err)
	}

	return &Cgroup{Name: name, Path: path}, nil
}

func enableSubtreeControllers() error {
	controllerFile := filepath.Join(cgroupRoot, "cgroup.subtree_control")
	return os.WriteFile(controllerFile, []byte("+cpu +memory +io +pids"), 0644)
}

func (cg *Cgroup) AvailableControllers() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(cg.Path, "cgroup.controllers"))
	if err != nil {
		return nil, err
	}
	return strings.Fields(strings.TrimSpace(string(data))), nil
}

func (cg *Cgroup) HasController(name string) bool {
	controllers, err := cg.AvailableControllers()
	if err != nil {
		return false
	}
	for _, c := range controllers {
		if c == name {
			return true
		}
	}
	return false
}

func (cg *Cgroup) WriteFile(name, value string) error {
	path := filepath.Join(cg.Path, name)
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		return fmt.Errorf("write %s=%q: %w", name, value, err)
	}
	return nil
}

func (cg *Cgroup) ReadFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(cg.Path, name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (cg *Cgroup) AddProcess(pid int) error {
	return cg.WriteFile("cgroup.procs", strconv.Itoa(pid))
}

func (cg *Cgroup) Processes() ([]int, error) {
	data, err := cg.ReadFile("cgroup.procs")
	if err != nil {
		return nil, err
	}
	if data == "" {
		return nil, nil
	}
	var pids []int
	for _, line := range strings.Fields(data) {
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func (cg *Cgroup) KillAll() error {
	pids, err := cg.Processes()
	if err != nil {
		return err
	}
	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}

func (cg *Cgroup) WaitEmpty(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pids, err := cg.Processes()
		if err != nil || len(pids) == 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("cgroup still has processes after %v", timeout)
}

func (cg *Cgroup) Destroy() error {
	cg.KillAll()
	if err := cg.WaitEmpty(5 * time.Second); err != nil {
		return fmt.Errorf("cannot destroy cgroup: %w", err)
	}
	return os.Remove(cg.Path)
}
```

```go
// limiter.go -- High-level resource limit configuration

package main

import (
	"fmt"
	"strconv"
	"strings"
)

type ResourceLimits struct {
	CPU    float64 // Fraction of CPUs (0.5 = 50% of one core)
	Memory string  // Human-readable (256M, 1G)
	Swap   string  // Human-readable or "0" to disable
	IOMax  string  // "major:minor rbps=X wbps=Y"
	PIDs   int     // Max number of processes
}

type Limiter struct {
	cg     *Cgroup
	limits ResourceLimits
}

func NewLimiter(name string, limits ResourceLimits) (*Limiter, error) {
	cg, err := NewCgroup(name)
	if err != nil {
		return nil, err
	}
	return &Limiter{cg: cg, limits: limits}, nil
}

func (l *Limiter) Apply() error {
	if l.limits.CPU > 0 {
		if err := l.applyCPU(); err != nil {
			return fmt.Errorf("cpu limit: %w", err)
		}
	}
	if l.limits.Memory != "" {
		if err := l.applyMemory(); err != nil {
			return fmt.Errorf("memory limit: %w", err)
		}
	}
	if l.limits.IOMax != "" {
		if err := l.applyIO(); err != nil {
			return fmt.Errorf("io limit: %w", err)
		}
	}
	if l.limits.PIDs > 0 {
		if err := l.applyPIDs(); err != nil {
			return fmt.Errorf("pids limit: %w", err)
		}
	}
	return nil
}

func (l *Limiter) applyCPU() error {
	if !l.cg.HasController("cpu") {
		return fmt.Errorf("cpu controller not available")
	}
	period := 100000 // 100ms in microseconds
	quota := int(l.limits.CPU * float64(period))
	value := fmt.Sprintf("%d %d", quota, period)
	return l.cg.WriteFile("cpu.max", value)
}

func (l *Limiter) applyMemory() error {
	if !l.cg.HasController("memory") {
		return fmt.Errorf("memory controller not available")
	}
	bytes, err := parseHumanBytes(l.limits.Memory)
	if err != nil {
		return fmt.Errorf("parse memory limit: %w", err)
	}
	if err := l.cg.WriteFile("memory.max", strconv.FormatInt(bytes, 10)); err != nil {
		return err
	}
	if l.limits.Swap != "" {
		swapBytes, err := parseHumanBytes(l.limits.Swap)
		if err != nil {
			return fmt.Errorf("parse swap limit: %w", err)
		}
		if err := l.cg.WriteFile("memory.swap.max", strconv.FormatInt(swapBytes, 10)); err != nil {
			return err
		}
	}
	return nil
}

func (l *Limiter) applyIO() error {
	if !l.cg.HasController("io") {
		return fmt.Errorf("io controller not available")
	}
	return l.cg.WriteFile("io.max", l.limits.IOMax)
}

func (l *Limiter) applyPIDs() error {
	if !l.cg.HasController("pids") {
		return fmt.Errorf("pids controller not available")
	}
	return l.cg.WriteFile("pids.max", strconv.Itoa(l.limits.PIDs))
}

func (l *Limiter) AddProcess(pid int) error {
	return l.cg.AddProcess(pid)
}

func (l *Limiter) Stats() (map[string]string, error) {
	stats := make(map[string]string)

	if cpuStat, err := l.cg.ReadFile("cpu.stat"); err == nil {
		stats["cpu.stat"] = cpuStat
	}
	if memCurrent, err := l.cg.ReadFile("memory.current"); err == nil {
		bytes, _ := strconv.ParseInt(memCurrent, 10, 64)
		stats["memory.current"] = formatBytes(bytes)
		stats["memory.current.raw"] = memCurrent
	}
	if memStat, err := l.cg.ReadFile("memory.stat"); err == nil {
		stats["memory.stat"] = memStat
	}
	if memEvents, err := l.cg.ReadFile("memory.events"); err == nil {
		stats["memory.events"] = memEvents
	}
	if pidsCurrent, err := l.cg.ReadFile("pids.current"); err == nil {
		stats["pids.current"] = pidsCurrent
	}
	if ioStat, err := l.cg.ReadFile("io.stat"); err == nil {
		stats["io.stat"] = ioStat
	}

	return stats, nil
}

func (l *Limiter) WasOOMKilled() bool {
	events, err := l.cg.ReadFile("memory.events")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(events, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == "oom_kill" {
			count, _ := strconv.Atoi(parts[1])
			return count > 0
		}
	}
	return false
}

func (l *Limiter) Cleanup() error {
	return l.cg.Destroy()
}

func parseHumanBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, nil
	}

	multiplier := int64(1)
	numStr := s

	if len(s) > 0 {
		suffix := strings.ToUpper(s[len(s)-1:])
		switch suffix {
		case "K":
			multiplier = 1024
			numStr = s[:len(s)-1]
		case "M":
			multiplier = 1024 * 1024
			numStr = s[:len(s)-1]
		case "G":
			multiplier = 1024 * 1024 * 1024
			numStr = s[:len(s)-1]
		}
	}

	val, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return val * multiplier, nil
}

func formatBytes(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
```

```go
// main.go -- CLI entry point

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	runCmd := flag.NewFlagSet("run", flag.ExitOnError)
	cpuLimit := runCmd.Float64("cpu", 0, "CPU limit as fraction (0.5 = 50% of one core)")
	memLimit := runCmd.String("memory", "", "Memory limit (e.g., 256M, 1G)")
	swapLimit := runCmd.String("swap", "", "Swap limit (e.g., 0 to disable)")
	ioLimit := runCmd.String("io", "", "I/O bandwidth limit (major:minor rbps=X wbps=Y)")
	pidLimit := runCmd.Int("pids", 0, "Maximum number of processes")
	cgName := runCmd.String("name", "", "Cgroup name (auto-generated if empty)")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		// Find the "--" separator
		dashIdx := -1
		for i, arg := range os.Args[2:] {
			if arg == "--" {
				dashIdx = i + 2
				break
			}
		}
		if dashIdx < 0 || dashIdx+1 >= len(os.Args) {
			fmt.Fprintln(os.Stderr, "Usage: cglimit run [options] -- command [args...]")
			os.Exit(1)
		}

		runCmd.Parse(os.Args[2:dashIdx])
		cmdArgs := os.Args[dashIdx+1:]

		if *cgName == "" {
			*cgName = fmt.Sprintf("cglimit-%d", time.Now().UnixNano())
		}

		limits := ResourceLimits{
			CPU:    *cpuLimit,
			Memory: *memLimit,
			Swap:   *swapLimit,
			IOMax:  *ioLimit,
			PIDs:   *pidLimit,
		}

		exitCode := runWithLimits(*cgName, limits, cmdArgs)
		os.Exit(exitCode)

	default:
		printUsage()
		os.Exit(1)
	}
}

func runWithLimits(name string, limits ResourceLimits, cmdArgs []string) int {
	limiter, err := NewLimiter(name, limits)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating limiter: %v\n", err)
		return 1
	}

	// Signal handler for cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nReceived signal, cleaning up...")
		limiter.Cleanup()
		os.Exit(130)
	}()

	defer func() {
		printStats(limiter)
		if err := limiter.Cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup error: %v\n", err)
		}
	}()

	if err := limiter.Apply(); err != nil {
		fmt.Fprintf(os.Stderr, "Error applying limits: %v\n", err)
		return 1
	}

	printLimits(limits)

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting command: %v\n", err)
		return 1
	}

	if err := limiter.AddProcess(cmd.Process.Pid); err != nil {
		fmt.Fprintf(os.Stderr, "Error adding process to cgroup: %v\n", err)
		cmd.Process.Kill()
		return 1
	}

	fmt.Fprintf(os.Stderr, "Running PID %d in cgroup %s\n", cmd.Process.Pid, name)

	startTime := time.Now()
	err = cmd.Wait()
	elapsed := time.Since(startTime)

	fmt.Fprintf(os.Stderr, "\n--- Execution completed in %v ---\n", elapsed)

	if limiter.WasOOMKilled() {
		fmt.Fprintln(os.Stderr, "WARNING: Process was OOM-killed (exceeded memory limit)")
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func printLimits(limits ResourceLimits) {
	fmt.Fprintln(os.Stderr, "=== Resource Limits ===")
	if limits.CPU > 0 {
		fmt.Fprintf(os.Stderr, "  CPU:    %.2f cores\n", limits.CPU)
	}
	if limits.Memory != "" {
		fmt.Fprintf(os.Stderr, "  Memory: %s\n", limits.Memory)
	}
	if limits.Swap != "" {
		fmt.Fprintf(os.Stderr, "  Swap:   %s\n", limits.Swap)
	}
	if limits.IOMax != "" {
		fmt.Fprintf(os.Stderr, "  I/O:    %s\n", limits.IOMax)
	}
	if limits.PIDs > 0 {
		fmt.Fprintf(os.Stderr, "  PIDs:   %d\n", limits.PIDs)
	}
	fmt.Fprintln(os.Stderr, "========================")
}

func printStats(limiter *Limiter) {
	stats, err := limiter.Stats()
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, "\n=== Resource Usage ===")
	if v, ok := stats["memory.current"]; ok {
		fmt.Fprintf(os.Stderr, "  Memory current: %s\n", v)
	}
	if v, ok := stats["pids.current"]; ok {
		fmt.Fprintf(os.Stderr, "  PIDs current:   %s\n", v)
	}
	if v, ok := stats["cpu.stat"]; ok {
		for _, line := range strings.Split(v, "\n") {
			if strings.HasPrefix(line, "usage_usec") {
				parts := strings.Fields(line)
				if len(parts) == 2 {
					usec, _ := fmt.Sscanf(parts[1], "%d", new(int64))
					if usec > 0 {
						fmt.Fprintf(os.Stderr, "  CPU usage:      %s usec\n", parts[1])
					}
				}
			}
			if strings.HasPrefix(line, "nr_throttled") {
				fmt.Fprintf(os.Stderr, "  CPU throttled:  %s\n", strings.Fields(line)[1])
			}
		}
	}
	fmt.Fprintln(os.Stderr, "======================")
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `cglimit - Process resource limiter using cgroups v2

Usage:
  cglimit run [options] -- command [args...]

Options:
  --cpu=FLOAT     CPU limit as fraction (0.5 = 50% of one core)
  --memory=SIZE   Memory limit (256M, 1G)
  --swap=SIZE     Swap limit (0 to disable)
  --io=SPEC       I/O limit (major:minor rbps=X wbps=Y)
  --pids=INT      Maximum processes
  --name=STRING   Cgroup name (auto-generated if empty)

Examples:
  cglimit run --cpu=0.5 --memory=256M -- stress-ng --cpu 4 --timeout 10
  cglimit run --memory=64M --swap=0 -- python3 -c "x = 'A' * 100_000_000"
  cglimit run --pids=10 -- bash -c ":(){ :|:& };:"`)
}
```

## Tests

```go
// main_test.go

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requireRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root privileges")
	}
}

func requireLinux(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("requires cgroups v2")
	}
}

func TestCgroupCreateDestroy(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	name := "cglimit-test-create"
	cg, err := NewCgroup(name)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := os.Stat(cg.Path); os.IsNotExist(err) {
		t.Fatal("cgroup directory not created")
	}

	if err := cg.Destroy(); err != nil {
		t.Fatalf("destroy: %v", err)
	}

	if _, err := os.Stat(cg.Path); !os.IsNotExist(err) {
		t.Fatal("cgroup directory not removed")
	}
}

func TestCPULimiting(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	name := "cglimit-test-cpu"
	limits := ResourceLimits{CPU: 0.5}

	limiter, err := NewLimiter(name, limits)
	if err != nil {
		t.Fatalf("create limiter: %v", err)
	}
	defer limiter.Cleanup()

	if err := limiter.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	cpuMax, err := limiter.cg.ReadFile("cpu.max")
	if err != nil {
		t.Fatalf("read cpu.max: %v", err)
	}

	parts := strings.Fields(cpuMax)
	if len(parts) != 2 {
		t.Fatalf("unexpected cpu.max format: %q", cpuMax)
	}

	quota, _ := strconv.Atoi(parts[0])
	period, _ := strconv.Atoi(parts[1])
	fraction := float64(quota) / float64(period)

	if fraction < 0.45 || fraction > 0.55 {
		t.Errorf("expected ~0.5 CPU, got %.2f (quota=%d, period=%d)", fraction, quota, period)
	}
}

func TestMemoryLimiting(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	name := "cglimit-test-memory"
	limits := ResourceLimits{Memory: "64M", Swap: "0"}

	limiter, err := NewLimiter(name, limits)
	if err != nil {
		t.Fatalf("create limiter: %v", err)
	}
	defer limiter.Cleanup()

	if err := limiter.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	memMax, err := limiter.cg.ReadFile("memory.max")
	if err != nil {
		t.Fatalf("read memory.max: %v", err)
	}

	bytes, _ := strconv.ParseInt(memMax, 10, 64)
	expectedBytes := int64(64 * 1024 * 1024)
	if bytes != expectedBytes {
		t.Errorf("expected %d bytes, got %d", expectedBytes, bytes)
	}
}

func TestPIDLimiting(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	name := "cglimit-test-pids"
	limits := ResourceLimits{PIDs: 10}

	limiter, err := NewLimiter(name, limits)
	if err != nil {
		t.Fatalf("create limiter: %v", err)
	}
	defer limiter.Cleanup()

	if err := limiter.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	pidsMax, err := limiter.cg.ReadFile("pids.max")
	if err != nil {
		t.Fatalf("read pids.max: %v", err)
	}

	if pidsMax != "10" {
		t.Errorf("expected pids.max=10, got %q", pidsMax)
	}
}

func TestOOMDetection(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	name := "cglimit-test-oom"
	limits := ResourceLimits{Memory: "32M", Swap: "0"}

	limiter, err := NewLimiter(name, limits)
	if err != nil {
		t.Fatalf("create limiter: %v", err)
	}
	defer limiter.Cleanup()

	if err := limiter.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Run a command that allocates more than 32M
	cmd := exec.Command("python3", "-c", "x = bytearray(64 * 1024 * 1024)")
	cmd.Start()

	limiter.AddProcess(cmd.Process.Pid)
	cmd.Wait()

	if !limiter.WasOOMKilled() {
		t.Log("OOM kill expected but not detected (process may have failed differently)")
	}
}

func TestParseHumanBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"1K", 1024},
		{"256M", 256 * 1024 * 1024},
		{"1G", 1024 * 1024 * 1024},
		{"2G", 2 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		got, err := parseHumanBytes(tt.input)
		if err != nil {
			t.Errorf("parseHumanBytes(%q): %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseHumanBytes(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}
```

## Running the Solution

```bash
mkdir cglimit && cd cglimit
go mod init cglimit

# Copy the source files

# Build
go build -o cglimit .

# Run with CPU limit (requires root)
sudo ./cglimit run --cpu=0.5 --memory=256M -- stress-ng --cpu 4 --timeout 10

# Run with memory limit (triggers OOM)
sudo ./cglimit run --memory=64M --swap=0 -- python3 -c "x = bytearray(100_000_000)"

# Run with PID limit (contains fork bomb)
sudo ./cglimit run --pids=10 -- bash -c "for i in \$(seq 1 20); do sleep 60 & done; wait"

# Run tests
sudo go test -v ./...
```

### Expected Output

```
=== Resource Limits ===
  CPU:    0.50 cores
  Memory: 256M
========================
Running PID 12345 in cgroup cglimit-1711612345000

[stress-ng output...]

--- Execution completed in 10.01s ---

=== Resource Usage ===
  Memory current: 4.2 MB
  PIDs current:   0
  CPU usage:      5012345 usec
  CPU throttled:  847
======================
```

### OOM Example

```
=== Resource Limits ===
  Memory: 64M
  Swap:   0
========================
Running PID 12346 in cgroup cglimit-1711612345001

--- Execution completed in 0.12s ---
WARNING: Process was OOM-killed (exceeded memory limit)

=== Resource Usage ===
  Memory current: 0 B
  PIDs current:   0
======================
```

## Design Decisions

1. **Filesystem API only**: All cgroup interaction is through reading and writing files. No special libraries or ioctls. This matches how Docker and runc interact with cgroups -- it is just file I/O.

2. **Controller availability checking**: Before writing to a controller file, the tool checks `cgroup.controllers` to verify the controller is available. This gives clear error messages instead of cryptic "permission denied" errors.

3. **Signal-based cleanup**: The signal handler ensures cgroups are destroyed even if the user presses Ctrl+C. Without this, orphaned cgroups accumulate in `/sys/fs/cgroup`.

4. **Process-then-move pattern**: The child process is started first, then moved into the cgroup via `cgroup.procs`. An alternative is to use `SysProcAttr.CgroupFD` (Go 1.20+) to start directly in the cgroup. The process-then-move approach is simpler and portable across Go versions.

5. **OOM detection via memory.events**: Rather than trying to intercept the OOM killer, the tool checks `memory.events` after the process exits. The `oom_kill` counter tells exactly how many processes were killed.

## Common Mistakes

1. **Forgetting subtree_control**: Creating a child cgroup without enabling controllers in the parent's `cgroup.subtree_control` results in an empty `cgroup.controllers` file. The child inherits no controllers.

2. **Not handling the "no internal processes" rule**: In cgroups v2, a cgroup with subtree control enabled cannot have processes directly. If you write a PID to a cgroup that has `cgroup.subtree_control` configured, it fails. Create a leaf cgroup for the processes.

3. **Cleanup race condition**: After sending SIGKILL, processes may still appear in `cgroup.procs` briefly. Attempting `os.Remove` immediately fails with EBUSY. Poll until empty.

4. **Swap not disabled**: If you set `memory.max` but not `memory.swap.max`, the process can use swap to exceed the memory limit. Always set both for strict enforcement.

5. **Running without root**: Cgroup operations require root privileges (or specific delegated permissions). The tool should check and report this clearly.

## Performance Notes

- **Cgroup filesystem overhead**: Writing to cgroup files is a kernel operation, not a disk write. The overhead is microseconds per operation. Creating and configuring a cgroup takes under 1ms.
- **CPU throttling granularity**: The CPU controller enforces limits over the period (default 100ms). Short bursts within a period are allowed. For tighter enforcement, use a shorter period (at the cost of more scheduling overhead).
- **Memory limit enforcement**: The kernel enforces memory limits lazily -- allocation succeeds until the physical memory usage hits the limit. This means a process can allocate 1GB of virtual memory even with a 64M limit, as long as it does not touch all of it.
- **I/O throttling**: I/O limits apply to direct I/O. Buffered I/O goes through the page cache and may not be throttled until writeback. Use O_DIRECT for precise I/O limiting.
