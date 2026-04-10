# 52. Build a Multi-tenant SaaS Framework
**Difficulty**: Insane

## Prerequisites
- Mastered: Ecto multi-tenancy patterns, PostgreSQL schemas and row-level security, Phoenix plugs and router pipelines, GenServer and process registries, JWT libraries (Joken/JOSE), ETS-based rate limiting, PubSub, telemetry
- Study first: "Multi-tenant SaaS Architecture" (AWS whitepaper), Triplex library source on GitHub, Stripe billing docs and webhook verification, PostgreSQL Row Level Security official docs, "The Art of SaaS" (Fowler patterns), Oban docs for background jobs

## Problem Statement
Build a production-grade multi-tenant SaaS framework in Elixir that allows multiple isolated tenants to share a single application deployment while maintaining strict data separation, per-tenant configuration, and a billing lifecycle — without any tenant ever touching another tenant's data.

1. Implement tenant isolation at the database layer: support both schema-per-tenant isolation (each tenant gets its own PostgreSQL schema with full table set) and row-level-security isolation (single schema with `tenant_id` column and PostgreSQL RLS policies). The isolation strategy must be configurable per deployment without changing application code.
2. Build a tenant resolution pipeline as a Plug: extract tenant identity from subdomain (`acme.app.com`), custom HTTP header (`X-Tenant-ID`), or a claim embedded in the JWT (`"tenant"` key). Once resolved, store the current tenant in the process dictionary and thread it through every Ecto query automatically via a custom Ecto.Repo.
3. Implement tenant provisioning as an atomic operation: creating a tenant creates its schema (or seeds its RLS rows), runs all pending Ecto migrations scoped to that tenant, inserts required seed data (default roles, settings, admin user), and creates a Stripe customer — all within a single database transaction rolled back completely on any step failure.
4. Add per-tenant rate limiting using a token bucket algorithm backed by ETS: each tenant has its own bucket with configurable refill rate and burst capacity; rate limits are enforced at the Plug level before any application logic runs; bucket state survives Plug process restarts via ETS public tables owned by a dedicated GenServer.
5. Implement feature flags per tenant: a tenant can have features enabled/disabled or assigned to a cohort for A/B testing; flag evaluation must be sub-millisecond (ETS lookup); flags are updated via admin API and propagate to all nodes within 1 second via PubSub; support gradual rollout by percentage of tenants.
6. Build usage metering: count API calls, storage bytes written, and active user sessions per tenant within rolling 1-hour and 30-day windows; persist counters to PostgreSQL every 60 seconds via Oban job; expose `Metering.get_usage(tenant_id, :api_calls, :last_30_days)` with consistent reads.
7. Integrate Stripe billing: receive Stripe webhooks, verify the signature, and map webhook events (`invoice.paid`, `customer.subscription.updated`, `customer.subscription.deleted`) to tenant plan changes; enforce plan limits (e.g., max users, max API calls/day) by checking metered usage against the active plan at request time.
8. Build an admin panel (LiveView or JSON API): list all tenants with usage stats, current plan, and last active timestamp; impersonate any tenant (switch tenant context for the admin session); view per-tenant metrics charts; manually override feature flags; suspend or delete a tenant with cascading cleanup.

## Acceptance Criteria
- [ ] Tenant isolation: queries from tenant A never return rows belonging to tenant B — verified by a property-based test that inserts data under one tenant and asserts it is invisible from another, for both schema-level and RLS isolation modes
- [ ] Tenant resolution: a request arriving with subdomain, header, or JWT claim resolves to the correct tenant context; an unresolvable tenant returns 404 (not 401); resolution adds under 1 ms of overhead measured by a plug benchmark
- [ ] Tenant provisioning: `Provisioning.create_tenant/1` creates schema + runs migrations + seeds + creates Stripe customer atomically; if Stripe API returns an error the entire transaction is rolled back and no partial schema exists in PostgreSQL
- [ ] Rate limiting: a tenant configured for 100 req/s cannot exceed that rate — verified by a test that sends 200 requests in 500 ms and asserts exactly 100 succeed (±2%); buckets for different tenants are fully independent and do not interfere
- [ ] Feature flags: flag state changes via admin API propagate to all nodes within 1 second; evaluating a flag for a tenant is a pure ETS lookup with no database call; gradual rollout at 50% assigns consistently (same tenant always gets the same variant)
- [ ] Usage metering: `Metering.get_usage/3` returns accurate counts within ±5% of actual; counters flush to PostgreSQL every 60 seconds without blocking request handling; a tenant that exceeds its plan limit receives 429 with `Retry-After` header
- [ ] Billing integration: receiving a `customer.subscription.updated` webhook with a new plan immediately updates the tenant's active plan and enforces new limits on the next request; invalid webhook signatures return 400 and are logged with full payload for audit
- [ ] Admin panel: lists all tenants with real-time usage; impersonating tenant X allows the admin to see exactly what tenant X sees with no data leakage to/from the admin's own tenant; suspending a tenant causes all subsequent requests from that tenant to return 403 within 500 ms of suspension

## What You Will Learn
- Database multi-tenancy trade-offs: schema isolation vs. row-level security — when each is appropriate and what PostgreSQL features each relies on
- Building Plug pipelines that carry scoped context through every layer without polluting function signatures
- Atomic provisioning flows that span multiple external systems (database, Stripe) with all-or-nothing rollback
- ETS as a first-class production data structure: ownership, concurrency guarantees, and durability strategies
- Webhook security: signature verification, idempotency keys, and exactly-once processing patterns
- Gradual feature rollout mechanics: consistent hashing for cohort assignment, flag propagation via PubSub
- Billing lifecycle state machines and the edge cases that break naive implementations (dunning, proration, upgrade mid-cycle)

## Hints (research topics, NO tutorials)
- Study how Triplex wraps Ecto.Repo to prefix every query with the current schema — then implement your own version to understand the mechanics before using the library
- For RLS: `SET LOCAL app.current_tenant = '...'` inside a transaction sets a session variable that PostgreSQL RLS policies can read via `current_setting('app.current_tenant')`
- ETS `update_counter/4` is atomic — use it for token bucket increment/decrement without a GenServer bottleneck per request
- Stripe webhook idempotency: store processed `stripe_event_id` in a database table; if you receive the same event twice, return 200 immediately without re-processing
- For flag propagation: use `Phoenix.PubSub.broadcast/3` on flag update + `Phoenix.PubSub.subscribe/2` in each node's flag cache GenServer to invalidate and reload
- Schema-per-tenant migrations: run `Ecto.Migrator.run/4` scoped to each tenant's schema in a Task.async_stream to provision N tenants in parallel during deployment

## Reference Material
- "Multi-tenant SaaS Architecture" — AWS Whitepaper (search AWS docs)
- PostgreSQL Row Level Security: https://www.postgresql.org/docs/current/ddl-rowsecurity.html
- Triplex library: https://github.com/ateliware/triplex
- Stripe Webhooks guide: https://stripe.com/docs/webhooks
- Stripe signature verification: `Stripe.WebhookPlug` implementation as reference
- "Patterns of Enterprise Application Architecture" — Fowler, chapter on multi-tenancy
- Oban documentation: https://hexdocs.pm/oban

## Difficulty Rating ★★★★★★☆
The challenge is not any single component — it is the intersection of database isolation guarantees, atomic cross-system provisioning, sub-millisecond flag evaluation at request time, and a correct billing lifecycle that handles the full Stripe event state machine without data corruption under concurrent webhooks.

## Estimated Time
80–140 hours
