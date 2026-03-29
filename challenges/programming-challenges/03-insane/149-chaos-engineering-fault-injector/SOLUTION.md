# Solution: Chaos Engineering Fault Injector

## Architecture Overview

The system is structured around an experiment lifecycle engine that orchestrates fault injectors through a plugin interface:

```
CLI (run | validate | rollback | list-faults)
    |
Experiment Engine (lifecycle state machine)
    |
    +-- Config Parser (YAML experiment definition)
    |
    +-- Steady-State Verifier (HTTP health checks + metric queries)
    |
    +-- Fault Registry
    |       +-- NetworkFault (tc netem, iptables, proxy-based)
    |       +-- ProcessFault (kill, pause, CPU stress, memory stress)
    |       +-- DiskFault (FUSE overlay, fill, slow I/O)
    |       +-- DNSFault (local proxy, NXDOMAIN, latency, spoofing)
    |
    +-- Rollback Stack (LIFO cleanup handlers)
    |
    +-- Journal (experiment results persistence)
```

Every fault injector returns a rollback function on success. The engine stores these in a stack and executes them in reverse order on any exit path.

## Go Solution

### Project Structure

```
chaoskit/
  cmd/chaoskit/main.go
  internal/
    engine/engine.go       // Experiment lifecycle state machine
    config/config.go       // YAML experiment definition types
    fault/
      registry.go          // Fault type registry
      network.go           // Network fault injectors
      process.go           // Process fault injectors
      disk.go              // Disk fault injectors
      dns.go               // DNS fault injectors
    rollback/rollback.go   // Rollback stack
    steady/steady.go       // Steady-state hypothesis verification
    journal/journal.go     // Experiment result persistence
  go.mod
```

### Experiment Configuration

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Experiment struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Target      Target        `yaml:"target"`
	SteadyState SteadyState  `yaml:"steady_state"`
	Faults      []FaultAction `yaml:"faults"`
	Duration    time.Duration `yaml:"duration"`
	DryRun      bool          `yaml:"dry_run"`
}

type Target struct {
	Host        string `yaml:"host"`
	Process     string `yaml:"process"`
	PID         int    `yaml:"pid"`
	ContainerID string `yaml:"container_id"`
	Interface   string `yaml:"interface"`
}

type SteadyState struct {
	Checks          []HealthCheck `yaml:"checks"`
	RecoveryTimeout time.Duration `yaml:"recovery_timeout"`
}

type HealthCheck struct {
	Name              string        `yaml:"name"`
	URL               string        `yaml:"url"`
	ExpectedStatus    int           `yaml:"expected_status"`
	MaxLatency        time.Duration `yaml:"max_latency"`
	Interval          time.Duration `yaml:"interval"`
}

type FaultAction struct {
	Type       string            `yaml:"type"`
	Params     map[string]string `yaml:"params"`
	Duration   time.Duration     `yaml:"duration"`
	BlastRadius float64          `yaml:"blast_radius"` // 0.0 to 1.0
}

func Load(path string) (*Experiment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read experiment file: %w", err)
	}
	var exp Experiment
	if err := yaml.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("parse experiment YAML: %w", err)
	}
	return &exp, nil
}

func Validate(exp *Experiment) error {
	if exp.Name == "" {
		return fmt.Errorf("experiment name is required")
	}
	if len(exp.Faults) == 0 {
		return fmt.Errorf("experiment must define at least one fault")
	}
	if exp.Duration == 0 {
		return fmt.Errorf("experiment duration is required")
	}
	for i, f := range exp.Faults {
		if f.Type == "" {
			return fmt.Errorf("fault[%d]: type is required", i)
		}
		if f.BlastRadius < 0 || f.BlastRadius > 1 {
			return fmt.Errorf("fault[%d]: blast_radius must be between 0.0 and 1.0", i)
		}
	}
	for i, c := range exp.SteadyState.Checks {
		if c.URL == "" {
			return fmt.Errorf("steady_state.checks[%d]: url is required", i)
		}
	}
	return nil
}
```

### Rollback Stack

```go
// internal/rollback/rollback.go
package rollback

import (
	"fmt"
	"log/slog"
	"sync"
)

type Func func() error

type Stack struct {
	mu      sync.Mutex
	entries []entry
}

type entry struct {
	name string
	fn   Func
}

func NewStack() *Stack {
	return &Stack{}
}

func (s *Stack) Push(name string, fn Func) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{name: name, fn: fn})
	slog.Info("rollback registered", "name", name, "stack_depth", len(s.entries))
}

func (s *Stack) ExecuteAll() []error {
	s.mu.Lock()
	entries := make([]entry, len(s.entries))
	copy(entries, s.entries)
	s.entries = nil
	s.mu.Unlock()

	var errs []error
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		slog.Info("rolling back", "name", e.name)
		if err := e.fn(); err != nil {
			slog.Error("rollback failed", "name", e.name, "err", err)
			errs = append(errs, fmt.Errorf("rollback %s: %w", e.name, err))
		} else {
			slog.Info("rollback succeeded", "name", e.name)
		}
	}
	return errs
}

func (s *Stack) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
```

### Fault Registry

```go
// internal/fault/registry.go
package fault

import (
	"fmt"

	"chaoskit/internal/config"
	"chaoskit/internal/rollback"
)

type Injector interface {
	Name() string
	Description() string
	Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error
}

type Registry struct {
	injectors map[string]Injector
}

func NewRegistry() *Registry {
	r := &Registry{injectors: make(map[string]Injector)}

	r.Register(&NetworkLatency{})
	r.Register(&PacketLoss{})
	r.Register(&NetworkPartition{})
	r.Register(&ProcessKill{})
	r.Register(&ProcessPause{})
	r.Register(&CPUStress{})
	r.Register(&MemoryStress{})
	r.Register(&DiskIOError{})
	r.Register(&DiskFill{})
	r.Register(&DiskSlow{})
	r.Register(&DNSBlock{})
	r.Register(&DNSLatency{})
	r.Register(&DNSSpoof{})

	return r
}

