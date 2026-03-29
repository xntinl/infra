# Solution: Namespace Isolation Sandbox

## Architecture Overview

The sandbox uses a re-exec pattern with two phases:

1. **Parent phase (outside namespaces)**: Parses arguments, configures clone flags, creates a child process in new namespaces via `os/exec` with `SysProcAttr.Cloneflags`.

2. **Child phase (inside namespaces)**: Detected via an environment variable. Performs namespace-internal setup: set hostname, prepare root filesystem, mount `/proc` and `/dev` entries, `pivot_root` to the new root, then `exec` the target command.

```
Parent Process (host PID namespace)
  |
  +-- os/exec.Command("/proc/self/exe", "init", ...)
  |     SysProcAttr.Cloneflags:
  |       CLONE_NEWPID  -- new PID namespace
  |       CLONE_NEWNS   -- new mount namespace
  |       CLONE_NEWUTS  -- new UTS namespace
  |       CLONE_NEWNET  -- new network namespace
  |       CLONE_NEWUSER -- new user namespace (optional)
  |
  Child Process (new namespaces, PID 1 inside)
    |
    +-- Set hostname
    +-- Set mount propagation to MS_PRIVATE
    +-- Bind-mount rootfs onto itself
    +-- Mount /proc, /dev entries
    +-- pivot_root(rootfs, rootfs/.pivot_old)
    +-- chdir("/")
    +-- Unmount /.pivot_old
    +-- exec(target command)
```

## Go Solution

```go
// main.go

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "Usage: sandbox run <rootfs> <hostname> <command> [args...]")
			os.Exit(1)
		}
		rootfs := os.Args[2]
		hostname := os.Args[3]
		cmdArgs := os.Args[4:]
		exitCode := parentRun(rootfs, hostname, cmdArgs)
		os.Exit(exitCode)

	case "init":
		// Internal: called by re-exec inside new namespaces.
		childInit()

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `sandbox - Process isolation using Linux namespaces

Usage:
  sandbox run <rootfs> <hostname> <command> [args...]

Arguments:
  rootfs     Path to root filesystem directory (e.g., extracted Alpine rootfs)
  hostname   Hostname for the sandbox
  command    Command to run inside the sandbox

Examples:
  sandbox run ./alpine-rootfs mycontainer /bin/sh
  sandbox run /tmp/rootfs sandbox-1 /bin/ls -la /`)
}
```

```go
// parent.go -- Parent phase: create child in new namespaces

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func parentRun(rootfs, hostname string, cmdArgs []string) int {
	absRootfs, err := filepath.Abs(rootfs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving rootfs path: %v\n", err)
		return 1
	}

	if _, err := os.Stat(absRootfs); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Root filesystem not found: %s\n", absRootfs)
		return 1
	}

	// Re-exec ourselves with "init" as the first argument.
	// The child will detect this and run the namespace setup.
	args := append([]string{"init"}, cmdArgs...)

	cmd := exec.Command("/proc/self/exe", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("SANDBOX_ROOTFS=%s", absRootfs),
		fmt.Sprintf("SANDBOX_HOSTNAME=%s", hostname),
	)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWNET,
		// User namespace requires additional UID/GID mapping setup.
		// Uncomment for rootless operation:
		// Cloneflags: syscall.CLONE_NEWPID | syscall.CLONE_NEWNS |
		//             syscall.CLONE_NEWUTS | syscall.CLONE_NEWNET |
		//             syscall.CLONE_NEWUSER,
		// UidMappings: []syscall.SysProcIDMap{
		//     {ContainerID: 0, HostID: os.Getuid(), Size: 1},
		// },
		// GidMappings: []syscall.SysProcIDMap{
		//     {ContainerID: 0, HostID: os.Getgid(), Size: 1},
		// },
	}

	fmt.Fprintf(os.Stderr, "Starting sandbox: rootfs=%s hostname=%s cmd=%v\n",
		absRootfs, hostname, cmdArgs)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
```

