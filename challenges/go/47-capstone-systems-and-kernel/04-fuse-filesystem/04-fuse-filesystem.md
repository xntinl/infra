<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h
-->

# FUSE Filesystem

## The Challenge

Implement a fully functional userspace filesystem using FUSE (Filesystem in Userspace) in Go that presents a virtual filesystem accessible through the standard VFS interface (ls, cat, cp, mkdir, etc.). Your filesystem must support regular files, directories, symbolic links, hard links, file permissions, and extended attributes, all backed by a storage engine of your choice (in-memory tree, flat files, SQLite, or a custom format). Going beyond a simple in-memory filesystem, you must implement a layered/overlay filesystem that combines a read-only base layer with a writable upper layer (similar to Docker's overlay filesystem), where writes, deletes, and metadata changes are captured in the upper layer while reads fall through to the base layer for unmodified files.

## Requirements

1. Implement the core FUSE operations: `Lookup`, `Getattr`, `Setattr`, `Readdir`, `Open`, `Read`, `Write`, `Create`, `Mkdir`, `Rmdir`, `Unlink`, `Rename`, `Symlink`, `Readlink`, `Link`, `Statfs`, handling all required POSIX semantics including error codes (ENOENT, EEXIST, EISDIR, ENOTDIR, EACCES, etc.).
2. Support file permissions (mode bits: rwxrwxrwx), ownership (uid/gid), and timestamps (atime, mtime, ctime) with proper enforcement on all operations.
3. Implement the overlay layer: the filesystem takes two directory paths as arguments (base and upper); reads check the upper layer first, falling through to the base layer if the file is not found or not modified in the upper layer.
4. Implement copy-on-write for the overlay: when a file from the base layer is opened for writing, copy it to the upper layer first, then apply the write to the upper copy. Subsequent reads of that file serve from the upper layer.
5. Implement whiteout files for deletion in the overlay: when a file from the base layer is deleted, create a whiteout marker in the upper layer (a special file or metadata entry) that causes the file to appear deleted even though it still exists in the base layer.
6. Implement opaque directories: when a directory in the base layer is replaced (rmdir + mkdir) in the upper layer, mark the upper directory as opaque so its contents completely replace (not merge with) the base layer's contents.
7. Support extended attributes (xattr): `Getxattr`, `Setxattr`, `Listxattr`, `Removexattr` operations, used internally for storing overlay metadata (whiteout markers, opaque flags) and available to users.
8. Implement basic caching: maintain an in-memory inode cache with a configurable size limit, evicting least-recently-used entries when full, to avoid repeated disk lookups for hot metadata.

## Hints

- Use the `hanwen/go-fuse` library (specifically the `fs` package, not the older `fuse` package) which provides a modern Go API for implementing FUSE filesystems with an `Inode` tree.
- Each inode in go-fuse implements interfaces like `fs.NodeLookuper`, `fs.NodeOpener`, `fs.NodeReader`, `fs.NodeWriter`, etc.
- For the overlay, create a custom inode type that holds references to both the upper and base paths, checking the upper first on every operation.
- Whiteout files in Linux overlay filesystems are character devices with major/minor 0/0; alternatively, use a dotfile convention like `.wh.<filename>`.
- `Statfs` should aggregate the available space from the upper layer's backing store.
- Test with standard UNIX tools: `ls -la`, `cat`, `echo "data" > file`, `mkdir`, `rm`, `ln -s`, `cp`, `mv`.
- FUSE requires the `fuse` kernel module and the user to be in the `fuse` group (or root).
- Mount with `fusermount3` for unprivileged mounting.

## Success Criteria

1. The filesystem mounts successfully and `ls`, `cat`, `echo > file`, `mkdir`, `rm`, `cp`, `mv`, `ln`, `ln -s` all work correctly.
2. File permissions are enforced: a file created with mode 0444 cannot be written by a non-root user.
3. Overlay reads: files in the base layer are visible in the mounted filesystem; files in the upper layer override base layer files with the same name.
4. Copy-on-write works: modifying a base layer file creates a copy in the upper layer; the original base file is unmodified.
5. Whiteout deletion: deleting a base layer file makes it invisible in the mounted filesystem, but the base file remains untouched.
6. Opaque directories: replacing a base directory in the upper layer shows only the upper layer's contents, not a merge.
7. Extended attributes can be set and read back correctly using `setfattr`/`getfattr` commands.
8. The filesystem handles concurrent access from multiple processes without corruption.

## Research Resources

- FUSE protocol documentation -- https://libfuse.github.io/doxygen/
- hanwen/go-fuse library -- https://github.com/hanwen/go-fuse
- Linux OverlayFS documentation -- https://www.kernel.org/doc/html/latest/filesystems/overlayfs.html
- "FUSE: Filesystem in Userspace" -- original FUSE paper
- Docker overlay2 storage driver -- https://docs.docker.com/storage/storagedriver/overlayfs-driver/
- POSIX filesystem semantics -- IEEE Std 1003.1
