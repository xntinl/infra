# 6. Service Layer Pattern

<!--
difficulty: advanced
concepts: [service-layer, business-logic, orchestration, transaction-management, domain-rules]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [repository-pattern, dependency-injection, transactions, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Repository Pattern](../05-repository-pattern/05-repository-pattern.md)
- Understanding of transactions and error handling

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a service layer that orchestrates domain logic across multiple repositories
- **Separate** business rules from data access and HTTP concerns
- **Manage** cross-cutting concerns like transactions and validation in the service layer

## Why Service Layer Pattern

Repositories handle data access. HTTP handlers handle request parsing. The service layer sits between them, orchestrating business logic. An "order placement" involves checking inventory, processing payment, updating stock, and sending notifications -- spanning multiple repositories and external services. Putting this logic in a handler makes it untestable. Putting it in a repository mixes concerns. The service layer is where it belongs.

## The Problem

Build an e-commerce service layer with an `OrderService` that orchestrates order placement across inventory, payment, and notification systems. The service must enforce business rules, manage transactions, and remain testable.

### Requirements

1. Define repository interfaces for orders, inventory, and users
2. Build an `OrderService` that depends on all three repositories plus a payment processor
3. Implement `PlaceOrder` with business rules: check stock, verify user, charge payment, decrement inventory, save order
4. If any step fails, roll back previous changes
5. Write tests that verify business rules using mock repositories

### Hints

<details>
<summary>Hint 1: Service structure</summary>

```go
type OrderService struct {
    orders    OrderRepository
    inventory InventoryRepository
    users     UserRepository
    payments  PaymentProcessor
    notifier  Notifier
}

func NewOrderService(
    orders OrderRepository,
    inventory InventoryRepository,
    users UserRepository,
    payments PaymentProcessor,
    notifier Notifier,
) *OrderService {
    return &OrderService{orders, inventory, users, payments, notifier}
}
```
</details>

<details>
<summary>Hint 2: Business rule enforcement</summary>

```go
func (s *OrderService) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*Order, error) {
    // 1. Validate the request
    if len(req.Items) == 0 {
        return nil, ErrEmptyOrder
    }

    // 2. Verify user exists
    user, err := s.users.GetByID(ctx, req.UserID)
    if err != nil {
        return nil, fmt.Errorf("user lookup: %w", err)
    }

    // 3. Check and reserve inventory for each item
    // 4. Calculate total
    // 5. Charge payment
    // 6. Save order
    // 7. Send notification (non-fatal if it fails)
}
```
</details>

<details>
<summary>Hint 3: Compensation-based rollback</summary>

When you cannot use a database transaction (e.g., payment is an external API), use compensating actions:

```go
txnID, err := s.payments.Charge(ctx, total)
if err != nil {
    // Release reserved inventory
    for _, item := range reserved {
        s.inventory.Release(ctx, item.ProductID, item.Quantity)
    }
    return nil, fmt.Errorf("payment: %w", err)
}
```
</details>

## Verification

Your program should demonstrate:

```
=== Successful Order ===
  Checked inventory for 3 items
  Charged $149.97 to alice@example.com (txn=txn-001)
  Order ORD-001 saved with status=confirmed
  Notification sent to alice@example.com

=== Insufficient Stock ===
  Error: insufficient stock for "Widget": have 0, need 5

=== Payment Failure ===
  Error: payment failed: card declined
  Inventory released for 2 items (compensating action)
```

```bash
go run main.go
go test -v ./...
```

## What's Next

Continue to [07 - Adapter Pattern](../07-adapter-pattern/07-adapter-pattern.md) to learn how to integrate external systems behind a consistent interface.

## Summary

- The service layer orchestrates business logic across multiple repositories and external services
- Business rules live in the service, not in repositories or handlers
- Use domain errors (`ErrInsufficientStock`, `ErrEmptyOrder`) for business rule violations
- When database transactions are not possible, use compensating actions for rollback
- Non-critical operations (notifications) should not block or fail the main workflow
- Services depend on interfaces, making them fully testable with mocks

## Reference

- [Martin Fowler: Service Layer](https://martinfowler.com/eaaCatalog/serviceLayer.html)
- [Clean Architecture](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [Hexagonal Architecture](https://alistair.cockburn.us/hexagonal-architecture/)
