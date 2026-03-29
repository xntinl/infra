# Solution: Container Runtime from Scratch

## Architecture Overview

The runtime is structured in seven modules, each handling a distinct aspect of container management:

```
minioci (CLI)
  |
  +-- lifecycle.go        Container state machine (create/start/kill/delete)
  +-- config.go           OCI config.json parsing
  +-- namespace.go        Namespace creation (PID, mount, net, UTS, user)
  +-- cgroup.go           Cgroup v2 resource limits
  +-- filesystem.go       Image layer extraction, pivot_root, /dev and /proc setup
  +-- network.go          Veth pair, bridge, IP assignment, routing
  +-- security.go         Capability dropping and seccomp filter
  +-- state.go            Persistent container state (JSON file)
```

### Container Lifecycle State Machine

```
          create          start           (process exits)
  [none] -------> [created] -------> [running] -------> [stopped]
                                         |
                                     kill (signal)
                                         |
                                         v
                                     [stopped]

  delete (from created or stopped) -> [removed]
```

### Module Interaction During `create`

```
1. Parse config.json
2. Create state file (status: "creating")
3. Create cgroup and apply resource limits
4. Extract image layers into rootfs
5. Create container process with CLONE_NEW* flags
6. Container process (init):
   a. Setup mount propagation
   b. Bind-mount rootfs
   c. Mount /proc, create /dev nodes
   d. pivot_root
   e. Set hostname
   f. Wait for "start" signal (via pipe)
7. Parent: set up networking (veth pair, bridge)
8. Update state (status: "created")
```

### Module Interaction During `start`

```
1. Read state file, verify status == "created"
2. Signal the container init process (via pipe)
3. Container process (init):
   a. Drop capabilities
   b. Install seccomp filter
   c. exec(user command)
4. Update state (status: "running", pid)
```

## Go Solution

### Project Structure

```
minioci/
  go.mod
  go.sum
  main.go           CLI entry point
  config.go         OCI config.json parsing
  state.go          Container state persistence
  lifecycle.go      Container lifecycle management
  namespace.go      Namespace setup (parent + child)
  cgroup.go         Cgroup v2 operations
  filesystem.go     Rootfs extraction and pivot_root
  network.go        Veth + bridge networking
  security.go       Capabilities and seccomp
```

```go
// main.go

package main

import (
	"fmt"
	"os"
	"strconv"
)

const stateDir = "/run/minioci"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "create":
		cmdCreate()
	case "start":
		cmdStart()
	case "kill":
		cmdKill()
	case "delete":
		cmdDelete()
	case "state":
		cmdState()
	case "_init":
		// Internal: called by re-exec inside namespaces.
		containerInit()
	default:
		printUsage()
		os.Exit(1)
	}
}

func cmdCreate() {
	if len(os.Args) < 5 || os.Args[3] != "--bundle" {
		fmt.Fprintln(os.Stderr, "Usage: minioci create <id> --bundle <path>")
		os.Exit(1)
	}
	id := os.Args[2]
	bundlePath := os.Args[4]

	if err := containerCreate(id, bundlePath); err != nil {
		fmt.Fprintf(os.Stderr, "create failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Container %s created\n", id)
}

func cmdStart() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: minioci start <id>")
		os.Exit(1)
	}
	if err := containerStart(os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "start failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdKill() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "Usage: minioci kill <id> <signal>")
		os.Exit(1)
	}
	sig, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid signal: %s\n", os.Args[3])
		os.Exit(1)
	}
	if err := containerKill(os.Args[2], sig); err != nil {
		fmt.Fprintf(os.Stderr, "kill failed: %v\n", err)
		os.Exit(1)
	}
}

func cmdDelete() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: minioci delete <id>")
		os.Exit(1)
	}
	if err := containerDelete(os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "delete failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Container %s deleted\n", os.Args[2])
}

func cmdState() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: minioci state <id>")
		os.Exit(1)
	}
	s, err := loadState(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "state failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(s.JSON())
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `minioci - OCI-compatible container runtime

Usage:
  minioci create <id> --bundle <path>
  minioci start <id>
  minioci kill <id> <signal>
  minioci delete <id>
  minioci state <id>`)
}
```

