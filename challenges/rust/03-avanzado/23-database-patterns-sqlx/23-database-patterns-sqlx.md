# 23. Database Patterns

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Exercise 13 (serde and serialization)
- Exercise 22 (networking with tokio, axum HTTP)
- Familiarity with SQL (CREATE TABLE, SELECT, INSERT, JOIN, transactions)
- A running PostgreSQL instance (local or Docker)

## Learning Objectives

- Use sqlx compile-time checked queries against a real database schema
- Configure and tune connection pools for production workloads
- Implement transactions with proper isolation levels and rollback semantics
- Run and manage database migrations with sqlx-cli
- Design the repository pattern to decouple business logic from persistence
- Compare query builders, raw SQL, and ORMs with concrete trade-off analysis
- Handle database errors idiomatically with typed error hierarchies
- Build async database operations that compose with tokio's runtime

## Concepts

### Part 1: sqlx Fundamentals

sqlx is an async SQL toolkit for Rust. Its defining feature is compile-time query verification: the `query!` macro connects to your database at compile time, checks that your SQL is valid, and infers Rust types from the schema. No ORM. No runtime reflection.

```rust
use sqlx::postgres::PgPoolOptions;
use sqlx::{PgPool, FromRow};

#[derive(Debug, FromRow)]
struct User {
    id: i64,
    email: String,
    name: String,
    created_at: chrono::NaiveDateTime,
}

// Compile-time checked: if the SQL is invalid or the columns don't match,
// this fails at compile time, not at runtime.
async fn get_user_by_id(pool: &PgPool, user_id: i64) -> sqlx::Result<Option<User>> {
    sqlx::query_as!(
        User,
        r#"SELECT id, email, name, created_at FROM users WHERE id = $1"#,
        user_id
    )
    .fetch_optional(pool)
    .await
}

// query! returns an anonymous struct with typed fields.
// No need to define a struct if you only need a few fields.
async fn count_users(pool: &PgPool) -> sqlx::Result<i64> {
    let row = sqlx::query!(r#"SELECT COUNT(*) as "count!" FROM users"#)
        .fetch_one(pool)
        .await?;
    Ok(row.count)
}
```

The `"count!"` syntax in the query tells sqlx "this column is NOT NULL even though COUNT(*) returns Option<i64> by default." The `!` suffix overrides nullability inference.

Other type overrides:
- `"column_name: Type"` -- force the Rust type (e.g., `"id: i32"` when the column is BIGINT but you want i32)
- `"column_name?"` -- force the column to be `Option<T>`
- `"column_name!"` -- force the column to be non-nullable `T`

### Compile-Time Verification Setup

sqlx checks queries against a live database at compile time. You provide the connection string via the `DATABASE_URL` environment variable:

```bash
# .env file (read by sqlx automatically)
DATABASE_URL=postgres://user:password@localhost:5432/mydb
```

For CI environments where a database is not available, sqlx supports offline mode. Run `cargo sqlx prepare` locally to generate a `.sqlx/` directory with cached query metadata. Check this directory into version control:

```bash
# Generate offline query data
cargo sqlx prepare

# Compile without a database (CI)
SQLX_OFFLINE=true cargo build
```

### Part 2: Connection Pools

Every database connection is expensive: TCP handshake, TLS negotiation, authentication, session setup. A connection pool maintains a set of open connections and lends them out to callers.

```rust
use sqlx::postgres::PgPoolOptions;

async fn create_pool() -> PgPool {
    PgPoolOptions::new()
        // Maximum connections in the pool.
        // Rule of thumb: connections = (2 * cpu_cores) + disk_spindles
        // For SSD-backed Postgres: 10-20 is a good starting point.
        .max_connections(20)

        // Minimum idle connections to keep alive.
        // Prevents cold-start latency after idle periods.
        .min_connections(5)

        // How long to wait for a connection before giving up.
        // Set this to your API timeout minus processing time.
        .acquire_timeout(std::time::Duration::from_secs(3))

        // How long a connection can live before being recycled.
        // Prevents issues with stale server-side state.
        .max_lifetime(std::time::Duration::from_mins(30))

        // How long an idle connection stays in the pool.
        .idle_timeout(std::time::Duration::from_mins(10))

        // Run a test query after acquiring to verify the connection is alive.
        .after_connect(|conn, _meta| {
            Box::pin(async move {
                sqlx::query("SET application_name = 'my-service'")
                    .execute(&mut *conn)
                    .await?;
                Ok(())
            })
        })

        .connect(&std::env::var("DATABASE_URL").expect("DATABASE_URL must be set"))
        .await
        .expect("failed to create pool")
}
```

**Pool sizing trade-offs:**
- Too few connections: requests queue up waiting for a free connection. Latency spikes under load.
- Too many connections: each Postgres connection consumes ~10MB of RAM on the server side. 100 connections = 1GB of server memory just for connections. Context-switching overhead degrades throughput.
- The optimal pool size is almost always smaller than you think. A pool of 10 connections can handle thousands of requests per second if queries are fast.

### Part 3: Transactions

A transaction groups multiple operations into an atomic unit. Either all succeed or all are rolled back.

```rust
use sqlx::{PgPool, Postgres, Transaction};

async fn transfer_funds(
    pool: &PgPool,
    from_id: i64,
    to_id: i64,
    amount: i64,
) -> sqlx::Result<()> {
    // Begin a transaction. This acquires a connection from the pool.
    let mut tx: Transaction<'_, Postgres> = pool.begin().await?;

    // Debit the sender. FOR UPDATE locks the row until the transaction commits.
    let from_balance = sqlx::query_scalar!(
        r#"UPDATE accounts SET balance = balance - $1
           WHERE id = $2 AND balance >= $1
           RETURNING balance"#,
        amount,
        from_id
    )
    .fetch_optional(&mut *tx)
    .await?;

    // If the UPDATE matched no rows, the sender has insufficient funds.
    let from_balance = from_balance.ok_or_else(|| {
        sqlx::Error::Protocol("insufficient funds or account not found".into())
    })?;

    // Credit the receiver
    sqlx::query!(
        r#"UPDATE accounts SET balance = balance + $1 WHERE id = $2"#,
        amount,
        to_id
    )
    .execute(&mut *tx)
    .await?;

    // Record the transfer
    sqlx::query!(
        r#"INSERT INTO transfers (from_id, to_id, amount, created_at)
           VALUES ($1, $2, $3, NOW())"#,
        from_id,
        to_id,
        amount
    )
    .execute(&mut *tx)
    .await?;

    // Commit. If this line is not reached (early return via ?), the
    // transaction is rolled back when `tx` is dropped.
    tx.commit().await?;

    Ok(())
}
```

