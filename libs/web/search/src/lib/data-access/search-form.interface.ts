import { FormControl } from '@angular/forms';

import type { SearchTypeOption, Searchable } from '@shared/types';

export type { SearchTypeOption, Searchable } from '@shared/types';

export interface SearchFormModel {
  searchTerm: FormControl<string>;
  type: FormControl<SearchTypeOption>;
}

export interface SearchResponse {
  results: Searchable[];
  total: number;
}