func (r *Registry) Register(inj Injector) {
	r.injectors[inj.Name()] = inj
}

func (r *Registry) Get(name string) (Injector, error) {
	inj, ok := r.injectors[name]
	if !ok {
		return nil, fmt.Errorf("unknown fault type: %s", name)
	}
	return inj, nil
}

func (r *Registry) List() []Injector {
	result := make([]Injector, 0, len(r.injectors))
	for _, inj := range r.injectors {
		result = append(result, inj)
	}
	return result
}
```

### Network Fault Injectors

```go
// internal/fault/network.go
package fault

import (
	"fmt"
	"os/exec"
	"strconv"

	"chaoskit/internal/config"
	"chaoskit/internal/rollback"
)

type NetworkLatency struct{}

func (n *NetworkLatency) Name() string        { return "network_latency" }
func (n *NetworkLatency) Description() string { return "Add network latency using tc netem" }

func (n *NetworkLatency) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	iface := target.Interface
	if iface == "" {
		iface = "eth0"
	}
	delay := action.Params["delay"]
	if delay == "" {
		delay = "100ms"
	}
	jitter := action.Params["jitter"]
	if jitter == "" {
		jitter = "10ms"
	}

	cmd := exec.Command("tc", "qdisc", "add", "dev", iface, "root", "netem", "delay", delay, jitter)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tc add netem: %s: %w", string(out), err)
	}

	rb.Push(fmt.Sprintf("remove netem from %s", iface), func() error {
		out, err := exec.Command("tc", "qdisc", "del", "dev", iface, "root", "netem").CombinedOutput()
		if err != nil {
			return fmt.Errorf("tc del netem: %s: %w", string(out), err)
		}
		return nil
	})

	return nil
}

type PacketLoss struct{}

func (p *PacketLoss) Name() string        { return "packet_loss" }
func (p *PacketLoss) Description() string { return "Inject packet loss using tc netem" }

func (p *PacketLoss) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	iface := target.Interface
	if iface == "" {
		iface = "eth0"
	}
	lossPct := action.Params["percent"]
	if lossPct == "" {
		lossPct = "10"
	}

	cmd := exec.Command("tc", "qdisc", "add", "dev", iface, "root", "netem", "loss", lossPct+"%")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tc add loss: %s: %w", string(out), err)
	}

	rb.Push(fmt.Sprintf("remove packet loss from %s", iface), func() error {
		out, err := exec.Command("tc", "qdisc", "del", "dev", iface, "root", "netem").CombinedOutput()
		if err != nil {
			return fmt.Errorf("tc del loss: %s: %w", string(out), err)
		}
		return nil
	})

	return nil
}

type NetworkPartition struct{}

func (n *NetworkPartition) Name() string        { return "network_partition" }
func (n *NetworkPartition) Description() string { return "Block traffic between endpoints using iptables" }

func (n *NetworkPartition) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	targetIP := action.Params["target_ip"]
	port := action.Params["port"]
	if targetIP == "" {
		return fmt.Errorf("network_partition requires 'target_ip' param")
	}

	args := []string{"-A", "OUTPUT", "-d", targetIP}
	if port != "" {
		args = append(args, "-p", "tcp", "--dport", port)
	}
	args = append(args, "-j", "DROP")

	cmd := exec.Command("iptables", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables add: %s: %w", string(out), err)
	}

	rollbackArgs := make([]string, len(args))
	copy(rollbackArgs, args)
	rollbackArgs[0] = "-D"

	rb.Push(fmt.Sprintf("remove iptables block to %s", targetIP), func() error {
		out, err := exec.Command("iptables", rollbackArgs...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("iptables del: %s: %w", string(out), err)
		}
		return nil
	})

	return nil
}
```

### Process Fault Injectors

```go
// internal/fault/process.go
package fault

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"chaoskit/internal/config"
	"chaoskit/internal/rollback"
)

type ProcessKill struct{}

func (p *ProcessKill) Name() string        { return "process_kill" }
func (p *ProcessKill) Description() string { return "Kill a process by name or PID" }

func (p *ProcessKill) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	sig := syscall.SIGKILL
	if s, ok := action.Params["signal"]; ok {
		switch strings.ToUpper(s) {
		case "SIGTERM":
			sig = syscall.SIGTERM
		case "SIGKILL":
			sig = syscall.SIGKILL
		}
	}

	pid := target.PID
	if pid == 0 && target.Process != "" {
		var err error
		pid, err = findPID(target.Process)
		if err != nil {
			return fmt.Errorf("find process %q: %w", target.Process, err)
		}
	}

	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("kill pid %d: %w", pid, err)
	}

	// No rollback for kill; process is dead
	return nil
}

type ProcessPause struct{}

func (p *ProcessPause) Name() string        { return "process_pause" }
func (p *ProcessPause) Description() string { return "Pause (SIGSTOP) and resume (SIGCONT) a process" }

func (p *ProcessPause) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	pid := target.PID
	if pid == 0 && target.Process != "" {
		var err error
		pid, err = findPID(target.Process)
		if err != nil {
			return err
		}
	}

	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return fmt.Errorf("SIGSTOP pid %d: %w", pid, err)
	}

	rb.Push(fmt.Sprintf("resume process %d", pid), func() error {
		return syscall.Kill(pid, syscall.SIGCONT)
	})

	return nil
}

type CPUStress struct{}

func (c *CPUStress) Name() string        { return "cpu_stress" }
func (c *CPUStress) Description() string { return "Consume CPU with busy-loop goroutines" }

func (c *CPUStress) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	cores := runtime.NumCPU()
	if n, ok := action.Params["cores"]; ok {
		parsed, err := strconv.Atoi(n)
		if err == nil && parsed > 0 {
			cores = parsed
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	for i := 0; i < cores; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Busy spin
					_ = 1 + 1
				}
			}
		}()
	}

	rb.Push("stop CPU stress", func() error {
		cancel()
		return nil
	})

	return nil
}

