package provider

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure      = &sshLoginResource{}
	_ resource.ResourceWithImportState    = &sshLoginResource{}
	_ resource.ResourceWithValidateConfig = &sshLoginResource{}
	_ resource.ResourceWithModifyPlan     = &sshLoginResource{}
)

func NewSSHLoginResource() resource.Resource { return &sshLoginResource{} }

type sshLoginResource struct{ clientResource }

type sshLoginModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Host           types.String `tfsdk:"host"`
	Port           types.Int64  `tfsdk:"port"`
	User           types.String `tfsdk:"user"`
	Password       types.String `tfsdk:"password"`
	PrivateKeyName types.String `tfsdk:"private_key_name"`
	ThemeName      types.String `tfsdk:"theme_name"`
	IdleTimeout    types.Int64  `tfsdk:"idle_timeout"`
	LoginMode      types.String `tfsdk:"login_mode"`
}

func (r *sshLoginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_login"
}

func (r *sshLoginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A stored SSH target and its credentials. Grant access with mamori_permission (type \"ssh\" or \"sftp\").",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Server-assigned login id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"host": schema.StringAttribute{Required: true},
			"port": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(22)},
			"user": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"password": schema.StringAttribute{
				Description: "Password for user. Not returned by the server, so drift is not detected.",
				Optional:    true,
				Sensitive:   true,
			},
			"private_key_name": schema.StringAttribute{
				Description: "Name of a mamori SSH key to authenticate with instead of a password.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"theme_name":   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"idle_timeout": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(30)},
			"login_mode": schema.StringAttribute{
				Description: "How the login authenticates to the remote server: \"key\" uses private_key_name, \"cred\" uses user and password, " +
					"\"mamori\" prompts the connecting user (Web SSH only). The server derives the mode from the credentials, " +
					"so when unset it is key if private_key_name is set, else cred if password is set, else mamori.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{stringvalidator.OneOf("key", "cred", "mamori")},
			},
		},
	}
}

func (r *sshLoginResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m sshLoginModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !known(m.LoginMode) {
		return
	}
	set := func(v types.String) bool { return v.IsUnknown() || v.ValueString() != "" }
	need := func(a string, v types.String) {
		if !set(v) {
			resp.Diagnostics.AddAttributeError(path.Root(a), "Missing credential", a+" is required with login_mode "+m.LoginMode.ValueString()+".")
		}
	}
	forbid := func(a string, v types.String) {
		if set(v) {
			resp.Diagnostics.AddAttributeError(path.Root(a), "Credential not used", a+" cannot be used with login_mode "+m.LoginMode.ValueString()+".")
		}
	}
	switch m.LoginMode.ValueString() {
	case "key":
		need("private_key_name", m.PrivateKeyName)
	case "cred":
		need("password", m.Password)
		forbid("private_key_name", m.PrivateKeyName)
	case "mamori":
		forbid("private_key_name", m.PrivateKeyName)
		forbid("password", m.Password)
	}
}

// ModifyPlan fills in an unset login_mode with the mode the server will
// derive from the planned credentials.
func (r *sshLoginResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var config, plan sshLoginModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !config.LoginMode.IsNull() {
		return
	}
	mode := types.StringUnknown()
	if !plan.PrivateKeyName.IsUnknown() && !plan.Password.IsUnknown() {
		mode = types.StringValue(string(sshLoginModeFor(plan.PrivateKeyName.ValueString(), plan.Password.ValueString())))
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("login_mode"), mode)...)
}

// sshLoginModeFor returns the mode the server derives from a login's
// credentials.
func sshLoginModeFor(privateKeyName, password string) mamori.SSHLoginMode {
	switch {
	case privateKeyName != "":
		return mamori.SSHLoginModeKey
	case password != "":
		return mamori.SSHLoginModeCredentials
	}
	return mamori.SSHLoginModePrompt
}

func (m *sshLoginModel) toLogin() *mamori.SSHLogin {
	return &mamori.SSHLogin{
		ID:             m.ID.ValueString(),
		Name:           m.Name.ValueString(),
		Host:           m.Host.ValueString(),
		Port:           int(m.Port.ValueInt64()),
		User:           m.User.ValueString(),
		Password:       m.Password.ValueString(),
		PrivateKeyName: m.PrivateKeyName.ValueString(),
		ThemeName:      m.ThemeName.ValueString(),
		IdleTimeout:    int(m.IdleTimeout.ValueInt64()),
		LoginMode:      mamori.SSHLoginMode(m.LoginMode.ValueString()),
	}
}

// find returns the named login's row and its decoded form.
func (r *sshLoginResource) find(ctx context.Context, name string) (mamori.Row, *mamori.SSHLogin, error) {
	rows, err := r.client.SSHLogins.GetAll(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		if rowString(row, "name") != name {
			continue
		}
		b, err := json.Marshal(row)
		if err != nil {
			return nil, nil, err
		}
		var l mamori.SSHLogin
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, nil, err
		}
		return row, &l, nil
	}
	return nil, nil, mamori.ErrNotFound
}

func (r *sshLoginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshLoginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.SSHLogins.Create(ctx, plan.toLogin()); err != nil {
		addError(&resp.Diagnostics, "create", "SSH login", err)
		return
	}
	_, l, err := r.find(ctx, plan.Name.ValueString())
	if err != nil {
		addError(&resp.Diagnostics, "read back", "SSH login", err)
		return
	}
	plan.ID = types.StringValue(l.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshLoginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshLoginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	row, l, err := r.find(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "SSH login", err)
		return
	}
	state.ID = types.StringValue(l.ID)
	if l.Host != "" {
		state.Host = types.StringValue(l.Host)
		state.User = types.StringValue(l.User)
		state.Port = types.Int64Value(int64(l.Port))
	}
	// Only overwrite optional settings the listing actually reports.
	if _, ok := row["private_key_name"]; ok {
		state.PrivateKeyName = types.StringValue(l.PrivateKeyName)
	}
	if _, ok := row["theme_name"]; ok {
		state.ThemeName = types.StringValue(l.ThemeName)
	}
	if s := rowString(row, "idle_timeout"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			state.IdleTimeout = types.Int64Value(n)
		}
	}
	if l.LoginMode != "" {
		state.LoginMode = types.StringValue(string(l.LoginMode))
	} else if state.LoginMode.IsNull() || state.LoginMode.IsUnknown() {
		state.LoginMode = types.StringValue(string(sshLoginModeFor(state.PrivateKeyName.ValueString(), state.Password.ValueString())))
	}
	if state.Port.IsNull() {
		state.Port = types.Int64Value(22)
	}
	if state.IdleTimeout.IsNull() {
		state.IdleTimeout = types.Int64Value(30)
	}
	for _, s := range []*types.String{&state.User, &state.PrivateKeyName, &state.ThemeName} {
		if s.IsNull() {
			*s = types.StringValue("")
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sshLoginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshLoginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.SSHLogins.Update(ctx, plan.toLogin()); err != nil {
		addError(&resp.Diagnostics, "update", "SSH login", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshLoginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshLoginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.SSHLogins.Delete(ctx, state.Name.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "SSH login", err)
	}
}

func (r *sshLoginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
