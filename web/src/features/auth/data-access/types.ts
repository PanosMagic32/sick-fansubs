/**
 * Auth wire types — request/response DTOs matching the Go handler
 * contracts (internal/handler/auth.go, internal/identity/user.go).
 *
 * Feature types live in the
 * feature's data-access/ (the sketch's auth.types.ts). Generic problem
 * types stay in shared/api/types.ts — they belong to the transport.
 */

// ── Public user projection (match model.PublicUser in Go) ──────────────

/** Safe user projection returned in API responses (no password, no email). */
export interface PublicUser {
  id: string;
  username: string;
  role: string;
}

// ── Sign-in ───────────────────────────────────────────────────────────

export interface SignInRequest {
  identifier: string;
  password: string;
}

export interface SignInResponse {
  csrfToken: string;
  user: PublicUser;
  /**
   * Forced-change flag: true when the account carries the flag; gated
   * endpoints 403 until the password changes (the auth subtree stays
   * reachable) — the client routes to the change-password form proactively.
   */
  mustChangePassword: boolean;
}

// ── Registration ──────────────────────────────────────────────────────

export interface RegisterRequest {
  username: string;
  email: string;
  password: string;
}

/** Registration returns the same shape as sign-in (201 Created). */
export type RegisterResponse = SignInResponse;

// ── Session bootstrap ─────────────────────────────────────────────────

export interface SessionBootstrapResponse {
  authenticated: boolean;
  /** Present only when authenticated. */
  csrfToken?: string;
  /** null when unauthenticated. */
  user: PublicUser | null;
  /**
   * Forced-change flag — always present (false when
   * unauthenticated); true means the account must change its password.
   */
  mustChangePassword: boolean;
}

// ── Sign-out ──────────────────────────────────────────────────────────

export interface SignOutResponse {
  authenticated: false;
}

// ── Password change ───────────────────────────────────────────────────

export interface PasswordChangeRequest {
  currentPassword: string;
  newPassword: string;
}

/** Password change returns the same shape as sign-out. */
export type PasswordChangeResponse = SignOutResponse;

// ── Self-service password reset ───────────────────────────────────────

export interface ForgotPasswordRequest {
  /** Username or email (case-insensitive lookup). */
  identifier: string;
}

/** Always 200 with an empty object — enumeration-safe. */
export interface ForgotPasswordResponse {
  [key: string]: never;
}

export interface ResetPasswordRequest {
  /** The base64url token from the emailed link (43 chars). */
  token: string;
  password: string;
}

/** 204 No Content — resolves undefined through the shared transport. */
export type ResetPasswordResponse = undefined;

// ── Email verification ───────────────────────────────────────────────

export interface VerifyEmailRequest {
  /** The base64url token from the emailed link (43 chars). */
  token: string;
}

/** 204 No Content — resolves undefined through the shared transport. */
export type VerifyEmailResponse = undefined;

/** 204 No Content — the resend is an empty POST. */
export type ResendVerificationResponse = undefined;

// ── Email change ─────────────────────────────────────────────────────

export interface EmailChangeRequest {
  /** The new email address (lowercased server-side). */
  email: string;
  /** The current password — required re-authentication (the email is the recovery identifier). */
  currentPassword: string;
}

/** 200 — the refreshed profile (mirrors the avatar-upload shape). */
export type EmailChangeResponse = UserProfile;

// ── Current-user profile ──────────────────────────────────────────────

/**
 * Full read-only profile of the current user — GET /api/v1/users/me.
 * Mirrors the Go wire DTO internal/handler/users.go (UserProfile).
 */
export interface UserProfile {
  id: string;
  username: string;
  email: string;
  role: string;
  /**
   * Whether the account proved inbox control: true for
   * grandfathered imported accounts, verified links, and self-service
   * password resets; false for new registrations and fresh email changes.
   */
  emailVerified: boolean;
  /** Canonical UTC RFC 3339 instant with milliseconds. */
  createdAt: string;
  /**
   * ABSOLUTE served avatar URL (the server projects the stored path
   * through the served mapping), or null when no avatar is set.
   * Rendered directly as an img src.
   */
  avatarUrl: string | null;
}

// ── Favorites ────────────────────────────────────────────────────────

/**
 * One favorite list item — mirrors the Go wire DTO
 * internal/handler/favorites.go (favoriteItemDTO). The tabs separate the
 * content types, so the item carries no type discriminator.
 */
export interface FavoriteItem {
  id: string;
  title: string;
  /** The blog post's display subtitle; empty for projects. */
  subtitle: string;
  description: string;
  thumbnailUrl: string;
  publishedAt: string;
  favoritedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
}

export interface FavoriteListResponse {
  items: FavoriteItem[];
  pageInfo: {
    hasNextPage: boolean;
    endCursor: string | null;
    /** Filtered row count at read time — the pager's page-count numerator. */
    total: number;
  };
}

/** GET …/favorites/{kind}/{id} status envelope. */
export interface FavoriteStatusResponse {
  favorited: boolean;
}

/** The two favoritable content kinds (the URL segment names). */
export type FavoriteKind = "blog-posts" | "projects";

// ── Active sessions ───────────────────────────────────────────────────

/**
 * One active session — mirrors the Go wire DTO internal/handler/sessions.go
 * (sessionItemDTO). clientLabel is a bounded, closed-vocabulary device label
 * captured at sign-in, or null when the classifier recognized nothing.
 * createdAt/expiresAt are canonical UTC instants; current
 * marks the session making the request.
 */
export interface SessionItem {
  id: string;
  clientLabel: string | null;
  createdAt: string;
  expiresAt: string;
  current: boolean;
}

export interface SessionListResponse {
  items: SessionItem[];
  pageInfo: {
    hasNextPage: boolean;
    endCursor: string | null;
    /** Filtered row count at read time — the pager's page-count numerator. */
    total: number;
  };
}
