package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

var (
	_ resource.ResourceWithConfigure      = &remoteDesktopLoginResource{}
	_ resource.ResourceWithImportState    = &remoteDesktopLoginResource{}
	_ resource.ResourceWithValidateConfig = &remoteDesktopLoginResource{}
)

func NewRemoteDesktopLoginResource() resource.Resource { return &remoteDesktopLoginResource{} }

// remoteDesktopLoginResource manages an RDP or VNC login. Settings without an
// attribute (display tweaks such as font smoothing) keep the client defaults
// on create and the server's values on update.
type remoteDesktopLoginResource struct{ clientResource }

type remoteDesktopLoginModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Protocol      types.String `tfsdk:"protocol"`
	Host          types.String `tfsdk:"host"`
	Port          types.Int64  `tfsdk:"port"`
	RecordSession types.Bool   `tfsdk:"record_session"`
	LoginMode     types.String `tfsdk:"login_mode"`
	Username      types.String `tfsdk:"username"`
	Password      types.String `tfsdk:"password"`
	Width         types.Int64  `tfsdk:"width"`
	Height        types.Int64  `tfsdk:"height"`

	// RDP only.
	Domain             types.String `tfsdk:"domain"`
	Security           types.String `tfsdk:"security"`
	IgnoreCert         types.Bool   `tfsdk:"ignore_cert"`
	Console            types.Bool   `tfsdk:"console"`
	InitialProgram     types.String `tfsdk:"initial_program"`
	KeyboardLayout     types.String `tfsdk:"keyboard_layout"`
	ColorDepth         types.Int64  `tfsdk:"color_depth"`
	DisableCopy        types.Bool   `tfsdk:"disable_copy"`
	DisablePaste       types.Bool   `tfsdk:"disable_paste"`
	NormalizeClipboard types.String `tfsdk:"normalize_clipboard"`
}

// rdpOnlyAttrs are rejected when protocol is vnc.
var rdpOnlyAttrs = []string{"domain", "security", "ignore_cert", "console", "initial_program", "keyboard_layout", "color_depth", "disable_copy", "disable_paste", "normalize_clipboard"}

func (r *remoteDesktopLoginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_remote_desktop_login"
}

func (r *remoteDesktopLoginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Optional settings with server-side defaults: unset means "keep the
	// default", and the value actually stored is read back into state.
	str := func(desc string, v ...validator.String) schema.StringAttribute {
		return schema.StringAttribute{Description: desc, Optional: true, Computed: true, Validators: v,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	}
	num := func(desc string, v ...validator.Int64) schema.Int64Attribute {
		return schema.Int64Attribute{Description: desc, Optional: true, Computed: true, Validators: v,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}}
	}
	flag := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{Description: desc, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}}
	}
	rdp := " RDP only."
	resp.Schema = schema.Schema{
		Description: "A stored RDP or VNC login. Grant access with mamori_permission (type \"remote_desktop\").",
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
			"protocol": schema.StringAttribute{
				Description:   "rdp or vnc.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf("rdp", "vnc")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"host": schema.StringAttribute{Required: true},
			"port": num("Port to connect to. Defaults to 3389.", int64validator.Between(1, 65535)),
			"record_session": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"login_mode": schema.StringAttribute{
				Description: "How users authenticate: \"manual\" uses username/password, \"os\" lets the remote OS prompt, \"mamori\" has mamori prompt for credentials.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(string(mamori.RemoteDesktopLoginModeManual)),
				Validators:  []validator.String{stringvalidator.OneOf("manual", "os", "mamori")},
			},
			"username": schema.StringAttribute{Description: "Stored user, for login_mode manual.", Optional: true},
			"password": schema.StringAttribute{
				Description: "Stored password, for login_mode manual. Not read back from the server, so drift is not detected.",
				Optional:    true,
				Sensitive:   true,
			},
			"width":  num("Screen width in pixels. Defaults to 1024.", int64validator.AtLeast(1)),
			"height": num("Screen height in pixels. Defaults to 768.", int64validator.AtLeast(1)),

			"domain":          str("Windows domain of username." + rdp),
			"security":        str("Security mode: any, nla, nla-ext or rdp. Defaults to any."+rdp, stringvalidator.OneOf("any", "nla", "nla-ext", "rdp")),
			"ignore_cert":     flag("Ignore the server certificate. Defaults to true." + rdp),
			"console":         flag("Connect to the console session." + rdp),
			"initial_program": str("Program to start instead of the desktop." + rdp),
			"keyboard_layout": str("Server keyboard layout, e.g. en-us-qwerty, de-de-qwertz, failsafe. Defaults to en-us-qwerty." + rdp),
			"color_depth":     num("Bits per pixel: 8, 16 or 24. Defaults to 24."+rdp, int64validator.OneOf(8, 16, 24)),
			"disable_copy":    flag("Block copying from the remote desktop." + rdp),
			"disable_paste":   flag("Block pasting into the remote desktop." + rdp),
			"normalize_clipboard": str("Clipboard line endings: preserve, unix or windows. Defaults to windows."+rdp,
				stringvalidator.OneOf("preserve", "unix", "windows")),
		},
	}
}

