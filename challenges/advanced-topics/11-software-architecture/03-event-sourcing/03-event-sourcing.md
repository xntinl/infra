<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [event-log, aggregate-rehydration, snapshots, event-versioning, upcasting, temporal-queries, event-store]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: evaluate
prerequisites: [domain-driven-design, cqrs-pattern, go-interfaces, rust-traits, basic-persistence-patterns]
papers: [Fowler-EventSourcing-2005, Young-CQRS-2010]
industry_use: [EventStoreDB, Axon-Framework, Temporal, LinkedIn-Kafka-event-store, Martin-Fowler-bliki]
language_contrast: medium
-->

# Event Sourcing

> Instead of storing the current state of an object, store the sequence of events that produced it — current state is always a projection of the history.

## Mental Model

Every database system you have ever used stores the current state and throws away the history. A user updates their email address and the old address is gone. An order is fulfilled and the intermediate states — draft, confirmed, payment pending — are gone. An account balance changes and the transaction that changed it is recorded elsewhere, if at all. This is so normal that most engineers never question it.

Event Sourcing inverts this. The event log is the source of truth. The current state is a derived value — you reconstruct it by replaying events from the beginning. "What is this order's current status?" becomes "replay all events for this order and apply them in sequence." The database stores the sequence of facts that happened, not the current snapshot of what exists.

The immediate, practical consequence is that you never lose history. You cannot have an "undo" feature in a mutable system without building a separate changelog. You cannot answer "what did this aggregate look like last Tuesday?" without time-travel backups. You cannot audit "who changed this field and when?" without a separate audit table. Event Sourcing gives you all of this for free, because the history is the system.

The less obvious consequence is that the event log enables temporal decoupling. A new feature that needs to backfill historical data — "show me all orders placed during a promotional campaign that ran last year" — can replay the event store from any point in time. A new read model (CQRS projection) can be built by replaying the entire event history. This means adding a new view of historical data requires writing a new projector and replaying, not migrating a database.

The cost is real. Reconstructing aggregate state by replaying events from the beginning is O(n) in the number of events. For aggregates with long histories (an account with five years of daily transactions), this is too slow. Snapshots solve this: periodically capture the aggregate's current state as a snapshot; replay only the events after the most recent snapshot. Snapshots are an optimization, not a core part of the pattern — start without them and add them when profiling shows they are needed.

Event versioning is the other hidden cost. Events are stored permanently. When the business logic changes and you need an `OrderPlaced` event to carry a new field, old events in the store don't have that field. You handle this with upcasting: when reading an old event, a transformation layer adds default values or computes the new field from existing data. This is manageable but requires deliberate discipline from the first event you ever store.

## Core Concepts

### Event Store

An append-only log, partitioned by aggregate identity. You write new events for an aggregate stream (`append(orderId, events)`); you read all events for an aggregate stream (`load(orderId) -> []Event`). Optimistic concurrency control is built in: when appending, you specify the expected version (last event number) and the store rejects the write if another writer has appended since you read. This prevents lost updates without database-level locks.

### Aggregate Rehydration

To use an aggregate, load all its events from the store and "apply" them in sequence to rebuild the aggregate's state. The aggregate starts in an initial (empty) state and each event mutates that state. The apply logic is pure: same events, same final state, always.

### Snapshots

A snapshot is a serialized representation of the aggregate's state at a specific event version. When loading, first check for a snapshot; if one exists, start from its state and apply only the events after its version. This keeps rehydration O(1) + O(events since last snapshot) instead of O(total events).

Snapshots are independent of the event log — you can delete all snapshots and rebuild them from the event log without losing data.

### Event Versioning and Upcasting

Events are immutable once written. When the schema changes (new required field, renamed field, split event), you version events: `OrderPlacedV1`, `OrderPlacedV2`. Upcasting is the process of converting an old event version to the current version when loading from the store. An upcaster for `OrderPlacedV1 -> V2` adds the new field with a computed or default value.

### Optimistic Concurrency Control

When appending events for an aggregate, you specify the version you expect the stream to be at. If the stream has been modified since you loaded it, the append fails. The application service catches this conflict and retries (re-load the aggregate, re-apply the command, re-try the append). This is the Event Sourcing equivalent of an optimistic lock.

## Implementation: Go

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Event Store ─────────────────────────────────────────────────────────────

// StoredEvent is the envelope that the event store persists.
// The payload is the actual domain event; everything else is infrastructure metadata.
type StoredEvent struct {
	StreamID  string
	Version   int64
	EventType string
	Payload   interface{}
	StoredAt  time.Time
}

// ErrVersionConflict is returned when an optimistic concurrency conflict occurs.
var ErrVersionConflict = errors.New("version conflict: stream modified since last read")

