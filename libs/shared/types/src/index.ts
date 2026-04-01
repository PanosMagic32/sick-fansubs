export type SearchTypeOption = 'all' | 'blog-post' | 'project';

export interface UserRef {
  _id?: string;
  username?: string;
  email?: string;
  avatar?: string;
  isAdmin?: boolean;
}

export interface BlogPost {
  _id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLinkTorrent: string;
  downloadLink: string;
  dateTimeCreated: Date | string;
  downloadLink4kTorrent?: string;
  downloadLink4k?: string;
  creator?: UserRef;
  updatedBy?: UserRef;
  updatedAt?: Date | string;
}

export type CreateBlogPost = Omit<BlogPost, '_id' | 'creator'>;

export type EditBlogPost = Omit<BlogPost, 'dateTimeCreated' | 'creator'>;

export interface Project {
  _id?: string;
  title: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date | string;
  creator?: UserRef;
  updatedBy?: UserRef;
  batchDownloadLinks?: ProjectBatchDownloadLink[];
  updatedAt?: Date | string;
}

export interface ProjectBatchDownloadLink {
  name: string;
  downloadLinkTorrent: string;
  downloadLink: string;
  downloadLink4kTorrent?: string;
  downloadLink4k?: string;
}

export interface CreateProject {
  title: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date | string;
  batchDownloadLinks?: ProjectBatchDownloadLink[];
}

export interface EditProject {
  title: string;
  description: string;
  thumbnail: string;
  batchDownloadLinks?: ProjectBatchDownloadLink[];
}

export interface Searchable {
  _id: string;
  title: string;
  subtitle?: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date | string;
  type: 'blog-post' | 'project';
  downloadLink?: string;
  downloadLink4k?: string;
}
