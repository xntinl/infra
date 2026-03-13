# 5. S3 Event Processing

<!--
difficulty: advanced
concepts: [s3-event, object-key, url-decoding, event-record, object-processing]
tools: [go, aws-cli]
estimated_time: 35m
bloom_level: analyze
prerequisites: [lambda-handler-patterns, io-filesystem, json-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Lambda Handler Patterns exercise
- Understanding of I/O streams and JSON encoding
- Familiarity with S3 concepts (buckets, keys, objects)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a Lambda handler that processes S3 event notifications
- **Analyze** S3 event record structure including bucket name, object key, and event type
- **Handle** URL-encoded object keys correctly
- **Design** an event processor that filters by event type and file extension

## Why S3 Event Processing Matters

S3 event notifications trigger Lambda functions when objects are created, deleted, or modified. This pattern drives image processing pipelines, log ingestion, data transformation, and file validation workflows. A common mistake is forgetting that S3 object keys are URL-encoded in the event -- a file named `my folder/report.csv` arrives as `my+folder/report.csv`.

Understanding S3 event structure and processing patterns is fundamental to building data pipelines on AWS.

## The Problem

Build a Lambda handler that processes S3 PUT events for a data ingestion pipeline. When a CSV file is uploaded to a bucket, the handler should:

1. Extract the bucket and key from the event
2. Decode the URL-encoded key
3. Filter to only process `.csv` files
4. Simulate processing the file (log details, count would-be rows)
5. Handle multiple records in a single event

## Requirements

1. **Handler signature** -- `func(ctx context.Context, event events.S3Event) error`
2. **URL decoding** -- use `url.QueryUnescape` on the object key (S3 uses `+` for spaces)
3. **Extension filtering** -- only process objects ending in `.csv`; log and skip all others
4. **Multi-record handling** -- iterate over all `event.Records`; process each independently
5. **Event type check** -- only process `ObjectCreated:Put` events
6. **Result struct** -- return processing details including bucket, key, size, and event type
7. **Tests** -- cover normal CSV upload, non-CSV skip, URL-encoded keys, and multiple records

## Hints

<details>
<summary>Hint 1: Decoding the object key</summary>

```go
import "net/url"

func decodeKey(encodedKey string) (string, error) {
    // S3 replaces spaces with '+', which url.QueryUnescape handles
    return url.QueryUnescape(encodedKey)
}
```

</details>

<details>
<summary>Hint 2: Checking file extension</summary>

```go
import "path/filepath"

if filepath.Ext(decodedKey) != ".csv" {
    log.Printf("skipping non-CSV file: %s", decodedKey)
    continue
}
```

</details>

<details>
<summary>Hint 3: Fabricating S3 test events</summary>

```go
event := events.S3Event{
    Records: []events.S3EventRecord{
        {
            EventName: "ObjectCreated:Put",
            S3: events.S3Entity{
                Bucket: events.S3Bucket{Name: "my-bucket"},
                Object: events.S3Object{
                    Key:  "data/report+2024.csv",
                    Size: 1024,
                },
            },
        },
    },
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- A CSV upload event is processed and logs correct bucket/key/size
- URL-encoded keys like `my+folder/data.csv` decode to `my folder/data.csv`
- Non-CSV files (`.json`, `.txt`) are skipped without error
- Multiple records in one event are all processed
- Non-PUT events are skipped

## What's Next

Continue to [06 - Kubernetes client-go](../06-kubernetes-client-go/06-kubernetes-client-go.md) to learn how to interact with a Kubernetes cluster from Go.

## Summary

- S3 event notifications arrive as `events.S3Event` containing one or more `S3EventRecord` entries
- Object keys are URL-encoded -- always use `url.QueryUnescape` before using the key
- Filter by `EventName` (e.g., `ObjectCreated:Put`) and file extension to process only relevant objects
- Process each record independently; do not let one failure prevent processing the rest
- S3 events are one of the most common Lambda triggers for data pipeline workloads

## Reference

- [events.S3Event](https://pkg.go.dev/github.com/aws/aws-lambda-go/events#S3Event)
- [S3 event notification types](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-event-types-and-destinations.html)
- [Lambda with S3](https://docs.aws.amazon.com/lambda/latest/dg/with-s3.html)
