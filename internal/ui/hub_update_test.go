package ui

import "testing"

func TestHubBuildDisablesUpdateCommandAndNudge(t *testing.T) {
	old := updateChecksEnabled
	SetUpdateChecksEnabled(false)
	t.Cleanup(func() { SetUpdateChecksEnabled(old) })

	h := &Home{}
	if cmd := h.checkForUpdate(); cmd != nil {
		t.Fatal("checkForUpdate returned network command while disabled")
	}
	if h.shouldRenderUpdateNudge() {
		t.Fatal("update nudge rendered while checks disabled")
	}
}
