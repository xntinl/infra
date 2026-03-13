# 23. Database Patterns with sqlx, Diesel, and SeaORM

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Exercise 22 (axum handlers, extractors, State)
- Understanding of SQL (SELECT, INSERT, JOIN, transactions)
- A running PostgreSQL instance (Docker recommended)

## Learning Objectives

- Use sqlx compile-time checked queries with `query!` and `query_as!` macros
- Model data with Diesel's schema DSL and type-safe query builder
- Build async queries with SeaORM's entity-based approach
- Design a repository pattern that abstracts the database layer behind traits
- Test database code with transaction rollback strategies
- Execute bulk operations efficiently across all three libraries
- Select the right library for a given project by analyzing trade-offs across 10 dimensions

## Setup: PostgreSQL Schema

All exercises use this shared schema. Run it against your local PostgreSQL:

```sql
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE posts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_posts_user_id ON posts(user_id);
CREATE INDEX idx_posts_published ON posts(published) WHERE published = true;

CREATE TABLE tags (
    id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE post_tags (
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag_id  UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
```

## Concepts

### Approach 1: sqlx (Compile-Time Checked Queries)

sqlx verifies your SQL at compile time by connecting to the database. No DSL, no ORM -- just SQL strings that the compiler validates against your actual schema.

```rust
use sqlx::postgres::PgPoolOptions;
use sqlx::{FromRow, PgPool};
use uuid::Uuid;
use chrono::{DateTime, Utc};

#[derive(Debug, FromRow)]
struct User {
    id: Uuid,
    email: String,
    name: String,
    created_at: DateTime<Utc>,
}

#[derive(Debug, FromRow)]
struct Post {
    id: Uuid,
    user_id: Uuid,
    title: String,
    body: String,
    published: bool,
    created_at: DateTime<Utc>,
    updated_at: DateTime<Utc>,
}

// Connection pool setup
async fn create_pool(database_url: &str) -> PgPool {
    PgPoolOptions::new()
        .max_connections(10)
        .min_connections(2)
        .acquire_timeout(std::time::Duration::from_secs(5))
        .idle_timeout(std::time::Duration::from_secs(300))
        .connect(database_url)
        .await
        .expect("failed to connect to database")
}

// Compile-time checked queries: if the SQL is wrong, it fails at compile time.
// Requires DATABASE_URL env var at compile time.
async fn get_user_by_email(pool: &PgPool, email: &str) -> Result<Option<User>, sqlx::Error> {
    sqlx::query_as!(
        User,
        r#"SELECT id, email, name, created_at FROM users WHERE email = $1"#,
        email
    )
    .fetch_optional(pool)
    .await
}

async fn create_user(pool: &PgPool, email: &str, name: &str) -> Result<User, sqlx::Error> {
    sqlx::query_as!(
        User,
        r#"
        INSERT INTO users (email, name)
        VALUES ($1, $2)
        RETURNING id, email, name, created_at
        "#,
        email,
        name
    )
    .fetch_one(pool)
    .await
}

// Transactions
async fn create_post_with_tags(
    pool: &PgPool,
    user_id: Uuid,
    title: &str,
    body: &str,
    tag_names: &[String],
) -> Result<Post, sqlx::Error> {
    let mut tx = pool.begin().await?;

    let post = sqlx::query_as!(
        Post,
        r#"
        INSERT INTO posts (user_id, title, body)
        VALUES ($1, $2, $3)
        RETURNING id, user_id, title, body, published, created_at, updated_at
        "#,
        user_id,
        title,
        body
    )
    .fetch_one(&mut *tx)
    .await?;

    for tag_name in tag_names {
        // Upsert tag
        let tag_id: Uuid = sqlx::query_scalar!(
            r#"
            INSERT INTO tags (name) VALUES ($1)
            ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
            RETURNING id
            "#,
            tag_name
        )
        .fetch_one(&mut *tx)
        .await?;

        // Link post to tag
        sqlx::query!(
            "INSERT INTO post_tags (post_id, tag_id) VALUES ($1, $2)",
            post.id,
            tag_id
        )
        .execute(&mut *tx)
        .await?;
    }

    tx.commit().await?;
    Ok(post)
}

// Offline mode: `cargo sqlx prepare` generates a .sqlx/ directory with query metadata.
// CI builds use this directory instead of a live database.
// Run: cargo sqlx prepare -- --lib
```

**Trade-off**: sqlx gives you raw SQL power with compile-time safety, but every query is a hand-written string. Refactoring a column name requires updating every query that references it. There is no schema migration tool built in (use `sqlx migrate` or a standalone tool like `refinery`).

