<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [saga-pattern, choreography, orchestration, compensating-transactions, idempotency, distributed-consistency, temporal-workflows]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [domain-driven-design, event-driven-architecture, distributed-systems-basics, go-goroutines, rust-tokio]
papers: [Garcia-Molina-Sagas-1987]
industry_use: [Uber-Cadence-Temporal, Airbnb-workflows, Amazon-Step-Functions, Saga-Pattern-Richardson]
language_contrast: medium
-->

# Saga and Distributed Workflows

> A saga is a sequence of local transactions where each step publishes an event or message that triggers the next, and every step that cannot be rolled back has a compensating transaction that undoes its effects.

## Mental Model

Distributed transactions — the kind where you lock rows across two databases until all services confirm — are theoretically possible and practically catastrophic. They require all participating services to be available and responsive simultaneously, which violates the core property of distributed systems: partial failure is normal. Two-Phase Commit (2PC) is the protocol that makes distributed transactions work, and its failure modes (coordinator crashes mid-commit, participant timeouts, transaction log corruption) are a primary source of production incidents in distributed systems.

The saga pattern replaces distributed transactions with a sequence of local transactions coordinated by events. Each step in a saga executes a local transaction and either publishes a success event (triggering the next step) or a failure event (triggering compensating transactions for all completed steps). There is no global lock. Each service is independently available. Failure is handled by undoing what was done, not by holding locks until the problem resolves.

The canonical example is order placement across three services: `OrderService` creates an order in `pending` state, `PaymentService` charges the card, `InventoryService` reserves the items. If inventory reservation fails, the payment must be refunded and the order cancelled. If payment fails, only the order needs to be cancelled. This "undo" logic is the compensating transaction — it is the saga's answer to rollback.

There are two ways to coordinate a saga. Choreography-based sagas have no central coordinator: each service listens for events from other services and decides what to do. `OrderPlaced` → PaymentService listens and charges card → `PaymentSucceeded` → InventoryService listens and reserves → `InventoryReserved` → OrderService listens and confirms. Failures publish `PaymentFailed` or `ReservationFailed` events that trigger compensating transactions. This is highly decoupled but difficult to understand: to know what happens when `PaymentFailed` is published, you have to read every service's event handler.

Orchestration-based sagas have a central coordinator (the orchestrator or workflow engine) that explicitly calls each service and handles failures. The orchestrator knows the full workflow: "call payment, then call inventory, if inventory fails call payment-refund, if payment fails call order-cancel." This is easier to understand but creates a coupling to the orchestrator. Temporal and AWS Step Functions are production orchestration engines.

The hidden complexity of sagas is idempotency. Because distributed systems guarantee at-least-once delivery (messages can be delivered more than once), every step must be idempotent: if `ReserveInventory` is called twice for the same order, it must produce the same result as if called once. Implementing idempotency requires idempotency tokens (a unique identifier per logical operation) and deduplication at the receiving service.

## Core Concepts

### Compensating Transactions

Every step in a saga that cannot be rolled back atomically must have a compensating transaction. Compensation is not the same as undo: compensation brings the system to a consistent state from the business perspective, which may not be the same as the state before the step. Charging a credit card cannot be literally undone — a refund is the compensation.

### Idempotency Tokens

An idempotency token (also called an idempotency key) is a client-generated unique ID per logical operation. If the same operation is submitted twice with the same token, the second submission is a duplicate and should return the same result as the first without re-executing the operation. Idempotency tokens are what make sagas safe under at-least-once delivery.

### Choreography vs Orchestration

| | Choreography | Orchestration |
|--|--|--|
| Coordination | Implicit via events | Explicit via orchestrator |
| Coupling | Services know about events, not each other | Services know about their own API, not the workflow |
| Visibility | Hard to see the full workflow | Workflow is explicit in the orchestrator |
| Failure handling | Each service handles its own compensations | Orchestrator handles all compensations |
| Good for | Simple, stable workflows | Complex workflows with many failure paths |

### Saga Failure Modes

**Backward recovery**: Compensate all completed steps in reverse order. This is the standard recovery: if step 3 fails, undo step 2 then undo step 1.

**Forward recovery**: Retry the failed step. Appropriate when the step is likely to succeed on retry (transient network error). Combine with a retry limit and backward recovery as the fallback.

