<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [ports-and-adapters, dependency-inversion, primary-ports, secondary-ports, testability, clean-architecture]
languages: [go, rust]
estimated_reading_time: 65 min
bloom_level: evaluate
prerequisites: [domain-driven-design, go-interfaces, rust-traits, dependency-injection]
papers: [Cockburn-Hexagonal-2005]
industry_use: [Netflix-OSS, Spring-hexagonal, Rust-actix-hexagonal, Go-clean-arch]
language_contrast: low
-->

# Hexagonal Architecture

> Business logic should not know or care whether it is being called from an HTTP endpoint, a gRPC server, a CLI, or a test — and it should not know whether it persists data to PostgreSQL, Redis, or an in-memory map.

## Mental Model

The business logic of most software systems is relatively small. For an e-commerce system, the rules about orders, inventory, and payments fit in a few thousand lines. The rest — the HTTP handlers, the SQL queries, the message consumer wiring, the configuration loading — is infrastructure. Infrastructure is not the point; it is scaffolding. But in most codebases, the scaffolding is entangled with the point.

When an HTTP handler constructs a database connection, builds a SQL query, calls some business logic, and formats the response, none of those four concerns are separated. When you want to test the business logic, you need a real database. When you want to add a gRPC interface, you copy the HTTP handler and add gRPC-specific code around the same business logic. When you want to swap PostgreSQL for DynamoDB, you touch files that also contain business rules. This is the problem Hexagonal Architecture (also called Ports and Adapters, also called Clean Architecture) solves.

The central insight is: the application defines interfaces (Ports) that represent what it needs from the world, and outside code provides implementations (Adapters). The application layer says "I need something that can save an order" — that is a Port. PostgreSQL is one Adapter. An in-memory map is another. The application never imports either; it imports only the Port interface and depends on having an implementation injected.

The "hexagonal" metaphor is about the equal status of all external actors: a user driving the application through HTTP is no more special than a test driving it directly, or a message queue triggering it. All external drivers go through the same Port — there is no "real" path and "test" path. There is one application, and many Adapters that can connect to it.

What you get from this discipline: pure business logic that runs in any test without a network or database. Adding a new transport (gRPC, CLI, WebSocket) requires writing a new Adapter, not modifying the domain. Swapping infrastructure (PostgreSQL for DynamoDB) requires writing a new Adapter, not touching business logic. This is the architectural property most senior developers have been burned for not having: the ability to change infrastructure without changing the system.

## Core Concepts

### Ports

A Port is an interface that the application core defines and the outside world implements (for secondary/driven ports) or calls (for primary/driving ports).

**Primary ports** (driving): The interface through which external actors drive the application. Your HTTP handler calls `OrderService.PlaceOrder(cmd)` — `OrderService` is the primary port. The HTTP handler is the adapter.

**Secondary ports** (driven): The interface through which the application calls external systems. `OrderRepository.Save(order)` is a secondary port. The PostgreSQL implementation is the adapter.

### Adapters

An Adapter translates between the external format and the application's format. The HTTP Adapter translates HTTP requests into commands and HTTP responses from the application's results. The PostgreSQL Adapter translates `OrderRepository.Save` calls into SQL.

### Dependency Direction

Dependencies always point inward. The HTTP Adapter imports the application. The application imports nothing from adapters. The domain imports nothing from the application layer. This is strict and non-negotiable — it is what makes the architecture testable.

### The Composition Root

Somewhere, the adapters are wired together: "use the PostgreSQL adapter for OrderRepository, use the HTTP adapter for the primary port." This is the composition root, typically `main()`. The composition root is the one place that knows about all adapters. Change it to swap infrastructure.

## Implementation: Go

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// ─── Domain Layer ─────────────────────────────────────────────────────────────
// The domain has zero imports from application, infrastructure, or transport.

type OrderID string
type CustomerID string

type Order struct {
	ID         OrderID
	CustomerID CustomerID
	Items      []OrderItem
	Status     string
}

type OrderItem struct {
	ProductID string
	Quantity  int
	PriceCents int64
}

type OrderDomainError struct {
	Code    string
	Message string
}

func (e *OrderDomainError) Error() string { return fmt.Sprintf("[%s] %s", e.Code, e.Message) }

