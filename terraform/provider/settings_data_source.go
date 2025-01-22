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

// Ensure DS compliance
var (
	_ datasource.DataSource              = &settingsDataSource{}
	_ datasource.DataSourceWithConfigure = &settingsDataSource{}
)

// NewSettingsDataSource => a DS for the single /settings object
func NewSettingsDataSource() datasource.DataSource {
	return &settingsDataSource{}
}

type settingsDataSource struct {
	httpClient *http.Client
	endpoint   string
}

type settingsDSModel struct {
	ID                  types.String `tfsdk:"id"`
	DisableIPv4         types.Bool   `tfsdk:"disable_ipv4"`
	OneCGNATRoute       types.String `tfsdk:"one_cgnat_route"`
	RandomizeClientPort types.Bool   `tfsdk:"randomize_client_port"`
}

func (d *settingsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *settingsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings"
}

// We have no required inputs, we just read the single Settings if it exists
func (d *settingsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading the single /settings object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'settings' if found.",
				Computed:    true,
			},
			"disable_ipv4": schema.BoolAttribute{
				Description: "Disable IPv4 setting.",
				Computed:    true,
			},
			"one_cgnat_route": schema.StringAttribute{
				Description: "OneCGNATRoute.",
				Computed:    true,
			},
			"randomize_client_port": schema.BoolAttribute{
				Description: "Randomize client port.",
				Computed:    true,
			},
		},
	}
}

func (d *settingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// We read the single object, no input needed
	var data settingsDSModel

	getURL := fmt.Sprintf("%s/settings", d.endpoint)
	tflog.Debug(ctx, "Reading settings data source", map[string]interface{}{"url": getURL})

	body, err := doSettingsDSRequest(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// no settings => no state
			return
		}
		resp.Diagnostics.AddError("Read settings DS error", err.Error())
		return
	}

	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse DS error", err.Error())
		return
	}

	data.ID = types.StringValue("settings")

	if disable, ok := fetched["disableIPv4"].(bool); ok {
		data.DisableIPv4 = types.BoolValue(disable)
	} else {
		data.DisableIPv4 = types.BoolValue(false)
	}

	if route, ok := fetched["oneCGNATRoute"].(string); ok {
		data.OneCGNATRoute = types.StringValue(route)
	} else {
		data.OneCGNATRoute = types.StringValue("")
	}

	if randPort, ok := fetched["randomizeClientPort"].(bool); ok {
		data.RandomizeClientPort = types.BoolValue(randPort)
	} else {
		data.RandomizeClientPort = types.BoolValue(false)
	}

	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

func doSettingsDSRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create DS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("settings DS request error: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		return nil, &NotFoundError{Message: "Settings not found"}
	}
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", res.StatusCode, msg)
	}

	return io.ReadAll(res.Body)
}
