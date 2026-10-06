package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure   = &httpResourceResource{}
	_ resource.ResourceWithImportState = &httpResourceResource{}
)

func NewHTTPResourceResource() resource.Resource { return &httpResourceResource{} }

type httpResourceResource struct{ clientResource }

type httpResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	URL            types.String `tfsdk:"url"`
	Description    types.String `tfsdk:"description"`
	ExcludeFromPAC types.Bool   `tfsdk:"exclude_from_pac"`
	RecordSession  types.Bool   `tfsdk:"record_session"`
}

func (r *httpResourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_http_resource"
}

func (r *httpResourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A web application proxied by mamori. Grant access with mamori_permission (type \"http_resource\").",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Server-assigned resource id.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"url": schema.StringAttribute{
				Description: "URL of the application. The host and port are derived from it.",
				Required:    true,
			},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"exclude_from_pac": schema.BoolAttribute{
				Description: "Leave the host out of the generated proxy auto-config file.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"record_session": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		},
	}
}

func (m *httpResourceModel) toResource() *mamori.HTTPResource {
	h := &mamori.HTTPResource{
		ID:             m.ID.ValueString(),
		Name:           m.Name.ValueString(),
		Description:    m.Description.ValueString(),
		ExcludeFromPAC: m.ExcludeFromPAC.ValueBool(),
		RecordSession:  m.RecordSession.ValueBool(),
	}
	h.SetURL(m.URL.ValueString())
	return h
}

func (r *httpResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan httpResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.HTTPResources.Create(ctx, plan.toResource()); err != nil {
		addError(&resp.Diagnostics, "create", "HTTP resource", err)
		return
	}
	got, err := r.client.HTTPResources.GetByName(ctx, plan.Name.ValueString())
	if err != nil {
		addError(&resp.Diagnostics, "read back", "HTTP resource", err)
		return
	}
	plan.ID = types.StringValue(got.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *httpResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state httpResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	h, err := r.client.HTTPResources.GetByName(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "HTTP resource", err)
		return
	}
	state.ID = types.StringValue(h.ID)
	state.URL = types.StringValue(h.URL)
	state.Description = types.StringValue(h.Description)
	state.ExcludeFromPAC = types.BoolValue(h.ExcludeFromPAC)
	state.RecordSession = types.BoolValue(h.RecordSession)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *httpResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan httpResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.HTTPResources.Update(ctx, plan.toResource()); err != nil {
		addError(&resp.Diagnostics, "update", "HTTP resource", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *httpResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state httpResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.HTTPResources.Delete(ctx, state.Name.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "HTTP resource", err)
	}
}

func (r *httpResourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