**Pivot transaction**: The point of no return in a saga. Steps before the pivot are compensatable. Steps after the pivot are not (they complete the business transaction). The pivot is typically the "commit" — for an order, it is the moment the payment is captured and inventory is reserved.

## Implementation: Go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Domain Events ────────────────────────────────────────────────────────────

type SagaEvent struct {
	SagaID    string
	EventType string
	Payload   map[string]interface{}
}

// ─── Idempotency Store ────────────────────────────────────────────────────────

// IdempotencyStore deduplicates operations by idempotency key.
// A real store would use Redis or a database table with a unique constraint.
type IdempotencyStore struct {
	mu      sync.RWMutex
	records map[string]interface{} // key → result
}

func NewIdempotencyStore() *IdempotencyStore {
	return &IdempotencyStore{records: make(map[string]interface{})}
}

// RecordOrGet returns (existingResult, true) if key was already processed,
// or (nil, false) if key is new. When false, caller must process and call SetResult.
func (s *IdempotencyStore) RecordOrGet(key string) (interface{}, bool) {
	s.mu.RLock()
	result, exists := s.records[key]
	s.mu.RUnlock()
	return result, exists
}

func (s *IdempotencyStore) SetResult(key string, result interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = result
}

// ─── Service Stubs (simplified) ──────────────────────────────────────────────

type OrderService struct {
	idempotency *IdempotencyStore
}

func (svc *OrderService) CreateOrder(idempotencyKey, orderID, customerID string) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		fmt.Printf("[OrderService] Duplicate request for %s — returning cached result\n", idempotencyKey)
		return nil
	}
	fmt.Printf("[OrderService] Creating order %s for customer %s\n", orderID, customerID)
	svc.idempotency.SetResult(idempotencyKey, "created")
	return nil
}

func (svc *OrderService) CancelOrder(idempotencyKey, orderID string) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		return nil
	}
	fmt.Printf("[OrderService] COMPENSATE: cancelling order %s\n", orderID)
	svc.idempotency.SetResult(idempotencyKey, "cancelled")
	return nil
}

type PaymentService struct {
	idempotency *IdempotencyStore
	shouldFail  bool
}

func (svc *PaymentService) ChargeCard(idempotencyKey, orderID string, amountCents int64) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		return nil
	}
	if svc.shouldFail {
		return fmt.Errorf("payment declined for order %s", orderID)
	}
	fmt.Printf("[PaymentService] Charged %d cents for order %s\n", amountCents, orderID)
	svc.idempotency.SetResult(idempotencyKey, "charged")
	return nil
}

func (svc *PaymentService) RefundPayment(idempotencyKey, orderID string) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		return nil
	}
	fmt.Printf("[PaymentService] COMPENSATE: refunding payment for order %s\n", orderID)
	svc.idempotency.SetResult(idempotencyKey, "refunded")
	return nil
}

type InventoryService struct {
	idempotency *IdempotencyStore
	shouldFail  bool
}

func (svc *InventoryService) ReserveItems(idempotencyKey, orderID string, items []string) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		return nil
	}
	if svc.shouldFail {
		return fmt.Errorf("insufficient inventory for order %s", orderID)
	}
	fmt.Printf("[InventoryService] Reserved items %v for order %s\n", items, orderID)
	svc.idempotency.SetResult(idempotencyKey, "reserved")
	return nil
}

func (svc *InventoryService) ReleaseReservation(idempotencyKey, orderID string) error {
	if _, exists := svc.idempotency.RecordOrGet(idempotencyKey); exists {
		return nil
	}
	fmt.Printf("[InventoryService] COMPENSATE: releasing reservation for order %s\n", orderID)
	svc.idempotency.SetResult(idempotencyKey, "released")
	return nil
}

// ─── PART 1: Choreography-Based Saga ─────────────────────────────────────────

// EventBus is the backbone of a choreography saga.
// Each service subscribes to events from other services.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]func(SagaEvent)
}

func NewEventBus() *EventBus {
	return &EventBus{handlers: make(map[string][]func(SagaEvent))}
}

