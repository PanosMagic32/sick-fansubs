/**
 * The member-management visibility predicates — the client-side mirrors of
 * the server's peer matrix (reset/role/delete and status change). Pure
 * functions of the actor's session state and the loaded row, so the member
 * section stays wiring and each predicate is reviewable in one place.
 *
 * Pure VISIBILITY: the server enforces the floor, the peer rules, and the
 * CSRF/origin chain; mirroring them client-side keeps a well-meaning member
 * of staff from generating guaranteed-failure audited 403s.
 */

import type { SessionState } from "@features/auth/data-access/session.js";

import type { StaffUser, UserRole } from "../data-access/users-admin-api.js";

/** Accepted role values, in display order. */
export const ROLES: UserRole[] = ["user", "moderator", "admin", "super-admin"];

/**
 * The roles the admin transition policy may set (moderator ↔ user) —
 * display order. Super-admins pick from the full ROLES set; this is the
 * single source for the admin's reduced choice.
 */
const ADMIN_ROLE_CHOICES: UserRole[] = ["moderator", "user"];

/** True while the viewer may manage THIS row — reset its password, change
 * its role, or delete it. Super-admins act everywhere; admins only on
 * moderator/user rows. */
export function canManageRow(
  sessionState: SessionState | undefined,
  item: StaffUser,
): boolean {
  if (sessionState?.status !== "authenticated") return false;
  const actor = sessionState.user.role;
  if (actor === "super-admin") return true;
  return (
    actor === "admin" && (item.role === "moderator" || item.role === "user")
  );
}

/** True while the viewer may suspend/reactivate THIS row: super-admin
 * everywhere; admin on moderator/user rows; moderator on user rows only. */
export function canChangeStatusRow(
  sessionState: SessionState | undefined,
  item: StaffUser,
): boolean {
  if (sessionState?.status !== "authenticated") return false;
  const actor = sessionState.user.role;
  if (actor === "super-admin") return true;
  if (actor === "admin") {
    return item.role === "moderator" || item.role === "user";
  }
  return actor === "moderator" && item.role === "user";
}

/** The role options this viewer may set (the transition policy): a
 * super-admin picks from all four roles; an admin from moderator/user
 * only. */
export function roleOptions(
  sessionState: SessionState | undefined,
): UserRole[] {
  if (
    sessionState?.status === "authenticated" &&
    sessionState.user.role === "super-admin"
  ) {
    return ROLES;
  }
  return ADMIN_ROLE_CHOICES;
}