// EventStore is an append-only log partitioned by stream ID (aggregate ID).
type EventStore interface {
	Append(streamID string, expectedVersion int64, events []interface{}) error
	Load(streamID string, fromVersion int64) ([]StoredEvent, error)
	// LoadSnapshot returns the most recent snapshot at or before maxVersion.
	LoadSnapshot(streamID string) (*Snapshot, error)
	SaveSnapshot(snap Snapshot) error
}

// Snapshot captures aggregate state at a specific event version.
type Snapshot struct {
	StreamID string
	Version  int64
	State    interface{}
	SavedAt  time.Time
}

// InMemoryEventStore implements EventStore for testing and development.
// A production store would be EventStoreDB, PostgreSQL (with an events table),
// or DynamoDB with a stream-per-aggregate design.
type InMemoryEventStore struct {
	mu        sync.RWMutex
	streams   map[string][]StoredEvent
	snapshots map[string]*Snapshot
}

func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		streams:   make(map[string][]StoredEvent),
		snapshots: make(map[string]*Snapshot),
	}
}

func (s *InMemoryEventStore) Append(streamID string, expectedVersion int64, events []interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream := s.streams[streamID]
	currentVersion := int64(len(stream))

	// Optimistic concurrency check: reject if stream was modified since we read it
	if currentVersion != expectedVersion {
		return fmt.Errorf("%w: expected version %d, got %d",
			ErrVersionConflict, expectedVersion, currentVersion)
	}

	for i, payload := range events {
		stored := StoredEvent{
			StreamID:  streamID,
			Version:   currentVersion + int64(i) + 1,
			EventType: fmt.Sprintf("%T", payload),
			Payload:   payload,
			StoredAt:  time.Now(),
		}
		s.streams[streamID] = append(s.streams[streamID], stored)
	}
	return nil
}

func (s *InMemoryEventStore) Load(streamID string, fromVersion int64) ([]StoredEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream := s.streams[streamID]
	if int64(len(stream)) < fromVersion {
		return nil, fmt.Errorf("stream %q has %d events, requested from %d",
			streamID, len(stream), fromVersion)
	}
	return stream[fromVersion:], nil
}

func (s *InMemoryEventStore) LoadSnapshot(streamID string) (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snapshots[streamID]
	return snap, nil
}

func (s *InMemoryEventStore) SaveSnapshot(snap Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snap.StreamID] = &snap
	return nil
}

// ─── Domain Events (versioned) ───────────────────────────────────────────────

// Events are versioned from the start.
// V1 is the initial version. When the schema changes, add V2 and an upcaster.
type OrderPlacedV1 struct {
	OrderID    string
	CustomerID string
	TotalCents int64
	PlacedAt   time.Time
}

type ItemAdded struct {
	ProductID string
	Quantity  int
	UnitCents int64
}

type OrderCancelled struct {
	Reason      string
	CancelledAt time.Time
}

type OrderShipped struct {
	TrackingNumber string
	ShippedAt      time.Time
}

// ─── Upcaster ────────────────────────────────────────────────────────────────

// Upcasting is the process of converting an old event format to the current format.
// Here: if OrderPlacedV1 lacks a currency field (added in V2), the upcaster fills it in.
// In production, upcasters are applied when loading events from the store.
type OrderPlacedV2 struct {
	OrderID    string
	CustomerID string
	TotalCents int64
	Currency   string // new field in V2
	PlacedAt   time.Time
}

func upcastOrderPlacedV1(v1 OrderPlacedV1) OrderPlacedV2 {
	return OrderPlacedV2{
		OrderID:    v1.OrderID,
		CustomerID: v1.CustomerID,
		TotalCents: v1.TotalCents,
		Currency:   "USD", // default for all historical events
		PlacedAt:   v1.PlacedAt,
	}
}

// ─── Aggregate ───────────────────────────────────────────────────────────────

type OrderAggStatus string

const (
	OrderAggEmpty     OrderAggStatus = ""
	OrderAggPlaced    OrderAggStatus = "placed"
	OrderAggShipped   OrderAggStatus = "shipped"
	OrderAggCancelled OrderAggStatus = "cancelled"
)

// OrderAggState is the state rebuilt from events.
// It is separate from the aggregate root to make snapshot serialization clean.
type OrderAggState struct {
	ID         string
	CustomerID string
	TotalCents int64
	Currency   string
	Status     OrderAggStatus
	Items      []ItemAdded
	Version    int64 // the event version this state corresponds to
}

// OrderAggregate is the aggregate root.
// It holds state rebuilt from events and collects new events to append.
type OrderAggregate struct {
	state             OrderAggState
	uncommittedEvents []interface{} // new events not yet persisted
}

