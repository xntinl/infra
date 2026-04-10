# 33. Build a REST API Framework

**Difficulty**: Insane

---

## Prerequisites

- Elixir macros and compile-time DSL design
- HTTP/1.1 and REST conventions (HATEOAS, content negotiation)
- JSON Schema and OpenAPI 3.0 specification structure
- Cursor and offset pagination patterns
- Elixir behaviours for pluggable adapters
- Ecto schemas as a model reference (not required as a dependency)
- YAML generation from Elixir data structures

---

## Problem Statement

Build a framework for defining REST APIs that generates routes, validates requests, shapes responses, and automatically produces an OpenAPI 3.0 specification from the same source of truth. The framework must:

1. Provide a resource DSL that generates standard CRUD routes from a single declaration
2. Validate all incoming request parameters, headers, and bodies against a schema before the controller action runs
3. Shape response bodies by selecting fields, embedding nested resources, and enforcing a consistent envelope
4. Auto-generate filter, sort, and pagination parameters from the resource schema definition
5. Implement both cursor-based and offset-based pagination with hypermedia links
6. Derive a complete OpenAPI 3.0 YAML/JSON specification from the DSL declarations without any duplication

---

## Acceptance Criteria

- [ ] Resource DSL: `resource :users, UserController, schema: UserSchema` generates routes `GET /users`, `GET /users/:id`, `POST /users`, `PUT /users/:id`, `PATCH /users/:id`, `DELETE /users/:id`; each route dispatches to the corresponding controller action; nested resources `resources :posts, under: :users` generate `/users/:user_id/posts`
- [ ] Request validation: each action has a declared parameter schema; path params, query params, and body are validated against their schemas before the action runs; validation errors return `422 Unprocessable Entity` with a JSON body listing field-level errors; unknown fields in the body are rejected with a configurable policy
- [ ] Response shaping: `fields=name,email` query param limits response fields to the named subset; `include=posts,comments` embeds related resources inline; responses are wrapped in `{"data": ...}` for single resources and `{"data": [...], "meta": {...}, "links": {...}}` for collections
- [ ] Filtering: query params `?age_gte=18&role=admin&created_after=2024-01-01` are automatically parsed and applied when the resource schema declares the corresponding filterable fields; non-filterable fields are rejected with a `400` error
- [ ] Sorting: `?sort=created_at:desc,name:asc` sorts the result set; the framework validates that the sort fields are declared as sortable in the schema; multiple sort fields are applied in order
- [ ] Pagination: `?page=2&per_page=25` implements offset pagination; `?cursor=base64token&limit=25` implements cursor pagination; responses include `"links": {"prev": "...", "next": "...", "first": "..."}` in the collection envelope; the framework handles cursor generation and decoding
- [ ] OpenAPI 3.0: `GET /openapi.json` and `GET /openapi.yaml` return a complete OpenAPI 3.0 specification derived from all registered resources; paths, operations, parameters, request bodies, and response schemas are generated automatically; the spec validates against the OpenAPI 3.0 JSON Schema

---

## What You Will Learn

- Compile-time DSL design for resource-oriented APIs
- JSON Schema validation and the difference between schema and validation logic
- OpenAPI 3.0 spec structure and how to generate it programmatically
- Cursor pagination mechanics: encoding position as an opaque token
- Field selection and resource embedding as response transformation steps
- Filter/sort parameter parsing and safe query building patterns
- HATEOAS link generation for navigable API responses

---

## Hints

- Study the JSON:API specification (jsonapi.org) — your response envelope can follow this standard or define its own
- Research OpenAPI 3.0 spec structure; the `components/schemas` section allows reuse across operations
- Cursor pagination requires encoding enough state to reproduce the "next page" query; think about what that state must include for sorted, filtered queries
- Investigate how `Ecto.Schema` uses compile-time attribute accumulation — your schema DSL can use the same approach
- Think about how field selection interacts with embedded resources — selecting `user.name` should not trigger a query for `user.posts` if `include=posts` is not requested
- Look into YAML generation — Elixir maps can be serialized to YAML with a library or by implementing a simple encoder for the subset you need

---

## Reference Material

- OpenAPI 3.0 Specification (spec.openapis.org)
- JSON:API Specification (jsonapi.org)
- "Building APIs You Won't Hate" — Phil Sturgeon
- Swagger/OpenAPI tooling for spec validation (swagger.io/tools/swagger-editor)
- JSONAPI Elixir library source for response shaping reference (github.com/jeregrine/jsonapi)

---

## Difficulty Rating ★★★★★★

Deriving a complete, valid OpenAPI 3.0 spec from a macro DSL while implementing correct cursor pagination, field selection, and schema validation simultaneously tests the full breadth of API design knowledge.

---

## Estimated Time

60–90 hours
