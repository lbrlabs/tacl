// auto_approvers_data_source.go

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

// Ensure DS compliance
var (
	_ datasource.DataSource              = &autoApproversDataSource{}
	_ datasource.DataSourceWithConfigure = &autoApproversDataSource{}
)

func NewAutoApproversDataSource() datasource.DataSource {
	return &autoApproversDataSource{}
}

type autoApproversDataSource struct {
	httpClient *http.Client
	endpoint   string
}

type autoApproversDSModel struct {
	ID       types.String   `tfsdk:"id"`
	Routes   types.Map      `tfsdk:"routes"`
	ExitNode []types.String `tfsdk:"exit_node"`
}

func (d *autoApproversDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *autoApproversDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auto_approvers"
}

func (d *autoApproversDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading the single autoapprovers object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'autoapprovers' if present.",
				Computed:    true,
			},
			"routes": schema.MapAttribute{
				Description: "Map route => list of autoapprove users.",
				Computed:    true,
				ElementType: types.ListType{ElemType: types.StringType},
			},
			"exit_node": schema.ListAttribute{
				Description: "ExitNode => slice of strings.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *autoApproversDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// This DS has no required input, we just read the single object
	var data autoApproversDSModel

	getURL := fmt.Sprintf("%s/autoapprovers", d.endpoint)
	tflog.Debug(ctx, "Reading autoapprovers data source", map[string]interface{}{"url": getURL})

	body, err := doSingleObjectReq(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// no object => no state
			return
		}
		resp.Diagnostics.AddError("Read DS error", err.Error())
		return
	}

	var fetched tsclient.ACLAutoApprovers
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse DS error", err.Error())
		return
	}

	data.ID = types.StringValue("autoapprovers")
	data.Routes = toTerraformMapOfStringList(fetched.Routes)
	data.ExitNode = toTerraformStringSlice(fetched.ExitNode)

	diags := resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}
