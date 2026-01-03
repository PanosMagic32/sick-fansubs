import type { User } from '@api/user';

export type BlogPost = {
  _id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLinkTorrent: string;
  downloadLink: string;
  dateTimeCreated: Date;
  downloadLink4kTorrent?: string | undefined;
  downloadLink4k?: string | undefined;
  creator?: User;
};

export type CreateBlogPost = Omit<BlogPost, '_id' | 'creator'>;

export type EditBlogPost = Omit<BlogPost, 'dateTimeCreated' | 'creator'>;

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