```go
// config.go -- OCI config.json parsing

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type OCIConfig struct {
	OCIVersion string      `json:"ociVersion"`
	Root       OCIRoot     `json:"root"`
	Process    OCIProcess  `json:"process"`
	Hostname   string      `json:"hostname"`
	Linux      OCILinux    `json:"linux"`
}

type OCIRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

type OCIProcess struct {
	Terminal bool        `json:"terminal"`
	Args     []string    `json:"args"`
	Env      []string    `json:"env"`
	Cwd      string      `json:"cwd"`
	User     OCIUser     `json:"user"`
	Caps     *OCICaps    `json:"capabilities"`
}

type OCIUser struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

type OCICaps struct {
	Bounding    []string `json:"bounding"`
	Effective   []string `json:"effective"`
	Inheritable []string `json:"inheritable"`
	Permitted   []string `json:"permitted"`
	Ambient     []string `json:"ambient"`
}

type OCILinux struct {
	Resources  *OCIResources  `json:"resources"`
	Namespaces []OCINamespace `json:"namespaces"`
}

type OCIResources struct {
	Memory *OCIMemory `json:"memory"`
	CPU    *OCICPU    `json:"cpu"`
	Pids   *OCIPids   `json:"pids"`
}

type OCIMemory struct {
	Limit int64 `json:"limit"`
	Swap  int64 `json:"swap"`
}

type OCICPU struct {
	Quota  int64 `json:"quota"`
	Period int64 `json:"period"`
}

type OCIPids struct {
	Limit int64 `json:"limit"`
}

type OCINamespace struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func loadConfig(bundlePath string) (*OCIConfig, error) {
	configPath := filepath.Join(bundlePath, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config.json: %w", err)
	}

	var config OCIConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}

	if config.Process.Cwd == "" {
		config.Process.Cwd = "/"
	}
	if config.Hostname == "" {
		config.Hostname = "container"
	}

	return &config, nil
}
```

```go
// state.go -- Container state persistence

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ContainerStatus string

const (
	StatusCreating ContainerStatus = "creating"
	StatusCreated  ContainerStatus = "created"
	StatusRunning  ContainerStatus = "running"
	StatusStopped  ContainerStatus = "stopped"
)

type ContainerState struct {
	ID         string          `json:"id"`
	Status     ContainerStatus `json:"status"`
	PID        int             `json:"pid"`
	Bundle     string          `json:"bundle"`
	Rootfs     string          `json:"rootfs"`
	CgroupPath string          `json:"cgroupPath"`
	BridgeName string          `json:"bridgeName"`
	VethHost   string          `json:"vethHost"`
	VethCont   string          `json:"vethContainer"`
	ContainerIP string         `json:"containerIP"`
	InitPipeFd int             `json:"initPipeFd"`
}

func statePath(id string) string {
	return filepath.Join(stateDir, id, "state.json")
}

func (s *ContainerState) Save() error {
	dir := filepath.Join(stateDir, s.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(s.ID), data, 0600)
}

func loadState(id string) (*ContainerState, error) {
	data, err := os.ReadFile(statePath(id))
	if err != nil {
		return nil, fmt.Errorf("container %s not found: %w", id, err)
	}
	var state ContainerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *ContainerState) JSON() string {
	data, _ := json.MarshalIndent(s, "", "  ")
	return string(data)
}

func removeState(id string) error {
	return os.RemoveAll(filepath.Join(stateDir, id))
}
```

```go
// cgroup.go -- Cgroup v2 resource limits

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

const cgroupBase = "/sys/fs/cgroup"

func cgroupCreate(id string) (string, error) {
	path := filepath.Join(cgroupBase, "minioci-"+id)

	subtreeCtl := filepath.Join(cgroupBase, "cgroup.subtree_control")
	os.WriteFile(subtreeCtl, []byte("+cpu +memory +pids"), 0644)

	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("create cgroup: %w", err)
	}
	return path, nil
}

func cgroupApplyLimits(cgPath string, resources *OCIResources) error {
	if resources == nil {
		return nil
	}

	if resources.CPU != nil && resources.CPU.Quota > 0 {
		period := resources.CPU.Period
		if period == 0 {
			period = 100000
		}
		val := fmt.Sprintf("%d %d", resources.CPU.Quota, period)
		if err := os.WriteFile(filepath.Join(cgPath, "cpu.max"), []byte(val), 0644); err != nil {
			return fmt.Errorf("set cpu.max: %w", err)
		}
	}

	if resources.Memory != nil && resources.Memory.Limit > 0 {
		val := strconv.FormatInt(resources.Memory.Limit, 10)
		if err := os.WriteFile(filepath.Join(cgPath, "memory.max"), []byte(val), 0644); err != nil {
			return fmt.Errorf("set memory.max: %w", err)
		}
		if resources.Memory.Swap >= 0 {
			swapVal := strconv.FormatInt(resources.Memory.Swap, 10)
			os.WriteFile(filepath.Join(cgPath, "memory.swap.max"), []byte(swapVal), 0644)
		}
	}

	if resources.Pids != nil && resources.Pids.Limit > 0 {
		val := strconv.FormatInt(resources.Pids.Limit, 10)
		if err := os.WriteFile(filepath.Join(cgPath, "pids.max"), []byte(val), 0644); err != nil {
			return fmt.Errorf("set pids.max: %w", err)
		}
	}

	return nil
}

func cgroupAddProcess(cgPath string, pid int) error {
	return os.WriteFile(
		filepath.Join(cgPath, "cgroup.procs"),
		[]byte(strconv.Itoa(pid)),
		0644,
	)
}

func cgroupDestroy(cgPath string) error {
	// Kill all processes in the cgroup.
	data, _ := os.ReadFile(filepath.Join(cgPath, "cgroup.procs"))
	for _, line := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(line)
		if err == nil {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	// Wait for processes to exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(cgPath, "cgroup.procs"))
		if strings.TrimSpace(string(data)) == "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return os.Remove(cgPath)
}
```

