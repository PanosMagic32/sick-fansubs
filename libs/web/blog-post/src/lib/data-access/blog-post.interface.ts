import type { User } from '@api/user';

export interface BlogPost {
  _id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  dateTimeCreated: Date;
  downloadLink4k?: string;
  creator?: User;
}

export interface CreateBlogPost {
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  dateTimeCreated: Date;
  downloadLink4k?: string;
}

export interface EditBlogPost {
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  downloadLink4k?: string;
}

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
