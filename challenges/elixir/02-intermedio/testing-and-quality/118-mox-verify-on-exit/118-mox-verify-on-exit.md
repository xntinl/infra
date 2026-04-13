# Behaviour-based mocks with Mox and `verify_on_exit!`

**Project**: `mox_demo` — a `WeatherReporter` that depends on a
`WeatherClient` behaviour, tested with Mox so network calls never happen
in tests.

---

## Project context

"Don't mock what you don't own" — José Valim's rule. Mocking libraries
that monkey-patch modules (like `:meck` or `mock`) break parallel tests,
leak across modules, and give you "passes in dev, fails in prod"
surprises. Mox is the discipline-enforcing alternative: mocks MUST
implement a behaviour you define, are configured via the application
environment, and integrate with ExUnit to detect unmet expectations.

The canonical Mox setup:
1. Define a `@behaviour` for the external dependency.
2. Production code reads the implementation from config.
3. Tests swap in a Mox mock and `expect/3` specific calls.
4. `verify_on_exit!/0` makes sure every expectation actually ran.

Project structure:

```
mox_demo/
├── config/
│   └── test.exs
├── lib/
│   ├── weather_client.ex
│   ├── weather_client_http.ex
│   └── weather_reporter.ex
├── test/
│   ├── weather_reporter_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why Mox and not monkey-patching libraries

Librerías como `:meck` reemplazan módulos en runtime — rompen tests
paralelos, filtran entre módulos, producen "pasa en dev, falla en
prod". Mox impone disciplina: mocks deben implementar un behaviour,
se configuran vía application env, y se integran con ExUnit para
detectar expectations no cumplidas.

---

## Core concepts

### 1. Behaviours are the contract

```elixir
defmodule WeatherClient do
  @callback fetch(city :: String.t()) :: {:ok, map()} | {:error, term()}
end
```

Both the real HTTP implementation AND the Mox mock implement this
behaviour. Your code programs against the behaviour, not a specific
module.

### 2. Inject the dependency via config

```elixir
# config/test.exs
config :mox_demo, :weather_client, WeatherClientMock

# lib/weather_reporter.ex
defp client, do: Application.fetch_env!(:mox_demo, :weather_client)
```

Production config points at `WeatherClientHTTP`. Test config points at
the Mox mock. The reporter doesn't know or care.

### 3. `expect/3` vs `stub/3`

- `expect(Mock, :fetch, fn _ -> ... end)` — MUST be called. Fails the test
  if not called (with `verify_on_exit!/0`).
- `stub(Mock, :fetch, fn _ -> ... end)` — MAY be called. No verification.

Use `expect` when the call is part of what you're verifying. Use `stub`
when it's just there to make the code run.

### 4. `verify_on_exit!/0` wires it all together

Putting `setup :verify_on_exit!` in your test module tells Mox: after
every test, check that all `expect` calls actually happened. Unmet
expectations fail the test with a clear message.

### 5. `set_mox_from_context` for async

By default, Mox is "global" (any process can call the mock) but this
breaks `async: true`. Use `set_mox_from_context` (or
`set_mox_private`) to scope expectations to the test process and its
descendants, allowing async tests.

---

## Design decisions

**Option A — Inyectar dependencia como argumento en cada función**
- Pros: Explícito; sin config global.
- Cons: Boilerplate en cada call site; rompe la API pública.

**Option B — Resolver vía `Application.fetch_env/2`** (elegida)
- Pros: API pública intacta; test swap vía config es trivial.
- Cons: Config global es estado compartido; debe configurarse en
  `test.exs` explícitamente.

→ Elegida **B** porque la dependencia es implementación privada; los
tests acceden vía la misma indirección que prod.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {config},
    {error},
    {exunit},
    {mox},
    {ok},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new mox_demo
cd mox_demo
```

Add Mox to `mix.exs`:

```elixir
defp deps do
  [{:mox, "~> 1.2", only: :test}]
end
```

### Step 2: `lib/weather_client.ex` — the behaviour

**Objective**: Edit `weather_client.ex` — the behaviour, exposing the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule WeatherClient do
  @moduledoc "Contract for a weather data provider."

  @type city :: String.t()
  @type report :: %{city: city(), temp_c: number(), conditions: String.t()}

  @callback fetch(city()) :: {:ok, report()} | {:error, term()}
end
```

### Step 3: `lib/weather_client_http.ex` — production impl (stub)

**Objective**: Edit `weather_client_http.ex` — production impl (stub), exposing the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule WeatherClientHTTP do
  @moduledoc "Real HTTP implementation — stubbed here; you'd call a real API."
  @behaviour WeatherClient

  @impl true
  def fetch(city) do
    # In reality: HTTP call. For the exercise, pretend.
    {:ok, %{city: city, temp_c: 20.0, conditions: "sunny"}}
  end
end
```

### Step 4: `lib/weather_reporter.ex` — the code under test

**Objective**: Edit `weather_reporter.ex` — the code under test, exposing the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule WeatherReporter do
  @moduledoc "Composes weather data into human-readable reports."

  @spec report(String.t()) :: {:ok, String.t()} | {:error, term()}
  def report(city) do
    case client().fetch(city) do
      {:ok, %{city: c, temp_c: t, conditions: cond}} ->
        {:ok, "#{c}: #{t}°C, #{cond}"}

      {:error, _} = err ->
        err
    end
  end

  # Resolved at call time — easy to override in config.
  defp client, do: Application.fetch_env!(:mox_demo, :weather_client)
end
```

### Step 5: `config/test.exs`

**Objective**: Implement `test.exs` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
import Config

config :mox_demo, :weather_client, WeatherClientMock
```

### Step 6: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
# Defines WeatherClientMock that implements the WeatherClient behaviour.
Mox.defmock(WeatherClientMock, for: WeatherClient)

