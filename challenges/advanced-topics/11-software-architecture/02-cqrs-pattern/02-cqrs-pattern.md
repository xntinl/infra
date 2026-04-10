<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [command-query-separation, read-models, write-models, projections, eventual-consistency, independent-scaling]
languages: [go, rust]
estimated_reading_time: 60 min
bloom_level: evaluate
prerequisites: [domain-driven-design, basic-persistence-patterns, go-interfaces, rust-traits]
papers: [Meyer-CQS-1988, Young-CQRS-2010]
industry_use: [Netflix-recommendations, Axon-Framework, Microsoft-eShopOnContainers, EventStore]
language_contrast: medium
-->

# CQRS Pattern

> CQRS recognizes that reading data and changing data are fundamentally different operations that deserve different models — forcing one model to serve both is the root of most scaling and complexity problems in data-heavy systems.

## Mental Model

The write side of your system cares about correctness: did this command violate any business rule? Can we apply this change atomically? The read side cares about shape: give me all orders for customer 42, grouped by status, with product names denormalized, sorted by date. These two concerns are so different that the data structure optimized for one is nearly always wrong for the other.

A normalized relational schema is excellent for writes: no duplication, foreign key integrity, ACID transactions. It is often poor for reads: to show an order summary page you join `orders`, `order_lines`, `products`, `customers`, `addresses`, and `payment_methods`. That join is expensive, and it gets more expensive as the tables grow. The read model you actually want is a single wide document: `{ order_id, customer_name, items: [{product_name, qty, price}], total, status }`. Pre-materializing this document is what CQRS's read model (projection) does.

Command Query Responsibility Segregation separates the model that handles writes (commands) from the model used for reads (queries). The write side remains normalized and consistent; the read side is denormalized and eventually consistent. "Eventually consistent" means: after a write, the read model catches up within some milliseconds or seconds (in practice, usually under 100ms with an async projection). If your domain tolerates this lag — and most domains do — you gain the ability to optimize reads and writes completely independently.

Where CQRS becomes genuinely important is at scale. Netflix's recommendation system reads from a heavily denormalized read model that would be impossible to derive on the fly from a normalized store for hundreds of millions of requests per day. Uber's surge pricing reads from a pre-aggregated view of supply/demand in each cell. These systems use the CQRS insight even if they don't name it that: the shape of data at query time must be pre-computed, not computed on demand.

The hidden cost of CQRS is operational complexity. You now have two models to maintain, a projection mechanism that must be reliable, and a consistency lag that you must design your UI to handle. For a CRUD application, this is pure overhead. For a system with complex read requirements and high read-to-write ratios, it is the right trade.

## Core Concepts

### Commands and Command Handlers

A Command is an intent to change state: `PlaceOrderCommand`, `CancelOrderCommand`, `UpdateShippingAddressCommand`. Commands are rejected or accepted — they may fail if business rules are violated. Command Handlers load aggregates, execute domain methods, and persist results. One handler per command, one command at a time.

### Queries and Query Handlers

A Query is a request for data: `GetOrderByIdQuery`, `ListOrdersByCustomerQuery`. Queries have no side effects — they never change state. Query Handlers read directly from the read model, not from the aggregate store. They return view models (DTOs), not domain objects.

### Projections

A Projection builds and maintains the read model from the write model's output. Synchronous projections update the read model within the same transaction as the write. Asynchronous projections consume domain events (from a message bus or a polling mechanism) and update the read model after the fact. Asynchronous is more scalable but introduces eventual consistency.

### Read Model

The read model is a denormalized store optimized for the queries your application makes. It might be a set of Redis hashes, a set of Elasticsearch documents, or a set of materialized views in PostgreSQL. The crucial point: the read model is disposable. If it becomes corrupted, you rebuild it from scratch by replaying all events. This is the key operational property that makes CQRS safe.

### Eventual Consistency

