# account

Account feature library for authenticated profile management.

## Includes

- profile summary and profile edit form
- avatar upload through `POST /api/media/images`
- manual avatar URL override
- favorites split into two domains:
  - favorite blog posts
  - favorite projects
- server-side favorites sorting (`newest` / `oldest`) and pagination
- tabbed favorites UI (Posts/Projects)
- unsaved-changes guard for profile edits

## Notes

- user-facing copy stays in Greek
- avatar uploads are staged first, then persisted on save
- favorites sorting is API-driven to remain correct with pagination
- on ultra-wide screens the profile and favorites panes render side-by-side
