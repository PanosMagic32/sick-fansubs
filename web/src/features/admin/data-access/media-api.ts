/**
 * Media upload endpoint — POST /api/v1/media
 * via the shared requestForm transport.
 *
 * Wire facts:
 *  - The request is multipart/form-data with a single "file" part; the
 *    browser sets Content-Type (the JSON transport must not).
 *  - CSRF is always attached (the endpoint is POST-only and unsafe).
 *  - Success: 201 { path, url } — `path` is the storage-relative value the
 *    create form submits as `thumbnailPath` (never hand-built).
 *  - 413 = size/pixel bound; 422 = undecodable bytes / missing file.
 */

import {
  request,
  requestForm,
  type RequestOptions,
} from "@shared/api/client.js";

/** The 201 response body. */
export interface MediaUploadResult {
  path: string;
  url: string;
}

/** POST /api/v1/media — upload + process one image (admin+). */
export function uploadMedia(
  file: File,
  opts?: RequestOptions,
): Promise<MediaUploadResult> {
  const form = new FormData();
  form.append("file", file, file.name);
  return requestForm<MediaUploadResult>("/media", form, {
    signal: opts?.signal,
  });
}

/**
 * The path parameter of DELETE /api/v1/media/{file} — the LAST segment of
 * the served URL the upload response carried (the client never hand-builds
 * a media path). An empty string for a URL without a segment (callers treat
 * it as "nothing to delete").
 */
export function mediaFileFromURL(url: string): string {
  const trimmed = url.split("?")[0] ?? "";
  const segments = trimmed.split("/");
  return segments[segments.length - 1] ?? "";
}

/**
 * DELETE /api/v1/media/{file} — remove one UNREFERENCED file (admin+). A
 * 409 means the file is still referenced by content, which is a refusal to
 * delete, not a client bug: the widget's speculative delete treats it as
 * "keep the file" (the sweep reclaims it if it stops being referenced).
 */
export function deleteMedia(
  file: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/media/${encodeURIComponent(file)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}
