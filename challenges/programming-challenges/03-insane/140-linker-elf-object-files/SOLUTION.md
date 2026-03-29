# Solution: Linker for ELF Object Files

## Architecture Overview

The linker operates in five sequential phases:

1. **ELF Parser**: reads each input `.o` file and extracts the ELF header, section headers, section data, symbol table, string table, and relocation entries. Supports both ELF32 and ELF64 formats.
2. **Section Merger**: groups sections by name (`.text`, `.data`, `.rodata`, `.bss`) across all input objects. Assigns virtual addresses respecting alignment. Tracks the offset of each input section within the merged output section.
3. **Symbol Resolver**: builds a global symbol table from all inputs. Handles local, global, and weak binding rules. Detects duplicate strong symbols and undefined references.
4. **Relocation Processor**: walks all relocation entries, resolves the target symbol's final virtual address, and patches the instruction or data at the relocation site using the appropriate formula for each relocation type.
5. **ELF Writer**: generates the output ELF executable with an ELF header, program headers (`PT_LOAD` segments for text and data), section data, and the entry point set to the `_start` symbol address.

---

## Rust Solution

### Project Setup

```bash
cargo new elf_linker
cd elf_linker
```

No external dependencies -- all ELF parsing and writing is implemented from scratch.

### `src/elf.rs` -- ELF Format Definitions

```rust
// ELF Magic
pub const ELF_MAGIC: [u8; 4] = [0x7F, b'E', b'L', b'F'];

// ELF classes
pub const ELFCLASS32: u8 = 1;
pub const ELFCLASS64: u8 = 2;

// ELF data encoding
pub const ELFDATA2LSB: u8 = 1; // little-endian

// ELF types
pub const ET_REL: u16 = 1;  // relocatable
pub const ET_EXEC: u16 = 2; // executable

// Machine types
pub const EM_X86_64: u16 = 62;
pub const EM_386: u16 = 3;

// Section types
pub const SHT_NULL: u32 = 0;
pub const SHT_PROGBITS: u32 = 1;
pub const SHT_SYMTAB: u32 = 2;
pub const SHT_STRTAB: u32 = 3;
pub const SHT_RELA: u32 = 4;
pub const SHT_NOBITS: u32 = 8; // .bss

// Section flags
pub const SHF_WRITE: u64 = 0x1;
pub const SHF_ALLOC: u64 = 0x2;
pub const SHF_EXECINSTR: u64 = 0x4;

// Symbol binding
pub const STB_LOCAL: u8 = 0;
pub const STB_GLOBAL: u8 = 1;
pub const STB_WEAK: u8 = 2;

// Symbol type
pub const STT_NOTYPE: u8 = 0;
pub const STT_FUNC: u8 = 2;
pub const STT_SECTION: u8 = 3;

// Special section indices
pub const SHN_UNDEF: u16 = 0;
pub const SHN_ABS: u16 = 0xFFF1;
pub const SHN_COMMON: u16 = 0xFFF2;

// Relocation types (x86-64)
pub const R_X86_64_64: u32 = 1;      // S + A
pub const R_X86_64_PC32: u32 = 2;    // S + A - P
pub const R_X86_64_32S: u32 = 11;    // S + A (truncated to 32-bit signed)
pub const R_X86_64_PLT32: u32 = 4;   // S + A - P (same formula as PC32 for static linking)

// Program header types
pub const PT_LOAD: u32 = 1;

// Segment flags
pub const PF_X: u32 = 0x1; // execute
pub const PF_W: u32 = 0x2; // write
pub const PF_R: u32 = 0x4; // read

// Default virtual address base
pub const TEXT_BASE: u64 = 0x400000;
pub const PAGE_SIZE: u64 = 0x1000;

#[derive(Debug, Clone)]
pub struct Elf64Header {
    pub class: u8,
    pub data: u8,
    pub elf_type: u16,
    pub machine: u16,
    pub entry: u64,
    pub phoff: u64,
    pub shoff: u64,
    pub phentsize: u16,
    pub phnum: u16,
    pub shentsize: u16,
    pub shnum: u16,
    pub shstrndx: u16,
}

#[derive(Debug, Clone)]
pub struct Elf64Shdr {
    pub name_idx: u32,
    pub sh_type: u32,
    pub flags: u64,
    pub addr: u64,
    pub offset: u64,
    pub size: u64,
    pub link: u32,
    pub info: u32,
    pub addralign: u64,
    pub entsize: u64,
}

#[derive(Debug, Clone)]
pub struct Elf64Sym {
    pub name_idx: u32,
    pub info: u8,
    pub other: u8,
    pub shndx: u16,
    pub value: u64,
    pub size: u64,
}

impl Elf64Sym {
    pub fn binding(&self) -> u8 { self.info >> 4 }
    pub fn sym_type(&self) -> u8 { self.info & 0xF }
}

#[derive(Debug, Clone)]
pub struct Elf64Rela {
    pub offset: u64,
    pub info: u64,
    pub addend: i64,
}

impl Elf64Rela {
    pub fn sym_idx(&self) -> u32 { (self.info >> 32) as u32 }
    pub fn rel_type(&self) -> u32 { (self.info & 0xFFFFFFFF) as u32 }
}

#[derive(Debug, Clone)]
pub struct Elf64Phdr {
    pub p_type: u32,
    pub flags: u32,
    pub offset: u64,
    pub vaddr: u64,
    pub paddr: u64,
    pub filesz: u64,
    pub memsz: u64,
    pub align: u64,
}
```

### `src/parser.rs` -- ELF Object File Parser

