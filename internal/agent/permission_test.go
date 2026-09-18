package agent

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestPickPermissionRejectDoesNotAllow(t *testing.T) {
	params := acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow_once", Name: "Allow"},
			{Kind: acp.PermissionOptionKindRejectOnce, OptionId: "reject_once", Name: "Reject"},
		},
	}
	got, ok := pickPermission(params, false)
	if !ok || string(got.Outcome.Selected.OptionId) != "reject_once" {
		t.Fatalf("%+v ok=%v", got, ok)
	}
	_, ok = pickPermission(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{{Kind: acp.PermissionOptionKindAllowOnce, OptionId: "a", Name: "Allow"}},
	}, false)
	if ok {
		t.Fatal("reject must not fall back to allow")
	}
}
