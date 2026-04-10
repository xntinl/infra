<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [event-bus, outbox-pattern, exactly-once-semantics, at-least-once-delivery, idempotent-consumers, event-schema-evolution, kafka-partitions, consumer-groups]
languages: [go, rust]
estimated_reading_time: 65 min
bloom_level: evaluate
prerequisites: [domain-driven-design, cqrs-pattern, event-sourcing, distributed-systems-basics]
papers: [Kleppmann-DDIA-2017, Fowler-EventDrivenArchitecture-2017]
industry_use: [Apache-Kafka, AWS-SNS-SQS, Google-Pub-Sub, RabbitMQ, NATS]
language_contrast: low
-->

# Event-Driven Architecture

> Services that communicate through events decouple in time: the publisher does not know when or whether a subscriber processes the event, and the subscriber does not know whether the publisher is running.

## Mental Model

When service A calls service B synchronously, A's availability is tied to B's: if B is down, A's request fails. If B is slow, A's request is slow. If B needs to be redeployed, A either waits or handles the interruption. This is temporal coupling — the two services must be available at the same time for communication to succeed.

Event-driven architecture breaks temporal coupling. Service A publishes an event to a broker (Kafka, RabbitMQ, SNS); service B subscribes to that event and processes it whenever it runs. If B is down when A publishes, the event waits in the broker until B recovers. If B is slow, the event queues up. Services are decoupled in time: they can be redeployed, scaled, or fail independently without affecting each other's correctness.

The hidden complexity is delivery guarantees. A message broker can guarantee at-most-once (fast, unreliable), at-least-once (reliable, may deliver duplicates), or exactly-once (reliable, no duplicates, expensive). In practice, exactly-once is rarely available end-to-end and requires both the broker and the consumer to participate in a transaction. The practical default is at-least-once: the broker guarantees the event will be delivered, possibly more than once. Consumers must therefore be idempotent.

The outbox pattern solves a specific, critical problem: "how do I publish an event and save a database record atomically?" If you save to the database and then publish to Kafka, what happens if the application crashes between the two? The database is updated but the event is never published — services that depend on the event never receive it. The outbox pattern: save both the record and the event to the database in the same transaction, in an `outbox` table. A separate process (the relay) polls the outbox table and publishes events to the broker, marking them as published. Now the event publication is durably committed before the relay tries to publish it. The relay can crash and restart safely — it will find unpublished events in the outbox and publish them.

Event schema evolution is the long-term maintenance challenge. An event published today may be consumed months later by a subscriber that was not updated. Consumers must be forward-compatible: they must handle events with additional fields they don't recognize (by ignoring them) and events with missing optional fields (by using defaults). This requires explicit schema discipline: backward and forward compatibility, versioned schemas, and a schema registry (Confluent Schema Registry, AWS Glue) to prevent incompatible changes.

## Core Concepts

### Topics, Partitions, and Consumer Groups

A Kafka topic is a log, partitioned across brokers. Each partition is ordered. A consumer group reads from all partitions — each partition is assigned to exactly one consumer in the group at a time. Scaling consumers in a group scales read throughput up to the number of partitions. This is the fundamental scaling unit: if you need 10x throughput, create 10 partitions and run 10 consumers.

Event ordering is guaranteed within a partition, not across partitions. If event order matters for a set of events (all events for one order must be processed in order), use the entity ID as the partition key — all events for the same entity go to the same partition.

### Outbox Pattern

The outbox pattern ensures atomic publication of database writes and events. Steps:
1. Begin a database transaction.
2. Write the domain state change (order record, payment record).
3. Write the event to an `outbox` table in the same database.
4. Commit the transaction.
5. A relay process (or Debezium CDC) reads unpublished events from the outbox and publishes them to the broker.
6. The relay marks published events as processed.

The relay provides at-least-once delivery: if it publishes and then crashes before marking as processed, it will publish the same event again on restart. Consumers must be idempotent.

### Idempotent Consumers

A consumer is idempotent if processing the same event twice produces the same result as processing it once. Implementation strategies: (a) store processed event IDs in a deduplication table and skip duplicates, (b) use database unique constraints (inserting with the event ID as the key is idempotent — the second insert fails with a constraint violation, which you catch and ignore), (c) check-then-act patterns with optimistic concurrency.

### Event Schema Evolution

Schemas must be backward-compatible (old consumers can read new events) and forward-compatible (new consumers can read old events). Practical rules: (a) new fields must be optional with defaults, (b) field types cannot change in incompatible ways, (c) fields cannot be removed without a deprecation period, (d) event types cannot be renamed — add a new type and deprecate the old one.

Avro and Protobuf are the dominant schema formats because they enforce these rules explicitly. JSON requires a schema registry (Confluent Schema Registry) and discipline.

### Consumer Lag

