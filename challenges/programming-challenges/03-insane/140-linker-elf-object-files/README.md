# 140. Linker for ELF Object Files

<!--
difficulty: insane
category: systems-programming
languages: [rust]
concepts: [elf-format, symbol-resolution, relocation, section-merging, program-headers, static-linking, segment-layout]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [binary-file-parsing, x86-64-assembly-basics, object-file-concepts, memory-layout, endianness]
-->

## Languages

- Rust (1.75+ stable)

## Prerequisites

- ELF file format fundamentals (headers, sections, segments)
- x86-64 instruction encoding and addressing modes
- Relocation concepts (absolute vs PC-relative)
- Symbol visibility and binding (local, global, weak)
- Virtual memory layout and page alignment

## Learning Objectives

By the end of this challenge you will be able to **create** a static linker that combines ELF relocatable object files into a working executable, performing symbol resolution, relocation processing, and segment layout -- producing a binary that the Linux kernel can load and execute directly.

## The Challenge

Build a static linker that reads multiple ELF relocatable object files (`.o` files produced by `gcc -c` or `nasm`), merges their sections, resolves symbols across objects, processes relocations, and writes a valid ELF executable. The output binary must run on a Linux x86-64 system.

This is not an ELF parser or a hex dumper. You are building the tool that turns compiler output into runnable programs: the linker. You must handle the same symbol resolution rules as `ld` (strong vs weak symbols, multiple definitions, undefined references), apply the same relocation types the compiler emits (R_X86_64_64, R_X86_64_PC32, R_X86_64_PLT32), and produce an executable with correct program headers so the kernel maps it into memory and jumps to the entry point.

## Requirements

- [ ] ELF parser: read 64-bit ELF relocatable objects. Parse ELF header, section headers, symbol table (`.symtab`), string table (`.strtab`), and relocation sections (`.rela.*`)
- [ ] 32-bit ELF support: also parse 32-bit ELF objects (Elf32 header, section headers, symbol table)
- [ ] Section merging: combine `.text` sections from all inputs into one output `.text`, same for `.data`, `.rodata`, `.bss`. Respect alignment requirements
- [ ] Symbol resolution: build a global symbol table from all inputs. Handle local (STB_LOCAL), global (STB_GLOBAL), and weak (STB_WEAK) bindings. Detect duplicate strong symbols, resolve weak vs strong correctly
- [ ] Undefined symbol detection: error with clear message listing which object references which undefined symbol
- [ ] Relocation processing: apply `R_X86_64_64` (absolute 64-bit), `R_X86_64_PC32` (PC-relative 32-bit), `R_X86_64_PLT32` (PC-relative for calls), and `R_X86_64_32S` (signed 32-bit absolute)
- [ ] Entry point: default to `_start` symbol, configurable via `-e` flag
- [ ] Program header generation: create `PT_LOAD` segments for text (R+X) and data (R+W) with correct page-aligned virtual addresses
- [ ] Output: write a valid ELF executable that the Linux kernel loads and executes correctly
- [ ] Virtual address layout: text segment at 0x400000 (conventional), data segment at next page boundary after text
- [ ] BSS handling: `.bss` occupies virtual memory but no file space (`p_filesz < p_memsz`)
- [ ] Command-line interface: `./linker -o output input1.o input2.o [-e entry_symbol]`
- [ ] Error reporting: clear diagnostics for malformed ELF, unsupported relocation types, symbol conflicts

## Hints

1. Process linking in explicit phases: (1) parse all input objects and collect their sections and symbols, (2) merge sections and assign virtual addresses, (3) resolve all symbols to final addresses, (4) apply relocations using resolved addresses, (5) build program headers and write the output. Mixing these phases leads to circular dependencies between addresses and relocations.

2. Relocations encode *how* to patch a reference, not *where* the symbol is. For `R_X86_64_PC32`, the formula is `S + A - P` where S is the symbol's final virtual address, A is the addend from the relocation entry, and P is the address of the byte being patched. Get the signedness wrong and your jumps will land in the wrong place by 4GB.

## Acceptance Criteria

- [ ] Successfully parses 64-bit ELF relocatable objects produced by GCC/NASM
- [ ] Merges `.text`, `.data`, `.rodata`, and `.bss` sections from multiple inputs
- [ ] Resolves global symbols across object files correctly
- [ ] Weak symbols are overridden by strong symbols when both exist
- [ ] Duplicate strong symbol definitions produce a clear error
- [ ] Undefined symbol references produce a clear error listing the referencing object
- [ ] R_X86_64_64, R_X86_64_PC32, and R_X86_64_PLT32 relocations are applied correctly
- [ ] Output ELF executable has valid headers and runs on Linux x86-64
- [ ] A multi-file C program (compiled with `gcc -c -nostdlib`) links and runs via your linker
- [ ] BSS section has correct virtual size but zero file size
- [ ] `cargo test` passes with unit and integration tests

## Research Resources

- [Linkers and Loaders (John Levine)](https://linker.iecc.com/) -- the definitive book on linking, covers symbol resolution, relocation, and ELF format
- [ELF Specification (System V ABI)](https://refspecs.linuxfoundation.org/elf/elf.pdf) -- the formal ELF format specification
- [x86-64 System V ABI](https://gitlab.com/x86-psABIs/x86-64-ABI) -- relocation types, calling conventions, and segment layout for x86-64 Linux
- [Ian Lance Taylor: Linkers (blog series)](https://www.ics.uci.edu/~aburtsev/143A/2017winter/lectures/lecture09-linking/linkers.pdf) -- 20-part series covering every linker concept
- [Oracle Linker and Libraries Guide](https://docs.oracle.com/cd/E23824_01/html/819-0690/) -- detailed ELF section and relocation reference
- [mold linker source code](https://github.com/rui314/mold) -- modern high-performance linker, excellent study material
- [`readelf` and `objdump` man pages](https://man7.org/linux/man-pages/man1/readelf.1.html) -- essential debugging tools for inspecting ELF files
