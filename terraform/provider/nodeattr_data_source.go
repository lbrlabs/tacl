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

// nodeattrDataSource => read a single nodeAttrs array entry by index
var (
	_ datasource.DataSource              = &nodeattrDataSource{}
	_ datasource.DataSourceWithConfigure = &nodeattrDataSource{}
)

func NewNodeAttrDataSource() datasource.DataSource {
	return &nodeattrDataSource{}
}

type nodeattrDataSource struct {
	httpClient *http.Client
	endpoint   string
}

// nodeattrDSModel => same fields as resource, except we need ID as required input
type nodeattrDSModel struct {
	ID      types.String   `tfsdk:"id"`
	Target  []types.String `tfsdk:"target"`
	Attr    []types.String `tfsdk:"attr"`
	AppJSON types.String   `tfsdk:"app_json"`
}

func (d *nodeattrDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *nodeattrDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_nodeattr"
}

// We'll define a schema that requires "id" => array index
func (d *nodeattrDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading a single node attribute from /nodeattrs by index.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Index of the node attribute in TACL’s array.",
				Required:    true,
			},
			"target": schema.ListAttribute{
				Description: "List of target strings.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"attr": schema.ListAttribute{
				Description: "Optional list of attribute strings.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"app_json": schema.StringAttribute{
				Description: "If present, TACL's 'app' data as JSON.",
				Computed:    true,
			},
		},
	}
}

func (d *nodeattrDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data nodeattrDSModel
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	idxStr := data.ID.ValueString()
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Could not parse '%s' as integer", idxStr))
		return
	}

	getURL := fmt.Sprintf("%s/nodeattrs/%d", d.endpoint, idx)
	tflog.Debug(ctx, "Reading nodeattr (data source)", map[string]interface{}{
		"url":   getURL,
		"index": idx,
	})

	body, err := doNodeAttrDSHTTP(ctx, d.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// no data => do nothing
			return
		}
		resp.Diagnostics.AddError("Read nodeattr DS error", err.Error())
		return
	}

	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse DS response error", err.Error())
		return
	}

	// fill data
	if t, ok := fetched["target"].([]interface{}); ok {
		data.Target = toStringTypeSlice(t)
	}
	if a, ok := fetched["attr"].([]interface{}); ok {
		data.Attr = toStringTypeSlice(a)
	}
	if app, ok := fetched["app"]; ok {
		appBytes, _ := json.Marshal(app)
		data.AppJSON = types.StringValue(string(appBytes))
	} else {
		data.AppJSON = types.StringNull()
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

func doNodeAttrDSHTTP(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("nodeattr DS request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nodeattr DS request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "nodeattr not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, string(msg))
	}

	return io.ReadAll(resp.Body)
}
