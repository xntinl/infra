# WebAssembly Interpreter

**Project**: `wasmex` — a spec-compliant WebAssembly 1.0 interpreter in pure Elixir

---

## Project context

You are building `wasmex`, a WebAssembly interpreter the tooling team will embed in their plugin system. Third-party plugins are compiled to `.wasm` binaries and executed in a sandboxed environment. The interpreter parses the binary format, validates the module, and executes functions using a stack machine. No external Wasm runtimes are allowed — the interpreter runs entirely on the BEAM.

Project structure:

```
wasmex/
├── lib/
│   └── wasmex/
│       ├── application.ex
│       ├── parser/
│       │   ├── binary.ex          # ← .wasm binary format parser
│       │   ├── leb128.ex          # ← LEB128 variable-length integer codec
│       │   └── sections.ex        # ← per-section decoders (Type, Code, Export, ...)
│       ├── validator.ex           # ← static type checking before execution
│       ├── runtime/
│       │   ├── machine.ex         # ← stack machine execution loop
│       │   ├── frame.ex           # ← function activation frame
│       │   ├── memory.ex          # ← linear memory (Agent or ETS-backed)
│       │   └── instructions.ex    # ← instruction dispatch (100+ opcodes)
│       ├── module.ex              # ← instantiated module: exports + call API
│       └── host_functions.ex      # ← import binding (Elixir functions as WASM imports)
├── test/
│   └── wasmex/
│       ├── leb128_test.exs
│       ├── parser_test.exs
│       ├── validator_test.exs
│       ├── instructions_test.exs
│       └── integration_test.exs
├── bench/
│   └── execution_bench.exs
├── priv/
│   └── fixtures/
│       ├── fib.wasm               # ← compile from fib.wat with wat2wasm
│       └── sort.wasm
└── mix.exs
```

---

## Why LEB128 encoding for integers and not fixed 32/64-bit little-endian

LEB128 packs small integers (the common case — indexes, local counts, opcodes) into 1 byte instead of 4; Wasm binaries are dominated by small integers, so LEB128 wins 3-4x on binary size without a meaningful decode cost.

## Design decisions

**Option A — tree-walking interpreter over the AST**
- Pros: straightforward implementation
- Cons: 10-50x slower than necessary, no hope of JIT

**Option B — bytecode stack machine matching the Wasm execution model** (chosen)
- Pros: matches spec semantics directly, simpler validation, room to add a threaded dispatch optimization
- Cons: requires a decode pass

→ Chose **B** because the Wasm spec is defined in terms of a stack machine; implementing it as anything else is a constant translation tax.

## The business problem

The tooling team needs to run untrusted third-party code in their CI/CD pipeline without giving it OS-level access. WebAssembly's linear memory model and explicit import/export system make it an ideal sandbox: the module can only call functions you explicitly provide as imports, and can only access memory within its linear memory bounds. Any out-of-bounds access causes a trap (structured error), not a segfault.

Two invariants make the sandbox safe:

1. **All imports are explicit** — a module cannot call any function you have not provided.
2. **Memory is bounded** — accesses beyond `memory.size * 64KB` return a trap before executing.

---

## Why LEB128 for integer encoding

WebAssembly uses LEB128 (Little Endian Base 128) for all integer values in the binary format. LEB128 uses 7 bits per byte, with the high bit indicating whether more bytes follow. This allows small integers (< 128) to be encoded in 1 byte, while large integers use more bytes — efficient for typical module sizes where most indices are small.

Decoding unsigned LEB128:

```
byte 1: [1][bits 0-6]   -> high bit set, more bytes follow
byte 2: [0][bits 7-13]  -> high bit clear, this is the last byte
result = (bits 7-13) << 7 | (bits 0-6)
```

Signed LEB128 additionally sign-extends the final byte if the sign bit of the 7-bit group is set.

---

## Why the stack machine is the execution model

WebAssembly deliberately chose a stack machine (not a register machine) because:

1. Code is smaller — `i32.add` implicitly pops two stack values and pushes the result; a register machine needs explicit source/destination operands.
2. Validation is simpler — a type-checking pass can simulate the stack statically, verifying that every instruction has the correct operand types before execution.
3. JIT compilation is straightforward — each instruction maps directly to a push/pop/operation sequence.

Your interpreter maintains the stack as an Elixir list (head = top).

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix app with `lib/`, `test/`, and `bench/` carved out up front — every later phase drops into a slot that already exists.


```bash
mix new wasmex --sup
cd wasmex
mkdir -p lib/wasmex/{parser,runtime}
mkdir -p test/wasmex priv/fixtures bench
```