The difference between the latest event in a partition and the last event a consumer group has processed. High consumer lag indicates that consumers cannot keep up with producers. Monitor lag as a primary health metric for event-driven systems.

## Implementation: Go

```go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Event Types ──────────────────────────────────────────────────────────────

// EventEnvelope wraps domain events for the broker.
// Schema versioning starts here: EventType + Version identify the schema.
type EventEnvelope struct {
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	SchemaVersion int           `json:"schema_version"`
	AggregateID string          `json:"aggregate_id"`
	Payload     json.RawMessage `json:"payload"`
	PublishedAt time.Time       `json:"published_at"`
}

// OrderPlacedPayload is the current (V2) schema.
// V1 did not have Currency. All V1 events are upcasted by the consumer.
type OrderPlacedPayloadV2 struct {
	OrderID    string `json:"order_id"`
	CustomerID string `json:"customer_id"`
	TotalCents int64  `json:"total_cents"`
	Currency   string `json:"currency"` // new in V2
}

// ─── Outbox ───────────────────────────────────────────────────────────────────

// OutboxEvent is the record persisted to the `outbox` table.
type OutboxEvent struct {
	ID          int64
	EventID     string
	EventType   string
	Payload     []byte
	PublishedAt *time.Time // nil = not yet published
}

// OutboxRepository persists events to the outbox table.
// In production, this runs within the same transaction as the domain write.
type OutboxRepository interface {
	InsertEvent(ctx context.Context, event OutboxEvent) error
	FetchUnpublished(ctx context.Context, limit int) ([]OutboxEvent, error)
	MarkPublished(ctx context.Context, ids []int64) error
}

// OrderApplicationService demonstrates the outbox pattern.
// It saves the domain change AND the event in the same transaction.
type OrderApplicationService struct {
	db     *sql.DB // for the transaction
	outbox OutboxRepository
}

func (svc *OrderApplicationService) PlaceOrder(ctx context.Context, orderID, customerID string, totalCents int64) error {
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 1: Write the domain state change
	_, err = tx.ExecContext(ctx,
		"INSERT INTO orders (id, customer_id, total_cents, status) VALUES ($1, $2, $3, 'placed')",
		orderID, customerID, totalCents,
	)
	if err != nil {
		return fmt.Errorf("inserting order: %w", err)
	}

	// Step 2: Write the event to the outbox — SAME transaction
	payload, _ := json.Marshal(OrderPlacedPayloadV2{
		OrderID:    orderID,
		CustomerID: customerID,
		TotalCents: totalCents,
		Currency:   "USD",
	})
	envelope := EventEnvelope{
		EventID:       fmt.Sprintf("evt-%s", orderID),
		EventType:     "OrderPlaced",
		SchemaVersion: 2,
		AggregateID:   orderID,
		Payload:       payload,
		PublishedAt:   time.Now(),
	}
	envelopeBytes, _ := json.Marshal(envelope)

	if err := svc.outbox.InsertEvent(ctx, OutboxEvent{
		EventID:   envelope.EventID,
		EventType: envelope.EventType,
		Payload:   envelopeBytes,
	}); err != nil {
		return fmt.Errorf("inserting outbox event: %w", err)
	}

	// Both writes committed atomically — event publication is guaranteed
	return tx.Commit()
}

// ─── Outbox Relay ─────────────────────────────────────────────────────────────

// EventBroker is the abstraction over Kafka/RabbitMQ/NATS.
type EventBroker interface {
	Publish(ctx context.Context, topic string, event []byte) error
}

// OutboxRelay polls the outbox table and publishes unpublished events.
// Runs as a background goroutine. Safe to restart: it will find and republish.
type OutboxRelay struct {
	outbox   OutboxRepository
	broker   EventBroker
	interval time.Duration
	batchSize int
}

func NewOutboxRelay(outbox OutboxRepository, broker EventBroker) *OutboxRelay {
	return &OutboxRelay{
		outbox:    outbox,
		broker:    broker,
		interval:  500 * time.Millisecond,
		batchSize: 100,
	}
}

func (r *OutboxRelay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := r.processOnce(ctx); err != nil {
				fmt.Printf("[OutboxRelay] error: %v\n", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *OutboxRelay) processOnce(ctx context.Context) error {
	events, err := r.outbox.FetchUnpublished(ctx, r.batchSize)
	if err != nil {
		return fmt.Errorf("fetching unpublished events: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	var published []int64
	for _, evt := range events {
		var envelope EventEnvelope
		if err := json.Unmarshal(evt.Payload, &envelope); err != nil {
			fmt.Printf("[OutboxRelay] skipping malformed event %d: %v\n", evt.ID, err)
			continue
		}
		topic := "orders." + envelope.EventType

		if err := r.broker.Publish(ctx, topic, evt.Payload); err != nil {
			// Publication failed — do not mark as published.
			// Will retry on next polling cycle.
			fmt.Printf("[OutboxRelay] publish failed for event %d: %v\n", evt.ID, err)
			break // stop processing this batch to maintain order
		}
		published = append(published, evt.ID)
	}

	if len(published) > 0 {
		return r.outbox.MarkPublished(ctx, published)
	}
	return nil
}

// ─── Idempotent Consumer ─────────────────────────────────────────────────────

// DeduplicationStore tracks processed event IDs.
// In production: a Redis SET or a PostgreSQL table with a unique index on event_id.
type DeduplicationStore struct {
	mu      sync.RWMutex
	seen    map[string]time.Time
	ttl     time.Duration // evict after TTL to prevent unbounded growth
}

func NewDeduplicationStore(ttl time.Duration) *DeduplicationStore {
	return &DeduplicationStore{seen: make(map[string]time.Time), ttl: ttl}
}

// MarkSeen returns true if the event was already processed, false if it's new.
// If false, the caller should process the event — it is now marked as seen.
func (s *DeduplicationStore) MarkSeen(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.seen[eventID]; exists {
		return true // duplicate
	}
	s.seen[eventID] = time.Now()
	return false
}

// Evict removes expired entries (called periodically in production).
func (s *DeduplicationStore) Evict() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-s.ttl)
	for k, v := range s.seen {
		if v.Before(cutoff) {
			delete(s.seen, k)
		}
	}
}

// OrderEventConsumer processes order events idempotently.
type OrderEventConsumer struct {
	dedup *DeduplicationStore
}

func NewOrderEventConsumer(dedup *DeduplicationStore) *OrderEventConsumer {
	return &OrderEventConsumer{dedup: dedup}
}

func (c *OrderEventConsumer) Handle(rawEvent []byte) error {
	var envelope EventEnvelope
	if err := json.Unmarshal(rawEvent, &envelope); err != nil {
		return fmt.Errorf("unmarshalling envelope: %w", err)
	}

	// Idempotency check: skip if already processed
	if c.dedup.MarkSeen(envelope.EventID) {
		fmt.Printf("[Consumer] Skipping duplicate event %s\n", envelope.EventID)
		return nil
	}

	// Schema evolution: upcast old versions
	switch envelope.EventType {
	case "OrderPlaced":
		return c.handleOrderPlaced(envelope)
	default:
		// Unknown event type — forward compatibility: ignore gracefully
		fmt.Printf("[Consumer] Unknown event type %q — skipping\n", envelope.EventType)
		return nil
	}
}

func (c *OrderEventConsumer) handleOrderPlaced(envelope EventEnvelope) error {
	// Handle schema versioning — upcast V1 to V2
	var payload OrderPlacedPayloadV2
	if envelope.SchemaVersion == 1 {
		// V1 payload has no Currency field
		var v1 struct {
			OrderID    string `json:"order_id"`
			CustomerID string `json:"customer_id"`
			TotalCents int64  `json:"total_cents"`
		}
		if err := json.Unmarshal(envelope.Payload, &v1); err != nil {
			return fmt.Errorf("unmarshalling V1 payload: %w", err)
		}
		// Upcast: apply business default for missing field
		payload = OrderPlacedPayloadV2{
			OrderID:    v1.OrderID,
			CustomerID: v1.CustomerID,
			TotalCents: v1.TotalCents,
			Currency:   "USD", // default for all V1 events
		}
	} else {
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return fmt.Errorf("unmarshalling V2 payload: %w", err)
		}
	}

	fmt.Printf("[Consumer] Processing OrderPlaced: order=%s customer=%s total=%d %s\n",
		payload.OrderID, payload.CustomerID, payload.TotalCents, payload.Currency)
	return nil
}

// ─── In-Memory Infrastructure (for demo) ─────────────────────────────────────

type InMemoryOutboxRepository struct {
	mu     sync.Mutex
	events []OutboxEvent
	nextID int64
}

func (r *InMemoryOutboxRepository) InsertEvent(_ context.Context, event OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	event.ID = r.nextID
	r.events = append(r.events, event)
	return nil
}

func (r *InMemoryOutboxRepository) FetchUnpublished(_ context.Context, limit int) ([]OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []OutboxEvent
	for _, e := range r.events {
		if e.PublishedAt == nil {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *InMemoryOutboxRepository) MarkPublished(_ context.Context, ids []int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	idSet := make(map[int64]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	now := time.Now()
	for i := range r.events {
		if idSet[r.events[i].ID] {
			r.events[i].PublishedAt = &now
		}
	}
	return nil
}

// InMemoryBroker delivers events to subscribers synchronously.
// A real broker (Kafka, NATS) would be asynchronous and durable.
type InMemoryBroker struct {
	mu          sync.RWMutex
	subscribers map[string][]func([]byte)
}

func NewInMemoryBroker() *InMemoryBroker {
	return &InMemoryBroker{subscribers: make(map[string][]func([]byte))}
}

func (b *InMemoryBroker) Subscribe(topic string, handler func([]byte)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[topic] = append(b.subscribers[topic], handler)
}

func (b *InMemoryBroker) Publish(_ context.Context, topic string, event []byte) error {
	b.mu.RLock()
	handlers := b.subscribers[topic]
	b.mu.RUnlock()
	for _, h := range handlers {
		h(event)
	}
	return nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	outboxRepo := &InMemoryOutboxRepository{}
	broker := NewInMemoryBroker()
	dedup := NewDeduplicationStore(24 * time.Hour)
	consumer := NewOrderEventConsumer(dedup)

	// Consumer subscribes to the orders.OrderPlaced topic
	broker.Subscribe("orders.OrderPlaced", consumer.Handle)

	// Application service uses the outbox — no direct DB for demo, we simulate
	relay := NewOutboxRelay(outboxRepo, broker)

	// ── Simulate writing two orders with events ───────────────────────────
	fmt.Println("=== Outbox Pattern Demo ===")

	// Manually insert events (in production: done inside DB transaction by app service)
	for i := 1; i <= 3; i++ {
		orderID := fmt.Sprintf("order-%03d", i)
		payload, _ := json.Marshal(OrderPlacedPayloadV2{
			OrderID:    orderID,
			CustomerID: fmt.Sprintf("customer-%d", i*10),
			TotalCents: int64(i * 1000),
			Currency:   "USD",
		})
		envelope := EventEnvelope{
			EventID:       fmt.Sprintf("evt-%s", orderID),
			EventType:     "OrderPlaced",
			SchemaVersion: 2,
			AggregateID:   orderID,
			Payload:       payload,
			PublishedAt:   time.Now(),
		}
		envelopeBytes, _ := json.Marshal(envelope)
		outboxRepo.InsertEvent(context.Background(), OutboxEvent{
			EventID:   envelope.EventID,
			EventType: envelope.EventType,
			Payload:   envelopeBytes,
		})
		fmt.Printf("Order %s written to outbox\n", orderID)
	}

	// Relay processes the outbox and publishes to broker
	fmt.Println("\n--- Relay processing outbox ---")
	relay.processOnce(context.Background())

	// ── Idempotency: deliver the same events again ─────────────────────────
	fmt.Println("\n=== Idempotency Demo (duplicate delivery) ===")
	// Re-deliver the same events — consumer should skip them
	relay.processOnce(context.Background())

	// Actually simulate delivering duplicates to the consumer directly
	dupPayload, _ := json.Marshal(OrderPlacedPayloadV2{
		OrderID: "order-001", CustomerID: "customer-10", TotalCents: 1000, Currency: "USD",
	})
	dupEnvelope := EventEnvelope{
		EventID:       "evt-order-001", // same event ID as before
		EventType:     "OrderPlaced",
		SchemaVersion: 2,
		AggregateID:   "order-001",
		Payload:       dupPayload,
	}
	dupBytes, _ := json.Marshal(dupEnvelope)
	consumer.Handle(dupBytes) // should be skipped as duplicate

	// ── Schema evolution: V1 event consumed by V2 consumer ────────────────
	fmt.Println("\n=== Schema Evolution Demo (V1 → V2 upcasting) ===")
	v1Payload, _ := json.Marshal(map[string]interface{}{
		"order_id":    "order-legacy-001",
		"customer_id": "customer-old",
		"total_cents": 5000,
		// no currency field — V1 schema
	})
	v1Envelope := EventEnvelope{
		EventID:       "evt-v1-legacy",
		EventType:     "OrderPlaced",
		SchemaVersion: 1, // old version
		AggregateID:   "order-legacy-001",
		Payload:       v1Payload,
	}
	v1Bytes, _ := json.Marshal(v1Envelope)
	consumer.Handle(v1Bytes)

	_ = errors.New("") // silence import
}
```

