use ports::events::{CloudEvent, EventError, EventPublisher};

pub struct EventBridgePublisher {
    pub bus_name: String,
}

impl EventPublisher for EventBridgePublisher {
    async fn publish(&self, event: CloudEvent) -> Result<(), EventError> {
        tracing::info!(
            bus = %self.bus_name,
            event_type = %event.event_type,
            source = %event.source,
            "publishing event to EventBridge"
        );
        todo!("add aws-sdk-eventbridge dependency when needed")
    }
}
