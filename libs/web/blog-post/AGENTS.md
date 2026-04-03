# libs/web/blog-post — `@web/blog-post`

## Purpose

The root route of the app. Manages the blog post feed — the list of fansub subtitle releases with download links (standard, torrent, 4K, 4K torrent). Supports paginated browsing, creation, and editing for admins.

## Path Alias

`@web/blog-post` → `libs/web/blog-post/src/index.ts`

## Public API (Exports)

| Export           | Description                                      |
| ---------------- | ------------------------------------------------ |
| `blogPostRoutes` | Route config — mounted at `/` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path              | Guard               | Component                 | Load  |
| ----------------- | ------------------- | ------------------------- | ----- |
| ``(mounted at`/`) | —                   | `BlogPostListComponent`   | Eager |
| `/create`         | `authGuard` (admin) | `BlogPostCreateComponent` | Lazy  |
| `/:id/edit`       | `authGuard` (admin) | `BlogPostEditComponent`   | Lazy  |

## Key Files

### `src/lib/data-access/`

#### `blog-post.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getBlogPosts(postsPerPage, currentPage)` → `HttpResourceRef<BlogPostResponse>`
- `getBlogPostById(id)` → `HttpResourceRef<BlogPost>`
- `createBlogPost(post: Signal<CreateBlogPost | null>)` → fires only when signal is non-null
- `updateBlogPost(id, data: Signal<EditBlogPost | null>)` → fires only when signal is non-null
- `deleteBlogPost(id: Signal<string | null>)` → fires only when signal is non-null
- `getFavoriteBlogPostIds(userId)` → `GET /api/user/:id/favorites`
- `addFavoriteBlogPost(userId, postId)` → `PUT /api/user/:id/favorites/:postId`
- `removeFavoriteBlogPost(userId, postId)` → `DELETE /api/user/:id/favorites/:postId`

#### `blog-post.interface.ts`

| Type               | Description                                                                                                                                                                                                         |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BlogPost`         | Full entity: `_id?`, `title`, `subtitle`, `description`, `thumbnail`, `downloadLink`, `downloadLinkTorrent`, `downloadLink4k?`, `downloadLink4kTorrent?`, `dateTimeCreated`, `creator?`, `updatedAt?`, `updatedBy?` |
| `CreateBlogPost`   | `Omit<BlogPost, '_id' \| 'creator' \| 'updatedAt' \| 'updatedBy'>`                                                                                                                                                  |
| `EditBlogPost`     | `Omit<BlogPost, 'dateTimeCreated' \| 'creator' \| 'updatedAt' \| 'updatedBy'>`                                                                                                                                      |
| `BlogPostResponse` | `{ posts: BlogPost[]; count: number }`                                                                                                                                                                              |

Imports `User` type from `@api/user`.

#### `post-form.interface.ts`

`PostFormModel` — typed `FormGroup` shape:

- `title`, `subtitle`, `description`, `thumbnail`, `downloadLink`, `downloadLinkTorrent`, `downloadLink4k?`, `downloadLink4kTorrent?`

### `src/lib/feature/`

#### `blog-post-list/blog-post-list.component.ts` — `BlogPostListComponent` (`sf-blog-post-list`)

- Paginated list; syncs `page` and `pageSize` to query params via `ActivatedRoute`
- Shows FAB for creating (visible only to admin/super-admin roles via `TokenService.canManageContent`)
- Syncs authenticated user's favorites and handles add/remove favorite actions with optimistic-ish signal updates
- Uses `BlogPostItemComponent` for each post card
- `MatPaginator` for navigation

#### `blog-post-create/blog-post-create.component.ts` (lazy)

- Reactive form backed by `PostFormModel`
- Uses a `blogPost` signal — submitting sets the signal, which triggers the `httpResource` POST
- On success: navigates away

#### `blog-post-edit/blog-post-edit.component.ts` (lazy)

- `id` bound via `input.required<string>()` from route param
- Loads existing post to pre-fill form
- Handles update (PATCH) + delete (DELETE) via separate signals

### `src/lib/ui/`

| Component                     | Selector            | Description                                                                                                                                                                                               |
| ----------------------------- | ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `blog-post-form.component.ts` | `sf-blog-post-form` | Dumb/presentational — renders field set. Receives `form: FormGroup<PostFormModel>` as input.                                                                                                              |
| `blog-post-item.component.ts` | `sf-blog-post-item` | Card with creator/editor metadata: 70px circular avatar with fallback to logo, title, subtitle, created/edited dates with creator/editor usernames in Greek, download buttons, favorite toggle, edit FAB. |

## Dependencies

- `@web/auth` — `authGuard` (for create/edit routes)
- `@web/shared` — `TokenService`, `NoContentComponent`, `WebConfigService`
- `@api/user` — `User` type (imported via `blog-post.interface.ts`)

## Nx Tasks

```bash
pnpm nx lint web-blog-post
```

`blog-post` currently exposes only a `lint` target.

## Notes

- `downloadLink4k` and `downloadLink4kTorrent` are optional — validate their presence before rendering download buttons
- Image priority (`loading="eager"`) is applied to first two items for LCP optimization
- Creator avatar is 70px circular with 35% secondary color border and soft shadow; defaults to `/logo/logo.png` if creator has no avatar
- Timestamps and usernames are displayed in Greek ("Προστέθηκε: ... από", "Επεξεργάστηκε: ... από")
- Editor username shown only if `updatedBy` exists and differs from `creator`; falls back to "Άγνωστος χρήστης" if username unavailable
