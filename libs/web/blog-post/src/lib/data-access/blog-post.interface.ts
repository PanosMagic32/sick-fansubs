import type { User } from '@api/user';

export type BlogPost = {
  _id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  downloadLinkMagnet: string;
  dateTimeCreated: Date;
  downloadLink4k?: string | undefined;
  downloadLink4kMagnet?: string | undefined;
  creator?: User;
};

export type CreateBlogPost = Omit<BlogPost, '_id' | 'creator'>;

export type EditBlogPost = Omit<BlogPost, 'dateTimeCreated' | 'creator'>;

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