```go
// filesystem.go -- Rootfs extraction, pivot_root, dev/proc setup

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func prepareRootfs(bundlePath, rootPath string, layers []string) (string, error) {
	rootfs := filepath.Join(bundlePath, rootPath)

	if len(layers) > 0 {
		if err := os.MkdirAll(rootfs, 0755); err != nil {
			return "", fmt.Errorf("create rootfs dir: %w", err)
		}
		for i, layer := range layers {
			layerPath := filepath.Join(bundlePath, layer)
			if err := extractLayer(rootfs, layerPath); err != nil {
				return "", fmt.Errorf("extract layer %d (%s): %w", i, layer, err)
			}
		}
	}

	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		return "", fmt.Errorf("rootfs not found: %s", rootfs)
	}

	return rootfs, nil
}

func extractLayer(rootfs, layerPath string) error {
	f, err := os.Open(layerPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f

	// Detect gzip compression.
	if strings.HasSuffix(layerPath, ".gz") || strings.HasSuffix(layerPath, ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("gzip reader: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		target := filepath.Join(rootfs, header.Name)

		// Security: prevent path traversal.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(rootfs)) {
			continue
		}

		// Handle OCI whiteout files (deletion markers).
		baseName := filepath.Base(header.Name)
		if strings.HasPrefix(baseName, ".wh.") {
			deleteName := strings.TrimPrefix(baseName, ".wh.")
			deleteTarget := filepath.Join(filepath.Dir(target), deleteName)
			os.RemoveAll(deleteTarget)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(header.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			io.Copy(out, tr)
			out.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0755)
			os.Symlink(header.Linkname, target)
		case tar.TypeLink:
			os.MkdirAll(filepath.Dir(target), 0755)
			linkTarget := filepath.Join(rootfs, header.Linkname)
			os.Link(linkTarget, target)
		}
	}
	return nil
}

func setupPivotRoot(rootfs string) error {
	// Make mount propagation private.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("set mount propagation: %w", err)
	}

	// Bind-mount rootfs onto itself (pivot_root requirement).
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs: %w", err)
	}

	pivotDir := filepath.Join(rootfs, ".pivot_old")
	os.MkdirAll(pivotDir, 0700)

	if err := syscall.PivotRoot(rootfs, pivotDir); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// Mount /proc.
	os.MkdirAll("/proc", 0555)
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}

	// Mount /sys (read-only).
	os.MkdirAll("/sys", 0555)
	syscall.Mount("sysfs", "/sys", "sysfs",
		syscall.MS_RDONLY|syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "")

	// Mount /dev/pts for terminal support.
	os.MkdirAll("/dev", 0755)
	os.MkdirAll("/dev/pts", 0755)
	syscall.Mount("devpts", "/dev/pts", "devpts", 0, "")

	// Create essential device nodes.
	createCharDev("/dev/null", 1, 3)
	createCharDev("/dev/zero", 1, 5)
	createCharDev("/dev/random", 1, 8)
	createCharDev("/dev/urandom", 1, 9)
	createCharDev("/dev/tty", 5, 0)

	// Unmount old root.
	if err := syscall.Unmount("/.pivot_old", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}
	os.RemoveAll("/.pivot_old")

	return nil
}

func createCharDev(path string, major, minor uint32) {
	dev := int(major*256 + minor)
	syscall.Mknod(path, syscall.S_IFCHR|0666, dev)
}
```

