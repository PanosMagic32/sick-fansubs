import { User } from '@sick/api/user';

export interface BlogPost {
  id?: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  dateTimeCreated: string;
  creator?: User;
}

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