**Isolation levels** control what concurrent transactions can see:

```rust
use sqlx::postgres::PgPool;

async fn serializable_operation(pool: &PgPool) -> sqlx::Result<()> {
    let mut tx = pool.begin().await?;

    // Set isolation level for this transaction
    sqlx::query("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
        .execute(&mut *tx)
        .await?;

    // Operations here see a consistent snapshot.
    // If a conflicting transaction commits, this one gets a
    // serialization failure and must be retried.

    tx.commit().await?;
    Ok(())
}
```

| Isolation Level | Dirty Reads | Non-Repeatable Reads | Phantom Reads | Performance |
|---|---|---|---|---|
| READ UNCOMMITTED | Yes | Yes | Yes | Fastest |
| READ COMMITTED (Postgres default) | No | Yes | Yes | Fast |
| REPEATABLE READ | No | No | Yes (No in Postgres) | Moderate |
| SERIALIZABLE | No | No | No | Slowest |

### Part 4: Migrations

sqlx-cli manages schema migrations as versioned SQL files:

```bash
# Install sqlx-cli
cargo install sqlx-cli --no-default-features --features postgres

# Create the migrations directory and a new migration
sqlx migrate add create_users
# Creates: migrations/20240101120000_create_users.sql

# Run pending migrations
sqlx migrate run

# Revert the last migration
sqlx migrate revert
```

Migration file:

```sql
-- migrations/20240101120000_create_users.sql

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
```

You can also write reversible migrations by splitting into `up.sql` and `down.sql`:

```bash
sqlx migrate add -r create_users
# Creates:
#   migrations/20240101120000_create_users.up.sql
#   migrations/20240101120000_create_users.down.sql
```

Run migrations programmatically at application startup:

```rust
use sqlx::migrate::Migrator;
use sqlx::PgPool;

static MIGRATOR: Migrator = sqlx::migrate!(); // embeds migrations at compile time

async fn run_migrations(pool: &PgPool) {
    MIGRATOR.run(pool).await.expect("migration failed");
}
```

**Migration trade-offs:**
- Running migrations at startup is simple but dangerous in multi-instance deployments. Two instances may race. Use advisory locks or run migrations in a separate init step.
- Irreversible migrations (no down file) are actually safer in production. Down migrations can destroy data and are rarely tested. Prefer forward-only migrations that are additive.
- Always make migrations backward-compatible. Add columns with defaults, never rename columns in a single step. Deploy in stages: add new column, deploy code that writes both, backfill, deploy code that reads new, drop old.

### Part 5: The Repository Pattern

The repository pattern abstracts persistence behind a trait. Business logic depends on the trait, not on sqlx directly. This enables testing with in-memory implementations and swapping databases.

```rust
use async_trait::async_trait;

#[derive(Debug, Clone)]
pub struct User {
    pub id: i64,
    pub email: String,
    pub name: String,
}

#[derive(Debug)]
pub struct CreateUser {
    pub email: String,
    pub name: String,
    pub password_hash: String,
}

#[derive(Debug, thiserror::Error)]
pub enum RepoError {
    #[error("not found")]
    NotFound,
    #[error("duplicate entry: {0}")]
    Duplicate(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

#[async_trait]
pub trait UserRepository: Send + Sync {
    async fn find_by_id(&self, id: i64) -> Result<User, RepoError>;
    async fn find_by_email(&self, email: &str) -> Result<User, RepoError>;
    async fn create(&self, input: CreateUser) -> Result<User, RepoError>;
    async fn delete(&self, id: i64) -> Result<(), RepoError>;
    async fn list(&self, limit: i64, offset: i64) -> Result<Vec<User>, RepoError>;
}
```

The sqlx implementation:

```rust
use sqlx::PgPool;

pub struct PgUserRepository {
    pool: PgPool,
}

impl PgUserRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl UserRepository for PgUserRepository {
    async fn find_by_id(&self, id: i64) -> Result<User, RepoError> {
        sqlx::query_as!(
            User,
            r#"SELECT id, email, name FROM users WHERE id = $1"#,
            id
        )
        .fetch_optional(&self.pool)
        .await?
        .ok_or(RepoError::NotFound)
    }

    async fn find_by_email(&self, email: &str) -> Result<User, RepoError> {
        sqlx::query_as!(
            User,
            r#"SELECT id, email, name FROM users WHERE email = $1"#,
            email
        )
        .fetch_optional(&self.pool)
        .await?
        .ok_or(RepoError::NotFound)
    }

    async fn create(&self, input: CreateUser) -> Result<User, RepoError> {
        sqlx::query_as!(
            User,
            r#"INSERT INTO users (email, name, password_hash)
               VALUES ($1, $2, $3)
               RETURNING id, email, name"#,
            input.email,
            input.name,
            input.password_hash
        )
        .fetch_one(&self.pool)
        .await
        .map_err(|e| match &e {
            sqlx::Error::Database(db_err) if db_err.constraint() == Some("users_email_key") => {
                RepoError::Duplicate(input.email.clone())
            }
            _ => RepoError::Database(e),
        })
    }

    async fn delete(&self, id: i64) -> Result<(), RepoError> {
        let result = sqlx::query!(r#"DELETE FROM users WHERE id = $1"#, id)
            .execute(&self.pool)
            .await?;

        if result.rows_affected() == 0 {
            return Err(RepoError::NotFound);
        }
        Ok(())
    }

    async fn list(&self, limit: i64, offset: i64) -> Result<Vec<User>, RepoError> {
        let users = sqlx::query_as!(
            User,
            r#"SELECT id, email, name FROM users ORDER BY id LIMIT $1 OFFSET $2"#,
            limit,
            offset
        )
        .fetch_all(&self.pool)
        .await?;

        Ok(users)
    }
}
```

