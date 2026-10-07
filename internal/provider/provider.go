// Package provider implements the mamori Terraform/OpenTofu provider on top
// of the mamori Go client.
package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var _ provider.Provider = &mamoriProvider{}

// New returns a factory for the provider, as required by providerserver.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &mamoriProvider{version: version}
	}
}

type mamoriProvider struct {
	version string
}

type providerModel struct {
	Server             types.String `tfsdk:"server"`
	Username           types.String `tfsdk:"username"`
	Password           types.String `tfsdk:"password"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
}

func (p *mamoriProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "mamori"
	resp.Version = p.version
}

func (p *mamoriProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages objects on a mamori.io server.",
		Attributes: map[string]schema.Attribute{
			"server": schema.StringAttribute{
				Description: "Base URL of the mamori server, e.g. https://mamori.example.com. Defaults to MAMORI_SERVER.",
				Optional:    true,
			},
			"username": schema.StringAttribute{
				Description: "User to log in as. Defaults to MAMORI_USERNAME.",
				Optional:    true,
			},
			"password": schema.StringAttribute{
				Description: "Password of the login user. Defaults to MAMORI_PASSWORD.",
				Optional:    true,
				Sensitive:   true,
			},
			"insecure_skip_verify": schema.BoolAttribute{
				Description: "Skip TLS certificate verification (self-signed servers). Defaults to MAMORI_INSECURE_SKIP_VERIFY, else false.",
				Optional:    true,
			},
		},
	}
}

func (p *mamoriProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for attr, v := range map[string]types.String{"server": cfg.Server, "username": cfg.Username, "password": cfg.Password} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Unknown mamori provider setting",
				"The provider cannot log in until "+attr+" is known. Set it to a static value or use the environment variable.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	server := stringOrEnv(cfg.Server, "MAMORI_SERVER")
	username := stringOrEnv(cfg.Username, "MAMORI_USERNAME")
	password := stringOrEnv(cfg.Password, "MAMORI_PASSWORD")
	insecure := cfg.InsecureSkipVerify.ValueBool()
	if cfg.InsecureSkipVerify.IsNull() {
		insecure, _ = strconv.ParseBool(os.Getenv("MAMORI_INSECURE_SKIP_VERIFY"))
	}

	for attr, v := range map[string]string{"server": server, "username": username, "password": password} {
		if v == "" {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Missing mamori provider setting",
				"Set "+attr+" in the provider block or the matching MAMORI_* environment variable.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var opts []mamori.Option
	if insecure {
		opts = append(opts, mamori.WithInsecureSkipVerify())
	}
	c, err := mamori.New(server, opts...)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("server"), "Invalid mamori server", err.Error())
		return
	}
	if _, err := c.Login(ctx, username, password); err != nil {
		resp.Diagnostics.AddError("Unable to log in to mamori", err.Error())
		return
	}

	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *mamoriProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewUserResource,
		NewRoleResource,
		NewRoleGrantResource,
		NewSecretResource,
		NewDatasourceResource,
		NewIPResourceResource,
		NewHTTPResourceResource,
		NewSSHLoginResource,
		NewRequestableResourceResource,
		NewRemoteDesktopLoginResource,
		NewPermissionResource,
	}
}

func (p *mamoriProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func stringOrEnv(v types.String, env string) string {
	if !v.IsNull() {
		return v.ValueString()
	}
	return os.Getenv(env)
}