### Step 2: `mix.exs`

**Objective**: Benchee only — a sandbox must run untrusted code, so the runtime surface stays stdlib-only with zero transitive attack surface.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/wasmex/parser/leb128.ex`

**Objective**: Implement LEB128 as a pure binary codec — pattern-match the continuation bit on decode, recurse on the low 7 bits on encode, no parser state needed.


The LEB128 codec handles both unsigned and signed variable-length integer encoding. The decoder uses binary pattern matching on the high bit to determine whether more bytes follow. The encoder recursively emits 7-bit groups with continuation flags.

```elixir
defmodule Wasmex.Parser.LEB128 do
  @moduledoc """
  LEB128 variable-length integer codec.

  Used throughout the WebAssembly binary format for all integer constants,
  counts, indices, and type codes.
  """

  @doc """
  Decodes an unsigned LEB128 integer from a binary.
  Returns {value, remaining_binary} or {:error, :truncated}.
  """
  @spec decode_unsigned(binary()) :: {non_neg_integer(), binary()} | {:error, :truncated}
  def decode_unsigned(binary) do
    decode_unsigned(binary, 0, 0)
  end

  defp decode_unsigned(<<>>, _acc, _shift), do: {:error, :truncated}

  defp decode_unsigned(<<0::1, value::7, rest::binary>>, acc, shift) do
    # High bit is 0: this is the last byte
    {acc ||| (value <<< shift), rest}
  end

  defp decode_unsigned(<<1::1, value::7, rest::binary>>, acc, shift) do
    # High bit is 1: more bytes follow
    decode_unsigned(rest, acc ||| (value <<< shift), shift + 7)
  end

  @doc """
  Decodes a signed LEB128 integer from a binary.
  Returns {value, remaining_binary} or {:error, :truncated}.
  """
  @spec decode_signed(binary()) :: {integer(), binary()} | {:error, :truncated}
  def decode_signed(binary) do
    decode_signed(binary, 0, 0)
  end

  defp decode_signed(<<>>, _acc, _shift), do: {:error, :truncated}

  defp decode_signed(<<0::1, value::7, rest::binary>>, acc, shift) do
    result = acc ||| (value <<< shift)
    # Sign-extend if the sign bit of the 7-bit group is set
    final =
      if (value &&& 0x40) != 0 do
        result ||| -(1 <<< (shift + 7))
      else
        result
      end
    {final, rest}
  end

  defp decode_signed(<<1::1, value::7, rest::binary>>, acc, shift) do
    decode_signed(rest, acc ||| (value <<< shift), shift + 7)
  end

  @doc "Encodes a non-negative integer as unsigned LEB128."
  @spec encode_unsigned(non_neg_integer()) :: binary()
  def encode_unsigned(value) when value < 128, do: <<value>>
  def encode_unsigned(value) do
    <<1::1, (value &&& 0x7F)::7>> <> encode_unsigned(value >>> 7)
  end
end
```

### Step 4: `lib/wasmex/parser/binary.ex`

**Objective**: Validate the Wasm magic and version, then stream sections by ID and LEB128 size so unknown sections skip cleanly instead of aborting the parse.


The binary parser validates the Wasm magic number and version, then delegates section parsing. A minimal valid module contains just the 8-byte header. Each section has a one-byte ID and a LEB128-encoded size, allowing the parser to skip unknown sections gracefully.

```elixir
defmodule Wasmex.Parser.Binary do
  @moduledoc """
  Parses a .wasm binary into a module representation.

  The binary format starts with:
  - Magic number: 0x00 0x61 0x73 0x6D ("\\0asm")
  - Version: 0x01 0x00 0x00 0x00 (version 1)

  Followed by zero or more sections, each with:
  - Section ID (1 byte)
  - Section size (unsigned LEB128)
  - Section contents (size bytes)
  """

  alias Wasmex.Parser.LEB128

  @magic <<0x00, 0x61, 0x73, 0x6D>>
  @version_1 <<0x01, 0x00, 0x00, 0x00>>

  @doc "Parses a wasm binary into a module map."
  @spec parse(binary()) :: {:ok, map()} | {:error, atom()}
  def parse(<<@magic, @version_1, rest::binary>>) do
    sections = parse_sections(rest, %{})
    {:ok, %{
      types: Map.get(sections, 1, []),
      imports: Map.get(sections, 2, []),
      functions: Map.get(sections, 3, []),
      tables: Map.get(sections, 4, []),
      memory: Map.get(sections, 5, []),
      globals: Map.get(sections, 6, []),
      exports: Map.get(sections, 7, []),
      start: Map.get(sections, 8, nil),
      elements: Map.get(sections, 9, []),
      code: Map.get(sections, 10, []),
      data: Map.get(sections, 11, [])
    }}
  end

  def parse(<<@magic, _version::binary-size(4), _rest::binary>>), do: {:error, :unsupported_version}
  def parse(<<_other::binary-size(4), _rest::binary>>), do: {:error, :invalid_magic}
  def parse(_), do: {:error, :invalid_magic}

  defp parse_sections(<<>>, acc), do: acc

  defp parse_sections(<<section_id::8, rest::binary>>, acc) do
    case LEB128.decode_unsigned(rest) do
      {size, after_size} ->
        <<section_data::binary-size(size), remaining::binary>> = after_size
        new_acc = Map.put(acc, section_id, section_data)
        parse_sections(remaining, new_acc)

      {:error, _} ->
        acc
    end
  end

  defp parse_sections(_, acc), do: acc
