<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [ubiquitous-language, aggregates, value-objects, domain-events, bounded-contexts, anti-corruption-layer, repositories, domain-services]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [object-oriented-design, basic-persistence-patterns, go-interfaces, rust-traits]
papers: [Evans-DDD-2003, Vernon-IDDD-2013]
industry_use: [Axon-Framework, EventStoreDB, Microsoft-eShopOnContainers, Spotify-domain-model]
language_contrast: medium
-->

# Domain-Driven Design

> DDD is the discipline of making your code speak the same language as the business — because when they diverge, you build the wrong system correctly.

## Mental Model

At 10,000 lines of code, the names in your codebase start to drift from the names in the business. Your `User` entity carries fields that only make sense to the billing team. Your `Order` aggregate has methods that the shipping team calls "fulfillment" but you named "processing". The marketing team talks about "campaigns" but your code has `PromoCode`. At 10,000 lines this is annoying. At 100,000 lines, the engineering team and the domain experts cannot have a productive conversation without a translation layer in every meeting, and that translation layer is where bugs live.

The first insight of DDD is that this drift is not inevitable — it is a choice. If you decide that code names must match domain expert names, and you enforce that decision, you get Ubiquitous Language: one shared vocabulary used in meetings, documents, and code. When a domain expert says "an order can only be cancelled if it has not been shipped," that sentence maps directly to a method on an `Order` aggregate: `func (o *Order) Cancel() error`. No translation. No interpretation.

The second insight is about consistency boundaries. Most systems make the mistake of treating the entire database as the consistency unit — they begin a transaction, load any entities they need, modify them, and commit. This works until two teams need to modify the same tables concurrently, or until the aggregate spans so many rows that locking becomes a bottleneck. DDD replaces this with a precise definition: an Aggregate is the consistency boundary. Within one aggregate, all invariants (business rules) must hold after every operation. Across aggregates, consistency is eventual. This is not a design suggestion — it is a contract that determines how you structure your transactions, your locks, and your events. Getting this wrong is the most common and most expensive DDD mistake.

Bounded Contexts are the third insight, and the most politically charged one. The word "customer" means different things to the billing team (an entity with a payment method and a credit limit) and the shipping team (an entity with a delivery address and a preferred carrier). DDD says this is fine — these are different models for the same concept in different bounded contexts. Trying to unify them into one `Customer` entity that serves both contexts is the road to an unmaintainable god object. Instead, you define explicit boundaries, and where contexts interact, you build an Anti-Corruption Layer (ACL) that translates between their models.

## Core Concepts

### Ubiquitous Language

Every concept that domain experts use in conversation must appear in the code with the same name and meaning. This requires engineers to attend domain sessions, ask questions, and push back when business names are ambiguous. The language evolves: when domain experts start using a term you introduced, or when you adopt a term they corrected, that's Ubiquitous Language working.

Violations look like: `UserType = 3` (what does 3 mean?), `processOrder()` (the business calls it "confirm"), `getCustomerForBilling()` (why is "billing" in the method name?).

### Aggregates and Aggregate Roots

An Aggregate is a cluster of domain objects treated as a unit for data changes. The Aggregate Root is the single entry point: no external object holds a reference to an inner entity of the aggregate — they go through the root. This enforces the invariant: the root is responsible for all consistency rules within its boundary.

The rule of thumb: "if two things must be consistent together in a single transaction, they belong in the same aggregate." If they merely need to be eventually consistent, they should be separate aggregates connected by domain events.

A common mistake is making aggregates too large. An `Order` aggregate that contains the customer's full profile, all historical orders, and payment methods is not an aggregate — it is a god object with an aggregate hat on. Load only what you need to enforce the invariant for this operation.

### Value Objects

A Value Object has no identity. Two `Money` values of `100 USD` are equal regardless of when they were created. Value Objects are immutable — you don't "change" a price, you replace it with a new `Money` value. This eliminates an entire class of bugs where shared mutable state causes surprising side effects.

In practice: `Money`, `Email`, `Address`, `OrderId`, `Quantity` are Value Objects. `Order`, `Customer`, `Product` are Entities (they have identity that persists over time).

### Domain Events

A Domain Event is a fact that happened in the domain: `OrderPlaced`, `PaymentCaptured`, `ItemShipped`. Events are named in past tense because they are immutable facts. Publishing domain events is how aggregates communicate across bounded context boundaries without direct coupling.

### Repositories

A Repository is an abstraction over persistence. It speaks the domain language: `OrderRepository.FindByCustomer(customerId)`, not `db.Query("SELECT * FROM orders WHERE customer_id = ?")`. The domain layer defines the Repository interface. The infrastructure layer implements it with SQL, Redis, or whatever. This is how you get a domain model that has zero dependencies on infrastructure.

### Anti-Corruption Layer

