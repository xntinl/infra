use anyhow::Result;
use shared::init_tracing;

#[tokio::main]
async fn main() -> Result<()> {
    init_tracing();
    tracing::info!("starting health-api");

    let app = app_http::router();

    #[cfg(feature = "lambda")]
    {
        lambda_http::run(app).await.map_err(|e| anyhow::anyhow!(e))?;
    }

    #[cfg(not(feature = "lambda"))]
    {
        let listener = tokio::net::TcpListener::bind("0.0.0.0:8081").await?;
        tracing::info!("listening on 0.0.0.0:8081");
        axum::serve(listener, app).await?;
    }

    Ok(())
}