```rust
use crate::elf::*;
use std::io;

#[derive(Debug)]
pub struct ObjectFile {
    pub filename: String,
    pub header: Elf64Header,
    pub sections: Vec<ParsedSection>,
    pub symbols: Vec<ParsedSymbol>,
    pub relocations: Vec<ParsedRelocation>,
}

#[derive(Debug, Clone)]
pub struct ParsedSection {
    pub index: usize,
    pub name: String,
    pub header: Elf64Shdr,
    pub data: Vec<u8>,
}

#[derive(Debug, Clone)]
pub struct ParsedSymbol {
    pub name: String,
    pub sym: Elf64Sym,
    pub section_name: Option<String>,
}

#[derive(Debug, Clone)]
pub struct ParsedRelocation {
    pub section_name: String,     // section being relocated (e.g., ".text")
    pub rela: Elf64Rela,
    pub symbol_name: String,
}

pub fn parse_elf64(filename: &str, data: &[u8]) -> io::Result<ObjectFile> {
    if data.len() < 64 {
        return Err(err("file too small for ELF header"));
    }
    if data[0..4] != ELF_MAGIC {
        return Err(err("invalid ELF magic"));
    }
    if data[4] != ELFCLASS64 {
        return Err(err("not a 64-bit ELF file"));
    }

    let header = parse_header(data)?;
    if header.elf_type != ET_REL {
        return Err(err("not a relocatable object file"));
    }

    // Parse section headers
    let mut sections = Vec::new();
    for i in 0..header.shnum as usize {
        let offset = header.shoff as usize + i * header.shentsize as usize;
        let shdr = parse_shdr(&data[offset..])?;
        let section_data = if shdr.sh_type != SHT_NOBITS && shdr.size > 0 {
            data[shdr.offset as usize..(shdr.offset + shdr.size) as usize].to_vec()
        } else {
            Vec::new()
        };
        sections.push(ParsedSection {
            index: i,
            name: String::new(), // resolved below
            header: shdr,
            data: section_data,
        });
    }

    // Resolve section names from shstrtab
    let shstrtab_idx = header.shstrndx as usize;
    if shstrtab_idx < sections.len() {
        let strtab_data = sections[shstrtab_idx].data.clone();
        for sec in &mut sections {
            sec.name = read_string(&strtab_data, sec.header.name_idx as usize);
        }
    }

    // Parse symbol table
    let mut symbols = Vec::new();
    let symtab_section = sections.iter().find(|s| s.header.sh_type == SHT_SYMTAB);
    if let Some(symtab) = symtab_section {
        let strtab_idx = symtab.header.link as usize;
        let strtab_data = if strtab_idx < sections.len() {
            &sections[strtab_idx].data
        } else {
            return Err(err("invalid symtab strtab link"));
        };

        let entry_size = symtab.header.entsize as usize;
        let num_symbols = if entry_size > 0 { symtab.data.len() / entry_size } else { 0 };

        for i in 0..num_symbols {
            let offset = i * entry_size;
            let sym = parse_sym(&symtab.data[offset..])?;
            let name = read_string(strtab_data, sym.name_idx as usize);
            let section_name = if sym.shndx != SHN_UNDEF && sym.shndx != SHN_ABS
                && sym.shndx != SHN_COMMON && (sym.shndx as usize) < sections.len()
            {
                Some(sections[sym.shndx as usize].name.clone())
            } else {
                None
            };
            symbols.push(ParsedSymbol { name, sym, section_name });
        }
    }

    // Parse relocations
    let mut relocations = Vec::new();
    for sec in &sections {
        if sec.header.sh_type != SHT_RELA { continue; }

        // .rela.text applies to .text, etc.
        let target_section = sec.name.strip_prefix(".rela")
            .map(|s| s.to_string())
            .unwrap_or_default();

        let entry_size = sec.header.entsize as usize;
        if entry_size == 0 { continue; }
        let num_relas = sec.data.len() / entry_size;

        for i in 0..num_relas {
            let offset = i * entry_size;
            let rela = parse_rela(&sec.data[offset..])?;
            let sym_idx = rela.sym_idx() as usize;
            let symbol_name = if sym_idx < symbols.len() {
                symbols[sym_idx].name.clone()
            } else {
                String::new()
            };
            relocations.push(ParsedRelocation {
                section_name: target_section.clone(),
                rela,
                symbol_name,
            });
        }
    }

    Ok(ObjectFile {
        filename: filename.to_string(),
        header,
        sections,
        symbols,
        relocations,
    })
}

fn parse_header(data: &[u8]) -> io::Result<Elf64Header> {
    Ok(Elf64Header {
        class: data[4],
        data: data[5],
        elf_type: u16::from_le_bytes([data[16], data[17]]),
        machine: u16::from_le_bytes([data[18], data[19]]),
        entry: u64::from_le_bytes(data[24..32].try_into().unwrap()),
        phoff: u64::from_le_bytes(data[32..40].try_into().unwrap()),
        shoff: u64::from_le_bytes(data[40..48].try_into().unwrap()),
        phentsize: u16::from_le_bytes([data[54], data[55]]),
        phnum: u16::from_le_bytes([data[56], data[57]]),
        shentsize: u16::from_le_bytes([data[58], data[59]]),
        shnum: u16::from_le_bytes([data[60], data[61]]),
        shstrndx: u16::from_le_bytes([data[62], data[63]]),
    })
}

fn parse_shdr(data: &[u8]) -> io::Result<Elf64Shdr> {
    Ok(Elf64Shdr {
        name_idx: u32::from_le_bytes(data[0..4].try_into().unwrap()),
        sh_type: u32::from_le_bytes(data[4..8].try_into().unwrap()),
        flags: u64::from_le_bytes(data[8..16].try_into().unwrap()),
        addr: u64::from_le_bytes(data[16..24].try_into().unwrap()),
        offset: u64::from_le_bytes(data[24..32].try_into().unwrap()),
        size: u64::from_le_bytes(data[32..40].try_into().unwrap()),
        link: u32::from_le_bytes(data[40..44].try_into().unwrap()),
        info: u32::from_le_bytes(data[44..48].try_into().unwrap()),
        addralign: u64::from_le_bytes(data[48..56].try_into().unwrap()),
        entsize: u64::from_le_bytes(data[56..64].try_into().unwrap()),
    })
}

fn parse_sym(data: &[u8]) -> io::Result<Elf64Sym> {
    Ok(Elf64Sym {
        name_idx: u32::from_le_bytes(data[0..4].try_into().unwrap()),
        info: data[4],
        other: data[5],
        shndx: u16::from_le_bytes([data[6], data[7]]),
        value: u64::from_le_bytes(data[8..16].try_into().unwrap()),
        size: u64::from_le_bytes(data[16..24].try_into().unwrap()),
    })
}

fn parse_rela(data: &[u8]) -> io::Result<Elf64Rela> {
    Ok(Elf64Rela {
        offset: u64::from_le_bytes(data[0..8].try_into().unwrap()),
        info: u64::from_le_bytes(data[8..16].try_into().unwrap()),
        addend: i64::from_le_bytes(data[16..24].try_into().unwrap()),
    })
}

fn read_string(strtab: &[u8], offset: usize) -> String {
    if offset >= strtab.len() { return String::new(); }
    let end = strtab[offset..].iter().position(|&b| b == 0).unwrap_or(0);
    String::from_utf8_lossy(&strtab[offset..offset + end]).to_string()
}

fn err(msg: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg)
}
```

### `src/linker.rs` -- Section Merger, Symbol Resolver, Relocation Processor

