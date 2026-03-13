# 5. Overlay Filesystem

<!--
difficulty: insane
concepts: [overlayfs, copy-on-write, layer-management, whiteout-files, upperdir-lowerdir, filesystem-snapshots]
tools: [go, linux, mount]
estimated_time: 2h
bloom_level: create
prerequisites: [section 38 exercises 1-4, linux filesystem concepts, mount namespaces]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access and OverlayFS kernel support (standard on all modern kernels)
- Completed exercises 1-4 (namespaces, rootfs, networking, cgroups)
- Understanding of Linux mount semantics and filesystem layering concepts

## Learning Objectives

- **Create** overlay filesystem mounts that combine multiple read-only layers with a writable upper layer
- **Design** a layer management system that prepares, stacks, and tears down overlay mounts for container rootfs
- **Evaluate** the copy-on-write behavior and its impact on container filesystem performance and storage efficiency

## The Challenge

Every container runtime uses a layered filesystem to avoid duplicating base images. When ten containers run the same Ubuntu base, they share the read-only image layers and each container gets its own thin writable layer on top. OverlayFS is the kernel filesystem that makes this possible. It takes one or more read-only "lower" directories, a writable "upper" directory, a "work" directory for atomic operations, and presents a unified "merged" view.

In this exercise, you will implement overlay filesystem management for your container runtime. You will create overlay mounts using `syscall.Mount` with the `overlay` filesystem type, manage the directory structure for lower layers, upper layers, and work directories, and integrate this with the `pivot_root` mechanism from exercise 2 so that the container's root filesystem is an overlay mount.

The key complexity is managing multiple lower layers. OCI images consist of many layers stacked in order, and OverlayFS supports this via a colon-separated list in the mount options: `lowerdir=layer3:layer2:layer1` (leftmost is topmost). You must also handle whiteout files -- special files that mark deletions in upper layers so that files deleted in a container do not show through from the lower layers.

When the container exits, the upper layer contains all changes made during the container's lifetime. You can choose to discard it (ephemeral container) or preserve it (for commit/snapshot operations).

## Requirements

1. Create the overlay directory structure: `layers/`, `upper/`, `work/`, and `merged/` per container
2. Mount an overlay filesystem using `syscall.Mount("overlay", merged, "overlay", 0, opts)` with proper options
3. Support multiple lower layers specified as a colon-separated list
4. Integrate the overlay mount as the rootfs for `pivot_root` from exercise 2
5. Implement a `--layer` flag that can be specified multiple times to stack layers
6. Create a helper function to prepare layers from extracted tarballs
7. Handle whiteout files (`.wh.` prefix) correctly in the upper layer
8. Preserve the upper layer after container exit when `--snapshot` flag is provided
9. Clean up overlay mounts and temporary directories when the container exits without `--snapshot`
10. Display storage usage statistics (upper layer size, total layer size) on container exit

## Hints

- The mount options string format is: `lowerdir=/path/layer2:/path/layer1,upperdir=/path/upper,workdir=/path/work`.
- The work directory must be on the same filesystem as the upper directory. Both must be empty before the first mount.
- Use `filepath.Walk` or `fs.WalkDir` to calculate directory sizes for storage statistics.
- Whiteout files are created by the kernel automatically when you delete files in the merged view. A file named `.wh.filename` in the upper layer hides `filename` from lower layers.
- Opaque whiteout (`.wh..wh..opq`) hides an entire directory's contents from lower layers.
- For testing, create simple lower layers with a few files, then verify that modifications only appear in the upper layer.

## Success Criteria

1. The overlay mount presents a unified view of all lower layers plus the upper layer
2. Files written inside the container appear only in the upper layer directory
3. Files deleted inside the container create whiteout entries, not actual deletions in lower layers
4. Multiple lower layers are correctly stacked in the specified order
5. The overlay integrates with `pivot_root` to serve as the container rootfs
6. Container exit without `--snapshot` cleanly removes all overlay directories
7. Container exit with `--snapshot` preserves the upper layer for later reuse
8. Storage statistics are accurate and printed on exit

## Research Resources

- [OverlayFS kernel documentation](https://www.kernel.org/doc/Documentation/filesystems/overlayfs.txt) -- authoritative reference for overlay semantics
- [OCI Image Spec: Layer Filesystem Changeset](https://github.com/opencontainers/image-spec/blob/main/layer.md) -- how container image layers are defined
- [Docker storage drivers: OverlayFS](https://docs.docker.com/storage/storagedriver/overlayfs-driver/) -- practical overlay usage in Docker
- [Whiteout files specification](https://github.com/opencontainers/image-spec/blob/main/layer.md#whiteouts) -- deletion markers in layered filesystems
- [mount(2) man page](https://man7.org/linux/man-pages/man2/mount.2.html) -- system call reference

## What's Next

The next exercise implements OCI image pulling, downloading and extracting container image layers from a registry so you can run real container images rather than manually prepared rootfs directories.

## Summary

- OverlayFS provides copy-on-write layered filesystems by merging read-only lower layers with a writable upper layer
- Mount options specify `lowerdir`, `upperdir`, and `workdir` paths in a single options string
- Multiple lower layers are stacked using colon-separated paths with the leftmost being topmost
- Whiteout files (`.wh.` prefix) handle deletions without modifying read-only lower layers
- The overlay mount integrates with `pivot_root` to serve as the container's root filesystem
- Upper layer preservation enables container snapshots and image commit operations
