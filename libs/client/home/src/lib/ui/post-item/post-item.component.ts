import { Component, Input } from '@angular/core';

import { BlogPost } from '../../data-access/blog-post.interface';

@Component({
    selector: 'sick-post-item',
    templateUrl: './post-item.component.html',
    styleUrls: ['./post-item.component.scss'],
})
export class PostItemComponent {
    @Input() post!: BlogPost;
    @Input() index!: number;

    onDownload(url: string) {
        window.open(url);
    }
}
