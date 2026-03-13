# 8. Terraform Provider Skeleton

<!--
difficulty: insane
concepts: [terraform-plugin-framework, provider, resource, data-source, schema, crud, acceptance-test]
tools: [go, terraform]
estimated_time: 90m
bloom_level: create
prerequisites: [interfaces, http-programming, json-encoding, testing-ecosystem]
-->

## Prerequisites

- Go 1.22+ installed
- Terraform 1.5+ installed
- Strong understanding of Go interfaces and HTTP clients
- Familiarity with Terraform concepts (providers, resources, data sources, state)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a Terraform provider using `terraform-plugin-framework`
- **Implement** a resource with full CRUD lifecycle (Create, Read, Update, Delete)
- **Design** a provider schema with configuration, authentication, and validation
- **Evaluate** the differences between `terraform-plugin-framework` and the older SDK

## The Challenge

Build a Terraform provider for a fictional "Inventory" API. The provider manages `inventory_item` resources with attributes: `id`, `name`, `description`, `quantity`, and `tags`. You will implement:

1. A provider that accepts a `base_url` and `api_key` configuration
2. A resource with full CRUD operations backed by an HTTP API
3. A data source for reading existing items
4. Schema validation (quantity must be >= 0, name is required)
5. Unit tests for the provider logic and acceptance tests for the full lifecycle

The Inventory API is simulated with an in-memory HTTP server in tests.

## Requirements

1. **Provider configuration** -- accept `base_url` (required) and `api_key` (optional, from env var `INVENTORY_API_KEY`)
2. **Resource schema** -- `inventory_item` with `id` (computed), `name` (required, string), `description` (optional, string), `quantity` (required, int64, >= 0), `tags` (optional, set of strings)
3. **CRUD operations** -- implement `Create`, `Read`, `Update`, `Delete` methods that call the HTTP API
4. **Data source** -- `inventory_item` data source that reads an item by name
5. **Import** -- support `terraform import` by item ID
6. **Validation** -- quantity must be non-negative, name must be non-empty
7. **Acceptance tests** -- test the full resource lifecycle with a mock HTTP server

## Hints

<details>
<summary>Hint 1: Project structure</summary>

```
terraform-provider-inventory/
├── main.go
├── internal/
│   └── provider/
│       ├── provider.go
│       ├── item_resource.go
│       ├── item_data_source.go
│       └── provider_test.go
├── go.mod
└── go.sum
```

</details>

<details>
<summary>Hint 2: Provider implementation</summary>

```go
type InventoryProvider struct {
    version string
}

type InventoryProviderModel struct {
    BaseURL types.String `tfsdk:"base_url"`
    APIKey  types.String `tfsdk:"api_key"`
}

func (p *InventoryProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "base_url": schema.StringAttribute{Required: true},
            "api_key":  schema.StringAttribute{Optional: true, Sensitive: true},
        },
    }
}
```

</details>

<details>
<summary>Hint 3: Resource CRUD</summary>

```go
func (r *ItemResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
    var plan ItemResourceModel
    diags := req.Plan.Get(ctx, &plan)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    item, err := r.client.CreateItem(ctx, Item{
        Name:        plan.Name.ValueString(),
        Description: plan.Description.ValueString(),
        Quantity:    plan.Quantity.ValueInt64(),
    })
    if err != nil {
        resp.Diagnostics.AddError("Create failed", err.Error())
        return
    }

    plan.ID = types.StringValue(item.ID)
    resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}
```

</details>

<details>
<summary>Hint 4: Mock server for testing</summary>

```go
func newMockServer() *httptest.Server {
    items := make(map[string]Item)
    mux := http.NewServeMux()

    mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
        var item Item
        json.NewDecoder(r.Body).Decode(&item)
        item.ID = uuid.New().String()
        items[item.ID] = item
        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(item)
    })
    // GET, PUT, DELETE handlers...

    return httptest.NewServer(mux)
}
```

</details>

## Success Criteria

- `terraform plan` and `terraform apply` work with your provider against the mock server
- Creating an item, reading it back, updating a field, and deleting it all succeed
- `terraform import` brings an existing item into state
- Validation prevents negative quantities and empty names
- Acceptance tests run the full lifecycle in under 30 seconds
- The provider compiles and can be installed locally with `go install`

## Research Resources

- [terraform-plugin-framework documentation](https://developer.hashicorp.com/terraform/plugin/framework)
- [terraform-plugin-framework GitHub](https://github.com/hashicorp/terraform-plugin-framework)
- [HashiCorp provider scaffolding](https://github.com/hashicorp/terraform-provider-scaffolding-framework)
- [Acceptance testing guide](https://developer.hashicorp.com/terraform/plugin/framework/acctests)

## What's Next

Continue to [09 - Container Health Checks](../09-container-health-checks/09-container-health-checks.md) to build a production-ready health check server for containerized Go applications.

## Summary

- Terraform providers are Go binaries that implement the provider protocol via `terraform-plugin-framework`
- Resources implement CRUD lifecycle methods: Create, Read, Update, Delete
- Schemas define the provider configuration and resource attributes with types and validation
- Data sources implement Read-only access to existing resources
- Acceptance tests use `httptest.Server` to simulate the backing API without external dependencies
