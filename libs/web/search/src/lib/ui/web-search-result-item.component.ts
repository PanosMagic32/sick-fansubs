import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';
import { DatePipe } from '@angular/common';
import { Router, ActivatedRoute } from '@angular/router';

import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIcon } from '@angular/material/icon';

import { Searchable, SearchTypeOption } from '../data-access/search-form.interface';

@Component({
  selector: 'sf-web-search-result-item',
  templateUrl: './web-search-result-item.component.html',
  styleUrl: './web-search-result-item.component.scss',
  imports: [MatCardModule, MatButtonModule, MatIcon, DatePipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class WebSearchResultItemComponent {
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);

  readonly searchResult = input.required<Searchable>();
  readonly isAdmin = input.required<boolean>();

  onEdit(type: SearchTypeOption) {
    const path = type === 'blog-post' ? `/${this.searchResult()._id}/edit` : `/projects/${this.searchResult()._id}/edit`;
    this.router.navigate([path], {
      relativeTo: this.activatedRoute,
    });
  }

  onMore() {
    this.router.navigate(['/projects', this.searchResult()._id], {
      relativeTo: this.activatedRoute,
    });
  }

  onDownload(link: string) {
    window.open(link);
  }
}