After a command is processed, the read model is out of date until the projection catches up. For most user-facing operations, the lag is imperceptible (milliseconds to tens of milliseconds). For the case where a user writes and immediately reads, you either tolerate the lag, return the write result directly to the UI, or read from the write model for the requesting user's own writes (the "read-your-writes" guarantee via the write-side store).

## Implementation: Go

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Domain Types (write side) ───────────────────────────────────────────────

type OrderID string
type CustomerID string
type ProductID string

type OrderStatus string

const (
	StatusDraft     OrderStatus = "draft"
	StatusPlaced    OrderStatus = "placed"
	StatusCancelled OrderStatus = "cancelled"
	StatusShipped   OrderStatus = "shipped"
)

type OrderLine struct {
	ProductID   ProductID
	ProductName string
	Quantity    int
	UnitCents   int64
}

// Order is the write-side aggregate.
// It enforces business rules. It is NOT used for reading.
type Order struct {
	ID         OrderID
	CustomerID CustomerID
	Lines      []OrderLine
	Status     OrderStatus
	PlacedAt   time.Time
}

type DomainEvent interface {
	eventType() string
}

type OrderPlacedEvent struct {
	OrderID    OrderID
	CustomerID CustomerID
	Lines      []OrderLine
	PlacedAt   time.Time
}

func (e OrderPlacedEvent) eventType() string { return "OrderPlaced" }

type OrderCancelledEvent struct {
	OrderID     OrderID
	CancelledAt time.Time
}

func (e OrderCancelledEvent) eventType() string { return "OrderCancelled" }

// ─── Write Side: Command and Handlers ───────────────────────────────────────

type PlaceOrderCommand struct {
	OrderID    OrderID
	CustomerID CustomerID
	Lines      []OrderLine
}

type CancelOrderCommand struct {
	OrderID OrderID
}

// OrderWriteRepository is the write-side persistence abstraction.
type OrderWriteRepository interface {
	Save(order *Order) error
	FindByID(id OrderID) (*Order, error)
}

// EventBus publishes domain events after successful persistence.
type EventBus interface {
	Publish(event DomainEvent)
}

// PlaceOrderHandler handles the PlaceOrderCommand.
// It knows nothing about how orders will be read.
type PlaceOrderHandler struct {
	repo OrderWriteRepository
	bus  EventBus
}

func (h *PlaceOrderHandler) Handle(cmd PlaceOrderCommand) error {
	if len(cmd.Lines) == 0 {
		return errors.New("order must have at least one line")
	}
	order := &Order{
		ID:         cmd.OrderID,
		CustomerID: cmd.CustomerID,
		Lines:      cmd.Lines,
		Status:     StatusPlaced,
		PlacedAt:   time.Now(),
	}
	if err := h.repo.Save(order); err != nil {
		return fmt.Errorf("saving order: %w", err)
	}
	h.bus.Publish(OrderPlacedEvent{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		Lines:      order.Lines,
		PlacedAt:   order.PlacedAt,
	})
	return nil
}

// CancelOrderHandler handles the CancelOrderCommand.
type CancelOrderHandler struct {
	repo OrderWriteRepository
	bus  EventBus
}

func (h *CancelOrderHandler) Handle(cmd CancelOrderCommand) error {
	order, err := h.repo.FindByID(cmd.OrderID)
	if err != nil {
		return fmt.Errorf("loading order: %w", err)
	}
	if order.Status == StatusShipped {
		return errors.New("cannot cancel a shipped order")
	}
	if order.Status == StatusCancelled {
		return errors.New("order already cancelled")
	}
	order.Status = StatusCancelled
	if err := h.repo.Save(order); err != nil {
		return fmt.Errorf("saving cancelled order: %w", err)
	}
	h.bus.Publish(OrderCancelledEvent{
		OrderID:     order.ID,
		CancelledAt: time.Now(),
	})
	return nil
}

// ─── Read Models (view models, fully denormalized) ───────────────────────────

