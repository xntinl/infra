# 41. Custom File System (In-Memory)

<!--
difficulty: insane
category: systems-programming
languages: [rust]
concepts: [file-systems, inodes, block-allocation, directory-hierarchy, hard-links, symbolic-links, permissions, journaling, wal]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [rust-ownership, data-structures, bitwise-operations, tree-structures, serialization, error-handling]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership and interior mutability (`RefCell`, `Rc`, or arena allocation)
- Tree data structures for directory hierarchies
- Bitwise operations for permission flags and bitmap-based allocation
- Serialization for journal/WAL entries
- Error handling with custom error types

## Learning Objectives

- **Create** an inode-based file system with superblock, inode table, bitmap allocator, and data blocks
- **Implement** POSIX-like file operations including open, read, write, seek, and close with file descriptor management
- **Design** a directory hierarchy supporting hard links, symbolic links, and recursive operations
- **Architect** a write-ahead log (WAL) that provides crash consistency guarantees for metadata operations
- **Evaluate** the trade-offs between different block allocation strategies and their impact on fragmentation

## The Challenge

Every operating system depends on a file system to organize persistent data into files and directories. Beneath the abstractions of paths and file handles lies a concrete data structure: inodes that describe files, bitmaps that track which blocks are free, directories that map names to inode numbers, and journals that ensure metadata stays consistent if the system crashes mid-operation.

Build an in-memory file system from scratch. The file system uses fixed-size blocks for storage, an inode table for file metadata, bitmap-based block allocation, a directory hierarchy with hard and soft links, UNIX-style permissions, and a write-ahead log for crash consistency. Expose the file system through a CLI that supports standard operations (ls, mkdir, cat, echo, ln, chmod, stat).

This is not a toy wrapper around a HashMap. It is a faithful implementation of the structures that real file systems (ext4, XFS, ZFS) use, running in memory instead of on disk.

## Requirements

1. Implement a block layer: a contiguous array of fixed-size blocks (default 4096 bytes). The superblock (block 0) stores file system metadata: block size, total blocks, free block count, inode count, and root inode number
2. Implement a bitmap-based block allocator: a bitmap tracks which blocks are in use. Allocation scans for the first free bit, deallocation clears the bit. Support allocating contiguous runs for sequential writes
3. Implement an inode table: each inode stores file type (regular, directory, symlink), permissions (rwx for user/group/other as a 9-bit mask), owner UID/GID, size in bytes, timestamps (created, modified, accessed), link count, and an array of direct block pointers plus a single indirect block pointer
4. Implement directory entries: a directory is a file whose data blocks contain a list of `(name, inode_number)` pairs. Support `.` and `..` entries. Directory lookup, insertion, and removal must handle name collisions
5. Implement file operations: `create(path)`, `open(path, flags)`, `read(fd, buf, count)`, `write(fd, buf, count)`, `seek(fd, offset, whence)`, `close(fd)`, `delete(path)`. Open files are tracked via a file descriptor table with per-FD position cursors
6. Implement directory operations: `mkdir(path)`, `rmdir(path)` (fails if not empty), `ls(path)` (lists entries with metadata)
7. Implement hard links: `link(target, link_name)` creates a new directory entry pointing to the same inode. Deletion decrements the link count; the inode is freed only when link count reaches 0 and no open file descriptors reference it
8. Implement symbolic links: `symlink(target, link_name)` creates a new inode of type symlink whose data is the target path string. Path resolution follows symlinks transparently, with a recursion limit to detect cycles
9. Implement permissions: each operation checks the permission bits against the current UID/GID. `chmod(path, mode)` modifies permissions. `chown(path, uid, gid)` changes ownership
10. Implement a write-ahead log (WAL): before modifying any metadata (inode updates, bitmap changes, directory entries), write the intended changes to a sequential log. After the metadata is updated, mark the log entry as committed. On recovery (simulated), replay uncommitted log entries to restore consistency
11. Expose the file system through a CLI or REPL: commands include `mkdir`, `ls`, `touch`, `echo "text" > file`, `cat`, `rm`, `ln`, `ln -s`, `chmod`, `stat`, `df` (disk free)

## Acceptance Criteria

- [ ] Files can be created, written to, read from, and deleted with correct content
- [ ] Directories support nested creation (mkdir -p) and listing with metadata
- [ ] Hard links share the same inode: writing through one path is visible through the other
- [ ] Deleting one hard link does not affect other links to the same inode
- [ ] Symbolic links resolve correctly, including relative paths and chain resolution
- [ ] Symlink cycles are detected and return an error instead of infinite recursion
- [ ] Permission checks enforce rwx bits: a read-only file cannot be written, a non-executable directory cannot be listed
- [ ] The block bitmap accurately reflects allocated and free blocks (df output matches reality)
- [ ] WAL recovery works: simulate a crash (abort mid-operation), recover, and verify metadata consistency
- [ ] File descriptors track independent seek positions: two opens of the same file can read different offsets
- [ ] The indirect block pointer works: files larger than (direct_pointers * block_size) bytes are stored correctly
- [ ] Directory "." and ".." entries are correct at every level of the hierarchy
- [ ] Link count is accurate: it reflects the exact number of hard links plus directory references

## Research Resources

- [Operating Systems: Three Easy Pieces, Chapters 39-42 (File System Implementation)](https://pages.cs.wisc.edu/~remzi/OSTEP/) -- OSTEP's coverage of inodes, directories, bitmaps, and crash consistency is the definitive free resource
- [The Design and Implementation of a Log-Structured File System (Rosenblum & Ousterhout)](https://people.eecs.berkeley.edu/~brewer/cs262/LFS.pdf) -- the original LFS paper, foundational for understanding journaling
- [ext4 Disk Layout](https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout) -- real-world inode and block group structure
- [Linux VFS Documentation](https://www.kernel.org/doc/html/latest/filesystems/vfs.html) -- how Linux abstracts file system operations through the Virtual File System layer
- [FUSE (Filesystem in Userspace)](https://github.com/libfuse/libfuse) -- if you want to mount your file system as a real Linux mount point
- [Journaling the Linux ext2fs Filesystem (Tweedie, 2000)](https://pdos.csail.mit.edu/6.828/2020/readings/journal-design.pdf) -- the design behind ext3's journal, directly applicable to WAL implementation
