# 9. Control Plane gRPC

<!--
difficulty: insane
concepts: [grpc, xds-protocol, control-plane, service-discovery, dynamic-configuration, protobuf, streaming-rpc, lds-cds-eds-rds]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/08-observability, 33-tcp-udp-and-networking, 13-goroutines-and-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-08 (full proxy with metrics) or equivalent data plane experience
- Familiarity with Protocol Buffers and gRPC fundamentals (service definitions, streaming RPCs)

## Learning Objectives

- **Design** a gRPC-based control plane interface that dynamically configures the data plane's listeners, routes, clusters, and endpoints
- **Create** a streaming discovery service implementing the xDS protocol pattern for incremental configuration delivery
- **Evaluate** the trade-offs between polling, long-polling, and streaming approaches for configuration distribution in a service mesh

## The Challenge

A service mesh data plane is only half the story. The control plane tells the data plane what to do: which listeners to open, which routes to configure, which upstream clusters exist, and which endpoints belong to each cluster. In Istio and other production meshes, this communication uses the xDS (discovery service) protocol -- a set of gRPC streaming APIs where the control plane pushes configuration updates to data plane proxies in real time.

You will build both sides of this interface. On the data plane side, you will implement an xDS client that connects to a control plane, subscribes to configuration resources, and applies updates to the running proxy without restarting or dropping connections. On the control plane side, you will implement a minimal xDS server that manages configuration state, tracks connected proxies, and pushes configuration updates when resources change. The protocol uses bidirectional streaming: the client sends discovery requests specifying which resources it wants, and the server sends discovery responses containing the requested resources. The client must ACK or NACK each response to indicate whether it was successfully applied.

The critical challenge is correctness during configuration transitions: when a new route is pushed, existing in-flight requests must complete under the old configuration, new routes must take effect for new requests, and partially applied configurations must not leave the proxy in an inconsistent state.

## Requirements

1. Define Protocol Buffer messages for the core resource types: `Listener` (address, port, filter chain), `Route` (match criteria, upstream cluster, timeout), `Cluster` (name, load balancing policy, health check config), and `Endpoint` (address, port, weight, health status)
2. Define a gRPC `DiscoveryService` with a bidirectional streaming RPC `StreamResources(stream DiscoveryRequest) returns (stream DiscoveryResponse)`
3. Implement the xDS request/response protocol: clients send `DiscoveryRequest` with resource type, resource names, and version info; servers respond with `DiscoveryResponse` containing resources, version, and nonce
4. Implement ACK/NACK semantics: clients include the last received nonce in subsequent requests to acknowledge successful application, or include an error detail to NACK a failed configuration
5. Implement a control plane server that stores configuration state in memory, tracks connected data plane instances, and pushes updates to all connected clients when configuration changes
6. Implement a data plane xDS client that connects to the control plane, subscribes to all four resource types, and applies received configurations to the running proxy
7. Support incremental updates: when only endpoints change, only the endpoint discovery response is sent, not the entire configuration
8. Implement configuration versioning: each resource type has an independent version, and the server only sends resources newer than the client's last acknowledged version
9. Handle control plane disconnection gracefully: the data plane must continue operating with the last known good configuration and reconnect with exponential backoff
10. Implement a configuration snapshot that atomically applies a consistent set of resources (listeners + routes + clusters + endpoints) to prevent partially applied configurations
11. Write integration tests that verify configuration push, ACK/NACK, reconnection, and atomic configuration updates

## Hints

- Use `google.golang.org/grpc` for the gRPC framework and `google.golang.org/protobuf` for Protocol Buffer message handling
- For bidirectional streaming, the server goroutine pattern is: one goroutine reads requests from the stream, another sends responses, communicating via channels
- Store per-client subscription state (last ACKed version per resource type) in a `sync.Map` keyed by the client's node ID
- For atomic configuration snapshots, use a versioned pointer (`atomic.Pointer[ConfigSnapshot]`) that is swapped atomically when a complete update is applied
- For reconnection, implement a state machine: Connecting -> Connected -> Disconnected -> Reconnecting, with exponential backoff on the Reconnecting -> Connecting transition
- Use `protobuf.Any` to wrap typed resources in the generic `DiscoveryResponse`, and unpack them on the client side using the type URL
- For incremental updates, maintain a per-resource-type hash or version and only include resources that have changed since the client's last ACK

## Success Criteria

1. The control plane server accepts gRPC connections and streams configuration to connected data plane clients
2. Data plane clients correctly apply received Listener, Route, Cluster, and Endpoint configurations to the running proxy
3. ACK/NACK semantics work correctly: NACKed configurations are re-sent, ACKed versions are tracked per client
4. Configuration updates are delivered incrementally -- unchanged resource types are not re-sent
5. Data plane continues operating with last known good configuration during control plane disconnection
6. Reconnection occurs with exponential backoff and succeeds when the control plane becomes available
7. Atomic configuration snapshots prevent partially applied configurations
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Envoy xDS protocol specification](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol) -- the authoritative specification for xDS communication
- [Go gRPC documentation](https://grpc.io/docs/languages/go/) -- gRPC framework for Go
- [Protocol Buffers Go tutorial](https://protobuf.dev/getting-started/gotutorial/) -- protobuf message definition and code generation
- [Envoy control plane (go-control-plane)](https://github.com/envoyproxy/go-control-plane) -- reference Go implementation of an xDS server
- [xDS API overview](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/operations/dynamic_configuration) -- LDS, RDS, CDS, EDS resource types explained
- [gRPC bidirectional streaming](https://grpc.io/docs/what-is-grpc/core-concepts/#bidirectional-streaming-rpc) -- streaming RPC patterns

## What's Next

Continue to [Full Data Plane](../10-full-data-plane/10-full-data-plane.md) where you will integrate all components into a complete, production-grade service mesh data plane.

## Summary

- The xDS protocol provides a standardized interface between the control plane and data plane using gRPC streaming
- Bidirectional streaming enables real-time configuration push with ACK/NACK reliability semantics
- Incremental updates minimize network overhead by only transmitting changed resources
- Configuration versioning per resource type enables independent update cadences for listeners, routes, clusters, and endpoints
- Graceful disconnection handling ensures the data plane never enters an unconfigured state
- Atomic configuration snapshots prevent inconsistencies from partially applied updates