// Apply updates the aggregate state for a single event.
// Apply is PURE: no side effects, no external calls. Same event → same state change.
func (a *OrderAggregate) Apply(event interface{}) {
	switch e := event.(type) {
	case OrderPlacedV2:
		a.state.ID = e.OrderID
		a.state.CustomerID = e.CustomerID
		a.state.TotalCents = e.TotalCents
		a.state.Currency = e.Currency
		a.state.Status = OrderAggPlaced
	case OrderPlacedV1:
		// Upcast before applying — the aggregate only knows V2
		a.Apply(upcastOrderPlacedV1(e))
		return
	case ItemAdded:
		a.state.Items = append(a.state.Items, e)
		a.state.TotalCents += e.UnitCents * int64(e.Quantity)
	case OrderCancelled:
		a.state.Status = OrderAggCancelled
	case OrderShipped:
		a.state.Status = OrderAggShipped
	}
	a.state.Version++
}

// Command methods validate business rules before recording events.
// They never persist — they append to uncommittedEvents.
func (a *OrderAggregate) Place(orderID, customerID string) error {
	if a.state.Status != OrderAggEmpty {
		return fmt.Errorf("order already exists with status %q", a.state.Status)
	}
	event := OrderPlacedV2{
		OrderID:    orderID,
		CustomerID: customerID,
		TotalCents: 0,
		Currency:   "USD",
		PlacedAt:   time.Now(),
	}
	a.record(event)
	return nil
}

func (a *OrderAggregate) AddItem(productID string, quantity int, unitCents int64) error {
	if a.state.Status != OrderAggPlaced {
		return fmt.Errorf("can only add items to a placed order, status: %q", a.state.Status)
	}
	a.record(ItemAdded{ProductID: productID, Quantity: quantity, UnitCents: unitCents})
	return nil
}

func (a *OrderAggregate) Cancel(reason string) error {
	switch a.state.Status {
	case OrderAggShipped:
		return errors.New("cannot cancel a shipped order")
	case OrderAggCancelled:
		return errors.New("order already cancelled")
	case OrderAggEmpty:
		return errors.New("order does not exist")
	}
	a.record(OrderCancelled{Reason: reason, CancelledAt: time.Now()})
	return nil
}

func (a *OrderAggregate) record(event interface{}) {
	a.uncommittedEvents = append(a.uncommittedEvents, event)
	a.Apply(event) // apply immediately so state reflects the new event
}

func (a *OrderAggregate) FlushEvents() []interface{} {
	events := a.uncommittedEvents
	a.uncommittedEvents = nil
	return events
}

func (a *OrderAggregate) State() OrderAggState   { return a.state }
func (a *OrderAggregate) Version() int64         { return a.state.Version }

// ─── Repository (with snapshot support) ──────────────────────────────────────

// SnapshotThreshold controls how often snapshots are taken.
// After this many events since the last snapshot, a new snapshot is saved.
const SnapshotThreshold = 50

type OrderEventSourcedRepository struct {
	store EventStore
}

func NewOrderEventSourcedRepository(store EventStore) *OrderEventSourcedRepository {
	return &OrderEventSourcedRepository{store: store}
}

// Load rehydrates an aggregate from the event store.
// It checks for a snapshot first to avoid full replay.
func (r *OrderEventSourcedRepository) Load(orderID string) (*OrderAggregate, error) {
	agg := &OrderAggregate{}
	fromVersion := int64(0)

	snap, err := r.store.LoadSnapshot(orderID)
	if err != nil {
		return nil, fmt.Errorf("loading snapshot: %w", err)
	}
	if snap != nil {
		// Restore state from snapshot; replay only events after snapshot version
		agg.state = snap.State.(OrderAggState)
		fromVersion = snap.Version
	}

	events, err := r.store.Load(orderID, fromVersion)
	if err != nil {
		return nil, fmt.Errorf("loading events for order %q: %w", orderID, err)
	}
	for _, e := range events {
		agg.Apply(e.Payload)
	}
	return agg, nil
}

// Save appends uncommitted events and optionally saves a snapshot.
func (r *OrderEventSourcedRepository) Save(agg *OrderAggregate) error {
	events := agg.FlushEvents()
	if len(events) == 0 {
		return nil
	}
	// expectedVersion is state.Version - len(events) because Apply was called
	// as events were recorded, advancing the version in-memory.
	expectedVersion := agg.state.Version - int64(len(events))

	if err := r.store.Append(agg.state.ID, expectedVersion, events); err != nil {
		return fmt.Errorf("appending events: %w", err)
	}

	// Save a snapshot if we have crossed the threshold since the last one.
	snap, _ := r.store.LoadSnapshot(agg.state.ID)
	lastSnapVersion := int64(0)
	if snap != nil {
		lastSnapVersion = snap.Version
	}
	if agg.state.Version-lastSnapVersion >= SnapshotThreshold {
		return r.store.SaveSnapshot(Snapshot{
			StreamID: agg.state.ID,
			Version:  agg.state.Version,
			State:    agg.state,
			SavedAt:  time.Now(),
		})
	}
	return nil
}