An in-memory implementation for tests:

```rust
use std::collections::HashMap;
use std::sync::atomic::{AtomicI64, Ordering};
use tokio::sync::RwLock;

pub struct InMemoryUserRepository {
    store: RwLock<HashMap<i64, User>>,
    next_id: AtomicI64,
}

impl InMemoryUserRepository {
    pub fn new() -> Self {
        Self {
            store: RwLock::new(HashMap::new()),
            next_id: AtomicI64::new(1),
        }
    }
}

#[async_trait]
impl UserRepository for InMemoryUserRepository {
    async fn find_by_id(&self, id: i64) -> Result<User, RepoError> {
        self.store
            .read()
            .await
            .get(&id)
            .cloned()
            .ok_or(RepoError::NotFound)
    }

    async fn find_by_email(&self, email: &str) -> Result<User, RepoError> {
        self.store
            .read()
            .await
            .values()
            .find(|u| u.email == email)
            .cloned()
            .ok_or(RepoError::NotFound)
    }

    async fn create(&self, input: CreateUser) -> Result<User, RepoError> {
        let mut store = self.store.write().await;

        // Check uniqueness
        if store.values().any(|u| u.email == input.email) {
            return Err(RepoError::Duplicate(input.email));
        }

        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        let user = User {
            id,
            email: input.email,
            name: input.name,
        };
        store.insert(id, user.clone());
        Ok(user)
    }

    async fn delete(&self, id: i64) -> Result<(), RepoError> {
        self.store
            .write()
            .await
            .remove(&id)
            .map(|_| ())
            .ok_or(RepoError::NotFound)
    }

    async fn list(&self, limit: i64, offset: i64) -> Result<Vec<User>, RepoError> {
        let store = self.store.read().await;
        let users: Vec<User> = store
            .values()
            .skip(offset as usize)
            .take(limit as usize)
            .cloned()
            .collect();
        Ok(users)
    }
}
```

### Part 6: Query Builders vs Raw SQL

Three approaches to constructing queries, each with distinct trade-offs:

**Approach 1: Raw SQL with sqlx (compile-time checked)**

```rust
// Pros: full SQL power, compile-time verification, zero runtime overhead
// Cons: no composability, conditional clauses are awkward
async fn search_users_raw(
    pool: &PgPool,
    name_filter: Option<&str>,
    email_filter: Option<&str>,
) -> sqlx::Result<Vec<User>> {
    // You cannot conditionally add WHERE clauses with query_as!
    // You must write the full query with COALESCE or NULL checks:
    sqlx::query_as!(
        User,
        r#"SELECT id, email, name FROM users
           WHERE ($1::text IS NULL OR name ILIKE '%' || $1 || '%')
             AND ($2::text IS NULL OR email ILIKE '%' || $2 || '%')
           ORDER BY id"#,
        name_filter,
        email_filter
    )
    .fetch_all(pool)
    .await
}
```

**Approach 2: Dynamic query building with sqlx QueryBuilder**

```rust
use sqlx::QueryBuilder;

// Pros: composable, dynamic WHERE clauses, still parameterized (no SQL injection)
// Cons: no compile-time checking, runtime type mismatches are possible
async fn search_users_builder(
    pool: &PgPool,
    name_filter: Option<&str>,
    email_filter: Option<&str>,
    sort_by: &str,
    limit: i64,
) -> sqlx::Result<Vec<User>> {
    let mut qb: QueryBuilder<'_, sqlx::Postgres> =
        QueryBuilder::new("SELECT id, email, name FROM users WHERE 1=1");

    if let Some(name) = name_filter {
        qb.push(" AND name ILIKE ");
        qb.push_bind(format!("%{name}%"));
    }
    if let Some(email) = email_filter {
        qb.push(" AND email ILIKE ");
        qb.push_bind(format!("%{email}%"));
    }

    // Validate sort column to prevent injection (push, not push_bind, for identifiers)
    let sort_col = match sort_by {
        "name" | "email" | "created_at" => sort_by,
        _ => "id",
    };
    qb.push(format!(" ORDER BY {sort_col} LIMIT "));
    qb.push_bind(limit);

    qb.build_query_as::<User>()
        .fetch_all(pool)
        .await
}
```

**Approach 3: Bulk inserts with QueryBuilder**

```rust
async fn bulk_insert_users(
    pool: &PgPool,
    users: &[CreateUser],
) -> sqlx::Result<()> {
    if users.is_empty() {
        return Ok(());
    }

    let mut qb: QueryBuilder<'_, sqlx::Postgres> =
        QueryBuilder::new("INSERT INTO users (email, name, password_hash) ");

    qb.push_values(users, |mut b, user| {
        b.push_bind(&user.email)
         .push_bind(&user.name)
         .push_bind(&user.password_hash);
    });

    // Postgres bind parameter limit is 65535.
    // With 3 columns per row, max ~21845 rows per batch.

    qb.build().execute(pool).await?;
    Ok(())
}
```

**Trade-off summary:**

| Dimension | query!/query_as! | QueryBuilder | ORM (sea-orm, diesel) |
|---|---|---|---|
| **Compile-time safety** | Full (SQL + types) | None | Partial (schema types) |
| **Dynamic queries** | Awkward (NULL coalescing) | Natural | Natural |
| **SQL expressiveness** | Full SQL | Full SQL | Subset (escape hatch needed) |
| **Learning curve** | Know SQL + sqlx macros | Know SQL + builder API | Learn ORM DSL |
| **Refactoring** | Rename column = compile error | Rename column = runtime error | Depends on ORM |
| **Performance** | Zero overhead | Minimal (string building) | Query generation overhead |
| **Bulk operations** | Manual VALUES | push_values helper | Varies |

### Part 7: Error Handling Patterns

Database errors need structured handling. Map sqlx errors to domain errors:

