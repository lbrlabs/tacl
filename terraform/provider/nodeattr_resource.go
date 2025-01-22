// nodeattr_resource.go

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// nodeattrResource => manages one NodeAttr by stable ID
var (
	_ resource.Resource              = &nodeattrResource{}
	_ resource.ResourceWithConfigure = &nodeattrResource{}
)

func NewNodeAttrResource() resource.Resource {
	return &nodeattrResource{}
}

// nodeattrResource => main struct
type nodeattrResource struct {
	httpClient *http.Client
	endpoint   string
}

// nodeattrResourceModel => Terraform schema. Now "id" is the TACL UUID, not an index.
type nodeattrResourceModel struct {
	ID      types.String   `tfsdk:"id"`     // TACL's stable ID
	Target  []types.String `tfsdk:"target"` // required
	Attr    []types.String `tfsdk:"attr"`   // optional
	AppJSON types.String   `tfsdk:"app_json"`
}

// NodeAttrGrantInput => request body for create/update ("grant")
type NodeAttrGrantInput struct {
	Target []string               `json:"target"`
	Attr   []string               `json:"attr,omitempty"`
	App    map[string]interface{} `json:"app,omitempty"`
}

// NodeAttrResponse => TACL's returned object: stable ID + fields
type NodeAttrResponse struct {
	ID     string                 `json:"id"`
	Target []string               `json:"target"`
	Attr   []string               `json:"attr,omitempty"`
	App    map[string]interface{} `json:"app,omitempty"`
}

// -----------------------------------------------------------------------------
// Configure / Metadata / Schema
// -----------------------------------------------------------------------------

func (r *nodeattrResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *nodeattrResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_nodeattr"
}

