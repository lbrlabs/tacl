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

// Ensure interface compliance: Resource + ResourceWithConfigure
var (
	_ resource.Resource              = &settingsResource{}
	_ resource.ResourceWithConfigure = &settingsResource{}
)

// NewSettingsResource => returns a resource for the single /settings object
func NewSettingsResource() resource.Resource {
	return &settingsResource{}
}

// settingsResource => single-object resource for "tacl_settings"
type settingsResource struct {
	httpClient *http.Client
	endpoint   string
}

// We store ID="settings" once created, plus the 3 fields
type settingsResourceModel struct {
	ID                  types.String `tfsdk:"id"`                    // always "settings" after create
	DisableIPv4         types.Bool   `tfsdk:"disable_ipv4"`          // from JSON: "disableIPv4"
	OneCGNATRoute       types.String `tfsdk:"one_cgnat_route"`       // from JSON: "oneCGNATRoute"
	RandomizeClientPort types.Bool   `tfsdk:"randomize_client_port"` // from JSON: "randomizeClientPort"
}

func (r *settingsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *settingsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	// e.g. "tacl_settings"
	resp.TypeName = req.ProviderTypeName + "_settings"
}

// We define the 3 fields + computed ID
func (r *settingsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the single Settings object at /settings.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Always 'settings' once created.",
				Computed:    true,
			},
			"disable_ipv4": schema.BoolAttribute{
				Description: "Disable IPv4 setting (disableIPv4).",
				Required:    true,
			},
			"one_cgnat_route": schema.StringAttribute{
				Description: "OneCGNATRoute setting.",
				Required:    true,
			},
			"randomize_client_port": schema.BoolAttribute{
				Description: "Randomize client port (randomizeClientPort).",
				Required:    true,
			},
		},
	}
}

// CREATE => POST /settings => must not already exist
func (r *settingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data settingsResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Build the JSON payload from the plan
	payload := map[string]interface{}{
		"disableIPv4":         data.DisableIPv4.ValueBool(),
		"oneCGNATRoute":       data.OneCGNATRoute.ValueString(),
		"randomizeClientPort": data.RandomizeClientPort.ValueBool(),
	}

	postURL := fmt.Sprintf("%s/settings", r.endpoint)
	tflog.Debug(ctx, "Creating Settings via TACL", map[string]interface{}{"url": postURL, "payload": payload})

	body, err := doSettingsRequest(ctx, r.httpClient, http.MethodPost, postURL, payload)
	if err != nil {
		resp.Diagnostics.AddError("Create settings error", err.Error())
		return
	}

	// The server returns the newly created Settings in JSON
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		resp.Diagnostics.AddError("Parse create response error", err.Error())
		return
	}

	// ID => "settings"
	data.ID = types.StringValue("settings")

	// Optionally read back the final fields
	if disable, ok := created["disableIPv4"].(bool); ok {
		data.DisableIPv4 = types.BoolValue(disable)
	}
	if route, ok := created["oneCGNATRoute"].(string); ok {
		data.OneCGNATRoute = types.StringValue(route)
	}
	if randPort, ok := created["randomizeClientPort"].(bool); ok {
		data.RandomizeClientPort = types.BoolValue(randPort)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// READ => GET /settings => returns JSON or empty struct
func (r *settingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data settingsResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getURL := fmt.Sprintf("%s/settings", r.endpoint)
	tflog.Debug(ctx, "Reading Settings via TACL", map[string]interface{}{"url": getURL})

	body, err := doSettingsRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// no settings => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read settings error", err.Error())
		return
	}

	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse read response error", err.Error())
		return
	}

	// If server returned an empty object => TACL may treat that as "no settings"
	// We'll consider that as existing, but with defaults
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

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// UPDATE => PUT /settings => must exist first
func (r *settingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data settingsResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"disableIPv4":         data.DisableIPv4.ValueBool(),
		"oneCGNATRoute":       data.OneCGNATRoute.ValueString(),
		"randomizeClientPort": data.RandomizeClientPort.ValueBool(),
	}

	putURL := fmt.Sprintf("%s/settings", r.endpoint)
	tflog.Debug(ctx, "Updating Settings via TACL", map[string]interface{}{"url": putURL})

	body, err := doSettingsRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
	if err != nil {
		if IsNotFound(err) {
			// no existing => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update settings error", err.Error())
		return
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		resp.Diagnostics.AddError("Parse update response error", err.Error())
		return
	}

	data.ID = types.StringValue("settings")

	if disable, ok := updated["disableIPv4"].(bool); ok {
		data.DisableIPv4 = types.BoolValue(disable)
	}
	if route, ok := updated["oneCGNATRoute"].(string); ok {
		data.OneCGNATRoute = types.StringValue(route)
	}
	if randPort, ok := updated["randomizeClientPort"].(bool); ok {
		data.RandomizeClientPort = types.BoolValue(randPort)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// DELETE => DELETE /settings
func (r *settingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	delURL := fmt.Sprintf("%s/settings", r.endpoint)
	_, err := doSettingsRequest(ctx, r.httpClient, http.MethodDelete, delURL, nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Delete settings error", err.Error())
		return
	}
	// remove from state
	resp.State.RemoveResource(ctx)
}

// doSettingsRequest => helper for single-object /settings calls
func doSettingsRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to create settings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("settings request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "Settings not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, msg)
	}

	return io.ReadAll(resp.Body)
}