end
```

### Step 5: `lib/wasmex/runtime/frame.ex`

**Objective**: Model an activation record as an immutable struct holding locals, instruction pointer, and signature — one value per call, replaced on mutation.


An activation frame represents a function call in progress. It holds the function's local variables, the instruction pointer (index into the instruction list), and the function's type signature.

```elixir
defmodule Wasmex.Runtime.Frame do
  @moduledoc """
  Function activation frame for the stack machine.

  Each frame contains:
  - locals: list of local variable values (params + declared locals)
  - instructions: the function's instruction list
  - pc: current program counter (index into instructions)
  - func_type: the function's type signature for result count
  """

  defstruct [:locals, :instructions, :pc, :func_type]

  @doc "Creates a new frame for a function call with given arguments."
  @spec new(map(), [term()]) :: t()
  def new(func, args) do
    # Initialize locals: arguments first, then zero-initialized declared locals
    declared_locals = List.duplicate({:i32, 0}, Map.get(func, :local_count, 0))

    %__MODULE__{
      locals: args ++ declared_locals,
      instructions: func.body,
      pc: 0,
      func_type: func.type
    }
  end

  @doc "Returns the next instruction and advances the PC."
  @spec next_instruction(t()) :: {:ok, term(), t()} | :end_of_function
  def next_instruction(%__MODULE__{pc: pc, instructions: instructions} = frame) do
    if pc < length(instructions) do
      instruction = Enum.at(instructions, pc)
      {:ok, instruction, %{frame | pc: pc + 1}}
    else
      :end_of_function
    end
  end

  @doc "Gets a local variable by index."
  @spec get_local(t(), non_neg_integer()) :: term()
  def get_local(%__MODULE__{locals: locals}, index) do
    Enum.at(locals, index)
  end

  @doc "Sets a local variable by index."
  @spec set_local(t(), non_neg_integer(), term()) :: t()
  def set_local(%__MODULE__{locals: locals} = frame, index, value) do
    %{frame | locals: List.replace_at(locals, index, value)}
  end
