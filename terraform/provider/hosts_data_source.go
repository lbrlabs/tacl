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

// hostsDataSource => read a single host by name
var (
	_ datasource.DataSource              = &hostsDataSource{}
	_ datasource.DataSourceWithConfigure = &hostsDataSource{}
)

func NewHostsDataSource() datasource.DataSource {
	return &hostsDataSource{}
}

type hostsDataSource struct {
	httpClient *http.Client
	endpoint   string
}

type hostsDataSourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	IP   types.String `tfsdk:"ip"`
}

func (d *hostsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	p, ok := req.ProviderData.(*taclProvider)
	if !ok {
		return
	}
	d.httpClient = p.httpClient
	d.endpoint = p.endpoint
}

func (d *hostsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	// e.g. "tacl_host"
	resp.TypeName = req.ProviderTypeName + "_host"
}

func (d *hostsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	// user must specify name
	resp.Schema = schema.Schema{
		Description: "Data source for reading one host by name from /hosts.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Same as 'name' after read.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the host to look up.",
				Required:    true,
			},
			"ip": schema.StringAttribute{
				Description: "IP address for this host, if found.",
				Computed:    true,
			},
		},
	}
}

// Read => GET /hosts/:name
func (d *hostsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data hostsDataSourceModel

	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	getURL := fmt.Sprintf("%s/hosts/%s", d.endpoint, name)
	tflog.Debug(ctx, "Reading host DS via TACL", map[string]interface{}{
		"url":  getURL,
		"name": name,
	})

	body, err := doHostsDSRequest(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			resp.Diagnostics.AddWarning("Host not found", fmt.Sprintf("No host named '%s' found", name))
			return
		}
		resp.Diagnostics.AddError("Read host DS error", err.Error())
		return
	}

	// TACL returns { "name":"...", "ip":"..." }
	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse DS response error", err.Error())
		return
	}

	// Save results
	data.ID = data.Name
	if ip, ok := fetched["ip"].(string); ok {
		data.IP = types.StringValue(ip)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// doHostsDSRequest => minimal DS version of an HTTP call for /hosts
func doHostsDSRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to create DS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hosts DS request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "Host not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned HTTP %d: %s", resp.StatusCode, msg)
	}

	return io.ReadAll(resp.Body)
}
