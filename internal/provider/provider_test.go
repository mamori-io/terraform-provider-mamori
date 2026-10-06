package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	mamori "mamori.io/mamori-go-client"
)

func TestResourceSchemas(t *testing.T) {
	ctx := context.Background()
	p := New("test")()
	for _, f := range p.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "mamori"}, &meta)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: schema: %v", meta.TypeName, resp.Diagnostics)
		}
		if d := resp.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Errorf("%s: invalid schema: %v", meta.TypeName, d)
		}
	}
}

func TestPermissionGrantOptions(t *testing.T) {
	ctx := context.Background()
	privs, _ := types.SetValueFrom(ctx, types.StringType, []string{"SELECT"})
	m := permissionModel{
		Grantee:         types.StringValue("bob"),
		Type:            types.StringValue(permDatasource),
		Name:            types.StringNull(),
		Privileges:      privs,
		Datasource:      types.StringValue("pg"),
		Database:        types.StringValue("*"),
		Schema:          types.StringNull(),
		Object:          types.StringNull(),
		WhereClause:     types.StringNull(),
		RowLimit:        types.StringValue("none"),
		Unauthenticated: types.BoolValue(false),
		WithGrantOption: types.BoolValue(false),
		ValidFrom:       types.StringNull(),
		ValidUntil:      types.StringValue("2027-01-01 00:00"),
	}
	p, err := m.permission(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := p.GrantOptions()
	if o["object_name"] != `"pg".*` || o["limit"] != "none" || o["valid_until"] != "2027-01-01 00:00" {
		t.Errorf("unexpected grant options %v", o)
	}
	if got := m.id(); got != "bob:datasource:pg:*" {
		t.Errorf("id = %q", got)
	}
}

func TestPermissionMatches(t *testing.T) {
	rec := func(p mamori.Params) mamori.Permission {
		g, err := mamori.PermissionFromRecord(p)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	tests := []struct {
		name string
		want mamori.Permission
		got  mamori.Permission
		ok   bool
	}{
		{"secret", mamori.NewSecretPermission("s1", "bob"),
			rec(mamori.Params{"permissiontype": "REVEAL SECRET", "grantee": "bob", "key_name": `"S1"`}), true},
		{"secret other name", mamori.NewSecretPermission("s1", "bob"),
			rec(mamori.Params{"permissiontype": "REVEAL SECRET", "grantee": "bob", "key_name": "s2"}), false},
		{"ssh vs sftp", mamori.NewSSHLoginPermission("l", "bob"),
			rec(mamori.Params{"permissiontype": "SFTP", "grantee": "bob", "key_name": "l"}), false},
		{"ip unauthenticated differs", mamori.NewIPResourcePermission("n", "bob"),
			rec(mamori.Params{"permissiontype": "UNAUTHENTICATED IP USAGE", "grantee": "bob", "key_name": "n"}), false},
		{"mamori", mamori.NewMamoriPermission("bob", mamori.MamoriPrivilegeCreateUser, mamori.MamoriPrivilegeDropUser),
			rec(mamori.Params{"permissiontype": "DROP USER", "grantee": "bob"}), true},
		{"datasource", mamori.NewDatasourcePermission("bob", mamori.DBPermissionSelect).On("pg", "*", "", ""),
			rec(mamori.Params{"permissiontype": "SELECT", "grantee": "bob", "datasource": "pg"}), true},
		{"datasource other ds", mamori.NewDatasourcePermission("bob", mamori.DBPermissionSelect).On("pg", "*", "", ""),
			rec(mamori.Params{"permissiontype": "SELECT", "grantee": "bob", "datasource": "ora"}), false},
	}
	for _, tt := range tests {
		if got := permissionMatches(tt.want, tt.got); got != tt.ok {
			t.Errorf("%s: permissionMatches = %v, want %v", tt.name, got, tt.ok)
		}
	}
}

func TestRemoteDesktopLoginApplyFill(t *testing.T) {
	m := remoteDesktopLoginModel{
		Name:               types.StringValue("rd"),
		Protocol:           types.StringValue("rdp"),
		Host:               types.StringValue("10.0.0.20"),
		Port:               types.Int64Unknown(),
		RecordSession:      types.BoolValue(false),
		LoginMode:          types.StringValue("manual"),
		Username:           types.StringValue("ops"),
		Password:           types.StringValue("pw"),
		Width:              types.Int64Unknown(),
		Height:             types.Int64Value(900),
		Domain:             types.StringValue("CORP"),
		Security:           types.StringValue("nla"),
		IgnoreCert:         types.BoolUnknown(),
		Console:            types.BoolUnknown(),
		InitialProgram:     types.StringUnknown(),
		KeyboardLayout:     types.StringUnknown(),
		ColorDepth:         types.Int64Unknown(),
		DisableCopy:        types.BoolValue(true),
		DisablePaste:       types.BoolUnknown(),
		NormalizeClipboard: types.StringUnknown(),
	}
	l := mamori.NewRemoteDesktopLogin("rd", mamori.RemoteDesktopProtocolRDP)
	m.apply(l)
	o := l.RDP
	if o.Hostname != "10.0.0.20" || o.Port != 3389 || o.Username != "ops" || o.Password != "pw" || o.Domain != "CORP" ||
		!o.CredentialsRequired || o.Height != 900 || o.Width != 1024 || o.Security != "nla" || !o.DisableCopy || l.Record {
		t.Fatalf("unexpected login %+v record=%v", o, l.Record)
	}

	l.ID = 7
	var got remoteDesktopLoginModel
	got.fill(l)
	if got.ID.ValueString() != "7" || got.LoginMode.ValueString() != "manual" || got.Port.ValueInt64() != 3389 ||
		got.KeyboardLayout.ValueString() != "en-us-qwerty" || got.Username.ValueString() != "ops" || !got.Password.IsNull() {
		t.Errorf("unexpected state %+v", got)
	}

	v := mamori.NewRemoteDesktopLogin("v", mamori.RemoteDesktopProtocolVNC)
	got.fill(v)
	if !got.Domain.IsNull() || !got.ColorDepth.IsNull() || got.Protocol.ValueString() != "vnc" || got.LoginMode.ValueString() != "os" {
		t.Errorf("unexpected vnc state %+v", got)
	}
}