// ─── Application Service ─────────────────────────────────────────────────────

type PlaceOrderCmd struct {
	OrderID    string
	CustomerID string
}

type AddItemCmd struct {
	OrderID   string
	ProductID string
	Quantity  int
	UnitCents int64
}

type OrderCommandService struct {
	repo *OrderEventSourcedRepository
}

func NewOrderCommandService(repo *OrderEventSourcedRepository) *OrderCommandService {
	return &OrderCommandService{repo: repo}
}

func (svc *OrderCommandService) PlaceOrder(cmd PlaceOrderCmd) error {
	agg := &OrderAggregate{}
	if err := agg.Place(cmd.OrderID, cmd.CustomerID); err != nil {
		return err
	}
	return svc.repo.Save(agg)
}

func (svc *OrderCommandService) AddItem(cmd AddItemCmd) error {
	agg, err := svc.repo.Load(cmd.OrderID)
	if err != nil {
		return err
	}
	if err := agg.AddItem(cmd.ProductID, cmd.Quantity, cmd.UnitCents); err != nil {
		return err
	}
	return svc.repo.Save(agg)
}

func (svc *OrderCommandService) CancelOrder(orderID, reason string) error {
	agg, err := svc.repo.Load(orderID)
	if err != nil {
		return err
	}
	if err := agg.Cancel(reason); err != nil {
		return err
	}
	return svc.repo.Save(agg)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	store := NewInMemoryEventStore()
	repo := NewOrderEventSourcedRepository(store)
	svc := NewOrderCommandService(repo)

	if err := svc.PlaceOrder(PlaceOrderCmd{OrderID: "order-001", CustomerID: "customer-42"}); err != nil {
		fmt.Printf("Error placing order: %v\n", err)
		return
	}

	if err := svc.AddItem(AddItemCmd{OrderID: "order-001", ProductID: "prod-laptop", Quantity: 1, UnitCents: 149900}); err != nil {
		fmt.Printf("Error adding item: %v\n", err)
		return
	}

	// Reload from event store to demonstrate rehydration
	agg, err := repo.Load("order-001")
	if err != nil {
		fmt.Printf("Error loading order: %v\n", err)
		return
	}
	state := agg.State()
	fmt.Printf("Order %s: status=%s, total=%d cents, %d item(s), version=%d\n",
		state.ID, state.Status, state.TotalCents, len(state.Items), state.Version)

	// Demonstrate temporal query: what was the state after the first event?
	eventsAfterFirst, _ := store.Load("order-001", 1)
	replayAgg := &OrderAggregate{}
	replayAgg.Apply(store.streams["order-001"][0].Payload) // only the first event
	_ = eventsAfterFirst
	fmt.Printf("State after first event: status=%s\n", replayAgg.State().Status)

	// Demonstrate optimistic concurrency conflict
	agg1, _ := repo.Load("order-001")
	agg2, _ := repo.Load("order-001")
	agg1.AddItem("prod-2", 1, 2999)
	agg2.AddItem("prod-3", 1, 4999)
	svc.repo.Save(agg1) // succeeds
	if err := svc.repo.Save(agg2); err != nil {
		fmt.Printf("Expected conflict: %v\n", err)
	}
}
```

### Go-specific considerations

Go's interface-based polymorphism works well for the event store abstraction. The `InMemoryEventStore` is the development implementation; a PostgreSQL event store or EventStoreDB client would implement the same interface and be swapped at the composition root (main function or dependency injection container).

The `Apply` method's type switch on `interface{}` is Go's way of handling a discriminated union. It works but has no compile-time exhaustiveness. Adding a new event type and forgetting to add a case in `Apply` compiles and silently produces incorrect state. Compensate with tests that apply every known event type and verify the resulting state.

Go's lack of generics (in older versions) made event sourcing repositories require a lot of `interface{}` casting. With generics (Go 1.18+), you can write `EventSourcedRepository[T Aggregate]` — the pattern becomes cleaner but is not yet idiomatic in most production codebases.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

// ─── Event Store ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct StoredEvent {
    pub stream_id: String,
    pub version: u64,
    pub event_type: String,
    pub payload: OrderEvent,
    pub stored_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum EventStoreError {
    VersionConflict { expected: u64, actual: u64 },
    StreamNotFound(String),
}

impl std::fmt::Display for EventStoreError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            EventStoreError::VersionConflict { expected, actual } => {
                write!(f, "version conflict: expected {expected}, actual {actual}")
            }
            EventStoreError::StreamNotFound(id) => write!(f, "stream {id} not found"),
        }
    }
}

#[derive(Clone)]
pub struct InMemoryEventStore {
    streams: Arc<RwLock<HashMap<String, Vec<StoredEvent>>>>,
    snapshots: Arc<RwLock<HashMap<String, Snapshot>>>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Snapshot {
    pub stream_id: String,
    pub version: u64,
    pub state: OrderState,
    pub saved_at: DateTime<Utc>,
}

impl InMemoryEventStore {
    pub fn new() -> Self {
        InMemoryEventStore {
            streams: Arc::new(RwLock::new(HashMap::new())),
            snapshots: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn append(
        &self,
        stream_id: &str,
        expected_version: u64,
        events: Vec<OrderEvent>,
    ) -> Result<(), EventStoreError> {
        let mut streams = self.streams.write().unwrap();
        let stream = streams.entry(stream_id.to_string()).or_default();
        let current_version = stream.len() as u64;

        if current_version != expected_version {
            return Err(EventStoreError::VersionConflict {
                expected: expected_version,
                actual: current_version,
            });
        }

        for (i, event) in events.into_iter().enumerate() {
            let version = current_version + i as u64 + 1;
            stream.push(StoredEvent {
                stream_id: stream_id.to_string(),
                version,
                event_type: event.event_type_name().to_string(),
                payload: event,
                stored_at: Utc::now(),
            });
        }
        Ok(())
    }

    pub fn load(&self, stream_id: &str, from_version: u64) -> Vec<StoredEvent> {
        let streams = self.streams.read().unwrap();
        streams
            .get(stream_id)
            .map(|s| s[from_version as usize..].to_vec())
            .unwrap_or_default()
    }

    pub fn load_snapshot(&self, stream_id: &str) -> Option<Snapshot> {
        self.snapshots.read().unwrap().get(stream_id).cloned()
    }

    pub fn save_snapshot(&self, snapshot: Snapshot) {
        self.snapshots.write().unwrap().insert(snapshot.stream_id.clone(), snapshot);
    }
}

// ─── Domain Events ───────────────────────────────────────────────────────────

/// Rust enums give us a closed, exhaustive set of event types.
/// The compiler rejects any match that does not handle all variants.
/// This makes event versioning explicit: OrderPlacedV2 is a new variant.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum OrderEvent {
    OrderPlacedV1 {
        order_id: String,
        customer_id: String,
        total_cents: i64,
        placed_at: DateTime<Utc>,
    },
    OrderPlacedV2 {
        order_id: String,
        customer_id: String,
        total_cents: i64,
        currency: String,
        placed_at: DateTime<Utc>,
    },
    ItemAdded {
        product_id: String,
        quantity: u32,
        unit_cents: i64,
    },
    OrderCancelled {
        reason: String,
        cancelled_at: DateTime<Utc>,
    },
    OrderShipped {
        tracking_number: String,
        shipped_at: DateTime<Utc>,
    },
}

impl OrderEvent {
    pub fn event_type_name(&self) -> &'static str {
        match self {
            OrderEvent::OrderPlacedV1 { .. } => "OrderPlacedV1",
            OrderEvent::OrderPlacedV2 { .. } => "OrderPlacedV2",
            OrderEvent::ItemAdded { .. } => "ItemAdded",
            OrderEvent::OrderCancelled { .. } => "OrderCancelled",
            OrderEvent::OrderShipped { .. } => "OrderShipped",
        }
    }

    /// Upcast converts old event versions to the current version.
    /// Called transparently when loading from the event store.
    pub fn upcast(self) -> OrderEvent {
        match self {
            OrderEvent::OrderPlacedV1 {
                order_id,
                customer_id,
                total_cents,
                placed_at,
            } => OrderEvent::OrderPlacedV2 {
                order_id,
                customer_id,
                total_cents,
                currency: "USD".to_string(), // default for all V1 events
                placed_at,
            },
            other => other,
        }
    }
}

// ─── Aggregate State ─────────────────────────────────────────────────────────

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct OrderState {
    pub id: String,
    pub customer_id: String,
    pub status: OrderStatusAgg,
    pub total_cents: i64,
    pub currency: String,
    pub item_count: u32,
    pub version: u64,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum OrderStatusAgg {
    #[default]
    New,
    Placed,
    Cancelled,
    Shipped,
}

// ─── Aggregate Root ──────────────────────────────────────────────────────────

pub struct OrderAggregate {
    state: OrderState,
    uncommitted: Vec<OrderEvent>,
}

#[derive(Debug)]
pub enum OrderDomainError {
    AlreadyExists,
    NotPlaced,
    AlreadyCancelled,
    AlreadyShipped,
    InvalidQuantity,
}

impl std::fmt::Display for OrderDomainError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            OrderDomainError::AlreadyExists => write!(f, "order already exists"),
            OrderDomainError::NotPlaced => write!(f, "order is not in placed state"),
            OrderDomainError::AlreadyCancelled => write!(f, "order already cancelled"),
            OrderDomainError::AlreadyShipped => write!(f, "order already shipped"),
            OrderDomainError::InvalidQuantity => write!(f, "quantity must be greater than zero"),
        }
    }
}

impl OrderAggregate {
    pub fn new() -> Self {
        OrderAggregate {
            state: OrderState::default(),
            uncommitted: Vec::new(),
        }
    }

    /// Apply is deterministic and pure: same events always produce same state.
    pub fn apply(&mut self, event: OrderEvent) {
        // Upcast old event versions before applying
        let event = event.upcast();
        match event {
            OrderEvent::OrderPlacedV2 { ref order_id, ref customer_id, ref currency, .. } => {
                self.state.id = order_id.clone();
                self.state.customer_id = customer_id.clone();
                self.state.currency = currency.clone();
                self.state.status = OrderStatusAgg::Placed;
            }
            OrderEvent::ItemAdded { unit_cents, quantity, .. } => {
                self.state.total_cents += unit_cents * quantity as i64;
                self.state.item_count += quantity;
            }
            OrderEvent::OrderCancelled { .. } => {
                self.state.status = OrderStatusAgg::Cancelled;
            }
            OrderEvent::OrderShipped { .. } => {
                self.state.status = OrderStatusAgg::Shipped;
            }
            OrderEvent::OrderPlacedV1 { .. } => unreachable!("V1 events are upcasted before apply"),
        }
        self.state.version += 1;
    }

    fn record(&mut self, event: OrderEvent) {
        self.apply(event.clone());
        self.uncommitted.push(event);
    }

    pub fn place(&mut self, order_id: String, customer_id: String) -> Result<(), OrderDomainError> {
        if self.state.status != OrderStatusAgg::New {
            return Err(OrderDomainError::AlreadyExists);
        }
        self.record(OrderEvent::OrderPlacedV2 {
            order_id,
            customer_id,
            total_cents: 0,
            currency: "USD".to_string(),
            placed_at: Utc::now(),
        });
        Ok(())
    }

    pub fn add_item(
        &mut self,
        product_id: String,
        quantity: u32,
        unit_cents: i64,
    ) -> Result<(), OrderDomainError> {
        if self.state.status != OrderStatusAgg::Placed {
            return Err(OrderDomainError::NotPlaced);
        }
        if quantity == 0 {
            return Err(OrderDomainError::InvalidQuantity);
        }
        self.record(OrderEvent::ItemAdded { product_id, quantity, unit_cents });
        Ok(())
    }

    pub fn cancel(&mut self, reason: String) -> Result<(), OrderDomainError> {
        match self.state.status {
            OrderStatusAgg::Shipped => return Err(OrderDomainError::AlreadyShipped),
            OrderStatusAgg::Cancelled => return Err(OrderDomainError::AlreadyCancelled),
            _ => {}
        }
        self.record(OrderEvent::OrderCancelled { reason, cancelled_at: Utc::now() });
        Ok(())
    }

    pub fn flush(&mut self) -> Vec<OrderEvent> {
        std::mem::take(&mut self.uncommitted)
    }

    pub fn state(&self) -> &OrderState { &self.state }
    pub fn version(&self) -> u64 { self.state.version }
}

// ─── Repository ──────────────────────────────────────────────────────────────

const SNAPSHOT_THRESHOLD: u64 = 50;

pub struct OrderEventRepository {
    store: InMemoryEventStore,
}

impl OrderEventRepository {
    pub fn new(store: InMemoryEventStore) -> Self {
        OrderEventRepository { store }
    }

    pub fn load(&self, order_id: &str) -> Result<OrderAggregate, EventStoreError> {
        let mut agg = OrderAggregate::new();
        let mut from_version = 0u64;

        if let Some(snap) = self.store.load_snapshot(order_id) {
            agg.state = snap.state;
            from_version = snap.version;
        }

        let events = self.store.load(order_id, from_version);
        for stored in events {
            agg.apply(stored.payload);
        }
        Ok(agg)
    }

    pub fn save(&self, agg: &mut OrderAggregate) -> Result<(), EventStoreError> {
        let events = agg.flush();
        if events.is_empty() {
            return Ok(());
        }
        let expected_version = agg.state.version - events.len() as u64;
        self.store.append(&agg.state.id, expected_version, events)?;

        let last_snap_version = self.store
            .load_snapshot(&agg.state.id)
            .map(|s| s.version)
            .unwrap_or(0);

        if agg.state.version - last_snap_version >= SNAPSHOT_THRESHOLD {
            self.store.save_snapshot(Snapshot {
                stream_id: agg.state.id.clone(),
                version: agg.state.version,
                state: agg.state.clone(),
                saved_at: Utc::now(),
            });
        }
        Ok(())
    }
}

fn main() {
    let store = InMemoryEventStore::new();
    let repo = OrderEventRepository::new(store);

    let mut agg = OrderAggregate::new();
    agg.place("order-001".into(), "customer-42".into()).unwrap();
    agg.add_item("prod-laptop".into(), 1, 149900).unwrap();
    repo.save(&mut agg).unwrap();

    // Reload from event store — demonstrates rehydration
    let loaded = repo.load("order-001").unwrap();
    println!(
        "Order {}: status={:?}, total={} cents, version={}",
        loaded.state().id,
        loaded.state().status,
        loaded.state().total_cents,
        loaded.version()
    );
}
```

