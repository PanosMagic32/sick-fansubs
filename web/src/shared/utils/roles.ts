/**
 * Client-side role predicates and the role→Greek-label map, shared by the
 * account page, the admin users tab, and the notifications page.
 *
 * These are VISIBILITY/display helpers only — the server enforces the real
 * authorization (role checks + CSRF + origin + the draft gate).
 */

import { el } from "@shared/catalog/el.js";

/** Moderator, admin, or super-admin — the content edit/delete surface. */
export function isStaffRole(role: string): boolean {
  return role === "moderator" || role === "admin" || role === "super-admin";
}

/** Super-admin only — the log viewer and the audit browser. Narrower
 * than isStaffRole by design: those surfaces carry client addresses and
 * forensic detail, not aggregates. */
export function isSuperAdminRole(role: string): boolean {
  return role === "super-admin";
}

/** Admin or super-admin — content creation and media uploads. */
export function isContentCreatorRole(role: string): boolean {
  return role === "admin" || role === "super-admin";
}

/** The accepted role identifiers → Greek display labels (the catalog's
 * el.account.role* keys are the single source of the copy). */
const ROLE_LABELS: Record<string, string> = {
  "super-admin": el.account.roleSuperAdmin,
  admin: el.account.roleAdmin,
  moderator: el.account.roleModerator,
  user: el.account.roleUser,
};

/**
 * The Greek display label for a role identifier, or null when the role is
 * null/absent (the notifications item's nullable snapshot — no chip).
 * An unknown non-null value returns the catalog's roleUnknown fallback
 * (defense in depth: roles are CHECK-constrained in the schema, so the
 * raw identifier must never reach the UI).
 */
export function roleLabel(role: null): null;
export function roleLabel(role: string): string;
export function roleLabel(role: string | null): string | null;
export function roleLabel(role: string | null): string | null {
  if (role === null) return null;
  return ROLE_LABELS[role] ?? el.account.roleUnknown;
}
