export interface Searchable {
  _id: string;
  title: string;
  subtitle?: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date;
  downloadLink: string;
  downloadLink4k: string;
  type: 'blog-post' | 'project';
}
