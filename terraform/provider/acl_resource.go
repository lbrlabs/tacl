// acl_resource.go
package provider

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strconv"

    "github.com/hashicorp/terraform-plugin-framework/resource"
    "github.com/hashicorp/terraform-plugin-framework/resource/schema"
    "github.com/hashicorp/terraform-plugin-framework/types"
    "github.com/hashicorp/terraform-plugin-log/tflog"
)

// TaclACLEntry represents the new-style ACL JSON.
type TaclACLEntry struct {
    Action string   `json:"action"`          // e.g. "accept" or "deny"
    Src    []string `json:"src"`             // e.g. ["10.0.0.0/8"]
    Proto  string   `json:"proto,omitempty"` // e.g. "tcp" (optional)
    Dst    []string `json:"dst"`             // e.g. ["10.1.2.3/32:22","tag:prod:*"]
}

// Ensure interface compliance: we need Resource + ResourceWithConfigure.
var (
    _ resource.Resource              = &aclResource{}
    _ resource.ResourceWithConfigure = &aclResource{}
)

// NewACLResource is the constructor for "tacl_acl" resource (new-style).
func NewACLResource() resource.Resource {
    return &aclResource{}
}

// aclResource implements resource.Resource for "tacl_acl".
type aclResource struct {
    httpClient *http.Client
    endpoint   string
}

// aclResourceModel => Terraform schema mapping.
type aclResourceModel struct {
    // ID is the index in TACL’s /acls array, stored as a string (e.g. "0").
    ID     types.String   `tfsdk:"id"`
    Action types.String   `tfsdk:"action"`
    Src    []types.String `tfsdk:"src"`
    Proto  types.String   `tfsdk:"proto"`
    Dst    []types.String `tfsdk:"dst"`
}

// -----------------------------------------------------------------------------
// 1) Configure
// -----------------------------------------------------------------------------

func (r *aclResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    // Retrieve the provider’s data
    if req.ProviderData == nil {
        return
    }
    provider, ok := req.ProviderData.(*taclProvider)
    if !ok {
        return
    }
    r.httpClient = provider.httpClient
    r.endpoint = provider.endpoint
}

// -----------------------------------------------------------------------------
// 2) Metadata
// -----------------------------------------------------------------------------

func (r *aclResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    // Final type name => "tacl_acl"
    resp.TypeName = req.ProviderTypeName + "_acl"
}

// -----------------------------------------------------------------------------
// 3) Schema
// -----------------------------------------------------------------------------

func (r *aclResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Description: "Manages a single new-style ACL entry in TACL’s /acls array.",
        Attributes: map[string]schema.Attribute{
            "id": schema.StringAttribute{
                Description: "Index of this ACL entry in TACL’s array (stored as a string).",
                Computed:    true,
            },
            "action": schema.StringAttribute{
                Description: "The ACL action, e.g. 'accept' or 'deny'.",
                Required:    true,
            },
            "src": schema.ListAttribute{
                Description: "List of source CIDRs, tags, or hostnames.",
                Required:    true,
                ElementType: types.StringType,
            },
            "proto": schema.StringAttribute{
                Description: "Protocol, e.g. 'tcp' or 'udp' (optional).",
                Optional:    true,
            },
            "dst": schema.ListAttribute{
                Description: "List of destination CIDRs, tags, or hostnames (maybe with :port).",
                Required:    true,
                ElementType: types.StringType,
            },
        },
    }
}

// -----------------------------------------------------------------------------
// 4) Create
// -----------------------------------------------------------------------------