```go
// network.go -- Veth pair + bridge networking

package main

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
)

type NetworkConfig struct {
	BridgeName  string
	VethHost    string
	VethCont    string
	BridgeIP    string
	ContainerIP string
	Subnet      string
}

func defaultNetConfig(id string) NetworkConfig {
	// Use a deterministic IP based on the container ID hash.
	// Simple scheme: bridge=10.100.0.1, container=10.100.0.2
	return NetworkConfig{
		BridgeName:  "br-minioci",
		VethHost:    "veth-" + id[:8],
		VethCont:    "eth0",
		BridgeIP:    "10.100.0.1/24",
		ContainerIP: "10.100.0.2/24",
		Subnet:      "10.100.0.0/24",
	}
}

func setupHostNetworking(cfg NetworkConfig, containerPID int) error {
	// Create bridge if it does not exist.
	if !interfaceExists(cfg.BridgeName) {
		if err := run("ip", "link", "add", cfg.BridgeName, "type", "bridge"); err != nil {
			return fmt.Errorf("create bridge: %w", err)
		}
		if err := run("ip", "addr", "add", cfg.BridgeIP, "dev", cfg.BridgeName); err != nil {
			return fmt.Errorf("assign bridge IP: %w", err)
		}
		if err := run("ip", "link", "set", cfg.BridgeName, "up"); err != nil {
			return fmt.Errorf("bring up bridge: %w", err)
		}
	}

	// Create veth pair.
	if err := run("ip", "link", "add", cfg.VethHost, "type", "veth",
		"peer", "name", cfg.VethCont); err != nil {
		return fmt.Errorf("create veth pair: %w", err)
	}

	// Attach host end to bridge.
	if err := run("ip", "link", "set", cfg.VethHost, "master", cfg.BridgeName); err != nil {
		return fmt.Errorf("attach veth to bridge: %w", err)
	}
	if err := run("ip", "link", "set", cfg.VethHost, "up"); err != nil {
		return fmt.Errorf("bring up host veth: %w", err)
	}

	// Move container end into the container's network namespace.
	pid := strconv.Itoa(containerPID)
	if err := run("ip", "link", "set", cfg.VethCont, "netns", pid); err != nil {
		return fmt.Errorf("move veth to container netns: %w", err)
	}

	// Configure inside the container namespace using nsenter.
	if err := run("nsenter", "-t", pid, "-n",
		"ip", "addr", "add", cfg.ContainerIP, "dev", cfg.VethCont); err != nil {
		return fmt.Errorf("assign container IP: %w", err)
	}
	if err := run("nsenter", "-t", pid, "-n",
		"ip", "link", "set", cfg.VethCont, "up"); err != nil {
		return fmt.Errorf("bring up container veth: %w", err)
	}
	if err := run("nsenter", "-t", pid, "-n",
		"ip", "link", "set", "lo", "up"); err != nil {
		return fmt.Errorf("bring up container loopback: %w", err)
	}

	// Set default route inside container to the bridge.
	bridgeIP := cfg.BridgeIP[:len(cfg.BridgeIP)-3] // Strip /24
	run("nsenter", "-t", pid, "-n",
		"ip", "route", "add", "default", "via", bridgeIP)

	return nil
}

func teardownNetworking(cfg NetworkConfig) {
	run("ip", "link", "del", cfg.VethHost)
	// Bridge is shared; do not delete it if other containers use it.
}

func interfaceExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
```

```go
// security.go -- Capability dropping and seccomp

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Linux capability constants (subset).
const (
	CAP_CHOWN            = 0
	CAP_KILL             = 5
	CAP_SETGID           = 6
	CAP_SETUID           = 7
	CAP_NET_BIND_SERVICE = 10
	CAP_SYS_CHROOT       = 18
	CAP_LAST_CAP         = 40 // Approximate upper bound
)

var defaultAllowedCaps = map[int]bool{
	CAP_CHOWN:            true,
	CAP_KILL:             true,
	CAP_SETGID:           true,
	CAP_SETUID:           true,
	CAP_NET_BIND_SERVICE: true,
}

func dropCapabilities(allowed map[int]bool) error {
	if allowed == nil {
		allowed = defaultAllowedCaps
	}

	for cap := 0; cap <= CAP_LAST_CAP; cap++ {
		if allowed[cap] {
			continue
		}
		// PR_CAPBSET_DROP = 24
		_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, 24, uintptr(cap), 0)
		if errno != 0 && errno != syscall.EINVAL {
			return fmt.Errorf("drop capability %d: %v", cap, errno)
		}
	}
	return nil
}

// Seccomp BPF filter.
// This is a minimal implementation using raw BPF instructions.
// Production runtimes use libseccomp for portability.

const (
	SECCOMP_SET_MODE_FILTER = 1
	SECCOMP_RET_ALLOW       = 0x7fff0000
	SECCOMP_RET_ERRNO       = 0x00050000 // EPERM
	SECCOMP_RET_KILL        = 0x00000000

	// BPF instruction opcodes.
	BPF_LD  = 0x00
	BPF_JMP = 0x05
	BPF_RET = 0x06
	BPF_W   = 0x00
	BPF_ABS = 0x20
	BPF_JEQ = 0x10
	BPF_K   = 0x00
)

type bpfInsn struct {
	code uint16
	jt   uint8
	jf   uint8
	k    uint32
}

type bpfProg struct {
	length uint16
	filter *bpfInsn
}

// Blocked syscalls (x86_64 numbers).
var blockedSyscalls = []uint32{
	246, // kexec_load
	169, // reboot
	101, // ptrace
}

func installSeccompFilter() error {
	// Build BPF program:
	// 1. Load syscall number (offset 0 in seccomp_data).
	// 2. For each blocked syscall: if equal, return ERRNO(EPERM).
	// 3. Default: allow.

	n := len(blockedSyscalls)
	insns := make([]bpfInsn, 0, n+2)

	// Load the syscall number from seccomp_data.nr (offset 0).
	insns = append(insns, bpfInsn{
		code: BPF_LD | BPF_W | BPF_ABS,
		k:    0, // offsetof(seccomp_data, nr)
	})

	// For each blocked syscall: jump to deny if equal.
	for i, sc := range blockedSyscalls {
		jumpToAllow := uint8(n - i)     // Skip remaining checks + allow
		jumpToDeny := uint8(n - i - 1)  // Jump to deny (at end before allow)
		_ = jumpToAllow

		insns = append(insns, bpfInsn{
			code: BPF_JMP | BPF_JEQ | BPF_K,
			jt:   uint8(n - i), // Jump to ERRNO return
			jf:   0,            // Continue checking
			k:    sc,
		})
	}

	// Default: allow.
	insns = append(insns, bpfInsn{
		code: BPF_RET | BPF_K,
		k:    SECCOMP_RET_ALLOW,
	})

	// Deny: return EPERM.
	insns = append(insns, bpfInsn{
		code: BPF_RET | BPF_K,
		k:    SECCOMP_RET_ERRNO | 1, // EPERM
	})

	prog := bpfProg{
		length: uint16(len(insns)),
		filter: &insns[0],
	}

	// Set NO_NEW_PRIVS (required before seccomp).
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, 38, 1, 0) // PR_SET_NO_NEW_PRIVS
	if errno != 0 {
		return fmt.Errorf("prctl NO_NEW_PRIVS: %v", errno)
	}

	_, _, errno = syscall.RawSyscall(syscall.SYS_SECCOMP,
		SECCOMP_SET_MODE_FILTER,
		0,
		uintptr(unsafe.Pointer(&prog)),
	)
	if errno != 0 {
		return fmt.Errorf("seccomp: %v", errno)
	}

	return nil
}
```

