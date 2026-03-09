use anyhow::Result;
use shared::init_tracing;

#[cfg(feature = "lambda")]
#[tokio::main]
async fn main() -> Result<()> {
    init_tracing();
    tracing::info!("starting hello-scheduled");

    lambda_runtime::run(lambda_runtime::service_fn(
        |event: lambda_runtime::LambdaEvent<
            aws_lambda_events::eventbridge::EventBridgeEvent<serde_json::Value>,
        >| async move {
            domain::handle_scheduled_event(
                &event.payload.source,
                &event.payload.detail_type,
            );
            Ok::<(), Box<dyn std::error::Error + Send + Sync>>(())
        },
    ))
    .await
    .map_err(|e| anyhow::anyhow!(e))?;

    Ok(())
}

#[cfg(not(feature = "lambda"))]
#[tokio::main]
async fn main() -> Result<()> {
    init_tracing();
    tracing::info!("hello-scheduled running in local mode");
    domain::handle_scheduled_event("local", "test");
    Ok(())
}