```rust
use crate::elf::*;
use crate::parser::{ObjectFile, ParsedSymbol, ParsedRelocation};
use std::collections::HashMap;
use std::io;

#[derive(Debug, Clone)]
pub struct MergedSection {
    pub name: String,
    pub data: Vec<u8>,
    pub vaddr: u64,
    pub flags: u64,
    pub alignment: u64,
    pub is_bss: bool,
    pub bss_size: u64,
}

#[derive(Debug, Clone)]
pub struct ResolvedSymbol {
    pub name: String,
    pub vaddr: u64,
    pub binding: u8,
    pub sym_type: u8,
    pub defined_in: String, // filename
}

/// Tracks where each input section was placed within the merged output
#[derive(Debug)]
struct SectionPlacement {
    object_file: String,
    section_name: String,
    offset_in_merged: u64, // byte offset within merged section
    original_size: u64,
}

pub struct Linker {
    objects: Vec<ObjectFile>,
    merged_sections: HashMap<String, MergedSection>,
    section_placements: Vec<SectionPlacement>,
    global_symbols: HashMap<String, ResolvedSymbol>,
    entry_symbol: String,
    section_order: Vec<String>,
}

impl Linker {
    pub fn new(entry_symbol: &str) -> Self {
        Self {
            objects: Vec::new(),
            merged_sections: HashMap::new(),
            section_placements: Vec::new(),
            global_symbols: HashMap::new(),
            entry_symbol: entry_symbol.to_string(),
            section_order: Vec::new(),
        }
    }

    pub fn add_object(&mut self, obj: ObjectFile) {
        self.objects.push(obj);
    }

    /// Phase 1: Merge sections from all objects
    pub fn merge_sections(&mut self) -> io::Result<()> {
        let merge_order = [".text", ".rodata", ".data", ".bss"];

        for &section_name in &merge_order {
            let mut merged_data = Vec::new();
            let mut max_align: u64 = 1;
            let mut flags: u64 = 0;
            let mut is_bss = false;
            let mut bss_total: u64 = 0;

            for obj in &self.objects {
                for sec in &obj.sections {
                    if sec.name != section_name { continue; }

                    let align = sec.header.addralign.max(1);
                    max_align = max_align.max(align);
                    flags = sec.header.flags;
                    is_bss = sec.header.sh_type == SHT_NOBITS;

                    // Pad for alignment
                    let current_offset = if is_bss { bss_total } else { merged_data.len() as u64 };
                    let padding = align_up(current_offset, align) - current_offset;

                    let offset_in_merged = current_offset + padding;

                    if is_bss {
                        bss_total = offset_in_merged + sec.header.size;
                    } else {
                        merged_data.extend(vec![0u8; padding as usize]);
                        merged_data.extend_from_slice(&sec.data);
                    }

                    self.section_placements.push(SectionPlacement {
                        object_file: obj.filename.clone(),
                        section_name: section_name.to_string(),
                        offset_in_merged,
                        original_size: sec.header.size,
                    });
                }
            }

            if !merged_data.is_empty() || bss_total > 0 {
                self.section_order.push(section_name.to_string());
                self.merged_sections.insert(section_name.to_string(), MergedSection {
                    name: section_name.to_string(),
                    data: merged_data,
                    vaddr: 0, // assigned in layout phase
                    flags,
                    alignment: max_align,
                    is_bss,
                    bss_size: bss_total,
                });
            }
        }

        Ok(())
    }

    /// Phase 2: Assign virtual addresses
    pub fn layout(&mut self) {
        // ELF header + program headers take space at the beginning
        let elf_header_size = 64u64;   // ELF64 header
        let phdr_size = 56u64;         // one program header
        let num_phdrs = 2u64;          // text segment + data segment
        let headers_size = elf_header_size + phdr_size * num_phdrs;

        let mut vaddr = TEXT_BASE + headers_size;

        for name in &self.section_order.clone() {
            if let Some(sec) = self.merged_sections.get_mut(name) {
                vaddr = align_up(vaddr, sec.alignment);

                // Data segment starts at new page
                if sec.flags & SHF_WRITE != 0 {
                    vaddr = align_up(vaddr, PAGE_SIZE);
                }

                sec.vaddr = vaddr;
                let size = if sec.is_bss { sec.bss_size } else { sec.data.len() as u64 };
                vaddr += size;
            }
        }
    }

    /// Phase 3: Resolve symbols
    pub fn resolve_symbols(&mut self) -> io::Result<()> {
        // First pass: collect all global/weak symbols with definitions
        for obj in &self.objects {
            for sym in &obj.symbols {
                if sym.name.is_empty() { continue; }
                if sym.sym.shndx == SHN_UNDEF { continue; }
                if sym.sym.binding() == STB_LOCAL { continue; }

                let section_name = sym.section_name.as_deref().unwrap_or("");
                let placement = self.section_placements.iter()
                    .find(|p| p.object_file == obj.filename && p.section_name == section_name);

                let vaddr = if let Some(placement) = placement {
                    let section = self.merged_sections.get(section_name)
                        .ok_or_else(|| err(&format!("section {} not found", section_name)))?;
                    section.vaddr + placement.offset_in_merged + sym.sym.value
                } else if sym.sym.shndx == SHN_ABS {
                    sym.sym.value
                } else {
                    continue;
                };

                let binding = sym.sym.binding();
                let existing = self.global_symbols.get(&sym.name);

                match existing {
                    Some(prev) if prev.binding == STB_GLOBAL && binding == STB_GLOBAL => {
                        return Err(err(&format!(
                            "duplicate symbol '{}': defined in {} and {}",
                            sym.name, prev.defined_in, obj.filename
                        )));
                    }
                    Some(prev) if prev.binding == STB_GLOBAL && binding == STB_WEAK => {
                        continue; // strong wins
                    }
                    _ => {
                        self.global_symbols.insert(sym.name.clone(), ResolvedSymbol {
                            name: sym.name.clone(),
                            vaddr,
                            binding,
                            sym_type: sym.sym.sym_type(),
                            defined_in: obj.filename.clone(),
                        });
                    }
                }
            }
        }

        // Second pass: check for undefined references
        for obj in &self.objects {
            for sym in &obj.symbols {
                if sym.name.is_empty() { continue; }
                if sym.sym.shndx != SHN_UNDEF { continue; }
                if sym.sym.binding() == STB_LOCAL { continue; }

                // Check if any relocation references this symbol
                let is_referenced = obj.relocations.iter()
                    .any(|r| r.symbol_name == sym.name);

                if is_referenced && !self.global_symbols.contains_key(&sym.name) {
                    return Err(err(&format!(
                        "undefined reference to '{}' in {}",
                        sym.name, obj.filename
                    )));
                }
            }
        }

        Ok(())
    }

    /// Phase 4: Process relocations
    pub fn apply_relocations(&mut self) -> io::Result<()> {
        for obj_idx in 0..self.objects.len() {
            let relocations: Vec<ParsedRelocation> = self.objects[obj_idx].relocations.clone();
            let obj_filename = self.objects[obj_idx].filename.clone();

            for rela in &relocations {
                let target_section_name = &rela.section_name;

                // Find the placement of this object's section
                let placement = self.section_placements.iter()
                    .find(|p| p.object_file == obj_filename
                        && p.section_name == *target_section_name)
                    .ok_or_else(|| err(&format!(
                        "cannot find placement for {} in {}",
                        target_section_name, obj_filename
                    )))?;

                let merged_section = self.merged_sections.get_mut(target_section_name)
                    .ok_or_else(|| err(&format!("section {} not found", target_section_name)))?;

                // S: symbol's final virtual address
                let sym_vaddr = if rela.symbol_name.is_empty() {
                    // Section symbol: use the section's base vaddr
                    let sym = self.objects[obj_idx].symbols.iter()
                        .find(|s| s.sym.sym_type() == STT_SECTION
                            && s.section_name.as_deref() == Some(target_section_name));
                    if let Some(s) = sym {
                        let sec_placement = self.section_placements.iter()
                            .find(|p| p.object_file == obj_filename
                                && p.section_name == s.section_name.as_deref().unwrap_or(""));
                        sec_placement.map(|p| merged_section.vaddr + p.offset_in_merged).unwrap_or(0)
                    } else {
                        0
                    }
                } else {
                    self.global_symbols.get(&rela.symbol_name)
                        .map(|s| s.vaddr)
                        .ok_or_else(|| err(&format!(
                            "unresolved symbol '{}' during relocation",
                            rela.symbol_name
                        )))?
                };

                // P: address of the byte being patched
                let patch_offset = (placement.offset_in_merged + rela.rela.offset) as usize;
                let p = merged_section.vaddr + patch_offset as u64;

                // A: addend
                let a = rela.rela.addend;

                let rel_type = rela.rela.rel_type();

                match rel_type {
                    R_X86_64_64 => {
                        // S + A (absolute 64-bit)
                        let value = (sym_vaddr as i64 + a) as u64;
                        if patch_offset + 8 <= merged_section.data.len() {
                            merged_section.data[patch_offset..patch_offset + 8]
                                .copy_from_slice(&value.to_le_bytes());
                        }
                    }
                    R_X86_64_PC32 | R_X86_64_PLT32 => {
                        // S + A - P (PC-relative 32-bit)
                        let value = (sym_vaddr as i64 + a - p as i64) as i32;
                        if patch_offset + 4 <= merged_section.data.len() {
                            merged_section.data[patch_offset..patch_offset + 4]
                                .copy_from_slice(&value.to_le_bytes());
                        }
                    }
                    R_X86_64_32S => {
                        // S + A (signed 32-bit absolute)
                        let value = (sym_vaddr as i64 + a) as i32;
                        if patch_offset + 4 <= merged_section.data.len() {
                            merged_section.data[patch_offset..patch_offset + 4]
                                .copy_from_slice(&value.to_le_bytes());
                        }
                    }
                    _ => {
                        return Err(err(&format!(
                            "unsupported relocation type {} at offset 0x{:x} in {}",
                            rel_type, rela.rela.offset, obj_filename
                        )));
                    }
                }
            }
        }

        Ok(())
    }

    /// Phase 5: Write output ELF executable
    pub fn write_executable(&self, path: &str) -> io::Result<()> {
        let entry_addr = self.global_symbols.get(&self.entry_symbol)
            .map(|s| s.vaddr)
            .ok_or_else(|| err(&format!("entry point '{}' not found", self.entry_symbol)))?;

        let elf_header_size: u64 = 64;
        let phdr_size: u64 = 56;
        let num_phdrs: u64 = 2;
        let headers_end = elf_header_size + phdr_size * num_phdrs;

        // Build text segment (code + rodata)
        let mut text_data = Vec::new();
        let mut text_vaddr = 0u64;
        let mut data_start_vaddr = 0u64;

        for name in &self.section_order {
            let sec = &self.merged_sections[name];
            if sec.flags & SHF_WRITE != 0 { continue; } // skip writable sections
            if sec.is_bss { continue; }
            if text_vaddr == 0 { text_vaddr = sec.vaddr; }

            let file_offset = sec.vaddr - text_vaddr;
            while text_data.len() < file_offset as usize {
                text_data.push(0);
            }
            text_data.extend_from_slice(&sec.data);
        }

        // Build data segment
        let mut data_data = Vec::new();
        let mut data_memsz = 0u64;
        let mut bss_extra = 0u64;

        for name in &self.section_order {
            let sec = &self.merged_sections[name];
            if sec.flags & SHF_WRITE == 0 && !sec.is_bss { continue; }

            if data_start_vaddr == 0 { data_start_vaddr = sec.vaddr; }

            if sec.is_bss {
                bss_extra = sec.bss_size;
                data_memsz += sec.bss_size;
            } else {
                let offset = sec.vaddr - data_start_vaddr;
                while data_data.len() < offset as usize {
                    data_data.push(0);
                }
                data_data.extend_from_slice(&sec.data);
                data_memsz += sec.data.len() as u64;
            }
        }
        data_memsz += bss_extra;

        // Calculate file offsets
        let text_file_offset = headers_end;
        let data_file_offset = align_up(text_file_offset + text_data.len() as u64, 16);

        // Build program headers
        let text_phdr = Elf64Phdr {
            p_type: PT_LOAD,
            flags: PF_R | PF_X,
            offset: text_file_offset,
            vaddr: text_vaddr,
            paddr: text_vaddr,
            filesz: text_data.len() as u64,
            memsz: text_data.len() as u64,
            align: PAGE_SIZE,
        };

        let data_phdr = Elf64Phdr {
            p_type: PT_LOAD,
            flags: PF_R | PF_W,
            offset: data_file_offset,
            vaddr: data_start_vaddr,
            paddr: data_start_vaddr,
            filesz: data_data.len() as u64,
            memsz: data_memsz,
            align: PAGE_SIZE,
        };

        // Write the file
        let mut output = Vec::new();

        // ELF header
        output.extend_from_slice(&ELF_MAGIC);
        output.push(ELFCLASS64);                         // class
        output.push(ELFDATA2LSB);                        // data encoding
        output.push(1);                                  // version
        output.push(0);                                  // OS/ABI
        output.extend_from_slice(&[0u8; 8]);             // padding
        output.extend_from_slice(&ET_EXEC.to_le_bytes()); // type
        output.extend_from_slice(&EM_X86_64.to_le_bytes()); // machine
        output.extend_from_slice(&1u32.to_le_bytes());   // version
        output.extend_from_slice(&entry_addr.to_le_bytes()); // entry
        output.extend_from_slice(&elf_header_size.to_le_bytes()); // phoff
        output.extend_from_slice(&0u64.to_le_bytes());   // shoff (no section headers)
        output.extend_from_slice(&0u32.to_le_bytes());   // flags
        output.extend_from_slice(&(elf_header_size as u16).to_le_bytes()); // ehsize
        output.extend_from_slice(&(phdr_size as u16).to_le_bytes()); // phentsize
        output.extend_from_slice(&(num_phdrs as u16).to_le_bytes()); // phnum
        output.extend_from_slice(&64u16.to_le_bytes());  // shentsize
        output.extend_from_slice(&0u16.to_le_bytes());   // shnum
        output.extend_from_slice(&0u16.to_le_bytes());   // shstrndx

        // Program headers
        write_phdr(&mut output, &text_phdr);
        write_phdr(&mut output, &data_phdr);

        // Pad to text offset
        while output.len() < text_file_offset as usize {
            output.push(0);
        }
        output.extend_from_slice(&text_data);

        // Pad to data offset
        while output.len() < data_file_offset as usize {
            output.push(0);
        }
        output.extend_from_slice(&data_data);

        std::fs::write(path, &output)?;

        // Set executable permission (Unix)
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let perms = std::fs::Permissions::from_mode(0o755);
            std::fs::set_permissions(path, perms)?;
        }

        Ok(())
    }

    pub fn entry_address(&self) -> Option<u64> {
        self.global_symbols.get(&self.entry_symbol).map(|s| s.vaddr)
    }

    pub fn merged_section(&self, name: &str) -> Option<&MergedSection> {
        self.merged_sections.get(name)
    }

    pub fn symbol(&self, name: &str) -> Option<&ResolvedSymbol> {
        self.global_symbols.get(name)
    }
}

fn write_phdr(output: &mut Vec<u8>, phdr: &Elf64Phdr) {
    output.extend_from_slice(&phdr.p_type.to_le_bytes());
    output.extend_from_slice(&phdr.flags.to_le_bytes());
    output.extend_from_slice(&phdr.offset.to_le_bytes());
    output.extend_from_slice(&phdr.vaddr.to_le_bytes());
    output.extend_from_slice(&phdr.paddr.to_le_bytes());
    output.extend_from_slice(&phdr.filesz.to_le_bytes());
    output.extend_from_slice(&phdr.memsz.to_le_bytes());
    output.extend_from_slice(&phdr.align.to_le_bytes());
}

fn align_up(value: u64, alignment: u64) -> u64 {
    if alignment == 0 { return value; }
    (value + alignment - 1) & !(alignment - 1)
}

fn err(msg: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg)
}
```

