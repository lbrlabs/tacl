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

// Ensure interface compliance
var (
    _ resource.Resource              = &nodeattrResource{}
    _ resource.ResourceWithConfigure = &nodeattrResource{}
)

// NewNodeAttrResource => single "nodeattr" entry in TACL's nodeAttrs array
func NewNodeAttrResource() resource.Resource {
    return &nodeattrResource{}
}

type nodeattrResource struct {
    httpClient *http.Client
    endpoint   string
}

// nodeattrResourceModel => user sets `target` plus EXACTLY one of `attr` or `app_json`.
type nodeattrResourceModel struct {
    ID      types.String   `tfsdk:"id"`       // array index in string form
    Target  []types.String `tfsdk:"target"`   // required
    Attr    []types.String `tfsdk:"attr"`     // optional
    AppJSON types.String   `tfsdk:"app_json"` // optional
}

// We'll pass NodeAttrInput to TACL in create/update.
type NodeAttrInput struct {
    Target []string               `json:"target"`
    Attr   []string               `json:"attr,omitempty"`
    App    map[string]interface{} `json:"app,omitempty"`
}

// ----------------------------------------------------------------------------
// Configure / Metadata / Schema
// ----------------------------------------------------------------------------

func (r *nodeattrResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    p, ok := req.ProviderData.(*taclProvider)
    if !ok {
        return
    }
    r.httpClient = p.httpClient
    r.endpoint = p.endpoint
}

func (r *nodeattrResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_nodeattr"
}

func (r *nodeattrResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Description: "Manages one entry in TACL’s nodeAttrs array by index. Exactly one of `attr` or `app_json` must be set.",
        Attributes: map[string]schema.Attribute{
            "id": schema.StringAttribute{
                Description: "Index in TACL’s nodeAttrs array (string form).",
                Computed:    true,
            },
            "target": schema.ListAttribute{
                Description: "Required list of target strings.",
                Required:    true,
                ElementType: types.StringType,
            },
            "attr": schema.ListAttribute{
                Description: "Optional list of attribute strings if not using `app_json`.",
                Optional:    true,
                ElementType: types.StringType,
            },
            "app_json": schema.StringAttribute{
                Description: "Optional JSON for `app`. Must be empty if `attr` is used.",
                Optional:    true,
            },
        },
    }
}

// ----------------------------------------------------------------------------
// Create => POST => find new index
// ----------------------------------------------------------------------------

