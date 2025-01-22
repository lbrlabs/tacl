// auto_approvers_resource.go

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

var (
	_ resource.Resource              = &autoApproversResource{}
	_ resource.ResourceWithConfigure = &autoApproversResource{}
)

// NewAutoApproversResource is the constructor for the single ACLAutoApprovers resource.
func NewAutoApproversResource() resource.Resource {
	return &autoApproversResource{}
}

// autoApproversResource -> single object with ID="autoapprovers" once created.
type autoApproversResource struct {
	httpClient *http.Client
	endpoint   string
}

// We'll store routes as map[string][]string, exit_node as []string.
type autoApproversModel struct {
	ID       types.String   `tfsdk:"id"`        // always "autoapprovers" once created
	Routes   types.Map      `tfsdk:"routes"`    // map string => list string
	ExitNode []types.String `tfsdk:"exit_node"` // optional
}

func (r *autoApproversResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Metadata => resource "tacl_auto_approvers"
func (r *autoApproversResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auto_approvers"
}

func (r *autoApproversResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the single ACLAutoApprovers object at /autoapprovers.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'autoapprovers' once created.",
				Computed:    true,
			},
			"routes": schema.MapAttribute{
				Description: "Map of route => list of strings (auto-approve users).",
				Optional:    true,
				ElementType: types.ListType{ElemType: types.StringType},
			},
			"exit_node": schema.ListAttribute{
				Description: "ExitNode => slice of strings to auto-approve as exit nodes.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// CREATE => POST /autoapprovers
func (r *autoApproversResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data autoApproversModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert to tsclient.ACLAutoApprovers
	aap := tsclient.ACLAutoApprovers{
		Routes:   toStringSliceMap(data.Routes),
		ExitNode: toStringSlice(data.ExitNode),
	}

	url := fmt.Sprintf("%s/autoapprovers", r.endpoint)
	tflog.Debug(ctx, "Creating auto-approvers", map[string]interface{}{
		"url":     url,
		"payload": aap,
	})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodPost, url, aap)
	if err != nil {
		resp.Diagnostics.AddError("Create error", err.Error())
		return
	}

	var created tsclient.ACLAutoApprovers
	if err := json.Unmarshal(body, &created); err != nil {
		resp.Diagnostics.AddError("Error parse create response", err.Error())
		return
	}

	data.ID = types.StringValue("autoapprovers")
	data.Routes = toTerraformMapOfStringList(created.Routes)
	data.ExitNode = toTerraformStringSlice(created.ExitNode)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// READ => GET /autoapprovers
func (r *autoApproversResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data autoApproversModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/autoapprovers", r.endpoint)
	tflog.Debug(ctx, "Reading auto-approvers", map[string]interface{}{"url": url})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodGet, url, nil)
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read error", err.Error())
		return
	}

	var fetched tsclient.ACLAutoApprovers
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse read error", err.Error())
		return
	}

	data.ID = types.StringValue("autoapprovers")
	data.Routes = toTerraformMapOfStringList(fetched.Routes)
	data.ExitNode = toTerraformStringSlice(fetched.ExitNode)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// UPDATE => PUT /autoapprovers
func (r *autoApproversResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data autoApproversModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	aap := tsclient.ACLAutoApprovers{
		Routes:   toStringSliceMap(data.Routes),
		ExitNode: toStringSlice(data.ExitNode),
	}

	url := fmt.Sprintf("%s/autoapprovers", r.endpoint)
	tflog.Debug(ctx, "Updating auto-approvers", map[string]interface{}{"url": url})

	body, err := doSingleObjectReq(ctx, r.httpClient, http.MethodPut, url, aap)
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update error", err.Error())
		return
	}

	var updated tsclient.ACLAutoApprovers
	if err := json.Unmarshal(body, &updated); err != nil {
		resp.Diagnostics.AddError("Parse error", err.Error())
		return
	}

	data.ID = types.StringValue("autoapprovers")
	data.Routes = toTerraformMapOfStringList(updated.Routes)
	data.ExitNode = toTerraformStringSlice(updated.ExitNode)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// DELETE => DELETE /autoapprovers
func (r *autoApproversResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	url := fmt.Sprintf("%s/autoapprovers", r.endpoint)
	_, err := doSingleObjectReq(ctx, r.httpClient, http.MethodDelete, url, nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Delete error", err.Error())
		return
	}
	// remove from state
	resp.State.RemoveResource(ctx)
}