// OrderSummary is the read model for list views.
// It contains exactly what the order list UI needs — no joins required.
type OrderSummary struct {
	OrderID      OrderID
	CustomerID   CustomerID
	Status       OrderStatus
	TotalCents   int64
	ItemCount    int
	PlacedAt     time.Time
}

// OrderDetail is the read model for the detail view.
// Everything needed to render the order page is pre-materialized here.
type OrderDetail struct {
	OrderID    OrderID
	CustomerID CustomerID
	Status     OrderStatus
	PlacedAt   time.Time
	Lines      []OrderLineView
	TotalCents int64
}

type OrderLineView struct {
	ProductID   ProductID
	ProductName string
	Quantity    int
	UnitCents   int64
	LineCents   int64
}

// ─── Read Side: Query and Handlers ───────────────────────────────────────────

type GetOrderDetailQuery struct {
	OrderID OrderID
}

type ListOrdersByCustomerQuery struct {
	CustomerID CustomerID
}

// OrderReadRepository provides read-optimized access to the read model.
// It reads from a different store than the write repository.
type OrderReadRepository interface {
	GetDetail(id OrderID) (*OrderDetail, error)
	ListByCustomer(customerID CustomerID) ([]OrderSummary, error)
}

type GetOrderDetailHandler struct {
	readRepo OrderReadRepository
}

func (h *GetOrderDetailHandler) Handle(q GetOrderDetailQuery) (*OrderDetail, error) {
	detail, err := h.readRepo.GetDetail(q.OrderID)
	if err != nil {
		return nil, fmt.Errorf("fetching order detail: %w", err)
	}
	return detail, nil
}

type ListOrdersByCustomerHandler struct {
	readRepo OrderReadRepository
}

func (h *ListOrdersByCustomerHandler) Handle(q ListOrdersByCustomerQuery) ([]OrderSummary, error) {
	return h.readRepo.ListByCustomer(q.CustomerID)
}

// ─── Projection: keeps read model in sync ───────────────────────────────────

// OrderProjection builds and updates the read model from domain events.
// If the read model is lost, replay all events through this projector to rebuild it.
type OrderProjection struct {
	mu      sync.RWMutex
	details map[OrderID]*OrderDetail
}

func NewOrderProjection() *OrderProjection {
	return &OrderProjection{details: make(map[OrderID]*OrderDetail)}
}

// Apply is called for each domain event, in order.
func (p *OrderProjection) Apply(event DomainEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e := event.(type) {
	case OrderPlacedEvent:
		p.applyOrderPlaced(e)
	case OrderCancelledEvent:
		p.applyOrderCancelled(e)
	}
}

func (p *OrderProjection) applyOrderPlaced(e OrderPlacedEvent) {
	var total int64
	lines := make([]OrderLineView, len(e.Lines))
	for i, l := range e.Lines {
		lineCents := l.UnitCents * int64(l.Quantity)
		total += lineCents
		lines[i] = OrderLineView{
			ProductID:   l.ProductID,
			ProductName: l.ProductName,
			Quantity:    l.Quantity,
			UnitCents:   l.UnitCents,
			LineCents:   lineCents,
		}
	}
	p.details[e.OrderID] = &OrderDetail{
		OrderID:    e.OrderID,
		CustomerID: e.CustomerID,
		Status:     StatusPlaced,
		PlacedAt:   e.PlacedAt,
		Lines:      lines,
		TotalCents: total,
	}
}

func (p *OrderProjection) applyOrderCancelled(e OrderCancelledEvent) {
	if detail, ok := p.details[e.OrderID]; ok {
		detail.Status = StatusCancelled
	}
}

// GetDetail implements OrderReadRepository
func (p *OrderProjection) GetDetail(id OrderID) (*OrderDetail, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	detail, ok := p.details[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found in read model", id)
	}
	result := *detail
	return &result, nil
}