func (r *nodeattrResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one nodeattr entry by stable ID in TACL’s /nodeattrs.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "TACL's stable ID for this nodeattr.",
				Computed:    true,
			},
			"target": schema.ListAttribute{
				Description: "Required list of target strings (or forced to [\"*\"] if `app_json` is used).",
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

// -----------------------------------------------------------------------------
// 1) Create => POST /nodeattrs => returns {"id":"<uuid>", "target":[],"attr":[],"app":{}}
// -----------------------------------------------------------------------------

func (r *nodeattrResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan nodeattrResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasAttr := len(plan.Attr) > 0
	hasApp := plan.AppJSON.ValueString() != "" && !plan.AppJSON.IsNull() && !plan.AppJSON.IsUnknown()

	// exactly one
	if (hasAttr && hasApp) || (!hasAttr && !hasApp) {
		resp.Diagnostics.AddError("Invalid config",
			"Exactly one of `attr` or `app_json` must be set.")
		return
	}

	// Build input
	input := NodeAttrGrantInput{
		Target: toStringSlice(plan.Target),
	}
	if hasAttr {
		input.Attr = toStringSlice(plan.Attr)
	} else {
		// parse app JSON
		var app map[string]interface{}
		if err := json.Unmarshal([]byte(plan.AppJSON.ValueString()), &app); err != nil {
			resp.Diagnostics.AddError("Invalid app_json", err.Error())
			return
		}
		input.App = app
	}

	// POST /nodeattrs
	url := fmt.Sprintf("%s/nodeattrs", r.endpoint)
	tflog.Debug(ctx, "Creating nodeattr by ID", map[string]interface{}{
		"url": url, "payload": input,
	})

	body, err := doNodeAttrIDRequest(ctx, r.httpClient, http.MethodPost, url, input)
	if err != nil {
		resp.Diagnostics.AddError("Create nodeattr error", err.Error())
		return
	}

	var created NodeAttrResponse
	if e := json.Unmarshal(body, &created); e != nil {
		resp.Diagnostics.AddError("Parse create response error", e.Error())
		return
	}

	// Fill plan from server response
	plan.ID = types.StringValue(created.ID)
	plan.Target = toTerraformStringSlice(created.Target)
	if len(created.Attr) > 0 {
		plan.Attr = toTerraformStringSlice(created.Attr)
		plan.AppJSON = types.StringValue("")
	} else if created.App != nil {
		b, _ := json.Marshal(created.App)
		plan.AppJSON = types.StringValue(string(b))
		plan.Attr = []types.String{}
	}

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 2) Read => GET /nodeattrs/:id
// -----------------------------------------------------------------------------

func (r *nodeattrResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state nodeattrResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.ID.IsNull() || state.ID.ValueString() == "" {
		// no ID => remove
		resp.State.RemoveResource(ctx)
		return
	}
	id := state.ID.ValueString()

	url := fmt.Sprintf("%s/nodeattrs/%s", r.endpoint, id)
	tflog.Debug(ctx, "Reading nodeattr by ID", map[string]interface{}{
		"url": url, "id": id,
	})

	body, err := doNodeAttrIDRequest(ctx, r.httpClient, http.MethodGet, url, nil)
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read nodeattr error", err.Error())
		return
	}

	var fetched NodeAttrResponse
	if e := json.Unmarshal(body, &fetched); e != nil {
		resp.Diagnostics.AddError("Parse read response error", e.Error())
		return
	}

	state.ID = types.StringValue(fetched.ID)
	state.Target = toTerraformStringSlice(fetched.Target)
	if len(fetched.Attr) > 0 {
		state.Attr = toTerraformStringSlice(fetched.Attr)
		state.AppJSON = types.StringValue("")
	} else if fetched.App != nil {
		b, _ := json.Marshal(fetched.App)
		state.AppJSON = types.StringValue(string(b))
		state.Attr = []types.String{}
	} else {
		state.Attr = []types.String{}
		state.AppJSON = types.StringValue("")
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 3) Update => PUT /nodeattrs => { "id":"<uuid>","grant":{...} }
// -----------------------------------------------------------------------------

func (r *nodeattrResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// old => preserve ID
	var oldState nodeattrResourceModel
	diags := req.State.Get(ctx, &oldState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// plan => new config
	var plan nodeattrResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = oldState.ID
	id := plan.ID.ValueString()
	if id == "" {
		// no ID => remove
		resp.State.RemoveResource(ctx)
		return
	}

	hasAttr := len(plan.Attr) > 0
	hasApp := plan.AppJSON.ValueString() != "" && !plan.AppJSON.IsNull()
	if (hasAttr && hasApp) || (!hasAttr && !hasApp) {
		resp.Diagnostics.AddError("Invalid config",
			"Exactly one of `attr` or `app_json` must be set.")
		return
	}

	// build input
	input := NodeAttrGrantInput{
		Target: toStringSlice(plan.Target),
	}
	if hasAttr {
		input.Attr = toStringSlice(plan.Attr)
	} else {
		var app map[string]interface{}
		if err := json.Unmarshal([]byte(plan.AppJSON.ValueString()), &app); err != nil {
			resp.Diagnostics.AddError("Invalid app_json", err.Error())
			return
		}
		input.App = app
	}

	payload := map[string]interface{}{
		"id":    id,
		"grant": input,
	}

	url := fmt.Sprintf("%s/nodeattrs", r.endpoint)
	tflog.Debug(ctx, "Updating nodeattr by ID", map[string]interface{}{
		"url": url, "payload": payload,
	})

	body, err := doNodeAttrIDRequest(ctx, r.httpClient, http.MethodPut, url, payload)
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update nodeattr error", err.Error())
		return
	}

	var updated NodeAttrResponse
	if e := json.Unmarshal(body, &updated); e != nil {
		resp.Diagnostics.AddError("Parse update response error", e.Error())
		return
	}

	plan.ID = types.StringValue(updated.ID)
	plan.Target = toTerraformStringSlice(updated.Target)
	if len(updated.Attr) > 0 {
		plan.Attr = toTerraformStringSlice(updated.Attr)
		plan.AppJSON = types.StringValue("")
	} else if updated.App != nil {
		b, _ := json.Marshal(updated.App)
		plan.AppJSON = types.StringValue(string(b))
		plan.Attr = []types.String{}
	} else {
		plan.Attr = []types.String{}
		plan.AppJSON = types.StringValue("")
	}

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

// -----------------------------------------------------------------------------
// 4) Delete => DELETE => { "id":"<uuid>" }
// -----------------------------------------------------------------------------

func (r *nodeattrResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data nodeattrResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		// already removed
		resp.State.RemoveResource(ctx)
		return
	}

	payload := map[string]string{"id": id}
	url := fmt.Sprintf("%s/nodeattrs", r.endpoint)
	tflog.Debug(ctx, "Deleting nodeattr by ID", map[string]interface{}{
		"url": url, "payload": payload,
	})

	_, err := doNodeAttrIDRequest(ctx, r.httpClient, http.MethodDelete, url, payload)
	if err != nil {
		if isNotFound(err) {
			// already gone
		} else {
			resp.Diagnostics.AddError("Delete nodeattr error", err.Error())
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// doNodeAttrIDRequest => JSON-based request returning body or error
func doNodeAttrIDRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("request creation error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nodeattr ID request error: %w", err)
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