### Rust-specific considerations

Rust's enum with `#[derive(Serialize, Deserialize)]` from Serde is the natural fit for versioned domain events. The event store can serialize events to JSON using `serde_json` and deserialize them with proper versioning. The upcasting step transforms old enum variants to current ones before the aggregate's `apply` method sees them.

The `unreachable!()` macro on `OrderPlacedV1` after upcasting is a Rust convention for "this path should never be reached after our upcast transformation." If upcasting is correctly implemented, this branch is dead code, but Rust's exhaustiveness requires you to handle it. The `unreachable!` makes the assumption explicit and panics loudly in tests if the assumption breaks.

Rust's ownership model strengthens the event sourcing pattern: `flush()` uses `std::mem::take` to drain the uncommitted events, taking ownership of the vector and replacing it with an empty one. The caller receives owned events, and the aggregate's uncommitted list is empty. This is cleaner than Go's equivalent, which returns a slice and sets the field to nil.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Event type safety | `interface{}` type switch; non-exhaustive | `enum` match; compiler-enforced exhaustiveness |
| Upcasting | Function called in `Apply` switch case | `upcast()` method on the enum; called before `apply` |
| Snapshot serialization | Manual interface-based | `serde::Serialize/Deserialize` derive |
| Optimistic concurrency | `int64` version comparison | `u64` version comparison; same pattern |
| Event store abstraction | Interface with multiple implementations | Trait or direct struct (in-memory is concrete here) |
| Aggregate state copying | Struct field copy | `Clone` derive on `OrderState` |
| Flush pattern | Slice return + nil reset | `std::mem::take` |

