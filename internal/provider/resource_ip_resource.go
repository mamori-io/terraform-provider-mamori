package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure   = &ipResourceResource{}
	_ resource.ResourceWithImportState = &ipResourceResource{}
)

func NewIPResourceResource() resource.Resource { return &ipResourceResource{} }

type ipResourceResource struct{ clientResource }

type ipResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	CIDR  types.String `tfsdk:"cidr"`
	Ports types.String `tfsdk:"ports"`
}

func (r *ipResourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_resource"
}

func (r *ipResourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named network range reachable through mamori. Grant access with mamori_permission (type \"ip_resource\").",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cidr": schema.StringAttribute{Description: "Address range, e.g. 10.0.200.0/24.", Required: true},
			"ports": schema.StringAttribute{
				Description: "Comma separated ports, e.g. \"443,80\". Empty means all ports.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
		},
	}
}

func (m *ipResourceModel) toResource() *mamori.IPResource {
	return &mamori.IPResource{Name: m.Name.ValueString(), CIDR: m.CIDR.ValueString(), Ports: m.Ports.ValueString()}
}

func (r *ipResourceResource) find(ctx context.Context, name string) (*mamori.IPResource, error) {
	res, err := r.client.IPResources.List(ctx, mamori.SearchOptions{Take: 10, Filter: mamori.Filters{mamori.F("name", mamori.FilterEquals, name)}})
	if err != nil {
		return nil, err
	}
	for i := range res.Data {
		if res.Data[i].Name == name {
			return &res.Data[i], nil
		}
	}
	return nil, mamori.ErrNotFound
}

func (r *ipResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.IPResources.Create(ctx, plan.toResource()); err != nil {
		addError(&resp.Diagnostics, "create", "IP resource", err)
		return
	}
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ipResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.find(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "IP resource", err)
		return
	}
	state.ID = state.Name
	state.CIDR = types.StringValue(res.CIDR)
	state.Ports = types.StringValue(res.Ports)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ipResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.IPResources.Update(ctx, plan.Name.ValueString(), plan.toResource()); err != nil {
		addError(&resp.Diagnostics, "update", "IP resource", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ipResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.IPResources.Delete(ctx, state.Name.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "IP resource", err)
	}
}

func (r *ipResourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