func (b *EventBus) Subscribe(eventType string, handler func(SagaEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *EventBus) Publish(event SagaEvent) {
	b.mu.RLock()
	handlers := b.handlers[event.EventType]
	b.mu.RUnlock()
	for _, h := range handlers {
		h(event)
	}
}

// ChoreographySagaSetup wires the services together via events.
// The workflow is implicit: read each Subscribe call to understand it.
func ChoreographySagaSetup(
	bus *EventBus,
	orderSvc *OrderService,
	paymentSvc *PaymentService,
	inventorySvc *InventoryService,
) {
	// When OrderCreated → charge payment
	bus.Subscribe("OrderCreated", func(e SagaEvent) {
		sagaID := e.SagaID
		orderID := e.Payload["order_id"].(string)
		amountCents := e.Payload["amount_cents"].(int64)

		if err := paymentSvc.ChargeCard(sagaID+"-payment", orderID, amountCents); err != nil {
			bus.Publish(SagaEvent{SagaID: sagaID, EventType: "PaymentFailed",
				Payload: map[string]interface{}{"order_id": orderID, "error": err.Error()}})
			return
		}
		bus.Publish(SagaEvent{SagaID: sagaID, EventType: "PaymentSucceeded",
			Payload: map[string]interface{}{"order_id": orderID}})
	})

	// When PaymentSucceeded → reserve inventory
	bus.Subscribe("PaymentSucceeded", func(e SagaEvent) {
		sagaID := e.SagaID
		orderID := e.Payload["order_id"].(string)

		if err := inventorySvc.ReserveItems(sagaID+"-inventory", orderID, []string{"item-1", "item-2"}); err != nil {
			bus.Publish(SagaEvent{SagaID: sagaID, EventType: "ReservationFailed",
				Payload: map[string]interface{}{"order_id": orderID, "error": err.Error()}})
			return
		}
		bus.Publish(SagaEvent{SagaID: sagaID, EventType: "SagaCompleted",
			Payload: map[string]interface{}{"order_id": orderID}})
	})

	// Compensations:
	// When PaymentFailed → cancel order
	bus.Subscribe("PaymentFailed", func(e SagaEvent) {
		sagaID := e.SagaID
		orderID := e.Payload["order_id"].(string)
		orderSvc.CancelOrder(sagaID+"-cancel", orderID)
	})

	// When ReservationFailed → refund payment + cancel order
	bus.Subscribe("ReservationFailed", func(e SagaEvent) {
		sagaID := e.SagaID
		orderID := e.Payload["order_id"].(string)
		paymentSvc.RefundPayment(sagaID+"-refund", orderID)
		orderSvc.CancelOrder(sagaID+"-cancel", orderID)
	})

	bus.Subscribe("SagaCompleted", func(e SagaEvent) {
		fmt.Printf("[Saga:%s] Completed successfully for order %s\n",
			e.SagaID, e.Payload["order_id"])
	})
}

// ─── PART 2: Orchestration-Based Saga ─────────────────────────────────────────

// SagaStep defines one step in the orchestration saga, including its compensation.
type SagaStep struct {
	Name        string
	Execute     func(sagaID string) error
	Compensate  func(sagaID string) error
}

// SagaOrchestrator executes steps in order, compensating on failure.
type SagaOrchestrator struct {
	steps []SagaStep
}

func NewSagaOrchestrator(steps []SagaStep) *SagaOrchestrator {
	return &SagaOrchestrator{steps: steps}
}

// Execute runs the saga. On failure at step N, compensates steps N-1 to 0.
func (orch *SagaOrchestrator) Execute(ctx context.Context, sagaID string) error {
	completed := make([]int, 0, len(orch.steps))

	for i, step := range orch.steps {
		fmt.Printf("[Orchestrator:%s] Executing step: %s\n", sagaID, step.Name)
		if err := step.Execute(sagaID); err != nil {
			fmt.Printf("[Orchestrator:%s] Step %s failed: %v — compensating\n",
				sagaID, step.Name, err)
			// Compensate completed steps in reverse order
			for j := len(completed) - 1; j >= 0; j-- {
				completedStepIdx := completed[j]
				compStep := orch.steps[completedStepIdx]
				if compStep.Compensate != nil {
					fmt.Printf("[Orchestrator:%s] Compensating: %s\n", sagaID, compStep.Name)
					if compErr := compStep.Compensate(sagaID); compErr != nil {
						// Compensation failed — this requires human intervention or a dead-letter queue
						fmt.Printf("[Orchestrator:%s] COMPENSATION FAILED for %s: %v (ALERT REQUIRED)\n",
							sagaID, compStep.Name, compErr)
					}
				}
			}
			return fmt.Errorf("saga failed at step %s: %w", step.Name, err)
		}
		completed = append(completed, i)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	fmt.Printf("[Orchestrator:%s] Saga completed successfully\n", sagaID)
	return nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	idempotency := NewIdempotencyStore()
	orderSvc := &OrderService{idempotency: idempotency}
	paymentSvc := &PaymentService{idempotency: idempotency}
	inventorySvc := &InventoryService{idempotency: idempotency}

	// ── Choreography-Based Saga ───────────────────────────────────────────
	fmt.Println("=== Choreography-Based Saga (Success) ===")
	bus := NewEventBus()
	ChoreographySagaSetup(bus, orderSvc, paymentSvc, inventorySvc)

	sagaID := "saga-001"
	orderSvc.CreateOrder(sagaID+"-create", "order-001", "customer-42")
	bus.Publish(SagaEvent{
		SagaID:    sagaID,
		EventType: "OrderCreated",
		Payload: map[string]interface{}{
			"order_id":     "order-001",
			"amount_cents": int64(9999),
		},
	})

	fmt.Println("\n=== Choreography-Based Saga (Inventory Failure → Compensation) ===")
	inventorySvc.shouldFail = true
	idempotency2 := NewIdempotencyStore()
	orderSvc2 := &OrderService{idempotency: idempotency2}
	paymentSvc2 := &PaymentService{idempotency: idempotency2}
	inventorySvc2 := &InventoryService{idempotency: idempotency2, shouldFail: true}
	bus2 := NewEventBus()
	ChoreographySagaSetup(bus2, orderSvc2, paymentSvc2, inventorySvc2)

	sagaID2 := "saga-002"
	orderSvc2.CreateOrder(sagaID2+"-create", "order-002", "customer-43")
	bus2.Publish(SagaEvent{
		SagaID:    sagaID2,
		EventType: "OrderCreated",
		Payload: map[string]interface{}{
			"order_id":     "order-002",
			"amount_cents": int64(4999),
		},
	})

	// ── Orchestration-Based Saga ──────────────────────────────────────────
	fmt.Println("\n=== Orchestration-Based Saga (Payment Failure) ===")
	idempotency3 := NewIdempotencyStore()
	orderSvc3 := &OrderService{idempotency: idempotency3}
	paymentSvc3 := &PaymentService{idempotency: idempotency3, shouldFail: true}
	inventorySvc3 := &InventoryService{idempotency: idempotency3}

	orderID := "order-003"
	orderSvc3.CreateOrder("orch-create", orderID, "customer-44")

	orchestrator := NewSagaOrchestrator([]SagaStep{
		{
			Name:    "ChargePayment",
			Execute: func(id string) error { return paymentSvc3.ChargeCard(id+"-pay", orderID, 7999) },
			Compensate: func(id string) error { return paymentSvc3.RefundPayment(id+"-refund", orderID) },
		},
		{
			Name:    "ReserveInventory",
			Execute: func(id string) error { return inventorySvc3.ReserveItems(id+"-inv", orderID, []string{"item-1"}) },
			Compensate: func(id string) error { return inventorySvc3.ReleaseReservation(id+"-rel", orderID) },
		},
	})

	ctx := context.Background()
	if err := orchestrator.Execute(ctx, "saga-003"); err != nil {
		fmt.Printf("Saga failed (expected): %v\n", err)
		orderSvc3.CancelOrder("orch-cancel", orderID)
	}

	// Demonstrate idempotency: replay a step with the same key
	fmt.Println("\n=== Idempotency Demo ===")
	_ = paymentSvc.ChargeCard("saga-001-payment", "order-001", 9999) // duplicate — should not charge again
	_ = errors.New("") // silence unused import warning
	_ = time.Now()
}
```

### Go-specific considerations

Go's lack of algebraic types for workflow state means saga state is typically represented as a struct with step completion flags or as an ordered slice of completed step indices (as in the orchestrator above). For production sagas, Temporal's Go SDK provides durable workflow state — the workflow function can be paused, restarted, and replayed from checkpoints, with the entire execution history stored.

The `EventBus` in the choreography example is synchronous and in-process for demonstration. Production choreography sagas use a message broker (Kafka, RabbitMQ, NATS) where each service has its own consumer group. The broker provides at-least-once delivery, which is why idempotency is non-negotiable.

Error handling in the orchestrator is explicit: the orchestrator knows which steps completed and runs compensation in reverse. This is one advantage over choreography: the compensation logic is in one place, not scattered across services.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use async_trait::async_trait;
use tokio::sync::mpsc;

// ─── Saga Step Trait ─────────────────────────────────────────────────────────

/// SagaStep defines a step that can be executed and, if needed, compensated.
/// The `async_trait` macro is needed because Rust's trait system does not yet
/// support async functions in traits natively (stable as of Rust 1.75+, but
/// async_trait is still common in production code targeting older Rust versions).
#[async_trait]
pub trait SagaStep: Send + Sync {
    fn name(&self) -> &str;
    async fn execute(&self, saga_id: &str) -> Result<(), SagaError>;
    async fn compensate(&self, saga_id: &str) -> Result<(), SagaError>;
}

#[derive(Debug)]
pub struct SagaError {
    pub step: String,
    pub message: String,
}

impl std::fmt::Display for SagaError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "saga error at step {}: {}", self.step, self.message)
    }
}

// ─── Idempotency Store ───────────────────────────────────────────────────────

#[derive(Clone)]
pub struct IdempotencyStore {
    records: Arc<Mutex<HashMap<String, String>>>,
}

impl IdempotencyStore {
    pub fn new() -> Self {
        IdempotencyStore { records: Arc::new(Mutex::new(HashMap::new())) }
    }

    pub fn check_and_set(&self, key: &str, result: &str) -> bool {
        let mut records = self.records.lock().unwrap();
        if records.contains_key(key) {
            return true; // already processed
        }
        records.insert(key.to_string(), result.to_string());
        false // new entry — proceed with execution
    }
}

// ─── Payment Step ────────────────────────────────────────────────────────────

pub struct ChargePaymentStep {
    idempotency: IdempotencyStore,
    order_id: String,
    amount_cents: i64,
    should_fail: bool,
}

impl ChargePaymentStep {
    pub fn new(
        idempotency: IdempotencyStore,
        order_id: String,
        amount_cents: i64,
        should_fail: bool,
    ) -> Self {
        ChargePaymentStep { idempotency, order_id, amount_cents, should_fail }
    }
}

#[async_trait]
impl SagaStep for ChargePaymentStep {
    fn name(&self) -> &str { "ChargePayment" }

    async fn execute(&self, saga_id: &str) -> Result<(), SagaError> {
        let key = format!("{}-payment", saga_id);
        if self.idempotency.check_and_set(&key, "charged") {
            println!("[PaymentStep] Duplicate — returning cached result");
            return Ok(());
        }
        if self.should_fail {
            return Err(SagaError {
                step: self.name().to_string(),
                message: format!("payment declined for order {}", self.order_id),
            });
        }
        println!("[PaymentStep] Charged {} cents for order {}", self.amount_cents, self.order_id);
        Ok(())
    }

    async fn compensate(&self, saga_id: &str) -> Result<(), SagaError> {
        let key = format!("{}-payment-refund", saga_id);
        if self.idempotency.check_and_set(&key, "refunded") {
            return Ok(());
        }
        println!("[PaymentStep] COMPENSATE: refunding order {}", self.order_id);
        Ok(())
    }
}

// ─── Inventory Step ──────────────────────────────────────────────────────────

pub struct ReserveInventoryStep {
    idempotency: IdempotencyStore,
    order_id: String,
    items: Vec<String>,
    should_fail: bool,
}

impl ReserveInventoryStep {
    pub fn new(
        idempotency: IdempotencyStore,
        order_id: String,
        items: Vec<String>,
        should_fail: bool,
    ) -> Self {
        ReserveInventoryStep { idempotency, order_id, items, should_fail }
    }
}

#[async_trait]
impl SagaStep for ReserveInventoryStep {
    fn name(&self) -> &str { "ReserveInventory" }

    async fn execute(&self, saga_id: &str) -> Result<(), SagaError> {
        let key = format!("{}-inventory", saga_id);
        if self.idempotency.check_and_set(&key, "reserved") {
            return Ok(());
        }
        if self.should_fail {
            return Err(SagaError {
                step: self.name().to_string(),
                message: format!("insufficient inventory for order {}", self.order_id),
            });
        }
        println!("[InventoryStep] Reserved {:?} for order {}", self.items, self.order_id);
        Ok(())
    }

    async fn compensate(&self, saga_id: &str) -> Result<(), SagaError> {
        let key = format!("{}-inventory-release", saga_id);
        if self.idempotency.check_and_set(&key, "released") {
            return Ok(());
        }
        println!("[InventoryStep] COMPENSATE: releasing reservation for order {}", self.order_id);
        Ok(())
    }
}

// ─── Saga Orchestrator ────────────────────────────────────────────────────────

/// SagaOrchestrator executes steps sequentially, compensating on failure.
/// Rust's type system ensures all steps implement the SagaStep trait.
pub struct SagaOrchestrator {
    steps: Vec<Box<dyn SagaStep>>,
}

impl SagaOrchestrator {
    pub fn new(steps: Vec<Box<dyn SagaStep>>) -> Self {
        SagaOrchestrator { steps }
    }

    pub async fn execute(&self, saga_id: &str) -> Result<(), Vec<SagaError>> {
        let mut completed: Vec<usize> = Vec::new();

        for (i, step) in self.steps.iter().enumerate() {
            println!("[Orchestrator:{}] Executing: {}", saga_id, step.name());
            if let Err(e) = step.execute(saga_id).await {
                println!("[Orchestrator:{}] Step {} failed: {} — compensating", saga_id, step.name(), e);
                let mut compensation_errors = vec![e];

                for &j in completed.iter().rev() {
                    let comp_step = &self.steps[j];
                    println!("[Orchestrator:{}] Compensating: {}", saga_id, comp_step.name());
                    if let Err(comp_err) = comp_step.compensate(saga_id).await {
                        eprintln!("[Orchestrator:{}] COMPENSATION FAILED for {}: {} (ALERT)",
                            saga_id, comp_step.name(), comp_err);
                        compensation_errors.push(comp_err);
                    }
                }
                return Err(compensation_errors);
            }
            completed.push(i);
        }

        println!("[Orchestrator:{}] Saga completed successfully", saga_id);
        Ok(())
    }
}

// ─── Choreography via Channel (simplified) ───────────────────────────────────

#[derive(Debug, Clone)]
pub struct ChoreographyEvent {
    pub saga_id: String,
    pub event_type: String,
    pub order_id: String,
}

/// In a real choreography saga, each service would subscribe to a Kafka topic.
/// Here we simulate with tokio channels.
pub async fn run_choreography_saga(
    order_id: String,
    payment_should_fail: bool,
) {
    let (tx, mut rx) = mpsc::channel::<ChoreographyEvent>(16);
    let tx_clone = tx.clone();

    // Simulate publishing the first event
    tx.send(ChoreographyEvent {
        saga_id: "chore-001".to_string(),
        event_type: "OrderCreated".to_string(),
        order_id: order_id.clone(),
    }).await.unwrap();

    drop(tx); // close sender so rx eventually drains

    while let Some(event) = rx.recv().await {
        let tx = tx_clone.clone();
        match event.event_type.as_str() {
            "OrderCreated" => {
                println!("[Choreography] OrderCreated → charging payment");
                if payment_should_fail {
                    tx.send(ChoreographyEvent {
                        saga_id: event.saga_id.clone(),
                        event_type: "PaymentFailed".to_string(),
                        order_id: event.order_id.clone(),
                    }).await.ok();
                } else {
                    println!("[PaymentService] Charged for order {}", event.order_id);
                    tx.send(ChoreographyEvent {
                        saga_id: event.saga_id.clone(),
                        event_type: "PaymentSucceeded".to_string(),
                        order_id: event.order_id.clone(),
                    }).await.ok();
                }
            }
            "PaymentSucceeded" => {
                println!("[Choreography] PaymentSucceeded → reserving inventory");
                println!("[InventoryService] Reserved for order {}", event.order_id);
                tx.send(ChoreographyEvent {
                    saga_id: event.saga_id.clone(),
                    event_type: "SagaCompleted".to_string(),
                    order_id: event.order_id.clone(),
                }).await.ok();
            }
            "PaymentFailed" => {
                println!("[Choreography] PaymentFailed → compensating: cancelling order {}", event.order_id);
                // No more events — saga ends
            }
            "SagaCompleted" => {
                println!("[Choreography] Saga completed for order {}", event.order_id);
            }
            _ => {}
        }
    }
}

// ─── Main ─────────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() {
    println!("=== Orchestration Saga (Inventory Failure) ===");
    let idempotency = IdempotencyStore::new();

    let steps: Vec<Box<dyn SagaStep>> = vec![
        Box::new(ChargePaymentStep::new(
            idempotency.clone(),
            "order-001".to_string(),
            9999,
            false, // payment succeeds
        )),
        Box::new(ReserveInventoryStep::new(
            idempotency.clone(),
            "order-001".to_string(),
            vec!["item-A".to_string(), "item-B".to_string()],
            true, // inventory fails → triggers compensation of payment
        )),
    ];

    let orchestrator = SagaOrchestrator::new(steps);
    match orchestrator.execute("saga-rust-001").await {
        Ok(_) => println!("Saga succeeded"),
        Err(errors) => {
            println!("Saga failed with {} error(s):", errors.len());
            for e in &errors { println!("  - {e}"); }
        }
    }

    println!("\n=== Choreography Saga (Success) ===");
    run_choreography_saga("order-002".to_string(), false).await;

    println!("\n=== Choreography Saga (Payment Failure) ===");
    run_choreography_saga("order-003".to_string(), true).await;
}
```

### Rust-specific considerations

`Box<dyn SagaStep>` provides dynamic dispatch for the saga steps — each step can be a different concrete type as long as it implements the `SagaStep` trait. The `Vec<Box<dyn SagaStep>>` in `SagaOrchestrator` is the idiomatic Rust way to hold a heterogeneous collection of trait objects.

The `#[async_trait]` macro is required because Rust's native async functions in traits have limitations that make them impractical for `dyn Trait` dispatch without it. Native async trait support (via Return Position Impl Trait in traits) is available in Rust 1.75+, but `async_trait` remains common in production code for compatibility with older Rust versions and for clarity.

Rust's ownership model helps with the compensation logic: `completed.iter().rev()` iterates in reverse without copying, and each step's `compensate` call owns only what it needs. The error accumulation (`Vec<SagaError>`) lets the orchestrator collect both the failure error and any compensation failures, rather than silently dropping them.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Saga step abstraction | Function types in struct | `dyn SagaStep` trait objects |
| Async execution | `context.Context` + goroutines | `async/await` + Tokio |
| Idempotency store | `sync.RWMutex` + map | `Arc<Mutex<HashMap>>` |
| Choreography | `EventBus` struct with handler maps | `mpsc::channel` + match |
| Compensation error handling | Print + continue | Collect into `Vec<SagaError>` |
| Step reversal | `completed[j]` slice traversal | `completed.iter().rev()` |
| Temporal SDK integration | `go.temporal.io/sdk/workflow` | `temporal.io/sdk` (Rust SDK in development) |

## Production War Stories

**Uber Cadence → Temporal**: Uber built Cadence, an open-source workflow engine, to manage their ride-hailing saga: matching, pricing, driver assignment, payment, receipt generation — each step runs in a different service, and all of them must either complete or compensate. Cadence (later open-sourced and forked as Temporal) makes each saga step durable: if the worker crashes mid-saga, Temporal replays the workflow from its event history (Event Sourcing under the hood) and continues from where it left off. Temporal is now used at Snap, Netflix, Stripe, and Datadog.

**Airbnb's distributed workflow**: Airbnb's reservation system is a classic saga: reserve dates (calendar service), charge payment (payment service), notify host (notification service), update listing availability (listing service). Their engineering blog describes migrating from direct service calls (which caused partial failures with no compensation) to an orchestrated saga with explicit compensation steps. The migration reduced booking failure rates by ~40%.

**Amazon Step Functions**: Amazon's own distributed workflow service is an orchestration saga engine. Each Step Functions state machine is an orchestration saga: states correspond to saga steps, error handlers correspond to compensations. AWS Lambda, ECS tasks, or any AWS service can be a step. Step Functions was built because Amazon's own microservices needed a reliable way to coordinate long-running processes without distributed transactions.

**The compensation that failed**: A post-mortem from a fintech described a saga where the payment step succeeded (card charged), inventory reservation failed, and the compensation (payment refund) also failed due to a downstream rate limit. The result: customer was charged but received no goods. No alert was raised because the saga "completed" its compensation attempt. The lesson: failed compensations must be persisted and retried independently, or human-escalated, not silently logged. Temporal's workflow engine solves this by retrying compensation steps with configurable retry policies and alerting on terminal failures.

## Architectural Trade-offs

**When to use Sagas:**
- Business process spans multiple services and cannot be rolled back atomically
- 2PC distributed transactions are unacceptable (usually: always, for microservices)
- Each step has a well-defined compensating transaction
- Eventual consistency between services is acceptable for the business process

**When NOT to use Sagas:**
- All steps happen within a single service and a single database — use a local ACID transaction
- Some steps have no meaningful compensation (you cannot "un-send" an email — design around this)
- The business process requires immediate, synchronous consistency across all services — reconsider whether the decomposition is correct
- Team cannot maintain the idempotency guarantees — inconsistent idempotency is worse than no saga

**Choreography vs Orchestration decision:**
Use choreography when: services are independently deployable teams, the workflow is stable and simple, you want to avoid a central point of failure.
Use orchestration when: the workflow is complex with many failure paths, you need clear visibility into workflow state, you are integrating with Temporal/Step Functions.

Most production systems start with choreography and migrate to orchestration as the workflow grows complex.

## Common Pitfalls

**1. Not implementing compensations before going to production.** Teams build the happy path and defer compensations. The first production incident reveals that half the operations have no compensation. Compensations must be built alongside forward steps — they are part of the same feature.

**2. Non-idempotent saga steps.** At-least-once delivery is the default. Every saga step will eventually be called twice with the same parameters. If your `ChargePayment` step charges twice on duplicate delivery, you have double-charged a customer. Build idempotency into every step, not as an afterthought.

**3. Losing failed compensations.** When a compensation fails, it must be retried. Logging the error and continuing is the worst possible response: the saga appears complete but the system is in an inconsistent state. Failed compensations should go to a dead-letter queue or trigger an alert that requires human resolution.

**4. Making the orchestrator too smart.** An orchestrator that contains business logic ("if the order total is over $500, also call the fraud check service") has become a monolith. The orchestrator's job is coordination, not business decisions. Business logic belongs in the individual services.

**5. Confusing saga timeouts with saga failure.** A saga step that times out is not necessarily a failed step — the downstream service may have processed the request successfully before the timeout. The correct response to a timeout depends on whether the step is idempotent: if yes, retry safely; if no, query the downstream for the result before deciding to compensate.

## Exercises

**Exercise 1** (30 min): Trace through the orchestration saga. Add a fourth step: `SendConfirmationEmail`. Note that email sending has no meaningful compensation. Implement the step so that its `Compensate` method is a no-op and document why.

**Exercise 2** (2–4h): Implement a saga state store that persists each saga's status (which steps completed, which compensated). When the saga orchestrator restarts (simulate by creating a new instance), it should resume from the last known good state rather than re-executing completed steps.

**Exercise 3** (4–8h): Implement a retry mechanism in the orchestrator: before compensating on failure, retry the failed step up to 3 times with exponential backoff (100ms, 200ms, 400ms). Only compensate if all retries fail. Test with a step that fails the first two times and succeeds on the third.

**Exercise 4** (8–15h): Implement a Temporal-like workflow execution using Go channels and goroutines. Each workflow function is a goroutine that can suspend at "activity" calls (calls to external services). The runtime records completed activities and replays them on restart without re-executing. Use this to implement the order saga: if the worker crashes and restarts mid-saga, it should resume correctly.

## Further Reading

### Foundational Books

- **Microservices Patterns** — Chris Richardson (2018). Chapter 4 is the most thorough treatment of sagas in print, with extensive discussion of choreography vs orchestration and compensation design.
- **Designing Distributed Systems** — Brendan Burns (2018). Chapter on batch computation pipelines covers workflow coordination patterns.

### Blog Posts and Case Studies

- Garcia-Molina & Salem: "Sagas" (1987) — dl.acm.org. The original academic paper. Short, readable, and surprisingly directly applicable.
- Chris Richardson: "Pattern: Saga" — microservices.io/patterns/data/saga.html. The clearest modern explanation.
- Temporal engineering blog — temporal.io/blog. Case studies from Stripe, Netflix, Snap on production saga implementations.

### Production Code to Read

- **Temporal** — github.com/temporalio/temporal. The server-side implementation of a production saga orchestration engine.
- **Temporal Go SDK** — github.com/temporalio/sdk-go. The Go client. Workflow functions look synchronous but are event-sourced under the hood.
- **Eventuate Tram** — github.com/eventuate-tram. Chris Richardson's reference implementation of choreography and orchestration sagas in Java.

### Talks

- Chris Richardson: "Developing Microservices with Aggregates" (SpringOne 2016) — Clear explanation of why sagas are necessary when using DDD aggregates.
- Maxim Fateev: "Building Reliable Distributed Systems with Temporal" (QCon 2021) — Temporal's co-creator explaining the engine that powers production sagas at scale.