## Production War Stories

**Martin Fowler's original article (2005)**: Fowler named and described Event Sourcing in a bliki post that is still the canonical introduction. He cited banking as the domain where this pattern was already in use: a bank account's balance is computed from the transaction log, not stored directly. The insight was that this pattern, well-known in accounting, could be applied broadly to software.

**Greg Young and EventStoreDB**: Greg Young's 2010 CQRS document established Event Sourcing as a first-class architectural pattern paired with CQRS. He later built EventStoreDB, a purpose-built event store with projections, subscriptions, and optimistic concurrency built in. EventStoreDB is now used in production at Walmart, Lego, and numerous fintech companies.

**LinkedIn's Brooklin and Kafka as event stores**: LinkedIn uses Kafka not just as a message bus but as the source of truth for activity data. The "social graph" activity stream (connections, likes, follows) is stored as an ordered event log in Kafka. Downstream projections materialize different views (notifications, feed, analytics) from the same event stream. This is Event Sourcing at massive scale, even if LinkedIn does not always use that label.

**Uber's Cadence (now Temporal)**: Uber built Cadence, and it was later open-sourced as Temporal. Temporal's workflow engine is built on Event Sourcing: a workflow's state is the replay of its event history. When a workflow worker crashes and restarts, it rehydrates the workflow state from its event log and continues executing. This is the operational safety property of Event Sourcing in a distributed system.