When your bounded context must integrate with an external system (another service, a legacy system, a third-party API), the ACL translates their model into yours. Without it, their terminology and concepts leak into your domain, corrupting your Ubiquitous Language and creating coupling to an external model you don't control.

## Implementation: Go

```go
package main

import (
	"errors"
	"fmt"
	"time"
)

// ─── Value Objects ───────────────────────────────────────────────────────────

// Money is immutable. Two Money values with the same amount and currency are equal.
// Operations return new Money values; they never mutate.
type Money struct {
	amount   int64  // stored in minor units (cents) to avoid floating-point errors
	currency string // ISO 4217 code: "USD", "EUR"
}

func NewMoney(amount int64, currency string) (Money, error) {
	if currency == "" {
		return Money{}, errors.New("currency cannot be empty")
	}
	if amount < 0 {
		return Money{}, errors.New("money amount cannot be negative")
	}
	return Money{amount: amount, currency: currency}, nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("cannot add %s to %s", other.currency, m.currency)
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

func (m Money) IsGreaterThan(other Money) bool {
	return m.amount > other.amount
}

func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.amount, m.currency)
}

// OrderID is a typed value object — it prevents passing a ProductID where an OrderID is expected.
type OrderID string
type ProductID string
type CustomerID string

// ─── Domain Events ───────────────────────────────────────────────────────────

type DomainEvent interface {
	EventName() string
	OccurredAt() time.Time
}

type OrderPlaced struct {
	OrderID    OrderID
	CustomerID CustomerID
	Total      Money
	occurredAt time.Time
}

func (e OrderPlaced) EventName() string      { return "OrderPlaced" }
func (e OrderPlaced) OccurredAt() time.Time  { return e.occurredAt }

type OrderCancelled struct {
	OrderID    OrderID
	Reason     string
	occurredAt time.Time
}

func (e OrderCancelled) EventName() string      { return "OrderCancelled" }
func (e OrderCancelled) OccurredAt() time.Time  { return e.occurredAt }

// ─── Entities and Aggregates ─────────────────────────────────────────────────

type OrderStatus string

const (
	OrderStatusDraft     OrderStatus = "draft"
	OrderStatusPlaced    OrderStatus = "placed"
	OrderStatusShipped   OrderStatus = "shipped"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// OrderLine is an entity within the Order aggregate.
// It has no meaning outside the Order — you never load an OrderLine directly.
type OrderLine struct {
	productID ProductID
	quantity  int
	unitPrice Money
}

func (ol OrderLine) LineTotal() (Money, error) {
	total := ol.unitPrice
	for i := 1; i < ol.quantity; i++ {
		var err error
		total, err = total.Add(ol.unitPrice)
		if err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

// Order is the Aggregate Root.
// All mutations go through its methods. No external code touches OrderLine directly.
// The aggregate enforces the invariant: a placed order cannot be modified.
type Order struct {
	id         OrderID
	customerID CustomerID
	lines      []OrderLine
	status     OrderStatus
	placedAt   *time.Time

	// Uncommitted events are collected here and flushed after persistence.
	// The application layer is responsible for publishing them.
	uncommittedEvents []DomainEvent
}

func NewOrder(id OrderID, customerID CustomerID) *Order {
	return &Order{
		id:         id,
		customerID: customerID,
		status:     OrderStatusDraft,
		lines:      make([]OrderLine, 0),
	}
}

func (o *Order) AddLine(productID ProductID, quantity int, unitPrice Money) error {
	if o.status != OrderStatusDraft {
		return fmt.Errorf("cannot add items to an order in status %q", o.status)
	}
	if quantity <= 0 {
		return errors.New("quantity must be positive")
	}
	o.lines = append(o.lines, OrderLine{
		productID: productID,
		quantity:  quantity,
		unitPrice: unitPrice,
	})
	return nil
}

func (o *Order) Place() error {
	if o.status != OrderStatusDraft {
		return fmt.Errorf("can only place a draft order, current status: %q", o.status)
	}
	if len(o.lines) == 0 {
		return errors.New("cannot place an order with no items")
	}

	now := time.Now()
	o.status = OrderStatusPlaced
	o.placedAt = &now

	total, err := o.Total()
	if err != nil {
		return fmt.Errorf("calculating order total: %w", err)
	}

	o.uncommittedEvents = append(o.uncommittedEvents, OrderPlaced{
		OrderID:    o.id,
		CustomerID: o.customerID,
		Total:      total,
		occurredAt: now,
	})
	return nil
}

// Cancel enforces the business rule: shipped orders cannot be cancelled.
// This rule lives in the aggregate, not in the application service or the HTTP handler.
func (o *Order) Cancel(reason string) error {
	switch o.status {
	case OrderStatusCancelled:
		return errors.New("order is already cancelled")
	case OrderStatusShipped:
		return errors.New("cannot cancel a shipped order")
	}

	o.status = OrderStatusCancelled
	o.uncommittedEvents = append(o.uncommittedEvents, OrderCancelled{
		OrderID:    o.id,
		Reason:     reason,
		occurredAt: time.Now(),
	})
	return nil
}

func (o *Order) Total() (Money, error) {
	if len(o.lines) == 0 {
		return Money{}, errors.New("order has no lines")
	}
	total, err := o.lines[0].LineTotal()
	if err != nil {
		return Money{}, err
	}
	for _, line := range o.lines[1:] {
		lineTotal, err := line.LineTotal()
		if err != nil {
			return Money{}, err
		}
		total, err = total.Add(lineTotal)
		if err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

func (o *Order) FlushEvents() []DomainEvent {
	events := o.uncommittedEvents
	o.uncommittedEvents = nil
	return events
}

func (o *Order) ID() OrderID         { return o.id }
func (o *Order) Status() OrderStatus { return o.status }

// ─── Repository (interface defined in domain layer) ──────────────────────────

// OrderRepository is defined by the domain layer. Infrastructure implements it.
// The domain never imports a database driver — only this interface.
type OrderRepository interface {
	Save(order *Order) error
	FindByID(id OrderID) (*Order, error)
	FindByCustomer(customerID CustomerID) ([]*Order, error)
}

// ─── Application Service ─────────────────────────────────────────────────────

// EventPublisher is defined by the application layer.
// Infrastructure implements it with Kafka, RabbitMQ, or an in-process bus.
type EventPublisher interface {
	Publish(events []DomainEvent) error
}

// PlaceOrderCommand carries the intent and its input data.
// Commands are immutable value objects.
type PlaceOrderCommand struct {
	OrderID    OrderID
	CustomerID CustomerID
	Lines      []PlaceOrderLine
}

type PlaceOrderLine struct {
	ProductID ProductID
	Quantity  int
	UnitPrice Money
}

// OrderApplicationService orchestrates domain objects.
// It has NO business logic — it delegates all decisions to the domain.
// It is responsible for: loading aggregates, calling domain methods,
// persisting the result, and publishing events.
type OrderApplicationService struct {
	orders    OrderRepository
	publisher EventPublisher
}

func NewOrderApplicationService(orders OrderRepository, publisher EventPublisher) *OrderApplicationService {
	return &OrderApplicationService{orders: orders, publisher: publisher}
}

func (svc *OrderApplicationService) PlaceOrder(cmd PlaceOrderCommand) error {
	order := NewOrder(cmd.OrderID, cmd.CustomerID)

	for _, line := range cmd.Lines {
		if err := order.AddLine(line.ProductID, line.Quantity, line.UnitPrice); err != nil {
			return fmt.Errorf("adding order line: %w", err)
		}
	}

	if err := order.Place(); err != nil {
		return fmt.Errorf("placing order: %w", err)
	}

	if err := svc.orders.Save(order); err != nil {
		return fmt.Errorf("saving order: %w", err)
	}

	// Events are published after persistence. If this fails, use the outbox pattern
	// (see event-driven architecture section) to guarantee delivery.
	events := order.FlushEvents()
	if err := svc.publisher.Publish(events); err != nil {
		return fmt.Errorf("publishing events: %w", err)
	}
	return nil
}

func (svc *OrderApplicationService) CancelOrder(orderID OrderID, reason string) error {
	order, err := svc.orders.FindByID(orderID)
	if err != nil {
		return fmt.Errorf("loading order: %w", err)
	}

	if err := order.Cancel(reason); err != nil {
		return fmt.Errorf("cancelling order: %w", err)
	}

	if err := svc.orders.Save(order); err != nil {
		return fmt.Errorf("saving cancelled order: %w", err)
	}

	events := order.FlushEvents()
	return svc.publisher.Publish(events)
}

// ─── Anti-Corruption Layer ───────────────────────────────────────────────────

// ExternalPaymentService represents a third-party payment provider's model.
// Its types use their terminology, not ours.
type ExternalPaymentRecord struct {
	TransactionRef string
	AmountInCents  int64
	CurrencyCode   string
	StatusCode     int // 1=success, 2=pending, 3=failed
}

// PaymentStatus is our domain's concept of payment status.
type PaymentStatus string

const (
	PaymentStatusSucceeded PaymentStatus = "succeeded"
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusFailed    PaymentStatus = "failed"
)

// PaymentInfo is our domain's value object for payment information.
type PaymentInfo struct {
	Reference string
	Amount    Money
	Status    PaymentStatus
}

// PaymentACL translates the external payment provider's model into our domain model.
// Changes to the external API only affect this layer.
type PaymentACL struct{}

func (acl PaymentACL) Translate(external ExternalPaymentRecord) (PaymentInfo, error) {
	amount, err := NewMoney(external.AmountInCents, external.CurrencyCode)
	if err != nil {
		return PaymentInfo{}, fmt.Errorf("translating payment amount: %w", err)
	}

	status, err := acl.translateStatus(external.StatusCode)
	if err != nil {
		return PaymentInfo{}, err
	}

	return PaymentInfo{
		Reference: external.TransactionRef,
		Amount:    amount,
		Status:    status,
	}, nil
}

func (acl PaymentACL) translateStatus(code int) (PaymentStatus, error) {
	switch code {
	case 1:
		return PaymentStatusSucceeded, nil
	case 2:
		return PaymentStatusPending, nil
	case 3:
		return PaymentStatusFailed, nil
	default:
		return "", fmt.Errorf("unknown payment status code %d", code)
	}
}

// ─── In-memory Repository (infrastructure layer) ─────────────────────────────

type InMemoryOrderRepository struct {
	orders map[OrderID]*Order
}

func NewInMemoryOrderRepository() *InMemoryOrderRepository {
	return &InMemoryOrderRepository{orders: make(map[OrderID]*Order)}
}

func (r *InMemoryOrderRepository) Save(order *Order) error {
	r.orders[order.id] = order
	return nil
}

func (r *InMemoryOrderRepository) FindByID(id OrderID) (*Order, error) {
	order, ok := r.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}
	return order, nil
}

func (r *InMemoryOrderRepository) FindByCustomer(customerID CustomerID) ([]*Order, error) {
	var result []*Order
	for _, o := range r.orders {
		if o.customerID == customerID {
			result = append(result, o)
		}
	}
	return result, nil
}

// LoggingEventPublisher prints events to stdout for demonstration purposes.
type LoggingEventPublisher struct{}

func (p *LoggingEventPublisher) Publish(events []DomainEvent) error {
	for _, e := range events {
		fmt.Printf("[EVENT] %s at %s\n", e.EventName(), e.OccurredAt().Format(time.RFC3339))
	}
	return nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	repo := NewInMemoryOrderRepository()
	publisher := &LoggingEventPublisher{}
	svc := NewOrderApplicationService(repo, publisher)

	unitPrice, _ := NewMoney(2999, "USD") // $29.99

	cmd := PlaceOrderCommand{
		OrderID:    OrderID("order-001"),
		CustomerID: CustomerID("customer-42"),
		Lines: []PlaceOrderLine{
			{ProductID: "prod-notebook", Quantity: 2, UnitPrice: unitPrice},
		},
	}

	if err := svc.PlaceOrder(cmd); err != nil {
		fmt.Printf("Error placing order: %v\n", err)
		return
	}
	fmt.Println("Order placed successfully")

	// Demonstrate the invariant: shipped orders cannot be cancelled
	order, _ := repo.FindByID("order-001")
	order.status = OrderStatusShipped // simulate shipment

	if err := svc.CancelOrder("order-001", "customer changed mind"); err != nil {
		fmt.Printf("Expected error: %v\n", err) // "cannot cancel a shipped order"
	}
}
```

