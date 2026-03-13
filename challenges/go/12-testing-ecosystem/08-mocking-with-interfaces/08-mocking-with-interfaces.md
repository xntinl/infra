<!-- difficulty: intermediate -->
<!-- concepts: interface-based mocks, test doubles -->
<!-- tools: go test -->
<!-- estimated_time: 30m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 04-subtests-and-t-run -->

# Mocking with Interfaces

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Interfaces and implicit implementation
- Struct embedding
- Error handling patterns

## Learning Objectives

By the end of this exercise, you will be able to:
1. Design code that accepts interfaces for testability
2. Create mock implementations of interfaces
3. Write tests that verify behavior using test doubles
4. Distinguish between mocks, stubs, and fakes

## Why This Matters

Production code depends on databases, APIs, file systems, and other external services. You cannot (and should not) call these in unit tests. Go's interface system provides a clean solution: define behavior as an interface, accept interfaces in your functions, and substitute mock implementations in tests. No framework needed -- just define a struct that implements the interface.

## Instructions

You will build an order processing service and test it by mocking its dependencies.

### Scaffold

```bash
mkdir -p orderservice && cd orderservice
go mod init orderservice
```

`orderservice.go`:

```go
package orderservice

import (
	"fmt"
	"time"
)

// Order represents a customer order.
type Order struct {
	ID        string
	Customer  string
	Amount    float64
	Status    string
	CreatedAt time.Time
}

// OrderStore defines the interface for order persistence.
type OrderStore interface {
	Save(order *Order) error
	FindByID(id string) (*Order, error)
}

// Notifier defines the interface for sending notifications.
type Notifier interface {
	SendConfirmation(customerEmail string, orderID string) error
}

// OrderService processes orders using injected dependencies.
type OrderService struct {
	store    OrderStore
	notifier Notifier
}

// NewOrderService creates a new OrderService.
func NewOrderService(store OrderStore, notifier Notifier) *OrderService {
	return &OrderService{store: store, notifier: notifier}
}

// PlaceOrder creates and saves a new order, then sends a confirmation.
func (s *OrderService) PlaceOrder(customer string, amount float64) (*Order, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive, got %.2f", amount)
	}

	order := &Order{
		ID:        fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
		Customer:  customer,
		Amount:    amount,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := s.store.Save(order); err != nil {
		return nil, fmt.Errorf("saving order: %w", err)
	}

	if err := s.notifier.SendConfirmation(customer, order.ID); err != nil {
		// Log but don't fail the order
		order.Status = "confirmed_no_notification"
	} else {
		order.Status = "confirmed"
	}

	return order, nil
}

// GetOrder retrieves an order by ID.
func (s *OrderService) GetOrder(id string) (*Order, error) {
	order, err := s.store.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("finding order: %w", err)
	}
	return order, nil
}
```

### Your Task

Create `orderservice_test.go` with:

**1. A mock OrderStore**:

```go
package orderservice

import (
	"fmt"
	"testing"
)

// mockStore is a test double for OrderStore.
type mockStore struct {
	orders   map[string]*Order
	saveErr  error  // inject error for testing failure path
}

func newMockStore() *mockStore {
	return &mockStore{orders: make(map[string]*Order)}
}

func (m *mockStore) Save(order *Order) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.orders[order.ID] = order
	return nil
}

func (m *mockStore) FindByID(id string) (*Order, error) {
	order, ok := m.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %s not found", id)
	}
	return order, nil
}
```

**2. A mock Notifier**:

```go
type mockNotifier struct {
	calls   []string // records which customers were notified
	sendErr error
}

func (m *mockNotifier) SendConfirmation(customer string, orderID string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.calls = append(m.calls, customer)
	return nil
}
```

**3. Tests for the happy path, store failure, and notification failure**:

```go
func TestPlaceOrder_Success(t *testing.T) {
	store := newMockStore()
	notifier := &mockNotifier{}
	svc := NewOrderService(store, notifier)

	order, err := svc.PlaceOrder("alice@example.com", 99.99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != "confirmed" {
		t.Errorf("status = %q, want %q", order.Status, "confirmed")
	}
	if order.Amount != 99.99 {
		t.Errorf("amount = %.2f, want 99.99", order.Amount)
	}
	if len(notifier.calls) != 1 || notifier.calls[0] != "alice@example.com" {
		t.Errorf("notifier calls = %v, want [alice@example.com]", notifier.calls)
	}
	// Verify the order was saved
	saved, err := store.FindByID(order.ID)
	if err != nil {
		t.Fatalf("order not found in store: %v", err)
	}
	if saved.Customer != "alice@example.com" {
		t.Errorf("saved customer = %q, want alice@example.com", saved.Customer)
	}
}

func TestPlaceOrder_InvalidAmount(t *testing.T) {
	svc := NewOrderService(newMockStore(), &mockNotifier{})

	_, err := svc.PlaceOrder("bob@example.com", -10)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestPlaceOrder_StoreError(t *testing.T) {
	store := newMockStore()
	store.saveErr = fmt.Errorf("database connection lost")
	svc := NewOrderService(store, &mockNotifier{})

	_, err := svc.PlaceOrder("carol@example.com", 50.00)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
}

func TestPlaceOrder_NotificationFailure(t *testing.T) {
	store := newMockStore()
	notifier := &mockNotifier{sendErr: fmt.Errorf("email service down")}
	svc := NewOrderService(store, notifier)

	order, err := svc.PlaceOrder("dave@example.com", 25.00)
	if err != nil {
		t.Fatalf("order should succeed even if notification fails: %v", err)
	}
	if order.Status != "confirmed_no_notification" {
		t.Errorf("status = %q, want %q", order.Status, "confirmed_no_notification")
	}
}
```

### Verification

```bash
go test -v
```

All four tests should pass. The mock store and notifier isolate the service logic from any real infrastructure.

## Common Mistakes

1. **Mocking what you don't own**: Prefer interfaces you define. Wrap third-party types behind your own interface rather than mocking their concrete types.

2. **Over-mocking**: If you mock everything, you test nothing. Mock external boundaries (DB, HTTP, filesystem), not internal logic.

3. **Not verifying mock interactions**: A mock that just returns values is a stub. If you need to verify a method was called with specific arguments, record the calls like `mockNotifier.calls` does.

4. **Mocks that are too smart**: Keep mocks simple. A mock that reimplements business logic defeats the purpose of testing.

## Verify What You Learned

1. What is the difference between a mock, a stub, and a fake?
2. Why does Go not need a mocking framework for simple cases?
3. How do you test error paths using mocks?
4. What should you mock and what should you not?

## What's Next

The next exercise covers **httptest** -- Go's built-in package for testing HTTP handlers and clients without a real server.

## Summary

- Accept interfaces, return structs -- this makes code testable
- Create mock structs that implement interfaces for testing
- Inject errors into mocks to test failure paths
- Record method calls to verify interactions
- Keep mocks simple; complex mocks signal design problems

## Reference

- [Go interfaces](https://go.dev/tour/methods/9)
- [Go wiki: Test doubles](https://go.dev/wiki/TableDrivenTests)
- [Dave Cheney: SOLID Go Design](https://dave.cheney.net/2016/08/20/solid-go-design)
