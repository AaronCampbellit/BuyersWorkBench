package httpapi

import (
	"strings"
	"testing"
)

func TestCursorIsBoundToOpaqueSession(t *testing.T) {
	cursor := pageCursor{Workspace: "workspace", Generation: 4, View: "priorities", Filter: "active", After: "subject"}
	token := sealCursor(cursor, "opaque-server-session-a")
	decoded, err := openCursor(token, "opaque-server-session-a")
	if err != nil || decoded != cursor {
		t.Fatal("cursor failed roundtrip")
	}
	if _, err = openCursor(token, "other-user-session"); err == nil {
		t.Fatal("cross-session cursor accepted")
	}
	if _, err = openCursor(strings.Repeat("x", 2049), "opaque-server-session-a"); err == nil {
		t.Fatal("unbounded cursor accepted")
	}
	parts := strings.Split(token, ".")
	if _, err = openCursor(parts[0]+".AAAA", "opaque-server-session-a"); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}