// ListByCustomer implements OrderReadRepository
func (p *OrderProjection) ListByCustomer(customerID CustomerID) ([]OrderSummary, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []OrderSummary
	for _, d := range p.details {
		if d.CustomerID == customerID {
			result = append(result, OrderSummary{
				OrderID:    d.OrderID,
				CustomerID: d.CustomerID,
				Status:     d.Status,
				TotalCents: d.TotalCents,
				ItemCount:  len(d.Lines),
				PlacedAt:   d.PlacedAt,
			})
		}
	}
	return result, nil
}

// ─── In-process event bus ────────────────────────────────────────────────────

// SyncEventBus dispatches events synchronously. A real system would use Kafka
// or a similar broker for async dispatch and durability.
type SyncEventBus struct {
	handlers []func(DomainEvent)
}

func (b *SyncEventBus) Subscribe(handler func(DomainEvent)) {
	b.handlers = append(b.handlers, handler)
}

func (b *SyncEventBus) Publish(event DomainEvent) {
	for _, h := range b.handlers {
		h(event)
	}
}

// ─── In-memory write repository ──────────────────────────────────────────────

type InMemoryWriteRepo struct {
	mu     sync.RWMutex
	orders map[OrderID]*Order
}

func NewInMemoryWriteRepo() *InMemoryWriteRepo {
	return &InMemoryWriteRepo{orders: make(map[OrderID]*Order)}
}

func (r *InMemoryWriteRepo) Save(order *Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[order.ID] = order
	return nil
}

