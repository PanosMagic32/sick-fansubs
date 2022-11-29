import { User } from '@sick/api/user';

export interface Project {
  _id?: string;
  title: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: string;
  creator?: User;
  batchDownloadLinks?: string[];
}

export interface ProjectResponse {
  projects: Project[];
  count: number;
}
