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

// TaclSSHEntry => Represents the user-facing fields for an SSH rule.
type TaclSSHEntry struct {
	Action      string   `json:"action"`      // "accept" or "check"
	Src         []string `json:"src"`         // sources
	Dst         []string `json:"dst"`         // destinations
	Users       []string `json:"users"`       // SSH users
	CheckPeriod string   `json:"checkPeriod"` // optional duration, e.g. "12h"
	AcceptEnv   []string `json:"acceptEnv"`   // optional environment vars
}

// TaclSSHResponse => The server’s extended SSH shape: stable ID + fields above
type TaclSSHResponse struct {
	ID           string `json:"id"` // stable UUID from TACL
	TaclSSHEntry `json:",inline"`
}

// Ensure interface compliance with Terraform plugin framework.
var (
	_ resource.Resource              = &sshResource{}
	_ resource.ResourceWithConfigure = &sshResource{}
)

// NewSSHResource => constructor for "tacl_ssh" resource
func NewSSHResource() resource.Resource {
	return &sshResource{}
}

// sshResource => main struct implementing Resource
type sshResource struct {
	httpClient *http.Client
	endpoint   string
}

// sshResourceModel => Terraform schema for storing the user’s config + the ID
type sshResourceModel struct {
	ID          types.String   `tfsdk:"id"`
	Action      types.String   `tfsdk:"action"`
	Src         []types.String `tfsdk:"src"`
	Dst         []types.String `tfsdk:"dst"`
	Users       []types.String `tfsdk:"users"`
	CheckPeriod types.String   `tfsdk:"check_period"`
	AcceptEnv   []types.String `tfsdk:"accept_env"`
}

func (r *sshResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *sshResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh"
}