```rust
use sqlx::Error as SqlxError;

#[derive(Debug, thiserror::Error)]
pub enum DbError {
    #[error("record not found")]
    NotFound,

    #[error("unique constraint violated: {field}")]
    UniqueViolation { field: String },

    #[error("foreign key violation: {detail}")]
    ForeignKeyViolation { detail: String },

    #[error("connection pool exhausted")]
    PoolExhausted,

    #[error("query timeout")]
    Timeout,

    #[error("database error: {0}")]
    Other(#[source] SqlxError),
}

impl From<SqlxError> for DbError {
    fn from(e: SqlxError) -> Self {
        match &e {
            SqlxError::RowNotFound => DbError::NotFound,

            SqlxError::Database(db_err) => {
                // Postgres error codes: https://www.postgresql.org/docs/current/errcodes-appendix.html
                match db_err.code().as_deref() {
                    Some("23505") => DbError::UniqueViolation {
                        field: db_err.constraint().unwrap_or("unknown").to_string(),
                    },
                    Some("23503") => DbError::ForeignKeyViolation {
                        detail: db_err.detail().unwrap_or("").to_string(),
                    },
                    _ => DbError::Other(e),
                }
            }

            SqlxError::PoolTimedOut => DbError::PoolExhausted,

            SqlxError::Io(_) => DbError::Other(e),

            _ => DbError::Other(e),
        }
    }
}
```

### Retry Logic for Transient Errors

```rust
use std::time::Duration;

async fn with_retry<F, Fut, T>(max_retries: u32, mut f: F) -> Result<T, DbError>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<T, DbError>>,
{
    let mut attempt = 0;
    loop {
        match f().await {
            Ok(val) => return Ok(val),
            Err(e) if is_retryable(&e) && attempt < max_retries => {
                attempt += 1;
                let backoff = Duration::from_millis(100 * 2u64.pow(attempt));
                tokio::time::sleep(backoff).await;
            }
            Err(e) => return Err(e),
        }
    }
}

fn is_retryable(e: &DbError) -> bool {
    matches!(
        e,
        DbError::PoolExhausted | DbError::Timeout | DbError::Other(_)
    )
}
```

## Exercises

### Exercise 1: Repository with Transactions

Build a complete repository layer for an order management system with two entities: `Order` and `OrderItem`. An order contains multiple items. Creating an order must insert the order header and all items in a single transaction. If any item fails, the entire order is rolled back.

**Cargo.toml:**
```toml
[package]
name = "database-patterns"
edition = "2024"

[dependencies]
sqlx = { version = "0.8", features = ["runtime-tokio", "postgres", "chrono", "migrate"] }
tokio = { version = "1", features = ["full"] }
chrono = { version = "0.4", features = ["serde"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
async-trait = "0.1"
thiserror = "2"
uuid = { version = "1", features = ["v4"] }
```

**Schema:**
```sql
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_email VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    total_cents BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_name VARCHAR(255) NOT NULL,
    quantity INT NOT NULL CHECK (quantity > 0),
    price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_items_order_id ON order_items(order_id);
```

**Constraints:**
- Define `OrderRepository` trait with: `create_order`, `get_order_with_items`, `update_status`, `list_by_customer`
- `create_order` must use a transaction that inserts the order, all items, and updates the total
- Implement both `PgOrderRepository` (sqlx) and `InMemoryOrderRepository` (for tests)
- Map constraint violations to typed errors

<details>
<summary>Solution</summary>