### Approach 2: Diesel (Type-Safe Query Builder)

Diesel generates Rust types from your schema. Queries are built with a DSL that maps 1:1 to SQL, but column names and types are checked at compile time without connecting to a database.

```rust
// diesel.toml points to src/schema.rs
// Run: diesel setup && diesel migration run

// schema.rs (generated by diesel CLI)
diesel::table! {
    users (id) {
        id -> Uuid,
        email -> Text,
        name -> Text,
        created_at -> Timestamptz,
    }
}

diesel::table! {
    posts (id) {
        id -> Uuid,
        user_id -> Uuid,
        title -> Text,
        body -> Text,
        published -> Bool,
        created_at -> Timestamptz,
        updated_at -> Timestamptz,
    }
}

diesel::joinable!(posts -> users (user_id));
diesel::allow_tables_to_appear_in_same_query!(users, posts);

// models.rs
use diesel::prelude::*;
use uuid::Uuid;
use chrono::{DateTime, Utc};

#[derive(Queryable, Selectable, Debug)]
#[diesel(table_name = crate::schema::users)]
pub struct User {
    pub id: Uuid,
    pub email: String,
    pub name: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Insertable)]
#[diesel(table_name = crate::schema::users)]
pub struct NewUser<'a> {
    pub email: &'a str,
    pub name: &'a str,
}

// queries.rs
use diesel::prelude::*;

fn find_user_by_email(conn: &mut PgConnection, user_email: &str) -> QueryResult<Option<User>> {
    use crate::schema::users::dsl::*;

    users
        .filter(email.eq(user_email))
        .select(User::as_select())
        .first(conn)
        .optional()
}

fn create_user(conn: &mut PgConnection, new_email: &str, new_name: &str) -> QueryResult<User> {
    use crate::schema::users::dsl::*;

    diesel::insert_into(users)
        .values(NewUser { email: new_email, name: new_name })
        .returning(User::as_returning())
        .get_result(conn)
}

fn published_posts_by_user(conn: &mut PgConnection, uid: Uuid) -> QueryResult<Vec<Post>> {
    use crate::schema::posts::dsl::*;

    posts
        .filter(user_id.eq(uid).and(published.eq(true)))
        .order(created_at.desc())
        .select(Post::as_select())
        .load(conn)
}
```

**Trade-off**: Diesel's DSL is powerful and fully type-checked without a database connection, but it is synchronous by default. `diesel-async` exists but is a separate crate with its own connection types. Complex queries (subqueries, CTEs, window functions) sometimes require escaping to raw SQL via `sql_query()`.

### Approach 3: SeaORM (Async Entity-Based)

SeaORM is async-first and generates entity structs from migrations. It sits on top of sqlx but adds an Active Record-style API:

```rust
// Entity generated by `sea-orm-cli generate entity`
// entity/user.rs
use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "users")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub id: Uuid,
    #[sea_orm(unique)]
    pub email: String,
    pub name: String,
    pub created_at: DateTimeWithTimeZone,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_many = "super::post::Entity")]
    Posts,
}

impl Related<super::post::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Posts.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}

// Usage
use sea_orm::{DatabaseConnection, EntityTrait, QueryFilter, ColumnTrait, Set, ActiveModelTrait};

async fn find_user(db: &DatabaseConnection, email: &str) -> Result<Option<user::Model>, DbErr> {
    user::Entity::find()
        .filter(user::Column::Email.eq(email))
        .one(db)
        .await
}

async fn create_user(db: &DatabaseConnection, email: &str, name: &str) -> Result<user::Model, DbErr> {
    let new_user = user::ActiveModel {
        email: Set(email.to_owned()),
        name: Set(name.to_owned()),
        ..Default::default()
    };
    new_user.insert(db).await
}

async fn user_with_posts(
    db: &DatabaseConnection,
    user_id: Uuid,
) -> Result<Option<(user::Model, Vec<post::Model>)>, DbErr> {
    user::Entity::find_by_id(user_id)
        .find_with_related(post::Entity)
        .all(db)
        .await
        .map(|mut results| results.pop())
}
```

**Trade-off**: SeaORM provides the most "traditional ORM" experience with relations, eager loading, and Active Record patterns. But the generated entity code is verbose, and you lose direct control over the SQL. Performance-critical queries may need to fall back to `sea_orm::Statement::from_sql_and_values()`.

### Comparison Table