func (r *InMemoryWriteRepo) FindByID(id OrderID) (*Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}
	return o, nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	bus := &SyncEventBus{}
	writeRepo := NewInMemoryWriteRepo()
	projection := NewOrderProjection()

	// The projection subscribes to the event bus.
	// In an async system, this would be a separate process consuming from Kafka.
	bus.Subscribe(projection.Apply)

	placeHandler := &PlaceOrderHandler{repo: writeRepo, bus: bus}
	getDetailHandler := &GetOrderDetailHandler{readRepo: projection}
	listHandler := &ListOrdersByCustomerHandler{readRepo: projection}

	err := placeHandler.Handle(PlaceOrderCommand{
		OrderID:    "order-001",
		CustomerID: "customer-42",
		Lines: []OrderLine{
			{ProductID: "prod-1", ProductName: "Laptop", Quantity: 1, UnitCents: 149900},
			{ProductID: "prod-2", ProductName: "Mouse", Quantity: 2, UnitCents: 2999},
		},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Read from the read model (projection), not from the write store
	detail, err := getDetailHandler.Handle(GetOrderDetailQuery{OrderID: "order-001"})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Order %s: status=%s, total=%d cents, %d lines\n",
		detail.OrderID, detail.Status, detail.TotalCents, len(detail.Lines))

	summaries, _ := listHandler.Handle(ListOrdersByCustomerQuery{CustomerID: "customer-42"})
	fmt.Printf("Customer has %d orders\n", len(summaries))
}
```

### Go-specific considerations

Go channels and goroutines map naturally onto async CQRS projections. In production, the projection would run in a goroutine consuming from a channel (or a Kafka consumer group), applying events to the read store. The `sync.RWMutex` in the in-memory projection above is the simplest synchronization — a real read store (Redis, Elasticsearch) handles its own concurrency.

Go interfaces allow the projection to implement both `OrderReadRepository` and an event handler without any ceremony. This is idiomatic Go: small interfaces, multiple implementations. The projection struct satisfies the read repository interface silently — no `implements` declaration needed.

The `SyncEventBus` here is a demonstration stand-in. In production Go CQRS, use a real message broker. The bus abstraction remains valuable: it decouples the command handler from the projection, so the projection can be replaced (e.g., swap in-memory for Redis) without touching the command side.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use chrono::{DateTime, Utc};

// ─── Domain Types ────────────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct OrderId(pub String);

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct CustomerId(pub String);

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct ProductId(pub String);

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderStatus {
    Placed,
    Cancelled,
    Shipped,
}

#[derive(Debug, Clone)]
pub struct OrderLine {
    pub product_id: ProductId,
    pub product_name: String,
    pub quantity: u32,
    pub unit_cents: i64,
}

// ─── Domain Events ───────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub enum DomainEvent {
    OrderPlaced {
        order_id: OrderId,
        customer_id: CustomerId,
        lines: Vec<OrderLine>,
        placed_at: DateTime<Utc>,
    },
    OrderCancelled {
        order_id: OrderId,
        cancelled_at: DateTime<Utc>,
    },
}

// ─── Write Side ──────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct Order {
    pub id: OrderId,
    pub customer_id: CustomerId,
    pub lines: Vec<OrderLine>,
    pub status: OrderStatus,
}

pub trait OrderWriteRepository {
    fn save(&mut self, order: &Order) -> Result<(), String>;
    fn find_by_id(&self, id: &OrderId) -> Result<Order, String>;
}

pub trait EventPublisher {
    fn publish(&self, event: DomainEvent);
}

pub struct PlaceOrderCommand {
    pub order_id: OrderId,
    pub customer_id: CustomerId,
    pub lines: Vec<OrderLine>,
}

pub struct PlaceOrderHandler<R: OrderWriteRepository, P: EventPublisher> {
    repo: R,
    publisher: P,
}

impl<R: OrderWriteRepository, P: EventPublisher> PlaceOrderHandler<R, P> {
    pub fn new(repo: R, publisher: P) -> Self {
        PlaceOrderHandler { repo, publisher }
    }

    pub fn handle(&mut self, cmd: PlaceOrderCommand) -> Result<(), String> {
        if cmd.lines.is_empty() {
            return Err("order must have at least one line".to_string());
        }
        let order = Order {
            id: cmd.order_id.clone(),
            customer_id: cmd.customer_id.clone(),
            lines: cmd.lines.clone(),
            status: OrderStatus::Placed,
        };
        self.repo.save(&order)?;
        self.publisher.publish(DomainEvent::OrderPlaced {
            order_id: cmd.order_id,
            customer_id: cmd.customer_id,
            lines: cmd.lines,
            placed_at: Utc::now(),
        });
        Ok(())
    }
}

// ─── Read Models ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct OrderDetail {
    pub order_id: OrderId,
    pub customer_id: CustomerId,
    pub status: OrderStatus,
    pub placed_at: DateTime<Utc>,
    pub lines: Vec<OrderLineView>,
    pub total_cents: i64,
}

#[derive(Debug, Clone)]
pub struct OrderLineView {
    pub product_id: ProductId,
    pub product_name: String,
    pub quantity: u32,
    pub unit_cents: i64,
    pub line_cents: i64,
}

#[derive(Debug, Clone)]
pub struct OrderSummary {
    pub order_id: OrderId,
    pub customer_id: CustomerId,
    pub status: OrderStatus,
    pub total_cents: i64,
    pub item_count: usize,
}

// ─── Read Repository Trait ───────────────────────────────────────────────────

pub trait OrderReadRepository {
    fn get_detail(&self, id: &OrderId) -> Result<OrderDetail, String>;
    fn list_by_customer(&self, customer_id: &CustomerId) -> Vec<OrderSummary>;
}

// ─── Projection ──────────────────────────────────────────────────────────────

/// OrderProjection maintains the read model.
/// Arc<RwLock<...>> allows sharing across threads: multiple readers or one writer.
#[derive(Clone)]
pub struct OrderProjection {
    details: Arc<RwLock<HashMap<String, OrderDetail>>>,
}

impl OrderProjection {
    pub fn new() -> Self {
        OrderProjection {
            details: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn apply(&self, event: DomainEvent) {
        match event {
            DomainEvent::OrderPlaced { order_id, customer_id, lines, placed_at } => {
                let mut total_cents: i64 = 0;
                let line_views: Vec<OrderLineView> = lines
                    .iter()
                    .map(|l| {
                        let line_cents = l.unit_cents * l.quantity as i64;
                        total_cents += line_cents;
                        OrderLineView {
                            product_id: l.product_id.clone(),
                            product_name: l.product_name.clone(),
                            quantity: l.quantity,
                            unit_cents: l.unit_cents,
                            line_cents,
                        }
                    })
                    .collect();

                let detail = OrderDetail {
                    order_id: order_id.clone(),
                    customer_id,
                    status: OrderStatus::Placed,
                    placed_at,
                    lines: line_views,
                    total_cents,
                };
                self.details.write().unwrap().insert(order_id.0, detail);
            }
            DomainEvent::OrderCancelled { order_id, .. } => {
                if let Some(detail) = self.details.write().unwrap().get_mut(&order_id.0) {
                    detail.status = OrderStatus::Cancelled;
                }
            }
        }
    }
}

impl OrderReadRepository for OrderProjection {
    fn get_detail(&self, id: &OrderId) -> Result<OrderDetail, String> {
        self.details
            .read()
            .unwrap()
            .get(&id.0)
            .cloned()
            .ok_or_else(|| format!("order {} not found", id.0))
    }

    fn list_by_customer(&self, customer_id: &CustomerId) -> Vec<OrderSummary> {
        self.details
            .read()
            .unwrap()
            .values()
            .filter(|d| d.customer_id == *customer_id)
            .map(|d| OrderSummary {
                order_id: d.order_id.clone(),
                customer_id: d.customer_id.clone(),
                status: d.status.clone(),
                total_cents: d.total_cents,
                item_count: d.lines.len(),
            })
            .collect()
    }
}

// ─── Query Handlers ──────────────────────────────────────────────────────────

pub struct GetOrderDetailQuery {
    pub order_id: OrderId,
}

pub struct GetOrderDetailHandler<R: OrderReadRepository> {
    read_repo: R,
}

impl<R: OrderReadRepository> GetOrderDetailHandler<R> {
    pub fn handle(&self, query: GetOrderDetailQuery) -> Result<OrderDetail, String> {
        self.read_repo.get_detail(&query.order_id)
    }
}

// ─── Infrastructure ──────────────────────────────────────────────────────────

pub struct InMemoryWriteRepo {
    orders: HashMap<String, Order>,
}

impl InMemoryWriteRepo {
    pub fn new() -> Self { InMemoryWriteRepo { orders: HashMap::new() } }
}

impl OrderWriteRepository for InMemoryWriteRepo {
    fn save(&mut self, order: &Order) -> Result<(), String> {
        self.orders.insert(order.id.0.clone(), order.clone());
        Ok(())
    }
    fn find_by_id(&self, id: &OrderId) -> Result<Order, String> {
        self.orders
            .get(&id.0)
            .cloned()
            .ok_or_else(|| format!("order {} not found", id.0))
    }
}

pub struct SyncPublisher {
    projection: OrderProjection,
}

impl EventPublisher for SyncPublisher {
    fn publish(&self, event: DomainEvent) {
        self.projection.apply(event);
    }
}

fn main() {
    let projection = OrderProjection::new();
    let publisher = SyncPublisher { projection: projection.clone() };
    let write_repo = InMemoryWriteRepo::new();

    let mut place_handler = PlaceOrderHandler::new(write_repo, publisher);

    place_handler.handle(PlaceOrderCommand {
        order_id: OrderId("order-001".into()),
        customer_id: CustomerId("customer-42".into()),
        lines: vec![
            OrderLine {
                product_id: ProductId("prod-1".into()),
                product_name: "Laptop".to_string(),
                quantity: 1,
                unit_cents: 149900,
            },
            OrderLine {
                product_id: ProductId("prod-2".into()),
                product_name: "Mouse".to_string(),
                quantity: 2,
                unit_cents: 2999,
            },
        ],
    }).unwrap();

    let detail_handler = GetOrderDetailHandler { read_repo: projection };
    let detail = detail_handler.handle(GetOrderDetailQuery {
        order_id: OrderId("order-001".into()),
    }).unwrap();

    println!("Order {}: status={:?}, total={} cents, {} lines",
        detail.order_id.0, detail.status, detail.total_cents, detail.lines.len());
}
```