```rust
use async_trait::async_trait;
use chrono::NaiveDateTime;
use sqlx::{PgPool, Postgres, Transaction};
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct Order {
    pub id: Uuid,
    pub customer_email: String,
    pub status: String,
    pub total_cents: i64,
    pub created_at: NaiveDateTime,
}

#[derive(Debug, Clone)]
pub struct OrderItem {
    pub id: Uuid,
    pub order_id: Uuid,
    pub product_name: String,
    pub quantity: i32,
    pub price_cents: i64,
}

#[derive(Debug)]
pub struct CreateOrderInput {
    pub customer_email: String,
    pub items: Vec<CreateItemInput>,
}

#[derive(Debug)]
pub struct CreateItemInput {
    pub product_name: String,
    pub quantity: i32,
    pub price_cents: i64,
}

#[derive(Debug)]
pub struct OrderWithItems {
    pub order: Order,
    pub items: Vec<OrderItem>,
}

#[derive(Debug, thiserror::Error)]
pub enum OrderRepoError {
    #[error("order not found")]
    NotFound,
    #[error("empty order: must have at least one item")]
    EmptyOrder,
    #[error("invalid item: {0}")]
    InvalidItem(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

#[async_trait]
pub trait OrderRepository: Send + Sync {
    async fn create_order(&self, input: CreateOrderInput) -> Result<OrderWithItems, OrderRepoError>;
    async fn get_order_with_items(&self, id: Uuid) -> Result<OrderWithItems, OrderRepoError>;
    async fn update_status(&self, id: Uuid, status: &str) -> Result<Order, OrderRepoError>;
    async fn list_by_customer(&self, email: &str) -> Result<Vec<Order>, OrderRepoError>;
}

pub struct PgOrderRepository {
    pool: PgPool,
}

impl PgOrderRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl OrderRepository for PgOrderRepository {
    async fn create_order(&self, input: CreateOrderInput) -> Result<OrderWithItems, OrderRepoError> {
        if input.items.is_empty() {
            return Err(OrderRepoError::EmptyOrder);
        }

        let total_cents: i64 = input
            .items
            .iter()
            .map(|item| item.price_cents * item.quantity as i64)
            .sum();

        let mut tx: Transaction<'_, Postgres> = self.pool.begin().await?;

        // Insert order header
        let order = sqlx::query_as!(
            Order,
            r#"INSERT INTO orders (customer_email, total_cents)
               VALUES ($1, $2)
               RETURNING id, customer_email, status, total_cents, created_at"#,
            input.customer_email,
            total_cents
        )
        .fetch_one(&mut *tx)
        .await?;

        // Insert all items
        let mut items = Vec::with_capacity(input.items.len());
        for item_input in &input.items {
            let item = sqlx::query_as!(
                OrderItem,
                r#"INSERT INTO order_items (order_id, product_name, quantity, price_cents)
                   VALUES ($1, $2, $3, $4)
                   RETURNING id, order_id, product_name, quantity, price_cents"#,
                order.id,
                item_input.product_name,
                item_input.quantity,
                item_input.price_cents
            )
            .fetch_one(&mut *tx)
            .await
            .map_err(|e| match &e {
                sqlx::Error::Database(db_err)
                    if db_err.code().as_deref() == Some("23514") =>
                {
                    OrderRepoError::InvalidItem(format!(
                        "constraint violation for '{}': {}",
                        item_input.product_name,
                        db_err.detail().unwrap_or("")
                    ))
                }
                _ => OrderRepoError::Database(e),
            })?;
            items.push(item);
        }

        tx.commit().await?;

        Ok(OrderWithItems { order, items })
    }

    async fn get_order_with_items(&self, id: Uuid) -> Result<OrderWithItems, OrderRepoError> {
        let order = sqlx::query_as!(
            Order,
            r#"SELECT id, customer_email, status, total_cents, created_at
               FROM orders WHERE id = $1"#,
            id
        )
        .fetch_optional(&self.pool)
        .await?
        .ok_or(OrderRepoError::NotFound)?;

        let items = sqlx::query_as!(
            OrderItem,
            r#"SELECT id, order_id, product_name, quantity, price_cents
               FROM order_items WHERE order_id = $1 ORDER BY created_at"#,
            id
        )
        .fetch_all(&self.pool)
        .await?;

        Ok(OrderWithItems { order, items })
    }

    async fn update_status(&self, id: Uuid, status: &str) -> Result<Order, OrderRepoError> {
        sqlx::query_as!(
            Order,
            r#"UPDATE orders SET status = $1 WHERE id = $2
               RETURNING id, customer_email, status, total_cents, created_at"#,
            status,
            id
        )
        .fetch_optional(&self.pool)
        .await?
        .ok_or(OrderRepoError::NotFound)
    }

    async fn list_by_customer(&self, email: &str) -> Result<Vec<Order>, OrderRepoError> {
        let orders = sqlx::query_as!(
            Order,
            r#"SELECT id, customer_email, status, total_cents, created_at
               FROM orders WHERE customer_email = $1 ORDER BY created_at DESC"#,
            email
        )
        .fetch_all(&self.pool)
        .await?;

        Ok(orders)
    }
}

// In-memory implementation for testing
pub struct InMemoryOrderRepository {
    orders: tokio::sync::RwLock<std::collections::HashMap<Uuid, Order>>,
    items: tokio::sync::RwLock<Vec<OrderItem>>,
}

impl InMemoryOrderRepository {
    pub fn new() -> Self {
        Self {
            orders: tokio::sync::RwLock::new(std::collections::HashMap::new()),
            items: tokio::sync::RwLock::new(Vec::new()),
        }
    }
}

#[async_trait]
impl OrderRepository for InMemoryOrderRepository {
    async fn create_order(&self, input: CreateOrderInput) -> Result<OrderWithItems, OrderRepoError> {
        if input.items.is_empty() {
            return Err(OrderRepoError::EmptyOrder);
        }

        let total_cents: i64 = input
            .items
            .iter()
            .map(|i| i.price_cents * i.quantity as i64)
            .sum();

        let order = Order {
            id: Uuid::new_v4(),
            customer_email: input.customer_email,
            status: "pending".to_string(),
            total_cents,
            created_at: chrono::Utc::now().naive_utc(),
        };

        let mut order_items = Vec::new();
        for item_input in &input.items {
            if item_input.quantity <= 0 {
                return Err(OrderRepoError::InvalidItem(
                    format!("quantity must be > 0 for '{}'", item_input.product_name),
                ));
            }
            order_items.push(OrderItem {
                id: Uuid::new_v4(),
                order_id: order.id,
                product_name: item_input.product_name.clone(),
                quantity: item_input.quantity,
                price_cents: item_input.price_cents,
            });
        }

        self.orders.write().await.insert(order.id, order.clone());
        self.items.write().await.extend(order_items.clone());

        Ok(OrderWithItems {
            order,
            items: order_items,
        })
    }

    async fn get_order_with_items(&self, id: Uuid) -> Result<OrderWithItems, OrderRepoError> {
        let orders = self.orders.read().await;
        let order = orders.get(&id).cloned().ok_or(OrderRepoError::NotFound)?;
        let items = self.items.read().await;
        let order_items: Vec<OrderItem> = items
            .iter()
            .filter(|i| i.order_id == id)
            .cloned()
            .collect();
        Ok(OrderWithItems {
            order,
            items: order_items,
        })
    }

    async fn update_status(&self, id: Uuid, status: &str) -> Result<Order, OrderRepoError> {
        let mut orders = self.orders.write().await;
        let order = orders.get_mut(&id).ok_or(OrderRepoError::NotFound)?;
        order.status = status.to_string();
        Ok(order.clone())
    }

    async fn list_by_customer(&self, email: &str) -> Result<Vec<Order>, OrderRepoError> {
        let orders = self.orders.read().await;
        let mut result: Vec<Order> = orders
            .values()
            .filter(|o| o.customer_email == email)
            .cloned()
            .collect();
        result.sort_by(|a, b| b.created_at.cmp(&a.created_at));
        Ok(result)
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Use in-memory repo for demonstration
    let repo = InMemoryOrderRepository::new();

    let order = repo
        .create_order(CreateOrderInput {
            customer_email: "alice@example.com".to_string(),
            items: vec![
                CreateItemInput {
                    product_name: "Widget".to_string(),
                    quantity: 3,
                    price_cents: 1500,
                },
                CreateItemInput {
                    product_name: "Gadget".to_string(),
                    quantity: 1,
                    price_cents: 4999,
                },
            ],
        })
        .await?;

    println!("created order {} with {} items", order.order.id, order.items.len());
    println!("total: ${:.2}", order.order.total_cents as f64 / 100.0);

    let updated = repo.update_status(order.order.id, "confirmed").await?;
    println!("status: {}", updated.status);

    let fetched = repo.get_order_with_items(order.order.id).await?;
    for item in &fetched.items {
        println!("  - {} x{} @ ${:.2}", item.product_name, item.quantity, item.price_cents as f64 / 100.0);
    }

    Ok(())
}
```
</details>

