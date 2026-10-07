package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure   = &requestableResourceResource{}
	_ resource.ResourceWithImportState = &requestableResourceResource{}
)

func NewRequestableResourceResource() resource.Resource { return &requestableResourceResource{} }

// requestableResourceResource lets a user or role request access to a
// resource through an on-demand policy. It is identified by type, grantee,
// resource name and login; the policy, privileges and description can change
// in place.
type requestableResourceResource struct{ clientResource }

type requestableResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ResourceType  types.String `tfsdk:"resource_type"`
	ResourceName  types.String `tfsdk:"resource_name"`
	ResourceLogin types.String `tfsdk:"resource_login"`
	Grantee       types.String `tfsdk:"grantee"`
	PolicyName    types.String `tfsdk:"policy_name"`
	Privileges    types.String `tfsdk:"privileges"`
	Description   types.String `tfsdk:"description"`
}

// requestableTypes maps resource_type values onto the client's types.
var requestableTypes = map[string]mamori.RequestableResourceType{
	"datasource":     mamori.RequestableResourceTypeDatasource,
	"http_resource":  mamori.RequestableResourceTypeHTTPResource,
	"remote_desktop": mamori.RequestableResourceTypeRemoteDesktop,
	"secret":         mamori.RequestableResourceTypeSecret,
	"ip_resource":    mamori.RequestableResourceTypeIPResource,
	"ssh_login":      mamori.RequestableResourceTypeSSHLogin,
	"encryption_key": mamori.RequestableResourceTypeEncryptionKey,
	"resource_group": mamori.RequestableResourceTypeResourceGroup,
	"script":         mamori.RequestableResourceTypeScript,
	"script_flow":    mamori.RequestableResourceTypeScriptFlow,
}

func requestableTypeNames() []string {
	return slices.Sorted(maps.Keys(requestableTypes))
}

func (r *requestableResourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_requestable_resource"
}

func (r *requestableResourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Makes a resource requestable by a user or role through an on-demand policy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Server-assigned id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"resource_type": schema.StringAttribute{
				Description:   "Kind of resource: " + strings.Join(requestableTypeNames(), ", ") + ".",
				Required:      true,
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf(requestableTypeNames()...)},
			},
			"resource_name": schema.StringAttribute{
				Description:   "Name of the resource, e.g. the SSH login name.",
				Required:      true,
				PlanModifiers: replace,
			},
			"resource_login": schema.StringAttribute{
				Description:   "Login on the resource, for datasources.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				PlanModifiers: replace,
			},
			"grantee": schema.StringAttribute{
				Description:   "User or role that may request the resource.",
				Required:      true,
				PlanModifiers: replace,
			},
			"policy_name": schema.StringAttribute{
				Description: "On-demand policy requests go through.",
				Required:    true,
			},
			"privileges": schema.StringAttribute{
				Description:   "Comma-separated privileges granted on approval. Defaults to the type's usual ones (e.g. \"SSH,SFTP\" for ssh_login).",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
		},
	}
}

func (m *requestableResourceModel) toResource() *mamori.RequestableResource {
	return &mamori.RequestableResource{
		ID:            m.ID.ValueString(),
		ResourceType:  requestableTypes[m.ResourceType.ValueString()],
		ResourceName:  m.ResourceName.ValueString(),
		ResourceLogin: m.ResourceLogin.ValueString(),
		Grantee:       m.Grantee.ValueString(),
		PolicyName:    m.PolicyName.ValueString(),
		Privileges:    m.Privileges.ValueString(),
		Description:   m.Description.ValueString(),
	}
}

// find returns the server record with m's type, grantee, name and login.
func (r *requestableResourceResource) find(ctx context.Context, m *requestableResourceModel) (*mamori.RequestableResource, error) {
	q := mamori.RequestableResourceQuery{
		Type:     requestableTypes[m.ResourceType.ValueString()],
		Grantee:  m.Grantee.ValueString(),
		Resource: m.ResourceName.ValueString(),
	}
	// The login is matched here rather than in the query, since an empty
	// query field means "any".
	res, err := r.client.RequestableResources.ListFor(ctx, 0, 100, q)
	if err != nil {
		return nil, err
	}
	for i := range res.Data {
		if res.Data[i].ResourceLogin == m.ResourceLogin.ValueString() {
			return &res.Data[i], nil
		}
	}
	return nil, mamori.ErrNotFound
}

func (m *requestableResourceModel) fill(rr *mamori.RequestableResource) {
	m.ID = types.StringValue(rr.ID)
	if rr.PolicyName != "" {
		m.PolicyName = types.StringValue(rr.PolicyName)
	}
	if rr.Privileges != "" {
		m.Privileges = types.StringValue(rr.Privileges)
	}
	m.Description = types.StringValue(rr.Description)
}

func (r *requestableResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan requestableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rr := plan.toResource()
	if _, err := r.client.RequestableResources.Create(ctx, rr); err != nil {
		addError(&resp.Diagnostics, "create", "requestable resource", err)
		return
	}
	// Create fills in default privileges.
	plan.Privileges = types.StringValue(rr.Privileges)
	got, err := r.find(ctx, &plan)
	if err != nil {
		addError(&resp.Diagnostics, "read back", "requestable resource", err)
		return
	}
	plan.ID = types.StringValue(got.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *requestableResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state requestableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.find(ctx, &state)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "requestable resource", err)
		return
	}
	state.fill(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *requestableResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state requestableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	rr := plan.toResource()
	if rr.Privileges == "" {
		rr.Privileges = rr.ResourceType.DefaultPrivileges()
	}
	if _, err := r.client.RequestableResources.Update(ctx, rr); err != nil {
		addError(&resp.Diagnostics, "update", "requestable resource", err)
		return
	}
	plan.Privileges = types.StringValue(rr.Privileges)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *requestableResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state requestableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.RequestableResources.Delete(ctx, state.ID.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "requestable resource", err)
	}
}

// ImportState takes "resource_type:grantee:resource_name"; the name may itself
// contain colons. resource_login is taken as empty.
func (r *requestableResourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected resource_type:grantee:resource_name, got %q", req.ID))
		return
	}
	if _, ok := requestableTypes[parts[0]]; !ok {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("unknown resource_type %q", parts[0]))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("grantee"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_name"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_login"), "")...)
}
