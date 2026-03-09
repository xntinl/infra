use serde::{Deserialize, Serialize};
use std::future::Future;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CloudEvent {
    pub id: String,
    pub source: String,
    pub event_type: String,
    pub data: serde_json::Value,
}

pub trait EventPublisher: Send + Sync {
    fn publish(
        &self,
        event: CloudEvent,
    ) -> impl Future<Output = Result<(), EventError>> + Send;
}

#[derive(Debug, thiserror::Error)]
pub enum EventError {
    #[error("publish failed: {0}")]
    PublishFailed(String),
    #[error("serialization error: {0}")]
    Serialization(String),
}