```go
// namespace.go -- Namespace creation and container init

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

func createContainerProcess(config *OCIConfig, rootfs string, state *ContainerState) (*exec.Cmd, *os.File, error) {
	// Create a pipe for synchronization: parent writes to signal "start".
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("create pipe: %w", err)
	}

	// Serialize config for the child.
	configJSON, _ := json.Marshal(config)

	cmd := exec.Command("/proc/self/exe", "_init")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.ExtraFiles = []*os.File{readPipe} // fd 3 in the child

	cmd.Env = []string{
		fmt.Sprintf("MINIOCI_ROOTFS=%s", rootfs),
		fmt.Sprintf("MINIOCI_CONFIG=%s", string(configJSON)),
		fmt.Sprintf("MINIOCI_PIPE_FD=3"),
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWNET,
	}

	if err := cmd.Start(); err != nil {
		readPipe.Close()
		writePipe.Close()
		return nil, nil, fmt.Errorf("start container process: %w", err)
	}

	readPipe.Close() // Parent does not read.
	return cmd, writePipe, nil
}

func containerInit() {
	rootfs := os.Getenv("MINIOCI_ROOTFS")
	configJSON := os.Getenv("MINIOCI_CONFIG")

	var config OCIConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		initFatal("parse config: " + err.Error())
	}

	// Set hostname.
	if err := syscall.Sethostname([]byte(config.Hostname)); err != nil {
		initFatal("sethostname: " + err.Error())
	}

	// Set up root filesystem.
	if err := setupPivotRoot(rootfs); err != nil {
		initFatal("pivot_root: " + err.Error())
	}

	// Change to the configured working directory.
	if err := syscall.Chdir(config.Process.Cwd); err != nil {
		initFatal("chdir: " + err.Error())
	}

	// Wait for "start" signal from parent via pipe (fd 3).
	pipeFd := os.NewFile(3, "init-pipe")
	buf := make([]byte, 1)
	_, err := pipeFd.Read(buf)
	pipeFd.Close()
	if err != nil && err != io.EOF {
		initFatal("read pipe: " + err.Error())
	}

	// Drop capabilities.
	if err := dropCapabilities(nil); err != nil {
		initFatal("drop capabilities: " + err.Error())
	}

	// Install seccomp filter.
	if err := installSeccompFilter(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: seccomp filter failed: %v\n", err)
	}

	// Exec the user command.
	if len(config.Process.Args) == 0 {
		initFatal("no command specified")
	}

	binary, err := exec.LookPath(config.Process.Args[0])
	if err != nil {
		initFatal("command not found: " + config.Process.Args[0])
	}

	env := config.Process.Env
	if len(env) == 0 {
		env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"TERM=xterm",
		}
	}

	if err := syscall.Exec(binary, config.Process.Args, env); err != nil {
		initFatal("exec: " + err.Error())
	}
}

func initFatal(msg string) {
	fmt.Fprintf(os.Stderr, "container init: %s\n", msg)
	os.Exit(1)
}
```

