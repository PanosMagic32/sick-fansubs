package identity

// Role values, mirroring the users.role CHECK and the API's public role
// strings. RoleWeight's hierarchy serves the draft-visibility gate only —
// capability checks stay explicit per operation below.
const (
	RoleUser       = "user"
	RoleModerator  = "moderator"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super-admin"
)

// RoleWeight returns the hierarchy position of a role, or -1 for an unknown
// value. The draft-visibility gate compares weights with a STRICT inequality
// (same-role peers never see each other's drafts).
func RoleWeight(role string) int {
	switch role {
	case RoleUser:
		return 0
	case RoleModerator:
		return 1
	case RoleAdmin:
		return 2
	case RoleSuperAdmin:
		return 3
	default:
		return -1
	}
}

// IsValidRole reports whether role is one of the accepted values.
func IsValidRole(role string) bool {
	return RoleWeight(role) >= 0
}

// CanCreateContent reports whether the role may create blog posts/projects
// (admin and above — moderators edit only).
func CanCreateContent(role string) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}

// CanModerateContent reports whether the role may use the content-moderation
// surface: the staff content reads, content updates, and staff comment
// deletion (moderator and above). Content deletion is CanDeleteContent,
// which requires admin+.
func CanModerateContent(role string) bool {
	return RoleWeight(role) >= RoleWeight(RoleModerator)
}

// CanDeleteContent reports whether the role may hard-delete blog posts or
// projects (admin and above — moderators edit only; the moderator-delete
// option stays open for a later ruling).
func CanDeleteContent(role string) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}

// CanUploadMedia reports whether the role may upload media objects
// (admin and above).
func CanUploadMedia(role string) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}

// CanDeleteMedia reports whether the role may explicitly delete media
// objects (the same admin+ floor as uploads — media lifecycle is an admin
// workflow).
func CanDeleteMedia(role string) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}

// CanViewStaffList reports whether the role may open the staff user list
// (moderator and above — moderators suspend/reactivate users, so they need
// the list to find them).
func CanViewStaffList(role string) bool {
	return RoleWeight(role) >= RoleWeight(RoleModerator)
}

// CanViewStaffEmails reports whether the role may see email addresses in the
// staff user list (admin and above — a moderator's page omits the email
// field entirely, it is not nulled).
func CanViewStaffEmails(role string) bool {
	return RoleWeight(role) >= RoleWeight(RoleAdmin)
}

// CanViewMetrics reports whether the role may read the staff dashboard
// metrics (moderator and above — the same floor as the dashboard tabs and
// the staff user list; the aggregates carry no per-role visibility masking).
func CanViewMetrics(role string) bool {
	return RoleWeight(role) >= RoleWeight(RoleModerator)
}

// CanViewLogs reports whether the role may read the application log file and
// the audit-event browser (super-admin only). The floor is
// deliberately narrower than CanViewMetrics: those surfaces carry client
// addresses and forensic detail, not aggregates.
func CanViewLogs(role string) bool {
	return role == RoleSuperAdmin
}

// CanResetPassword reports whether actorRole may reset passwords for
// targetRole: super-admins may reset anyone including themselves; admins
// may reset moderators and users only — never other admins or super-admins.
// Everyone else is below the floor.
func CanResetPassword(actorRole, targetRole string) bool {
	switch actorRole {
	case RoleSuperAdmin:
		return true
	case RoleAdmin:
		return targetRole == RoleModerator || targetRole == RoleUser
	default:
		return false
	}
}

// CanChangeRole reports whether actorRole may change targetRole to newRole
// (the complete transition policy):
//
//	super-admin: any target (incl. self) → any of the four roles
//	admin: moderator/user targets → moderator ↔ user only
//	everyone else: no role changes
//
// The caller validates newRole against IsValidRole first; this predicate is
// purely the actor/target/newRole transition edge. The last-active-super-admin
// guard is a store-level check — it needs the target's live status and the
// active super-admin count, which only the transaction can see.
func CanChangeRole(actorRole, targetRole, newRole string) bool {
	switch actorRole {
	case RoleSuperAdmin:
		return true
	case RoleAdmin:
		return (targetRole == RoleModerator || targetRole == RoleUser) &&
			(newRole == RoleModerator || newRole == RoleUser)
	default:
		return false
	}
}

// CanDeleteUser reports whether actorRole may delete targetRole's account:
// super-admins may delete anyone including themselves; admins may delete
// moderators and users only. The predicate mirrors CanResetPassword, but is
// written explicitly so a future change to reset policy cannot silently
// change deletion policy.
func CanDeleteUser(actorRole, targetRole string) bool {
	switch actorRole {
	case RoleSuperAdmin:
		return true
	case RoleAdmin:
		return targetRole == RoleModerator || targetRole == RoleUser
	default:
		return false
	}
}

// CanChangeUserStatus reports whether actorRole may suspend or reactivate
// targetRole's account: the one user-management capability below the admin
// floor — "moderators can suspend/reactivate users but cannot change roles".
// Super-admins manage anyone including themselves; admins manage moderators
// and users; moderators manage users only. The last-active-super-admin guard
// is a store-level check: it needs the target's live status and the active
// super-admin count, which only the write transaction can see.
func CanChangeUserStatus(actorRole, targetRole string) bool {
	switch actorRole {
	case RoleSuperAdmin:
		return true
	case RoleAdmin:
		return targetRole == RoleModerator || targetRole == RoleUser
	case RoleModerator:
		return targetRole == RoleUser
	default:
		return false
	}
}
