# account

Account feature library for authenticated profile management.

## Includes

- profile summary and profile edit form
- avatar upload through `POST /api/media/images`
- manual avatar URL override
- favorites listing and pagination

## Notes

- user-facing copy stays in Greek
- avatar uploads are staged first, then persisted on save
- on ultra-wide screens the profile and favorites panes render side-by-side
