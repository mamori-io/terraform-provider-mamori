package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	mamori "mamori.io/mamori-go-client"
)

// clientResource is embedded by every resource to receive the logged-in
// client from the provider.
type clientResource struct {
	client *mamori.Client
}

func (r *clientResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*mamori.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *mamori.Client, got %T", req.ProviderData))
		return
	}
	r.client = c
}

// isNotFound reports whether err means the remote object does not exist.
func isNotFound(err error) bool {
	return errors.Is(err, mamori.ErrNotFound) || mamori.StatusCode(err) == http.StatusNotFound
}

func addError(d *diag.Diagnostics, action, object string, err error) {
	d.AddError(fmt.Sprintf("Unable to %s %s", action, object), err.Error())
}

// rowString returns row[key] as a string, matching the key case-insensitively
// since query results are not consistent about column name case.
func rowString(row mamori.Row, key string) string {
	v, ok := row[key]
	if !ok {
		for k, x := range row {
			if strings.EqualFold(k, key) {
				v, ok = x, true
				break
			}
		}
	}
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

// diagError flattens error diagnostics into an error.
func diagError(d diag.Diagnostics) error {
	var errs []error
	for _, e := range d.Errors() {
		errs = append(errs, fmt.Errorf("%s: %s", e.Summary(), e.Detail()))
	}
	return errors.Join(errs...)
}
