import type { User } from '@sick/api/user';

export interface BlogPost {
  _id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  dateTimeCreated: Date;
  creator?: User;
}

export interface CreateBlogPost {
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  dateTimeCreated: Date;
}

export interface EditBlogPost {
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
}

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
