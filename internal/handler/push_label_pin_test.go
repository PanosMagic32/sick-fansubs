package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// webLabelClampPattern extracts the web rename field's rune clamp. The field
// clamps drafts to the server's bound before a request leaves the browser; a
// drift would let the client accept or refuse names the server disagrees
// about, so the copy is pinned here (the push-kind pin precedent).
var webLabelClampPattern = regexp.MustCompile(`const MAX_DEVICE_NAME_RUNES = (\d+)`)

func TestPushLabelRuneBoundMatchesWebClamp(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(
		handlerRepoRoot(t), "web", "src", "features", "notifications", "ui", "push-devices-section.ts"))
	if err != nil {
		t.Fatalf("read web clamp source: %v", err)
	}
	m := webLabelClampPattern.FindSubmatch(source)
	if m == nil {
		t.Fatal("push-devices-section.ts must declare `MAX_DEVICE_NAME_RUNES = <n>`")
	}
	if got := string(m[1]); got != strconv.Itoa(maxPushLabelRunes) {
		t.Errorf("web clamp = %s, want maxPushLabelRunes = %d", got, maxPushLabelRunes)
	}
}
