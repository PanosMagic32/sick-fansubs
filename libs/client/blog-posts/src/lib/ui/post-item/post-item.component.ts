import { Component, Input } from '@angular/core';
import { Router } from '@angular/router';

import { BlogPost } from '../../data-access/blog-post.interface';

@Component({
  selector: 'sick-post-item',
  templateUrl: './post-item.component.html',
  styleUrls: ['./post-item.component.scss'],
})
export class PostItemComponent {
  @Input() post!: BlogPost;
  @Input() index!: number;
  @Input() isAdmin!: boolean;

  constructor(private readonly router: Router) {}

  onDownload(url: string) {
    window.open(url);
  }

  onEdit() {
    this.router.navigate(['blog-post', this.post._id, 'edit'], { replaceUrl: true });
  }
}
