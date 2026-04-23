# Contributing to terraform-provider-monotaur

This document describes the conventions and patterns used in this provider so
that new resources follow the same structure as existing ones.

## Prerequisites

- Go 1.25+
- Access to the Monotaur API for acceptance tests (optional for local development)

## Repository layout

```
internal/
  api/           Generated oapi-codegen client (do not edit by hand)
  client/        High-level wrapper: JSON:API helpers, ETag, error decoding
  provider/      Terraform plugin-framework resources and data sources
main.go
openapi.json    Source of truth for API types
oapi-codegen.yaml  Code-generation config
```

## Per-resource implementation pattern

Every managed resource follows a vertical slice:

```
internal/provider/<name>_resource.go
internal/provider/<name>_data_source.go
internal/provider/<name>_resource_test.go
```

### 1. Identify the API shape

Open `internal/api/api.gen.go` and search for `Attributes` and `Relationships`
types for your resource. For a resource named `widget` you will find:

| Generated type | Purpose |
|---|---|
| `AttributesInCreateWidgetRequest` | Fields sent on POST |
| `AttributesInUpdateWidgetRequest` | Fields sent on PATCH |
| `AttributesInWidgetResponse` | Fields returned by GET/POST/PATCH |
| `RelationshipsInCreateWidgetRequest` | Relationships sent on POST |
| `RelationshipsInUpdateWidgetRequest` | Relationships sent on PATCH |
| `RelationshipsInWidgetResponse` | Relationships returned by GET/POST/PATCH |
| `DataInWidgetResponse` | Top-level data object (has `Id`, `Type`, `Attributes`, `Relationships`) |

### 2. Define the Terraform schema

Create `internal/provider/<name>_resource.go`. Use these mapping rules:

| API field category | Schema declaration |
|---|---|
| Required create attribute | `Required: true` |
| Optional create attribute | `Optional: true, Computed: true` |
| Server-assigned field (`id`, `createDateTime`, `name`, etc.) | `Computed: true` (+ `UseStateForUnknown` for `id` and `createDateTime`) |
| To-many relationship | `schema.ListAttribute{ElementType: types.StringType, Optional: true, Computed: true}`, attribute name `<relation>_ids` |
| To-one relationship | `schema.StringAttribute{Optional: true}`, attribute name `<relation>_id` |

**`UseStateForUnknown` for Optional+Computed attributes**

Every attribute that is both `Optional: true` and `Computed: true` must include
`stringplanmodifier.UseStateForUnknown()` (or the appropriate type-specific
plan modifier, e.g. `listplanmodifier.UseStateForUnknown()`). Without it,
Terraform will produce a spurious plan diff on every refresh because the
computed value is treated as unknown.

```go
"color": schema.StringAttribute{
    Optional: true,
    Computed: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),
    },
},
"component_ids": schema.ListAttribute{
    Optional:    true,
    Computed:    true,
    ElementType: types.StringType,
    PlanModifiers: []planmodifier.List{
        listplanmodifier.UseStateForUnknown(),
    },
},
```

`openapi:discriminator` fields on API structs are internal to JSON:API and must
NOT be surfaced in the Terraform schema.

### 3. Implement the model struct

Define a `<name>ResourceModel` struct with `tfsdk` tags matching the schema
keys:

```go
type widgetResourceModel struct {
    ID             types.String `tfsdk:"id"`
    Name           types.String `tfsdk:"name"`
    // ... all schema attributes
    ComponentIDs   types.List   `tfsdk:"component_ids"`
}
```

### 4. Implement CRUD methods

Each method follows the same pattern:

1. Read plan/state into the model struct.
2. Build the API request body using generated types from `internal/api`.
3. Call the appropriate generated method on `c.client.Inner()`.
4. Check the response with `client.CheckResponse(resp)`.
5. Decode with `client.UnmarshalDocument[api.DataIn<Name>Response](resp.Body)`.
6. Call the `flatten<Name>` helper to map the API response back into the model.
7. Save the updated model to state with `resp.State.Set(ctx, &model)`.

```go
// Create — POST /widgets
func (r *widgetResource) Create(ctx context.Context, ...) {
    var plan widgetResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    // build body, call Inner().PostWidget..., flatten, set state
}

// Read — GET /widgets/{id}
// Update — PATCH /widgets/{id}
// Delete — DELETE /widgets/{id}
// ImportState — sets "id" from the import argument; Read is invoked automatically
```

**ImportState pattern**

Always use `resource.ImportStatePassthroughID` rather than a manual
implementation. The helper writes the import argument into the `id` attribute
and the framework then invokes `Read` automatically to populate the rest of
state:

```go
func (r *widgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

Import the `"github.com/hashicorp/terraform-plugin-framework/path"` package.

### 5. Implement the flatten helper

`flatten<Name>` converts a `api.DataIn<Name>Response` into the model struct.
It is a pure function (no side effects, no API calls) and must handle nil
attribute/relationship pointers gracefully:

```go
func flattenWidget(ctx context.Context, data api.DataInWidgetResponse, model *widgetResourceModel) diag.Diagnostics {
    model.ID = types.StringValue(data.Id)
    if data.Attributes != nil {
        // map every attribute field
    }
    if data.Relationships != nil {
        // map every relationship using stringListToSlice / types.ListValueFrom
    } else {
        // set all relationship lists to empty (not null)
        model.ComponentIDs = types.ListValueMust(types.StringType, nil)
    }
    return diags
}
```

Use `types.StringNull()` for absent optional strings and
`types.ListValueMust(types.StringType, nil)` for absent relationship lists so
that the Terraform state is always populated (not unknown).

**Timestamp formatting**

Always format `time.Time` values using `time.RFC3339` — never use
`time.Time.String()` which produces Go's default layout and is not a valid
RFC 3339 timestamp:

```go
// correct
model.CreateDateTime = types.StringValue(attrs.CreateDateTime.Format(time.RFC3339))