end
```

### Step 6: `lib/wasmex/runtime/machine.ex`

**Objective**: Drive execution with an explicit frame stack — never recurse on the BEAM — and dispatch each opcode with wrapping integer ops at the declared bit width.


The stack machine executes Wasm instructions using an explicit frame stack (not Elixir call stack recursion). Each instruction dispatches on its opcode, manipulating the value stack and control flow. Integer arithmetic wraps at the appropriate bit width using Bitwise operations.

```elixir
defmodule Wasmex.Runtime.Machine do
  @moduledoc """
  Stack machine execution loop.

  The machine maintains:
  - stack: Elixir list of {:i32, integer()} | {:i64, integer()} | {:f32, float()} | {:f64, float()}
  - frames: list of %Frame{} structs (call stack)
  - memory: reference to the linear memory Agent/ETS

  Execution model:
  1. Pop the current frame's program counter (index into instructions list)
  2. Dispatch on the instruction opcode
  3. Update stack, locals, memory, and PC
  4. Recurse until :return or the instruction list is exhausted

  This implementation uses an iterative loop with an explicit frame stack --
  the same technique used by production interpreters.
  """

  alias Wasmex.Runtime.Frame

  @i32_max 0xFFFFFFFF

  @doc "Executes a function by name with given arguments. Returns {:ok, [values]} or {:error, trap}."
  @spec call(map(), String.t(), [term()]) :: {:ok, [term()]} | {:error, term()}
  def call(module_instance, function_name, args) do
    with {:ok, func} <- Map.fetch(module_instance.exports, function_name),
         :ok <- validate_args(func, args) do
      initial_frame = Frame.new(func, args)
      execute([initial_frame], [], module_instance)
    end
  end

  # The main execution loop -- iterative, not recursive
  defp execute([], stack, _module) do
    {:ok, stack}
  end

  defp execute([frame | rest_frames], stack, module) do
    case Frame.next_instruction(frame) do
      {:ok, instruction, next_frame} ->
        case dispatch(instruction, stack, next_frame, rest_frames, module) do
          {:continue, new_stack, new_frame, new_rest} ->
            execute([new_frame | new_rest], new_stack, module)

          {:return, result_stack, parent_frames} ->
            execute(parent_frames, result_stack, module)

          {:trap, reason} ->
            {:error, {:trap, reason}}
        end

      :end_of_function ->
        result_count = func_result_count(frame)
        {results, remaining} = Enum.split(stack, result_count)
        execute(rest_frames, results ++ remaining, module)
    end
  end

  # -- Numeric constants --

  defp dispatch({:i32, :const, value}, stack, frame, rest, _module) do
    {:continue, [{:i32, value} | stack], frame, rest}
  end

  defp dispatch({:i64, :const, value}, stack, frame, rest, _module) do
    {:continue, [{:i64, value} | stack], frame, rest}
  end

  # -- i32 arithmetic (all operations wrap at 2^32) --

  defp dispatch({:i32, :add}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = (a + b) &&& @i32_max
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :sub}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = (a - b) &&& @i32_max
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :mul}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = (a * b) &&& @i32_max
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :div_s}, [{:i32, 0}, _ | _stack], _frame, _rest, _module) do
    {:trap, :integer_divide_by_zero}
  end

  defp dispatch({:i32, :div_s}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = div(to_signed32(a), to_signed32(b)) &&& @i32_max
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :lt_s}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = if to_signed32(a) < to_signed32(b), do: 1, else: 0
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :gt_s}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = if to_signed32(a) > to_signed32(b), do: 1, else: 0
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :eq}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    result = if a == b, do: 1, else: 0
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :eqz}, [{:i32, a} | stack], frame, rest, _module) do
    result = if a == 0, do: 1, else: 0
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  # -- Local variable access --

  defp dispatch({:local, :get, index}, stack, frame, rest, _module) do
    value = Frame.get_local(frame, index)
    {:continue, [value | stack], frame, rest}
  end

  defp dispatch({:local, :set, index}, [value | stack], frame, rest, _module) do
    new_frame = Frame.set_local(frame, index, value)
    {:continue, stack, new_frame, rest}
  end

  defp dispatch({:local, :tee, index}, [value | _] = stack, frame, rest, _module) do
    new_frame = Frame.set_local(frame, index, value)
    {:continue, stack, new_frame, rest}
  end

  # -- Function calls --

  defp dispatch({:call, func_index}, stack, frame, rest, module) do
    func = Enum.at(module.functions, func_index)
    param_count = length(func.type.params)
    {args, remaining_stack} = Enum.split(stack, param_count)
    new_frame = Frame.new(func, Enum.reverse(args))
    {:continue, remaining_stack, new_frame, [frame | rest]}
  end

  # -- Control flow --

  defp dispatch({:if, _type, then_body, else_body}, [{:i32, condition} | stack], frame, rest, _module) do
    body = if condition != 0, do: then_body, else: (else_body || [])
    # Inject the chosen body's instructions at the current PC position
    remaining_instructions = Enum.drop(frame.instructions, frame.pc)
    new_instructions = Enum.take(frame.instructions, frame.pc - 1) ++ body ++ remaining_instructions
    new_frame = %{frame | instructions: new_instructions, pc: frame.pc - 1 + 0}
    # Simpler: just prepend the body instructions
    body_frame = %{frame | instructions: body ++ Enum.drop(frame.instructions, frame.pc), pc: 0}
    {:continue, stack, body_frame, rest}
  end

  defp dispatch({:block, _type, instructions}, stack, frame, rest, _module) do
    block_frame = %{frame | instructions: instructions ++ Enum.drop(frame.instructions, frame.pc), pc: 0}
    {:continue, stack, block_frame, rest}
  end

  defp dispatch({:loop, _type, instructions}, stack, frame, rest, _module) do
    loop_frame = %{frame | instructions: instructions, pc: 0}
    {:continue, stack, loop_frame, rest}
  end

  defp dispatch({:br, 0}, stack, frame, rest, _module) do
    # Branch to the end of the current block (skip remaining instructions)
    {:continue, stack, %{frame | pc: length(frame.instructions)}, rest}
  end

  defp dispatch({:br, label_depth}, stack, _frame, rest, _module) when label_depth > 0 do
    # Pop frames until reaching the target label depth
    {_popped, remaining} = Enum.split(rest, label_depth - 1)
    case remaining do
      [target_frame | outer] ->
        {:continue, stack, %{target_frame | pc: length(target_frame.instructions)}, outer}
      [] ->
        {:trap, :invalid_branch_depth}
    end
  end

  defp dispatch({:br_if, label_depth}, [{:i32, condition} | stack], frame, rest, module) do
    if condition != 0 do
      dispatch({:br, label_depth}, stack, frame, rest, module)
    else
      {:continue, stack, frame, rest}
    end
  end

  defp dispatch({:return}, stack, frame, rest, _module) do
    result_count = func_result_count(frame)
    {results, _} = Enum.split(stack, result_count)
    {:return, results, rest}
  end

  defp dispatch(:nop, stack, frame, rest, _module) do
    {:continue, stack, frame, rest}
  end

  defp dispatch(:unreachable, _stack, _frame, _rest, _module) do
    {:trap, :unreachable}
  end

  # -- Drop and Select --

  defp dispatch(:drop, [_ | stack], frame, rest, _module) do
    {:continue, stack, frame, rest}
  end

  defp dispatch(:select, [{:i32, condition}, val2, val1 | stack], frame, rest, _module) do
    result = if condition != 0, do: val1, else: val2
    {:continue, [result | stack], frame, rest}
  end

  # -- Catch-all for unimplemented instructions --

  defp dispatch(instruction, _stack, _frame, _rest, _module) do
    {:trap, {:unimplemented_instruction, instruction}}
  end

  # -- Helpers --

  defp func_result_count(frame) do
    case frame.func_type do
      %{results: results} -> length(results)
      _ -> 1
    end
  end

  defp validate_args(func, args) do
    expected = length(func.type.params)
    if length(args) == expected, do: :ok, else: {:error, :argument_count_mismatch}
  end

  # Convert unsigned 32-bit to signed 32-bit for signed operations
  defp to_signed32(n) when n >= 0x80000000, do: n - 0x100000000
  defp to_signed32(n), do: n