**The post-mortem that prompted a fintech to adopt Event Sourcing**: A recurring pattern in fintech post-mortems involves a mutable balance table where a bug applied a debit twice. Because the table stored only current state, the incident could not be fully reconstructed — when did the duplicate happen? Which transaction? What was the state before? The investigation took weeks. After adopting Event Sourcing, the same class of bug would be immediately visible in the event log.

## Architectural Trade-offs

**When to use Event Sourcing:**
- Full audit log is a hard requirement (financial services, healthcare, legal)
- Need to replay history for new read models or analytics (often discovered mid-project)
- Domain has natural event semantics (order placed, payment captured, item shipped)
- Already using CQRS and the write model has no natural "current state" to query
- Debugging production issues requires time-travel (what was the state at 3am Tuesday?)

**When NOT to use Event Sourcing:**
- Simple CRUD without meaningful history (user profile settings, configuration tables)
- Team is not yet comfortable with eventual consistency — Event Sourcing makes consistency more explicit, not easier
- Domain events are poorly defined — if you cannot name your events as past-tense business facts, your domain model is not ready
- Short-lived aggregates with no audit requirement — event-sourcing a shopping cart session is overkill
- Reporting-heavy systems where the read model queries are unknown in advance and change frequently

**The upcasting maintenance cost**: As a system evolves over years, the number of event versions grows. An active system might have `OrderPlacedV1` through `OrderPlacedV6`. Each version needs an upcaster chain (V1→V2→V3... or V1→current, V2→current). This is manageable but requires rigorous discipline. Teams that "just change the event schema" without versioning break the event log.

