package identity

import "testing"

func TestRoleWeight(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		role string
		want int
	}{
		{"user", RoleUser, 0},
		{"moderator", RoleModerator, 1},
		{"admin", RoleAdmin, 2},
		{"super-admin", RoleSuperAdmin, 3},
		{"unknown role", "bogus", -1},
		{"empty role", "", -1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RoleWeight(tt.role); got != tt.want {
				t.Errorf("RoleWeight(%q) = %d, want %d", tt.role, got, tt.want)
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	// Complete capability matrix: create = admin+,
	// moderate (edit + staff reads + comment staff-delete) = moderator+,
	// content delete = admin+ (moderators edit only),
	// upload = admin+, media delete = admin+,
	// staff-list read = moderator+, staff email = admin+,
	// IsValidRole = the four accepted roles only.
	cases := []struct {
		name        string
		role        string
		create      bool
		mod         bool
		delete      bool
		upload      bool
		deleteMedia bool
		staffList   bool
		staffEmails bool
		validRole   bool
	}{
		{name: "user", role: RoleUser, validRole: true},
		{name: "moderator", role: RoleModerator, mod: true, staffList: true, validRole: true},
		{name: "admin", role: RoleAdmin, create: true, mod: true, delete: true, upload: true, deleteMedia: true, staffList: true, staffEmails: true, validRole: true},
		{name: "super-admin", role: RoleSuperAdmin, create: true, mod: true, delete: true, upload: true, deleteMedia: true, staffList: true, staffEmails: true, validRole: true},
		{name: "unknown role", role: "bogus"},
		{name: "empty role", role: ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanCreateContent(tt.role); got != tt.create {
				t.Errorf("CanCreateContent(%q) = %v, want %v", tt.role, got, tt.create)
			}
			if got := CanModerateContent(tt.role); got != tt.mod {
				t.Errorf("CanModerateContent(%q) = %v, want %v", tt.role, got, tt.mod)
			}
			if got := CanDeleteContent(tt.role); got != tt.delete {
				t.Errorf("CanDeleteContent(%q) = %v, want %v", tt.role, got, tt.delete)
			}
			if got := CanUploadMedia(tt.role); got != tt.upload {
				t.Errorf("CanUploadMedia(%q) = %v, want %v", tt.role, got, tt.upload)
			}
			if got := CanDeleteMedia(tt.role); got != tt.deleteMedia {
				t.Errorf("CanDeleteMedia(%q) = %v, want %v", tt.role, got, tt.deleteMedia)
			}
			if got := CanViewStaffList(tt.role); got != tt.staffList {
				t.Errorf("CanViewStaffList(%q) = %v, want %v", tt.role, got, tt.staffList)
			}
			if got := CanViewStaffEmails(tt.role); got != tt.staffEmails {
				t.Errorf("CanViewStaffEmails(%q) = %v, want %v", tt.role, got, tt.staffEmails)
			}
			if got := IsValidRole(tt.role); got != tt.validRole {
				t.Errorf("IsValidRole(%q) = %v, want %v", tt.role, got, tt.validRole)
			}
		})
	}
}

func TestCanViewMetrics(t *testing.T) {
	t.Parallel()

	// The metrics floor is moderator+ — the same floor as
	// the dashboard tabs and the staff user list.
	cases := []struct {
		name string
		role string
		want bool
	}{
		{"user below the floor", RoleUser, false},
		{"moderator", RoleModerator, true},
		{"admin", RoleAdmin, true},
		{"super-admin", RoleSuperAdmin, true},
		{"unknown role", "bogus", false},
		{"empty role", "", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanViewMetrics(tt.role); got != tt.want {
				t.Errorf("CanViewMetrics(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestCanViewLogs(t *testing.T) {
	t.Parallel()

	// The log viewer and the audit browser are super-admin
	// only — narrower than the metrics floor, because those surfaces carry
	// client addresses and forensic detail rather than aggregates.
	cases := []struct {
		name string
		role string
		want bool
	}{
		{"user below the floor", RoleUser, false},
		{"moderator below the floor", RoleModerator, false},
		{"admin below the floor", RoleAdmin, false},
		{"super-admin", RoleSuperAdmin, true},
		{"unknown role", "bogus", false},
		{"empty role", "", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanViewLogs(tt.role); got != tt.want {
				t.Errorf("CanViewLogs(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestCanResetPassword(t *testing.T) {
	t.Parallel()

	// Reset peer matrix: super-admin resets anyone (incl. self);
	// admin resets moderator/user only; everyone else is below the floor.
	cases := []struct {
		name          string
		actor, target string
		want          bool
	}{
		{"super-admin resets user", RoleSuperAdmin, RoleUser, true},
		{"super-admin resets moderator", RoleSuperAdmin, RoleModerator, true},
		{"super-admin resets admin", RoleSuperAdmin, RoleAdmin, true},
		{"super-admin resets super-admin (peer or self)", RoleSuperAdmin, RoleSuperAdmin, true},
		{"admin resets user", RoleAdmin, RoleUser, true},
		{"admin resets moderator", RoleAdmin, RoleModerator, true},
		{"admin resets admin (peer — denied)", RoleAdmin, RoleAdmin, false},
		{"admin resets super-admin (denied)", RoleAdmin, RoleSuperAdmin, false},
		{"moderator below the floor", RoleModerator, RoleUser, false},
		{"user below the floor", RoleUser, RoleUser, false},
		{"bogus actor", "bogus", RoleUser, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanResetPassword(tt.actor, tt.target); got != tt.want {
				t.Errorf("CanResetPassword(%q, %q) = %v, want %v", tt.actor, tt.target, got, tt.want)
			}
		})
	}
}

func TestCanChangeRole(t *testing.T) {
	t.Parallel()

	// Transition policy: super-admin → any target, any role;
	// admin → moderator/user targets, moderator ↔ user only; everyone else
	// never. newRole validity is the caller's concern (IsValidRole) — this
	// predicate is the transition edge only.
	cases := []struct {
		name                   string
		actor, target, newRole string
		want                   bool
	}{
		{"super-admin: user → moderator", RoleSuperAdmin, RoleUser, RoleModerator, true},
		{"super-admin: moderator → admin", RoleSuperAdmin, RoleModerator, RoleAdmin, true},
		{"super-admin: admin → super-admin", RoleSuperAdmin, RoleAdmin, RoleSuperAdmin, true},
		{"super-admin: self admin → user", RoleSuperAdmin, RoleSuperAdmin, RoleUser, true},
		{"super-admin: peer super-admin → admin", RoleSuperAdmin, RoleSuperAdmin, RoleAdmin, true},
		{"admin: user → moderator", RoleAdmin, RoleUser, RoleModerator, true},
		{"admin: moderator → user", RoleAdmin, RoleModerator, RoleUser, true},
		{"admin: user → admin (promotion denied)", RoleAdmin, RoleUser, RoleAdmin, false},
		{"admin: moderator → super-admin (denied)", RoleAdmin, RoleModerator, RoleSuperAdmin, false},
		{"admin: admin → user (peer denied)", RoleAdmin, RoleAdmin, RoleUser, false},
		{"admin: super-admin target (denied)", RoleAdmin, RoleSuperAdmin, RoleUser, false},
		{"moderator: never", RoleModerator, RoleUser, RoleModerator, false},
		{"user: never", RoleUser, RoleUser, RoleUser, false},
		{"bogus actor", "bogus", RoleUser, RoleModerator, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanChangeRole(tt.actor, tt.target, tt.newRole); got != tt.want {
				t.Errorf("CanChangeRole(%q, %q, %q) = %v, want %v",
					tt.actor, tt.target, tt.newRole, got, tt.want)
			}
		})
	}
}

func TestCanDeleteUser(t *testing.T) {
	t.Parallel()

	// Deletion peer matrix (mirrors the reset matrix deliberately):
	// super-admin deletes anyone (incl. self); admin deletes
	// moderator/user only.
	cases := []struct {
		name          string
		actor, target string
		want          bool
	}{
		{"super-admin deletes user", RoleSuperAdmin, RoleUser, true},
		{"super-admin deletes moderator", RoleSuperAdmin, RoleModerator, true},
		{"super-admin deletes admin", RoleSuperAdmin, RoleAdmin, true},
		{"super-admin deletes super-admin (peer or self)", RoleSuperAdmin, RoleSuperAdmin, true},
		{"admin deletes user", RoleAdmin, RoleUser, true},
		{"admin deletes moderator", RoleAdmin, RoleModerator, true},
		{"admin deletes admin (peer — denied)", RoleAdmin, RoleAdmin, false},
		{"admin deletes super-admin (denied)", RoleAdmin, RoleSuperAdmin, false},
		{"moderator below the floor", RoleModerator, RoleUser, false},
		{"user below the floor", RoleUser, RoleUser, false},
		{"bogus actor", "bogus", RoleUser, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanDeleteUser(tt.actor, tt.target); got != tt.want {
				t.Errorf("CanDeleteUser(%q, %q) = %v, want %v", tt.actor, tt.target, got, tt.want)
			}
		})
	}
}

func TestCanChangeUserStatus(t *testing.T) {
	t.Parallel()

	// Status matrix: super-admin suspends/reactivates anyone
	// (incl. self); admin manages moderator/user; moderator manages user only
	// — the one user-management capability below the admin floor.
	cases := []struct {
		name          string
		actor, target string
		want          bool
	}{
		{"super-admin manages user", RoleSuperAdmin, RoleUser, true},
		{"super-admin manages moderator", RoleSuperAdmin, RoleModerator, true},
		{"super-admin manages admin", RoleSuperAdmin, RoleAdmin, true},
		{"super-admin manages super-admin (peer or self)", RoleSuperAdmin, RoleSuperAdmin, true},
		{"admin manages user", RoleAdmin, RoleUser, true},
		{"admin manages moderator", RoleAdmin, RoleModerator, true},
		{"admin manages admin (peer — denied)", RoleAdmin, RoleAdmin, false},
		{"admin manages super-admin (denied)", RoleAdmin, RoleSuperAdmin, false},
		{"moderator manages user", RoleModerator, RoleUser, true},
		{"moderator manages moderator (peer — denied)", RoleModerator, RoleModerator, false},
		{"moderator manages admin (denied)", RoleModerator, RoleAdmin, false},
		{"user below the floor", RoleUser, RoleUser, false},
		{"bogus actor", "bogus", RoleUser, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CanChangeUserStatus(tt.actor, tt.target); got != tt.want {
				t.Errorf("CanChangeUserStatus(%q, %q) = %v, want %v", tt.actor, tt.target, got, tt.want)
			}
		})
	}
}
