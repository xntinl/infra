use std::future::Future;

pub trait ObjectStorage: Send + Sync {
    fn get(
        &self,
        key: &str,
    ) -> impl Future<Output = Result<Vec<u8>, StorageError>> + Send;

    fn put(
        &self,
        key: &str,
        data: Vec<u8>,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;

    fn delete(
        &self,
        key: &str,
    ) -> impl Future<Output = Result<(), StorageError>> + Send;
}

#[derive(Debug, thiserror::Error)]
pub enum StorageError {
    #[error("object not found: {0}")]
    NotFound(String),
    #[error("storage unavailable: {0}")]
    Unavailable(String),
}
