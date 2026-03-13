# 6. OCI Image Pulling

<!--
difficulty: insane
concepts: [oci-image-spec, container-registry, image-manifest, layer-extraction, content-addressable-storage, docker-registry-v2]
tools: [go, linux, http-client]
estimated_time: 3h
bloom_level: create
prerequisites: [section 38 exercises 1-5, section 17 http programming, section 18 encoding json, oci image spec basics]
-->

## Prerequisites

- Go 1.22+ installed
- Linux environment with root access
- Completed exercises 1-5 (namespaces, rootfs, networking, cgroups, overlayfs)
- Familiarity with HTTP APIs and JSON parsing (sections 17-18)
- Understanding of container image concepts (layers, manifests, digests)
- Network access to Docker Hub or another OCI-compliant registry

## Learning Objectives

- **Create** an OCI image puller that downloads manifests, configs, and layers from a container registry
- **Design** a content-addressable storage system for caching downloaded image layers locally
- **Evaluate** the OCI Distribution Specification and its authentication, content negotiation, and error handling requirements

## The Challenge

Up to now, your container runtime has required manually prepared root filesystems. Real container runtimes pull images from registries -- Docker Hub, GitHub Container Registry, Amazon ECR -- using the OCI Distribution Specification (originally Docker Registry HTTP API V2). This protocol uses a REST API where you first fetch a manifest (which lists the image's layers and configuration), then download each layer as a gzipped tarball, and extract them in order to construct the root filesystem.

In this exercise, you will implement an image puller that speaks the OCI Distribution protocol. You will handle the authentication flow (Docker Hub uses token-based auth with a separate token service), parse image manifests and configuration objects, download layers with progress reporting, verify content by SHA256 digest, and extract layers into the overlay filesystem structure from exercise 5.

The protocol has several non-obvious details. Content negotiation via `Accept` headers determines whether you get an OCI manifest or a Docker manifest. Multi-platform images use a manifest list (or OCI index) that contains pointers to per-platform manifests, and you must select the correct one for `linux/amd64`. Layer media types determine the compression format. And Docker Hub specifically requires a `Bearer` token obtained from `auth.docker.io` before you can pull from `registry-1.docker.io`.

## Requirements

1. Implement the OCI Distribution token authentication flow (anonymous and token-based)
2. Fetch and parse image manifests (`application/vnd.oci.image.manifest.v1+json` and Docker v2 format)
3. Handle manifest lists / OCI indexes by selecting the correct platform (`linux/amd64`)
4. Download image layers as gzipped tarballs with SHA256 digest verification
5. Implement a content-addressable store: layers cached by digest, skip re-downloading existing layers
6. Extract layers into the overlay filesystem directory structure from exercise 5
7. Parse the image configuration to extract environment variables, entrypoint, cmd, and working directory
8. Display download progress (bytes downloaded, percentage, layer count)
9. Implement an `--image` flag accepting standard image references (e.g., `alpine:3.19`, `ubuntu:22.04`)
10. Parse image references into registry, repository, and tag components (handle default registry `docker.io`)
11. Apply image configuration (env vars, working directory, entrypoint) when starting the container
12. Support pulling from registries using HTTPS with TLS verification

## Hints

- Docker Hub token auth: `GET https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull` returns a JSON token.
- Set `Authorization: Bearer <token>` on subsequent requests to `https://registry-1.docker.io/v2/`.
- The manifest endpoint is `GET /v2/<name>/manifests/<reference>`. Set the `Accept` header to request specific manifest types.
- Layer blobs are at `GET /v2/<name>/blobs/<digest>`. The registry may redirect (HTTP 307) to a CDN.
- Use `crypto/sha256` to verify digests as you download. Stream through a `hash.Hash` writer.
- Use `archive/tar` and `compress/gzip` to extract layers. Apply them in order to create the overlay lower directories.

## Success Criteria

1. The puller authenticates with Docker Hub and downloads image manifests
2. Multi-platform images are resolved to the correct `linux/amd64` manifest
3. All layers are downloaded, verified by digest, and cached locally
4. Subsequent pulls of the same image skip already-cached layers
5. Layers are correctly extracted and arranged for overlay filesystem mounting
6. Image configuration (env, entrypoint, cmd, workdir) is parsed and applied
7. Progress is displayed during download showing bytes and percentages
8. The full pipeline works: `--image alpine:3.19` results in a running Alpine container

## Research Resources

- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec/blob/main/spec.md) -- the registry API protocol
- [OCI Image Specification](https://github.com/opencontainers/image-spec/blob/main/spec.md) -- manifest, config, and layer formats
- [Docker Registry HTTP API V2](https://docs.docker.com/registry/spec/api/) -- Docker-specific extensions and auth flow
- [Docker Hub Token Authentication](https://docs.docker.com/registry/spec/auth/token/) -- token service flow
- [go-containerregistry library](https://github.com/google/go-containerregistry) -- reference Go implementation (study, do not import wholesale)
- [Skopeo source code](https://github.com/containers/skopeo) -- another reference implementation for registry interactions

## What's Next

The next exercise implements full container lifecycle management -- creating, starting, stopping, and removing containers with persistent state tracking.

## Summary

- OCI Distribution Specification defines a REST API for pulling and pushing container images
- Image manifests list layers (as digests) and point to the image configuration
- Multi-platform images use manifest lists that must be resolved to the correct architecture
- Docker Hub requires token-based authentication via a separate auth service
- Content-addressable storage caches layers by SHA256 digest for deduplication
- Layer extraction order matters: layers are applied bottom-up to construct the filesystem