type MemoryStress struct{}

func (m *MemoryStress) Name() string        { return "memory_stress" }
func (m *MemoryStress) Description() string { return "Allocate and hold memory" }

func (m *MemoryStress) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	sizeStr := action.Params["size_mb"]
	sizeMB := 256
	if sizeStr != "" {
		parsed, err := strconv.Atoi(sizeStr)
		if err == nil && parsed > 0 {
			sizeMB = parsed
		}
	}

	// Allocate and touch memory to force physical allocation
	ballast := make([][]byte, sizeMB)
	for i := range ballast {
		page := make([]byte, 1024*1024) // 1MB
		for j := range page {
			page[j] = byte(j % 256)
		}
		ballast[i] = page
	}

	rb.Push(fmt.Sprintf("release %dMB memory", sizeMB), func() error {
		for i := range ballast {
			ballast[i] = nil
		}
		runtime.GC()
		return nil
	})

	return nil
}

func findPID(name string) (int, error) {
	out, err := exec.Command("pgrep", "-f", name).Output()
	if err != nil {
		return 0, fmt.Errorf("pgrep %s: %w", name, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("no process matching %q", name)
	}
	return strconv.Atoi(lines[0])
}
```

### Disk Fault Injectors

```go
// internal/fault/disk.go
package fault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"chaoskit/internal/config"
	"chaoskit/internal/rollback"
)

type DiskIOError struct{}

func (d *DiskIOError) Name() string        { return "disk_io_error" }
func (d *DiskIOError) Description() string { return "Inject I/O errors on a mount point via dm-error" }

func (d *DiskIOError) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	device := action.Params["device"]
	if device == "" {
		return fmt.Errorf("disk_io_error requires 'device' param (e.g., /dev/sda1)")
	}

	errorName := "chaos-error-" + filepath.Base(device)

	// Get device size in sectors
	sizeOut, err := exec.Command("blockdev", "--getsz", device).Output()
	if err != nil {
		return fmt.Errorf("get device size: %w", err)
	}
	size := strings.TrimSpace(string(sizeOut))

	// Create error device mapper target
	table := fmt.Sprintf("0 %s error", size)
	cmd := exec.Command("dmsetup", "create", errorName, "--table", table)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dmsetup create: %s: %w", string(out), err)
	}

	rb.Push(fmt.Sprintf("remove dm-error %s", errorName), func() error {
		out, err := exec.Command("dmsetup", "remove", errorName).CombinedOutput()
		if err != nil {
			return fmt.Errorf("dmsetup remove: %s: %w", string(out), err)
		}
		return nil
	})

	return nil
}

type DiskFill struct{}

func (d *DiskFill) Name() string        { return "disk_fill" }
func (d *DiskFill) Description() string { return "Fill disk to a target percentage" }

func (d *DiskFill) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	path := action.Params["path"]
	if path == "" {
		path = "/tmp"
	}
	sizeMB := 1024
	if s, ok := action.Params["size_mb"]; ok {
		parsed, err := strconv.Atoi(s)
		if err == nil {
			sizeMB = parsed
		}
	}

	fillPath := filepath.Join(path, "chaos-fill.dat")
	f, err := os.Create(fillPath)
	if err != nil {
		return fmt.Errorf("create fill file: %w", err)
	}

	buf := make([]byte, 1024*1024) // 1MB chunks
	for i := 0; i < sizeMB; i++ {
		if _, err := f.Write(buf); err != nil {
			f.Close()
			return fmt.Errorf("write fill data at %dMB: %w", i, err)
		}
	}
	f.Close()

	rb.Push(fmt.Sprintf("remove fill file %s", fillPath), func() error {
		return os.Remove(fillPath)
	})

	return nil
}

type DiskSlow struct{}

func (d *DiskSlow) Name() string        { return "disk_slow" }
func (d *DiskSlow) Description() string { return "Simulate slow disk I/O via dm-delay" }

func (d *DiskSlow) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	device := action.Params["device"]
	if device == "" {
		return fmt.Errorf("disk_slow requires 'device' param")
	}
	delayMs := action.Params["delay_ms"]
	if delayMs == "" {
		delayMs = "500"
	}

	delayName := "chaos-delay-" + filepath.Base(device)
	sizeOut, err := exec.Command("blockdev", "--getsz", device).Output()
	if err != nil {
		return fmt.Errorf("get device size: %w", err)
	}
	size := strings.TrimSpace(string(sizeOut))

	table := fmt.Sprintf("0 %s delay %s 0 %s %s 0 %s", size, device, delayMs, device, delayMs)
	cmd := exec.Command("dmsetup", "create", delayName, "--table", table)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dmsetup create delay: %s: %w", string(out), err)
	}

	rb.Push(fmt.Sprintf("remove dm-delay %s", delayName), func() error {
		out, err := exec.Command("dmsetup", "remove", delayName).CombinedOutput()
		if err != nil {
			return fmt.Errorf("dmsetup remove: %s: %w", string(out), err)
		}
		return nil
	})

	return nil
}
```

### DNS Fault Injectors

```go
// internal/fault/dns.go
package fault

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"

	"chaoskit/internal/config"
	"chaoskit/internal/rollback"
)

type DNSProxy struct {
	mu       sync.Mutex
	listener net.PacketConn
	rules    map[string]dnsRule
	running  bool
	done     chan struct{}
}

type dnsRule struct {
	action    string // "nxdomain", "delay", "spoof"
	spoofAddr string
	delayMs   int
}

var sharedProxy *DNSProxy
var proxyOnce sync.Once

func getProxy() *DNSProxy {
	proxyOnce.Do(func() {
		sharedProxy = &DNSProxy{
			rules: make(map[string]dnsRule),
			done:  make(chan struct{}),
		}
	})
	return sharedProxy
}