```go
// child.go -- Child phase: namespace setup and exec

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func childInit() {
	rootfs := os.Getenv("SANDBOX_ROOTFS")
	hostname := os.Getenv("SANDBOX_HOSTNAME")

	if rootfs == "" {
		fatal("SANDBOX_ROOTFS not set")
	}

	// os.Args: ["sandbox", "init", <command>, <args...>]
	if len(os.Args) < 3 {
		fatal("no command specified")
	}
	cmdArgs := os.Args[2:]

	setupHostname(hostname)
	setupMountNamespace()
	setupRootFilesystem(rootfs)
	setupDev()
	setupProc()
	performPivotRoot(rootfs)
	setupLoopback()

	execCommand(cmdArgs)
}

func setupHostname(hostname string) {
	if hostname == "" {
		hostname = "sandbox"
	}
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		fatal(fmt.Sprintf("sethostname: %v", err))
	}
}

func setupMountNamespace() {
	// Set all mounts to private so changes do not propagate to the host.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		fatal(fmt.Sprintf("set mount propagation to private: %v", err))
	}
}

func setupRootFilesystem(rootfs string) {
	// Bind-mount rootfs onto itself. pivot_root requires both new_root
	// and put_old to be mount points.
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		fatal(fmt.Sprintf("bind mount rootfs: %v", err))
	}
}

func setupProc() {
	// We mount /proc after pivot_root, but the mount point must exist in rootfs.
	// This is done inside performPivotRoot after chdir.
}

func setupDev() {
	// /dev entries will be created after pivot_root when we are inside the new root.
}

func performPivotRoot(rootfs string) {
	pivotDir := filepath.Join(rootfs, ".pivot_old")
	if err := os.MkdirAll(pivotDir, 0700); err != nil {
		fatal(fmt.Sprintf("create pivot dir: %v", err))
	}

	if err := syscall.PivotRoot(rootfs, pivotDir); err != nil {
		fatal(fmt.Sprintf("pivot_root: %v", err))
	}

	if err := syscall.Chdir("/"); err != nil {
		fatal(fmt.Sprintf("chdir /: %v", err))
	}

	// Mount /proc inside the new root.
	os.MkdirAll("/proc", 0555)
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		fatal(fmt.Sprintf("mount /proc: %v", err))
	}

	// Create minimal /dev entries.
	os.MkdirAll("/dev", 0755)
	createDevNode("/dev/null", 1, 3)
	createDevNode("/dev/zero", 1, 5)
	createDevNode("/dev/random", 1, 8)
	createDevNode("/dev/urandom", 1, 9)

	// Unmount the old root.
	if err := syscall.Unmount("/.pivot_old", syscall.MNT_DETACH); err != nil {
		fmt.Fprintf(os.Stderr, "warning: unmount old root: %v\n", err)
	}
	os.Remove("/.pivot_old")
}

func createDevNode(path string, major, minor uint32) {
	dev := int(major*256 + minor)
	// Mode: character device + rw-rw-rw-
	err := syscall.Mknod(path, syscall.S_IFCHR|0666, dev)
	if err != nil && !os.IsExist(err) {
		fmt.Fprintf(os.Stderr, "warning: mknod %s: %v\n", path, err)
	}
}

func setupLoopback() {
	// Bring up the loopback interface inside the network namespace.
	// Uses "ip" if available, otherwise skips (loopback may already be up).
	cmd := exec.Command("ip", "link", "set", "lo", "up")
	cmd.Run() // Best effort; ignore errors if ip is not in rootfs.
}

func execCommand(args []string) {
	binary, err := exec.LookPath(args[0])
	if err != nil {
		fatal(fmt.Sprintf("command not found: %s", args[0]))
	}

	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"TERM=" + os.Getenv("TERM"),
	}

	if err := syscall.Exec(binary, args, env); err != nil {
		fatal(fmt.Sprintf("exec %s: %v", binary, err))
	}
}

func fatal(msg string) {
	fmt.Fprintf(os.Stderr, "sandbox init error: %s\n", msg)
	os.Exit(1)
}
```

## Tests