### Exercise 2: Dynamic Search with QueryBuilder

Build a search function that dynamically constructs queries based on user-provided filters. Support filtering users by name (partial match), email (partial match), date range (created_at), and sorting by any allowed column. Implement pagination with cursor-based pagination (not OFFSET).

**Constraints:**
- Use `sqlx::QueryBuilder` for dynamic query construction
- All user inputs must be parameterized (no string interpolation for values)
- Column names for ORDER BY must be validated against an allowlist
- Cursor-based pagination uses the last seen `id` rather than OFFSET
- Return both results and a `next_cursor` for the caller

<details>
<summary>Solution</summary>

```rust
use chrono::NaiveDateTime;
use sqlx::{PgPool, QueryBuilder, FromRow};

#[derive(Debug, Clone, FromRow)]
pub struct User {
    pub id: i64,
    pub email: String,
    pub name: String,
    pub created_at: NaiveDateTime,
}

#[derive(Debug, Default)]
pub struct UserSearch {
    pub name: Option<String>,
    pub email: Option<String>,
    pub created_after: Option<NaiveDateTime>,
    pub created_before: Option<NaiveDateTime>,
    pub sort_by: Option<String>,
    pub sort_order: Option<String>,  // "asc" or "desc"
    pub cursor: Option<i64>,         // last seen id
    pub limit: Option<i64>,
}

pub struct SearchResult {
    pub users: Vec<User>,
    pub next_cursor: Option<i64>,
}

const ALLOWED_SORT_COLUMNS: &[&str] = &["id", "name", "email", "created_at"];
const DEFAULT_LIMIT: i64 = 20;
const MAX_LIMIT: i64 = 100;

pub async fn search_users(
    pool: &PgPool,
    params: &UserSearch,
) -> sqlx::Result<SearchResult> {
    let limit = params
        .limit
        .unwrap_or(DEFAULT_LIMIT)
        .min(MAX_LIMIT);

    // Request one extra row to determine if there are more results
    let fetch_limit = limit + 1;

    let mut qb: QueryBuilder<'_, sqlx::Postgres> =
        QueryBuilder::new("SELECT id, email, name, created_at FROM users WHERE 1=1");

    if let Some(name) = &params.name {
        qb.push(" AND name ILIKE ");
        qb.push_bind(format!("%{name}%"));
    }

    if let Some(email) = &params.email {
        qb.push(" AND email ILIKE ");
        qb.push_bind(format!("%{email}%"));
    }

    if let Some(after) = params.created_after {
        qb.push(" AND created_at >= ");
        qb.push_bind(after);
    }

    if let Some(before) = params.created_before {
        qb.push(" AND created_at <= ");
        qb.push_bind(before);
    }

    // Cursor-based pagination: WHERE id > last_seen_id
    if let Some(cursor) = params.cursor {
        qb.push(" AND id > ");
        qb.push_bind(cursor);
    }

    // Validate and apply sort column
    let sort_col = params
        .sort_by
        .as_deref()
        .filter(|col| ALLOWED_SORT_COLUMNS.contains(col))
        .unwrap_or("id");

    let sort_dir = match params.sort_order.as_deref() {
        Some("desc") => "DESC",
        _ => "ASC",
    };

    // Column names cannot be parameterized; validate and interpolate directly.
    qb.push(format!(" ORDER BY {sort_col} {sort_dir}"));

    qb.push(" LIMIT ");
    qb.push_bind(fetch_limit);

    let mut users: Vec<User> = qb
        .build_query_as::<User>()
        .fetch_all(pool)
        .await?;

    // Determine next cursor
    let next_cursor = if users.len() as i64 > limit {
        users.truncate(limit as usize);
        users.last().map(|u| u.id)
    } else {
        None
    };

    Ok(SearchResult { users, next_cursor })
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let pool = sqlx::PgPool::connect(
        &std::env::var("DATABASE_URL").unwrap_or_else(|_| {
            "postgres://localhost:5432/testdb".to_string()
        }),
    )
    .await?;

    let result = search_users(
        &pool,
        &UserSearch {
            name: Some("alice".to_string()),
            sort_by: Some("created_at".to_string()),
            sort_order: Some("desc".to_string()),
            limit: Some(10),
            ..Default::default()
        },
    )
    .await?;

    for user in &result.users {
        println!("{}: {} <{}>", user.id, user.name, user.email);
    }

    if let Some(cursor) = result.next_cursor {
        println!("next page: cursor={cursor}");
    } else {
        println!("no more results");
    }

    Ok(())
}
```
</details>

### Exercise 3: Migration and Seed Pipeline

Build a standalone binary that: (1) runs all pending migrations, (2) checks if seed data exists, (3) inserts seed data inside a transaction if the database is empty, (4) prints a summary of all tables and row counts. This simulates a production database initialization pipeline.

**Constraints:**
- Use `sqlx::migrate!()` for embedded migrations
- Seed data must be idempotent (safe to run multiple times)
- Use a transaction for all seed inserts
- Query `information_schema.tables` to list all user tables
- Print elapsed time for migrations and seeding separately

<details>
<summary>Solution</summary>