var (
	ErrEmptyOrder  = &OrderDomainError{Code: "EMPTY_ORDER", Message: "order must have at least one item"}
	ErrOrderLocked = &OrderDomainError{Code: "ORDER_LOCKED", Message: "order cannot be modified in current state"}
)

func NewOrder(id OrderID, customerID CustomerID, items []OrderItem) (*Order, error) {
	if len(items) == 0 {
		return nil, ErrEmptyOrder
	}
	return &Order{
		ID:         id,
		CustomerID: customerID,
		Items:      items,
		Status:     "placed",
	}, nil
}

// ─── Application Layer ────────────────────────────────────────────────────────
// Defines the ports (interfaces). Imports only the domain.

// PlaceOrderCommand is the input to the PlaceOrder use case.
type PlaceOrderCommand struct {
	OrderID    OrderID
	CustomerID CustomerID
	Items      []OrderItem
}

// PlaceOrderResult is the output.
type PlaceOrderResult struct {
	OrderID OrderID
	Status  string
}

// OrderService is the PRIMARY PORT — it is the boundary through which
// all external actors (HTTP, gRPC, CLI, tests) interact with the application.
type OrderService interface {
	PlaceOrder(cmd PlaceOrderCommand) (PlaceOrderResult, error)
	GetOrder(id OrderID) (*Order, error)
}

// OrderRepository is a SECONDARY PORT — it is what the application needs
// from the persistence infrastructure, expressed as a domain-language interface.
type OrderRepository interface {
	Save(order *Order) error
	FindByID(id OrderID) (*Order, error)
}

// NotificationService is another SECONDARY PORT — the application
// declares that it needs to send notifications, without specifying how.
type NotificationService interface {
	NotifyOrderPlaced(order *Order) error
}

// IDGenerator is a SECONDARY PORT for generating unique identifiers.
type IDGenerator interface {
	NewOrderID() OrderID
}

// orderApplicationService is the application's implementation of OrderService.
// It depends only on the secondary port interfaces — never on concrete adapters.
type orderApplicationService struct {
	orders        OrderRepository
	notifications NotificationService
}

func NewOrderApplicationService(
	orders OrderRepository,
	notifications NotificationService,
) OrderService {
	return &orderApplicationService{
		orders:        orders,
		notifications: notifications,
	}
}

func (svc *orderApplicationService) PlaceOrder(cmd PlaceOrderCommand) (PlaceOrderResult, error) {
	order, err := NewOrder(cmd.OrderID, cmd.CustomerID, cmd.Items)
	if err != nil {
		return PlaceOrderResult{}, fmt.Errorf("creating order: %w", err)
	}

	if err := svc.orders.Save(order); err != nil {
		return PlaceOrderResult{}, fmt.Errorf("saving order: %w", err)
	}

	// Best-effort notification — not critical path. A real system would use
	// a transactional outbox here (see event-driven architecture section).
	if err := svc.notifications.NotifyOrderPlaced(order); err != nil {
		fmt.Printf("WARNING: notification failed for order %s: %v\n", order.ID, err)
	}

	return PlaceOrderResult{OrderID: order.ID, Status: order.Status}, nil
}

func (svc *orderApplicationService) GetOrder(id OrderID) (*Order, error) {
	return svc.orders.FindByID(id)
}

// ─── Infrastructure Adapters ──────────────────────────────────────────────────
// Adapters depend on the application layer (they implement its interfaces).
// The application never depends on adapters.

// InMemoryOrderRepository is a SECONDARY ADAPTER for testing and development.
type InMemoryOrderRepository struct {
	mu     sync.RWMutex
	orders map[OrderID]*Order
}

func NewInMemoryOrderRepository() *InMemoryOrderRepository {
	return &InMemoryOrderRepository{orders: make(map[OrderID]*Order)}
}

func (r *InMemoryOrderRepository) Save(order *Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[order.ID] = order
	return nil
}

func (r *InMemoryOrderRepository) FindByID(id OrderID) (*Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}
	return o, nil
}

// LoggingNotificationService is a SECONDARY ADAPTER that logs instead of
// sending real emails/SMS — used in development and integration tests.
type LoggingNotificationService struct{}

func (n *LoggingNotificationService) NotifyOrderPlaced(order *Order) error {
	fmt.Printf("[NOTIFICATION] Order %s placed for customer %s\n",
		order.ID, order.CustomerID)
	return nil
}

