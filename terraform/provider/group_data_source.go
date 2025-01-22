// group_data_source.go
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ datasource.DataSource              = &groupDataSource{}
	_ datasource.DataSourceWithConfigure = &groupDataSource{}
)

// NewGroupDataSource constructor.
func NewGroupDataSource() datasource.DataSource {
	return &groupDataSource{}
}

type groupDataSource struct {
	httpClient *http.Client
	endpoint   string
}

type groupDataSourceModel struct {
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	Members     []types.String `tfsdk:"members"`
	Description types.String   `tfsdk:"description"`
}

// Configure gets a handle to the provider’s httpClient & endpoint.
func (d *groupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	provider, ok := req.ProviderData.(*taclProvider)
	if !ok {
		return
	}
	d.httpClient = provider.httpClient
	d.endpoint = provider.endpoint
}

// Metadata sets the data source name, e.g. "tacl_group".
func (d *groupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema => user must specify `name`, we’ll return `id`, `members`, `description`.
func (d *groupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always the same as `name` for reference.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the group to look up.",
				Required:    true,
			},
			"members": schema.ListAttribute{
				Description: "List of group members.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"description": schema.StringAttribute{
				Description: "Description for the group.",
				Computed:    true,
			},
		},
	}
}

// Read => GET /groups/:name
func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data groupDataSourceModel
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	getURL := fmt.Sprintf("%s/groups/%s", d.endpoint, name)
	tflog.Debug(ctx, "Reading group via TACL (Data Source)", map[string]interface{}{
		"url":   getURL,
		"group": name,
	})

	respBody, err := doDSRequest(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// The group doesn't exist
			resp.Diagnostics.AddWarning("Group not found", fmt.Sprintf("No group named '%s' found.", name))
			return
		}
		resp.Diagnostics.AddError("Error reading group data source", err.Error())
		return
	}

	// Parse JSON => { "name":"...", "members":[],"description":"" }
	var fetched map[string]interface{}
	if err := json.Unmarshal(respBody, &fetched); err != nil {
		resp.Diagnostics.AddError("JSON parse error", err.Error())
		return
	}

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

// Basic doRequest for data source.
func doDSRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "Data source group not found"}
	}
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return respBody, nil
}