```rust
use sqlx::{PgPool, Row};
use std::time::Instant;

static MIGRATOR: sqlx::migrate::Migrator = sqlx::migrate!("./migrations");

async fn run_migrations(pool: &PgPool) -> Result<(), sqlx::Error> {
    let start = Instant::now();
    MIGRATOR.run(pool).await?;
    println!("migrations completed in {:?}", start.elapsed());
    Ok(())
}

async fn seed_if_empty(pool: &PgPool) -> Result<(), sqlx::Error> {
    let start = Instant::now();

    // Check if seed data already exists
    let count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM users")
        .fetch_one(pool)
        .await
        .unwrap_or(0);

    if count > 0 {
        println!("seed skipped: {} users already exist", count);
        return Ok(());
    }

    let mut tx = pool.begin().await?;

    // Seed users
    let users = vec![
        ("admin@example.com", "Admin User", "$argon2_hash_placeholder"),
        ("alice@example.com", "Alice Smith", "$argon2_hash_placeholder"),
        ("bob@example.com", "Bob Jones", "$argon2_hash_placeholder"),
    ];

    for (email, name, hash) in &users {
        sqlx::query(
            "INSERT INTO users (email, name, password_hash)
             VALUES ($1, $2, $3)
             ON CONFLICT (email) DO NOTHING",
        )
        .bind(email)
        .bind(name)
        .bind(hash)
        .execute(&mut *tx)
        .await?;
    }

    // Seed orders for alice
    let alice_id: i64 = sqlx::query_scalar(
        "SELECT id FROM users WHERE email = 'alice@example.com'",
    )
    .fetch_one(&mut *tx)
    .await?;

    sqlx::query(
        "INSERT INTO orders (customer_email, status, total_cents)
         VALUES ('alice@example.com', 'completed', 4500)",
    )
    .execute(&mut *tx)
    .await?;

    tx.commit().await?;

    println!("seed completed in {:?}: {} users inserted", start.elapsed(), users.len());
    Ok(())
}

async fn print_table_summary(pool: &PgPool) -> Result<(), sqlx::Error> {
    println!("\n--- Table Summary ---");

    let tables: Vec<String> = sqlx::query_scalar(
        "SELECT table_name::text FROM information_schema.tables
         WHERE table_schema = 'public'
           AND table_type = 'BASE TABLE'
         ORDER BY table_name",
    )
    .fetch_all(pool)
    .await?;

    for table in &tables {
        // Using format! for table name is safe here because we got
        // the name from information_schema, not user input.
        let count_query = format!("SELECT COUNT(*) FROM \"{table}\"");
        let count: i64 = sqlx::query_scalar(&count_query)
            .fetch_one(pool)
            .await
            .unwrap_or(-1);
        println!("  {table:<30} {count:>8} rows");
    }

    Ok(())
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let database_url = std::env::var("DATABASE_URL")
        .unwrap_or_else(|_| "postgres://localhost:5432/testdb".to_string());

    let pool = sqlx::PgPool::connect(&database_url).await?;

    run_migrations(&pool).await?;
    seed_if_empty(&pool).await?;
    print_table_summary(&pool).await?;

    pool.close().await;
    println!("\ndone.");

    Ok(())
}
```
</details>

### Exercise 4: Streaming Large Result Sets

Build a function that processes millions of rows without loading them all into memory. Use sqlx's streaming API (`fetch`) to process rows one at a time, compute running statistics (count, sum, min, max, average), and write aggregated results to a summary table.

**Constraints:**
- Use `sqlx::query().fetch()` which returns a `Stream`
- Process at least 100,000 conceptual rows (use generate_series in Postgres)
- Track memory usage: the peak RSS should not grow proportionally to row count
- Write the summary inside a transaction
- Compare streaming vs `fetch_all` in terms of memory and timing

<details>
<summary>Solution</summary>

```rust
use futures::StreamExt;
use sqlx::PgPool;
use std::time::Instant;

#[derive(Debug, Default)]
struct Stats {
    count: i64,
    sum: i64,
    min: i64,
    max: i64,
}

impl Stats {
    fn update(&mut self, value: i64) {
        self.count += 1;
        self.sum += value;
        if self.count == 1 {
            self.min = value;
            self.max = value;
        } else {
            self.min = self.min.min(value);
            self.max = self.max.max(value);
        }
    }

    fn average(&self) -> f64 {
        if self.count == 0 {
            0.0
        } else {
            self.sum as f64 / self.count as f64
        }
    }
}

async fn streaming_aggregate(pool: &PgPool) -> Result<Stats, sqlx::Error> {
    let start = Instant::now();
    let mut stats = Stats::default();

    // generate_series produces rows without a real table
    let mut stream = sqlx::query_scalar::<_, i64>(
        "SELECT val FROM generate_series(1, 1000000) AS val",
    )
    .fetch(pool);

    while let Some(row) = stream.next().await {
        let value = row?;
        stats.update(value);
    }

    println!(
        "streaming: {} rows in {:?}",
        stats.count,
        start.elapsed()
    );
    Ok(stats)
}

async fn fetch_all_aggregate(pool: &PgPool) -> Result<Stats, sqlx::Error> {
    let start = Instant::now();
    let mut stats = Stats::default();

    let rows: Vec<i64> = sqlx::query_scalar(
        "SELECT val FROM generate_series(1, 1000000) AS val",
    )
    .fetch_all(pool)
    .await?;

    for value in rows {
        stats.update(value);
    }

    println!(
        "fetch_all: {} rows in {:?}",
        stats.count,
        start.elapsed()
    );
    Ok(stats)
}

async fn save_summary(pool: &PgPool, stats: &Stats) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;

    sqlx::query(
        "CREATE TABLE IF NOT EXISTS summaries (
            id SERIAL PRIMARY KEY,
            row_count BIGINT NOT NULL,
            total_sum BIGINT NOT NULL,
            min_val BIGINT NOT NULL,
            max_val BIGINT NOT NULL,
            avg_val DOUBLE PRECISION NOT NULL,
            computed_at TIMESTAMP NOT NULL DEFAULT NOW()
        )",
    )
    .execute(&mut *tx)
    .await?;

    sqlx::query(
        "INSERT INTO summaries (row_count, total_sum, min_val, max_val, avg_val)
         VALUES ($1, $2, $3, $4, $5)",
    )
    .bind(stats.count)
    .bind(stats.sum)
    .bind(stats.min)
    .bind(stats.max)
    .bind(stats.average())
    .execute(&mut *tx)
    .await?;

    tx.commit().await?;
    Ok(())
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let pool = sqlx::PgPool::connect(
        &std::env::var("DATABASE_URL")
            .unwrap_or_else(|_| "postgres://localhost:5432/testdb".to_string()),
    )
    .await?;

    println!("--- Streaming approach (constant memory) ---");
    let stats_stream = streaming_aggregate(&pool).await?;
    println!(
        "  count={}, sum={}, min={}, max={}, avg={:.2}",
        stats_stream.count,
        stats_stream.sum,
        stats_stream.min,
        stats_stream.max,
        stats_stream.average()
    );

    println!("\n--- fetch_all approach (loads all rows into memory) ---");
    let stats_all = fetch_all_aggregate(&pool).await?;
    println!(
        "  count={}, sum={}, min={}, max={}, avg={:.2}",
        stats_all.count,
        stats_all.sum,
        stats_all.min,
        stats_all.max,
        stats_all.average()
    );

    save_summary(&pool, &stats_stream).await?;
    println!("\nsummary saved to database");

    Ok(())
}
```

