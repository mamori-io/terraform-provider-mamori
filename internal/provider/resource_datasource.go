package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure   = &datasourceResource{}
	_ resource.ResourceWithImportState = &datasourceResource{}
)

func NewDatasourceResource() resource.Resource { return &datasourceResource{} }

// datasourceResource manages a target database. The server does not report
// the configured options back in a stable form, so Read only checks that the
// datasource still exists; settings changed outside Terraform are not
// detected.
type datasourceResource struct{ clientResource }

type datasourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Type                    types.String `tfsdk:"type"`
	Driver                  types.String `tfsdk:"driver"`
	Host                    types.String `tfsdk:"host"`
	Port                    types.String `tfsdk:"port"`
	Database                types.String `tfsdk:"database"`
	TempDatabase            types.String `tfsdk:"temp_database"`
	User                    types.String `tfsdk:"user"`
	Password                types.String `tfsdk:"password"`
	Group                   types.String `tfsdk:"group"`
	URLProperties           types.String `tfsdk:"url_properties"`
	ConnectionString        types.String `tfsdk:"connection_string"`
	ExtraOptions            types.String `tfsdk:"extra_options"`
	CredentialResetDays     types.String `tfsdk:"credential_reset_days"`
	CredentialRole          types.String `tfsdk:"credential_role"`
	CaseSensitive           types.Bool   `tfsdk:"case_sensitive"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
	WebSQLAutoCommitDefault types.Bool   `tfsdk:"websql_auto_commit_default"`
}

func (r *datasourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datasource"
}

func (r *datasourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	opt := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Description: desc, Optional: true}
	}
	resp.Schema = schema.Schema{
		Description: "A target database proxied by mamori. Only changes made through Terraform are tracked: " +
			"the server's copy of the settings is not read back, and removing a setting from the configuration does not clear it on the server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name":                  schema.StringAttribute{Required: true, PlanModifiers: replace},
			"type":                  schema.StringAttribute{Description: "Datasource type, e.g. POSTGRESQL, ORACLE, SQL_SERVER.", Required: true, PlanModifiers: replace},
			"driver":                opt("Name of the configured database driver."),
			"host":                  opt("Database host. Not used with connection_string."),
			"port":                  opt("Database port."),
			"database":              opt("Default database."),
			"temp_database":         opt("Database for temporary tables. Defaults to database."),
			"user":                  opt("Database user mamori connects as."),
			"password":              schema.StringAttribute{Description: "Password for user.", Optional: true, Sensitive: true},
			"group":                 opt("Datasource group."),
			"url_properties":        opt("Extra JDBC URL properties, e.g. \"ssl=true;sslmode=require\"."),
			"connection_string":     opt("Full connection string, used instead of host, port and database."),
			"extra_options":         opt("Additional comma separated options in mamori SQL syntax, e.g. \"POOL_MAXIMUM '3'\"."),
			"credential_reset_days": opt("Enable managed passwords, reset every n days."),
			"credential_role":       opt("Role used for managed password resets."),
			"case_sensitive":        schema.BoolAttribute{Description: "Treat object names as case sensitive.", Optional: true},
			"enabled":               schema.BoolAttribute{Optional: true},
			"websql_auto_commit_default": schema.BoolAttribute{
				Description: "Default auto-commit for WebSQL sessions. The server defaults to false for Oracle and true otherwise.",
				Optional:    true,
			},
		},
	}
}

func optBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func (m *datasourceModel) toDatasource() *mamori.Datasource {
	return &mamori.Datasource{
		Name:                    m.Name.ValueString(),
		Type:                    m.Type.ValueString(),
		Driver:                  m.Driver.ValueString(),
		Host:                    m.Host.ValueString(),
		Port:                    m.Port.ValueString(),
		Database:                m.Database.ValueString(),
		TempDatabase:            m.TempDatabase.ValueString(),
		User:                    m.User.ValueString(),
		Password:                m.Password.ValueString(),
		Group:                   m.Group.ValueString(),
		URLProperties:           m.URLProperties.ValueString(),
		ConnectionString:        m.ConnectionString.ValueString(),
		ExtraOptions:            m.ExtraOptions.ValueString(),
		CredentialResetDays:     m.CredentialResetDays.ValueString(),
		CredentialRole:          m.CredentialRole.ValueString(),
		CaseSensitive:           m.CaseSensitive.ValueBool(),
		Enabled:                 optBool(m.Enabled),
		WebSQLAutoCommitDefault: optBool(m.WebSQLAutoCommitDefault),
	}
}

// datasourceChanges returns the settings that differ between state and plan.
func datasourceChanges(state, plan *datasourceModel) mamori.DatasourceUpdate {
	var u mamori.DatasourceUpdate
	str := func(dst **string, s, p types.String) {
		if !s.Equal(p) {
			v := p.ValueString()
			*dst = &v
		}
	}
	str(&u.Driver, state.Driver, plan.Driver)
	str(&u.Host, state.Host, plan.Host)
	str(&u.Port, state.Port, plan.Port)
	str(&u.Database, state.Database, plan.Database)
	str(&u.TempDatabase, state.TempDatabase, plan.TempDatabase)
	str(&u.User, state.User, plan.User)
	str(&u.Password, state.Password, plan.Password)
	str(&u.Group, state.Group, plan.Group)
	str(&u.URLProperties, state.URLProperties, plan.URLProperties)
	str(&u.ConnectionString, state.ConnectionString, plan.ConnectionString)
	str(&u.ExtraOptions, state.ExtraOptions, plan.ExtraOptions)
	str(&u.CredentialResetDays, state.CredentialResetDays, plan.CredentialResetDays)
	str(&u.CredentialRole, state.CredentialRole, plan.CredentialRole)
	if !state.CaseSensitive.Equal(plan.CaseSensitive) {
		u.CaseSensitive = optBool(plan.CaseSensitive)
		if u.CaseSensitive == nil {
			f := false
			u.CaseSensitive = &f
		}
	}
	if !state.Enabled.Equal(plan.Enabled) {
		u.Enabled = optBool(plan.Enabled)
	}
	if !state.WebSQLAutoCommitDefault.Equal(plan.WebSQLAutoCommitDefault) {
		u.WebSQLAutoCommitDefault = optBool(plan.WebSQLAutoCommitDefault)
	}
	return u
}

func (r *datasourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan datasourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Datasources.Create(ctx, plan.toDatasource()); err != nil {
		addError(&resp.Diagnostics, "create", "datasource", err)
		return
	}
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *datasourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state datasourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	row, err := r.client.Datasources.Read(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "datasource", err)
		return
	}
	state.ID = state.Name
	// On import nothing but the name is known; fill in the type so the
	// next plan does not force a replacement.
	if state.Type.IsNull() {
		if t := rowString(row, "type"); t != "" {
			state.Type = types.StringValue(t)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *datasourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state datasourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Datasources.Update(ctx, state.toDatasource(), datasourceChanges(&state, &plan), false); err != nil {
		addError(&resp.Diagnostics, "update", "datasource", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *datasourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state datasourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Datasources.Delete(ctx, state.Name.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "datasource", err)
	}
}

func (r *datasourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