end
```

### Step 7: `lib/wasmex/module.ex`

**Objective**: Represent an instantiated module as a struct of resolved exports and function bodies — one lookup, zero reparsing at call time.


The module struct represents an instantiated Wasm module with resolved exports and function bodies ready for execution.

```elixir
defmodule Wasmex.Module do
  @moduledoc """
  Represents an instantiated WebAssembly module.

  Instantiation resolves imports, initializes memory, and builds
  the export map that the Machine uses for function lookup.
  """

  defstruct [:exports, :functions, :memory, :tables, :globals]

  @doc "Instantiates a parsed module with the given import map."
  @spec instantiate(map(), map()) :: {:ok, t()} | {:error, term()}
  def instantiate(parsed_module, _imports) do
    {:ok, %__MODULE__{
      exports: Map.get(parsed_module, :exports, %{}),
      functions: Map.get(parsed_module, :functions, []),
      memory: nil,
      tables: [],
      globals: []
    }}
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Pin the public contract with a frozen suite — if the interpreter drifts, these tests are the single source of truth that call it out.


```elixir
# test/wasmex/leb128_test.exs
defmodule Wasmex.Parser.LEB128Test do
  use ExUnit.Case, async: true

  alias Wasmex.Parser.LEB128


  describe "LEB128" do

  test "decodes single-byte unsigned" do
    assert {42, <<>>} = LEB128.decode_unsigned(<<42>>)
  end

  test "decodes multi-byte unsigned" do
    # 300 = 0b100101100 -> LEB128: 0b10101100 0b00000010 = <<0xAC, 0x02>>
    assert {300, <<>>} = LEB128.decode_unsigned(<<0xAC, 0x02>>)
  end

  test "decodes signed negative" do
    # -1 in signed LEB128 is 0x7F
    assert {-1, <<>>} = LEB128.decode_signed(<<0x7F>>)
  end

  test "decodes signed positive" do
    assert {42, <<>>} = LEB128.decode_signed(<<42>>)
  end

  test "round-trip unsigned" do
    for n <- [0, 1, 127, 128, 255, 1000, 65535] do
      encoded = LEB128.encode_unsigned(n)
      assert {^n, <<>>} = LEB128.decode_unsigned(encoded)
    end
  end

  test "returns error on truncated input" do
    # Multi-byte value with only first byte present
    assert {:error, :truncated} = LEB128.decode_unsigned(<<0x80>>)
  end


  end
end
```

