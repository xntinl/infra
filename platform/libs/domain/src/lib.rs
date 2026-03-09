pub mod models;

use models::{HealthResponse, HelloResponse};

pub fn greet() -> HelloResponse {
    HelloResponse {
        message: "Hello from Rust!".into(),
    }
}

pub fn health() -> HealthResponse {
    HealthResponse {
        status: "ok".into(),
    }
}

pub fn handle_scheduled_event(source: &str, detail_type: &str) {
    tracing::info!(source, detail_type, "processed scheduled event");
}