### Rust-specific considerations

`Arc<RwLock<HashMap<...>>>` is the idiomatic Rust approach for a shared, mutable read model that is read from multiple threads. The `Arc` allows sharing ownership across the projection and the query handler; the `RwLock` allows concurrent reads (which dominate in a read-heavy system) while serializing writes.

Rust's enum exhaustiveness makes the projection's `match event { ... }` safe: adding a new event variant forces every match to handle it, including the projection. This prevents the common projection bug where a new event type is added to the system but the projection silently ignores it, causing the read model to diverge from the write model.

The `Clone` on `OrderProjection` is shallow — it clones the `Arc`, not the underlying data. This is Rust's reference-counted smart pointer doing what it is designed for: sharing access to the same projection state across command handlers and query handlers without copying.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Read/write separation | Two interfaces, one for each side | Two traits, compiler enforces they are used correctly |
| Projection concurrency | `sync.RWMutex` on projection struct | `Arc<RwLock<T>>` shared via clone |
| New event handling | Silent miss if `switch` is incomplete | Compile error if `match` is non-exhaustive |
| Query handler wiring | Interface passed in constructor | Trait generic, monomorphized at compile time |
| Boilerplate | Light; interfaces are implicit | Heavier; explicit `impl Trait for Type` for each type |
| Async projection | Goroutine + channel | Tokio task + `mpsc::channel` |
| Rebuilding projections | Iterate event store, call `projection.Apply` | Iterate event store, call `projection.apply` |