| Dimension | sqlx | Diesel | SeaORM |
|---|---|---|---|
| **Query style** | Raw SQL strings | Type-safe DSL | Active Record + query builder |
| **Compile-time checks** | Yes (requires DB connection) | Yes (no DB connection needed) | Partial (entity derives checked) |
| **Async support** | Native | Via diesel-async crate | Native |
| **Migration tool** | `sqlx migrate` | `diesel migration` | `sea-orm-cli migrate` |
| **Schema generation** | Manual structs + `FromRow` | Auto from `diesel print-schema` | Auto from `sea-orm-cli generate` |
| **Complex queries** | Full SQL flexibility | DSL covers most; raw SQL escape hatch | Query builder + raw SQL escape |
| **Relation support** | Manual JOINs | `joinable!` macro + `.inner_join()` | `Related` trait + eager loading |
| **Connection pooling** | Built-in (`PgPoolOptions`) | BYO (r2d2 sync, bb8/deadpool async) | Built-in (wraps sqlx) |
| **Offline CI** | `.sqlx/` prepared queries | Schema DSL is self-contained | Entity code is self-contained |
| **Learning curve** | Low (just SQL) | Medium (DSL + schema macros) | Medium (entity model + ActiveModel) |
| **Maturity** | High (0.8.x) | Very high (2.x, since 2015) | High (1.x / 2.0) |
| **Best for** | Teams that prefer SQL, microservices | Type-safety purists, sync codebases | Rapid CRUD development, Active Record fans |

### Repository Pattern

Abstract the database behind a trait so handlers never depend on a specific library:

```rust
use async_trait::async_trait;
use uuid::Uuid;

// Domain types (no database annotations)
pub struct User {
    pub id: Uuid,
    pub email: String,
    pub name: String,
}

pub struct CreateUserInput {
    pub email: String,
    pub name: String,
}

#[async_trait]
pub trait UserRepository: Send + Sync {
    async fn find_by_id(&self, id: Uuid) -> Result<Option<User>, anyhow::Error>;
    async fn find_by_email(&self, email: &str) -> Result<Option<User>, anyhow::Error>;
    async fn create(&self, input: CreateUserInput) -> Result<User, anyhow::Error>;
    async fn delete(&self, id: Uuid) -> Result<bool, anyhow::Error>;
}

// sqlx implementation
pub struct SqlxUserRepository {
    pool: sqlx::PgPool,
}

#[async_trait]
impl UserRepository for SqlxUserRepository {
    async fn find_by_id(&self, id: Uuid) -> Result<Option<User>, anyhow::Error> {
        let row = sqlx::query_as!(
            User,
            "SELECT id, email, name FROM users WHERE id = $1",
            id
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row)
    }

    async fn find_by_email(&self, email: &str) -> Result<Option<User>, anyhow::Error> {
        let row = sqlx::query_as!(
            User,
            "SELECT id, email, name FROM users WHERE email = $1",
            email
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row)
    }

    async fn create(&self, input: CreateUserInput) -> Result<User, anyhow::Error> {
        let user = sqlx::query_as!(
            User,
            "INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name",
            input.email,
            input.name
        )
        .fetch_one(&self.pool)
        .await?;
        Ok(user)
    }

    async fn delete(&self, id: Uuid) -> Result<bool, anyhow::Error> {
        let result = sqlx::query!("DELETE FROM users WHERE id = $1", id)
            .execute(&self.pool)
            .await?;
        Ok(result.rows_affected() > 0)
    }
}

// In axum, inject as trait object:
// .with_state(Arc::new(SqlxUserRepository { pool }) as Arc<dyn UserRepository>)
```

### Testing with Transaction Rollback

