import type { Project } from '@shared/types';

export type { CreateProject, EditProject, Project } from '@shared/types';

export interface ProjectResponse {
  projects: Project[];
  count: number;
}