### Go-specific considerations

Go's interface system aligns naturally with DDD's dependency inversion. The `OrderRepository` interface is defined in the domain package, and the infrastructure package imports the domain — not the other way around. This is the correct dependency direction without any framework magic.

Go's lack of inheritance prevents the common DDD mistake of using deep class hierarchies for domain modeling. Composition via embedding is available, but most Go DDD code avoids it for aggregates — an `Order` that embeds a base `AggregateRoot` struct is idiomatic in Java DDD but feels unnatural in Go. Instead, Go practitioners tend to keep the event collection on the aggregate directly or use a thin wrapper.

The absence of sum types (sealed unions) in Go means representing domain states with string constants or iota. This compiles but provides no exhaustiveness check — a `switch` on `OrderStatus` that misses `OrderStatusShipped` will compile without warning. Rust's enums solve this; in Go, you compensate with table-driven tests that cover every status.

Value Objects in Go are best represented as structs with unexported fields and constructor functions that validate invariants. The `Money` struct above demonstrates this: you cannot construct an invalid `Money` (negative amount, empty currency) because the only path is `NewMoney`, which validates.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::fmt;
use chrono::{DateTime, Utc};

// ─── Value Objects ──────────────────────────────────────────────────────────

/// Money is a value object: immutable, equality by value, no identity.
/// The type system enforces immutability — there are no &mut Money methods.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Money {
    amount: i64,   // minor units (cents)
    currency: String,
}