func (r *sshResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a single SSH rule by stable ID in TACL’s /ssh.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "TACL's stable UUID for this SSH rule.",
				Computed:    true,
			},
			"action": schema.StringAttribute{
				Description: "SSH action, e.g. 'accept' or 'check'.",
				Required:    true,
			},
			"src": schema.ListAttribute{
				Description: "List of source specifications (e.g. tags, CIDRs).",
				Required:    true,
				ElementType: types.StringType,
			},
			"dst": schema.ListAttribute{
				Description: "List of destination specifications (e.g. host:port).",
				Required:    true,
				ElementType: types.StringType,
			},
			"users": schema.ListAttribute{
				Description: "List of SSH users allowed.",
				Required:    true,
				ElementType: types.StringType,
			},
			"check_period": schema.StringAttribute{
				Description: "Optional duration for 'check' actions, e.g. '12h'.",
				Optional:    true,
			},
			"accept_env": schema.ListAttribute{
				Description: "Optional list of environment variable patterns to allow.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// ----------------------------------------------------------------------------
// 4) Create
// ----------------------------------------------------------------------------

func (r *sshResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// 1. Read plan data
	var plan sshResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. Convert plan to JSON for TACL => TaclSSHEntry
	payload := TaclSSHEntry{
		Action:      plan.Action.ValueString(),
		Src:         toStringSlice(plan.Src),
		Dst:         toStringSlice(plan.Dst),
		Users:       toStringSlice(plan.Users),
		CheckPeriod: plan.CheckPeriod.ValueString(),
		AcceptEnv:   toStringSlice(plan.AcceptEnv),
	}

	// 3. POST /ssh => create a new item with a server-generated ID
	postURL := fmt.Sprintf("%s/ssh", r.endpoint)
	tflog.Debug(ctx, "Creating SSH rule", map[string]interface{}{
		"url":     postURL,
		"payload": payload,
	})

	body, err := doSSHIDRequest(ctx, r.httpClient, http.MethodPost, postURL, payload)
	if err != nil {
		resp.Diagnostics.AddError("Create SSH error", err.Error())
		return
	}

	// 4. Parse response => TaclSSHResponse
	var created TaclSSHResponse
	if e := json.Unmarshal(body, &created); e != nil {
		resp.Diagnostics.AddError("Parse create response error", e.Error())
		return
	}

	// 5. Save ID + other fields to state
	plan.ID = types.StringValue(created.ID)
	plan.Action = types.StringValue(created.Action)
	plan.Src = toTerraformStringSlice(created.Src)
	plan.Dst = toTerraformStringSlice(created.Dst)
	plan.Users = toTerraformStringSlice(created.Users)
	plan.CheckPeriod = types.StringValue(created.CheckPeriod)
	plan.AcceptEnv = toTerraformStringSlice(created.AcceptEnv)

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// 5) Read
// ----------------------------------------------------------------------------

func (r *sshResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// 1. Get current state (need the ID)
	var state sshResourceModel
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

	// 3. GET /ssh/:id
	getURL := fmt.Sprintf("%s/ssh/%s", r.endpoint, id)
	tflog.Debug(ctx, "Reading SSH rule by ID", map[string]interface{}{
		"url": getURL,
		"id":  id,
	})

	body, err := doSSHIDRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if isNotFound(err) {
			// TACL says it's gone => remove from TF
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read SSH error", err.Error())
		return
	}

	var fetched TaclSSHResponse
	if e := json.Unmarshal(body, &fetched); e != nil {
		resp.Diagnostics.AddError("Parse read response error", e.Error())
		return
	}

	// 4. Update state with fetched data
	state.ID = types.StringValue(fetched.ID)
	state.Action = types.StringValue(fetched.Action)
	state.Src = toTerraformStringSlice(fetched.Src)
	state.Dst = toTerraformStringSlice(fetched.Dst)
	state.Users = toTerraformStringSlice(fetched.Users)
	state.CheckPeriod = types.StringValue(fetched.CheckPeriod)
	state.AcceptEnv = toTerraformStringSlice(fetched.AcceptEnv)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// 6) Update
// ----------------------------------------------------------------------------

func (r *sshResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// 1. Old state => preserve ID
	var oldState sshResourceModel
	diags := req.State.Get(ctx, &oldState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. New plan => user config
	var plan sshResourceModel
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

	// 4. Convert plan to TaclSSHEntry
	input := TaclSSHEntry{
		Action:      plan.Action.ValueString(),
		Src:         toStringSlice(plan.Src),
		Dst:         toStringSlice(plan.Dst),
		Users:       toStringSlice(plan.Users),
		CheckPeriod: plan.CheckPeriod.ValueString(),
		AcceptEnv:   toStringSlice(plan.AcceptEnv),
	}

	// 5. PUT /ssh => typically something like { "id": "<uuid>", "action": "...", ... }
	//    or { "id":"<uuid>", "entry": { ... } }
	putURL := fmt.Sprintf("%s/ssh", r.endpoint)
	payload := map[string]interface{}{
		"id":          id,
		"action":      input.Action,
		"src":         input.Src,
		"dst":         input.Dst,
		"users":       input.Users,
		"checkPeriod": input.CheckPeriod,
		"acceptEnv":   input.AcceptEnv,
	}

	tflog.Debug(ctx, "Updating SSH rule by ID", map[string]interface{}{
		"url":     putURL,
		"payload": payload,
	})

	body, err := doSSHIDRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
	if err != nil {
		if isNotFound(err) {
			// TACL says it's gone => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update SSH error", err.Error())
		return
	}

	var updated TaclSSHResponse
	if e := json.Unmarshal(body, &updated); e != nil {
		resp.Diagnostics.AddError("Parse update response error", e.Error())
		return
	}

	// 6. Merge updated data back
	plan.ID = types.StringValue(updated.ID)
	plan.Action = types.StringValue(updated.Action)
	plan.Src = toTerraformStringSlice(updated.Src)
	plan.Dst = toTerraformStringSlice(updated.Dst)
	plan.Users = toTerraformStringSlice(updated.Users)
	plan.CheckPeriod = types.StringValue(updated.CheckPeriod)
	plan.AcceptEnv = toTerraformStringSlice(updated.AcceptEnv)

	// 7. Save final
	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

// ----------------------------------------------------------------------------
// 7) Delete
// ----------------------------------------------------------------------------

func (r *sshResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data sshResourceModel
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

	// DELETE => /ssh => { "id":"<uuid>" }
	delURL := fmt.Sprintf("%s/ssh", r.endpoint)
	payload := map[string]string{"id": id}

	tflog.Debug(ctx, "Deleting SSH rule by ID", map[string]interface{}{
		"url":     delURL,
		"payload": payload,
	})

	_, err := doSSHIDRequest(ctx, r.httpClient, http.MethodDelete, delURL, payload)
	if err != nil {
		if isNotFound(err) {
			// already gone
		} else {
			resp.Diagnostics.AddError("Delete SSH error", err.Error())
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

// ----------------------------------------------------------------------------
// Helper HTTP logic
// ----------------------------------------------------------------------------

// doSSHIDRequest => helper for create/read/update/delete calls.
func doSSHIDRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("SSH ID request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "SSH rule not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, string(msg))
	}

	return io.ReadAll(resp.Body)
}
