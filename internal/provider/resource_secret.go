package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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
	_ resource.ResourceWithConfigure   = &secretResource{}
	_ resource.ResourceWithImportState = &secretResource{}
)

func NewSecretResource() resource.Resource { return &secretResource{} }

type secretResource struct{ clientResource }

type secretModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Protocol    types.String `tfsdk:"protocol"`
	Secret      types.String `tfsdk:"secret"`
	Parts       types.List   `tfsdk:"parts"`
	Username    types.String `tfsdk:"username"`
	Hostname    types.String `tfsdk:"hostname"`
	Description types.String `tfsdk:"description"`
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optionalString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Description: desc, Optional: true, Computed: true, Default: stringdefault.StaticString("")}
	}
	resp.Schema = schema.Schema{
		Description: "A secret stored in the mamori vault. Grant access with mamori_permission (type \"secret\").",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Server-assigned secret id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"protocol": schema.StringAttribute{
				Description: "One of \"\" (generic), \"db\", \"ssh\" or \"rdp\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Validators:  []validator.String{stringvalidator.OneOf("", "db", "ssh", "rdp")},
			},
			"secret": schema.StringAttribute{
				Description: "Secret value. The server does not return it, so drift in the value is not detected.",
				Optional:    true,
				Sensitive:   true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("secret"), path.MatchRoot("parts")),
				},
			},
			"parts": schema.ListAttribute{
				Description: "Names of the secrets making up a multi-part secret. Use instead of secret.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"username":    optionalString("User name the secret belongs to."),
			"hostname":    optionalString("Host the secret is for."),
			"description": optionalString(""),
		},
	}
}

func (m *secretModel) toSecret(ctx context.Context) (*mamori.Secret, error) {
	s := mamori.NewSecret(mamori.SecretProtocol(m.Protocol.ValueString()), m.Name.ValueString())
	s.ID = m.ID.ValueString()
	s.Secret = m.Secret.ValueString()
	s.Username = m.Username.ValueString()
	s.Hostname = m.Hostname.ValueString()
	s.Description = m.Description.ValueString()
	if !m.Parts.IsNull() && !m.Parts.IsUnknown() {
		s.Parts = []string{}
		if d := m.Parts.ElementsAs(ctx, &s.Parts, false); d.HasError() {
			return nil, diagError(d)
		}
	}
	return s, nil
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := plan.toSecret(ctx)
	if err != nil {
		addError(&resp.Diagnostics, "create", "secret", err)
		return
	}
	if _, err := r.client.Secrets.Create(ctx, s); err != nil {
		addError(&resp.Diagnostics, "create", "secret", err)
		return
	}
	// The create response is loosely typed; look the secret up to get its id.
	got, err := r.client.Secrets.GetByName(ctx, s.Name)
	if err != nil {
		addError(&resp.Diagnostics, "read back", "secret", err)
		return
	}
	plan.ID = types.StringValue(got.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Secrets.GetByName(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "secret", err)
		return
	}
	state.ID = types.StringValue(s.ID)
	state.Protocol = types.StringValue(string(s.Protocol))
	state.Username = types.StringValue(s.Username)
	state.Hostname = types.StringValue(s.Hostname)
	state.Description = types.StringValue(s.Description)
	if s.Type == mamori.SecretTypeMultiSecret {
		parts, d := types.ListValueFrom(ctx, types.StringType, s.Parts)
		resp.Diagnostics.Append(d...)
		state.Parts = parts
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := plan.toSecret(ctx)
	if err == nil {
		_, err = r.client.Secrets.Update(ctx, s)
	}
	if err != nil {
		addError(&resp.Diagnostics, "update", "secret", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Secrets.Delete(ctx, state.ID.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "secret", err)
	}
}

func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