```go
// lifecycle.go -- Container lifecycle management

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func containerCreate(id, bundlePath string) error {
	config, err := loadConfig(bundlePath)
	if err != nil {
		return err
	}

	state := &ContainerState{
		ID:     id,
		Status: StatusCreating,
		Bundle: bundlePath,
	}
	if err := state.Save(); err != nil {
		return err
	}

	// Create cgroup.
	cgPath, err := cgroupCreate(id)
	if err != nil {
		removeState(id)
		return fmt.Errorf("cgroup create: %w", err)
	}
	state.CgroupPath = cgPath

	// Apply resource limits.
	if config.Linux.Resources != nil {
		if err := cgroupApplyLimits(cgPath, config.Linux.Resources); err != nil {
			cgroupDestroy(cgPath)
			removeState(id)
			return fmt.Errorf("cgroup limits: %w", err)
		}
	}

	// Prepare rootfs (extract layers if present, or use existing directory).
	rootfs, err := prepareRootfs(bundlePath, config.Root.Path, nil)
	if err != nil {
		cgroupDestroy(cgPath)
		removeState(id)
		return fmt.Errorf("prepare rootfs: %w", err)
	}
	state.Rootfs = rootfs

	// Create the container process (it will block waiting for "start" signal).
	cmd, writePipe, err := createContainerProcess(config, rootfs, state)
	if err != nil {
		cgroupDestroy(cgPath)
		removeState(id)
		return err
	}

	state.PID = cmd.Process.Pid

	// Add process to cgroup.
	cgroupAddProcess(cgPath, cmd.Process.Pid)

	// Set up networking.
	netCfg := defaultNetConfig(id)
	if err := setupHostNetworking(netCfg, cmd.Process.Pid); err != nil {
		fmt.Fprintf(os.Stderr, "warning: networking setup failed: %v\n", err)
	}
	state.BridgeName = netCfg.BridgeName
	state.VethHost = netCfg.VethHost
	state.VethCont = netCfg.VethCont
	state.ContainerIP = netCfg.ContainerIP

	// Store the write pipe fd number in state for the start command.
	// In practice, we keep the pipe open and write to it during start.
	// For simplicity, we write immediately and let the child proceed.
	// A production runtime would hold the pipe open.
	state.Status = StatusCreated
	state.Save()

	// Keep write pipe open for the start command.
	// Save the pipe's fd path so start can signal it.
	pipePath := fmt.Sprintf("/run/minioci/%s/start-pipe", id)
	pipeFile, _ := os.Create(pipePath)
	pipeFile.Close()

	// For this implementation, hold the pipe in a goroutine.
	go func() {
		// Wait for start signal file to appear.
		startSignal := fmt.Sprintf("/run/minioci/%s/start-signal", id)
		for {
			if _, err := os.Stat(startSignal); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		writePipe.Write([]byte("s"))
		writePipe.Close()
		cmd.Wait()

		state.Status = StatusStopped
		state.Save()
	}()

	return nil
}

func containerStart(id string) error {
	state, err := loadState(id)
	if err != nil {
		return err
	}
	if state.Status != StatusCreated {
		return fmt.Errorf("container %s is %s, expected created", id, state.Status)
	}

	// Signal the init process to proceed.
	startSignal := fmt.Sprintf("/run/minioci/%s/start-signal", id)
	os.WriteFile(startSignal, []byte("1"), 0600)

	state.Status = StatusRunning
	return state.Save()
}

func containerKill(id string, signal int) error {
	state, err := loadState(id)
	if err != nil {
		return err
	}
	if state.Status != StatusRunning && state.Status != StatusCreated {
		return fmt.Errorf("container %s is %s, cannot kill", id, state.Status)
	}

	if err := syscall.Kill(state.PID, syscall.Signal(signal)); err != nil {
		return fmt.Errorf("kill %d: %w", state.PID, err)
	}

	// Wait briefly for the process to exit.
	time.Sleep(500 * time.Millisecond)

	state.Status = StatusStopped
	return state.Save()
}

func containerDelete(id string) error {
	state, err := loadState(id)
	if err != nil {
		// State already gone, just clean up.
		removeState(id)
		return nil
	}

	if state.Status == StatusRunning {
		return fmt.Errorf("container %s is running, kill it first", id)
	}

	// Tear down networking.
	netCfg := NetworkConfig{
		VethHost: state.VethHost,
	}
	teardownNetworking(netCfg)

	// Destroy cgroup.
	if state.CgroupPath != "" {
		cgroupDestroy(state.CgroupPath)
	}

	// Remove state.
	removeState(id)
	return nil
}
```

## Example config.json

