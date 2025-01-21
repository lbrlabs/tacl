package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// Ensure interface compliance
var (
	_ resource.Resource              = &derpMapResource{}
	_ resource.ResourceWithConfigure = &derpMapResource{}
)

// NewDERPMapResource => returns a resource for the single DERPMap at /derpmap.
func NewDERPMapResource() resource.Resource {
	return &derpMapResource{}
}

// derpMapResource is a single-object resource. We'll store ID = "derpmap" once created.
type derpMapResource struct {
	httpClient *http.Client
	endpoint   string
}

// derpMapResourceModel => we store the entire DERPMap as a string in `derpmap_json`.
type derpMapResourceModel struct {
	ID          types.String `tfsdk:"id"`           // "derpmap"
	DerpMapJson types.String `tfsdk:"derpmap_json"` // raw JSON
}

// Configure => retrieve provider httpClient, endpoint
func (r *derpMapResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Metadata => "tacl_derpmap"
func (r *derpMapResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_derpmap"
}

// Schema => for simplicity, just store raw JSON
func (r *derpMapResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the single ACLDERPMap object at /derpmap.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'derpmap' once created.",
				Computed:    true,
			},
			"derpmap_json": schema.StringAttribute{
				Description: "Full DERPMap JSON. If you prefer typed fields, expand them here.",
				Required:    true,
			},
		},
	}
}

// Create => POST /derpmap
func (r *derpMapResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data derpMapResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert user-provided derpmap_json => tsclient.ACLDERPMap
	var newDM tsclient.ACLDERPMap
	if dmStr := data.DerpMapJson.ValueString(); dmStr != "" {
		if err := json.Unmarshal([]byte(dmStr), &newDM); err != nil {
			resp.Diagnostics.AddError("Parse derpmap_json error", err.Error())
			return
		}
	}

	postURL := fmt.Sprintf("%s/derpmap", r.endpoint)
	tflog.Debug(ctx, "Creating DERPMap", map[string]interface{}{"url": postURL})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodPost, postURL, newDM)
	if err != nil {
		resp.Diagnostics.AddError("Create DERPMap error", err.Error())
		return
	}

	var created tsclient.ACLDERPMap
	if err := json.Unmarshal(body, &created); err != nil {
		resp.Diagnostics.AddError("Parse create response error", err.Error())
		return
	}

	// ID => "derpmap"
	data.ID = types.StringValue("derpmap")

	// Convert the newly created object back to JSON for storing in Terraform state
	raw, _ := json.MarshalIndent(created, "", "  ")
	data.DerpMapJson = types.StringValue(string(raw))

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Read => GET /derpmap
func (r *derpMapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data derpMapResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getURL := fmt.Sprintf("%s/derpmap", r.endpoint)
	tflog.Debug(ctx, "Reading DERPMap", map[string]interface{}{"url": getURL})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// No DERPMap => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read DERPMap error", err.Error())
		return
	}

	var fetched tsclient.ACLDERPMap
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse read response error", err.Error())
		return
	}

	data.ID = types.StringValue("derpmap")

	// Convert fetched object => JSON
	raw, _ := json.MarshalIndent(fetched, "", "  ")
	data.DerpMapJson = types.StringValue(string(raw))

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Update => PUT /derpmap
func (r *derpMapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data derpMapResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// user-provided => parse JSON => tsclient.ACLDERPMap
	var updatedDM tsclient.ACLDERPMap
	if dmStr := data.DerpMapJson.ValueString(); dmStr != "" {
		if err := json.Unmarshal([]byte(dmStr), &updatedDM); err != nil {
			resp.Diagnostics.AddError("Parse derpmap_json error", err.Error())
			return
		}
	}

	putURL := fmt.Sprintf("%s/derpmap", r.endpoint)
	tflog.Debug(ctx, "Updating DERPMap", map[string]interface{}{"url": putURL})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodPut, putURL, updatedDM)
	if err != nil {
		if IsNotFound(err) {
			// no DERPMap => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update DERPMap error", err.Error())
		return
	}

	var returned tsclient.ACLDERPMap
	if err := json.Unmarshal(body, &returned); err != nil {
		resp.Diagnostics.AddError("Parse update response error", err.Error())
		return
	}

	data.ID = types.StringValue("derpmap")
	raw, _ := json.MarshalIndent(returned, "", "  ")
	data.DerpMapJson = types.StringValue(string(raw))

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Delete => DELETE /derpmap
func (r *derpMapResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	delURL := fmt.Sprintf("%s/derpmap", r.endpoint)
	_, err := doSingleObjectReq(ctx, r.httpClient, http.MethodDelete, delURL, nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Delete DERPMap error", err.Error())
		return
	}
	// remove from state
	resp.State.RemoveResource(ctx)
}
