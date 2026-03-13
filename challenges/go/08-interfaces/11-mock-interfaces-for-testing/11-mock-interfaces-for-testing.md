# 11. Mock Interfaces for Testing

<!--
difficulty: advanced
concepts: [mocking, fakes, stubs, test-doubles, interface-testing, mockgen]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [dependency-injection-with-interfaces, testing-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 10 (Dependency Injection)
- Basic familiarity with `testing` package

## The Problem

When your code depends on external systems (databases, APIs, file systems), tests that call those systems are slow, flaky, and hard to set up. Interfaces let you replace real dependencies with test doubles. Go has three main approaches: hand-written fakes, hand-written mocks with call recording, and generated mocks.

Your task: implement each approach for the same interface and understand the trade-offs.

## Hints

<details>
<summary>Hint 1: The interface to mock</summary>

```go
type Notifier interface {
    Send(to string, message string) error
}
```
</details>

<details>
<summary>Hint 2: Hand-written fake</summary>

A fake has working logic but uses simple in-memory data instead of real I/O:

```go
type FakeNotifier struct {
    Sent []struct{ To, Message string }
}

func (f *FakeNotifier) Send(to, message string) error {
    f.Sent = append(f.Sent, struct{ To, Message string }{to, message})
    return nil
}
```
</details>

<details>
<summary>Hint 3: Recording mock with verification</summary>

```go
type MockNotifier struct {
    SendFunc func(to, message string) error
    calls    []struct{ To, Message string }
}

func (m *MockNotifier) Send(to, message string) error {
    m.calls = append(m.calls, struct{ To, Message string }{to, message})
    if m.SendFunc != nil {
        return m.SendFunc(to, message)
    }
    return nil
}

func (m *MockNotifier) SendCallCount() int {
    return len(m.calls)
}

func (m *MockNotifier) SendCalledWith(i int) (string, string) {
    return m.calls[i].To, m.calls[i].Message
}
```
</details>

<details>
<summary>Hint 4: Function field pattern</summary>

The simplest mock uses a function field:

```go
type StubNotifier struct {
    SendFunc func(to, message string) error
}

func (s *StubNotifier) Send(to, message string) error {
    return s.SendFunc(to, message)
}
```

In tests:
```go
notifier := &StubNotifier{
    SendFunc: func(to, message string) error {
        return fmt.Errorf("network error")
    },
}
```
</details>

## Requirements

1. Define a `Notifier` interface with a `Send(to, message string) error` method
2. Write a `NotificationService` that depends on `Notifier` via constructor injection
3. Implement three test doubles:
   - **Fake**: records all sent messages in a slice for later inspection
   - **Stub**: uses a configurable function field to control return values
   - **Mock**: records calls and provides verification methods (`CallCount`, `CalledWith`)
4. Write tests using each approach:
   - Test success path with the fake
   - Test error handling with the stub (make `Send` return an error)
   - Test that the correct arguments were passed using the mock
5. All tests must pass with `go test -v`

## Verification

Run:

```bash
go test -v ./...
```

Your tests should verify:
1. When notification succeeds, the service returns nil
2. When notification fails, the service propagates the error
3. The correct recipient and message were passed to `Send`
4. `Send` was called exactly once per notification request

Check your understanding:
- When would you prefer a fake over a mock?
- What is the disadvantage of generated mocks vs hand-written ones?
- How does the function-field stub pattern trade off against a full mock?

## What's Next

Continue to [12 - Interface Pollution Anti-Patterns](../12-interface-pollution-anti-patterns/12-interface-pollution-anti-patterns.md) to learn what NOT to do with interfaces.

## Reference

- [Go Wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)
- [Mitchell Hashimoto: Advanced Testing with Go (GopherCon 2017)](https://www.youtube.com/watch?v=8hQG7QlcLBk)
- [gomock](https://github.com/uber-go/mock)
- [testify/mock](https://github.com/stretchr/testify)