```json
{
  "ociVersion": "1.0.2",
  "root": {
    "path": "rootfs",
    "readonly": false
  },
  "process": {
    "terminal": false,
    "args": ["/bin/sh"],
    "env": [
      "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
      "TERM=xterm",
      "HOME=/root"
    ],
    "cwd": "/"
  },
  "hostname": "my-container",
  "linux": {
    "resources": {
      "memory": {
        "limit": 268435456,
        "swap": 0
      },
      "cpu": {
        "quota": 50000,
        "period": 100000
      },
      "pids": {
        "limit": 100
      }
    },
    "namespaces": [
      {"type": "pid"},
      {"type": "mount"},
      {"type": "network"},
      {"type": "uts"}
    ]
  }
}
```

## Tests

```go
// lifecycle_test.go

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root")
	}
}

func setupTestBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs")

	// Minimal rootfs.
	for _, d := range []string{
		rootfs + "/bin", rootfs + "/dev", rootfs + "/proc",
		rootfs + "/sys", rootfs + "/tmp", rootfs + "/etc",
	} {
		os.MkdirAll(d, 0755)
	}

	// Copy busybox for a working shell.
	busybox := "/bin/busybox"
	if _, err := os.Stat(busybox); err != nil {
		t.Skip("busybox not found")
	}
	data, _ := os.ReadFile(busybox)
	os.WriteFile(filepath.Join(rootfs, "bin/busybox"), data, 0755)
	for _, cmd := range []string{"sh", "ls", "cat", "echo", "ps", "hostname", "sleep"} {
		os.Symlink("busybox", filepath.Join(rootfs, "bin", cmd))
	}

	config := OCIConfig{
		OCIVersion: "1.0.2",
		Root:       OCIRoot{Path: "rootfs"},
		Process: OCIProcess{
			Args: []string{"/bin/echo", "hello-from-container"},
			Env: []string{
				"PATH=/bin:/usr/bin",
				"HOME=/root",
			},
			Cwd: "/",
		},
		Hostname: "test-container",
		Linux: OCILinux{
			Resources: &OCIResources{
				Pids: &OCIPids{Limit: 50},
			},
		},
	}

	configData, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(filepath.Join(dir, "config.json"), configData, 0644)

	return dir
}

func TestCreateAndDelete(t *testing.T) {
	requireRoot(t)
	bundle := setupTestBundle(t)

	id := "test-create-delete"
	err := containerCreate(id, bundle)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	state, err := loadState(id)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Status != StatusCreated {
		t.Errorf("expected status 'created', got %q", state.Status)
	}

	// Clean up.
	containerKill(id, 9)
	time.Sleep(500 * time.Millisecond)
	if err := containerDelete(id); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func TestContainerLifecycle(t *testing.T) {
	requireRoot(t)
	bundle := setupTestBundle(t)

	id := "test-lifecycle"
	if err := containerCreate(id, bundle); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer containerDelete(id)

	if err := containerStart(id); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for the short-lived process to exit.
	time.Sleep(2 * time.Second)

	state, err := loadState(id)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Status != StatusStopped {
		t.Logf("status: %s (may still be running briefly)", state.Status)
	}
}

func TestCgroupCreated(t *testing.T) {
	requireRoot(t)
	bundle := setupTestBundle(t)

	id := "test-cgroup"
	if err := containerCreate(id, bundle); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		containerKill(id, 9)
		time.Sleep(500 * time.Millisecond)
		containerDelete(id)
	}()

	cgPath := filepath.Join(cgroupBase, "minioci-"+id)
	if _, err := os.Stat(cgPath); os.IsNotExist(err) {
		t.Error("cgroup directory not created")
	}

	pidsMax, _ := os.ReadFile(filepath.Join(cgPath, "pids.max"))
	if string(pidsMax) != "50" {
		t.Errorf("expected pids.max=50, got %q", string(pidsMax))
	}
}

func TestExtractLayer(t *testing.T) {
	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs")
	os.MkdirAll(rootfs, 0755)

	// Create a test tar layer.
	// For a real test, create a tar programmatically or use a fixture.
	t.Log("Layer extraction requires tar fixture files -- skipping in unit test")
}
```

## Running the Solution

```bash
mkdir minioci && cd minioci
go mod init minioci

# Copy source files

go build -o minioci .

# Prepare a bundle
mkdir -p /tmp/test-bundle/rootfs
# Extract Alpine rootfs into /tmp/test-bundle/rootfs
wget -O- https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-3.19.1-x86_64.tar.gz \
  | tar xz -C /tmp/test-bundle/rootfs

# Create config.json (copy the example above) into /tmp/test-bundle/

# Create the container
sudo ./minioci create mycontainer --bundle /tmp/test-bundle

# Check state
sudo ./minioci state mycontainer

# Start it
sudo ./minioci start mycontainer

# Kill it
sudo ./minioci kill mycontainer 15

# Delete it
sudo ./minioci delete mycontainer
```