#[derive(Debug)]
pub enum MoneyError {
    NegativeAmount,
    EmptyCurrency,
    CurrencyMismatch { expected: String, got: String },
}

impl fmt::Display for MoneyError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            MoneyError::NegativeAmount => write!(f, "money amount cannot be negative"),
            MoneyError::EmptyCurrency => write!(f, "currency cannot be empty"),
            MoneyError::CurrencyMismatch { expected, got } => {
                write!(f, "currency mismatch: expected {expected}, got {got}")
            }
        }
    }
}

impl Money {
    pub fn new(amount: i64, currency: impl Into<String>) -> Result<Self, MoneyError> {
        if amount < 0 {
            return Err(MoneyError::NegativeAmount);
        }
        let currency = currency.into();
        if currency.is_empty() {
            return Err(MoneyError::EmptyCurrency);
        }
        Ok(Money { amount, currency })
    }

    pub fn add(&self, other: &Money) -> Result<Money, MoneyError> {
        if self.currency != other.currency {
            return Err(MoneyError::CurrencyMismatch {
                expected: self.currency.clone(),
                got: other.currency.clone(),
            });
        }
        Ok(Money {
            amount: self.amount + other.amount,
            currency: self.currency.clone(),
        })
    }

    pub fn amount(&self) -> i64 { self.amount }
    pub fn currency(&self) -> &str { &self.currency }
}

