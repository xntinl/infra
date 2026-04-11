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
byte 1: [1][bits 0-6]   → high bit set, more bytes follow
byte 2: [0][bits 7-13]  → high bit clear, this is the last byte
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

```bash
mix new wasmex --sup
cd wasmex
mkdir -p lib/wasmex/{parser,runtime}
mkdir -p test/wasmex priv/fixtures bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/wasmex/parser/leb128.ex`

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

### Step 4: `lib/wasmex/runtime/machine.ex`

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

  Trampolining: Elixir is not tail-call optimized for mutual recursion.
  For deeply recursive Wasm programs, a naive recursive interpreter will
  exhaust the Erlang call stack. Use an explicit stack (continuation stack)
  or a trampoline to avoid this.

  This implementation uses an iterative loop with an explicit frame stack —
  the same technique used by production interpreters.
  """

  alias Wasmex.Runtime.Frame

  @doc "Executes a function by name with given arguments. Returns {:ok, [values]} or {:error, trap}."
  @spec call(map(), String.t(), [term()]) :: {:ok, [term()]} | {:error, term()}
  def call(module_instance, function_name, args) do
    with {:ok, func} <- Map.fetch(module_instance.exports, function_name),
         :ok <- validate_args(func, args) do
      initial_frame = Frame.new(func, args)
      execute([initial_frame], [], module_instance)
    end
  end

  # The main execution loop — iterative, not recursive
  defp execute([], stack, _module) do
    # No more frames: return top of stack
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
        # Return from current frame — pop frame and return results to parent
        result_values = Enum.take(stack, func_result_count(frame))
        execute(rest_frames, result_values ++ drop_frame_locals(stack, frame), module)
    end
  end

  defp dispatch({:i32, :const, value}, stack, frame, rest, _module) do
    {:continue, [{:i32, value} | stack], frame, rest}
  end

  defp dispatch({:i32, :add}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    # i32 arithmetic wraps at 2^32
    result = rem(a + b, 0x1_0000_0000)
    {:continue, [{:i32, result} | stack], frame, rest}
  end

  defp dispatch({:i32, :sub}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    # TODO: wrap to i32 range
    {:continue, [{:i32, a - b} | stack], frame, rest}
  end

  defp dispatch({:i32, :mul}, [{:i32, b}, {:i32, a} | stack], frame, rest, _module) do
    # TODO: implement
    {:continue, stack, frame, rest}
  end

  defp dispatch({:local, :get, index}, stack, frame, rest, _module) do
    value = Frame.get_local(frame, index)
    {:continue, [value | stack], frame, rest}
  end

  defp dispatch({:local, :set, index}, [value | stack], frame, rest, _module) do
    new_frame = Frame.set_local(frame, index, value)
    {:continue, stack, new_frame, rest}
  end

  defp dispatch({:call, func_index}, stack, frame, rest, module) do
    # TODO: look up function in module, build new frame, push current frame
    {:continue, stack, frame, rest}
  end

  defp dispatch({:block, _type, instructions}, stack, frame, rest, _module) do
    # TODO: push a block frame for structured control flow
    {:continue, stack, frame, rest}
  end

  defp dispatch({:br, label_depth}, stack, frame, rest, _module) do
    # TODO: branch to label at depth; pop frames accordingly
    {:continue, stack, frame, rest}
  end

  defp dispatch({:return}, stack, frame, rest, _module) do
    # Return from function: pop results and discard locals
    result_count = func_result_count(frame)
    {results, _} = Enum.split(stack, result_count)
    {:return, results, rest}
  end

  defp dispatch(instruction, stack, frame, rest, _module) do
    # TODO: implement remaining ~80 MVP instructions
    # For unimplemented: {:trap, {:unimplemented_instruction, instruction}}
    {:continue, stack, frame, rest}
  end

  defp func_result_count(_frame), do: 1  # TODO: get from frame's function type
  defp drop_frame_locals(stack, _frame), do: stack  # TODO: implement
  defp validate_args(_func, _args), do: :ok  # TODO: implement
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/wasmex/leb128_test.exs
defmodule Wasmex.Parser.LEB128Test do
  use ExUnit.Case, async: true

  alias Wasmex.Parser.LEB128

  test "decodes single-byte unsigned" do
    assert {42, <<>>} = LEB128.decode_unsigned(<<42>>)
  end

  test "decodes multi-byte unsigned" do
    # 300 = 0b100101100 → LEB128: 0b10101100 0b00000010 = <<0xAC, 0x02>>
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
```

```elixir
# test/wasmex/parser_test.exs
defmodule Wasmex.ParserTest do
  use ExUnit.Case, async: true

  alias Wasmex.Parser.Binary

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
```

### Step 6: Run the tests

```bash
# Skip wasm_fixtures tests if you haven't compiled the .wat files yet
mix test test/wasmex/ --exclude wasm_fixtures --trace
```

---

## Trade-off analysis

| Aspect | Interpreting (your impl) | JIT compiling | Native via NIF |
|--------|-------------------------|---------------|----------------|
| Safety | full Elixir sandbox | compile-time escapes | NIF crash = VM crash |
| Execution speed | ~100–1000x slower than native | ~2–10x slower | native speed |
| Startup time | < 1ms per module | JIT warmup overhead | DL load overhead |
| Memory usage | stack as Elixir list | compiled code | minimal |
| Portability | all BEAM platforms | platform-specific | platform-specific |
| Implementation complexity | moderate | very high | high (NIF safety) |

Reflection: your interpreter runs Wasm at ~100x slower than native. For the plugin use case (running user-defined validation logic on each request), is this acceptable? At what point would you consider adding a NIF for hot paths?

---

## Common production mistakes

**1. Forgetting that `i32.add` wraps at 2³²**
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

## Resources

- [WebAssembly Core Specification 1.0](https://webassembly.github.io/spec/core/) — the authoritative reference; the binary format, execution semantics, and validation rules are all here
- [WebAssembly Binary Format](https://webassembly.github.io/spec/core/binary/) — sections 5.1–5.5 cover the exact byte layout you must parse
- [WebAssembly Opcode Table](https://webassembly.github.io/spec/core/binary/instructions.html) — the complete list of opcodes; implement the ~50 most common first
- [LEB128 — Wikipedia](https://en.wikipedia.org/wiki/LEB128) — with worked examples; cross-check your implementation against the examples
- [wat2wasm tool](https://github.com/WebAssembly/wabt) — WebAssembly Binary Toolkit; use this to compile `.wat` text format to `.wasm` binaries for your test fixtures
- ["Programming WebAssembly with Rust"](https://pragprog.com/titles/khrust/programming-webassembly-with-rust/) — Kevin Hoffman — the execution model chapters are language-agnostic and directly applicable
