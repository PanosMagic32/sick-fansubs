import { User } from '@sick/api/user';

export interface BlogPost {
  _id?: string;
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
