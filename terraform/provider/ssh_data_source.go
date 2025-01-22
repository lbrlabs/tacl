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
	_ datasource.DataSource              = &sshDataSource{}
	_ datasource.DataSourceWithConfigure = &sshDataSource{}
)

// NewSSHDataSource => constructor for "tacl_ssh" data source.
func NewSSHDataSource() datasource.DataSource {
	return &sshDataSource{}
}

// sshDataSource => fetches a single SSH rule by integer index.
type sshDataSource struct {
	httpClient *http.Client
	endpoint   string
}

// sshDataSourceModel => local struct for reading the SSH rule by index
type sshDataSourceModel struct {
	// We'll store the index in "id" or a separate field—mirroring your ACL DS approach that used ID as index.
	ID          types.String   `tfsdk:"id"` // index as string
	Action      types.String   `tfsdk:"action"`
	Src         []types.String `tfsdk:"src"`
	Dst         []types.String `tfsdk:"dst"`
	Users       []types.String `tfsdk:"users"`
	CheckPeriod types.String   `tfsdk:"check_period"`
	AcceptEnv   []types.String `tfsdk:"accept_env"`
}

// ----- 1) Configure / 2) Metadata / 3) Schema -----

func (d *sshDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sshDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh"
}

func (d *sshDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading a single SSH rule by integer index in TACL’s array.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Index of the SSH rule in TACL’s array (e.g. '0').",
				Required:    true,
			},
			"action": schema.StringAttribute{
				Description: "SSH action: 'accept' or 'check'.",
				Computed:    true,
			},
			"src": schema.ListAttribute{
				Description: "Sources for the SSH rule.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"dst": schema.ListAttribute{
				Description: "Destinations for the SSH rule.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"users": schema.ListAttribute{
				Description: "SSH users allowed.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"check_period": schema.StringAttribute{
				Description: "Duration (e.g. '12h') for check actions.",
				Computed:    true,
			},
			"accept_env": schema.ListAttribute{
				Description: "Environment variable patterns allowed.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// ----- 4) Read -----

func (d *sshDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data sshDataSourceModel
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Interpret the "id" attribute as the numeric index
	idxStr := data.ID.ValueString()
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		resp.Diagnostics.AddError("Invalid index", fmt.Sprintf("Cannot parse '%s' as integer", idxStr))
		return
	}

	// GET /ssh/:index
	getURL := fmt.Sprintf("%s/ssh/%d", d.endpoint, idx)
	tflog.Debug(ctx, "Reading SSH data source by index", map[string]interface{}{
		"url":   getURL,
		"index": idx,
	})

	respBody, err := doSSHIndexRequest(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			resp.Diagnostics.AddWarning("Not found", fmt.Sprintf("No SSH rule at index %d", idx))
			return
		}
		resp.Diagnostics.AddError("Error reading SSH data source", err.Error())
		return
	}

	// The server presumably returns the raw TaclSSHEntry shape for that index
	// (or you can define a dedicated shape).
	var sshRule TaclSSHEntry
	if err := json.Unmarshal(respBody, &sshRule); err != nil {
		resp.Diagnostics.AddError("JSON parse error", err.Error())
		return
	}

	// Populate Terraform state
	data.Action = types.StringValue(sshRule.Action)
	data.Src = toTerraformStringSlice(sshRule.Src)
	data.Dst = toTerraformStringSlice(sshRule.Dst)
	data.Users = toTerraformStringSlice(sshRule.Users)
	data.CheckPeriod = types.StringValue(sshRule.CheckPeriod)
	data.AcceptEnv = toTerraformStringSlice(sshRule.AcceptEnv)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// doSSHIndexRequest => simpler than resource calls, but same idea
func doSSHIndexRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, &NotFoundError{Message: "SSH rule not found"}
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