The cleanest way to test database code is to wrap each test in a transaction that never commits:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use sqlx::PgPool;

    // sqlx provides #[sqlx::test] which creates a test database and runs migrations
    #[sqlx::test(migrations = "./migrations")]
    async fn test_create_user(pool: PgPool) {
        let repo = SqlxUserRepository { pool };

        let user = repo.create(CreateUserInput {
            email: "test@example.com".into(),
            name: "Test User".into(),
        }).await.unwrap();

        assert_eq!(user.email, "test@example.com");

        let found = repo.find_by_email("test@example.com").await.unwrap();
        assert!(found.is_some());
    }

    // Manual transaction rollback approach (works with any pool)
    #[tokio::test]
    async fn test_with_rollback() {
        let pool = PgPool::connect(&std::env::var("DATABASE_URL").unwrap())
            .await
            .unwrap();

        let mut tx = pool.begin().await.unwrap();

        // All operations go through &mut tx instead of &pool
        sqlx::query!("INSERT INTO users (email, name) VALUES ($1, $2)", "rollback@test.com", "Rollback")
            .execute(&mut *tx)
            .await
            .unwrap();

        let count: i64 = sqlx::query_scalar!("SELECT count(*) FROM users WHERE email = $1", "rollback@test.com")
            .fetch_one(&mut *tx)
            .await
            .unwrap()
            .unwrap_or(0);

        assert_eq!(count, 1);

        // tx is dropped without commit -> automatic rollback
        // The inserted row never persists
    }
}
```

### Bulk Operations

For large data loads, batch inserts dramatically outperform individual inserts:

```rust
// sqlx: UNNEST-based bulk insert (PostgreSQL)
async fn bulk_create_users(pool: &PgPool, users: &[(String, String)]) -> Result<Vec<User>, sqlx::Error> {
    let emails: Vec<&str> = users.iter().map(|(e, _)| e.as_str()).collect();
    let names: Vec<&str> = users.iter().map(|(_, n)| n.as_str()).collect();

    sqlx::query_as!(
        User,
        r#"
        INSERT INTO users (email, name)
        SELECT * FROM UNNEST($1::text[], $2::text[])
        RETURNING id, email, name
        "#,
        &emails as &[&str],
        &names as &[&str]
    )
    .fetch_all(pool)
    .await
}

// sqlx: COPY IN for maximum throughput (raw binary protocol)
// For 100k+ rows, COPY is 10-50x faster than INSERT
async fn bulk_copy_users(pool: &PgPool, users: Vec<(String, String)>) -> Result<u64, sqlx::Error> {
    let mut tx = pool.begin().await?;

    // Build a temporary table, COPY into it, then INSERT SELECT
    sqlx::query("CREATE TEMP TABLE tmp_users (email TEXT, name TEXT) ON COMMIT DROP")
        .execute(&mut *tx)
        .await?;

    // Use a multi-row VALUES for moderate sizes
    let mut query = String::from("INSERT INTO tmp_users (email, name) VALUES ");
    let mut params_idx = 1;
    for (i, _) in users.iter().enumerate() {
        if i > 0 { query.push(','); }
        query.push_str(&format!("(${}, ${})", params_idx, params_idx + 1));
        params_idx += 2;
    }

    let mut q = sqlx::query(&query);
    for (email, name) in &users {
        q = q.bind(email).bind(name);
    }
    q.execute(&mut *tx).await?;

    let result = sqlx::query("INSERT INTO users (email, name) SELECT email, name FROM tmp_users")
        .execute(&mut *tx)
        .await?;

    tx.commit().await?;
    Ok(result.rows_affected())
}
```

## Exercises

### Exercise 1: sqlx Blog API

Build a complete blog API using sqlx with the schema above. Implement these operations:
- Create user, list users, get user by ID
- Create post (with tags), list posts (with filtering by published/tag), get post with author and tags
- Publish/unpublish a post
- All mutating operations in transactions

**Cargo.toml:**
```toml
[package]
name = "database-patterns"
edition = "2024"

