import type { User } from '@api/user';

export interface Project {
  _id?: string;
  title: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date;
  creator?: User;
  batchDownloadLinks?: string[];
}

export interface CreateProject {
  title: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date;
  batchDownloadLinks?: string[];
}

export interface EditProject {
  title: string;
  description: string;
  thumbnail: string;
  batchDownloadLinks?: string[];
}

export interface ProjectResponse {
  projects: Project[];
  count: number;
}
