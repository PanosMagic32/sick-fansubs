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

| Path        | Guard               | Component                 | Load  |
| ----------- | ------------------- | ------------------------- | ----- |
| ``(root`/`) | —                   | `BlogPostListComponent`   | Eager |
| `/create`   | `authGuard` (admin) | `BlogPostCreateComponent` | Lazy  |
| `/:id/edit` | `authGuard` (admin) | `BlogPostEditComponent`   | Lazy  |

## Key Files

### `src/lib/data-access/`

#### `blog-post.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getBlogPosts(postsPerPage, currentPage)` → `HttpResourceRef<BlogPostResponse>`
- `getBlogPostById(id)` → `HttpResourceRef<BlogPost>`
- `createBlogPost(post: Signal<CreateBlogPost | null>)` → fires only when signal is non-null
- `updateBlogPost(id, data: Signal<EditBlogPost | null>)` → fires only when signal is non-null
- `deleteBlogPost(id: Signal<string | null>)` → fires only when signal is non-null

#### `blog-post.interface.ts`

| Type               | Description                                                                                                                                                                             |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BlogPost`         | Full entity: `_id?`, `title`, `subtitle`, `description`, `thumbnail`, `downloadLink`, `downloadLinkTorrent`, `downloadLink4k?`, `downloadLink4kTorrent?`, `dateTimeCreated`, `creator?` |
| `CreateBlogPost`   | `Omit<BlogPost, '_id' \| 'creator'>`                                                                                                                                                    |
| `EditBlogPost`     | `Omit<BlogPost, 'dateTimeCreated' \| 'creator'>`                                                                                                                                        |
| `BlogPostResponse` | `{ posts: BlogPost[]; count: number }`                                                                                                                                                  |

Imports `User` type from `@api/user`.

#### `post-form.interface.ts`

`PostFormModel` — typed `FormGroup` shape:

- `title`, `subtitle`, `description`, `thumbnail`, `downloadLink`, `downloadLinkTorrent`, `downloadLink4k?`, `downloadLink4kTorrent?`

### `src/lib/feature/`

#### `blog-post-list/blog-post-list.component.ts` — `BlogPostListComponent` (`sf-blog-post-list`)

- Paginated list; syncs `page` and `pageSize` to query params via `ActivatedRoute`
- Shows FAB for creating (visible only to admins via `TokenService.isAdmin`)
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

| Component                     | Selector            | Description                                                                                                                   |
| ----------------------------- | ------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `blog-post-form.component.ts` | `sf-blog-post-form` | Dumb/presentational — renders field set. Receives `form: FormGroup<PostFormModel>` as input.                                  |
| `blog-post-item.component.ts` | `sf-blog-post-item` | Card: thumbnail, title, subtitle, date, download buttons. Edit mini-FAB for admins. Priority image loading for first 2 items. |

## Dependencies

- `@web/auth` — `authGuard` (for create/edit routes)
- `@web/shared` — `TokenService`, `NoContentComponent`, `WebConfigService`
- `@api/user` — `User` type (imported via `blog-post.interface.ts`)

## Nx Tasks

```bash
pnpm nx lint web-blog-post
pnpm nx test web-blog-post
```

## Notes

- `downloadLink4k` and `downloadLink4kTorrent` are optional — validate their presence before rendering download buttons
- Image priority (`loading="eager"`) is applied to first two items for LCP optimization
