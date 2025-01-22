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

// hostsResource implements Resource and ResourceWithConfigure for "tacl_hosts" (multi-object).
var (
	_ resource.Resource              = &hostsResource{}
	_ resource.ResourceWithConfigure = &hostsResource{}
)

// NewHostsResource is the constructor for "tacl_host" resource
// which manages a single host entry at /hosts.
func NewHostsResource() resource.Resource {
	return &hostsResource{}
}

// hostsResource manages a single Host: {Name, IP}.
type hostsResource struct {
	httpClient *http.Client
	endpoint   string
}

// hostsResourceModel => "tacl_host"
type hostsResourceModel struct {
	ID   types.String `tfsdk:"id"`   // we store the host's Name as ID
	Name types.String `tfsdk:"name"` // required
	IP   types.String `tfsdk:"ip"`   // required
}

// Configure => retrieve the provider’s HTTP client & endpoint
func (r *hostsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	p, ok := req.ProviderData.(*taclProvider)
	if !ok {
		return
	}
	r.httpClient = p.httpClient
	r.endpoint = p.endpoint
}

// Metadata => resource type "tacl_host"
func (r *hostsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host"
}

// Schema => { name (required), ip (required) }, and an ID that we store the same as name.
func (r *hostsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a single host entry in TACL’s /hosts array, which is ultimately stored as a map of Name=>IP.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Same as the host's Name.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Unique hostname.",
				Required:    true,
			},
			"ip": schema.StringAttribute{
				Description: "IP address (or IP/CIDR) for this host.",
				Required:    true,
			},
		},
	}
}

// Create => POST /hosts => add new host
func (r *hostsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data hostsResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"name": data.Name.ValueString(),
		"ip":   data.IP.ValueString(),
	}

	postURL := fmt.Sprintf("%s/hosts", r.endpoint)
	tflog.Debug(ctx, "Creating host via TACL", map[string]interface{}{
		"url":     postURL,
		"payload": payload,
	})

	body, err := doHostsRequest(ctx, r.httpClient, http.MethodPost, postURL, payload)
	if err != nil {
		resp.Diagnostics.AddError("Create host error", err.Error())
		return
	}

	// TACL returns the newly created host => { "name":"...", "ip":"..." }
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		resp.Diagnostics.AddError("JSON parse error", err.Error())
		return
	}

	// We'll store ID = Name
	data.ID = data.Name

	// Save final state
	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Read => GET /hosts/:name => retrieve a single host
func (r *hostsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data hostsResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	getURL := fmt.Sprintf("%s/hosts/%s", r.endpoint, name)
	tflog.Debug(ctx, "Reading host via TACL", map[string]interface{}{
		"url":  getURL,
		"name": name,
	})

	body, err := doHostsRequest(ctx, r.httpClient, http.MethodGet, getURL, nil)
	if err != nil {
		if IsNotFound(err) {
			// Host not found => remove from state
			tflog.Warn(ctx, "Host not found, removing from state", map[string]interface{}{"name": name})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read host error", err.Error())
		return
	}

	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		resp.Diagnostics.AddError("Parse read error", err.Error())
		return
	}

	data.ID = data.Name
	// We expect { "name":"...", "ip":"..." }
	if ip, ok := fetched["ip"].(string); ok {
		data.IP = types.StringValue(ip)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Update => PUT /hosts => { "name":..., "ip":... }
func (r *hostsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data hostsResourceModel
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TACL expects { "name":"...", "ip":"..." }
	payload := map[string]interface{}{
		"name": data.Name.ValueString(),
		"ip":   data.IP.ValueString(),
	}

	putURL := fmt.Sprintf("%s/hosts", r.endpoint)
	tflog.Debug(ctx, "Updating host via TACL", map[string]interface{}{
		"url":     putURL,
		"payload": payload,
	})

	body, err := doHostsRequest(ctx, r.httpClient, http.MethodPut, putURL, payload)
	if err != nil {
		if IsNotFound(err) {
			// If not found => remove from state
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Update host error", err.Error())
		return
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		resp.Diagnostics.AddError("Parse update error", err.Error())
		return
	}

	// Overwrite state => ID stays the same
	data.ID = data.Name
	if ipStr, ok := updated["ip"].(string); ok {
		data.IP = types.StringValue(ipStr)
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

// Delete => DELETE /hosts => { "name": "hostname" }
func (r *hostsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data hostsResourceModel
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	delURL := fmt.Sprintf("%s/hosts", r.endpoint)
	tflog.Debug(ctx, "Deleting host via TACL", map[string]interface{}{
		"url":  delURL,
		"name": data.Name.ValueString(),
	})

	payload := map[string]string{
		"name": data.Name.ValueString(),
	}

	_, err := doHostsRequest(ctx, r.httpClient, http.MethodDelete, delURL, payload)
	if err != nil {
		if IsNotFound(err) {
			// already gone
		} else {
			resp.Diagnostics.AddError("Delete host error", err.Error())
			return
		}
	}
	// remove from state
	resp.State.RemoveResource(ctx)
}

// doHostsRequest => helper for /hosts endpoints
func doHostsRequest(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, &NotFoundError{Message: "Host not found"}
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", resp.StatusCode, msg)
	}

	return io.ReadAll(resp.Body)
}