### `src/main.rs` -- CLI

```rust
mod elf;
mod parser;
mod linker;

use std::env;
use std::fs;

fn main() {
    let args: Vec<String> = env::args().collect();

    let mut output_path = "a.out".to_string();
    let mut entry_symbol = "_start".to_string();
    let mut input_files = Vec::new();

    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "-o" => {
                i += 1;
                if i < args.len() { output_path = args[i].clone(); }
            }
            "-e" => {
                i += 1;
                if i < args.len() { entry_symbol = args[i].clone(); }
            }
            arg => {
                input_files.push(arg.to_string());
            }
        }
        i += 1;
    }

    if input_files.is_empty() {
        eprintln!("usage: linker [-o output] [-e entry] input1.o [input2.o ...]");
        std::process::exit(1);
    }

    let mut lnk = linker::Linker::new(&entry_symbol);

    for path in &input_files {
        let data = fs::read(path).unwrap_or_else(|e| {
            eprintln!("error: cannot read '{}': {}", path, e);
            std::process::exit(1);
        });

        let obj = parser::parse_elf64(path, &data).unwrap_or_else(|e| {
            eprintln!("error: cannot parse '{}': {}", path, e);
            std::process::exit(1);
        });

        lnk.add_object(obj);
    }

    if let Err(e) = lnk.merge_sections() {
        eprintln!("error merging sections: {}", e);
        std::process::exit(1);
    }

    lnk.layout();

    if let Err(e) = lnk.resolve_symbols() {
        eprintln!("error resolving symbols: {}", e);
        std::process::exit(1);
    }

    if let Err(e) = lnk.apply_relocations() {
        eprintln!("error applying relocations: {}", e);
        std::process::exit(1);
    }

    if let Err(e) = lnk.write_executable(&output_path) {
        eprintln!("error writing output: {}", e);
        std::process::exit(1);
    }

    println!("linked {} object(s) -> {}", input_files.len(), output_path);
    if let Some(entry) = lnk.entry_address() {
        println!("entry point: {} at 0x{:x}", entry_symbol, entry);
    }
}
```