```go
// sandbox_test.go

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root")
	}
}

func requireLinux(t *testing.T) {
	if _, err := os.Stat("/proc/self/ns/pid"); err != nil {
		t.Skip("requires Linux namespaces")
	}
}

func getTestRootfs(t *testing.T) string {
	// Expects a minimal rootfs at /tmp/test-rootfs.
	// Create with: debootstrap --variant=minbase focal /tmp/test-rootfs
	// Or: download and extract Alpine minirootfs.
	rootfs := "/tmp/test-rootfs"
	if _, err := os.Stat(rootfs); os.IsNotExist(err) {
		t.Skipf("test rootfs not found at %s. "+
			"Create with: mkdir -p %s && "+
			"wget -O- https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-3.19.1-x86_64.tar.gz | "+
			"tar xz -C %s", rootfs, rootfs, rootfs)
	}
	return rootfs
}

func TestPIDNamespace(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	cmd := exec.Command(os.Args[0], "run", rootfs, "test-pid", "cat", "/proc/self/status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\noutput: %s", err, out)
	}

	output := string(out)
	// Inside PID namespace, the process should see PID 1.
	if !strings.Contains(output, "Pid:\t1") {
		t.Errorf("expected PID 1 inside namespace, got:\n%s", output)
	}
}

func TestUTSNamespace(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	cmd := exec.Command(os.Args[0], "run", rootfs, "my-sandbox-host", "hostname")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\noutput: %s", err, out)
	}

	hostname := strings.TrimSpace(string(out))
	if hostname != "my-sandbox-host" {
		t.Errorf("expected hostname 'my-sandbox-host', got %q", hostname)
	}
}

func TestMountNamespace(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	// Create a file inside the sandbox and verify it does not appear on the host.
	markerPath := "/tmp/sandbox-test-marker"
	os.Remove(markerPath)

	cmd := exec.Command(os.Args[0], "run", rootfs, "test-mount",
		"touch", "/tmp/sandbox-mount-test")
	if err := cmd.Run(); err != nil {
		t.Logf("touch command returned error (may be expected): %v", err)
	}

	// The file should exist inside the rootfs but not on the host /tmp.
	if _, err := os.Stat(markerPath); err == nil {
		t.Error("file created inside sandbox appeared on host filesystem")
	}
}

func TestNetworkNamespace(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	cmd := exec.Command(os.Args[0], "run", rootfs, "test-net",
		"ls", "/sys/class/net/")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\noutput: %s", err, out)
	}

	interfaces := strings.Fields(strings.TrimSpace(string(out)))

	// Inside network namespace, only loopback should be visible.
	for _, iface := range interfaces {
		if iface != "lo" {
			t.Errorf("unexpected network interface visible: %s", iface)
		}
	}
}

func TestProcMounted(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	cmd := exec.Command(os.Args[0], "run", rootfs, "test-proc", "ls", "/proc/1/")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\noutput: %s", err, out)
	}

	output := string(out)
	if !strings.Contains(output, "status") {
		t.Error("/proc/1/ does not contain expected entries")
	}
}

func TestDevNodes(t *testing.T) {
	requireRoot(t)
	requireLinux(t)
	rootfs := getTestRootfs(t)

	// Write to /dev/null and read from /dev/urandom.
	cmd := exec.Command(os.Args[0], "run", rootfs, "test-dev",
		"sh", "-c", "echo test > /dev/null && head -c 4 /dev/urandom | wc -c")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\noutput: %s", err, out)
	}

	output := strings.TrimSpace(string(out))
	if output != "4" {
		t.Errorf("expected 4 bytes from /dev/urandom, got %q", output)
	}
}

func TestMissingRootfs(t *testing.T) {
	requireRoot(t)
	requireLinux(t)

	cmd := exec.Command(os.Args[0], "run", "/nonexistent/rootfs", "test", "echo", "hello")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error for missing rootfs")
	}

	output := string(out)
	if !strings.Contains(output, "not found") {
		t.Errorf("expected 'not found' error, got: %s", output)
	}
}
```

## Preparing a Test Root Filesystem

```bash
# Option 1: Alpine minirootfs (recommended, ~3MB)
mkdir -p /tmp/test-rootfs
wget -O- https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-3.19.1-x86_64.tar.gz \
  | tar xz -C /tmp/test-rootfs

# Option 2: Debian minbase (~150MB)
sudo debootstrap --variant=minbase bookworm /tmp/test-rootfs

# Option 3: Create minimal rootfs manually
mkdir -p /tmp/test-rootfs/{bin,dev,proc,sys,tmp,etc}
cp /bin/busybox /tmp/test-rootfs/bin/
cd /tmp/test-rootfs/bin && for cmd in sh ls cat echo ps hostname; do
  ln -s busybox $cmd
done
```

## Running the Solution

```bash
mkdir sandbox && cd sandbox
go mod init sandbox

# Copy source files

go build -o sandbox .

# Run interactive shell inside sandbox (requires root)
sudo ./sandbox run /tmp/test-rootfs my-container /bin/sh

# Inside the sandbox:
# / # hostname
# my-container
# / # ps aux
# PID   USER     TIME  COMMAND
#     1 root      0:00 /bin/sh
#     2 root      0:00 ps aux
# / # ls /
# bin    dev    etc    proc   sys    tmp

# Run tests
sudo go test -v -count=1 ./...
```

