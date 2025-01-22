// group_resource.go
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

var (
	_ resource.Resource              = &groupResource{}
	_ resource.ResourceWithConfigure = &groupResource{}
)

// NewGroupResource is the constructor for the group resource.
func NewGroupResource() resource.Resource {
	return &groupResource{}
}

type groupResource struct {
	httpClient *http.Client
	endpoint   string
}

type groupResourceModel struct {
	ID          types.String   `tfsdk:"id"`          // We'll store the group's name as ID
	Name        types.String   `tfsdk:"name"`        // Required
	Members     []types.String `tfsdk:"members"`     // Optional
	Description types.String   `tfsdk:"description"` // Optional
}

// Configure extracts the provider's httpClient and endpoint
func (r *groupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Metadata sets the resource type name, e.g. "tacl_group".
func (r *groupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines the resource attributes.
func (r *groupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Internal ID, same as `name`.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the group.",
				Required:    true,
			},
			"members": schema.ListAttribute{
				Description: "List of group members (strings: emails, other groups, etc.).",
				Optional:    true,
				ElementType: types.StringType,
			},
			"description": schema.StringAttribute{
				Description: "Optional description for the group.",
				Optional:    true,
			},
		},
	}
}

// Create => POST /groups
func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data groupResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"name":        data.Name.ValueString(),
		"members":     toStringSlice(data.Members),
		"description": data.Description.ValueString(),
	}

	postURL := fmt.Sprintf("%s/groups", r.endpoint)
	tflog.Debug(ctx, "Creating group via TACL", map[string]interface{}{
		"url":     postURL,
		"payload": payload,
	})

	body, err := doRequest(ctx, r.httpClient, http.MethodPost, postURL, payload)
	if err != nil {
		resp.Diagnostics.AddError("Create group error", err.Error())
		return
	}

	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		resp.Diagnostics.AddError("Error parsing create response", err.Error())
		return
	}

	// For simplicity, just set ID = name. If TACL modifies the name, you'd read it from `created`.
	data.ID = data.Name

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Read => GET /groups/:name
func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data groupResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	getURL := fmt.Sprintf("%s/groups/%s", r.endpoint, name)
	tflog.Debug(ctx, "Reading group via TACL", map[string]interface{}{
		"url":  getURL,
		"name": name,
	})

	body, err := doRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// If 404, group no longer exists => remove from state
			tflog.Warn(ctx, "Group not found, removing from state", map[string]interface{}{"name": name})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read group error", err.Error())
		return
	}

	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Error parsing read response", err.Error())
		return
	}

	// Update state from TACL's response
	data.ID = types.StringValue(name)
	data.Name = types.StringValue(name)

	if members, ok := fetched["members"].([]interface{}); ok {
		data.Members = toStringTypeSlice(members)
	}
	if desc, ok := fetched["description"].(string); ok {
		data.Description = types.StringValue(desc)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Update => PUT /groups
func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data groupResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"name":        data.Name.ValueString(),
		"members":     toStringSlice(data.Members),
		"description": data.Description.ValueString(),
	}

	putURL := fmt.Sprintf("%s/groups", r.endpoint)
	tflog.Debug(ctx, "Updating group via TACL", map[string]interface{}{
		"url":     putURL,
		"payload": payload,
	})

	body, err := doRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
	if err != nil {
		if IsNotFound(err) {
			// If TACL says 404, group doesn't exist => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update group error", err.Error())
		return
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		resp.Diagnostics.AddError("Error parsing update response", err.Error())
		return
	}

	// Optionally refresh data from the updated object
	if members, ok := updated["members"].([]interface{}); ok {
		data.Members = toStringTypeSlice(members)
	}
	if desc, ok := updated["description"].(string); ok {
		data.Description = types.StringValue(desc)
	}

	data.ID = data.Name

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Delete => DELETE /groups
func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data groupResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	delURL := fmt.Sprintf("%s/groups", r.endpoint)
	tflog.Debug(ctx, "Deleting group via TACL", map[string]interface{}{
		"url":  delURL,
		"name": data.Name.ValueString(),
	})

	payload := map[string]string{
		"name": data.Name.ValueString(),
	}

	_, err := doRequest(ctx, r.httpClient, http.MethodDelete, delURL, payload)
	if err != nil {
		if IsNotFound(err) {
			// Already gone
		} else {
			resp.Diagnostics.AddError("Delete group error", err.Error())
			return
		}
	}

	// Remove from state
	resp.State.RemoveResource(ctx)
}

func doRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// Group not found
		return nil, &NotFoundError{Message: "group not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, string(msg))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return respBody, nil
}
