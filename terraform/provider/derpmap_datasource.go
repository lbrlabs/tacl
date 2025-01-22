package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// Ensure interface compliance
var (
	_ datasource.DataSource              = &derpMapDataSource{}
	_ datasource.DataSourceWithConfigure = &derpMapDataSource{}
)

// NewDERPMapDataSource => single object DS for /derpmap
func NewDERPMapDataSource() datasource.DataSource {
	return &derpMapDataSource{}
}

type derpMapDataSource struct {
	httpClient *http.Client
	endpoint   string
}

// We'll store just a single `id` + raw JSON.
type derpMapDSModel struct {
	ID          types.String `tfsdk:"id"`
	DerpMapJson types.String `tfsdk:"derpmap_json"`
}

func (d *derpMapDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *derpMapDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_derpmap"
}

func (d *derpMapDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading the single DERPMap object at /derpmap.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'derpmap' if found.",
				Computed:    true,
			},
			"derpmap_json": schema.StringAttribute{
				Description: "Full DERPMap JSON. If you want typed fields, expand them here.",
				Computed:    true,
			},
		},
	}
}

// Read => GET /derpmap
func (d *derpMapDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// no input
	var data derpMapDSModel

	getURL := fmt.Sprintf("%s/derpmap", d.endpoint)
	tflog.Debug(ctx, "Reading DERPMap data source", map[string]interface{}{"url": getURL})

	body, err := doSingleObjectReq(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// no object => no state
			return
		}
		resp.Diagnostics.AddError("Read DS error", err.Error())
		return
	}

	var fetched tsclient.ACLDERPMap
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse DS error", err.Error())
		return
	}

	data.ID = types.StringValue("derpmap")

	// Convert to JSON for storing
	raw, _ := json.MarshalIndent(fetched, "", "  ")
	data.DerpMapJson = types.StringValue(string(raw))

	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}
