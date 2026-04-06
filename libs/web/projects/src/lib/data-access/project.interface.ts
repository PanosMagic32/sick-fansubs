import type { Project } from '@shared/types';

export type { CreateProject, EditProject, Project, ProjectBatchDownloadLink } from '@shared/types';

export interface FavoriteProjectIdsResponse {
  favoriteProjectIds: string[];
}

export interface ProjectResponse {
  projects: Project[];
  count: number;
}
