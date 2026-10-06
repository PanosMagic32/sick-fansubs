/**
 * Role helper tests — the shared roleLabel map pins the role→Greek-label
 * contract and the unknown-role fallback (the raw identifier must never
 * reach the UI).
 */

import { describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";

import {
  isContentCreatorRole,
  isStaffRole,
  isSuperAdminRole,
  roleLabel,
} from "./roles.js";

describe("roles", () => {
  test("isStaffRole accepts moderator and above only", () => {
    expect(isStaffRole("user")).toBe(false);
    expect(isStaffRole("moderator")).toBe(true);
    expect(isStaffRole("admin")).toBe(true);
    expect(isStaffRole("super-admin")).toBe(true);
  });

  test("isSuperAdminRole accepts super-admin only (the logs floor)", () => {
    expect(isSuperAdminRole("user")).toBe(false);
    expect(isSuperAdminRole("moderator")).toBe(false);
    expect(isSuperAdminRole("admin")).toBe(false);
    expect(isSuperAdminRole("super-admin")).toBe(true);
    expect(isSuperAdminRole("")).toBe(false);
  });

  test("isContentCreatorRole accepts admin and above only", () => {
    expect(isContentCreatorRole("moderator")).toBe(false);
    expect(isContentCreatorRole("admin")).toBe(true);
    expect(isContentCreatorRole("super-admin")).toBe(true);
  });

  test("roleLabel maps every accepted role to the catalog copy", () => {
    expect(roleLabel("user")).toBe(el.account.roleUser);
    expect(roleLabel("moderator")).toBe(el.account.roleModerator);
    expect(roleLabel("admin")).toBe(el.account.roleAdmin);
    expect(roleLabel("super-admin")).toBe(el.account.roleSuperAdmin);
  });

  test("roleLabel returns null for a null snapshot and the fallback for unknown roles", () => {
    expect(roleLabel(null)).toBeNull();
    // Unknown values fall back to the Greek "unknown role" copy — never
    // the raw identifier.
    expect(roleLabel("owner")).toBe(el.account.roleUnknown);
  });
});