### Go-specific considerations

Go's `database/sql` transaction model maps directly onto the outbox pattern: `db.BeginTx`, two `ExecContext` calls (one for the domain record, one for the outbox), `tx.Commit`. If the commit fails, both writes are rolled back — no orphaned outbox entries, no domain writes without corresponding events.

The `OutboxRelay` runs in a goroutine; cancelling the context cleanly shuts it down. The relay's polling loop is simple and testable: `processOnce` can be called directly in tests without starting a ticker. This testability-first design is what the hexagonal architecture section advocates for all application components.

JSON with `json.RawMessage` allows the event envelope to carry versioned payloads without knowing the payload schema at the envelope level. The consumer unmarshals the payload after checking the schema version. In production, use Avro or Protobuf with a schema registry for stronger guarantees.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use serde::{Deserialize, Serialize};
use tokio::sync::mpsc;
use tokio::time::{interval, sleep};

// ─── Event Schemas ────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EventEnvelope {
    pub event_id: String,
    pub event_type: String,
    pub schema_version: u32,
    pub aggregate_id: String,
    pub payload: serde_json::Value,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct OrderPlacedV2 {
    pub order_id: String,
    pub customer_id: String,
    pub total_cents: i64,
    pub currency: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct OrderPlacedV1 {
    pub order_id: String,
    pub customer_id: String,
    pub total_cents: i64,
    // no currency
}

impl From<OrderPlacedV1> for OrderPlacedV2 {
    fn from(v1: OrderPlacedV1) -> Self {
        OrderPlacedV2 {
            order_id: v1.order_id,
            customer_id: v1.customer_id,
            total_cents: v1.total_cents,
            currency: "USD".to_string(),
        }
    }
}

// ─── Outbox ───────────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct OutboxRecord {
    pub id: u64,
    pub event_id: String,
    pub event_type: String,
    pub payload: Vec<u8>,
    pub published: bool,
}

