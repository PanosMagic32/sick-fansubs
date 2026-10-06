package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store/storetest"
)

// Project content-administration store tests:
// create/index atomicity, slug uniqueness, the published_at_ms transition,
// revision guards, cascades, and the draft-visibility gate — the
// blog-admin store test's counterpart for projects.

func setupProjectAdminStore(t *testing.T) *sql.DB {
	t.Helper()
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Three users: an admin (weight 2), a super-admin (3), a moderator (1).
	for _, u := range []storetest.UserSpec{
		{ID: "admin1", Username: "Admin", Role: "admin"},
		{ID: "sa1", Username: "SA", Role: "super-admin"},
		{ID: "mod1", Username: "Mod", Role: "moderator"},
		{ID: "user1", Username: "User"},
	} {
		storetest.InsertUser(t, db, u)
	}
	return db
}

func createProject(t *testing.T, db *sql.DB, id, slug, status string, publishedMS int64) *StaffProject {
	t.Helper()
	project, _, err := CreateProject(t.Context(), db, CreateProjectParams{
		ID:            id,
		Title:         "Title " + id,
		Description:   "Desc " + id,
		Slug:          slug,
		ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:        status,
		CreatorID:     "admin1",
		UpdaterID:     "admin1",
		PublishedAtMS: publishedMS,
		NowMS:         5000,
	})
	if err != nil {
		t.Fatalf("CreateProject(%s): %v", id, err)
	}
	return project
}