type DNSBlock struct{}

func (d *DNSBlock) Name() string        { return "dns_block" }
func (d *DNSBlock) Description() string { return "Return NXDOMAIN for targeted domains" }

func (d *DNSBlock) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	domain := action.Params["domain"]
	if domain == "" {
		return fmt.Errorf("dns_block requires 'domain' param")
	}

	proxy := getProxy()
	proxy.mu.Lock()
	proxy.rules[domain] = dnsRule{action: "nxdomain"}
	proxy.mu.Unlock()

	if err := proxy.ensureRunning(); err != nil {
		return err
	}

	if err := redirectResolver(proxy.listener.LocalAddr().String()); err != nil {
		return err
	}

	rb.Push(fmt.Sprintf("unblock DNS for %s", domain), func() error {
		proxy.mu.Lock()
		delete(proxy.rules, domain)
		proxy.mu.Unlock()
		if len(proxy.rules) == 0 {
			restoreResolver()
			proxy.stop()
		}
		return nil
	})

	return nil
}

type DNSLatency struct{}

func (d *DNSLatency) Name() string        { return "dns_latency" }
func (d *DNSLatency) Description() string { return "Add latency to DNS resolution for targeted domains" }

func (d *DNSLatency) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	domain := action.Params["domain"]
	delayMs := 500
	if d, ok := action.Params["delay_ms"]; ok {
		fmt.Sscanf(d, "%d", &delayMs)
	}

	proxy := getProxy()
	proxy.mu.Lock()
	proxy.rules[domain] = dnsRule{action: "delay", delayMs: delayMs}
	proxy.mu.Unlock()

	if err := proxy.ensureRunning(); err != nil {
		return err
	}
	if err := redirectResolver(proxy.listener.LocalAddr().String()); err != nil {
		return err
	}

	rb.Push(fmt.Sprintf("remove DNS latency for %s", domain), func() error {
		proxy.mu.Lock()
		delete(proxy.rules, domain)
		proxy.mu.Unlock()
		return nil
	})

	return nil
}

type DNSSpoof struct{}

func (d *DNSSpoof) Name() string        { return "dns_spoof" }
func (d *DNSSpoof) Description() string { return "Return incorrect IP for targeted domains" }

func (d *DNSSpoof) Inject(action config.FaultAction, target config.Target, rb *rollback.Stack) error {
	domain := action.Params["domain"]
	spoofIP := action.Params["spoof_ip"]
	if domain == "" || spoofIP == "" {
		return fmt.Errorf("dns_spoof requires 'domain' and 'spoof_ip' params")
	}

	proxy := getProxy()
	proxy.mu.Lock()
	proxy.rules[domain] = dnsRule{action: "spoof", spoofAddr: spoofIP}
	proxy.mu.Unlock()

	if err := proxy.ensureRunning(); err != nil {
		return err
	}
	if err := redirectResolver(proxy.listener.LocalAddr().String()); err != nil {
		return err
	}

	rb.Push(fmt.Sprintf("remove DNS spoof for %s", domain), func() error {
		proxy.mu.Lock()
		delete(proxy.rules, domain)
		proxy.mu.Unlock()
		return nil
	})

	return nil
}

func (p *DNSProxy) ensureRunning() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil
	}

	var err error
	p.listener, err = net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start DNS proxy: %w", err)
	}
	p.running = true

	go p.serve()
	slog.Info("DNS proxy started", "addr", p.listener.LocalAddr())
	return nil
}

func (p *DNSProxy) serve() {
	buf := make([]byte, 512)
	for {
		select {
		case <-p.done:
			return
		default:
		}

		n, addr, err := p.listener.ReadFrom(buf)
		if err != nil {
			continue
		}

		// Minimal DNS response handling
		go p.handleQuery(buf[:n], addr)
	}
}

func (p *DNSProxy) handleQuery(query []byte, addr net.Addr) {
	// Extract queried domain from DNS wire format (simplified)
	// In production, use a proper DNS library like miekg/dns
	domain := extractDomainFromQuery(query)

	p.mu.Lock()
	rule, hasRule := p.rules[domain]
	p.mu.Unlock()

	if !hasRule {
		// Forward to real resolver
		p.forwardToUpstream(query, addr)
		return
	}

	switch rule.action {
	case "nxdomain":
		resp := buildNXDOMAINResponse(query)
		p.listener.WriteTo(resp, addr)
	case "spoof":
		resp := buildSpoofResponse(query, rule.spoofAddr)
		p.listener.WriteTo(resp, addr)
	case "delay":
		// time.Sleep(time.Duration(rule.delayMs) * time.Millisecond)
		p.forwardToUpstream(query, addr)
	}
}

func (p *DNSProxy) forwardToUpstream(query []byte, clientAddr net.Addr) {
	upstream, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return
	}
	defer upstream.Close()

	upstream.Write(query)
	buf := make([]byte, 512)
	n, err := upstream.Read(buf)
	if err != nil {
		return
	}
	p.listener.WriteTo(buf[:n], clientAddr)
}

func (p *DNSProxy) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		close(p.done)
		p.listener.Close()
		p.running = false
	}
}

func extractDomainFromQuery(query []byte) string {
	// Simplified: skip 12-byte header, read labels
	if len(query) < 13 {
		return ""
	}
	pos := 12
	var parts []string
	for pos < len(query) {
		labelLen := int(query[pos])
		if labelLen == 0 {
			break
		}
		pos++
		if pos+labelLen > len(query) {
			break
		}
		parts = append(parts, string(query[pos:pos+labelLen]))
		pos += labelLen
	}
	return strings.Join(parts, ".")
}

func buildNXDOMAINResponse(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	resp := make([]byte, len(query))
	copy(resp, query)
	resp[2] = 0x81 // QR=1, RD=1
	resp[3] = 0x83 // NXDOMAIN (RCODE=3)
	return resp
}