## Production War Stories

**Netflix recommendation system**: Netflix's recommendation engine is one of the most cited CQRS examples at scale. The write side records user interactions (views, ratings, pauses). The read side is a heavily denormalized store of pre-computed recommendations, updated by offline Spark jobs and near-real-time streaming projections. Querying recommendations never touches the interaction store — it reads from a pre-materialized document per user. At hundreds of millions of users, the separation between the write model (interactions) and the read model (recommendations) is what makes the system feasible.

**Axon Framework's adoption at bol.com (Dutch e-commerce)**: bol.com adopted Axon Framework (Java CQRS + Event Sourcing) for their order management system. They reported that the ability to rebuild read models from the event store allowed them to add a new reporting view without touching any existing code — they wrote a new projector that replayed the event history and materialized a new read model. This is the operational benefit of CQRS that is difficult to quantify until you experience it.

**LinkedIn's feed system**: LinkedIn's news feed is built on the CQRS insight: writes (posting, liking, commenting) go to one system; reads (rendering the feed) come from a pre-assembled feed store built by a fan-out service. A direct query at read time would require joining hundreds of tables across services. The pre-materialized feed is the read model; the fan-out service is the projection.

## Architectural Trade-offs

**When to use CQRS:**
- Read-to-write ratio is heavily skewed toward reads (>10:1)
- Read models require data from multiple aggregates or services, making live joins expensive
- Read and write scalability requirements differ significantly (need more read replicas than write capacity)
- Audit or compliance requirements benefit from an event log (pairs naturally with Event Sourcing)
- Need to add new read views without changing the write model

**When NOT to use CQRS:**
- Simple CRUD systems where the write and read shapes are identical
- Small teams who will struggle with the operational complexity of two data stores and a projection mechanism
- Systems where read-your-writes consistency is hard to give up (e.g., financial UIs where the user sees an immediate confirmation and then reads back the exact same data)
- Domains where eventual consistency is unacceptable (medical record systems where a clinician needs immediate, authoritative reads after writes)

