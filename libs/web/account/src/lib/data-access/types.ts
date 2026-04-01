export type AccountViewState = 'loading' | 'error' | 'ready' | 'idle';

export type FavoritesViewState =
  | 'loading-ids'
  | 'ids-error'
  | 'empty-ids'
  | 'loading-posts'
  | 'posts-error'
  | 'empty-posts'
  | 'ready';
