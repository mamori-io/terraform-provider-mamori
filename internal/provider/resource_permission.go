package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure      = &permissionResource{}
	_ resource.ResourceWithValidateConfig = &permissionResource{}
)

func NewPermissionResource() resource.Resource { return &permissionResource{} }

// permissionResource grants one mamori permission. Every attribute forces
// replacement, so a change revokes the old grant and grants the new one.
type permissionResource struct{ clientResource }

type permissionModel struct {
	ID              types.String `tfsdk:"id"`
	Grantee         types.String `tfsdk:"grantee"`
	Type            types.String `tfsdk:"type"`
	Name            types.String `tfsdk:"name"`
	Privileges      types.Set    `tfsdk:"privileges"`
	Datasource      types.String `tfsdk:"datasource"`
	Database        types.String `tfsdk:"database"`
	Schema          types.String `tfsdk:"schema"`
	Object          types.String `tfsdk:"object"`
	WhereClause     types.String `tfsdk:"where_clause"`
	RowLimit        types.String `tfsdk:"row_limit"`
	Unauthenticated types.Bool   `tfsdk:"unauthenticated"`
	WithGrantOption types.Bool   `tfsdk:"with_grant_option"`
	ValidFrom       types.String `tfsdk:"valid_from"`
	ValidUntil      types.String `tfsdk:"valid_until"`
}

// Permission types accepted by the type attribute.
const (
	permDatasource    = "datasource"
	permMamori        = "mamori"
	permCredential    = "credential"
	permPolicy        = "policy"
	permKey           = "encryption_key"
	permSSH           = "ssh"
	permSFTP          = "sftp"
	permRemoteDesktop = "remote_desktop"
	permIPResource    = "ip_resource"
	permHTTPResource  = "http_resource"
	permSecret        = "secret"
	permScript        = "script"
	permScriptFlow    = "script_flow"
)

// namedPermTypes are the types granted on a single object given by name.
var namedPermTypes = []string{permPolicy, permKey, permSSH, permSFTP, permRemoteDesktop, permIPResource, permHTTPResource, permSecret, permScript, permScriptFlow}

func (r *permissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

func (r *permissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	opt := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Description: desc, Optional: true, PlanModifiers: replace}
	}
	allTypes := append([]string{permDatasource, permMamori, permCredential}, namedPermTypes...)
	resp.Schema = schema.Schema{
		Description: "Grants a permission to a user or role. Use mamori_role_grant to grant roles.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"grantee": schema.StringAttribute{Description: "User or role receiving the permission.", Required: true, PlanModifiers: replace},
			"type": schema.StringAttribute{
				Description:   "What is granted: " + strings.Join(allTypes, ", ") + ".",
				Required:      true,
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf(allTypes...)},
			},
			"name": opt("Object granted on, for the named types (secret, ssh, ip_resource, ...). For credential, the database login name."),
			"privileges": schema.SetAttribute{
				Description:   "For datasource: database privileges such as SELECT or INSERT. For mamori: server privileges such as CREATE USER.",
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"datasource":   opt("Datasource name (datasource and credential types). \"*\" matches all."),
			"database":     opt("Database, for the datasource type. \"*\" matches all; omit to stop the path here."),
			"schema":       opt("Schema, for the datasource type."),
			"object":       opt("Table or other object, for the datasource type."),
			"where_clause": opt("Row filter (without WHERE), for the datasource type."),
			"row_limit":    opt("Row limit for the datasource type: a number or \"none\"."),
			"unauthenticated": schema.BoolAttribute{
				Description:   "For ip_resource: grant UNAUTHENTICATED IP USAGE (no 2FA each time) instead of IP USAGE.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"with_grant_option": schema.BoolAttribute{
				Description:   "Allow the grantee to grant the permission on.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"valid_from":  opt("Start of the grant, \"YYYY-MM-DD HH:mm\"."),
			"valid_until": opt("End of the grant, \"YYYY-MM-DD HH:mm\"."),
		},
	}
}

func (r *permissionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m permissionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Type.IsUnknown() {
		return
	}
	t := m.Type.ValueString()
	require := func(attr string, v interface{ IsNull() bool }) {
		if v.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Missing attribute", fmt.Sprintf("%s is required when type is %q.", attr, t))
		}
	}
	switch t {
	case permDatasource:
		require("privileges", m.Privileges)
		require("datasource", m.Datasource)
	case permMamori:
		require("privileges", m.Privileges)
	case permCredential:
		require("datasource", m.Datasource)
	default:
		require("name", m.Name)
	}
}