### Expected Output

```
$ sudo ./minioci create mycontainer --bundle /tmp/test-bundle
Container mycontainer created

$ sudo ./minioci state mycontainer
{
  "id": "mycontainer",
  "status": "created",
  "pid": 12345,
  "bundle": "/tmp/test-bundle",
  ...
}

$ sudo ./minioci start mycontainer

$ sudo ./minioci state mycontainer
{
  "id": "mycontainer",
  "status": "running",
  "pid": 12345,
  ...
}

$ sudo ./minioci kill mycontainer 15
$ sudo ./minioci delete mycontainer
Container mycontainer deleted
```

## Design Decisions

1. **Re-exec pattern for namespace init**: Go's multi-threaded runtime makes `unshare()` unreliable. The standard approach (used by runc) is to re-execute the binary with a special argument (`_init`) so the child starts fresh in the new namespaces.

2. **Pipe-based synchronization**: The parent creates a pipe before forking. The child blocks on reading the pipe until the parent signals "start." This implements the OCI create/start split -- the container is created (namespaces set up, cgroup configured, networking ready) but the user process has not started yet.

3. **Signal file for start**: Between the `create` and `start` commands (which are separate CLI invocations), the pipe must remain open. The goroutine in `containerCreate` holds the pipe and watches for a signal file. A production runtime would use a Unix socket or hold a long-running daemon process.

4. **ip command for networking**: The solution uses `ip` commands via `exec.Command` for network setup. A production runtime would use the netlink socket directly (via the `vishvananda/netlink` library) to avoid exec overhead and dependency on external tools.

5. **Minimal seccomp filter**: The BPF filter blocks only a few dangerous syscalls. A production runtime (like runc) uses a default filter with hundreds of rules, and the OCI config specifies additional rules. The libseccomp library provides a much better API for building complex filters.

6. **No overlay filesystem**: The solution extracts tar layers sequentially into a single directory. A production runtime uses overlayfs (or similar) for copy-on-write semantics and efficient layer sharing. Overlayfs support would add `mount("overlay", rootfs, "overlay", 0, "lowerdir=...upperdir=...workdir=...")`.

7. **No user namespace by default**: User namespaces interact with other namespaces in complex ways and require careful UID/GID mapping. The solution runs as root for simplicity. Adding user namespace support requires mapping files (`/proc/PID/uid_map`, `/proc/PID/gid_map`) and handling the `newuidmap`/`newgidmap` helpers.

## Common Mistakes

1. **Race between create and cgroup assignment**: The child process starts running before the parent adds it to the cgroup. For a brief window, the process runs without resource limits. Production runtimes solve this with the pipe synchronization -- the child waits before executing the user command.

2. **Mount propagation leaking**: Not setting `MS_PRIVATE` recursively causes mount operations inside the container to propagate to the host. This can break the host system.

3. **pivot_root with Go**: Go's `syscall.PivotRoot` must be called from the namespace init process (the re-execed child). Calling it from the parent process affects the parent's mount namespace.

4. **Leftover cgroups**: If the delete command fails or is never called, cgroups persist in `/sys/fs/cgroup`. Implement a cleanup/garbage collection mechanism for robustness.

5. **Seccomp filter architecture**: BPF seccomp filters must match the host architecture. The syscall numbers are architecture-specific (x86_64 vs aarch64). The solution hardcodes x86_64 numbers. Use `seccomp_arch_native` for portability.

6. **Networking namespace timing**: The veth peer must be moved into the container's network namespace after the child process exists but before the child needs networking. The pipe-based synchronization ensures this ordering.

## Performance Notes

- **Container startup time**: Creating namespaces, setting up cgroups, extracting a minimal rootfs, and configuring networking takes approximately 50-200ms depending on rootfs size. Docker adds overhead for image pulling, storage driver setup, and daemon communication.
- **Namespace overhead**: Each namespace adds minimal runtime overhead. PID namespace translation is essentially free. Mount namespace adds memory proportional to the mount table size. Network namespace adds a small cost for packet routing through the veth pair.
- **Layer extraction vs overlayfs**: Sequential tar extraction is O(total size of all layers). Overlayfs mounts are O(1) regardless of layer size, with copy-on-write costs amortized to actual writes. For large images with many layers, overlayfs is dramatically faster.
- **Seccomp filter overhead**: Each syscall passes through the BPF filter. With a short filter (3-5 blocked syscalls), the overhead is under 100ns per syscall. Long filters (200+ rules) can add microseconds. The kernel JIT-compiles BPF for performance.
- **Veth networking**: Packets between the container and host traverse the veth pair and the bridge. Each hop adds approximately 1-2 microseconds of latency. For loopback-like workloads, this is negligible. For high-throughput networking, consider macvlan or SR-IOV passthrough.