func (r *aclResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
    var data aclResourceModel

    // Get user inputs from the plan
    diags := req.Plan.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // Convert to JSON payload
    newACL := TaclACLEntry{
        Action: data.Action.ValueString(),
        Src:    toGoStringSlice(data.Src),
        Proto:  data.Proto.ValueString(),
        Dst:    toGoStringSlice(data.Dst),
    }

    // POST /acls
    postURL := fmt.Sprintf("%s/acls", r.endpoint)
    tflog.Debug(ctx, "Creating ACL via TACL (new-style)", map[string]interface{}{
        "url": postURL,
        "acl": newACL,
    })

    body, err := doNewStyleACLRequest(ctx, r.httpClient, http.MethodPost, postURL, newACL)
    if err != nil {
        resp.Diagnostics.AddError("Create ACL error", err.Error())
        return
    }

    // The server returns the created ACL object (but not its index).
    var created TaclACLEntry
    if err := json.Unmarshal(body, &created); err != nil {
        resp.Diagnostics.AddError("Error parsing create response", err.Error())
        return
    }

    // GET /acls => find the index of newly-created ACL
    getAllURL := fmt.Sprintf("%s/acls", r.endpoint)
    allBody, err := doNewStyleACLRequest(ctx, r.httpClient, http.MethodGet, getAllURL, nil)
    if err != nil {
        resp.Diagnostics.AddError("Failed to list ACLs after create", err.Error())
        return
    }

    var allACLs []TaclACLEntry
    if err := json.Unmarshal(allBody, &allACLs); err != nil {
        resp.Diagnostics.AddError("Error parsing ACL list response", err.Error())
        return
    }

    idx := findNewStyleACLIndex(allACLs, created)
    if idx < 0 {
        resp.Diagnostics.AddError("Not found", "Could not find newly created ACL in the list.")
        return
    }

    // Store index in data.ID
    data.ID = types.StringValue(fmt.Sprintf("%d", idx))

    // Save final state
    diags = resp.State.Set(ctx, &data)
    resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 5) Read
// -----------------------------------------------------------------------------