func (m *permissionModel) permission(ctx context.Context) (mamori.Permission, error) {
	var privs []string
	if !m.Privileges.IsNull() {
		if d := m.Privileges.ElementsAs(ctx, &privs, false); d.HasError() {
			return nil, diagError(d)
		}
	}
	grantee, name := m.Grantee.ValueString(), m.Name.ValueString()

	var p mamori.Permission
	switch m.Type.ValueString() {
	case permDatasource:
		dp := mamori.NewDatasourcePermission(grantee)
		for _, x := range privs {
			dp.Permissions = append(dp.Permissions, mamori.DBPermission(x))
		}
		dp.On(m.Datasource.ValueString(), m.Database.ValueString(), m.Schema.ValueString(), m.Object.ValueString())
		dp.Where = m.WhereClause.ValueString()
		dp.RowLimit = m.RowLimit.ValueString()
		p = dp
	case permMamori:
		mp := mamori.NewMamoriPermission(grantee)
		for _, x := range privs {
			mp.Permissions = append(mp.Permissions, mamori.MamoriPrivilege(x))
		}
		p = mp
	case permCredential:
		cp := mamori.NewCredentialPermission(grantee)
		cp.Datasource, cp.LoginName = m.Datasource.ValueString(), name
		p = cp
	case permPolicy:
		p = mamori.NewPolicyPermission(name, grantee)
	case permKey:
		p = mamori.NewKeyPermission(name, grantee)
	case permSSH:
		p = mamori.NewSSHLoginPermission(name, grantee)
	case permSFTP:
		p = mamori.NewSFTPLoginPermission(name, grantee)
	case permRemoteDesktop:
		p = mamori.NewRemoteDesktopLoginPermission(name, grantee)
	case permIPResource:
		ip := mamori.NewIPResourcePermission(name, grantee)
		ip.Unauthenticated = m.Unauthenticated.ValueBool()
		p = ip
	case permHTTPResource:
		p = mamori.NewHTTPResourcePermission(name, grantee)
	case permSecret:
		p = mamori.NewSecretPermission(name, grantee)
	case permScript:
		p = mamori.NewScriptPermission(name, grantee)
	case permScriptFlow:
		p = mamori.NewScriptFlowPermission(name, grantee)
	default:
		return nil, fmt.Errorf("unknown permission type %q", m.Type.ValueString())
	}

	b := p.Base()
	b.WithGrantOption = m.WithGrantOption.ValueBool()
	from, until := m.ValidFrom.ValueString(), m.ValidUntil.ValueString()
	switch {
	case from != "" && until != "":
		b.Validity = mamori.ValidBetweenTimes(from, until)
	case from != "":
		b.Validity = mamori.ValidFromTime(from)
	case until != "":
		b.Validity = mamori.ValidUntilTime(until)
	}
	return p, nil
}

func (m *permissionModel) id() string {
	parts := []string{m.Grantee.ValueString(), m.Type.ValueString()}
	for _, v := range []types.String{m.Name, m.Datasource, m.Database, m.Schema, m.Object} {
		if !v.IsNull() {
			parts = append(parts, v.ValueString())
		}
	}
	return strings.Join(parts, ":")
}

func (r *permissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan permissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := plan.permission(ctx)
	if err == nil {
		_, err = r.client.Permissions.Grant(ctx, p)
	}
	if err != nil {
		addError(&resp.Diagnostics, "grant", "permission", err)
		return
	}
	plan.ID = types.StringValue(plan.id())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *permissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state permissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want, err := state.permission(ctx)
	if err != nil {
		addError(&resp.Diagnostics, "read", "permission", err)
		return
	}
	res, err := r.client.Permissions.List(ctx, state.Grantee.ValueString(), nil)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "permission", err)
		return
	}
	// The client cannot decode policy grant records, so they cannot be
	// verified; assume the grant is still there.
	if _, ok := want.(*mamori.PolicyPermission); ok {
		return
	}
	for _, rec := range res.Data {
		// Records of types the client does not know (role grants, ...) are
		// irrelevant here.
		g, err := mamori.PermissionFromRecord(rec)
		if err == nil && permissionMatches(want, g) {
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *permissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every attribute requires replacement, so there is nothing to update.
	var plan permissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *permissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state permissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := state.permission(ctx)
	if err == nil {
		_, err = r.client.Permissions.Revoke(ctx, p)
	}
	if err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "revoke", "permission", err)
	}
}

// permissionMatches reports whether the granted record g covers (part of)
// the permission want. Matching is deliberately loose: the server lists one
// record per privilege and its naming varies by permission type, so a
// permission is treated as present while any of its records still exists.
func permissionMatches(want, g mamori.Permission) bool {
	same := func(a, b string) bool {
		return strings.EqualFold(strings.Trim(a, `"`), strings.Trim(b, `"`))
	}
	switch w := want.(type) {
	case *mamori.DatasourcePermission:
		x, ok := g.(*mamori.DatasourcePermission)
		if !ok || (x.Datasource != "" && !same(x.Datasource, w.Datasource)) {
			return false
		}
		for _, a := range w.Permissions {
			for _, b := range x.Permissions {
				if strings.EqualFold(string(a), string(b)) {
					return true
				}
			}
		}
		return false
	case *mamori.MamoriPermission:
		x, ok := g.(*mamori.MamoriPermission)
		if !ok {
			return false
		}
		for _, a := range w.Permissions {
			for _, b := range x.Permissions {
				if strings.EqualFold(string(a), string(b)) {
					return true
				}
			}
		}
		return false
	case *mamori.CredentialPermission:
		x, ok := g.(*mamori.CredentialPermission)
		return ok && same(x.Datasource, w.Datasource) && (w.LoginName == "" || same(x.LoginName, w.LoginName))
	case *mamori.IPResourcePermission:
		x, ok := g.(*mamori.IPResourcePermission)
		return ok && same(x.Name, w.Name) && x.Unauthenticated == w.Unauthenticated
	}
	// Named permissions: same concrete type and object name.
	if fmt.Sprintf("%T", want) != fmt.Sprintf("%T", g) {
		return false
	}
	return same(permissionName(want), permissionName(g))
}

func permissionName(p mamori.Permission) string {
	switch x := p.(type) {
	case *mamori.PolicyPermission:
		return x.Name
	case *mamori.KeyPermission:
		return x.Name
	case *mamori.SSHLoginPermission:
		return x.Name
	case *mamori.SFTPLoginPermission:
		return x.Name
	case *mamori.RemoteDesktopLoginPermission:
		return x.Name
	case *mamori.HTTPResourcePermission:
		return x.Name
	case *mamori.SecretPermission:
		return x.Name
	case *mamori.ScriptPermission:
		return x.Name
	case *mamori.ScriptFlowPermission:
		return x.Name
	}
	return ""
}