```elixir
# test/wasmex/parser_test.exs
defmodule Wasmex.ParserTest do
  use ExUnit.Case, async: true

  alias Wasmex.Parser.Binary


  describe "Parser" do

  test "parses wasm magic and version" do
    # Minimal valid wasm module: magic + version + empty
    wasm = <<0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00>>
    assert {:ok, _module} = Binary.parse(wasm)
  end

  test "rejects invalid magic number" do
    wasm = <<0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00>>
    assert {:error, :invalid_magic} = Binary.parse(wasm)
  end

  test "rejects unsupported version" do
    wasm = <<0x00, 0x61, 0x73, 0x6D, 0x02, 0x00, 0x00, 0x00>>
    assert {:error, :unsupported_version} = Binary.parse(wasm)
  end


  end
end
```

```elixir
# test/wasmex/integration_test.exs
defmodule Wasmex.IntegrationTest do
  use ExUnit.Case, async: true

  @fib_wasm_path Path.join(__DIR__, "../priv/fixtures/fib.wasm")

  # Run: wat2wasm fib.wat -o priv/fixtures/fib.wasm
  # fib.wat:
  # (module
  #   (func $fib (export "fib") (param i32) (result i32)
  #     (if (result i32) (i32.lt_s (local.get 0) (i32.const 2))
  #       (then (local.get 0))
  #       (else
  #         (i32.add
  #           (call $fib (i32.sub (local.get 0) (i32.const 1)))
  #           (call $fib (i32.sub (local.get 0) (i32.const 2))))))))

  @tag :wasm_fixtures
  describe "Integration" do

  test "executes fibonacci(10) = 55" do
    wasm = File.read!(@fib_wasm_path)
    {:ok, module} = Wasmex.Parser.Binary.parse(wasm)
    {:ok, instance} = Wasmex.Module.instantiate(module, %{})
    assert {:ok, [{:i32, 55}]} = Wasmex.Runtime.Machine.call(instance, "fib", [{:i32, 10}])
  end

  @tag :wasm_fixtures
  test "executes fibonacci(0) = 0" do
    wasm = File.read!(@fib_wasm_path)
    {:ok, module} = Wasmex.Parser.Binary.parse(wasm)
    {:ok, instance} = Wasmex.Module.instantiate(module, %{})
    assert {:ok, [{:i32, 0}]} = Wasmex.Runtime.Machine.call(instance, "fib", [{:i32, 0}])
  end

  end
end
```

### Step 9: Run the tests

**Objective**: Run the suite end-to-end with `--trace` so failures name the exact layer — parser, frame, machine, or module — without guesswork.


```bash
# Skip wasm_fixtures tests if you haven't compiled the .wat files yet
mix test test/wasmex/ --exclude wasm_fixtures --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the WebAssembly interpreter invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark workload

El benchmark mide:
1. **LEB128 decoding**: parsing overhead (codec is on the hot path).
2. **Machine instruction dispatch**: evaluación de cada instrucción.
3. **Recursive function calls**: frame stack overhead vs. BEAM call stack.

```elixir
# bench/wasmex_bench.exs
alias Wasmex.Parser.{Binary, LEB128}
alias Wasmex.Runtime.Machine
alias Wasmex.Module

# Pre-load test fixtures
{:ok, fib_wasm} = File.read(Path.join(__DIR__, "../priv/fixtures/fib.wasm"))
{:ok, fib_module} = Binary.parse(fib_wasm)
{:ok, fib_instance} = Module.instantiate(fib_module, %{})

# LEB128 codec performance
leb_unsigned_1byte = <<42>>
leb_unsigned_2byte = <<0xAC, 0x02>>
leb_unsigned_5byte = <<0xFF, 0xFF, 0xFF, 0xFF, 0x0F>>

defp call_fib(instance, n) do
  {:ok, [{:i32, _result}]} = Machine.call(instance, "fib", [{:i32, n}])
end

