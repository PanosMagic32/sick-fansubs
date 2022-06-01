import { Component, OnInit } from '@angular/core';

import { BlogPost } from '@sick/api/blog-post';

import { BlogPostService } from '../../data-access/blog-post.service';

@Component({
  selector: 'sick-post-list',
  templateUrl: './post-list.component.html',
  styles: [],
})
export class PostListComponent implements OnInit {
  posts: BlogPost[] = [];

  constructor(private blogPostService: BlogPostService) {}

  ngOnInit(): void {
    this.blogPostService.getBlogPosts().subscribe({
      next: (posts) => {
        console.log(posts);
        this.posts = posts;
      },
      error: (err) => {
        console.log(err);
      },
    });
  }
}