// Newtype pattern: OrderId, ProductId, CustomerId are distinct types.
// The compiler rejects passing a ProductId where an OrderId is expected.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct OrderId(String);

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct ProductId(String);

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct CustomerId(String);

impl OrderId {
    pub fn new(id: impl Into<String>) -> Self { OrderId(id.into()) }
}
impl ProductId {
    pub fn new(id: impl Into<String>) -> Self { ProductId(id.into()) }
}
impl CustomerId {
    pub fn new(id: impl Into<String>) -> Self { CustomerId(id.into()) }
}

// ─── Domain Events ──────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub enum DomainEvent {
    OrderPlaced {
        order_id: OrderId,
        customer_id: CustomerId,
        total: Money,
        occurred_at: DateTime<Utc>,
    },
    OrderCancelled {
        order_id: OrderId,
        reason: String,
        occurred_at: DateTime<Utc>,
    },
}

impl DomainEvent {
    pub fn event_name(&self) -> &str {
        match self {
            DomainEvent::OrderPlaced { .. } => "OrderPlaced",
            DomainEvent::OrderCancelled { .. } => "OrderCancelled",
        }
    }
}

// ─── Aggregate ──────────────────────────────────────────────────────────────

/// OrderStatus uses a Rust enum — exhaustiveness is checked at compile time.
/// Any match that misses a variant fails to compile.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderStatus {
    Draft,
    Placed,
    Shipped,
    Cancelled,
}

#[derive(Debug, Clone)]
pub struct OrderLine {
    product_id: ProductId,
    quantity: u32,
    unit_price: Money,
}

impl OrderLine {
    pub fn line_total(&self) -> Result<Money, MoneyError> {
        let mut total = self.unit_price.clone();
        for _ in 1..self.quantity {
            total = total.add(&self.unit_price)?;
        }
        Ok(total)
    }
}

#[derive(Debug)]
pub enum OrderError {
    InvalidStatus { operation: &'static str, current: OrderStatus },
    EmptyOrder,
    InvalidQuantity,
    MoneyError(MoneyError),
}

impl fmt::Display for OrderError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            OrderError::InvalidStatus { operation, current } => {
                write!(f, "cannot {operation} order in status {current:?}")
            }
            OrderError::EmptyOrder => write!(f, "cannot place an order with no items"),
            OrderError::InvalidQuantity => write!(f, "quantity must be positive"),
            OrderError::MoneyError(e) => write!(f, "money error: {e}"),
        }
    }
}

impl From<MoneyError> for OrderError {
    fn from(e: MoneyError) -> Self { OrderError::MoneyError(e) }
}

/// Order is the Aggregate Root.
/// Private fields enforce that all mutations go through the aggregate's methods.
/// The type system prevents constructing an Order in an invalid state.
pub struct Order {
    id: OrderId,
    customer_id: CustomerId,
    lines: Vec<OrderLine>,
    status: OrderStatus,
    uncommitted_events: Vec<DomainEvent>,
}

impl Order {
    pub fn new(id: OrderId, customer_id: CustomerId) -> Self {
        Order {
            id,
            customer_id,
            lines: Vec::new(),
            status: OrderStatus::Draft,
            uncommitted_events: Vec::new(),
        }
    }

    pub fn add_line(
        &mut self,
        product_id: ProductId,
        quantity: u32,
        unit_price: Money,
    ) -> Result<(), OrderError> {
        if self.status != OrderStatus::Draft {
            return Err(OrderError::InvalidStatus {
                operation: "add items to",
                current: self.status.clone(),
            });
        }
        if quantity == 0 {
            return Err(OrderError::InvalidQuantity);
        }
        self.lines.push(OrderLine { product_id, quantity, unit_price });
        Ok(())
    }