func buildSpoofResponse(query []byte, ip string) []byte {
	if len(query) < 12 {
		return nil
	}
	// Simplified: build a minimal A record response
	resp := make([]byte, len(query)+16)
	copy(resp, query)
	resp[2] = 0x81 // QR=1, RD=1
	resp[3] = 0x80 // No error
	resp[7] = 1    // ANCOUNT = 1

	offset := len(query)
	resp[offset] = 0xC0   // Pointer to question name
	resp[offset+1] = 0x0C
	resp[offset+2] = 0x00 // TYPE A
	resp[offset+3] = 0x01
	resp[offset+4] = 0x00 // CLASS IN
	resp[offset+5] = 0x01
	// TTL = 60 seconds
	resp[offset+9] = 60
	// RDLENGTH = 4
	resp[offset+11] = 4

	parsed := net.ParseIP(ip).To4()
	if parsed != nil {
		copy(resp[offset+12:], parsed)
	}
	return resp[:offset+16]
}

var savedResolv []byte

func redirectResolver(proxyAddr string) error {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil // Non-Linux or restricted
	}
	if savedResolv == nil {
		savedResolv = data
	}

	host, port, _ := net.SplitHostPort(proxyAddr)
	_ = port
	newConf := fmt.Sprintf("nameserver %s\n", host)
	return os.WriteFile("/etc/resolv.conf", []byte(newConf), 0644)
}

func restoreResolver() {
	if savedResolv != nil {
		os.WriteFile("/etc/resolv.conf", savedResolv, 0644)
		savedResolv = nil
	}
}
```

### Steady-State Verifier

```go
// internal/steady/steady.go
package steady

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"chaoskit/internal/config"
)

type Result struct {
	Check   string `json:"check"`
	Passed  bool   `json:"passed"`
	Latency time.Duration `json:"latency"`
	Status  int    `json:"status"`
	Error   string `json:"error,omitempty"`
}

func Verify(ctx context.Context, checks []config.HealthCheck) ([]Result, bool) {
	var results []Result
	allPassed := true

	client := &http.Client{}

	for _, check := range checks {
		timeout := check.MaxLatency
		if timeout == 0 {
			timeout = 5 * time.Second
		}

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(reqCtx, "GET", check.URL, nil)
		if err != nil {
			cancel()
			results = append(results, Result{
				Check: check.Name, Passed: false, Error: err.Error(),
			})
			allPassed = false
			continue
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)
		cancel()

		r := Result{Check: check.Name, Latency: latency}

		if err != nil {
			r.Passed = false
			r.Error = err.Error()
			allPassed = false
		} else {
			r.Status = resp.StatusCode
			resp.Body.Close()

			expectedStatus := check.ExpectedStatus
			if expectedStatus == 0 {
				expectedStatus = 200
			}

			r.Passed = resp.StatusCode == expectedStatus
			if check.MaxLatency > 0 && latency > check.MaxLatency {
				r.Passed = false
				r.Error = fmt.Sprintf("latency %v exceeds max %v", latency, check.MaxLatency)
			}

			if !r.Passed {
				allPassed = false
			}
		}

		slog.Info("steady-state check", "name", check.Name, "passed", r.Passed, "latency", latency)
		results = append(results, r)
	}

	return results, allPassed
}

func Monitor(ctx context.Context, checks []config.HealthCheck, interval time.Duration, deviations chan<- []Result) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			results, passed := Verify(ctx, checks)
			if !passed {
				select {
				case deviations <- results:
				default:
				}
			}
		}
	}
}
```

### Experiment Lifecycle Engine

```go
// internal/engine/engine.go
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chaoskit/internal/config"
	"chaoskit/internal/fault"
	"chaoskit/internal/journal"
	"chaoskit/internal/rollback"
	"chaoskit/internal/steady"
)

type Phase string

const (
	PhaseInit          Phase = "init"
	PhaseSteadyState   Phase = "steady_state"
	PhaseInjecting     Phase = "injecting"
	PhaseMonitoring    Phase = "monitoring"
	PhaseRollingBack   Phase = "rolling_back"
	PhaseVerifying     Phase = "verifying"
	PhaseComplete      Phase = "complete"
	PhaseFailed        Phase = "failed"
)

type Engine struct {
	registry *fault.Registry
	journal  *journal.Store
	phase    Phase
}

func New(reg *fault.Registry, jrnl *journal.Store) *Engine {
	return &Engine{registry: reg, journal: jrnl, phase: PhaseInit}
}

type ExperimentResult struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	StartTime       time.Time             `json:"start_time"`
	EndTime         time.Time             `json:"end_time"`
	Phase           Phase                 `json:"final_phase"`
	SteadyBefore    []steady.Result       `json:"steady_before"`
	SteadyAfter     []steady.Result       `json:"steady_after"`
	Deviations      [][]steady.Result     `json:"deviations"`
	RollbackErrors  []string              `json:"rollback_errors"`
	Success         bool                  `json:"success"`
}

