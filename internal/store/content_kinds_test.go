package store

import "testing"

// TestContentKindDescriptors pins the invariants every reader relies on:
// allContentKinds holds the two declared kinds in order, every field is
// populated, and the kinds share no name.
func TestContentKindDescriptors(t *testing.T) {
	t.Parallel()

	if len(allContentKinds) != 2 || allContentKinds[0].kind != BlogContent.kind || allContentKinds[1].kind != ProjectContent.kind {
		t.Fatalf("allContentKinds = %+v, want the two declared kinds in order", allContentKinds)
	}

	fields := map[string]func(ContentKind) string{
		"kind":          func(k ContentKind) string { return k.kind },
		"label":         func(k ContentKind) string { return k.label },
		"noun":          func(k ContentKind) string { return k.noun },
		"ownColumn":     func(k ContentKind) string { return k.ownColumn },
		"table":         func(k ContentKind) string { return k.table },
		"comments":      func(k ContentKind) string { return k.comments },
		"commentFK":     func(k ContentKind) string { return k.commentFK },
		"hearts":        func(k ContentKind) string { return k.hearts },
		"favorites":     func(k ContentKind) string { return k.favorites },
		"favoriteFK":    func(k ContentKind) string { return k.favoriteFK },
		"downloads":     func(k ContentKind) string { return k.downloads },
		"downloadFK":    func(k ContentKind) string { return k.downloadFK },
		"downloadLabel": func(k ContentKind) string { return k.downloadLabel },
		"searchIndex":   func(k ContentKind) string { return k.searchIndex },
	}
	for name, field := range fields {
		blog, project := field(BlogContent), field(ProjectContent)
		if blog == "" || project == "" {
			t.Errorf("%s: empty for blog (%q) or project (%q)", name, blog, project)
		}
		if blog == project {
			t.Errorf("%s: %q is shared by both kinds — each kind owns its names", name, blog)
		}
	}

	if !BlogContent.hasSubtitle || ProjectContent.hasSubtitle {
		t.Errorf("hasSubtitle = (%v, %v), want (true, false): the blog own column is the display subtitle",
			BlogContent.hasSubtitle, ProjectContent.hasSubtitle)
	}
}