func (r *aclResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
    var data aclResourceModel
    // Read the existing state to see what ID we have
    diags := req.State.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    idxStr := data.ID.ValueString()
    idx, err := strconv.Atoi(idxStr)
    if err != nil {
        resp.Diagnostics.AddWarning("Invalid ID", fmt.Sprintf("Could not parse ACL index '%s'", idxStr))
        resp.State.RemoveResource(ctx)
        return
    }

    // GET /acls/:index
    getURL := fmt.Sprintf("%s/acls/%d", r.endpoint, idx)
    tflog.Debug(ctx, "Reading ACL (new-style)", map[string]interface{}{
        "url":   getURL,
        "index": idx,
    })

    body, err := doNewStyleACLRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
    if err != nil {
        if IsNotFound(err) {
            tflog.Warn(ctx, "ACL not found, removing from state", map[string]interface{}{"index": idx})
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("Read ACL error", err.Error())
        return
    }

    var fetched TaclACLEntry
    if err := json.Unmarshal(body, &fetched); err != nil {
        resp.Diagnostics.AddError("Error parsing read response", err.Error())
        return
    }

    // Populate Terraform state
    data.Action = types.StringValue(fetched.Action)
    data.Src = toTerraformStringSlice(fetched.Src)
    data.Proto = types.StringValue(fetched.Proto)
    data.Dst = toTerraformStringSlice(fetched.Dst)

    diags = resp.State.Set(ctx, &data)
    resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 6) Update
// -----------------------------------------------------------------------------

func (r *aclResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
    // 1) Read new user changes into `plan`
    var plan aclResourceModel
    diags := req.Plan.Get(ctx, &plan)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // 2) Read old resource state into `state`
    var state aclResourceModel
    diags = req.State.Get(ctx, &state)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // 3) Merge: copy the old state's ID into the plan (since ID is computed)
    plan.ID = state.ID

    // 4) Convert ID to int
    idx, err := strconv.Atoi(plan.ID.ValueString())
    if err != nil {
        resp.Diagnostics.AddWarning("Invalid ID", fmt.Sprintf("Could not parse ACL index '%s'", plan.ID.ValueString()))
        resp.State.RemoveResource(ctx)
        return
    }

    // 5) Build the updated ACL
    updatedACL := TaclACLEntry{
        Action: plan.Action.ValueString(),
        Src:    toGoStringSlice(plan.Src),
        Proto:  plan.Proto.ValueString(),
        Dst:    toGoStringSlice(plan.Dst),
    }

    // 6) PUT => /acls with { "index": idx, "entry": updatedACL }
    payload := map[string]interface{}{
        "index": idx,
        "entry": updatedACL,
    }
    putURL := fmt.Sprintf("%s/acls", r.endpoint)
    tflog.Debug(ctx, "Updating ACL (new-style)", map[string]interface{}{
        "url":     putURL,
        "payload": payload,
    })

    body, err := doNewStyleACLRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
    if err != nil {
        if IsNotFound(err) {
            // The entry was missing, so remove from state
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("Update ACL error", err.Error())
        return
    }

    var returned TaclACLEntry
    if err := json.Unmarshal(body, &returned); err != nil {
        resp.Diagnostics.AddError("Error parsing update response", err.Error())
        return
    }

    // 7) Refresh plan’s fields
    plan.Action = types.StringValue(returned.Action)
    plan.Src = toTerraformStringSlice(returned.Src)
    plan.Proto = types.StringValue(returned.Proto)
    plan.Dst = toTerraformStringSlice(returned.Dst)

    // 8) Write final merged state
    diags = resp.State.Set(ctx, &plan)
    resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 7) Delete
// -----------------------------------------------------------------------------

func (r *aclResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
    var data aclResourceModel
    diags := req.State.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    idx, err := strconv.Atoi(data.ID.ValueString())
    if err != nil {
        // If not parseable, remove from state
        resp.State.RemoveResource(ctx)
        return
    }

    delURL := fmt.Sprintf("%s/acls", r.endpoint)
    tflog.Debug(ctx, "Deleting ACL (new-style)", map[string]interface{}{
        "url":   delURL,
        "index": idx,
    })

    payload := map[string]int{"index": idx}
    _, err = doNewStyleACLRequest(ctx, r.httpClient, http.MethodDelete, delURL, payload)
    if err != nil {
        if IsNotFound(err) {
            // Already gone
        } else {
            resp.Diagnostics.AddError("Delete ACL error", err.Error())
            return
        }
    }

    resp.State.RemoveResource(ctx)
}

// -----------------------------------------------------------------------------
// findNewStyleACLIndex => naive match of action, src, proto, dst
// -----------------------------------------------------------------------------

func findNewStyleACLIndex(all []TaclACLEntry, entry TaclACLEntry) int {
    for i, a := range all {
        if a.Action != entry.Action {
            continue
        }
        if !equalStringSlice(a.Src, entry.Src) {
            continue
        }
        if a.Proto != entry.Proto {
            continue
        }
        if !equalStringSlice(a.Dst, entry.Dst) {
            continue
        }
        return i
    }
    return -1
}

// doNewStyleACLRequest => general JSON request, returning body or error
func doNewStyleACLRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
    var body io.Reader
    if payload != nil {
        data, err := json.Marshal(payload)
        if err != nil {
            return nil, fmt.Errorf("failed to marshal payload: %w", err)
        }
        body = bytes.NewBuffer(data)
    }

    req, err := http.NewRequestWithContext(ctx, method, url, body)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    res, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request error: %w", err)
    }
    defer res.Body.Close()

    if res.StatusCode == 404 {
        // not found
        return nil, &NotFoundError{Message: "ACL not found"}
    }
    if res.StatusCode >= 300 {
        msg, _ := io.ReadAll(res.Body)
        return nil, fmt.Errorf("TACL returned %d: %s", res.StatusCode, string(msg))
    }

    respBody, err := io.ReadAll(res.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response: %w", err)
    }
    return respBody, nil
}
