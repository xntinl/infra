# 4. Dependency Injection

<!--
difficulty: intermediate
concepts: [dependency-injection, constructor-injection, interface-deps, testability, wire]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [interfaces, structs-and-methods, testing-ecosystem]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of interfaces and testing

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** constructor injection to decouple components
- **Define** narrow interfaces at the consumer, not the provider
- **Write** testable services by injecting mock dependencies
- **Compare** manual DI with DI frameworks

## Why Dependency Injection

Without DI, services create their own dependencies: `func NewOrderService() { db := sql.Open(...) }`. This couples the service to a specific database, making it impossible to test without a running database. With DI, you pass dependencies through the constructor: `func NewOrderService(db Database)`. In tests, you pass a mock. In production, you pass the real implementation.

Go's implicit interfaces make DI natural. Define the interface where it is used (the consumer), not where it is implemented (the provider). This keeps interfaces small and focused.

## The Problem

Build an order processing system with three services: OrderService, PaymentGateway, and NotificationService. Wire them together using constructor injection, then demonstrate testability by swapping real implementations with mocks.

## Requirements

1. Define small interfaces in the consumer package, not the provider
2. Each service depends on interfaces, never on concrete types
3. Wire everything in `main` (manual DI)
4. Write tests using mock implementations
5. Demonstrate how adding a new implementation requires zero changes to existing services

## Step 1 -- Define Services with Interface Dependencies

```bash
mkdir -p ~/go-exercises/di
cd ~/go-exercises/di
go mod init di
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

// --- Domain types ---

type Order struct {
	ID        string
	UserEmail string
	Items     []string
	Total     float64
	Status    string
	CreatedAt time.Time
}

// --- Interfaces (defined by the consumer) ---

type OrderStore interface {
	Save(ctx context.Context, order *Order) error
	FindByID(ctx context.Context, id string) (*Order, error)
}

type PaymentProcessor interface {
	Charge(ctx context.Context, email string, amount float64) (transactionID string, err error)
}

type Notifier interface {
	Send(ctx context.Context, to, subject, body string) error
}

// --- OrderService (the consumer) ---

type OrderService struct {
	store    OrderStore
	payment  PaymentProcessor
	notifier Notifier
}

func NewOrderService(store OrderStore, payment PaymentProcessor, notifier Notifier) *OrderService {
	return &OrderService{
		store:    store,
		payment:  payment,
		notifier: notifier,
	}
}

func (s *OrderService) PlaceOrder(ctx context.Context, order *Order) error {
	// Charge payment
	txnID, err := s.payment.Charge(ctx, order.UserEmail, order.Total)
	if err != nil {
		return fmt.Errorf("payment failed: %w", err)
	}
	fmt.Printf("  Payment charged: txn=%s\n", txnID)

	// Save order
	order.Status = "confirmed"
	order.CreatedAt = time.Now()
	if err := s.store.Save(ctx, order); err != nil {
		return fmt.Errorf("save failed: %w", err)
	}
	fmt.Printf("  Order saved: id=%s\n", order.ID)

	// Send notification
	body := fmt.Sprintf("Your order %s for $%.2f has been confirmed.", order.ID, order.Total)
	if err := s.notifier.Send(ctx, order.UserEmail, "Order Confirmed", body); err != nil {
		// Notification failure is non-fatal
		fmt.Printf("  Warning: notification failed: %v\n", err)
	} else {
		fmt.Printf("  Notification sent to %s\n", order.UserEmail)
	}

	return nil
}

// --- Real implementations ---

type InMemoryStore struct {
	orders map[string]*Order
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{orders: make(map[string]*Order)}
}

func (s *InMemoryStore) Save(_ context.Context, order *Order) error {
	s.orders[order.ID] = order
	return nil
}

func (s *InMemoryStore) FindByID(_ context.Context, id string) (*Order, error) {
	o, ok := s.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %s not found", id)
	}
	return o, nil
}

type ConsoleNotifier struct{}

func (n ConsoleNotifier) Send(_ context.Context, to, subject, body string) error {
	fmt.Printf("  [EMAIL] To: %s | Subject: %s\n", to, subject)
	return nil
}

type FakePayment struct{}

func (p FakePayment) Charge(_ context.Context, email string, amount float64) (string, error) {
	return fmt.Sprintf("txn_%s_%.0f", email[:3], amount*100), nil
}

// --- Wiring in main ---

func main() {
	// Manual dependency injection
	store := NewInMemoryStore()
	payment := FakePayment{}
	notifier := ConsoleNotifier{}

	orderService := NewOrderService(store, payment, notifier)

	order := &Order{
		ID:        "ORD-001",
		UserEmail: "alice@example.com",
		Items:     []string{"Widget", "Gadget"},
		Total:     99.95,
	}

	fmt.Println("Placing order:")
	if err := orderService.PlaceOrder(context.Background(), order); err != nil {
		log.Fatal(err)
	}

	// Verify it was stored
	found, _ := store.FindByID(context.Background(), "ORD-001")
	fmt.Printf("\nStored order: %s status=%s\n", found.ID, found.Status)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Placing order:
  Payment charged: txn=ali_9995
  Order saved: id=ORD-001
  Notification sent to alice@example.com
  [EMAIL] To: alice@example.com | Subject: Order Confirmed
Stored order: ORD-001 status=confirmed
```