func projectFTSRowCount(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM project_search s JOIN projects p ON p.rowid = s.rowid WHERE p.id = ?`, id,
	).Scan(&n); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	return n
}

func TestCreateProject_PublishedIndexesAtomically(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)

	project := createProject(t, db, "p1", "p1", "published", 5000)

	if project.Revision != 1 {
		t.Errorf("revision = %d, want 1", project.Revision)
	}
	if project.PublishedAtMS == nil || *project.PublishedAtMS != 5000 {
		t.Errorf("published_at_ms = %v, want 5000", project.PublishedAtMS)
	}
	if project.Slug != "p1" {
		t.Errorf("slug = %q, want p1", project.Slug)
	}
	if n := projectFTSRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (published rows index atomically)", n)
	}
}

func TestCreateProject_PublishedWithDownloadsIndexesAtomically(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)

	_, _, err := CreateProject(t.Context(), db, CreateProjectParams{
		ID:            "p1",
		Title:         "T",
		Description:   "D",
		Slug:          "p1",
		ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:        "published",
		CreatorID:     "admin1",
		UpdaterID:     "admin1",
		PublishedAtMS: 5000,
		NowMS:         5000,
		Downloads: []Download{
			{Label: "Batch", MagnetLink: dlLink("magnet:?xt=one")},
			{Label: "Batch 2", MagnetLink: dlLink("magnet:?xt=two")},
		},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// The index row is keyed to the CONTENT row's rowid even when download
	// rows were inserted first: with two downloads the failing capture reads
	// the LAST download's rowid and the join finds nothing.
	if n := projectFTSRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (the content rowid keys the index, not the downloads)", n)
	}
}

func TestCreateProject_DraftStaysOutOfTheIndex(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)

	project := createProject(t, db, "d1", "d1", "draft", 0)

	if project.PublishedAtMS != nil {
		t.Errorf("published_at_ms = %v, want nil for a draft", *project.PublishedAtMS)
	}
	if n := projectFTSRowCount(t, db, "d1"); n != 0 {
		t.Errorf("fts rows = %d, want 0 (drafts stay out of the index)", n)
	}
}

func TestCreateProject_SlugTaken(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "p1", "taken", "published", 5000)

	// A taken slug is ErrSlugTaken — the create caller's retry signal.
	_, _, err := CreateProject(ctx, db, CreateProjectParams{
		ID: "p2", Title: "T", Description: "", Slug: "taken",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", CreatorID: "admin1", UpdaterID: "admin1",
		PublishedAtMS: 6000, NowMS: 6000,
	})
	if !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("err = %v, want ErrSlugTaken", err)
	}

	// An ID collision is NOT narrowed to ErrSlugTaken (the primary key is a
	// different constraint — swallowing it as a slug collision would make
	// the caller retry suffixes forever).
	_, _, err = CreateProject(ctx, db, CreateProjectParams{
		ID: "p1", Title: "T", Description: "", Slug: "fresh",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", CreatorID: "admin1", UpdaterID: "admin1",
		PublishedAtMS: 6000, NowMS: 6000,
	})
	if err == nil || errors.Is(err, ErrSlugTaken) {
		t.Fatalf("err = %v, want a non-ErrSlugTaken failure for the id collision", err)
	}
}

func TestUpdateProject_PublishesAndReindexes(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "d1", "d1", "draft", 0)

	project, _, err := UpdateProject(ctx, db, "d1", 1, UpdateProjectParams{
		Title: "New Title", Description: "New desc", Slug: "d1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "admin1", NowMS: 9000,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if project.Status != "published" || project.Revision != 2 {
		t.Errorf("project = status %s revision %d, want published/2", project.Status, project.Revision)
	}
	// The first transition into published stamps the publish time.
	if project.PublishedAtMS == nil || *project.PublishedAtMS != 9000 {
		t.Errorf("published_at_ms = %v, want the transition stamp 9000", project.PublishedAtMS)
	}
	if n := projectFTSRowCount(t, db, "d1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 after publishing", n)
	}

	// The re-publish path preserves the original stamp and reindexes the
	// NEW text.
	project, _, err = UpdateProject(ctx, db, "d1", 2, UpdateProjectParams{
		Title: "Updated Again", Description: "Second desc", Slug: "d1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "sa1", NowMS: 12000,
	})
	if err != nil {
		t.Fatalf("UpdateProject (re-publish): %v", err)
	}
	if project.PublishedAtMS == nil || *project.PublishedAtMS != 9000 {
		t.Errorf("published_at_ms = %v, want the ORIGINAL stamp 9000 preserved", project.PublishedAtMS)
	}
	var text string
	if err := db.QueryRow(
		`SELECT s.text FROM project_search s JOIN projects p ON p.rowid = s.rowid WHERE p.id = 'd1'`,
	).Scan(&text); err != nil {
		t.Fatalf("read fts text: %v", err)
	}
	if !strings.Contains(text, "Updated") || !strings.Contains(text, "Again") {
		t.Errorf("fts text = %q, want the UPDATED title indexed", text)
	}
}

func TestUpdateProject_LeavingPublishedRemovesFavoritesKeepsStampAndIndex(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "p1", "p1", "published", 5000)
	if _, err := db.Exec(
		`INSERT INTO project_favorites (user_id, project_id, created_at_ms) VALUES ('user1', 'p1', 6000)`); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	project, _, err := UpdateProject(ctx, db, "p1", 1, UpdateProjectParams{
		Title: "T", Description: "", Slug: "p1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "archived", UpdaterID: "mod1", NowMS: 7000,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if project.PublishedAtMS == nil || *project.PublishedAtMS != 5000 {
		t.Errorf("published_at_ms = %v, want the stamp preserved on archive", project.PublishedAtMS)
	}
	var favs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_favorites WHERE project_id = 'p1'`).Scan(&favs); err != nil {
		t.Fatalf("count favorites: %v", err)
	}
	if favs != 0 {
		t.Errorf("favorites = %d, want 0 after leaving published (the favorites cascade)", favs)
	}
	// The archived row KEEPS its stamp, and the index membership is
	// stamp-based — the FTS row survives; the query-time
	// status filter hides it from search.
	if n := projectFTSRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (stamp preserved → index kept; status filters at query time)", n)
	}
}

