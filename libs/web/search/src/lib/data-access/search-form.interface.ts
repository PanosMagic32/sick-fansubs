import { FormControl } from '@angular/forms';

export type SearchTypeOption = 'all' | 'blog-post' | 'project';

export interface SearchFormModel {
  searchTerm: FormControl<string>;
  type: FormControl<SearchTypeOption>;
}

export interface Searchable {
  _id: string;
  title: string;
  subtitle?: string;
  description: string;
  thumbnail: string;
  dateTimeCreated: Date;
  type: SearchTypeOption;
  downloadLink: string;
  downloadLink4k: string;
}

export interface SearchResponse {
  results: Searchable[];
  total: number;
}