[dependencies]
tokio = { version = "1", features = ["full"] }
sqlx = { version = "0.8", features = ["runtime-tokio", "tls-rustls", "postgres", "uuid", "chrono", "macros"] }
uuid = { version = "1", features = ["v4", "serde"] }
chrono = { version = "0.4", features = ["serde"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
axum = "0.8"
anyhow = "1"
async-trait = "0.1"
```

**Constraints:**
- Use `query_as!` for all queries (compile-time checked)
- Proper error handling (no `unwrap` in library code)
- Paginated list endpoints (limit + offset)

<details>
<summary>Solution</summary>

```rust
use axum::{
    Router, Json,
    routing::{get, post, patch},
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use sqlx::PgPool;
use uuid::Uuid;
use chrono::{DateTime, Utc};

#[derive(Debug, Serialize, sqlx::FromRow)]
struct User {
    id: Uuid,
    email: String,
    name: String,
    created_at: DateTime<Utc>,
}

#[derive(Debug, Serialize, sqlx::FromRow)]
struct Post {
    id: Uuid,
    user_id: Uuid,
    title: String,
    body: String,
    published: bool,
    created_at: DateTime<Utc>,
    updated_at: DateTime<Utc>,
}

#[derive(Debug, Serialize)]
struct PostDetail {
    #[serde(flatten)]
    post: Post,
    author_name: String,
    tags: Vec<String>,
}

#[derive(Deserialize)]
struct CreateUserReq { email: String, name: String }

#[derive(Deserialize)]
struct CreatePostReq { title: String, body: String, tags: Vec<String> }

#[derive(Deserialize)]
struct ListParams { limit: Option<i64>, offset: Option<i64>, published: Option<bool>, tag: Option<String> }

type AppError = (StatusCode, String);

fn internal_error(e: impl std::fmt::Display) -> AppError {
    (StatusCode::INTERNAL_SERVER_ERROR, e.to_string())
}

async fn create_user(
    State(pool): State<PgPool>,
    Json(req): Json<CreateUserReq>,
) -> Result<(StatusCode, Json<User>), AppError> {
    let user = sqlx::query_as!(
        User,
        "INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, created_at",
        req.email, req.name
    )
    .fetch_one(&pool)
    .await
    .map_err(internal_error)?;

    Ok((StatusCode::CREATED, Json(user)))
}

async fn list_users(
    State(pool): State<PgPool>,
    Query(params): Query<ListParams>,
) -> Result<Json<Vec<User>>, AppError> {
    let limit = params.limit.unwrap_or(50);
    let offset = params.offset.unwrap_or(0);

    let users = sqlx::query_as!(
        User,
        "SELECT id, email, name, created_at FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2",
        limit, offset
    )
    .fetch_all(&pool)
    .await
    .map_err(internal_error)?;

    Ok(Json(users))
}

async fn create_post(
    State(pool): State<PgPool>,
    Path(user_id): Path<Uuid>,
    Json(req): Json<CreatePostReq>,
) -> Result<(StatusCode, Json<Post>), AppError> {
    let mut tx = pool.begin().await.map_err(internal_error)?;

    let post = sqlx::query_as!(
        Post,
        r#"INSERT INTO posts (user_id, title, body)
           VALUES ($1, $2, $3)
           RETURNING id, user_id, title, body, published, created_at, updated_at"#,
        user_id, req.title, req.body
    )
    .fetch_one(&mut *tx)
    .await
    .map_err(internal_error)?;

    for tag_name in &req.tags {
        let tag_id: Uuid = sqlx::query_scalar!(
            "INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id",
            tag_name
        )
        .fetch_one(&mut *tx)
        .await
        .map_err(internal_error)?;

        sqlx::query!("INSERT INTO post_tags (post_id, tag_id) VALUES ($1, $2)", post.id, tag_id)
            .execute(&mut *tx)
            .await
            .map_err(internal_error)?;
    }

    tx.commit().await.map_err(internal_error)?;
    Ok((StatusCode::CREATED, Json(post)))
}

async fn get_post(
    State(pool): State<PgPool>,
    Path(post_id): Path<Uuid>,
) -> Result<Json<PostDetail>, AppError> {
    let post = sqlx::query_as!(
        Post,
        "SELECT id, user_id, title, body, published, created_at, updated_at FROM posts WHERE id = $1",
        post_id
    )
    .fetch_optional(&pool)
    .await
    .map_err(internal_error)?
    .ok_or((StatusCode::NOT_FOUND, "post not found".into()))?;

    let author_name: String = sqlx::query_scalar!("SELECT name FROM users WHERE id = $1", post.user_id)
        .fetch_one(&pool)
        .await
        .map_err(internal_error)?;

    let tags: Vec<String> = sqlx::query_scalar!(
        "SELECT t.name FROM tags t JOIN post_tags pt ON t.id = pt.tag_id WHERE pt.post_id = $1 ORDER BY t.name",
        post_id
    )
    .fetch_all(&pool)
    .await
    .map_err(internal_error)?;

    Ok(Json(PostDetail { post, author_name, tags }))
}

async fn list_posts(
    State(pool): State<PgPool>,
    Query(params): Query<ListParams>,
) -> Result<Json<Vec<Post>>, AppError> {
    let limit = params.limit.unwrap_or(50);
    let offset = params.offset.unwrap_or(0);

    let posts = match (&params.published, &params.tag) {
        (Some(pub_filter), None) => {
            sqlx::query_as!(
                Post,
                r#"SELECT id, user_id, title, body, published, created_at, updated_at
                   FROM posts WHERE published = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3"#,
                pub_filter, limit, offset
            )
            .fetch_all(&pool).await
        }
        (None, Some(tag_name)) => {
            sqlx::query_as!(
                Post,
                r#"SELECT p.id, p.user_id, p.title, p.body, p.published, p.created_at, p.updated_at
                   FROM posts p
                   JOIN post_tags pt ON p.id = pt.post_id
                   JOIN tags t ON pt.tag_id = t.id
                   WHERE t.name = $1
                   ORDER BY p.created_at DESC LIMIT $2 OFFSET $3"#,
                tag_name, limit, offset
            )
            .fetch_all(&pool).await
        }
        _ => {
            sqlx::query_as!(
                Post,
                "SELECT id, user_id, title, body, published, created_at, updated_at FROM posts ORDER BY created_at DESC LIMIT $1 OFFSET $2",
                limit, offset
            )
            .fetch_all(&pool).await
        }
    }.map_err(internal_error)?;

    Ok(Json(posts))
}

async fn toggle_publish(
    State(pool): State<PgPool>,
    Path(post_id): Path<Uuid>,
) -> Result<Json<Post>, AppError> {
    let post = sqlx::query_as!(
        Post,
        r#"UPDATE posts SET published = NOT published, updated_at = now()
           WHERE id = $1
           RETURNING id, user_id, title, body, published, created_at, updated_at"#,
        post_id
    )
    .fetch_optional(&pool)
    .await
    .map_err(internal_error)?
    .ok_or((StatusCode::NOT_FOUND, "post not found".into()))?;

    Ok(Json(post))
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let pool = PgPool::connect(&std::env::var("DATABASE_URL")?).await?;
    sqlx::migrate!("./migrations").run(&pool).await?;

    let app = Router::new()
        .route("/users", get(list_users).post(create_user))
        .route("/users/{user_id}/posts", post(create_post))
        .route("/posts", get(list_posts))
        .route("/posts/{id}", get(get_post))
        .route("/posts/{id}/publish", patch(toggle_publish))
        .with_state(pool);

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await?;
    axum::serve(listener, app).await?;
    Ok(())
}
```
</details>

### Exercise 2: Repository Pattern with Swappable Backend

Define a `PostRepository` trait and implement it twice: once with sqlx and once with an in-memory `HashMap`. Use the trait in an axum handler. Write tests using the in-memory implementation (no database needed for tests).

**Constraints:**
- The trait must not leak sqlx or any other library types
- Domain types in a separate module with no database dependencies
- axum State holds `Arc<dyn PostRepository>`
- At least 5 test cases using the in-memory backend

<details>
<summary>Solution</summary>

```rust
// domain.rs
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct Post {
    pub id: Uuid,
    pub user_id: Uuid,
    pub title: String,
    pub body: String,
    pub published: bool,
}

pub struct CreatePost {
    pub user_id: Uuid,
    pub title: String,
    pub body: String,
}

#[derive(Debug, thiserror::Error)]
pub enum RepoError {
    #[error("not found")]
    NotFound,
    #[error("conflict: {0}")]
    Conflict(String),
    #[error("internal: {0}")]
    Internal(String),
}

// repository trait
#[async_trait::async_trait]
pub trait PostRepository: Send + Sync {
    async fn create(&self, input: CreatePost) -> Result<Post, RepoError>;
    async fn find_by_id(&self, id: Uuid) -> Result<Option<Post>, RepoError>;
    async fn list(&self, limit: usize, offset: usize) -> Result<Vec<Post>, RepoError>;
    async fn publish(&self, id: Uuid) -> Result<Post, RepoError>;
    async fn delete(&self, id: Uuid) -> Result<bool, RepoError>;
}

// in_memory.rs
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;

pub struct InMemoryPostRepo {
    store: Arc<RwLock<HashMap<Uuid, Post>>>,
}

impl InMemoryPostRepo {
    pub fn new() -> Self {
        Self { store: Arc::new(RwLock::new(HashMap::new())) }
    }
}

#[async_trait::async_trait]
impl PostRepository for InMemoryPostRepo {
    async fn create(&self, input: CreatePost) -> Result<Post, RepoError> {
        let post = Post {
            id: Uuid::new_v4(),
            user_id: input.user_id,
            title: input.title,
            body: input.body,
            published: false,
        };
        self.store.write().await.insert(post.id, post.clone());
        Ok(post)
    }

    async fn find_by_id(&self, id: Uuid) -> Result<Option<Post>, RepoError> {
        Ok(self.store.read().await.get(&id).cloned())
    }

    async fn list(&self, limit: usize, offset: usize) -> Result<Vec<Post>, RepoError> {
        let store = self.store.read().await;
        Ok(store.values().skip(offset).take(limit).cloned().collect())
    }

    async fn publish(&self, id: Uuid) -> Result<Post, RepoError> {
        let mut store = self.store.write().await;
        let post = store.get_mut(&id).ok_or(RepoError::NotFound)?;
        post.published = true;
        Ok(post.clone())
    }

    async fn delete(&self, id: Uuid) -> Result<bool, RepoError> {
        Ok(self.store.write().await.remove(&id).is_some())
    }
}

// tests
#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn create_and_find() {
        let repo = InMemoryPostRepo::new();
        let post = repo.create(CreatePost {
            user_id: Uuid::new_v4(), title: "Test".into(), body: "Body".into(),
        }).await.unwrap();

        let found = repo.find_by_id(post.id).await.unwrap();
        assert!(found.is_some());
        assert_eq!(found.unwrap().title, "Test");
    }

    #[tokio::test]
    async fn find_nonexistent() {
        let repo = InMemoryPostRepo::new();
        assert!(repo.find_by_id(Uuid::new_v4()).await.unwrap().is_none());
    }

    #[tokio::test]
    async fn publish_post() {
        let repo = InMemoryPostRepo::new();
        let post = repo.create(CreatePost {
            user_id: Uuid::new_v4(), title: "Draft".into(), body: "...".into(),
        }).await.unwrap();
        assert!(!post.published);

        let published = repo.publish(post.id).await.unwrap();
        assert!(published.published);
    }

    #[tokio::test]
    async fn publish_nonexistent() {
        let repo = InMemoryPostRepo::new();
        assert!(matches!(repo.publish(Uuid::new_v4()).await, Err(RepoError::NotFound)));
    }

    #[tokio::test]
    async fn delete_post() {
        let repo = InMemoryPostRepo::new();
        let post = repo.create(CreatePost {
            user_id: Uuid::new_v4(), title: "Gone".into(), body: "...".into(),
        }).await.unwrap();

        assert!(repo.delete(post.id).await.unwrap());
        assert!(!repo.delete(post.id).await.unwrap());
        assert!(repo.find_by_id(post.id).await.unwrap().is_none());
    }
}
```
</details>

### Exercise 3: Bulk Import with Performance Comparison

Write a benchmark that inserts 10,000 users using three strategies: (a) individual INSERT statements, (b) batched INSERT with UNNEST, (c) COPY via temp table. Report throughput (rows/second) and total time for each.

**Constraints:**
- Each strategy runs in its own transaction (rolled back after timing)
- Generate unique emails using a counter
- Print a comparison table at the end

<details>
<summary>Solution</summary>

```rust
use sqlx::PgPool;
use std::time::Instant;

const COUNT: usize = 10_000;

async fn individual_inserts(pool: &PgPool) -> std::time::Duration {
    let mut tx = pool.begin().await.unwrap();
    let start = Instant::now();

    for i in 0..COUNT {
        sqlx::query("INSERT INTO users (email, name) VALUES ($1, $2)")
            .bind(format!("individual-{i}@test.com"))
            .bind(format!("User {i}"))
            .execute(&mut *tx)
            .await
            .unwrap();
    }

    let elapsed = start.elapsed();
    tx.rollback().await.unwrap();
    elapsed
}

async fn batch_unnest(pool: &PgPool) -> std::time::Duration {
    let mut tx = pool.begin().await.unwrap();
    let start = Instant::now();

    let emails: Vec<String> = (0..COUNT).map(|i| format!("batch-{i}@test.com")).collect();
    let names: Vec<String> = (0..COUNT).map(|i| format!("User {i}")).collect();

    sqlx::query(
        "INSERT INTO users (email, name) SELECT * FROM UNNEST($1::text[], $2::text[])"
    )
    .bind(&emails)
    .bind(&names)
    .execute(&mut *tx)
    .await
    .unwrap();

    let elapsed = start.elapsed();
    tx.rollback().await.unwrap();
    elapsed
}

async fn copy_temp_table(pool: &PgPool) -> std::time::Duration {
    let mut tx = pool.begin().await.unwrap();
    let start = Instant::now();

    sqlx::query("CREATE TEMP TABLE tmp_users (email TEXT, name TEXT) ON COMMIT DROP")
        .execute(&mut *tx).await.unwrap();

    // Build multi-row VALUES in chunks of 1000
    for chunk_start in (0..COUNT).step_by(1000) {
        let chunk_end = (chunk_start + 1000).min(COUNT);
        let chunk_size = chunk_end - chunk_start;

        let placeholders: Vec<String> = (0..chunk_size)
            .map(|i| format!("(${}, ${})", i * 2 + 1, i * 2 + 2))
            .collect();
        let sql = format!("INSERT INTO tmp_users (email, name) VALUES {}", placeholders.join(","));

        let mut q = sqlx::query(&sql);
        for i in chunk_start..chunk_end {
            q = q.bind(format!("copy-{i}@test.com")).bind(format!("User {i}"));
        }
        q.execute(&mut *tx).await.unwrap();
    }

    sqlx::query("INSERT INTO users (email, name) SELECT email, name FROM tmp_users")
        .execute(&mut *tx).await.unwrap();

    let elapsed = start.elapsed();
    tx.rollback().await.unwrap();
    elapsed
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let pool = PgPool::connect(&std::env::var("DATABASE_URL")?).await?;

    let t1 = individual_inserts(&pool).await;
    let t2 = batch_unnest(&pool).await;
    let t3 = copy_temp_table(&pool).await;

    println!("\n{:-<60}", "");
    println!("{:<25} {:>12} {:>12}", "Strategy", "Time", "Rows/sec");
    println!("{:-<60}", "");
    println!("{:<25} {:>12?} {:>12.0}", "Individual INSERT", t1, COUNT as f64 / t1.as_secs_f64());
    println!("{:<25} {:>12?} {:>12.0}", "Batch UNNEST", t2, COUNT as f64 / t2.as_secs_f64());
    println!("{:<25} {:>12?} {:>12.0}", "COPY temp table", t3, COUNT as f64 / t3.as_secs_f64());
    println!("{:-<60}", "");

    Ok(())
}
```

Typical results on a local PostgreSQL:
- Individual: ~3-5 seconds (2,000-3,000 rows/sec)
- UNNEST: ~100-200ms (50,000-100,000 rows/sec)
- COPY temp table: ~150-300ms (30,000-70,000 rows/sec)

The UNNEST approach is usually fastest for moderate sizes because it is a single round-trip. For very large datasets (100k+), actual COPY binary protocol is fastest, but requires lower-level access that sqlx does not expose directly.
</details>

## Common Mistakes

1. **Holding a pool connection across long-lived operations.** Each `query!` borrows from the pool briefly. But `pool.begin()` holds a connection for the entire transaction lifetime. Long transactions exhaust the pool.

2. **N+1 queries in loops.** Fetching posts then looping to fetch each post's author is N+1. Use JOINs or batch fetch (`WHERE id = ANY($1)`).

3. **Not using `fetch_optional` for lookups.** `fetch_one` panics (well, returns `RowNotFound` error) if no rows match. Use `fetch_optional` for lookups by ID or unique key.

4. **Ignoring connection pool settings.** Default pool sizes are often too large. Each PostgreSQL connection uses ~10MB of RAM. A pool of 100 connections on a server with 1GB RAM is a problem. Start with `max_connections = 10` and measure.

5. **Testing against a shared database.** Tests that mutate data in a shared database are flaky. Use `#[sqlx::test]` which creates an isolated test database, or wrap every test in a rolled-back transaction.

## Verification

```bash
# Set up PostgreSQL
docker run -d --name pg -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16
export DATABASE_URL="postgres://postgres:test@localhost/postgres"

# Run schema
psql $DATABASE_URL < schema.sql

# Compile (requires DB for sqlx macros)
cargo build

# Prepare for offline mode (CI)
cargo sqlx prepare -- --lib

cargo test
cargo clippy
```

## Summary

sqlx is the right choice when you want full SQL control with compile-time safety and your team is comfortable writing SQL. Diesel fits when you want a type-safe DSL and offline compilation. SeaORM fits when you want rapid CRUD development with familiar ORM patterns. The repository pattern lets you defer the choice or swap implementations without changing business logic. For bulk operations, always measure -- the difference between individual inserts and batched operations is orders of magnitude.

## What's Next

Exercise 24 dives into advanced macro patterns, which are heavily used by all three database libraries (sqlx's `query!`, diesel's `table!`, sea-orm's `DeriveEntityModel`). Understanding how these macros work will deepen your ability to debug compilation errors from these libraries.

## Resources

- [sqlx documentation](https://docs.rs/sqlx/latest/sqlx/)
- [sqlx offline mode](https://github.com/launchbadge/sqlx/blob/main/sqlx-cli/README.md)
- [Diesel getting started](https://diesel.rs/guides/getting-started)
- [SeaORM tutorial](https://www.sea-ql.org/SeaORM/docs/index/)
- [PostgreSQL UNNEST](https://www.postgresql.org/docs/current/functions-array.html)