// ─── HTTP Adapter (Primary Adapter) ──────────────────────────────────────────
// The HTTP adapter translates HTTP requests into application commands.
// It depends on OrderService (the primary port) — never on the domain directly.

type HTTPAdapter struct {
	service OrderService
}

func NewHTTPAdapter(service OrderService) *HTTPAdapter {
	return &HTTPAdapter{service: service}
}

type placeOrderRequest struct {
	CustomerID string    `json:"customer_id"`
	Items      []itemReq `json:"items"`
}

type itemReq struct {
	ProductID  string `json:"product_id"`
	Quantity   int    `json:"quantity"`
	PriceCents int64  `json:"price_cents"`
}

type placeOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

func (h *HTTPAdapter) HandlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var req placeOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	items := make([]OrderItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = OrderItem{
			ProductID:  item.ProductID,
			Quantity:   item.Quantity,
			PriceCents: item.PriceCents,
		}
	}

	cmd := PlaceOrderCommand{
		OrderID:    OrderID(fmt.Sprintf("order-%d", 1000)), // real system: use ID generator
		CustomerID: CustomerID(req.CustomerID),
		Items:      items,
	}

	result, err := h.service.PlaceOrder(cmd)
	if err != nil {
		var domainErr *OrderDomainError
		if errors.As(err, &domainErr) {
			http.Error(w, domainErr.Message, http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(placeOrderResponse{
		OrderID: string(result.OrderID),
		Status:  result.Status,
	})
}

func (h *HTTPAdapter) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := OrderID(r.URL.Query().Get("id"))
	order, err := h.service.GetOrder(orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// ─── CLI Adapter (Another Primary Adapter) ────────────────────────────────────
// The same business logic, different transport. Zero changes to the domain.

type CLIAdapter struct {
	service OrderService
}

func NewCLIAdapter(service OrderService) *CLIAdapter {
	return &CLIAdapter{service: service}
}

func (cli *CLIAdapter) PlaceTestOrder() {
	result, err := cli.service.PlaceOrder(PlaceOrderCommand{
		OrderID:    "order-cli-001",
		CustomerID: "customer-cli",
		Items: []OrderItem{
			{ProductID: "prod-1", Quantity: 1, PriceCents: 9900},
		},
	})
	if err != nil {
		fmt.Printf("CLI error: %v\n", err)
		return
	}
	fmt.Printf("CLI placed order: %s (status: %s)\n", result.OrderID, result.Status)
}

// ─── Composition Root (main) ──────────────────────────────────────────────────
// This is the ONLY place that knows about all adapters.
// Swapping infrastructure means changing this function only.

func main() {
	// Wire secondary adapters (infrastructure)
	orderRepo := NewInMemoryOrderRepository()
	notifications := &LoggingNotificationService{}

	// Create the application service by injecting secondary ports
	service := NewOrderApplicationService(orderRepo, notifications)

	// Wire primary adapters (transports)
	cliAdapter := NewCLIAdapter(service)
	cliAdapter.PlaceTestOrder()

	httpAdapter := NewHTTPAdapter(service)
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", httpAdapter.HandlePlaceOrder)
	mux.HandleFunc("/orders/get", httpAdapter.HandleGetOrder)

	fmt.Println("HTTP adapter listening on :8080")
	// http.ListenAndServe(":8080", mux) // uncomment to serve
	_ = mux
}
```

### Go-specific considerations

Go interfaces are defined by the consumer, not the provider — an ideal match for Hexagonal Architecture. `OrderRepository` is defined in the application layer, and the infrastructure adapter implements it implicitly. No `implements` declaration, no base type inheritance. The Go compiler verifies the contract when the adapter is passed to the application service.

The composition root in Go is almost always `main()`. For larger applications, a `wire` (Google) or `fx` (Uber) dependency injection container replaces manual wiring in `main`. Both approaches produce the same architecture — the DI container is the composition root.

Go's `errors.As` in the HTTP adapter shows how domain errors propagate through the layers: the domain returns typed errors, the application layer wraps them with `fmt.Errorf("context: %w", err)`, and the HTTP adapter unwraps them to produce the correct HTTP status code. The adapter handles the transport concern (HTTP status codes) without the domain needing to know HTTP exists.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

// ─── Domain Layer ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct OrderId(pub String);

#[derive(Debug, Clone)]
pub struct CustomerId(pub String);

#[derive(Debug, Clone)]
pub struct OrderItem {
    pub product_id: String,
    pub quantity: u32,
    pub price_cents: i64,
}

#[derive(Debug, Clone)]
pub struct Order {
    pub id: OrderId,
    pub customer_id: CustomerId,
    pub items: Vec<OrderItem>,
    pub status: OrderStatus,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderStatus {
    Placed,
    Cancelled,
    Shipped,
}

#[derive(Debug)]
pub enum DomainError {
    EmptyOrder,
    InvalidState { message: String },
}

impl std::fmt::Display for DomainError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            DomainError::EmptyOrder => write!(f, "order must have at least one item"),
            DomainError::InvalidState { message } => write!(f, "invalid state: {message}"),
        }
    }
}

impl Order {
    pub fn new(id: OrderId, customer_id: CustomerId, items: Vec<OrderItem>) -> Result<Self, DomainError> {
        if items.is_empty() {
            return Err(DomainError::EmptyOrder);
        }
        Ok(Order { id, customer_id, items, status: OrderStatus::Placed })
    }
}

// ─── Application Layer: Ports ────────────────────────────────────────────────

/// PlaceOrderCommand is the input to the use case.
/// It is defined in the application layer, not the domain.
pub struct PlaceOrderCommand {
    pub order_id: OrderId,
    pub customer_id: CustomerId,
    pub items: Vec<OrderItem>,
}

pub struct PlaceOrderResult {
    pub order_id: OrderId,
    pub status: OrderStatus,
}

/// OrderService is the PRIMARY PORT.
/// Any external driver (HTTP, gRPC, CLI, test) calls this trait.
pub trait OrderService: Send + Sync {
    fn place_order(&self, cmd: PlaceOrderCommand) -> Result<PlaceOrderResult, ApplicationError>;
    fn get_order(&self, id: &OrderId) -> Result<Order, ApplicationError>;
}

/// OrderRepository is a SECONDARY PORT.
/// The application depends on this abstraction; infrastructure implements it.
pub trait OrderRepository: Send + Sync {
    fn save(&self, order: &Order) -> Result<(), RepositoryError>;
    fn find_by_id(&self, id: &OrderId) -> Result<Order, RepositoryError>;
}

/// NotificationPort is a SECONDARY PORT for external notifications.
pub trait NotificationPort: Send + Sync {
    fn notify_order_placed(&self, order: &Order) -> Result<(), String>;
}

#[derive(Debug)]
pub enum ApplicationError {
    Domain(DomainError),
    Repository(RepositoryError),
    NotFound(String),
}

impl From<DomainError> for ApplicationError {
    fn from(e: DomainError) -> Self { ApplicationError::Domain(e) }
}

impl From<RepositoryError> for ApplicationError {
    fn from(e: RepositoryError) -> Self { ApplicationError::Repository(e) }
}

impl std::fmt::Display for ApplicationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ApplicationError::Domain(e) => write!(f, "domain error: {e}"),
            ApplicationError::Repository(e) => write!(f, "repository error: {e}"),
            ApplicationError::NotFound(id) => write!(f, "not found: {id}"),
        }
    }
}

#[derive(Debug)]
pub enum RepositoryError {
    NotFound(String),
    StorageFailure(String),
}

impl std::fmt::Display for RepositoryError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            RepositoryError::NotFound(id) => write!(f, "order {id} not found"),
            RepositoryError::StorageFailure(msg) => write!(f, "storage failure: {msg}"),
        }
    }
}

// ─── Application Service (implements OrderService) ───────────────────────────

pub struct OrderApplicationService {
    repository: Arc<dyn OrderRepository>,
    notifications: Arc<dyn NotificationPort>,
}

impl OrderApplicationService {
    pub fn new(
        repository: Arc<dyn OrderRepository>,
        notifications: Arc<dyn NotificationPort>,
    ) -> Self {
        OrderApplicationService { repository, notifications }
    }
}

impl OrderService for OrderApplicationService {
    fn place_order(&self, cmd: PlaceOrderCommand) -> Result<PlaceOrderResult, ApplicationError> {
        let order = Order::new(cmd.order_id, cmd.customer_id, cmd.items)?;
        self.repository.save(&order)?;

        // Best-effort notification
        if let Err(e) = self.notifications.notify_order_placed(&order) {
            eprintln!("WARNING: notification failed: {e}");
        }

        Ok(PlaceOrderResult {
            order_id: order.id,
            status: order.status,
        })
    }

    fn get_order(&self, id: &OrderId) -> Result<Order, ApplicationError> {
        self.repository
            .find_by_id(id)
            .map_err(|e| match e {
                RepositoryError::NotFound(id) => ApplicationError::NotFound(id),
                e => ApplicationError::Repository(e),
            })
    }
}

// ─── Infrastructure Adapters ──────────────────────────────────────────────────

/// InMemoryOrderRepository is a SECONDARY ADAPTER.
/// It implements the OrderRepository port using an in-memory HashMap.
pub struct InMemoryOrderRepository {
    orders: Mutex<HashMap<String, Order>>,
}

impl InMemoryOrderRepository {
    pub fn new() -> Self {
        InMemoryOrderRepository { orders: Mutex::new(HashMap::new()) }
    }
}

impl OrderRepository for InMemoryOrderRepository {
    fn save(&self, order: &Order) -> Result<(), RepositoryError> {
        self.orders.lock().unwrap().insert(order.id.0.clone(), order.clone());
        Ok(())
    }

    fn find_by_id(&self, id: &OrderId) -> Result<Order, RepositoryError> {
        self.orders
            .lock()
            .unwrap()
            .get(&id.0)
            .cloned()
            .ok_or_else(|| RepositoryError::NotFound(id.0.clone()))
    }
}

pub struct StdoutNotificationAdapter;

impl NotificationPort for StdoutNotificationAdapter {
    fn notify_order_placed(&self, order: &Order) -> Result<(), String> {
        println!("[NOTIFICATION] Order {} placed for customer {}",
            order.id.0, order.customer_id.0);
        Ok(())
    }
}

// ─── Primary Adapters (Transports) ───────────────────────────────────────────

/// HttpAdapter is a PRIMARY ADAPTER. In production, this would use actix-web
/// or axum handlers. Here we show the structural pattern.
pub struct HttpAdapter {
    service: Arc<dyn OrderService>,
}

impl HttpAdapter {
    pub fn new(service: Arc<dyn OrderService>) -> Self {
        HttpAdapter { service }
    }

    /// Simulates handling a POST /orders request.
    pub fn handle_place_order(
        &self,
        customer_id: &str,
        product_id: &str,
        quantity: u32,
        price_cents: i64,
    ) -> Result<String, String> {
        let cmd = PlaceOrderCommand {
            order_id: OrderId("order-http-001".to_string()),
            customer_id: CustomerId(customer_id.to_string()),
            items: vec![OrderItem {
                product_id: product_id.to_string(),
                quantity,
                price_cents,
            }],
        };

        match self.service.place_order(cmd) {
            Ok(result) => Ok(format!("201 Created: order_id={}", result.order_id.0)),
            Err(ApplicationError::Domain(DomainError::EmptyOrder)) => {
                Err("422 Unprocessable: order must have items".to_string())
            }
            Err(e) => Err(format!("500 Internal Error: {e}")),
        }
    }
}

/// CliAdapter is another PRIMARY ADAPTER — same service, different driving interface.
pub struct CliAdapter {
    service: Arc<dyn OrderService>,
}

impl CliAdapter {
    pub fn new(service: Arc<dyn OrderService>) -> Self {
        CliAdapter { service }
    }

    pub fn run_interactive(&self) {
        println!("CLI: placing order...");
        match self.service.place_order(PlaceOrderCommand {
            order_id: OrderId("order-cli-001".to_string()),
            customer_id: CustomerId("customer-cli".to_string()),
            items: vec![OrderItem {
                product_id: "prod-book".to_string(),
                quantity: 3,
                price_cents: 1499,
            }],
        }) {
            Ok(result) => println!("CLI: order {} placed", result.order_id.0),
            Err(e) => println!("CLI error: {e}"),
        }
    }
}

// ─── Composition Root ─────────────────────────────────────────────────────────

fn main() {
    // Wire secondary adapters
    let repository: Arc<dyn OrderRepository> = Arc::new(InMemoryOrderRepository::new());
    let notifications: Arc<dyn NotificationPort> = Arc::new(StdoutNotificationAdapter);

    // Create the application service with injected secondary ports
    let service: Arc<dyn OrderService> = Arc::new(
        OrderApplicationService::new(repository, notifications)
    );

    // Wire primary adapters — same service instance, different transports
    let cli = CliAdapter::new(Arc::clone(&service));
    let http = HttpAdapter::new(Arc::clone(&service));

    cli.run_interactive();
    match http.handle_place_order("customer-http", "prod-widget", 2, 999) {
        Ok(msg) => println!("HTTP: {msg}"),
        Err(err) => println!("HTTP error: {err}"),
    }
}
```

### Rust-specific considerations

`Arc<dyn Trait>` is the idiomatic Rust way to inject secondary ports into the application service. The `Arc` provides shared ownership; `dyn Trait` provides dynamic dispatch. The `Send + Sync` bounds on the traits ensure the service can be used safely across threads (Tokio tasks, Actix workers).

Rust's `From<DomainError> for ApplicationError` implementation is how errors cross layer boundaries cleanly. The application service uses `?` to propagate domain errors, and `From` automatically wraps them. This keeps error handling at layer boundaries explicit and avoids `unwrap()` chains.

The composition root in Rust is more verbose than in Go because of explicit `Arc::clone` calls for shared ownership, but the intent is identical: one place that wires concrete types to abstract ports. In production Rust applications, the `dependency-injection` crate or simply structured modules with factory functions serve as the composition root.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Port definition | Interface in application package | Trait in application module |
| Implicit vs explicit | Implicit interface satisfaction | Explicit `impl Trait for Type` |
| Dependency injection | Function parameter or constructor injection | `Arc<dyn Trait>` + constructor |
| Error crossing layers | `errors.As` / `fmt.Errorf("%w", ...)` | `From` impl + `?` operator |
| Primary adapter threading | Goroutine-safe if interfaces are | `Send + Sync` bounds on traits |
| Composition root complexity | Low; Go DI is manual or via `wire` | Moderate; `Arc::clone` verbose |
| Test doubles | Interface + mock struct | Trait + mock struct |
| Performance overhead | Interface = single vtable lookup | `dyn Trait` = vtable; `impl Trait` = monomorphic (zero cost) |

## Production War Stories

**Netflix and the "Clean Architecture" migration**: Netflix's engineering blog documents a migration of their recommendation system from a monolith to a hexagonal structure. The critical enabler was that every external dependency (their own microservices, third-party data providers) was behind a port. When a third-party movie metadata provider changed their API, only the adapter changed. The domain logic — "recommend this movie because the user watched three similar ones" — was untouched.

**Pivotal's testing strategy**: Pivotal's engineering methodology (which influenced Spring Framework's hexagonal patterns) mandated that all business logic be testable without starting a server or connecting to a database. This was enforced architecturally: the application service accepted only interface parameters, making in-memory adapters the default for unit tests. The result was test suites that ran in under a second, enabling TDD at scale.

**The Strangler Fig at a legacy bank**: A common pattern for legacy migration is using Hexagonal Architecture as the receiving end. The new system defines ports; legacy adapters implement them, routing calls to the old system. As the legacy system is replaced piece by piece, only the adapters change — the domain and application logic remain stable. This is the Strangler Fig pattern (see microservices section) combined with Hexagonal Architecture.

**Go clean architecture at DigitalOcean**: DigitalOcean's engineering blog describes adopting clean architecture (a variant of hexagonal) for their droplet management service. The key win was independent deployment: the HTTP API team and the database team could work on their respective adapters without coordinating, because the port interface was stable.

## Architectural Trade-offs

**When to use Hexagonal Architecture:**
- Business logic is complex enough to be worth isolating (DDD aggregates, complex rules)
- Multiple transports are needed or expected (REST + gRPC, HTTP + message queue)
- Infrastructure is expected to change (cloud provider migration, ORM swap)
- Test speed matters — you want unit tests that run in milliseconds
- Team is large enough that domain and infrastructure are worked on by different people

**When NOT to use Hexagonal Architecture:**
- Simple CRUD applications where there is no real business logic to isolate
- Throwaway prototypes — the upfront structure cost is not justified for code you will discard
- Very small teams where the separation creates more indirection than clarity
- Systems with one transport and one data store that will never change — the abstraction provides no practical benefit

**The indirection cost**: Every port is an interface, and every interface is a layer of indirection. For a developer reading the code, "where does `OrderRepository.Save` actually write data?" requires following the wiring from the composition root. For teams unfamiliar with the pattern, this is genuinely disorienting. The payoff comes when the adapter needs to change — but until that day, the cost is navigational overhead. Document the composition root clearly.

## Common Pitfalls

**1. Letting infrastructure types leak into ports.** A port that returns `*sql.Rows` or takes a `*redis.Client` parameter is not a port — it is a thin wrapper around infrastructure. Ports speak domain language: `OrderRepository.FindByCustomer(CustomerID) ([]*Order, error)`, not `FindByCustomer(customerID string) ([]map[string]interface{}, error)`.

**2. Putting business logic in adapters.** An HTTP adapter that checks "if the order total is over $1000, apply a discount" is doing the application service's job. Adapters translate; they do not decide. All business logic belongs in the domain or application layer, where it is testable without infrastructure.

**3. Making ports too granular.** One interface per method (`SaveOrder`, `FindOrder`, `ListOrders` as separate interfaces) is technically correct but creates a composition explosion. Group related methods into one port per responsibility. `OrderRepository` with three methods is more navigable than three single-method interfaces.

**4. Skipping the composition root pattern.** When adapters are instantiated inside the application service or domain (e.g., `svc.repo = &PostgresOrderRepository{}`), you have coupled the domain to infrastructure and lost the ability to inject alternatives. The composition root discipline must be enforced — the `new` of any adapter belongs in `main`, not in business logic.

**5. Testing through the HTTP adapter.** Integration tests that make HTTP calls to test business logic bypass the primary port and couple your tests to the HTTP transport. Test business logic directly through the application service (primary port); reserve HTTP-level tests for the adapter itself (that it correctly translates requests and responses).

## Exercises

**Exercise 1** (30 min): Trace through the Go implementation. Add a `CancelOrderCommand` and implement it in the application service, using the `OrderRepository` to load and update the order. Add a `CancelOrderHandler` to the HTTP adapter.

**Exercise 2** (2–4h): Implement a PostgreSQL adapter for `OrderRepository`. Use the `database/sql` package (Go) or `sqlx`/`diesel` (Rust). Verify that the application service tests pass without modification — you only swap the adapter in the composition root.

**Exercise 3** (4–8h): Add a gRPC primary adapter alongside the HTTP adapter. Define the proto service. The gRPC handler calls the same `OrderService` interface. Verify that both HTTP and gRPC handlers work against the same in-memory repository in a test.

**Exercise 4** (8–15h): Implement a full service with three secondary ports (repository, notification, payment gateway) and two primary adapters (HTTP and a message queue consumer). Use a real message broker (NATS or RabbitMQ). The service should handle commands arriving via both HTTP and message queue, persisting to a real database, and publishing notifications. All business logic should remain testable with in-memory adapters.

## Further Reading

### Foundational Books

- **Clean Architecture** — Robert C. Martin (2017). "Clean Architecture" is Martin's restatement of hexagonal architecture with the Dependency Rule: source code dependencies always point inward.
- **Growing Object-Oriented Software, Guided by Tests** (Freeman & Pryce, 2009) — the ports-and-adapters pattern emerges naturally from test-driven development at the system level.

### Blog Posts and Case Studies

- Alistair Cockburn: "Hexagonal Architecture" (2005) — alistair.cockburn.us/hexagonal-architecture. The original article. Short and still the clearest statement of the pattern.
- Netflix Tech Blog: "The Netflix Tech Blog" — medium.com/netflix-techblog. Search for "clean architecture" or "hexagonal."
- Herberto Graca: "The Software Architecture Chronicles" — hgraca.com/tech-stories. An exhaustive history of software architecture patterns, with hexagonal as the culmination.

### Production Code to Read

- **eShopOnContainers** — github.com/dotnet-architecture/eShopOnContainers. Each microservice follows hexagonal architecture with explicit port interfaces.
- **go-clean-arch** — github.com/bxcodec/go-clean-arch. A popular Go template for clean architecture with HTTP, database, and caching adapters.

### Talks

- Alistair Cockburn: "Alistair in the Hexagone" (2017) — YouTube. Cockburn revisits his pattern 12 years later, addresses common misunderstandings.
- Robert Martin: "Clean Architecture" (NDC 2012) — YouTube. Martin's version of the same insight with the concentric rings diagram.