**Key insight**: `fetch()` returns a `Stream` that pulls rows one at a time from the database cursor. Memory usage is O(1) regardless of result set size. `fetch_all()` collects every row into a `Vec`, so memory usage is O(n). For 1 million rows of i64 values, `fetch_all` allocates ~8MB just for the values. For complex rows with strings, memory usage can be orders of magnitude higher.
</details>

## Common Mistakes

1. **Forgetting `DATABASE_URL` for compile-time checks.** `query!` and `query_as!` need a live database at compile time. Without `DATABASE_URL` set (or `.sqlx/` prepared queries), compilation fails with a cryptic error about connecting to the database.

2. **Holding a transaction open too long.** A transaction holds a database connection for its entire duration. If you do HTTP calls or sleep inside a transaction, you hold a pool connection hostage. Keep transactions short: gather data, begin, write, commit.

3. **Using OFFSET for pagination.** `OFFSET 10000` forces the database to scan and discard 10,000 rows before returning results. Cursor-based pagination (`WHERE id > $last_id ORDER BY id LIMIT 20`) is O(1) for any page.

4. **Not handling pool exhaustion.** If all pool connections are in use and `acquire_timeout` expires, sqlx returns `PoolTimedOut`. Your API should return 503 Service Unavailable, not 500 Internal Server Error.

5. **String interpolation in SQL.** Never use `format!("SELECT * FROM users WHERE name = '{name}'")`. Always use parameterized queries (`$1`, `$2`) via `push_bind()` or `query!` macros. String interpolation is SQL injection.

6. **Running migrations from multiple instances.** If two application instances start simultaneously and both run migrations, they may conflict. Use `sqlx migrate run` in a dedicated init container or CI step, not at application startup in multi-instance deployments.

## Verification

```bash
# Install sqlx-cli
cargo install sqlx-cli --no-default-features --features postgres

# Start a local Postgres (Docker)
docker run -d --name pg-test -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16

# Set DATABASE_URL
export DATABASE_URL="postgres://postgres:test@localhost:5432/postgres"

# Create the database and run migrations
sqlx database create
sqlx migrate run

# Build with compile-time checked queries
cargo build

# Run tests
cargo test

# Check for lint issues
cargo clippy -- -W clippy::pedantic

# Prepare offline query data for CI
cargo sqlx prepare
```

## Trade-Off Analysis

### sqlx vs Diesel vs SeaORM

| Dimension | sqlx | Diesel | SeaORM |
|---|---|---|---|
| **Philosophy** | SQL-first, thin wrapper | Type-safe DSL, schema-driven | ActiveRecord/ORM style |
| **Query style** | Raw SQL with macros | Rust DSL generates SQL | Rust DSL + raw SQL escape hatch |
| **Compile-time checks** | Against live DB or cached | Against local schema.rs | Against entity models |
| **Async support** | Native (tokio, async-std) | Sync only (blocking) | Native (built on sqlx) |
| **Migration** | SQL files via sqlx-cli | SQL or Rust-based via diesel-cli | SQL files via sea-orm-cli |
| **Learning curve** | Know SQL, learn macros | Learn Diesel DSL | Learn SeaORM conventions |
| **Complex queries** | Natural (write any SQL) | DSL can get awkward | Mix DSL + raw SQL |
| **Schema drift** | Compile error (query_as!) | Compile error (schema.rs) | Runtime error possible |
| **Ecosystem maturity** | ~4 years, very active | ~8 years, stable | ~3 years, growing |

Choose sqlx when you want full SQL control with compile-time safety. Choose Diesel when you want a sync-first, type-safe query DSL. Choose SeaORM when you want an ORM with async support and are comfortable with ActiveRecord patterns.

## What You Learned

- sqlx provides compile-time query verification against a real database schema, catching SQL errors before runtime
- Connection pools must be sized carefully: too few starve requests, too many exhaust database resources
- Transactions guarantee atomicity but hold connections; keep them short
- The repository pattern decouples business logic from persistence, enabling in-memory test implementations
- QueryBuilder enables dynamic query construction with parameterized values, avoiding SQL injection
- Cursor-based pagination outperforms OFFSET pagination at scale
- Streaming large result sets with `fetch()` keeps memory usage constant regardless of result size
- Database error codes can be mapped to typed domain errors for clean API responses

## What's Next

Exercise 24 explores advanced macro patterns for generating repository boilerplate, reducing the repetition seen in this exercise's trait implementations.

## Resources

- [sqlx documentation](https://docs.rs/sqlx/latest/sqlx/)
- [sqlx compile-time checking](https://docs.rs/sqlx/latest/sqlx/macro.query.html)
- [sqlx QueryBuilder](https://docs.rs/sqlx/latest/sqlx/struct.QueryBuilder.html)
- [PostgreSQL error codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
- [Connection pool sizing (HikariCP wiki, concepts apply)](https://github.com/brettwooldridge/HikariCP/wiki/About-Pool-Sizing)
- [Cursor-based pagination explained](https://use-the-index-luke.com/no-offset)
- [Diesel vs sqlx comparison](https://www.shuttle.rs/blog/2023/10/04/sql-in-rust)
