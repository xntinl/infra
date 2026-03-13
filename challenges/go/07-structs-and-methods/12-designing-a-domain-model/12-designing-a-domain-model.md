# 12. Designing a Domain Model

<!--
difficulty: advanced
concepts: [domain-driven-design, value-objects, entities, aggregates, struct-composition]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [constructor-functions-and-validation, embedding-for-composition, implementing-stringer]
-->

## The Problem

Real applications model business domains. An e-commerce system has orders, products, and customers. A banking system has accounts, transactions, and balances. Designing these domain models in Go requires combining everything you have learned about structs: constructors, validation, embedding, methods, value objects, and encapsulation.

Your task: design a complete domain model for an **order management system** using Go structs, demonstrating DDD concepts translated into idiomatic Go.

## Requirements

1. **Value Objects** (immutable, compared by value):
   - `Money` with amount (int64 cents) and currency (string)
   - `Address` with street, city, state, zip, country

2. **Entities** (identity-based, mutable):
   - `Customer` with ID, name, email, and default address
   - `Product` with ID, name, price (Money), and stock count
   - `Order` with ID, customer, line items, status, and timestamps

3. **Business Rules** (enforced by methods):
   - Orders cannot be placed with zero items
   - Order total is computed from line items
   - Order status transitions: `Draft -> Placed -> Shipped -> Delivered` (no skipping)
   - Products cannot have negative stock
   - Adding a line item reduces product stock

4. **Constructors**: every entity must have a `NewXxx` constructor with validation

5. **Stringer**: at least `Order` and `Money` must implement `fmt.Stringer`

## Hints

<details>
<summary>Hint 1: Money as a value object</summary>

```go
type Currency string

const (
    USD Currency = "USD"
    EUR Currency = "EUR"
)

type Money struct {
    amount   int64    // cents to avoid floating point
    currency Currency
}

func NewMoney(amount int64, currency Currency) Money {
    return Money{amount: amount, currency: currency}
}

func (m Money) Add(other Money) (Money, error) {
    if m.currency != other.currency {
        return Money{}, fmt.Errorf("cannot add %s to %s", m.currency, other.currency)
    }
    return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

func (m Money) String() string {
    return fmt.Sprintf("%.2f %s", float64(m.amount)/100, m.currency)
}
```
</details>

<details>
<summary>Hint 2: Order status as a state machine</summary>

```go
type OrderStatus int

const (
    OrderDraft OrderStatus = iota
    OrderPlaced
    OrderShipped
    OrderDelivered
)

func (s OrderStatus) canTransitionTo(next OrderStatus) bool {
    return next == s+1 // only sequential transitions allowed
}
```
</details>

<details>
<summary>Hint 3: Line items and order total</summary>

```go
type LineItem struct {
    Product  *Product
    Quantity int
    Price    Money // captured at time of adding
}

func (o *Order) Total() Money {
    total := NewMoney(0, USD)
    for _, item := range o.items {
        itemTotal := NewMoney(item.Price.amount*int64(item.Quantity), item.Price.currency)
        total, _ = total.Add(itemTotal)
    }
    return total
}
```
</details>

<details>
<summary>Hint 4: Enforcing invariants</summary>

```go
func (o *Order) Place() error {
    if len(o.items) == 0 {
        return errors.New("cannot place order with no items")
    }
    if !o.status.canTransitionTo(OrderPlaced) {
        return fmt.Errorf("cannot transition from %s to Placed", o.status)
    }
    o.status = OrderPlaced
    o.placedAt = time.Now()
    return nil
}
```
</details>

## Verification

Your program should demonstrate:

1. Creating products with prices in Money
2. Creating a customer with an address
3. Creating an order, adding line items (with stock reduction)
4. Computing the order total
5. Walking through the status state machine: Draft -> Placed -> Shipped -> Delivered
6. Attempting an invalid transition (e.g., Draft -> Shipped) and getting an error
7. Attempting to place an empty order and getting an error

Check your design:
- Are value objects truly immutable (unexported fields, no setters)?
- Do constructors validate all invariants?
- Can business rules be violated by direct field access?
- Does the model prevent impossible states?

## What's Next

You have completed the Structs and Methods section. Continue to [Section 08 - Interfaces](../../08-interfaces/01-implicit-interface-satisfaction/01-implicit-interface-satisfaction.md) to learn how Go's interface system enables polymorphism and decoupling.

## Reference

- [Domain-Driven Design (Eric Evans)](https://www.domainlanguage.com/ddd/)
- [Effective Go: Embedding](https://go.dev/doc/effective_go#embedding)
- [Go Blog: Organizing Go code](https://go.dev/blog/organizing-go-code)
- [Kat Zien - How Do You Structure Your Go Apps (GopherCon 2018)](https://www.youtube.com/watch?v=oL6JBUk6tj0)