pub struct InMemoryOutbox {
    records: Arc<Mutex<Vec<OutboxRecord>>>,
    next_id: Arc<Mutex<u64>>,
}

impl InMemoryOutbox {
    pub fn new() -> Arc<Self> {
        Arc::new(InMemoryOutbox {
            records: Arc::new(Mutex::new(Vec::new())),
            next_id: Arc::new(Mutex::new(0)),
        })
    }

    pub fn insert(&self, event_id: &str, event_type: &str, payload: Vec<u8>) {
        let mut id_lock = self.next_id.lock().unwrap();
        *id_lock += 1;
        let id = *id_lock;
        drop(id_lock);

        self.records.lock().unwrap().push(OutboxRecord {
            id,
            event_id: event_id.to_string(),
            event_type: event_type.to_string(),
            payload,
            published: false,
        });
    }

    pub fn fetch_unpublished(&self, limit: usize) -> Vec<OutboxRecord> {
        self.records
            .lock()
            .unwrap()
            .iter()
            .filter(|r| !r.published)
            .take(limit)
            .cloned()
            .collect()
    }

    pub fn mark_published(&self, ids: &[u64]) {
        let id_set: std::collections::HashSet<u64> = ids.iter().copied().collect();
        for record in self.records.lock().unwrap().iter_mut() {
            if id_set.contains(&record.id) {
                record.published = true;
            }
        }
    }
}

