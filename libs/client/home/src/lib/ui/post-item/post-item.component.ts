import { Component, Input, OnInit } from '@angular/core';

@Component({
  selector: 'sick-post-item',
  templateUrl: './post-item.component.html',
  styles: [],
})
export class PostItemComponent implements OnInit {
  @Input() post!: any; // TODO - make a BlogPost front model

  constructor() {}

  ngOnInit(): void {}

  onDownload(url: string) {
    window.open(url);
  }
}