func (r *remoteDesktopLoginResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m remoteDesktopLoginModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Protocol.ValueString() == "vnc" {
		for _, a := range rdpOnlyAttrs {
			var v attr.Value
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(a), &v)...)
			if v != nil && !v.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root(a), "RDP-only setting", a+" cannot be used with protocol vnc.")
			}
		}
	}
	if mode := m.LoginMode.ValueString(); mode != "" && mode != "manual" && !m.LoginMode.IsUnknown() {
		for _, a := range []string{"username", "password"} {
			var v attr.Value
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(a), &v)...)
			if v != nil && !v.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root(a), "Credentials not used", a+" is only used with login_mode manual.")
			}
		}
	}
}

// known reports whether a planned value was set (not null or unknown).
func known(v attr.Value) bool { return !v.IsNull() && !v.IsUnknown() }

// apply copies the planned settings onto l, leaving its other settings as
// they are.
func (m *remoteDesktopLoginModel) apply(l *mamori.RemoteDesktopLogin) {
	l.Name = m.Name.ValueString()
	l.SetProtocol(mamori.RemoteDesktopProtocol(m.Protocol.ValueString()))
	l.Record = m.RecordSession.ValueBool()

	port := 3389
	if l.RDP != nil && l.RDP.Port != 0 {
		port = l.RDP.Port
	} else if l.VNC != nil && l.VNC.Port != 0 {
		port = l.VNC.Port
	}
	if known(m.Port) {
		port = int(m.Port.ValueInt64())
	}
	l.At(m.Host.ValueString(), port)

	l.SetLoginMode(mamori.RemoteDesktopLoginMode(m.LoginMode.ValueString()))
	if m.LoginMode.ValueString() == string(mamori.RemoteDesktopLoginModeManual) {
		l.SetCredentials(m.Username.ValueString(), m.Password.ValueString(), m.Domain.ValueString())
	}

	if v := l.VNC; v != nil {
		if known(m.Width) {
			v.Width = int(m.Width.ValueInt64())
		}
		if known(m.Height) {
			v.Height = int(m.Height.ValueInt64())
		}
		return
	}
	o := l.RDP
	if known(m.Width) {
		o.Width = int(m.Width.ValueInt64())
	}
	if known(m.Height) {
		o.Height = int(m.Height.ValueInt64())
	}
	if known(m.Domain) {
		o.Domain = m.Domain.ValueString()
	}
	if known(m.Security) {
		o.Security = mamori.RemoteDesktopAuthenticationMode(m.Security.ValueString())
	}
	if known(m.IgnoreCert) {
		o.IgnoreCert = m.IgnoreCert.ValueBool()
	}
	if known(m.Console) {
		o.Console = m.Console.ValueBool()
	}
	if known(m.InitialProgram) {
		o.InitialProgram = m.InitialProgram.ValueString()
	}
	if known(m.KeyboardLayout) {
		o.ServerLayout = mamori.RemoteDesktopKeyboardLayout(m.KeyboardLayout.ValueString())
	}
	if known(m.ColorDepth) {
		o.ColorDepth = mamori.RemoteDesktopColorDepth(m.ColorDepth.ValueInt64())
	}
	if known(m.DisableCopy) {
		o.DisableCopy = m.DisableCopy.ValueBool()
	}
	if known(m.DisablePaste) {
		o.DisablePaste = m.DisablePaste.ValueBool()
	}
	if known(m.NormalizeClipboard) {
		o.NormalizeClipboard = mamori.RemoteDesktopClipboardMode(m.NormalizeClipboard.ValueString())
	}
}