// ─── Event Broker ────────────────────────────────────────────────────────────

pub struct InMemoryBroker {
    senders: Arc<Mutex<HashMap<String, Vec<mpsc::UnboundedSender<Vec<u8>>>>>>,
}

impl InMemoryBroker {
    pub fn new() -> Arc<Self> {
        Arc::new(InMemoryBroker {
            senders: Arc::new(Mutex::new(HashMap::new())),
        })
    }

    pub fn subscribe(&self, topic: &str) -> mpsc::UnboundedReceiver<Vec<u8>> {
        let (tx, rx) = mpsc::unbounded_channel();
        self.senders
            .lock()
            .unwrap()
            .entry(topic.to_string())
            .or_default()
            .push(tx);
        rx
    }

    pub fn publish(&self, topic: &str, payload: Vec<u8>) -> Result<(), String> {
        let senders = self.senders.lock().unwrap();
        if let Some(txs) = senders.get(topic) {
            for tx in txs {
                tx.send(payload.clone())
                    .map_err(|e| format!("send error: {e}"))?;
            }
        }
        Ok(())
    }
}

// ─── Outbox Relay ────────────────────────────────────────────────────────────

pub struct OutboxRelay {
    outbox: Arc<InMemoryOutbox>,
    broker: Arc<InMemoryBroker>,
}

impl OutboxRelay {
    pub fn new(outbox: Arc<InMemoryOutbox>, broker: Arc<InMemoryBroker>) -> Self {
        OutboxRelay { outbox, broker }
    }

    pub async fn run(self, mut shutdown: mpsc::Receiver<()>) {
        let mut ticker = interval(Duration::from_millis(500));
        loop {
            tokio::select! {
                _ = ticker.tick() => {
                    if let Err(e) = self.process_once() {
                        eprintln!("[Relay] error: {e}");
                    }
                }
                _ = shutdown.recv() => {
                    println!("[Relay] shutting down");
                    return;
                }
            }
        }
    }

    fn process_once(&self) -> Result<(), String> {
        let events = self.outbox.fetch_unpublished(100);
        if events.is_empty() {
            return Ok(());
        }

        let mut published_ids = Vec::new();
        for event in &events {
            let envelope: EventEnvelope = serde_json::from_slice(&event.payload)
                .map_err(|e| format!("deserialize error: {e}"))?;
            let topic = format!("orders.{}", envelope.event_type);
            self.broker.publish(&topic, event.payload.clone())?;
            published_ids.push(event.id);
        }
        self.outbox.mark_published(&published_ids);
        Ok(())
    }
}