func TestUpdateProject_SlugChangeAndTaken(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "p1", "one", "published", 5000)
	createProject(t, db, "p2", "two", "published", 5000)

	project, _, err := UpdateProject(ctx, db, "p1", 1, UpdateProjectParams{
		Title: "T", Description: "", Slug: "renamed",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "admin1", NowMS: 7000,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if project.Slug != "renamed" || project.Revision != 2 {
		t.Errorf("project = slug %s revision %d, want renamed/2", project.Slug, project.Revision)
	}

	// Moving onto another row's slug is ErrSlugTaken (the handler maps it
	// to 422 {slug, alreadyTaken}).
	_, _, err = UpdateProject(ctx, db, "p1", 2, UpdateProjectParams{
		Title: "T", Description: "", Slug: "two",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "admin1", NowMS: 8000,
	})
	if !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("err = %v, want ErrSlugTaken", err)
	}

	// Keeping the row's own slug is not a collision.
	project, _, err = UpdateProject(ctx, db, "p1", 2, UpdateProjectParams{
		Title: "T", Description: "", Slug: "renamed",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "admin1", NowMS: 8000,
	})
	if err != nil {
		t.Fatalf("UpdateProject (own slug): %v", err)
	}
	if project.Slug != "renamed" {
		t.Errorf("slug = %q, want renamed preserved", project.Slug)
	}
}

func TestUpdateProject_StaleRevisionConflicts(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)

	createProject(t, db, "p1", "p1", "published", 5000)

	_, _, err := UpdateProject(t.Context(), db, "p1", 99, UpdateProjectParams{
		Title: "T", Description: "", Slug: "p1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "mod1", NowMS: 7000,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a stale revision", err)
	}

	_, _, err = UpdateProject(t.Context(), db, "missing", 1, UpdateProjectParams{
		Title: "T", Description: "", Slug: "m",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "mod1", NowMS: 7000,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an unknown id", err)
	}
}

