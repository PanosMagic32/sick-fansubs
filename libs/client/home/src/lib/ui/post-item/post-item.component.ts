import { Component, Input } from '@angular/core';

@Component({
  selector: 'sick-post-item',
  templateUrl: './post-item.component.html',
})
export class PostItemComponent {
  @Input() post!: any; // TODO - make a BlogPost front model

  onDownload(url: string) {
    window.open(url);
  }
}