    pub fn place(&mut self) -> Result<(), OrderError> {
        if self.status != OrderStatus::Draft {
            return Err(OrderError::InvalidStatus {
                operation: "place",
                current: self.status.clone(),
            });
        }
        if self.lines.is_empty() {
            return Err(OrderError::EmptyOrder);
        }

        let total = self.total()?;
        self.status = OrderStatus::Placed;

        self.uncommitted_events.push(DomainEvent::OrderPlaced {
            order_id: self.id.clone(),
            customer_id: self.customer_id.clone(),
            total,
            occurred_at: Utc::now(),
        });
        Ok(())
    }

    pub fn cancel(&mut self, reason: String) -> Result<(), OrderError> {
        match self.status {
            OrderStatus::Cancelled => {
                return Err(OrderError::InvalidStatus {
                    operation: "cancel (already cancelled)",
                    current: OrderStatus::Cancelled,
                })
            }
            OrderStatus::Shipped => {
                return Err(OrderError::InvalidStatus {
                    operation: "cancel",
                    current: OrderStatus::Shipped,
                })
            }
            _ => {}
        }

        self.status = OrderStatus::Cancelled;
        self.uncommitted_events.push(DomainEvent::OrderCancelled {
            order_id: self.id.clone(),
            reason,
            occurred_at: Utc::now(),
        });
        Ok(())
    }

    pub fn total(&self) -> Result<Money, OrderError> {
        if self.lines.is_empty() {
            return Err(OrderError::EmptyOrder);
        }
        let mut total = self.lines[0].line_total()?;
        for line in &self.lines[1..] {
            let line_total = line.line_total()?;
            total = total.add(&line_total)?;
        }
        Ok(total)
    }

    pub fn flush_events(&mut self) -> Vec<DomainEvent> {
        std::mem::take(&mut self.uncommitted_events)
    }

    pub fn id(&self) -> &OrderId { &self.id }
    pub fn status(&self) -> &OrderStatus { &self.status }
}

// ─── Repository trait (domain layer) ────────────────────────────────────────

pub trait OrderRepository {
    fn save(&mut self, order: &Order) -> Result<(), RepositoryError>;
    fn find_by_id(&self, id: &OrderId) -> Result<Order, RepositoryError>;
}

#[derive(Debug)]
pub enum RepositoryError {
    NotFound(String),
    StorageError(String),
}

impl fmt::Display for RepositoryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            RepositoryError::NotFound(id) => write!(f, "order {id} not found"),
            RepositoryError::StorageError(msg) => write!(f, "storage error: {msg}"),
        }
    }
}

// ─── Application Service ─────────────────────────────────────────────────────

pub trait EventPublisher {
    fn publish(&self, events: Vec<DomainEvent>);
}

pub struct PlaceOrderCommand {
    pub order_id: OrderId,
    pub customer_id: CustomerId,
    pub lines: Vec<(ProductId, u32, Money)>,
}

pub struct OrderApplicationService<R: OrderRepository, P: EventPublisher> {
    repository: R,
    publisher: P,
}

impl<R: OrderRepository, P: EventPublisher> OrderApplicationService<R, P> {
    pub fn new(repository: R, publisher: P) -> Self {
        OrderApplicationService { repository, publisher }
    }

    pub fn place_order(&mut self, cmd: PlaceOrderCommand) -> Result<(), Box<dyn std::error::Error>> {
        let mut order = Order::new(cmd.order_id, cmd.customer_id);
        for (product_id, quantity, unit_price) in cmd.lines {
            order.add_line(product_id, quantity, unit_price)?;
        }
        order.place()?;
        self.repository.save(&order)?;
        let events = order.flush_events();
        self.publisher.publish(events);
        Ok(())
    }
}

// ─── In-memory infrastructure ────────────────────────────────────────────────

pub struct InMemoryOrderRepository {
    // We store a snapshot. A real Event Sourcing repo would store events instead.
    orders: HashMap<String, OrderSnapshot>,
}

struct OrderSnapshot {
    id: OrderId,
    customer_id: CustomerId,
    status: OrderStatus,
    lines: Vec<OrderLine>,
}

impl InMemoryOrderRepository {
    pub fn new() -> Self {
        InMemoryOrderRepository { orders: HashMap::new() }
    }
}

impl OrderRepository for InMemoryOrderRepository {
    fn save(&mut self, order: &Order) -> Result<(), RepositoryError> {
        self.orders.insert(
            order.id.0.clone(),
            OrderSnapshot {
                id: order.id.clone(),
                customer_id: order.customer_id.clone(),
                status: order.status.clone(),
                lines: order.lines.clone(),
            },
        );
        Ok(())
    }

    fn find_by_id(&self, id: &OrderId) -> Result<Order, RepositoryError> {
        let snap = self.orders.get(&id.0)
            .ok_or_else(|| RepositoryError::NotFound(id.0.clone()))?;
        let mut order = Order::new(snap.id.clone(), snap.customer_id.clone());
        order.status = snap.status.clone();
        order.lines = snap.lines.clone();
        Ok(order)
    }
}

pub struct PrintingEventPublisher;

