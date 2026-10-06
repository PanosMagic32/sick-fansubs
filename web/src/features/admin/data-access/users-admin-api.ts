/**
 * Staff user-list endpoint — built on the shared transport like the other
 * data-access modules.
 *
 * Wire facts:
 *  - Floor: moderator+ session (the server rejects below-moderator with
 *    403 and unauthenticated with the generic 401).
 *  - email is admin+-only: the server OMITS the key for moderator
 *    viewers, so the wire type marks it optional and the UI renders it
 *    only when present (server-authoritative, never guessed client-side).
 *  - avatarUrl is NOT role-gated: it is always present and null when the
 *    account has no avatar.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

export type UserRole = "user" | "moderator" | "admin" | "super-admin";
export type UserStatus = "active" | "suspended";

export interface StaffUser {
  id: string;
  username: string;
  /** Present only for admin+ viewers. */
  email?: string;
  role: UserRole;
  status: UserStatus;
  /** Whether the account proved inbox control. */
  emailVerified: boolean;
  /** The ABSOLUTE served avatar URL, or null when the account has no avatar
   * (always on the wire, never role-gated, unlike email). */
  avatarUrl: string | null;
  createdAt: string;
}

export interface StaffUserList {
  items: StaffUser[];
  pageInfo: { hasNextPage: boolean; endCursor: string | null; total: number };
}

export interface ListStaffUsersParams {
  limit: number;
  after?: string;
  role?: UserRole;
  status?: UserStatus;
}

/** GET /api/v1/users — staff keyset list (moderator+, filters optional). */
export function listStaffUsers(
  params: ListStaffUsersParams,
  opts?: RequestOptions,
): Promise<StaffUserList> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  if (params.role) query.set("role", params.role);
  if (params.status) query.set("status", params.status);
  return request<StaffUserList>(
    "GET",
    `/users?${query.toString()}`,
    undefined,
    {
      signal: opts?.signal,
    },
  );
}

/**
 * The one-time plaintext temp password — the ONLY place the wire carries
 * a password in plaintext; the response is no-store and the client shows
 * it once.
 */
export interface ResetPasswordResponse {
  password: string;
}

/**
 * POST /api/v1/users/{id}/reset-password — admin manual reset (admin+
 * with peer protection). Requires CSRF (unsafe method). Returns the temp
 * password exactly once.
 */
export function resetUserPassword(
  id: string,
  opts?: RequestOptions,
): Promise<ResetPasswordResponse> {
  return request<ResetPasswordResponse>(
    "POST",
    `/users/${id}/reset-password`,
    undefined,
    {
      needsCSRF: true,
      signal: opts?.signal,
    },
  );
}

/**
 * PATCH /api/v1/users/{id}/role — peer-protected role change. Requires
 * CSRF (unsafe method). Returns the updated StaffUser projection — the
 * server's response is authoritative, including the admin+-only email. A
 * 409 carries problem type /problems/auth/last-super-admin (the last active
 * super-admin guard); a 422 carries violation field "role", code
 * "invalidValue".
 */
export function changeUserRole(
  id: string,
  role: UserRole,
  opts?: RequestOptions,
): Promise<StaffUser> {
  return request<StaffUser>(
    "PATCH",
    `/users/${id}/role`,
    { role },
    {
      needsCSRF: true,
      signal: opts?.signal,
    },
  );
}

/**
 * DELETE /api/v1/users/{id} — peer-protected hard delete. Requires CSRF.
 * 204 No Content on success; a 409 carries problem type
 * /problems/auth/last-super-admin. The content the account authored
 * survives (the server SET NULLs the creator refs).
 */
export function deleteUser(id: string, opts?: RequestOptions): Promise<void> {
  return request<void>("DELETE", `/users/${id}`, undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/**
 * POST /api/v1/users/{id}/suspend — peer-protected suspension. Requires
 * CSRF (unsafe method). Returns the updated StaffUser projection (status =
 * suspended). A 409 carries problem type /problems/auth/last-super-admin
 * (the last active super-admin guard); a same-status POST is a 200 no-op.
 * The floor is moderator+ with peer protection (moderators manage users
 * only).
 */
export function suspendUser(
  id: string,
  opts?: RequestOptions,
): Promise<StaffUser> {
  return request<StaffUser>("POST", `/users/${id}/suspend`, undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/**
 * POST /api/v1/users/{id}/reactivate — peer-protected reactivation.
 * Requires CSRF. Returns the updated StaffUser projection (status =
 * active). A same-status POST is a 200 no-op. The floor and peer protection
 * mirror the suspend endpoint.
 */
export function reactivateUser(
  id: string,
  opts?: RequestOptions,
): Promise<StaffUser> {
  return request<StaffUser>("POST", `/users/${id}/reactivate`, undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}
