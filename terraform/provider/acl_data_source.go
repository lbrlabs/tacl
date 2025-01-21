// acl_data_source.go
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure interface compliance for Terraform Plugin Framework.
var (
	_ datasource.DataSource              = &aclDataSource{}
	_ datasource.DataSourceWithConfigure = &aclDataSource{}
)

// NewACLDataSource (new-style) => "tacl_acl" data source.
func NewACLDataSource() datasource.DataSource {
	return &aclDataSource{}
}

// aclDataSource => for a single new-style ACL (by index).
type aclDataSource struct {
	httpClient *http.Client
	endpoint   string
}

type aclDataSourceModel struct {
	ID     types.String   `tfsdk:"id"` // index as string
	Action types.String   `tfsdk:"action"`
	Src    []types.String `tfsdk:"src"`
	Proto  types.String   `tfsdk:"proto"`
	Dst    []types.String `tfsdk:"dst"`
}

func (d *aclDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *aclDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_acl"
}

func (d *aclDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading a single new-style ACL entry by index.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Index of the ACL entry in TACL’s array (e.g. '0').",
				Required:    true,
			},
			"action": schema.StringAttribute{
				Description: "ACL action, e.g. 'accept' or 'deny'.",
				Computed:    true,
			},
			"src": schema.ListAttribute{
				Description: "List of sources.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"proto": schema.StringAttribute{
				Description: "Protocol, e.g. 'tcp' or 'udp'.",
				Computed:    true,
			},
			"dst": schema.ListAttribute{
				Description: "List of destinations.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *aclDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data aclDataSourceModel
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	idxStr := data.ID.ValueString()
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		resp.Diagnostics.AddError("Invalid index", fmt.Sprintf("Cannot parse '%s' as integer", idxStr))
		return
	}

	getURL := fmt.Sprintf("%s/acls/%d", d.endpoint, idx)
	tflog.Debug(ctx, "Reading new-style ACL data source", map[string]interface{}{
		"url":   getURL,
		"index": idx,
	})

	respBody, err := doNewStyleACLDSRequest(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			resp.Diagnostics.AddWarning("Not found", fmt.Sprintf("No ACL at index %d", idx))
			return
		}
		resp.Diagnostics.AddError("Error reading ACL data source", err.Error())
		return
	}

	var acl TaclACLEntry
	if err := json.Unmarshal(respBody, &acl); err != nil {
		resp.Diagnostics.AddError("JSON parse error", err.Error())
		return
	}

	// Populate TF state
	data.Action = types.StringValue(acl.Action)
	data.Src = toTerraformStringSlice(acl.Src)
	data.Proto = types.StringValue(acl.Proto)
	data.Dst = toTerraformStringSlice(acl.Dst)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// doNewStyleACLDSRequest => similar to resource, but kept separate.
func doNewStyleACLDSRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