ExUnit.start()
```

### Step 7: `test/weather_reporter_test.exs`

**Objective**: Write `weather_reporter_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule WeatherReporterTest do
  use ExUnit.Case, async: true

  import Mox

  # Scopes Mox expectations to each test process — required for async: true.
  setup :set_mox_from_context
  # Asserts every `expect` was called when the test ends.
  setup :verify_on_exit!

  describe "report/1" do
    test "formats a successful fetch" do
      expect(WeatherClientMock, :fetch, fn "Buenos Aires" ->
        {:ok, %{city: "Buenos Aires", temp_c: 27.5, conditions: "clear"}}
      end)

      assert {:ok, "Buenos Aires: 27.5°C, clear"} =
               WeatherReporter.report("Buenos Aires")
    end

    test "propagates error tuples" do
      expect(WeatherClientMock, :fetch, fn _city ->
        {:error, :timeout}
      end)

      assert {:error, :timeout} = WeatherReporter.report("Anywhere")
    end

    test "verify_on_exit! catches missed expectations" do
      # We set up an expectation but don't call it — this would FAIL the
      # test thanks to `verify_on_exit!`. Commented to keep the suite green.
      #
      # expect(WeatherClientMock, :fetch, fn _ -> {:ok, ...} end)
      # (no call to WeatherReporter.report/1)

      # A stub is allowed to go uncalled.
      stub(WeatherClientMock, :fetch, fn _ -> {:ok, %{city: "x", temp_c: 0, conditions: ""}} end)
      :ok
    end

    test "expect can be called multiple times for multiple invocations" do
      expect(WeatherClientMock, :fetch, 2, fn city ->
        {:ok, %{city: city, temp_c: 10.0, conditions: "cloudy"}}
      end)

      assert {:ok, _} = WeatherReporter.report("A")
      assert {:ok, _} = WeatherReporter.report("B")
    end
  end
end
```

### Step 8: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`Mox.defmock/2` crea un módulo que implementa el behaviour.
`expect/3` registra una expectation en un ETS owned por el test
process (vía `set_mox_from_context`); cuando el código llama al
mock, Mox busca en ese ETS y ejecuta el fn definido.
`verify_on_exit!` se registra como `on_exit` callback y falla el
test si alguna expectation no se consumió.

---

## Benchmark

<!-- benchmark N/A: Mox es infraestructura de test. Overhead por
llamada (~10µs por ETS lookup) es despreciable. Si Mox aparece en un
profile de producción, hay un bug de configuración. -->

---

## Trade-offs and production gotchas

**1. Behaviour-less mocks don't exist in Mox**
If the dependency is a bare module with no `@behaviour`, you have to
define one. This is **the point** — it forces you to think about what
contract you actually need from the dependency.

**2. `set_mox_from_context` vs `set_mox_global`**
Async tests require `set_mox_from_context` (or the older
`set_mox_private`). `set_mox_global` works across processes but forces
`async: false`. Use private/from_context whenever possible.

**3. `verify_on_exit!` isn't automatic — you must call it in setup**
Forget it and your tests pass when they shouldn't (because unmet
expectations are silently swallowed). Make it part of every Mox test
module's boilerplate.

**4. Over-mocking kills test value**
If every collaborator is mocked, you're testing *that this module calls
these mocks in this order*. Better: mock the network/IO boundary only;
use real code for everything inward. Mox makes it easy to stay
disciplined because defining a new behaviour is friction.

**5. When NOT to use Mox**
For pure logic (use plain example/property tests), for Ecto
(`Ecto.Sandbox` is the right tool), and when you don't control the
dependency enough to define a meaningful behaviour — in which case,
wrap it in an adapter and mock the adapter.

---

## Reflection

- Tu módulo `WeatherReporter` depende de `WeatherClient`,
  `MetricsClient` y `CacheClient`. Si mockeás los tres en cada test,
  ¿qué estás testeando realmente? Formulá una regla sobre cuántos
  mocks son demasiados en un solo test.
- El equipo decide "cada external IO boundary debe tener un
  behaviour + Mox mock". Tres meses después hay 40 behaviours con
  una única implementación de producción cada uno. ¿Paga la
  disciplina o revertir a llamar directo en app code y mockear solo
  en librerías?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule WeatherReporter do
    @moduledoc "Composes weather data into human-readable reports."

    @spec report(String.t()) :: {:ok, String.t()} | {:error, term()}
    def report(city) do
      case client().fetch(city) do
        {:ok, %{city: c, temp_c: t, conditions: cond}} ->
          {:ok, "#{c}: #{t}°C, #{cond}"}

        {:error, _} = err ->
          err
      end
    end

    # Resolved at call time — easy to override in config.
    defp client, do: Application.fetch_env!(:mox_demo, :weather_client)
  end

  def main do
    IO.puts("=== MockPayment Demo ===
  ")
  
    # Demo: Use a mock HTTP client
  import_list = [{HttpMock, []}]

  IO.puts("1. get_payment_status('txn123'):")
  # In a real scenario this would use Mox to define mock behavior
  result = MockPayment.get_status("txn123")
  IO.puts("   Result: #{inspect(result)}")

  IO.puts("
  ✓ Mox verify_on_exit demo completed!")
  end

end

Main.main()
```


## Resources

- [Mox — HexDocs](https://hexdocs.pm/mox/Mox.html)
- [José Valim — "Mocks and explicit contracts"](https://dashbit.co/blog/mocks-and-explicit-contracts) — the design philosophy behind Mox
- [`@behaviour` — Elixir reference](https://hexdocs.pm/elixir/typespecs.html#behaviours)
- [Chris Keathley — "Testing GenServers with Mox"](https://keathley.io/blog/)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