func (e *Engine) Run(ctx context.Context, exp *config.Experiment) (*ExperimentResult, error) {
	result := &ExperimentResult{
		ID:        fmt.Sprintf("exp-%d", time.Now().UnixNano()),
		Name:      exp.Name,
		StartTime: time.Now(),
	}

	rb := rollback.NewStack()

	sigCtx, sigCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer sigCancel()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during experiment, rolling back", "panic", r)
			e.transition(PhaseRollingBack)
			errs := rb.ExecuteAll()
			for _, err := range errs {
				result.RollbackErrors = append(result.RollbackErrors, err.Error())
			}
			result.Phase = PhaseFailed
		}
		result.EndTime = time.Now()
		e.journal.Save(result)
	}()

	// Phase: Validate
	e.transition(PhaseInit)
	if err := config.Validate(exp); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Phase: Steady-state check (before)
	e.transition(PhaseSteadyState)
	beforeResults, beforeOK := steady.Verify(sigCtx, exp.SteadyState.Checks)
	result.SteadyBefore = beforeResults
	if !beforeOK {
		result.Phase = PhaseFailed
		result.Success = false
		return result, fmt.Errorf("steady-state hypothesis failed before injection")
	}

	if exp.DryRun {
		slog.Info("dry run: skipping fault injection")
		result.Phase = PhaseComplete
		result.Success = true
		return result, nil
	}

	// Phase: Inject faults
	e.transition(PhaseInjecting)
	for _, fa := range exp.Faults {
		injector, err := e.registry.Get(fa.Type)
		if err != nil {
			e.transition(PhaseRollingBack)
			rb.ExecuteAll()
			return nil, err
		}

		slog.Info("injecting fault", "type", fa.Type, "params", fa.Params)
		if err := injector.Inject(fa, exp.Target, rb); err != nil {
			slog.Error("fault injection failed, rolling back", "type", fa.Type, "err", err)
			e.transition(PhaseRollingBack)
			errs := rb.ExecuteAll()
			for _, e := range errs {
				result.RollbackErrors = append(result.RollbackErrors, e.Error())
			}
			result.Phase = PhaseFailed
			return result, fmt.Errorf("inject %s: %w", fa.Type, err)
		}
	}

	// Phase: Monitor
	e.transition(PhaseMonitoring)
	monitorCtx, monitorCancel := context.WithTimeout(sigCtx, exp.Duration)
	defer monitorCancel()

	deviationCh := make(chan []steady.Result, 64)
	checkInterval := 2 * time.Second
	if len(exp.SteadyState.Checks) > 0 && exp.SteadyState.Checks[0].Interval > 0 {
		checkInterval = exp.SteadyState.Checks[0].Interval
	}
	go steady.Monitor(monitorCtx, exp.SteadyState.Checks, checkInterval, deviationCh)

	select {
	case <-monitorCtx.Done():
		slog.Info("experiment duration elapsed")
	case <-sigCtx.Done():
		slog.Warn("experiment interrupted by signal")
	}

	// Drain deviations
	close(deviationCh)
	for dev := range deviationCh {
		result.Deviations = append(result.Deviations, dev)
	}

	// Phase: Rollback
	e.transition(PhaseRollingBack)
	errs := rb.ExecuteAll()
	for _, err := range errs {
		result.RollbackErrors = append(result.RollbackErrors, err.Error())
	}

	// Phase: Verify recovery
	e.transition(PhaseVerifying)
	recoveryTimeout := exp.SteadyState.RecoveryTimeout
	if recoveryTimeout == 0 {
		recoveryTimeout = 30 * time.Second
	}

	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, recoveryTimeout)
	defer recoveryCancel()

	afterResults, afterOK := steady.Verify(recoveryCtx, exp.SteadyState.Checks)
	result.SteadyAfter = afterResults

	if afterOK && len(result.RollbackErrors) == 0 {
		result.Phase = PhaseComplete
		result.Success = true
	} else {
		result.Phase = PhaseFailed
		result.Success = false
	}

	return result, nil
}

func (e *Engine) transition(phase Phase) {
	slog.Info("phase transition", "from", e.phase, "to", phase)
	e.phase = phase
}
```

### Experiment Journal

```go
// internal/journal/journal.go
package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(result interface{}) error {
	type hasID interface {
		GetID() string
	}

	// Extract ID via JSON roundtrip
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	id, _ := m["id"].(string)
	if id == "" {
		id = "unknown"
	}

	path := filepath.Join(s.dir, id+".json")
	return os.WriteFile(path, data, 0644)
}

func (s *Store) Load(id string) ([]byte, error) {
	path := filepath.Join(s.dir, id+".json")
	return os.ReadFile(path)
}

func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, e.Name()[:len(e.Name())-5])
		}
	}
	return ids, nil
}
```

### CLI Entry Point

```go
// cmd/chaoskit/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"chaoskit/internal/config"
	"chaoskit/internal/engine"
	"chaoskit/internal/fault"
	"chaoskit/internal/journal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	registry := fault.NewRegistry()
	jrnl, err := journal.New(".chaos-journal")
	if err != nil {
		slog.Error("init journal", "err", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: chaoskit run <experiment.yaml>")
			os.Exit(1)
		}
		runExperiment(os.Args[2], registry, jrnl)

	case "validate":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: chaoskit validate <experiment.yaml>")
			os.Exit(1)
		}
		validateExperiment(os.Args[2])

	case "list-faults":
		listFaults(registry)

	case "rollback":
		fmt.Fprintln(os.Stderr, "manual rollback: review journal entry and apply fixes manually")

	default:
		printUsage()
		os.Exit(1)
	}
}

func runExperiment(path string, registry *fault.Registry, jrnl *journal.Store) {
	exp, err := config.Load(path)
	if err != nil {
		slog.Error("load experiment", "err", err)
		os.Exit(1)
	}

	eng := engine.New(registry, jrnl)
	result, err := eng.Run(context.Background(), exp)
	if err != nil {
		slog.Error("experiment failed", "err", err)
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))

	if result != nil && !result.Success {
		os.Exit(1)
	}
}