## Step 2 -- Test with Mocks

Create `main_test.go`:

```go
package main

import (
	"context"
	"errors"
	"testing"
)

type MockStore struct {
	saved []*Order
}

func (m *MockStore) Save(_ context.Context, o *Order) error {
	m.saved = append(m.saved, o)
	return nil
}

func (m *MockStore) FindByID(_ context.Context, id string) (*Order, error) {
	for _, o := range m.saved {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, errors.New("not found")
}

type MockPayment struct {
	ShouldFail bool
}

func (m MockPayment) Charge(_ context.Context, _ string, _ float64) (string, error) {
	if m.ShouldFail {
		return "", errors.New("card declined")
	}
	return "mock-txn-001", nil
}

type MockNotifier struct {
	Sent []string
}

func (m *MockNotifier) Send(_ context.Context, to, _, _ string) error {
	m.Sent = append(m.Sent, to)
	return nil
}

func TestPlaceOrder_Success(t *testing.T) {
	store := &MockStore{}
	payment := MockPayment{}
	notifier := &MockNotifier{}
	svc := NewOrderService(store, payment, notifier)

	order := &Order{ID: "test-1", UserEmail: "test@test.com", Total: 50}
	err := svc.PlaceOrder(context.Background(), order)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 1 {
		t.Errorf("expected 1 saved order, got %d", len(store.saved))
	}
	if store.saved[0].Status != "confirmed" {
		t.Errorf("expected status confirmed, got %s", store.saved[0].Status)
	}
}

func TestPlaceOrder_PaymentFails(t *testing.T) {
	store := &MockStore{}
	payment := MockPayment{ShouldFail: true}
	notifier := &MockNotifier{}
	svc := NewOrderService(store, payment, notifier)

	order := &Order{ID: "test-2", UserEmail: "test@test.com", Total: 50}
	err := svc.PlaceOrder(context.Background(), order)
	if err == nil {
		t.Fatal("expected error for failed payment")
	}
	if len(store.saved) != 0 {
		t.Error("order should not be saved when payment fails")
	}
}
```

### Intermediate Verification

```bash
go test -v
```

## Common Mistakes

### Defining Wide Interfaces in the Provider

**Wrong:**

```go
// In the database package
type Database interface {
    Query(...) ...
    Exec(...) ...
    Begin(...) ...
    Ping(...) ...
    // 20 more methods
}
```

**Fix:** Define narrow interfaces in the consumer: `type OrderStore interface { Save; FindByID }`.

### Using a DI Container When Manual DI Suffices

**What happens:** Unnecessary complexity. Most Go programs do fine with manual wiring in `main`.

**Fix:** Start with manual DI. Introduce a framework (Wire, Fx) only when the dependency graph becomes complex.

## Verification

```bash
go run main.go
go test -v
```

## What's Next

Continue to [05 - Repository Pattern](../05-repository-pattern/05-repository-pattern.md) to learn how to abstract data access behind a clean interface.

## Summary

- Dependency injection passes dependencies through constructors, not globals
- Define interfaces where they are consumed, not where they are implemented
- Keep interfaces small: 1-3 methods per interface is typical in Go
- Manual DI in `main()` is simple and explicit -- no magic
- DI enables testing with mocks -- no real database or external service needed
- Go's implicit interface satisfaction means implementations do not declare their interfaces

## Reference

- [Go Wiki: CodeReviewComments - Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Accept interfaces, return structs](https://bryanftan.medium.com/accept-interfaces-return-structs-in-go-d4cab29a301b)
- [Wire: Compile-time DI](https://github.com/google/wire)