func TestUpdateProject_ConflictKeepsDownloads(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "p1", "p1", "published", 5000)
	if _, err := db.Exec(
		`INSERT INTO project_downloads (id, project_id, name, magnet_link, position, created_at_ms)
		 VALUES ('old1', 'p1', 'Batch', 'magnet:?xt=old', 0, 5000)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	_, _, err := UpdateProject(ctx, db, "p1", 99, UpdateProjectParams{
		Title:        "T",
		Description:  "D",
		Slug:         "p1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "admin1",
		NowMS:        9000,
		Downloads:    []Download{{Label: "Remastered", MagnetLink: dlLink("magnet:?xt=new")}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}

	// The stale write touched NOTHING — the old row is intact (the blog
	// store's mirror pin).
	var got string
	if err := db.QueryRow(`SELECT name FROM project_downloads WHERE project_id = 'p1'`).Scan(&got); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if got != "Batch" {
		t.Errorf("name = %q, want the untouched Batch row", got)
	}
}

func TestDeleteProject_Cascades(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "p1", "p1", "published", 5000)
	for _, q := range []string{
		`INSERT INTO project_favorites (user_id, project_id, created_at_ms) VALUES ('user1', 'p1', 6000)`,
		`INSERT INTO project_downloads (id, project_id, name, position, created_at_ms)
		 VALUES ('dl1', 'p1', 'Batch', 0, 6000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed cascade rows: %v", err)
		}
	}

	if err := DeleteProject(ctx, db, "p1", 1); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM projects WHERE id = 'p1'`,
		`SELECT COUNT(*) FROM project_favorites WHERE project_id = 'p1'`,
		`SELECT COUNT(*) FROM project_downloads WHERE project_id = 'p1'`,
	} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count after delete: %v", err)
		}
		if n != 0 {
			t.Errorf("q %s: count = %d, want 0 (hard-delete cascades)", q, n)
		}
	}
	if n := projectFTSRowCount(t, db, "p1"); n != 0 {
		t.Errorf("fts rows = %d, want 0 after delete", n)
	}

	if err := DeleteProject(ctx, db, "p1", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_StaleRevisionConflicts(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)

	createProject(t, db, "p1", "p1", "published", 5000)
	if err := DeleteProject(t.Context(), db, "p1", 42); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a stale revision", err)
	}
	// The row survived the conflict.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = 'p1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count = %d, want 1 after the conflicted delete", n)
	}
}

// TestStaffProjectDraftVisibility pins the draft-visibility gate matrix for
// projects (the same creatorRoleWeightCase the blog gate uses — pinned
// against the model by the blog's canary test).
func TestStaffProjectDraftVisibility(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "draft1", "draft1", "draft", 0)
	createProject(t, db, "pub1", "pub1", "published", 5000)

	cases := []struct {
		name     string
		viewerID string
		weight   int // identity.RoleWeight(viewer role)
		projID   string
		wantOK   bool
	}{
		{"creator sees own draft", "admin1", 2, "draft1", true},
		{"super-admin sees admin draft", "sa1", 3, "draft1", true},
		{"other admin excluded", "admin2", 2, "draft1", false},
		{"moderator excluded", "mod1", 1, "draft1", false},
		{"user excluded", "user1", 0, "draft1", false},
		{"published visible to moderator", "mod1", 1, "pub1", true},
		{"unknown id masked", "sa1", 3, "nope", false},
	}
	// The "other admin" case needs a second admin row.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "admin2", Username: "Admin2", Role: "admin",
	})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project, err := GetStaffProject(ctx, db, c.projID, c.viewerID, c.weight)
			if c.wantOK {
				if err != nil {
					t.Fatalf("GetStaffProject: %v (want the row)", err)
				}
				if project.ID != c.projID {
					t.Errorf("project id = %q, want %q", project.ID, c.projID)
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want masked ErrNotFound", err)
			}
		})
	}
}

// TestStaffProject_NullCreatorUpdater is the project counterpart of
// TestStaffBlogPost_NullCreatorUpdater: migrated rows carry NULL ids and
// the staff detail must read them without a Scan error.
func TestStaffProject_NullCreatorUpdater(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	cases := []struct {
		name        string
		id          string
		status      string
		publishedMS int64
		creator     any
		updater     any
	}{
		{"published-null-both", "mig-pub", "published", 5000, nil, nil},
		{"draft-null-both", "mig-draft", "draft", 0, nil, nil},
		{"published-null-creator", "mig-mixed", "published", 5000, nil, "admin1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := db.Exec(
				`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
					creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
				 VALUES (?, 'Migrated', '', ?, 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
				 	?, ?, ?, NULLIF(?, 0), 1000, 1000, 1)`, c.id, c.id, c.status, c.creator, c.updater, c.publishedMS); err != nil {
				t.Fatalf("seed migrated project: %v", err)
			}

			project, err := GetStaffProject(ctx, db, c.id, "mod1", 1)
			if err != nil {
				t.Fatalf("GetStaffProject(%s): %v (NULL ids must not fail the scan)", c.id, err)
			}
			if c.creator == nil && (project.CreatorID != "" || project.Creator != nil) {
				t.Errorf("project = %+v, want empty CreatorID and nil Creator for a NULL creator", project)
			}
			if c.updater == nil && (project.UpdaterID != "" || project.Updater != nil) {
				t.Errorf("project = %+v, want empty UpdaterID and nil Updater for a NULL updater", project)
			}
			if c.creator != nil && (project.CreatorID != "admin1" || project.Creator == nil) {
				t.Errorf("project = %+v, want the stamped creator", project)
			}
		})
	}
}

// TestUpdateProject_MigratedRow is the project counterpart of
// TestUpdateBlogPost_MigratedRow: the pre-read + revision-guarded update
// must work on a NULL-creator migrated row.
func TestUpdateProject_MigratedRow(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	if _, err := db.Exec(
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		 VALUES ('mig1', 'Migrated', '', 'mig1', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 	'published', NULL, NULL, 5000, 1000, 1000, 1)`); err != nil {
		t.Fatalf("seed migrated project: %v", err)
	}

	if _, err := GetStaffProject(ctx, db, "mig1", "mod1", 1); err != nil {
		t.Fatalf("staff pre-read: %v", err)
	}

	project, _, err := UpdateProject(ctx, db, "mig1", 1, UpdateProjectParams{
		Title:        "Migrated",
		Description:  "",
		Slug:         "mig1",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "mod1",
		NowMS:        9000,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if project.Revision != 2 || project.UpdaterID != "mod1" {
		t.Errorf("project = revision %d updater %q, want 2 / mod1", project.Revision, project.UpdaterID)
	}
}

// TestListStaffProjects_Filters is the projects half of the staff content
// filter pin: the filter composes with the visibility gate
// on the second content table too — the two lists share the rule, not the
// query.
func TestListStaffProjects_Filters(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	for _, p := range []struct {
		id, title, slug, status string
	}{
		{"pr1", "One Piece", "one-piece", "published"},
		{"pr2", "Άμλετ", "amlet", "draft"},
		{"pr3", "Dr. Stone", "dr-stone", "published"},
	} {
		if _, _, err := CreateProject(ctx, db, CreateProjectParams{
			ID:            p.id,
			Title:         p.title,
			Description:   "",
			Slug:          p.slug,
			ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
			Status:        p.status,
			CreatorID:     "admin1",
			UpdaterID:     "admin1",
			PublishedAtMS: 5000,
			NowMS:         5000,
		}); err != nil {
			t.Fatalf("seed %s: %v", p.id, err)
		}
	}

	// The status filter narrows to the draft, and the folded title filter
	// finds it by an accent-free, lowercase needle.
	projects, _, err := ListStaffProjects(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{Status: "draft"})
	if err != nil {
		t.Fatalf("status filter: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "pr2" {
		t.Fatalf("status=draft = %+v, want [pr2]", projects)
	}

	projects, _, err = ListStaffProjects(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{Query: "αμλετ"})
	if err != nil {
		t.Fatalf("query filter: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "pr2" {
		t.Fatalf("q=αμλετ = %+v, want [pr2]", projects)
	}

	// The query is split at word boundaries before folding, so a space in the
	// query reaches a punctuation-separated title (the revised match rule —
	// the blog store's twin case).
	projects, _, err = ListStaffProjects(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{Query: "dr stone"})
	if err != nil {
		t.Fatalf("word-boundary filter: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "pr3" {
		t.Fatalf("q=dr stone = %+v, want [pr3]", projects)
	}

	// Every word must be present: the first word matching is not enough.
	projects, _, err = ListStaffProjects(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{Query: "dr house"})
	if err != nil {
		t.Fatalf("all-words filter: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("q=dr house = %+v, want no match", projects)
	}

	// The gate still runs under the filter: the moderator cannot see the
	// admin-created draft even though the title matches.
	projects, _, err = ListStaffProjects(ctx, db, 10, nil, "mod1", 1, StaffContentFilter{Query: "αμλετ"})
	if err != nil {
		t.Fatalf("moderator filter: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("moderator saw %d rows, want 0", len(projects))
	}
}

func TestListStaffProjects_KeysetAndGate(t *testing.T) {
	t.Parallel()
	db := setupProjectAdminStore(t)
	ctx := t.Context()

	createProject(t, db, "a", "a", "published", 1000)
	createProject(t, db, "b", "b", "draft", 0)
	createProject(t, db, "c", "c", "published", 3000)

	// The moderator sees only the published rows, newest-updated first
	// (all updated at 5000 — id ASC is the tie-breaker).
	projects, hasNext, err := ListStaffProjects(ctx, db, 10, nil, "mod1", 1, StaffContentFilter{})
	if err != nil {
		t.Fatalf("ListStaffProjects: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false for 2 rows")
	}
	if len(projects) != 2 || projects[0].ID != "a" || projects[1].ID != "c" {
		t.Fatalf("projects = %+v, want [a c] (published only, id ASC tie-breaker)", projects)
	}

	// The super-admin sees all three (the draft's creator is an admin).
	projects, _, err = ListStaffProjects(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{})
	if err != nil {
		t.Fatalf("ListStaffProjects: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("super-admin sees %d rows, want 3", len(projects))
	}

	// Keyset: page size 1, second page starts after the first row.
	page1, hasNext, err := ListStaffProjects(ctx, db, 1, nil, "sa1", 3, StaffContentFilter{})
	if err != nil || !hasNext || len(page1) != 1 {
		t.Fatalf("page1 = %+v hasNext %v err %v, want 1 row + next", page1, hasNext, err)
	}
	page2, _, err := ListStaffProjects(ctx, db, 1,
		&AdminPageKey{UpdatedAtMS: page1[0].UpdatedAtMS, ID: page1[0].ID}, "sa1", 3, StaffContentFilter{})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID == page1[0].ID {
		t.Fatalf("page2 = %+v, want the next distinct row", page2)
	}
}
