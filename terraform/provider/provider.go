// provider.go
package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2/clientcredentials"
)

// taclProviderModel defines user-facing configuration fields.
type taclProviderModel struct {
	Endpoint     types.String `tfsdk:"endpoint"` // required
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	TailnetName  types.String `tfsdk:"tailnet_name"`
	Tags         types.String `tfsdk:"tags"`
	Ephemeral    types.Bool   `tfsdk:"ephemeral"`
}

// taclProvider holds state needed after configuration.
type taclProvider struct {
	httpClient    *http.Client
	endpoint      string
	tailnetName   string
	ephemeralMode bool
	tags          string
}

// Compile-time check that taclProvider implements provider.Provider.
var _ provider.Provider = (*taclProvider)(nil)

// New returns a single instance of the taclProvider.
func New() provider.Provider {
	return &taclProvider{}
}

// Metadata sets the provider name used in Terraform.
func (p *taclProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tacl"
}

// Schema defines the user-configurable attributes for the provider.
func (p *taclProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provider for TACL (Tailscale ACL).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Description: "TACL server URL (e.g. http://localhost:8080).",
				Required:    true,
			},
			"client_id": schema.StringAttribute{
				Description: "OAuth client ID for ephemeral Tailscale authentication (optional).",
				Optional:    true,
			},
			"client_secret": schema.StringAttribute{
				Description: "OAuth client secret for ephemeral Tailscale authentication (optional).",
				Optional:    true,
				Sensitive:   true,
			},
			"tailnet_name": schema.StringAttribute{
				Description: "Tailnet name for ephemeral Tailscale auth (e.g. mycorp.ts.net).",
				Optional:    true,
			},
			"tags": schema.StringAttribute{
				Description: "Comma-separated tags for ephemeral Tailscale nodes.",
				Optional:    true,
			},
			"ephemeral": schema.BoolAttribute{
				Description: "Whether ephemeral Tailscale keys are used (default true).",
				Optional:    true,
			},
		},
	}
}

// Configure sets up the provider after user config is parsed.
func (p *taclProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config taclProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Required: endpoint
	p.endpoint = config.Endpoint.ValueString()
	// Optional fields
	p.tailnetName = config.TailnetName.ValueString()
	p.ephemeralMode = !config.Ephemeral.IsNull() && config.Ephemeral.ValueBool()
	p.tags = config.Tags.ValueString()

	clientID := config.ClientID.ValueString()
	clientSecret := config.ClientSecret.ValueString()

	if clientID != "" && clientSecret != "" {
		// Ephemeral OAuth-based Tailscale auth
		tflog.Info(ctx, "Using ephemeral OAuth-based Tailscale auth")
		creds := clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     "https://login.tailscale.com/api/v2/oauth/token",
		}
		p.httpClient = creds.Client(context.Background())
	} else {
		tflog.Warn(ctx, "No Tailscale auth configured, using default client")
		p.httpClient = http.DefaultClient
	}

	tflog.Debug(ctx, fmt.Sprintf(
		"Provider configured with endpoint=%s, tailnet=%s, ephemeral=%v",
		p.endpoint, p.tailnetName, p.ephemeralMode))

	resp.ResourceData = p
	resp.DataSourceData = p
}

// DataSources returns a list of data source constructors.
func (p *taclProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewGroupDataSource,
		NewACLDataSource,
		NewAutoApproversDataSource,
		NewDERPMapDataSource,
		NewHostsDataSource,
		NewSettingsDataSource,
		NewNodeAttrDataSource,
	}
}

// Resources returns a list of resource constructors.
func (p *taclProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGroupResource,
		NewACLResource,
		NewAutoApproversResource,
		NewDERPMapResource,
		NewHostsResource,
		NewSettingsResource,
		NewNodeAttrResource,
	}
}
