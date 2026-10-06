package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.ResourceWithConfigure   = &roleGrantResource{}
	_ resource.ResourceWithImportState = &roleGrantResource{}
)

func NewRoleGrantResource() resource.Resource { return &roleGrantResource{} }

// roleGrantResource grants a role to a user or another role. Every attribute
// forces replacement: a grant is revoked and re-granted rather than edited.
type roleGrantResource struct{ clientResource }

type roleGrantModel struct {
	ID          types.String `tfsdk:"id"`
	Role        types.String `tfsdk:"role"`
	Grantee     types.String `tfsdk:"grantee"`
	AdminOption types.Bool   `tfsdk:"with_admin_option"`
}

func (r *roleGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_grant"
}

func (r *roleGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Grants a role to a user or role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "role:grantee",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role":    schema.StringAttribute{Required: true, PlanModifiers: replace},
			"grantee": schema.StringAttribute{Description: "User or role receiving the role.", Required: true, PlanModifiers: replace},
			"with_admin_option": schema.BoolAttribute{
				Description:   "Allow the grantee to grant the role on.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *roleGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Roles.GrantTo(ctx, plan.Role.ValueString(), plan.Grantee.ValueString(), plan.AdminOption.ValueBool(), nil); err != nil {
		addError(&resp.Diagnostics, "grant", "role", err)
		return
	}
	plan.ID = types.StringValue(plan.Role.ValueString() + ":" + plan.Grantee.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *roleGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rows, err := r.client.Roles.GetGrantees(ctx, state.Role.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "role grant", err)
		return
	}
	for _, row := range rows {
		if strings.EqualFold(rowString(row, "grantee"), state.Grantee.ValueString()) {
			if state.AdminOption.IsNull() {
				state.AdminOption = types.BoolValue(strings.EqualFold(rowString(row, "withadminoption"), "Y"))
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *roleGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every attribute requires replacement, so there is nothing to update.
	var plan roleGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *roleGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Roles.RevokeFrom(ctx, state.Role.ValueString(), state.Grantee.ValueString(), nil); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "revoke", "role", err)
	}
}

func (r *roleGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, grantee, ok := strings.Cut(req.ID, ":")
	if !ok || role == "" || grantee == "" {
		resp.Diagnostics.AddError("Invalid import id", "Expected role:grantee, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), role)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("grantee"), grantee)...)
}