## Common Pitfalls

**1. Storing too much in events, or too little.** Events should capture what happened in business terms, not technical operations. `UserUpdated { fields: { email: "new@example.com" } }` is too generic. `EmailAddressChanged { new_email: "new@example.com" }` names the business intent. At the other extreme, events that only record an ID and require re-loading the aggregate to understand the change are useless for projections.

**2. Using Event Sourcing for all aggregates in a system.** Not every aggregate benefits from Event Sourcing. A `ShippingAddress` lookup table has no interesting history. Apply Event Sourcing selectively to aggregates where history matters. A mixed persistence system (event-sourced for orders, conventional for reference data) is not only acceptable — it is usually the right design.

**3. Skipping event versioning until it hurts.** The pain of upcasting is proportional to how many incompatible schema changes accumulated before versioning was introduced. Start with versioning on day one. `OrderPlacedV1` is a commitment that V2 will exist when needed.

**4. Treating snapshots as optional and then suffering.** An aggregate with 10,000 events takes 10,000 apply operations to rehydrate. In production, this shows up as latency spikes under load. Profile early and add snapshots before your aggregate histories grow long.

**5. Not building an event migration path.** When a critical bug is discovered in a projector, the read model is wrong. The fix requires replaying all events through the corrected projector. If your system cannot replay 100 million events in a reasonable time (hours, not days), the Event Sourcing safety net is ineffective. Build and test replay before you need it in an incident.

## Exercises

**Exercise 1** (30 min): Trace through the Go implementation. Add an `OrderShipped` command to the `OrderAggregate`. Verify the invariant: only a `placed` order can be shipped, and shipping it transitions the status.

**Exercise 2** (2–4h): Implement a simple projection that reads from the `InMemoryEventStore` and builds a read model of all orders and their current statuses. Verify that replaying the full event history from scratch produces the same read model as incremental updates.

**Exercise 3** (4–8h): Implement the snapshot threshold in a way that works with a PostgreSQL-backed event store. Design the schema: events table (stream_id, version, event_type, payload JSON, stored_at) and snapshots table (stream_id, version, state JSON, saved_at). Implement the repository against this schema.

**Exercise 4** (8–15h): Implement event versioning end-to-end. Introduce a second version of `OrderPlaced` that adds a `currency` field. Write an upcaster. Seed the in-memory store with V1 events, then demonstrate that loading an aggregate with mixed V1 and V2 events produces the correct state. Write a stress test that creates aggregates with 1,000 events and measures the latency improvement from snapshots at various thresholds (10, 50, 100).

## Further Reading

### Foundational Books

- **Domain-Driven Design** — Evans. Chapter on domain events is the conceptual foundation.
- **Designing Data-Intensive Applications** — Kleppmann. Chapter 11 covers event streams and the append-only log as a fundamental primitive. Not DDD-specific but essential background.

### Blog Posts and Case Studies

- Martin Fowler: "Event Sourcing" — martinfowler.com/eaaDev/EventSourcing.html. The original naming.
- Greg Young: "CQRS and Event Sourcing" (2010) — cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf. Most detailed treatment.
- EventStoreDB documentation: "Event Sourcing Basics" — eventstore.com/blog/event-sourcing-basics.

### Production Code to Read

- **EventStoreDB** — github.com/EventStore/EventStore. The purpose-built event store. Read the client SDK to understand the append/read API.
- **Temporal** — github.com/temporalio/temporal. Temporal's workflow history is Event Sourcing in a distributed runtime.
- **eShopOnContainers** — Ordering microservice, specifically the `Infrastructure/EventSourcing` directory.

### Talks

- Greg Young: "CQRS and Event Sourcing" (GOTO 2014) — The canonical long-form talk. Clear on why Event Sourcing exists, what it costs, and when not to use it.
- Martin Fowler: "The Many Meanings of Event-Driven Architecture" (GOTO 2017) — Fowler distinguishes Event Sourcing from event notification and event-carried state transfer.
