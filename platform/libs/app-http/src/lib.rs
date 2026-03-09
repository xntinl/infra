use axum::{routing::get, Json, Router};

pub fn router() -> Router {
    Router::new()
        .route("/hello", get(hello_handler))
        .route("/health", get(health_handler))
}

async fn hello_handler() -> Json<domain::models::HelloResponse> {
    Json(domain::greet())
}

async fn health_handler() -> Json<domain::models::HealthResponse> {
    Json(domain::health())
}
