// acl_resource.go
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

// TaclACLEntry => Represents the ACL portion (action, src, proto, dst).
// On the server side, there's also an "id" string field in ExtendedACLEntry.
type TaclACLEntry struct {
	Action string   `json:"action"`          // e.g. "accept" or "deny"
	Src    []string `json:"src"`             // e.g. ["tag:dev"]
	Proto  string   `json:"proto,omitempty"` // optional
	Dst    []string `json:"dst"`             // e.g. ["tag:prod:*","10.1.2.3/32:22"]
}

// TaclACLResponse => The server's ExtendedACLEntry shape: stable ID + the fields above
type TaclACLResponse struct {
	ID           string `json:"id"` // stable UUID from TACL
	TaclACLEntry        // embed the rest
}

// Ensure interface compliance with Terraform plugin framework.
var (
	_ resource.Resource              = &aclResource{}
	_ resource.ResourceWithConfigure = &aclResource{}
)

// NewACLResource => constructor for "tacl_acl" resource
func NewACLResource() resource.Resource {
	return &aclResource{}
}

// aclResource => main struct implementing Resource
type aclResource struct {
	httpClient *http.Client
	endpoint   string
}

// aclResourceModel => Terraform schema for storing the user's config + the ID
type aclResourceModel struct {
	ID     types.String   `tfsdk:"id"`     // TACL's stable UUID
	Action types.String   `tfsdk:"action"` // "accept"/"deny"
	Src    []types.String `tfsdk:"src"`
	Proto  types.String   `tfsdk:"proto"`
	Dst    []types.String `tfsdk:"dst"`
}

//------------------------------------------------------------------------------
// 1) Configure / 2) Metadata / 3) Schema
//------------------------------------------------------------------------------

func (r *aclResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *aclResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_acl"
}

func (r *aclResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a single ACL entry by stable ID in TACL’s /acls.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "TACL's stable UUID for this ACL entry.",
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
				Description: "Optional protocol, e.g. 'tcp'.",
				Optional:    true,
			},
			"dst": schema.ListAttribute{
				Description: "List of destination CIDRs/tags. Possibly with :port.",
				Required:    true,
				ElementType: types.StringType,
			},
		},
	}
}

//------------------------------------------------------------------------------
// 4) Create
//------------------------------------------------------------------------------

func (r *aclResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// 1. Read plan data
	var plan aclResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. Convert to JSON for TACL => TaclACLEntry
	payload := TaclACLEntry{
		Action: plan.Action.ValueString(),
		Src:    toStringSlice(plan.Src),
		Proto:  plan.Proto.ValueString(),
		Dst:    toStringSlice(plan.Dst),
	}

	// 3. POST /acls => create a new item with a server-generated ID
	postURL := fmt.Sprintf("%s/acls", r.endpoint)
	tflog.Debug(ctx, "Creating ACL by ID", map[string]interface{}{
		"url":     postURL,
		"payload": payload,
	})

	body, err := doACLIDRequest(ctx, r.httpClient, http.MethodPost, postURL, payload)
	if err != nil {
		resp.Diagnostics.AddError("Create ACL error", err.Error())
		return
	}

	// 4. Parse response => TaclACLResponse
	var created TaclACLResponse
	if e := json.Unmarshal(body, &created); e != nil {
		resp.Diagnostics.AddError("Parse create response error", e.Error())
		return
	}

	// 5. Save ID + other fields to state
	plan.ID = types.StringValue(created.ID)
	plan.Action = types.StringValue(created.Action)
	plan.Src = toTerraformStringSlice(created.Src)
	plan.Proto = types.StringValue(created.Proto)
	plan.Dst = toTerraformStringSlice(created.Dst)

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

//------------------------------------------------------------------------------
// 5) Read
//------------------------------------------------------------------------------

func (r *aclResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// 1. Pull current state (need the ID)
	var state aclResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. If ID is empty => remove
	if state.ID.IsNull() || state.ID.ValueString() == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	id := state.ID.ValueString()

	// 3. GET /acls/:id
	getURL := fmt.Sprintf("%s/acls/%s", r.endpoint, id)
	tflog.Debug(ctx, "Reading ACL by ID", map[string]interface{}{
		"url": getURL,
		"id":  id,
	})

	body, err := doACLIDRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if isNotFound(err) {
			// TACL says it's gone => remove from TF
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read ACL error", err.Error())
		return
	}

	var fetched TaclACLResponse
	if e := json.Unmarshal(body, &fetched); e != nil {
		resp.Diagnostics.AddError("Parse read response error", e.Error())
		return
	}

	// 4. Update state with fetched data
	state.ID = types.StringValue(fetched.ID)
	state.Action = types.StringValue(fetched.Action)
	state.Src = toTerraformStringSlice(fetched.Src)
	state.Proto = types.StringValue(fetched.Proto)
	state.Dst = toTerraformStringSlice(fetched.Dst)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

//------------------------------------------------------------------------------
// 6) Update
//------------------------------------------------------------------------------

func (r *aclResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// 1. Old state => preserve ID
	var oldState aclResourceModel
	diags := req.State.Get(ctx, &oldState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. New plan => new user config
	var plan aclResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 3. Merge ID
	plan.ID = oldState.ID
	id := plan.ID.ValueString()
	if id == "" {
		// no ID => cannot update
		resp.State.RemoveResource(ctx)
		return
	}

	// 4. Convert plan to TaclACLEntry
	input := TaclACLEntry{
		Action: plan.Action.ValueString(),
		Src:    toStringSlice(plan.Src),
		Proto:  plan.Proto.ValueString(),
		Dst:    toStringSlice(plan.Dst),
	}

	// 5. PUT /acls => { "id":"<uuid>", "entry": { ... } }
	payload := map[string]interface{}{
		"id":    id,
		"entry": input,
	}
	putURL := fmt.Sprintf("%s/acls", r.endpoint)
	tflog.Debug(ctx, "Updating ACL by ID", map[string]interface{}{
		"url":     putURL,
		"payload": payload,
	})

	body, err := doACLIDRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
	if err != nil {
		if isNotFound(err) {
			// TACL says it's gone => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update ACL error", err.Error())
		return
	}

	var updated TaclACLResponse
	if e := json.Unmarshal(body, &updated); e != nil {
		resp.Diagnostics.AddError("Parse update response error", e.Error())
		return
	}

	// 6. Merge updated data back
	plan.ID = types.StringValue(updated.ID)
	plan.Action = types.StringValue(updated.Action)
	plan.Src = toTerraformStringSlice(updated.Src)
	plan.Proto = types.StringValue(updated.Proto)
	plan.Dst = toTerraformStringSlice(updated.Dst)

	// 7. Save final
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

//------------------------------------------------------------------------------
// 7) Delete
//------------------------------------------------------------------------------

func (r *aclResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data aclResourceModel
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

	// DELETE => /acls => { "id":"<uuid>" }
	delURL := fmt.Sprintf("%s/acls", r.endpoint)
	payload := map[string]string{"id": id}

	tflog.Debug(ctx, "Deleting ACL by ID", map[string]interface{}{
		"url":     delURL,
		"payload": payload,
	})

	_, err := doACLIDRequest(ctx, r.httpClient, http.MethodDelete, delURL, payload)
	if err != nil {
		if isNotFound(err) {
			// already gone
		} else {
			resp.Diagnostics.AddError("Delete ACL error", err.Error())
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

//------------------------------------------------------------------------------
// Helper HTTP logic
//------------------------------------------------------------------------------

func doACLIDRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("ACL ID request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "ACL not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, string(msg))
	}

	return io.ReadAll(resp.Body)
}