func (r *nodeattrResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
    var data nodeattrResourceModel
    diags := req.Plan.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // Check mutual exclusivity
    hasAttr := len(data.Attr) > 0
    hasApp := (!data.AppJSON.IsNull() && !data.AppJSON.IsUnknown() && data.AppJSON.ValueString() != "")
    if (hasAttr && hasApp) || (!hasAttr && !hasApp) {
        resp.Diagnostics.AddError("Invalid config", "Exactly one of `attr` or `app_json` must be set.")
        return
    }

    // Build NodeAttrInput
    input := NodeAttrInput{
        Target: toGoStringSlice(data.Target),
    }
    if hasAttr {
        input.Attr = toGoStringSlice(data.Attr)
    } else {
        var app map[string]interface{}
        if e := json.Unmarshal([]byte(data.AppJSON.ValueString()), &app); e != nil {
            resp.Diagnostics.AddError("Invalid app_json", e.Error())
            return
        }
        input.App = app
    }

    postURL := fmt.Sprintf("%s/nodeattrs", r.endpoint)
    tflog.Debug(ctx, "Creating nodeattr", map[string]interface{}{
        "url": postURL, "payload": input,
    })

    body, err := doNodeAttrReq(ctx, r.httpClient, http.MethodPost, postURL, input)
    if err != nil {
        resp.Diagnostics.AddError("Create nodeattr error", err.Error())
        return
    }

    // parse TACL's response => newly created object
    var created map[string]interface{}
    if err := json.Unmarshal(body, &created); err != nil {
        resp.Diagnostics.AddError("Parse create response error", err.Error())
        return
    }

    // GET /nodeattrs => find index
    listURL := fmt.Sprintf("%s/nodeattrs", r.endpoint)
    allBody, err := doNodeAttrReq(ctx, r.httpClient, http.MethodGet, listURL, nil)
    if err != nil {
        resp.Diagnostics.AddError("List nodeattrs error", err.Error())
        return
    }
    var all []map[string]interface{}
    if err := json.Unmarshal(allBody, &all); err != nil {
        resp.Diagnostics.AddError("Parse nodeattrs array error", err.Error())
        return
    }

    idx := findNodeAttrIndex(all, created)
    if idx < 0 {
        resp.Diagnostics.AddError("Not found", "Could not find newly created nodeattr in array.")
        return
    }
    data.ID = types.StringValue(strconv.Itoa(idx))

    diags = resp.State.Set(ctx, &data)
    resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// Read => GET /nodeattrs/:index
// ----------------------------------------------------------------------------

func (r *nodeattrResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
    var data nodeattrResourceModel
    diags := req.State.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    idxStr := data.ID.ValueString()
    idx, err := strconv.Atoi(idxStr)
    if err != nil {
        // invalid => remove
        resp.Diagnostics.AddWarning("Invalid ID", "Could not parse nodeattr index from state.")
        resp.State.RemoveResource(ctx)
        return
    }

    getURL := fmt.Sprintf("%s/nodeattrs/%d", r.endpoint, idx)
    tflog.Debug(ctx, "Reading nodeattr", map[string]interface{}{
        "url":   getURL,
        "index": idx,
    })

    body, e := doNodeAttrReq(ctx, r.httpClient, http.MethodGet, getURL, nil)
    if e != nil {
        if IsNotFound(e) {
            // TACL says index is gone => remove from state
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("Read nodeattr error", e.Error())
        return
    }

    var fetched map[string]interface{}
    if err := json.Unmarshal(body, &fetched); err != nil {
        resp.Diagnostics.AddError("Parse read response error", err.Error())
        return
    }

    fillResourceModel(&data, fetched)
    diags = resp.State.Set(ctx, &data)
    resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// Update => PUT => { index, grant }
// ----------------------------------------------------------------------------

func (r *nodeattrResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
    // Step 1. Read the old state into oldData, to preserve the old ID
    var oldData nodeattrResourceModel
    diags := req.State.Get(ctx, &oldData)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // Step 2. Read the new plan data (the user’s changes) into planData
    var planData nodeattrResourceModel
    diags = req.Plan.Get(ctx, &planData)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    // Check exclusivity again
    hasAttr := len(planData.Attr) > 0
    hasApp := (!planData.AppJSON.IsNull() && !planData.AppJSON.IsUnknown() && planData.AppJSON.ValueString() != "")
    if (hasAttr && hasApp) || (!hasAttr && !hasApp) {
        resp.Diagnostics.AddError(
            "Invalid config",
            "Must set exactly one of `attr` or `app_json` (not both, not neither).",
        )
        return
    }

    // Step 3. Parse the old ID
    idxStr := oldData.ID.ValueString()
    idx, err := strconv.Atoi(idxStr)
    if err != nil {
        // The old state had an invalid ID => remove resource from state
        resp.Diagnostics.AddWarning("Invalid ID", fmt.Sprintf("Could not parse nodeattr index '%s' from old state", idxStr))
        resp.State.RemoveResource(ctx)
        return
    }

    // Step 4. Build NodeAttrInput from the plan changes
    input := NodeAttrInput{
        Target: toGoStringSlice(planData.Target),
    }
    if hasAttr {
        input.Attr = toGoStringSlice(planData.Attr)
    } else {
        var app map[string]interface{}
        if e := json.Unmarshal([]byte(planData.AppJSON.ValueString()), &app); e != nil {
            resp.Diagnostics.AddError("Invalid app_json", e.Error())
            return
        }
        input.App = app
    }

    payload := map[string]interface{}{
        "index": idx,
        "grant": input,
    }

    // Step 5. Call TACL => PUT /nodeattrs
    putURL := fmt.Sprintf("%s/nodeattrs", r.endpoint)
    tflog.Debug(ctx, "Updating nodeattr", map[string]interface{}{
        "url":     putURL,
        "payload": payload,
    })

    body, e := doNodeAttrReq(ctx, r.httpClient, http.MethodPut, putURL, payload)
    if e != nil {
        if IsNotFound(e) {
            // TACL says it's gone => remove from state
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("Update nodeattr error", e.Error())
        return
    }

    var updated map[string]interface{}
    if err := json.Unmarshal(body, &updated); err != nil {
        resp.Diagnostics.AddError("Parse update response error", err.Error())
        return
    }

    // Step 6. Merge TACL’s updated data back into planData for final state
    fillResourceModel(&planData, updated)
    // Keep the old ID (index)
    planData.ID = oldData.ID

    // Step 7. Save final state
    diags = resp.State.Set(ctx, &planData)
    resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// Delete => { index }
// ----------------------------------------------------------------------------

func (r *nodeattrResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
    var data nodeattrResourceModel
    diags := req.State.Get(ctx, &data)
    resp.Diagnostics.Append(diags...)
    if resp.Diagnostics.HasError() {
        return
    }

    idxStr := data.ID.ValueString()
    idx, err := strconv.Atoi(idxStr)
    if err != nil {
        // Invalid => remove
        resp.State.RemoveResource(ctx)
        return
    }

    payload := map[string]int{"index": idx}
    delURL := fmt.Sprintf("%s/nodeattrs", r.endpoint)
    _, e := doNodeAttrReq(ctx, r.httpClient, http.MethodDelete, delURL, payload)
    if e != nil {
        if IsNotFound(e) {
            // Already gone
        } else {
            resp.Diagnostics.AddError("Delete nodeattr error", e.Error())
            return
        }
    }
    resp.State.RemoveResource(ctx)
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func doNodeAttrReq(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
    var body io.Reader
    if payload != nil {
        b, err := json.Marshal(payload)
        if err != nil {
            return nil, err
        }
        body = bytes.NewBuffer(b)
    }

    req, err := http.NewRequestWithContext(ctx, method, url, body)
    if err != nil {
        return nil, fmt.Errorf("nodeattr request creation error: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("nodeattr request error: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == 404 {
        return nil, &NotFoundError{Message: "nodeattr not found"}
    }
    if resp.StatusCode >= 300 {
        msg, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, string(msg))
    }

    return io.ReadAll(resp.Body)
}

// findNodeAttrIndex compares the newly created object to each item in the array to find matching JSON
func findNodeAttrIndex(all []map[string]interface{}, created map[string]interface{}) int {
    cBytes, _ := json.Marshal(created)
    for i, item := range all {
        iBytes, _ := json.Marshal(item)
        if string(iBytes) == string(cBytes) {
            return i
        }
    }
    return -1
}

// fillResourceModel => parse TACL's JSON => fill resource fields, using empty slices/strings
func fillResourceModel(data *nodeattrResourceModel, js map[string]interface{}) {
    // target
    if t, ok := js["target"].([]interface{}); ok {
        data.Target = toStringTypeSlice(t)
    } else {
        data.Target = []types.String{}
    }

    // attr
    if rawAttr, hasAttr := js["attr"]; hasAttr {
        // If "attr" is present but is an empty array, TACL might send []
        if arr, isArr := rawAttr.([]interface{}); isArr && len(arr) > 0 {
            data.Attr = toStringTypeSlice(arr)
        } else {
            // treat empty array as empty list
            data.Attr = []types.String{}
        }
    } else {
        // No "attr" key => store an empty slice
        data.Attr = []types.String{}
    }

    // app
    if rawApp, hasApp := js["app"]; hasApp {
        appBytes, _ := json.Marshal(rawApp)
        data.AppJSON = types.StringValue(string(appBytes))
    } else {
        // If server omits "app", we store an empty string
        data.AppJSON = types.StringValue("")
    }
}