impl EventPublisher for PrintingEventPublisher {
    fn publish(&self, events: Vec<DomainEvent>) {
        for event in events {
            println!("[EVENT] {}", event.event_name());
        }
    }
}

fn main() {
    let repo = InMemoryOrderRepository::new();
    let publisher = PrintingEventPublisher;
    let mut svc = OrderApplicationService::new(repo, publisher);

    let unit_price = Money::new(2999, "USD").unwrap();

    let cmd = PlaceOrderCommand {
        order_id: OrderId::new("order-001"),
        customer_id: CustomerId::new("customer-42"),
        lines: vec![(ProductId::new("prod-notebook"), 2, unit_price)],
    };

    match svc.place_order(cmd) {
        Ok(_) => println!("Order placed successfully"),
        Err(e) => println!("Error: {e}"),
    }
}
```

### Rust-specific considerations

Rust's enum exhaustiveness is the killer feature for DDD. `OrderStatus` as a Rust enum means every `match` must handle every variant — the compiler rejects code that ignores `OrderStatus::Shipped` in a cancel handler. This is exactly the kind of invariant that DDD practitioners try to enforce manually in other languages.

The newtype pattern (`struct OrderId(String)`) provides type safety with zero runtime cost. It is idiomatic Rust for Value Object identity types. In Go, `type OrderID string` provides similar protection; in Rust, the newtype is a struct and prevents implicit coercion more strictly.

Rust's ownership model maps well onto the Aggregate's "single owner" semantics: an aggregate should be loaded, mutated, and saved by one unit of work at a time. `&mut Order` in Rust makes this explicit — you cannot have two mutable references to the same aggregate simultaneously, which mirrors the intent that an aggregate is the unit of transactional consistency.

The generic `OrderApplicationService<R: OrderRepository, P: EventPublisher>` shows how Rust's trait system replaces Go's interface-based dependency injection. The tradeoff: Rust's approach is monomorphic (specialized at compile time, zero overhead) but creates more complex type signatures; Go's interfaces are dynamic dispatch with slightly more overhead but simpler to read.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Invariant enforcement | String constants for status; no exhaustiveness check on switch | Enum variants; compiler-enforced exhaustiveness |
| Value Object immutability | Convention (unexported fields + no mutation methods) | Enforced by type system (`&T` vs `&mut T`) |
| Repository injection | Interface in domain package; implicit satisfaction | Trait in domain module; explicit `impl Trait for Type` |
| Newtype IDs | `type OrderID string` — prevents most misuse | `struct OrderId(String)` — stricter, zero-cost |
| Error handling | `error` interface; errors lose type after wrapping | `Result<T, E>` with typed errors; exhaustive `match` |
| Boilerplate | Minimal; interfaces are small | More verbose; traits + impl blocks + From impls |
| Domain event collection | Slice on aggregate struct | `Vec<DomainEvent>` on struct; `std::mem::take` for drain |
| Testability | Easy mock via interface | Easy mock via trait + test struct |

## Production War Stories

**Uber Eats domain redesign (2018–2020)**: Uber Eats migrated from a monolith to a domain-driven microservices architecture after their original "restaurant" concept became polluted with delivery, billing, and menu concerns. Their engineering blog describes explicitly using Bounded Contexts to separate the "restaurant as menu provider" from the "restaurant as fulfillment location." The ACL between their ordering context and their dispatch context prevented the dispatch model from leaking into order management.

**Microsoft eShopOnContainers**: Microsoft's reference microservices application is a textbook DDD + CQRS + Event Sourcing implementation. The ordering microservice shows an `Order` aggregate with explicit domain events and the event-driven consistency between bounded contexts. The source code is the closest thing to "DDD done in production" you can study for free: github.com/dotnet-architecture/eShopOnContainers.

**Spotify's squad model and DDD**: Spotify's well-documented squad model was partly driven by DDD Bounded Contexts — each squad owns one bounded context, and cross-squad communication goes through defined APIs (Anti-Corruption Layers). The "we own this domain" organizational alignment that DDD recommends maps directly to how Spotify structured engineering ownership. When they diverged from this (squads that spanned multiple contexts), they reported increased coordination overhead.

**Domain model corruption at a fintech (unnamed)**: A common post-mortem pattern is a `User` aggregate that started as an authentication concept and accumulated 47 fields over four years — email, billing address, shipping preferences, KYC status, referral codes, loyalty points. The aggregate could not be saved in a single transaction without locking most of the user table. The fix was splitting into `AuthIdentity`, `BillingProfile`, and `LoyaltyAccount` bounded contexts, each with its own aggregate root. The migration took six months.

## Architectural Trade-offs

**When to use DDD:**
- Complex domain with rich business rules (financial services, healthcare, logistics, e-commerce)
- Multiple teams working on overlapping concepts that risk language drift
- System expected to grow beyond 50k lines over 2+ years
- Domain experts are available and willing to collaborate on language

**When NOT to use DDD:**
- CRUD applications with minimal business logic (a content management system, a simple form backend)
- Small teams (2–3 people) with full context — the overhead of explicit bounded contexts and aggregates is not justified
- Tight deadline prototypes — DDD pays dividends over years, not sprints
- When domain experts are unavailable — DDD without a domain expert produces a developer's guess at the domain, which is worse than naive CRUD

**The organizational cost**: DDD requires that engineers participate in domain discovery sessions. This is not a technical cost — it is a time and political cost. Engineering teams that treat themselves as ticket-executors will not adopt DDD successfully because the Ubiquitous Language cannot be built without conversation.

## Common Pitfalls

**1. Making aggregates too large because transactions are familiar.** The most common mistake: treating "what data might I need?" as the boundary rather than "what invariants must hold together?" An `Order` that loads the customer's full profile to check a discount code should load only the discount code, not the profile. Large aggregates become contention bottlenecks.

**2. Leaking infrastructure into the domain.** When a domain method imports a database package, a logger, or an HTTP client, you have inverted the dependency direction. The domain should have zero imports from infrastructure. If you catch yourself calling `slog.Info()` inside `Order.Place()`, extract it to the application service.

**3. Using domain events for integration before using them for intra-aggregate communication.** Teams often jump to publishing domain events to a message bus before establishing the pattern within the domain itself. Start with uncommitted events collected on the aggregate and flushed by the application service. Add the message bus later.

**4. Skipping the Ubiquitous Language because naming is hard.** Developers rename domain concepts because the domain names are long or ambiguous. Every rename that diverges from the domain expert's vocabulary accumulates translation debt. Push back on long or ambiguous domain names in domain sessions rather than renaming them in code.

**5. Treating the repository as a query engine.** Repositories should provide collection-like access: `FindByID`, `FindByCustomer`. Complex queries for read models (dashboards, reports) should not go through the aggregate repository — they should have their own read model (see CQRS section). A repository with 15 `FindBy*` methods is a query service wearing a repository hat.

## Exercises

**Exercise 1** (30 min): Trace through the Go implementation. Add an `UpdateShippingAddress` method to `Order` that is only allowed in `Draft` or `Placed` status, and write a test that verifies the status invariant.

**Exercise 2** (2–4h): Implement a `Catalog` bounded context with a `Product` aggregate. Define a `ProductId` value object and a `Price` value object. Add a domain event `PriceChanged`. In the `Order` bounded context, reference products by `ProductId` only (not by importing the `Product` aggregate) — they are separate bounded contexts.

**Exercise 3** (4–8h): Implement an Anti-Corruption Layer between an external inventory system (which uses `SKU` and `stock_qty`) and your `Order` bounded context (which uses `ProductId` and `Quantity`). The ACL should translate between the two models and handle the case where the external system's concept of "unavailable" maps to two different states in your domain.

**Exercise 4** (8–15h): Implement a complete small e-commerce domain with three bounded contexts: `Ordering`, `Payments`, and `Shipping`. Define explicit ACLs at the boundaries. Use domain events (collected on aggregates, not yet published to a bus) to communicate the facts that cross context boundaries. Include a repository per aggregate, an application service per context, and test the domain logic without any infrastructure.

## Further Reading

### Foundational Books

- **Domain-Driven Design** — Eric Evans (2003). The original text. Dense and repetitive, but chapters 5–7 (Aggregates, Repositories, Domain Events) are the canonical reference.
- **Implementing Domain-Driven Design** — Vaughn Vernon (2013). More practical than Evans. The chapters on Aggregate design are the clearest explanation of consistency boundaries in print.
- **Domain-Driven Design Distilled** — Vaughn Vernon (2016). A 170-page summary of the above two books. Read this first if you are new to DDD.

### Blog Posts and Case Studies

- Martin Fowler's bliki: "DDD Aggregate" — martinfowler.com/bliki/DDD_Aggregate.html
- Vaughn Vernon: "Effective Aggregate Design" (three-part series) — vaughnvernon.com
- Microsoft Architecture Guide: "Domain-Driven Design" — learn.microsoft.com/en-us/azure/architecture/microservices/model/domain-analysis

### Production Code to Read

- **eShopOnContainers** — github.com/dotnet-architecture/eShopOnContainers. The `ordering-microservice` is a full DDD + CQRS + Event Sourcing implementation.
- **Axon Framework samples** — github.com/AxonFramework/AxonFramework. Java, but the aggregate and event patterns are directly transferable.

### Talks

- Eric Evans: "DDD and Microservices: At Last, Some Boundaries" (GOTO 2015) — The talk where Evans explains how DDD Bounded Contexts map to microservice boundaries.
- Vaughn Vernon: "Reactive DDD: Modeling Uncertainty" (Reactive Summit 2016) — How Domain Events connect DDD to reactive/event-driven architectures.