**The consistency lag problem**: In a synchronous CQRS system (projection runs in the same transaction as the write), you get strong consistency but reduced write throughput. In an asynchronous system, you get high write throughput but must design the UI for eventual consistency. The most common architectural mistake is building an async CQRS system and then trying to fake synchronous consistency everywhere — you get the costs of both without the benefits of either.

## Common Pitfalls

**1. Putting business logic in the projection.** The projection's job is to reshape data, not to enforce rules. If you find yourself writing `if order.Status == "placed" && order.Total > 1000 { applyDiscount() }` in a projector, that logic belongs in the command handler or domain layer.

**2. Using the write repository for reads.** The entire point of CQRS is separate models. If a query handler calls `writeRepo.FindByID()` and then assembles a view model from the aggregate, you have the complexity of two models but the performance of one. Read from the read model.

**3. Not making projections idempotent.** Async projections will receive the same event more than once (at-least-once delivery is the practical default for message brokers). If applying the same `OrderPlaced` event twice creates two entries in the read model, your read model becomes inconsistent. Projections must be idempotent: applying the same event twice produces the same result as applying it once.

**4. Not building a projection replay mechanism before you need it.** The safety net of CQRS is "if the read model is wrong, replay from the event store." Teams that don't build this mechanism upfront discover they need it urgently when a projector bug corrupts the read model in production.

**5. Conflating CQRS with a specific technology.** CQRS is a pattern about model separation. You do not need Kafka, Event Sourcing, or a CQRS framework to implement it. A PostgreSQL write table and a PostgreSQL materialized view is CQRS. Start simple and add components only when the simple version bottlenecks.

## Exercises

**Exercise 1** (30 min): Trace through the Go implementation. Add a `GetOrdersByStatusQuery` that returns all orders in a given status. Add the corresponding read model method and query handler, and add the status index to the projection.

**Exercise 2** (2–4h): Implement an asynchronous projection using a goroutine and a buffered channel. The write handler publishes to the channel; the projection runs in a goroutine consuming from the channel. Add a method to wait for the projection to catch up (useful for tests).

**Exercise 3** (4–8h): Add a `CustomerOrderStats` read model that tracks, per customer: total orders, total spend, and date of last order. Build a projection that maintains this model from the same events. Verify that rebuilding it from scratch by replaying all events produces the same result as incremental updates.

**Exercise 4** (8–15h): Implement a persistent read model using Redis (or any real database). The projection persists read models to Redis, and query handlers read from Redis. Implement projection replay from the event store. Add a health check that detects when the projection falls behind and reports lag.

## Further Reading

### Foundational Books

- **Domain-Driven Design** — Evans. CQRS is an evolution of CQS (Bertrand Meyer, Object-Oriented Software Construction). Understanding the lineage clarifies the intent.
- **Implementing Domain-Driven Design** — Vernon. Chapter on CQRS is the clearest written treatment.

### Blog Posts and Case Studies

- Greg Young: "CQRS and Event Sourcing" (2010) — cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf. The document that named and popularized CQRS.
- Martin Fowler: "CQRS" — martinfowler.com/bliki/CQRS.html. A measured perspective on when to use it and the costs.
- Udi Dahan: "Clarified CQRS" — udidahan.com. Udi's clarification on the common misconceptions about CQRS requiring Event Sourcing.

### Production Code to Read

- **eShopOnContainers ordering microservice** — github.com/dotnet-architecture/eShopOnContainers. The `Ordering.API` project shows CQRS with MediatR: separate command and query handlers, separate read models.
- **Axon Framework** — github.com/AxonFramework/AxonFramework. Java but the patterns are language-agnostic.

### Talks

- Greg Young: "CQRS, Not Just for Wizards" (NDC 2012) — Available on YouTube. Greg explains why CQRS is simpler than the hype suggests.
- Udi Dahan: "If (domain logic) then CQRS or Saga?" — The decision framework for when each pattern applies.