// ─── Idempotent Consumer ─────────────────────────────────────────────────────

pub struct DeduplicationStore {
    seen: Mutex<HashMap<String, Instant>>,
    ttl: Duration,
}

impl DeduplicationStore {
    pub fn new(ttl: Duration) -> Arc<Self> {
        Arc::new(DeduplicationStore {
            seen: Mutex::new(HashMap::new()),
            ttl,
        })
    }

    /// Returns true if event_id was already seen (duplicate), false if new.
    pub fn mark_seen(&self, event_id: &str) -> bool {
        let mut seen = self.seen.lock().unwrap();
        if seen.contains_key(event_id) {
            return true;
        }
        seen.insert(event_id.to_string(), Instant::now());
        false
    }
}

pub struct OrderEventConsumer {
    dedup: Arc<DeduplicationStore>,
}

impl OrderEventConsumer {
    pub fn new(dedup: Arc<DeduplicationStore>) -> Self {
        OrderEventConsumer { dedup }
    }

    pub async fn run(&self, mut rx: mpsc::UnboundedReceiver<Vec<u8>>) {
        while let Some(raw) = rx.recv().await {
            if let Err(e) = self.handle(&raw) {
                eprintln!("[Consumer] error: {e}");
            }
        }
    }

    fn handle(&self, raw: &[u8]) -> Result<(), String> {
        let envelope: EventEnvelope = serde_json::from_slice(raw)
            .map_err(|e| format!("deserialize error: {e}"))?;

        if self.dedup.mark_seen(&envelope.event_id) {
            println!("[Consumer] Skipping duplicate: {}", envelope.event_id);
            return Ok(());
        }

        match envelope.event_type.as_str() {
            "OrderPlaced" => self.handle_order_placed(envelope),
            other => {
                // Forward compatibility: unknown event types are silently ignored
                println!("[Consumer] Unknown event type {other:?} — skipping");
                Ok(())
            }
        }
    }

    fn handle_order_placed(&self, envelope: EventEnvelope) -> Result<(), String> {
        // Schema upcasting: V1 → V2
        let payload: OrderPlacedV2 = if envelope.schema_version == 1 {
            let v1: OrderPlacedV1 = serde_json::from_value(envelope.payload)
                .map_err(|e| format!("deserialize V1: {e}"))?;
            v1.into() // uses From<OrderPlacedV1> for OrderPlacedV2
        } else {
            serde_json::from_value(envelope.payload)
                .map_err(|e| format!("deserialize V2: {e}"))?
        };

        println!(
            "[Consumer] OrderPlaced: order={} customer={} total={} {}",
            payload.order_id, payload.customer_id, payload.total_cents, payload.currency
        );
        Ok(())
    }
}