// wrong — .String() returns a Go-specific layout, not RFC 3339
model.CreateDateTime = types.StringValue(attrs.CreateDateTime.String())
```

Schema docstrings for timestamp attributes should read: "RFC 3339 timestamp".

### 6. Implement the data source

Create `internal/provider/<name>_data_source.go`. The data source:

- Accepts only `id` as a required input attribute.
- All other attributes are `Computed: true`.
- Calls `GetWidget(ctx, id, &api.GetWidgetParams{})` in its `Read` method.
- Reuses `flatten<Name>` by mapping through a temporary `<name>ResourceModel`.

### 7. Register in provider.go

Add constructors to the `Resources` and `DataSources` slices in
`internal/provider/provider.go`:

```go
func (p *MonotaurProvider) Resources(_ context.Context) []func() resource.Resource {
    return []func() resource.Resource{
        NewWidgetResource,
        // ...
    }
}

func (p *MonotaurProvider) DataSources(_ context.Context) []func() datasource.DataSource {
    return []func() datasource.DataSource{
        NewWidgetDataSource,
        // ...
    }
}
```

### 8. Write tests

Create `internal/provider/<name>_resource_test.go`. At minimum:

**Unit tests** (no live API, no `TF_ACC`):

- Export a thin test helper from the resource file:

  ```go
  // test_exports.go (or at the bottom of <name>_resource.go)
  type WidgetResourceModelForTest = widgetResourceModel
  func FlattenWidgetForTest(ctx context.Context, data api.DataInWidgetResponse, model *widgetResourceModel) diag.Diagnostics {
      return flattenWidget(ctx, data, model)
  }
  ```

- Write table-driven tests that call `FlattenWidgetForTest` with fixture data
  and assert every field in the model.
- Cover: all attributes set, nil optional attributes, nil relationships block,
  each relationship populated individually.

**Acceptance tests** (require `TF_ACC=1` and a live endpoint):

- Guard with:
  ```go
  if os.Getenv("TF_ACC") == "" {
      t.Skip("Set TF_ACC=1 to run acceptance tests")
  }
  ```
- Follow the sequence: create → plan (expect no changes) → update → import →
  destroy.

### 9. Verify

```bash
go build ./...          # must produce no output
go vet ./...            # must produce no output
go test ./... -run TestFlatten -v
```

## Running Acceptance Tests

Acceptance tests exercise the provider against a real Monotaur API instance.
They are env-gated via `TF_ACC` so that `go test ./...` (and CI unit-test jobs)
never dial an external service.

### Required environment variables

| Variable | Description |
|---|---|
| `MONOTAUR_ENDPOINT` | Base URL of the Monotaur API, e.g. `https://api.example.monotaur.io` |
| `MONOTAUR_API_KEY` | A valid API key with sufficient permissions to create/update/delete all resource types under test |

### Running the full suite

```bash
export MONOTAUR_ENDPOINT=https://api.example.monotaur.io
export MONOTAUR_API_KEY=mtat_...

make testacc
```

`make testacc` expands to:

```bash
TF_ACC=1 go test ./... -v -timeout 120m
```

### Running a single acceptance test

```bash
TF_ACC=1 MONOTAUR_ENDPOINT=https://api.example.monotaur.io \
         MONOTAUR_API_KEY=mtat_... \
  go test ./internal/provider/ -run TestAccLabelResource_basic -v
```

### Pointing at a local or staging instance

Set `MONOTAUR_ENDPOINT` to your local or staging base URL:

```bash
export MONOTAUR_ENDPOINT=http://localhost:3000
export MONOTAUR_API_KEY=<key-from-local-seed>
make testacc
```

### How acceptance tests are structured

Each acceptance test:

1. Guards execution with a check for `TF_ACC=1` (or calls `testAccPreCheck(t)`
   which also verifies the required env vars are present).
2. Uses `testAccProtoV6ProviderFactories` (defined in
   `internal/provider/provider_test.go`) to wire the local provider binary
   into the Terraform testing framework.
3. Follows the sequence: create → plan (expect no changes) → update → import →
   destroy — exercising all CRUD operations and import support.

## Relationship ID conventions

| Cardinality | Terraform attribute name | API JSON:API type |
|---|---|---|
| to-many | `<relation>_ids` (e.g. `component_ids`) | `[]<Type>IdentifierInRequest` with the appropriate `ResourceType` constant |
| to-one | `<relation>_id` (e.g. `monitor_id`) | `ToOne<Type>InRequest` |

Always use the `api.ResourceType<Name>` constant (e.g. `api.ResourceTypeComponents`)
for the `Type` field of identifier objects rather than a raw string literal.

## Client conventions

- Use `r.client.Inner()` to access the generated `api.ClientInterface`.
- Use `client.CheckResponse(resp)` to check HTTP status; it returns
  `client.ErrNotModified` for 304, an `*client.APIError` for 4xx/5xx.
- Use `client.UnmarshalDocument[T](resp.Body)` to decode single-resource
  responses and `client.UnmarshalCollectionDocument[T](resp.Body)` for lists.
- The `Configure` method on each resource/data source must type-assert
  `req.ProviderData` to `*client.Client`.

## Code style

- Follow standard Go conventions: `gofmt`, no unused imports, no unused variables.
- Use `tflog.Debug` for observability in CRUD methods (log before and after the
  API call with the resource ID).
- Keep error messages in the form `"Error <Verb>ing <Resource>"` / `"<detail>"`.
- Do not export internal helpers unless needed by tests; use the
  `<Name>ForTest` / `<Model>ForTest` export pattern described above.