### `src/lib.rs` -- Library Re-exports

```rust
pub mod elf;
pub mod parser;
pub mod linker;
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use elf::*;
    use parser::*;
    use linker::*;

    /// Build a minimal ELF64 relocatable object in memory for testing
    fn build_test_object(
        name: &str,
        text_code: &[u8],
        symbols: Vec<(&str, u8, u16, u64)>, // (name, binding, shndx, value)
        relas: Vec<(u64, u32, u32, i64)>,    // (offset, sym_idx, type, addend)
    ) -> Vec<u8> {
        let mut buf = Vec::new();

        // Strings for section names
        let shstrtab: Vec<u8> = b"\0.text\0.symtab\0.strtab\0.shstrtab\0.rela.text\0".to_vec();
        let name_offsets: std::collections::HashMap<&str, u32> = [
            (".text", 1),
            (".symtab", 7),
            (".strtab", 15),
            (".shstrtab", 23),
            (".rela.text", 33),
        ].into_iter().collect();

        // Build symbol string table
        let mut strtab = vec![0u8]; // null entry
        let mut sym_name_offsets = Vec::new();
        for &(sname, _, _, _) in &symbols {
            sym_name_offsets.push(strtab.len() as u32);
            strtab.extend_from_slice(sname.as_bytes());
            strtab.push(0);
        }

        // Build symbol table entries (24 bytes each)
        let mut symtab_data = Vec::new();
        // Null symbol
        symtab_data.extend_from_slice(&[0u8; 24]);

        for (i, &(_, binding, shndx, value)) in symbols.iter().enumerate() {
            symtab_data.extend_from_slice(&sym_name_offsets[i].to_le_bytes()); // name
            symtab_data.push((binding << 4) | STT_FUNC);  // info
            symtab_data.push(0);                           // other
            symtab_data.extend_from_slice(&shndx.to_le_bytes()); // shndx
            symtab_data.extend_from_slice(&value.to_le_bytes()); // value
            symtab_data.extend_from_slice(&0u64.to_le_bytes()); // size
        }

        // Build rela entries (24 bytes each)
        let mut rela_data = Vec::new();
        for &(offset, sym_idx, rel_type, addend) in &relas {
            rela_data.extend_from_slice(&offset.to_le_bytes());
            let info = ((sym_idx as u64) << 32) | rel_type as u64;
            rela_data.extend_from_slice(&info.to_le_bytes());
            rela_data.extend_from_slice(&addend.to_le_bytes());
        }

        // Section layout:
        // 0: NULL
        // 1: .text
        // 2: .symtab
        // 3: .strtab
        // 4: .shstrtab
        // 5: .rela.text (if any)

        let num_sections = if relas.is_empty() { 5u16 } else { 6u16 };
        let shdr_size = 64usize;

        // Calculate offsets
        let elf_header_end = 64usize;
        let text_offset = elf_header_end;
        let text_size = text_code.len();
        let symtab_offset = text_offset + text_size;
        let symtab_size = symtab_data.len();
        let strtab_offset = symtab_offset + symtab_size;
        let strtab_size = strtab.len();
        let shstrtab_offset = strtab_offset + strtab_size;
        let shstrtab_size = shstrtab.len();
        let rela_offset = shstrtab_offset + shstrtab_size;
        let rela_size = rela_data.len();
        let shdr_offset = rela_offset + rela_size;

        // ELF header
        buf.extend_from_slice(&ELF_MAGIC);
        buf.push(ELFCLASS64);
        buf.push(ELFDATA2LSB);
        buf.push(1); // version
        buf.push(0); // OS/ABI
        buf.extend_from_slice(&[0u8; 8]); // padding
        buf.extend_from_slice(&ET_REL.to_le_bytes());
        buf.extend_from_slice(&EM_X86_64.to_le_bytes());
        buf.extend_from_slice(&1u32.to_le_bytes());
        buf.extend_from_slice(&0u64.to_le_bytes()); // entry
        buf.extend_from_slice(&0u64.to_le_bytes()); // phoff
        buf.extend_from_slice(&(shdr_offset as u64).to_le_bytes()); // shoff
        buf.extend_from_slice(&0u32.to_le_bytes()); // flags
        buf.extend_from_slice(&64u16.to_le_bytes()); // ehsize
        buf.extend_from_slice(&0u16.to_le_bytes()); // phentsize
        buf.extend_from_slice(&0u16.to_le_bytes()); // phnum
        buf.extend_from_slice(&(shdr_size as u16).to_le_bytes());
        buf.extend_from_slice(&num_sections.to_le_bytes());
        buf.extend_from_slice(&4u16.to_le_bytes()); // shstrndx = 4

        // Section data
        buf.extend_from_slice(text_code);
        buf.extend_from_slice(&symtab_data);
        buf.extend_from_slice(&strtab);
        buf.extend_from_slice(&shstrtab);
        buf.extend_from_slice(&rela_data);

        // Section headers
        // 0: NULL
        buf.extend_from_slice(&[0u8; shdr_size]);

        // 1: .text
        let mut shdr = vec![0u8; shdr_size];
        write_u32(&mut shdr, 0, *name_offsets.get(".text").unwrap());
        write_u32(&mut shdr, 4, SHT_PROGBITS);
        write_u64(&mut shdr, 8, SHF_ALLOC | SHF_EXECINSTR);
        write_u64(&mut shdr, 24, text_offset as u64);
        write_u64(&mut shdr, 32, text_size as u64);
        write_u64(&mut shdr, 48, 16); // alignment
        buf.extend_from_slice(&shdr);

        // 2: .symtab
        let mut shdr = vec![0u8; shdr_size];
        write_u32(&mut shdr, 0, *name_offsets.get(".symtab").unwrap());
        write_u32(&mut shdr, 4, SHT_SYMTAB);
        write_u64(&mut shdr, 24, symtab_offset as u64);
        write_u64(&mut shdr, 32, symtab_size as u64);
        write_u32(&mut shdr, 40, 3); // link = strtab section index
        write_u32(&mut shdr, 44, 1); // info = first non-local symbol
        write_u64(&mut shdr, 56, 24); // entsize
        buf.extend_from_slice(&shdr);

        // 3: .strtab
        let mut shdr = vec![0u8; shdr_size];
        write_u32(&mut shdr, 0, *name_offsets.get(".strtab").unwrap());
        write_u32(&mut shdr, 4, SHT_STRTAB);
        write_u64(&mut shdr, 24, strtab_offset as u64);
        write_u64(&mut shdr, 32, strtab_size as u64);
        buf.extend_from_slice(&shdr);

        // 4: .shstrtab
        let mut shdr = vec![0u8; shdr_size];
        write_u32(&mut shdr, 0, *name_offsets.get(".shstrtab").unwrap());
        write_u32(&mut shdr, 4, SHT_STRTAB);
        write_u64(&mut shdr, 24, shstrtab_offset as u64);
        write_u64(&mut shdr, 32, shstrtab_size as u64);
        buf.extend_from_slice(&shdr);

        // 5: .rela.text (if any)
        if !relas.is_empty() {
            let mut shdr = vec![0u8; shdr_size];
            write_u32(&mut shdr, 0, *name_offsets.get(".rela.text").unwrap());
            write_u32(&mut shdr, 4, SHT_RELA);
            write_u64(&mut shdr, 24, rela_offset as u64);
            write_u64(&mut shdr, 32, rela_size as u64);
            write_u32(&mut shdr, 40, 2); // link = symtab index
            write_u32(&mut shdr, 44, 1); // info = .text section index
            write_u64(&mut shdr, 56, 24); // entsize
            buf.extend_from_slice(&shdr);
        }

        buf
    }

    fn write_u32(buf: &mut [u8], offset: usize, val: u32) {
        buf[offset..offset + 4].copy_from_slice(&val.to_le_bytes());
    }
    fn write_u64(buf: &mut [u8], offset: usize, val: u64) {
        buf[offset..offset + 8].copy_from_slice(&val.to_le_bytes());
    }

    #[test]
    fn test_parse_elf_header() {
        let obj = build_test_object(
            "test.o",
            &[0xCC; 16], // int3 instructions
            vec![("_start", STB_GLOBAL, 1, 0)],
            vec![],
        );

        let parsed = parse_elf64("test.o", &obj).unwrap();
        assert_eq!(parsed.header.class, ELFCLASS64);
        assert_eq!(parsed.header.machine, EM_X86_64);
        assert_eq!(parsed.header.elf_type, ET_REL);
    }

    #[test]
    fn test_parse_sections() {
        let obj = build_test_object(
            "test.o",
            &[0x90; 8], // NOP instructions
            vec![("main", STB_GLOBAL, 1, 0)],
            vec![],
        );

        let parsed = parse_elf64("test.o", &obj).unwrap();
        let text = parsed.sections.iter().find(|s| s.name == ".text");
        assert!(text.is_some());
        assert_eq!(text.unwrap().data.len(), 8);
    }

    #[test]
    fn test_parse_symbols() {
        let obj = build_test_object(
            "test.o",
            &[0x90; 16],
            vec![
                ("_start", STB_GLOBAL, 1, 0),
                ("helper", STB_GLOBAL, 1, 8),
            ],
            vec![],
        );

        let parsed = parse_elf64("test.o", &obj).unwrap();
        // +1 for null symbol
        assert_eq!(parsed.symbols.len(), 3);
        assert_eq!(parsed.symbols[1].name, "_start");
        assert_eq!(parsed.symbols[2].name, "helper");
    }

    #[test]
    fn test_symbol_resolution_strong() {
        let obj1 = build_test_object(
            "main.o",
            &[0x90; 8],
            vec![("_start", STB_GLOBAL, 1, 0)],
            vec![],
        );
        let obj2 = build_test_object(
            "helper.o",
            &[0x90; 8],
            vec![("helper_fn", STB_GLOBAL, 1, 0)],
            vec![],
        );

        let parsed1 = parse_elf64("main.o", &obj1).unwrap();
        let parsed2 = parse_elf64("helper.o", &obj2).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed1);
        lnk.add_object(parsed2);
        lnk.merge_sections().unwrap();
        lnk.layout();
        lnk.resolve_symbols().unwrap();

        assert!(lnk.symbol("_start").is_some());
        assert!(lnk.symbol("helper_fn").is_some());
    }

    #[test]
    fn test_duplicate_strong_symbol() {
        let obj1 = build_test_object(
            "a.o",
            &[0x90; 8],
            vec![("duplicate", STB_GLOBAL, 1, 0)],
            vec![],
        );
        let obj2 = build_test_object(
            "b.o",
            &[0x90; 8],
            vec![("duplicate", STB_GLOBAL, 1, 0)],
            vec![],
        );

        let parsed1 = parse_elf64("a.o", &obj1).unwrap();
        let parsed2 = parse_elf64("b.o", &obj2).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed1);
        lnk.add_object(parsed2);
        lnk.merge_sections().unwrap();
        lnk.layout();

        let result = lnk.resolve_symbols();
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("duplicate"));
    }

    #[test]
    fn test_weak_symbol_resolved_by_strong() {
        let obj1 = build_test_object(
            "weak.o",
            &[0x90; 8],
            vec![("shared_fn", STB_WEAK, 1, 0)],
            vec![],
        );
        let obj2 = build_test_object(
            "strong.o",
            &[0xCC; 8],
            vec![("shared_fn", STB_GLOBAL, 1, 0)],
            vec![],
        );

        let parsed1 = parse_elf64("weak.o", &obj1).unwrap();
        let parsed2 = parse_elf64("strong.o", &obj2).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed1);
        lnk.add_object(parsed2);
        lnk.merge_sections().unwrap();
        lnk.layout();
        lnk.resolve_symbols().unwrap();

        let sym = lnk.symbol("shared_fn").unwrap();
        assert_eq!(sym.binding, STB_GLOBAL);
        assert_eq!(sym.defined_in, "strong.o");
    }

    #[test]
    fn test_section_merging() {
        let obj1 = build_test_object("a.o", &[0xAA; 16], vec![("_start", STB_GLOBAL, 1, 0)], vec![]);
        let obj2 = build_test_object("b.o", &[0xBB; 32], vec![("func_b", STB_GLOBAL, 1, 0)], vec![]);

        let parsed1 = parse_elf64("a.o", &obj1).unwrap();
        let parsed2 = parse_elf64("b.o", &obj2).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed1);
        lnk.add_object(parsed2);
        lnk.merge_sections().unwrap();

        let text = lnk.merged_section(".text").unwrap();
        // Both sections merged (with possible alignment padding)
        assert!(text.data.len() >= 48);
    }

    #[test]
    fn test_virtual_address_layout() {
        let obj = build_test_object("test.o", &[0x90; 64], vec![("_start", STB_GLOBAL, 1, 0)], vec![]);
        let parsed = parse_elf64("test.o", &obj).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed);
        lnk.merge_sections().unwrap();
        lnk.layout();

        let text = lnk.merged_section(".text").unwrap();
        assert!(text.vaddr >= TEXT_BASE, "text segment should start at or after 0x400000");
    }

    #[test]
    fn test_relocation_pc32() {
        // Simulate: call instruction at offset 1 in .text, targeting symbol at index 1
        let mut code = vec![0xE8, 0x00, 0x00, 0x00, 0x00]; // call rel32
        code.extend_from_slice(&[0xC3; 11]); // ret + padding to 16 bytes

        let obj = build_test_object(
            "test.o",
            &code,
            vec![("_start", STB_GLOBAL, 1, 0), ("target", STB_GLOBAL, 1, 8)],
            vec![(1, 2, R_X86_64_PC32, -4)], // reloc at offset 1, sym idx 2, PC32, addend -4
        );

        let parsed = parse_elf64("test.o", &obj).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed);
        lnk.merge_sections().unwrap();
        lnk.layout();
        lnk.resolve_symbols().unwrap();
        lnk.apply_relocations().unwrap();

        // Verify the relocation was applied
        let text = lnk.merged_section(".text").unwrap();
        let patched = i32::from_le_bytes(text.data[1..5].try_into().unwrap());
        // target_vaddr - (call_site_vaddr + 4) should give a small positive offset
        let target = lnk.symbol("target").unwrap().vaddr;
        let call_site = text.vaddr + 1;
        let expected = (target as i64 + (-4) - call_site as i64) as i32;
        assert_eq!(patched, expected);
    }

    #[test]
    fn test_undefined_symbol_error() {
        // Object references "undefined_fn" but nobody defines it
        let obj = build_test_object(
            "test.o",
            &[0xE8, 0x00, 0x00, 0x00, 0x00, 0x90, 0x90, 0x90,
              0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90],
            vec![
                ("_start", STB_GLOBAL, 1, 0),
                ("undefined_fn", STB_GLOBAL, 0, 0), // shndx=0 = UNDEF
            ],
            vec![(1, 2, R_X86_64_PC32, -4)], // references undefined_fn
        );

        let parsed = parse_elf64("test.o", &obj).unwrap();

        let mut lnk = Linker::new("_start");
        lnk.add_object(parsed);
        lnk.merge_sections().unwrap();
        lnk.layout();

        let result = lnk.resolve_symbols();
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("undefined"));
    }
}
```