// ─── Main ─────────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() {
    let outbox = InMemoryOutbox::new();
    let broker = InMemoryBroker::new();
    let dedup = DeduplicationStore::new(Duration::from_secs(3600));

    let consumer = OrderEventConsumer::new(Arc::clone(&dedup));
    let mut rx = broker.subscribe("orders.OrderPlaced");

    // Start consumer in background
    tokio::spawn(async move {
        consumer.run(rx).await;
    });

    println!("=== Outbox + Relay Demo ===");

    // Insert events to outbox (simulating DB transaction)
    for i in 1u32..=3 {
        let order_id = format!("order-{i:03}");
        let payload = serde_json::to_vec(&EventEnvelope {
            event_id: format!("evt-{order_id}"),
            event_type: "OrderPlaced".to_string(),
            schema_version: 2,
            aggregate_id: order_id.clone(),
            payload: serde_json::to_value(OrderPlacedV2 {
                order_id: order_id.clone(),
                customer_id: format!("cust-{}", i * 10),
                total_cents: i as i64 * 1000,
                currency: "USD".to_string(),
            }).unwrap(),
        }).unwrap();

        outbox.insert(&format!("evt-{order_id}"), "OrderPlaced", payload);
        println!("Inserted to outbox: {order_id}");
    }

    // Run relay once
    let relay = OutboxRelay::new(Arc::clone(&outbox), Arc::clone(&broker));
    relay.process_once().unwrap();

    sleep(Duration::from_millis(100)).await; // let consumer process

    println!("\n=== Schema Evolution (V1 event) ===");
    let v1_payload = serde_json::to_vec(&EventEnvelope {
        event_id: "evt-v1-old".to_string(),
        event_type: "OrderPlaced".to_string(),
        schema_version: 1,
        aggregate_id: "order-old".to_string(),
        payload: serde_json::to_value(OrderPlacedV1 {
            order_id: "order-old".to_string(),
            customer_id: "cust-legacy".to_string(),
            total_cents: 9900,
        }).unwrap(),
    }).unwrap();
    broker.publish("orders.OrderPlaced", v1_payload).unwrap();
    sleep(Duration::from_millis(100)).await;

    println!("\n=== Idempotency Demo ===");
    // Re-publish an event that was already processed
    let dup_payload = serde_json::to_vec(&EventEnvelope {
        event_id: "evt-order-001".to_string(), // already seen
        event_type: "OrderPlaced".to_string(),
        schema_version: 2,
        aggregate_id: "order-001".to_string(),
        payload: serde_json::to_value(OrderPlacedV2 {
            order_id: "order-001".to_string(),
            customer_id: "cust-10".to_string(),
            total_cents: 1000,
            currency: "USD".to_string(),
        }).unwrap(),
    }).unwrap();
    broker.publish("orders.OrderPlaced", dup_payload).unwrap();
    sleep(Duration::from_millis(100)).await;
}
```

### Rust-specific considerations

Rust's `From<OrderPlacedV1> for OrderPlacedV2` impl is the idiomatic upcasting mechanism. Rather than a match statement inside the consumer, the `into()` call at the type level makes the conversion explicit and testable in isolation. This is cleaner than Go's equivalent embedded struct pattern.

Tokio's `select!` macro in the relay's run loop is the idiomatic way to combine periodic work (the ticker) with a shutdown signal (the channel). The `select!` races both futures — whichever completes first wins. This is Go's `select` equivalent with cleaner semantics.

`serde_json::Value` as the payload type in `EventEnvelope` allows the envelope to carry any payload schema without knowing it at the envelope level. The consumer deserializes the `Value` to the appropriate type after checking the schema version. In production, Serde with Protobuf (`prost` crate) or Avro (`apache-avro` crate) provides stronger schema enforcement.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Outbox transaction | `database/sql` BeginTx | `sqlx` or `diesel` transactions |
| Relay polling | `time.Ticker` + goroutine | `tokio::time::interval` + Tokio task |
| Schema upcasting | Type switch + struct copy | `From<V1> for V2` trait impl |
| Broker abstraction | Interface | Struct with method (or trait) |
| Consumer idempotency | `sync.RWMutex` map | `Mutex<HashMap>` |
| Unknown event handling | `default` in switch | `_` arm in match, with log |
| Shutdown signal | `context.Context` cancellation | `mpsc::Receiver<()>` or `CancellationToken` |
| JSON versioning | `encoding/json` + `json.RawMessage` | `serde_json` + `serde_json::Value` |

## Production War Stories

**LinkedIn and Kafka**: LinkedIn invented Kafka in 2011 to solve an event-driven architecture problem at scale: their activity stream (feeds, connections, likes) needed to be consumed by multiple downstream systems (analytics, recommendations, notifications) without the producer knowing about each consumer. Before Kafka, each producer had to push to each consumer's API — tight coupling that made adding a new consumer require a producer change. Kafka decoupled producers from consumers by design.

**Airbnb's Outbox Pattern**: Airbnb's engineering blog describes a class of bugs they called "half-written state" — database writes committed but events never published because the application crashed after the write but before the Kafka publish. They adopted the transactional outbox pattern across their services after one such bug caused booking confirmation emails to not be sent for thousands of reservations. The outbox relay made publication atomic with the database write.

**Stripe's event versioning discipline**: Stripe's API engineering blog is one of the best public resources on event schema evolution. Stripe maintains backward compatibility in their webhook events across years: they never remove fields, they mark fields as deprecated before removal (with a minimum 6-month notice), and they never change field types in incompatible ways. Their schema registry enforces this. The result: a consumer written in 2019 can still receive and process Stripe events in 2024 without modification.

**The at-least-once incident**: A fintech's event-driven payment system used at-least-once delivery but did not implement idempotent consumers. A message broker failover caused approximately 0.1% of payment events to be delivered twice. Without idempotency, payments were processed twice. The incident affected ~50 customers and required manual investigation of every event during the failover window. The fix was a deduplication table with event IDs and unique constraints.

## Architectural Trade-offs

**When to use Event-Driven Architecture:**
- Services need to be decoupled in time (one service can be down without affecting others)
- Multiple services need to react to the same business fact (order placed → send email, update inventory, trigger analytics)
- High throughput async processing where synchronous response is not required
- Building read projections or CQRS read models from write-side events

**When NOT to:**
- Request-response patterns where the caller needs an immediate answer (place order and get an order ID back synchronously — use an HTTP API, not events, for the initial response)
- Simple systems with two services that must always be co-deployed and have no independent scaling
- Teams that cannot maintain idempotency guarantees
- When event ordering is critical across entity types and cannot be partitioned cleanly

**The "event-carried state transfer" trap**: When events carry the full current state of an entity (rather than just what changed), consumers become dependent on receiving every event in order to maintain an accurate view. Missing one event means the consumer has stale state with no way to detect it. Use events to signal that something happened; let consumers query for the current state if they need it (event notification pattern), or use CQRS projections that can be fully rebuilt from the event log.

## Common Pitfalls

**1. Publishing events outside a transaction.** Save to database, then publish to Kafka — if the application crashes between the two, the event is lost. Always use the outbox pattern or Kafka transactions (Kafka's native transactional API) to make persistence and publication atomic.

**2. Non-idempotent consumers.** At-least-once delivery is the practical default for every production message broker. Every consumer must be idempotent. Testing idempotency means delivering the same event twice and verifying the result is correct.

**3. Using events for request-response.** Synchronous request-response over an asynchronous event bus (publish a command event, wait for a response event) is anti-pattern: it adds all the complexity of async without any of the benefits. Use HTTP or gRPC for request-response; use events for notifications.

**4. Not monitoring consumer lag.** A consumer that falls behind silently can accumulate millions of unprocessed events before anyone notices. Monitor consumer group lag as a primary health metric. Alert when lag exceeds a threshold (appropriate to the business: payment events might alert at 100 events of lag; analytics events might tolerate 100,000).

**5. Ignoring schema evolution until version 2.** The discipline of backward/forward compatibility is easiest to establish on event version 1. Adding fields as required (non-optional) breaks consumers. Removing fields breaks consumers. The rules must be in place before the first consumer is deployed, not after.

## Exercises

**Exercise 1** (30 min): Trace through the Go outbox relay. Add a `processedCount` counter to the relay that tracks how many events have been published. Add a method to check whether all outbox events are published (useful for test assertions).

**Exercise 2** (2–4h): Implement a PostgreSQL-backed outbox repository. Design the `outbox` table schema: `(id SERIAL PRIMARY KEY, event_id UUID UNIQUE, event_type TEXT, payload JSONB, created_at TIMESTAMPTZ, published_at TIMESTAMPTZ)`. Implement `InsertEvent` and `FetchUnpublished` using `database/sql`. Test that two concurrent relay processes do not publish the same event twice (use `SELECT ... FOR UPDATE SKIP LOCKED` to claim a batch).

**Exercise 3** (4–8h): Implement consumer lag monitoring. The relay tracks the latest published sequence number; each consumer tracks the latest processed sequence number. A monitoring goroutine periodically computes lag = published - processed and logs it. Add a test that verifies: (a) when the consumer is stopped, lag increases; (b) when the consumer restarts, lag decreases.

**Exercise 4** (8–15h): Implement a complete event-driven order notification system. When `OrderPlaced` is published, three consumers react: (1) email notification consumer (logs "sending email"), (2) inventory consumer (logs "reserving items"), (3) analytics consumer (logs "recording event"). Add schema evolution: introduce a V2 `OrderPlaced` event with a new `shipping_address` field. Verify that all three consumers handle both V1 and V2 events correctly. Implement end-to-end idempotency testing by delivering each event twice and verifying no duplicate side effects.

## Further Reading

### Foundational Books

- **Designing Data-Intensive Applications** — Kleppmann (2017). Chapter 11 on stream processing is the most thorough treatment of event-driven systems: delivery semantics, ordering, exactly-once, consumer groups. Essential reading.
- **Enterprise Integration Patterns** — Hohpe & Woolf (2003). The original catalog of messaging patterns. Still the reference for message channel types, routing patterns, and transformation patterns.

### Blog Posts and Case Studies

- Martin Fowler: "The Many Meanings of Event-Driven Architecture" (2017) — martinfowler.com. Fowler distinguishes event notification, event-carried state transfer, event sourcing, and CQRS. Essential clarity.
- Jay Kreps (LinkedIn): "The Log: What every software engineer should know about real-time data's unifying abstraction" (2013) — engineering.linkedin.com. The philosophical foundation for Kafka and event-driven architecture at scale.
- Stripe Engineering: "Advanced webhook delivery" — stripe.com/blog. Stripe's treatment of at-least-once delivery and idempotency in production.

### Production Code to Read

- **Confluent Kafka clients** — github.com/confluentinc/confluent-kafka-go (Go). The production Kafka client with consumer group management.
- **Debezium** — debezium.io. The canonical CDC (Change Data Capture) tool for the outbox pattern. Reads database transaction logs instead of polling the outbox table.
- **Eventuate Tram** — github.com/eventuate-tram. Reference implementation of the transactional outbox pattern in Java.

### Talks

- Martin Fowler: "The Many Meanings of Event-Driven Architecture" (GOTO Chicago 2017) — YouTube. 45 minutes of essential clarity on what "event-driven" actually means.
- Jay Kreps: "I ♥ Logs" (Strangeloop 2013) — The talk version of the log essay. Explains why an append-only log is the unifying primitive for event-driven systems.
- Gregor Hohpe: "Enterprise Integration Patterns Revisited" (QCon 2017) — How the original 2003 patterns map to modern cloud messaging.
