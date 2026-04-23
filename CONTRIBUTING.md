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
// ImportState — GET /widgets/{id}, set full state
```

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

- Guard with `if !acctest.IsAccTest() { t.Skip(...) }` (or a manual
  `os.Getenv("TF_ACC") == ""` check until the testing dependency is added).
- Follow the sequence: create → plan (expect no changes) → update → import →
  destroy.

### 9. Verify

```bash
go build ./...          # must produce no output
go vet ./...            # must produce no output
go test ./... -run TestFlatten -v
```

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