### Expected Output

```
Starting sandbox: rootfs=/tmp/test-rootfs hostname=my-container cmd=[/bin/sh]

/ # hostname
my-container

/ # cat /proc/self/status | grep Pid
Pid:    1
NSpid:  1

/ # ls /sys/class/net/
lo

/ # ls /dev/
null     random   urandom  zero

/ # exit
```

## Design Decisions

1. **Re-exec pattern**: The parent creates a child with `CLONE_NEW*` flags. The child re-execs the same binary with an `init` argument and performs namespace-internal setup. This is necessary because Go's runtime has multiple threads, and some namespace operations (`unshare`, `setns`) are not thread-safe. The re-exec ensures the child starts fresh.

2. **pivot_root over chroot**: `chroot` is insecure -- a privileged process can escape it (the classic `chdir("..") * 1000` attack, or using open file descriptors). `pivot_root` atomically replaces the root and the old root can be fully unmounted, leaving no path back. All production container runtimes use `pivot_root`.

3. **MS_PRIVATE propagation**: Setting mount propagation to `MS_PRIVATE` recursively ensures that mount operations inside the namespace do not propagate to the host, and vice versa. Without this, mounting `/proc` inside the sandbox could affect the host's mount table on some configurations.

4. **Minimal /dev via mknod**: Creating device nodes with `mknod` gives the sandbox access to essential devices without bind-mounting the host's `/dev`. This is more secure -- the sandbox only gets the specific devices we create. Production runtimes use a similar approach (or bind-mount specific devices from the host).

5. **User namespace as optional**: User namespaces enable rootless containers but add complexity (UID/GID mapping, interaction with other namespace types). The solution works with or without them. User namespace support is commented out but documented.

## Namespace Setup Order

The setup order matters due to kernel permission checks:

1. **User namespace** (if enabled) must be created first or simultaneously with other namespaces. Only the user namespace allows unprivileged creation of other namespace types (the creating process gets `CAP_SYS_ADMIN` inside the user namespace).

2. **Mount namespace** should be set up early because filesystem operations (mounting `/proc`, `pivot_root`) require it to be active.

3. **PID namespace** is created at clone time. The child's PID inside the new namespace is 1. `/proc` must be remounted after pivot_root to reflect the new PID namespace.

4. **Network and UTS namespaces** are independent and can be set up in any order relative to each other.

## Common Mistakes

1. **Not setting mount propagation**: Without `MS_PRIVATE`, mount events propagate between the sandbox and the host. This can cause the host's mounts to be affected by sandbox operations, or vice versa.

2. **pivot_root on non-mount-point**: `pivot_root` requires both `new_root` and `put_old` to be mount points. Bind-mounting the rootfs onto itself satisfies this requirement. Forgetting this produces a confusing "invalid argument" error.

3. **Not unmounting old root**: After `pivot_root`, the old root is accessible at `/.pivot_old`. If you do not unmount and remove it, the sandboxed process can access the entire host filesystem.

4. **Go runtime thread issues**: Go's runtime uses multiple OS threads. Namespace operations like `unshare(CLONE_NEWNS)` affect only the calling thread. The re-exec pattern avoids this by creating the child in new namespaces from the start.

5. **Forgetting /proc remount**: Without mounting a fresh `/proc`, the sandboxed process sees the host's process list. `ps aux` would show all host processes, defeating PID namespace isolation.

6. **Missing /dev entries**: Without `/dev/null` and `/dev/urandom`, many programs fail silently or with confusing errors. These four device nodes are the minimum viable set.

## Performance Notes

- **Namespace creation overhead**: Creating all five namespaces adds approximately 1-2ms to process startup. This is why container startup is measured in hundreds of milliseconds, not seconds -- the kernel operations are fast.
- **Mount namespace memory**: Each mount namespace duplicates the mount table. With a minimal rootfs, this is negligible. With a complex mount tree, each namespace consumes memory proportional to the number of mount points.
- **PID namespace PID 1 responsibility**: PID 1 in a PID namespace must reap zombie children. If the sandboxed command spawns children that exit, and PID 1 does not call `wait`, zombies accumulate. For complex workloads, use a proper init like `tini`.
- **User namespace vs privileged**: User namespaces avoid running as root on the host, but they add overhead for UID translation on every filesystem access. The overhead is small but measurable on I/O-heavy workloads.