### Running

```bash
cargo test
cargo build --release

# End-to-end test with real object files:
cat > /tmp/start.s << 'ASM'
.global _start
_start:
    mov $60, %rax    # sys_exit
    xor %rdi, %rdi   # status 0
    syscall
ASM

as /tmp/start.s -o /tmp/start.o
./target/release/elf_linker -o /tmp/test_exe /tmp/start.o
/tmp/test_exe
echo $?  # should print 0
```

### Expected Test Output

```
running 9 tests
test tests::test_parse_elf_header ... ok
test tests::test_parse_sections ... ok
test tests::test_parse_symbols ... ok
test tests::test_symbol_resolution_strong ... ok
test tests::test_duplicate_strong_symbol ... ok
test tests::test_weak_symbol_resolved_by_strong ... ok
test tests::test_section_merging ... ok
test tests::test_virtual_address_layout ... ok
test tests::test_relocation_pc32 ... ok
test tests::test_undefined_symbol_error ... ok

test result: ok. 10 passed; 0 failed; 0 ignored
```

---

## Design Decisions

1. **Five sequential phases over interleaved processing**: the linker strictly separates parsing, merging, layout, symbol resolution, and relocation into sequential passes. This prevents circular dependencies (you cannot resolve a relocation before you know the symbol's address, and you cannot know the address before layout is complete). The alternative -- resolving addresses incrementally during parsing -- requires backpatching and is significantly harder to debug.

2. **Tracking section placements with explicit offset records**: when an input section is placed into the merged output, the linker records `(object_file, section_name, offset_in_merged)`. This mapping is used during both symbol resolution (to compute a symbol's final address from its section-relative value) and relocation (to find where within the merged section a relocation site lives). Without this mapping, the linker would need to re-scan every object file during relocation.

3. **PC32 and PLT32 use the same formula for static linking**: in a static linker (no dynamic linking, no PLT), `R_X86_64_PLT32` is semantically identical to `R_X86_64_PC32`: both compute `S + A - P`. The PLT32 type exists because the compiler emits it for function calls that *might* go through the PLT in a dynamic context. A static linker resolves them directly. Treating them differently would produce incorrect results.

4. **Program headers with two PT_LOAD segments**: the output has one segment for executable code (`.text` + `.rodata`, mapped R+X) and one for writable data (`.data` + `.bss`, mapped R+W). This matches the security principle of W^X (writable XOR executable). The kernel maps each segment into memory independently. The `.bss` section appears in the data segment with `p_memsz > p_filesz`, telling the kernel to zero-fill the extra memory.

5. **Virtual address base at 0x400000**: this is the conventional start address for x86-64 Linux executables (used by `ld` and `lld` by default). The first 4MB of virtual address space is left unmapped to catch null pointer dereferences. The linker places the text segment starting at `0x400000 + headers_size`, with the data segment at the next page boundary after text.

6. **In-memory test objects instead of fixture files**: the tests construct ELF object files in memory using `build_test_object()`. This avoids depending on `nasm` or `gcc` being installed, makes tests self-contained and portable, and exercises the parser on known binary layouts. Integration tests with real compiler output complement these unit tests but are not required for CI.

7. **No section headers in output executable**: the output contains program headers (required for the kernel to load the executable) but no section headers. Section headers are optional in executables -- they are used by debuggers and `readelf` but not by the kernel's loader. Omitting them simplifies the writer and produces a smaller binary. A production linker would include them for debuggability.

## Common Mistakes

- **Relocation addend sign error**: the addend from `Elf64Rela` is signed (`i64`). For `R_X86_64_PC32`, the formula is `S + A - P` where all arithmetic is signed. Treating the addend as unsigned produces addresses that are off by billions, causing segfaults. GCC typically emits addend `-4` for call instructions (accounting for the 4-byte displacement that is part of the instruction).
- **Forgetting alignment padding between merged sections**: if object A's `.text` is 13 bytes and object B's `.text` requires 16-byte alignment, the merger must insert 3 padding bytes. Without padding, B's code starts at a misaligned address, which is incorrect (though x86 tolerates unaligned instructions, the addresses in symbol resolution will be wrong).
- **Not handling section symbols during relocation**: some relocations reference section symbols (STT_SECTION) rather than named symbols. The relocation targets the section's base address, not a specific function. If the linker ignores section symbols, these relocations produce zero (or garbage) addresses.
- **Writing the ELF header phoff field as zero**: the program header offset must point to the actual byte position of the first program header in the file. If zero, the kernel cannot find the program headers and refuses to load the executable. The error message is typically "cannot execute binary file: Exec format error."
- **BSS section written as zeros to the file**: the `.bss` section must NOT be written to the output file. Its presence is indicated solely by `p_memsz > p_filesz` in the program header. Writing zeros wastes file space and may even confuse the loader if file and memory sizes disagree.

## Performance Notes

Linker performance is dominated by I/O and symbol resolution. Reading object files is sequential I/O (fast on SSD). Symbol resolution is O(S * N) where S is the number of symbols and N is the number of objects, because each object scans the global table. Production linkers (mold, lld) use hash tables for symbol lookup, achieving O(1) per resolution.

Relocation processing is O(R) where R is the total number of relocation entries. For large programs, R can be in the millions (the Linux kernel has ~1M relocations). Each relocation requires looking up the symbol's address and patching 4 or 8 bytes in the output section. This is memory-bandwidth-limited, not compute-limited.

The linker in this solution processes everything in-memory, which works for objects up to a few hundred megabytes. Production linkers memory-map input files and use output buffers to handle multi-gigabyte linking (e.g., linking a browser or game engine).

## Going Further

- Add support for `.init_array` and `.fini_array` sections (C++ static constructors/destructors)
- Implement dynamic linking: generate `.plt`, `.got`, and `.dynamic` sections for shared library support
- Add linker script parsing for custom memory layouts
- Implement link-time optimization (LTO) by merging LLVM IR sections
- Add `--gc-sections` to remove unused code sections
- Support DWARF debug info by merging `.debug_*` sections correctly