Benchee.run(
  %{
    "LEB128 decode unsigned (1 byte)" => fn -> LEB128.decode_unsigned(leb_unsigned_1byte) end,
    "LEB128 decode unsigned (2 bytes)" => fn -> LEB128.decode_unsigned(leb_unsigned_2byte) end,
    "LEB128 decode unsigned (5 bytes)" => fn -> LEB128.decode_unsigned(leb_unsigned_5byte) end,
    "LEB128 encode unsigned (128)" => fn -> LEB128.encode_unsigned(128) end,
    "fib(10) recursive calls, 177 function invocations" => fn -> call_fib(fib_instance, 10) end,
    "fib(15) recursive calls, 1973 function invocations" => fn -> call_fib(fib_instance, 15) end,
    "fib(20) recursive calls, 21891 function invocations" => fn -> call_fib(fib_instance, 20) end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

**Key metrics**:
- LEB128 1-byte: < 1 µs (must be sub-microsecond)
- LEB128 multi-byte: < 5 µs (parsing overhead acceptable)
- fib(10): < 1 ms (frame overhead acceptable for plugin use case)
- fib(20): < 100 ms (tree recursion is expensive, but predictable)

**Acceptable slowdown**: 30-100x vs. native V8 (tree-walking interpreter vs. JIT). At this point, the interpreter is suitable for validation logic but not for compute-heavy plugins.

## Deep Dive: NIF Callbacks and BEAM Scheduling Implications

Native Implemented Functions (NIFs) are C/Rust code called from Elixir. They are fast (no VM overhead) but dangerous: a blocking NIF blocks the entire BEAM scheduler thread, starving all other processes.

**The BEAM scheduler model**: One OS thread per logical scheduler (typically one per CPU core). When a process calls a NIF, the thread executes C code. If the NIF blocks (e.g., calling `read()` on a socket), the thread is blocked, and all 100+ other processes on that scheduler are frozen.

**Problem**: A Rustler NIF that calls `std::fs::File::open()` or `std::net::TcpStream::connect()` may block. Meanwhile, unrelated Erlang processes starve.

**Solutions**:
1. **Dirty schedulers**: Mark the NIF as `:dirty_io` or `:dirty_cpu`. The BEAM reserves separate threads for blocking work. The calling process is moved off the main scheduler; others continue. Trade-off: dirty threads are a limited resource; over-subscribe and throughput drops.
2. **Async via thread pool**: Spawn a C thread from a thread pool, return immediately, and notify Erlang via callback. Complex but non-blocking.
3. **Never block in NIFs**: Only call non-blocking C functions (pure computation, hash functions). Delegate I/O to Erlang processes.

Rustler's `:dirty_io` attribute automates dirty scheduler mapping. For a Rust function that calls a blocking OS API:

```rust
pub fn expensive_operation(a: u32) -> u32 {
    // Blocking operations here, will run on dirty_io scheduler
    std::thread::sleep(Duration::from_secs(1));
    a + 1
}
#[rustler::nif(schedule = "DirtyIo")]
pub fn expensive_operation_nif(a: u32) -> u32 { expensive_operation(a) }
```

**Production pattern**: Reserve dirty schedulers for truly blocking I/O. Measure scheduler utilization to confirm no starvation under load. Prefer Elixir async processes for I/O when possible; they are more observable and composable.

---

## Key Concepts: LEB128 Encoding and Stack Machine Semantics

**LEB128 (Little Endian Base 128)**: Codificación variable-length que usa 7 bits de datos por byte, con el bit alto indicando si hay más bytes. Ejemplo: el número 300 (0x12C = 0b100101100):

```
300 en binario: 0b100101100 (9 bits)
Dividir en grupos de 7 bits desde el LSB:
  grupo 1: 0b0101100 (44 = 0x2C)
  grupo 2: 0b0000010 (2 = 0x02)

LEB128 encoding:
  byte 1: 1 (continuation) | 0b0101100 = 0xAC
  byte 2: 0 (final)        | 0b0000010 = 0x02
  resultado: 0xAC 0x02

Ventaja: números < 128 ocupan 1 byte (muy común en Wasm). Números grandes usan 2-5 bytes.
```

Para signed LEB128, el sign-extend se aplica si el MSB del último grupo está establecido:

```
-1 en signed LEB128:
  -1 = 0xFFFFFFFF en 32-bit, pero en LEB128 se codifica compactamente:
  7 bits de -1 es 0b1111111 = 0x7F (con MSB set)
  → sign-extend → -1
```

**Stack machine execution model**: La máquina Wasm mantiene una pila de valores y un stack de frames de activación. Cada instrucción:

1. **Pop** los argumentos de la pila
2. **Compute** el resultado
3. **Push** el resultado de vuelta

Ejemplo: `i32.add`:

```
Stack antes: [a, b, ...]
Operación: pop b, pop a, push (a + b) & 0xFFFFFFFF
Stack después: [(a+b) & 0xFFFFFFFF, ...]
```

Las instrucciones de control (`if`, `block`, `loop`, `br`) manipulan el frame stack, no la pila de valores. Esto permite:

- **Bloques estructurados**: nested control flow con un exit point explícito.
- **Branches etiquetadas**: `br N` salta al label N (0 = bloque más interno, 1 = uno arriba, etc).

**Frame stack vs. value stack**:

```
Frame stack (control flow):
  [Frame3 (local vars), Frame2 (local vars), Frame1 (local vars)]
     ↓
     └── instructions para Frame1

Value stack (data):
  [v0, v1, v2, ...] ← top (se manipula por instrucciones)
```

Separar frames y valores permite que un `call` pushee un nuevo frame sin afectar los datos que están siendo computados.

**Type system invariants (pre-execution validation)**:

1. **All values are explicitly typed**: `:i32`, `:i64`, `:f32`, `:f64`. No hay conversiones implícitas.
2. **Stack must be balanced at block boundaries**: El validator comprueba que antes de exit de un bloque, la pila tiene exactamente el número de resultados declarados.
3. **Locals are typed and bounds-checked**: Acceso a un local fuera de rango es una trap.

---

## Trade-off analysis

| Aspect | Interpreting (your impl) | JIT compiling | Native via NIF |
|--------|-------------------------|---------------|----------------|
| Safety | full Elixir sandbox | compile-time escapes | NIF crash = VM crash |
| Execution speed | ~100-1000x slower than native | ~2-10x slower | native speed |
| Startup time | < 1ms per module | JIT warmup overhead | DL load overhead |
| Memory usage | stack as Elixir list | compiled code | minimal |
| Portability | all BEAM platforms | platform-specific | platform-specific |
| Implementation complexity | moderate | very high | high (NIF safety) |

Reflection: your interpreter runs Wasm at ~100x slower than native. For the plugin use case (running user-defined validation logic on each request), is this acceptable? At what point would you consider adding a NIF for hot paths?

---

## Common production mistakes

**1. Forgetting that `i32.add` wraps at 2^32**
WebAssembly integers use two's complement with wrap-around. `i32.add(2147483647, 1)` returns `-2147483648`, not an error. If your interpreter uses Elixir integers (arbitrary precision), you must explicitly wrap arithmetic to 32-bit or 64-bit range.

**2. Incorrect `br` (branch) semantics for loops vs. blocks**
`br N` in a `block` exits the block (breaks out). `br N` in a `loop` jumps to the top of the loop (continues). The depth N counts from the innermost enclosing block/loop/function. This asymmetry is intentional and subtle — read the spec carefully before implementing control flow.

**3. LEB128 decoding accepting too-long encodings**
A valid i32 value fits in at most 5 bytes of LEB128 (ceil(32/7) = 5). A malicious module might encode a small integer with unnecessary continuation bytes. The decoder must reject encodings longer than the maximum for the declared type.

**4. Memory.grow not checking the max page limit**
`memory.grow` must check against the module's declared maximum memory size (if present). Growing beyond this limit must return -1 (the WASM convention for failure), not a trap.

**5. Not handling `unreachable` instruction as a trap**
The `unreachable` instruction is not "undefined behavior" — it is a guaranteed trap. A module may use it to mark paths the developer believes are impossible. If reached at runtime, your interpreter must trap with `:unreachable`, not skip or panic.

---

## Reflection

Tree-walking your Wasm interpreter is probably 30x slower than V8's JIT. At what use case does that 30x gap stop mattering — plugin sandboxes, edge computing, or client validation? Pick one and justify.

## Resources

- [WebAssembly Core Specification 1.0](https://webassembly.github.io/spec/core/) — the authoritative reference; the binary format, execution semantics, and validation rules are all here
- [WebAssembly Binary Format](https://webassembly.github.io/spec/core/binary/) — sections 5.1-5.5 cover the exact byte layout you must parse
- [WebAssembly Opcode Table](https://webassembly.github.io/spec/core/binary/instructions.html) — the complete list of opcodes; implement the ~50 most common first
- [LEB128 — Wikipedia](https://en.wikipedia.org/wiki/LEB128) — with worked examples; cross-check your implementation against the examples
- [wat2wasm tool](https://github.com/WebAssembly/wabt) — WebAssembly Binary Toolkit; use this to compile `.wat` text format to `.wasm` binaries for your test fixtures
- ["Programming WebAssembly with Rust"](https://pragprog.com/titles/khrust/programming-webassembly-with-rust/) — Kevin Hoffman — the execution model chapters are language-agnostic and directly applicable