// fill sets the state from the server's copy of the login. The password is
// left alone because the server does not return it.
func (m *remoteDesktopLoginModel) fill(l *mamori.RemoteDesktopLogin) {
	m.ID = types.StringValue(strconv.Itoa(l.ID))
	m.Protocol = types.StringValue(string(l.Protocol))
	m.RecordSession = types.BoolValue(l.Record)
	m.LoginMode = types.StringValue(string(l.LoginMode()))

	optString := func(s string) types.String {
		if s == "" {
			return types.StringNull()
		}
		return types.StringValue(s)
	}
	var user string
	if v := l.VNC; v != nil {
		m.Host, m.Port, user = types.StringValue(v.Hostname), types.Int64Value(int64(v.Port)), v.Username
		m.Width, m.Height = types.Int64Value(int64(v.Width)), types.Int64Value(int64(v.Height))
		m.Domain, m.Security, m.InitialProgram, m.KeyboardLayout, m.NormalizeClipboard =
			types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull()
		m.IgnoreCert, m.Console, m.DisableCopy, m.DisablePaste = types.BoolNull(), types.BoolNull(), types.BoolNull(), types.BoolNull()
		m.ColorDepth = types.Int64Null()
	} else {
		o := l.RDP
		m.Host, m.Port, user = types.StringValue(o.Hostname), types.Int64Value(int64(o.Port)), o.Username
		m.Width, m.Height = types.Int64Value(int64(o.Width)), types.Int64Value(int64(o.Height))
		m.Domain = types.StringValue(o.Domain)
		m.Security = types.StringValue(string(o.Security))
		m.IgnoreCert = types.BoolValue(o.IgnoreCert)
		m.Console = types.BoolValue(o.Console)
		m.InitialProgram = types.StringValue(o.InitialProgram)
		m.KeyboardLayout = types.StringValue(string(o.ServerLayout))
		m.ColorDepth = types.Int64Value(int64(o.ColorDepth))
		m.DisableCopy = types.BoolValue(o.DisableCopy)
		m.DisablePaste = types.BoolValue(o.DisablePaste)
		m.NormalizeClipboard = types.StringValue(string(o.NormalizeClipboard))
	}
	m.Username = optString(user)
}

func (r *remoteDesktopLoginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan remoteDesktopLoginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	l := mamori.NewRemoteDesktopLogin(plan.Name.ValueString(), mamori.RemoteDesktopProtocol(plan.Protocol.ValueString()))
	plan.apply(l)
	if _, err := r.client.RemoteDesktops.Create(ctx, l); err != nil {
		addError(&resp.Diagnostics, "create", "remote desktop login", err)
		return
	}
	got, err := r.client.RemoteDesktops.GetByName(ctx, l.Name)
	if err != nil {
		addError(&resp.Diagnostics, "read back", "remote desktop login", err)
		return
	}
	plan.fill(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *remoteDesktopLoginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state remoteDesktopLoginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	l, err := r.client.RemoteDesktops.GetByName(ctx, state.Name.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addError(&resp.Diagnostics, "read", "remote desktop login", err)
		return
	}
	state.fill(l)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *remoteDesktopLoginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan remoteDesktopLoginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Start from the server's copy so settings without an attribute survive.
	l, err := r.client.RemoteDesktops.GetByName(ctx, plan.Name.ValueString())
	if err != nil {
		addError(&resp.Diagnostics, "read", "remote desktop login", err)
		return
	}
	if l.ID < 0 {
		addError(&resp.Diagnostics, "update", "remote desktop login", fmt.Errorf("server did not report an id for %q", l.Name))
		return
	}
	plan.apply(l)
	if _, err := r.client.RemoteDesktops.Update(ctx, l); err != nil {
		addError(&resp.Diagnostics, "update", "remote desktop login", err)
		return
	}
	got, err := r.client.RemoteDesktops.GetByName(ctx, l.Name)
	if err != nil {
		addError(&resp.Diagnostics, "read back", "remote desktop login", err)
		return
	}
	plan.fill(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *remoteDesktopLoginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state remoteDesktopLoginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.RemoteDesktops.Delete(ctx, state.Name.ValueString()); err != nil && !isNotFound(err) {
		addError(&resp.Diagnostics, "delete", "remote desktop login", err)
	}
}

func (r *remoteDesktopLoginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