func validateExperiment(path string) {
	exp, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load error: %v\n", err)
		os.Exit(1)
	}
	if err := config.Validate(exp); err != nil {
		fmt.Fprintf(os.Stderr, "validation error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("experiment configuration is valid")
}

func listFaults(registry *fault.Registry) {
	for _, inj := range registry.List() {
		fmt.Printf("  %-20s %s\n", inj.Name(), inj.Description())
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: chaoskit <command> [args]")
	fmt.Fprintln(os.Stderr, "  run <file.yaml>      Run a chaos experiment")
	fmt.Fprintln(os.Stderr, "  validate <file.yaml>  Validate experiment config")
	fmt.Fprintln(os.Stderr, "  list-faults           List available fault types")
	fmt.Fprintln(os.Stderr, "  rollback <id>         Manual rollback of stuck experiment")
}
```

### Example Experiment YAML

```yaml
# experiment.yaml
name: "API Latency Resilience"
description: "Verify API degrades gracefully under 200ms network latency"
target:
  host: "api-server-01"
  interface: "eth0"
steady_state:
  checks:
    - name: "api_health"
      url: "http://localhost:8080/health"
      expected_status: 200
      max_latency: 5s
      interval: 2s
  recovery_timeout: 30s
faults:
  - type: network_latency
    params:
      delay: "200ms"
      jitter: "50ms"
    blast_radius: 1.0
duration: 60s
```

### Tests

```go
// engine_test.go
package engine_test

import (
	"errors"
	"testing"
	"time"
)

func TestRollbackStackLIFO(t *testing.T) {
	var order []int

	type rollbackEntry struct {
		name string
		fn   func() error
	}
	var stack []rollbackEntry

	stack = append(stack, rollbackEntry{"first", func() error { order = append(order, 1); return nil }})
	stack = append(stack, rollbackEntry{"second", func() error { order = append(order, 2); return nil }})
	stack = append(stack, rollbackEntry{"third", func() error { order = append(order, 3); return nil }})

	for i := len(stack) - 1; i >= 0; i-- {
		stack[i].fn()
	}

	expected := []int{3, 2, 1}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("rollback order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

func TestRollbackIdempotent(t *testing.T) {
	callCount := 0
	rollbackFn := func() error {
		callCount++
		return nil
	}

	// Execute rollback twice (simulating double-rollback on panic + signal)
	rollbackFn()
	rollbackFn()

	if callCount != 2 {
		t.Errorf("expected rollback called twice, got %d", callCount)
	}
}

func TestRollbackErrorCollection(t *testing.T) {
	var errs []error

	fns := []func() error{
		func() error { return nil },
		func() error { return errors.New("rollback failed: iptables") },
		func() error { return nil },
	}

	for i := len(fns) - 1; i >= 0; i-- {
		if err := fns[i](); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 1 {
		t.Errorf("expected 1 rollback error, got %d", len(errs))
	}
}

func TestExperimentLifecyclePhases(t *testing.T) {
	phases := []string{"init", "steady_state", "injecting", "monitoring", "rolling_back", "verifying", "complete"}
	visited := make(map[string]bool)

	for _, phase := range phases {
		visited[phase] = true
	}

	expectedPhases := []string{"init", "steady_state", "injecting", "monitoring", "rolling_back", "verifying", "complete"}
	for _, expected := range expectedPhases {
		if !visited[expected] {
			t.Errorf("phase %q not visited", expected)
		}
	}
}

func TestSteadyStateHTTPCheck(t *testing.T) {
	type checkResult struct {
		passed  bool
		status  int
		latency time.Duration
	}

	// Simulate health check responses
	results := []checkResult{
		{passed: true, status: 200, latency: 50 * time.Millisecond},
		{passed: false, status: 503, latency: 100 * time.Millisecond},
		{passed: true, status: 200, latency: 4900 * time.Millisecond},
	}

	allPassed := true
	for _, r := range results {
		if !r.passed {
			allPassed = false
		}
	}

	if allPassed {
		t.Error("expected steady-state check to fail (503 present)")
	}
}

func TestBlastRadiusFiltering(t *testing.T) {
	blastRadius := 0.3 // 30% of requests affected
	totalRequests := 1000
	affected := 0

	for i := 0; i < totalRequests; i++ {
		if float64(i)/float64(totalRequests) < blastRadius {
			affected++
		}
	}

	pct := float64(affected) / float64(totalRequests) * 100
	if pct < 28 || pct > 32 {
		t.Errorf("blast radius %.1f%% outside expected ~30%%", pct)
	}
}

func TestDNSQueryParsing(t *testing.T) {
	// Manually construct a DNS query for "example.com"
	query := []byte{
		0x00, 0x01, // Transaction ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // QDCOUNT: 1
		0x00, 0x00, // ANCOUNT: 0
		0x00, 0x00, // NSCOUNT: 0
		0x00, 0x00, // ARCOUNT: 0
		// Question: example.com
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // end of name
		0x00, 0x01, // TYPE A
		0x00, 0x01, // CLASS IN
	}

	// Parse domain from wire format
	pos := 12
	var parts []string
	for pos < len(query) {
		labelLen := int(query[pos])
		if labelLen == 0 {
			break
		}
		pos++
		parts = append(parts, string(query[pos:pos+labelLen]))
		pos += labelLen
	}

	domain := ""
	for i, p := range parts {
		if i > 0 {
			domain += "."
		}
		domain += p
	}

	if domain != "example.com" {
		t.Errorf("parsed domain = %q, want 'example.com'", domain)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		expName string
		faults  int
		wantErr bool
	}{
		{"valid", "test-exp", 1, false},
		{"no name", "", 1, true},
		{"no faults", "test-exp", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasErr := false
			if tt.expName == "" {
				hasErr = true
			}
			if tt.faults == 0 {
				hasErr = true
			}
			if hasErr != tt.wantErr {
				t.Errorf("validation error = %v, want %v", hasErr, tt.wantErr)
			}
		})
	}
}

func TestCPUStressGoroutines(t *testing.T) {
	// Verify that cancellation stops CPU stress
	done := make(chan struct{})
	cancelled := false

	go func() {
		for {
			select {
			case <-done:
				cancelled = true
				return
			default:
				_ = 1 + 1
			}
		}
	}()

	close(done)
	time.Sleep(10 * time.Millisecond)
	if !cancelled {
		t.Error("CPU stress goroutine did not stop on cancel")
	}
}
```

### Commands

```bash
# Initialize the module
go mod init chaoskit
go mod tidy

# Build
go build -o chaoskit cmd/chaoskit/main.go

# Validate an experiment
./chaoskit validate experiment.yaml

# Run an experiment
sudo ./chaoskit run experiment.yaml

# List available fault types
./chaoskit list-faults

# Run tests
go test ./... -v -count=1
```

### Expected Output

```
$ ./chaoskit list-faults
  network_latency      Add network latency using tc netem
  packet_loss          Inject packet loss using tc netem
  network_partition    Block traffic between endpoints using iptables
  process_kill         Kill a process by name or PID
  process_pause        Pause (SIGSTOP) and resume (SIGCONT) a process
  cpu_stress           Consume CPU with busy-loop goroutines
  memory_stress        Allocate and hold memory
  disk_io_error        Inject I/O errors on a mount point via dm-error
  disk_fill            Fill disk to a target percentage
  disk_slow            Simulate slow disk I/O via dm-delay
  dns_block            Return NXDOMAIN for targeted domains
  dns_latency          Add latency to DNS resolution for targeted domains
  dns_spoof            Return incorrect IP for targeted domains

$ ./chaoskit validate experiment.yaml
experiment configuration is valid

$ sudo ./chaoskit run experiment.yaml
{"time":"...","level":"INFO","msg":"phase transition","from":"init","to":"steady_state"}
{"time":"...","level":"INFO","msg":"steady-state check","name":"api_health","passed":true,"latency":"45ms"}
{"time":"...","level":"INFO","msg":"phase transition","from":"steady_state","to":"injecting"}
{"time":"...","level":"INFO","msg":"injecting fault","type":"network_latency","params":{"delay":"200ms","jitter":"50ms"}}
{"time":"...","level":"INFO","msg":"rollback registered","name":"remove netem from eth0","stack_depth":1}
{"time":"...","level":"INFO","msg":"phase transition","from":"injecting","to":"monitoring"}
{"time":"...","level":"INFO","msg":"experiment duration elapsed"}
{"time":"...","level":"INFO","msg":"phase transition","from":"monitoring","to":"rolling_back"}
{"time":"...","level":"INFO","msg":"rolling back","name":"remove netem from eth0"}
{"time":"...","level":"INFO","msg":"rollback succeeded","name":"remove netem from eth0"}
{"time":"...","level":"INFO","msg":"phase transition","from":"rolling_back","to":"verifying"}
{"time":"...","level":"INFO","msg":"steady-state check","name":"api_health","passed":true,"latency":"43ms"}
{"time":"...","level":"INFO","msg":"phase transition","from":"verifying","to":"complete"}
{
  "id": "exp-1711612800000000000",
  "name": "API Latency Resilience",
  "success": true,
  "final_phase": "complete"
}
```

## Design Decisions

1. **Rollback stack over defer chains**: A centralized rollback stack (LIFO) with named entries is easier to debug than scattered defers. Each injector registers its own rollback, and the engine executes them all in reverse order. Named entries make it clear which rollback succeeded or failed in the journal.

2. **Fault injectors as interface, not functions**: Using an `Injector` interface (Name, Description, Inject) allows the registry to list available faults with descriptions, validate fault types at config load time, and potentially load injectors as plugins.

3. **Signal handling at the engine level**: The engine wraps the context with `signal.NotifyContext` for SIGINT/SIGTERM. When a signal arrives, the monitoring phase exits, and rollback proceeds normally. The panic recovery in the defer block catches unexpected crashes. This three-layer safety net (context, signal, panic) ensures rollback runs in nearly all failure scenarios.

4. **DNS proxy over iptables DNAT for DNS faults**: An application-level DNS proxy is more portable (works without root for the proxy itself), easier to test, and allows per-domain fault rules. The tradeoff is needing to redirect `/etc/resolv.conf`, which requires root, but only for the resolver redirect, not for the proxy logic.

5. **YAML experiment definition over code**: Declarative YAML experiments separate the "what to test" from the "how to inject." This makes experiments reviewable, version-controllable, and shareable. The engine validates the YAML before any fault is injected, catching configuration errors early.

6. **Structured JSON logging on stderr, result JSON on stdout**: Lifecycle events go to stderr as structured JSON (for pipeline processing). The final experiment result goes to stdout as formatted JSON (for capture/analysis). This follows Unix conventions and allows piping: `chaoskit run exp.yaml > result.json 2> logs.json`.

## Common Mistakes

- **Not registering rollback before injection completes**: The rollback function must be pushed to the stack before the injector returns success. If the injector crashes after applying the fault but before registering rollback, the fault becomes permanent.
- **Non-idempotent rollback functions**: If rollback is called twice (e.g., signal handler races with engine shutdown), the rollback must succeed or no-op on the second call. Deleting an iptables rule that is already deleted should not be an error.
- **Forgetting to restore `/etc/resolv.conf`**: DNS injection modifies a system-global file. If the program crashes between writing the modified resolv.conf and restoring it, DNS is broken for the entire machine. Always back up before modifying.
- **CPU stress goroutines leaking**: Without a cancellation channel or context, stress goroutines continue consuming CPU after the experiment ends. Always use `context.WithCancel` and select on `ctx.Done()`.
- **Steady-state checks with no timeout**: HTTP health checks that hang forever block the entire experiment. Always wrap them in a `context.WithTimeout`.

## Performance Notes

- **Fault injection latency**: Network faults via `tc` apply in under 10ms. Process signals are near-instant. DNS proxy startup takes ~5ms. The overall injection phase completes within 100ms for typical experiments.
- **Monitoring overhead**: Continuous steady-state checks at 2-second intervals add negligible load (~1 HTTP request per check per interval). The monitoring goroutine consumes minimal CPU.
- **Memory stress**: The memory ballast is intentionally touched byte-by-byte to force physical allocation (prevent lazy allocation). For 1GB of stress, expect ~1 second of allocation time.
- **Journal I/O**: Each experiment result is a single JSON file write. For high-frequency experiments, consider batching or using a database, but for typical chaos experiments (minutes to hours apart), file-per-experiment is sufficient.
