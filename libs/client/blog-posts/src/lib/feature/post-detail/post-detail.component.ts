import { Component, OnInit } from '@angular/core';
import { FormGroup, FormControl, Validators } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';

import { PostForm } from '../../data-access/post-form.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import { BlogPost } from '../../data-access/blog-post.interface';
import { map } from 'rxjs';

@Component({
  selector: 'sick-post-detail',
  templateUrl: './post-detail.component.html',
  styleUrl: './post-detail.component.scss',
})
export class PostDetailComponent implements OnInit {
  private id = '';

  editForm = new FormGroup<PostForm>({
    title: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    subtitle: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    description: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    thumbnail: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    downloadLink: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
  });

  get formControl() {
    return this.editForm.controls;
  }

  constructor(
    private readonly blogPostService: BlogPostService,
    private readonly route: ActivatedRoute,
  ) {}

  ngOnInit(): void {
    this.id = this.route.snapshot.paramMap.get('id') ?? '';

    if (this.id !== '') {
      this.blogPostService
        .getBlogPostById(this.id)
        .pipe(
          map((blogPost) => ({
            title: blogPost.title,
            subtitle: blogPost.subtitle,
            description: blogPost.description,
            thumbnail: blogPost.thumbnail,
            downloadLink: blogPost.downloadLink,
          })),
        )
        .subscribe((blogPost) => this.editForm.setValue(blogPost));
    }
  }

  onSave() {
    this.blogPostService.updateBlogPost(this.id, this.editForm.value as BlogPost);
  }

  onDelete() {
    this.blogPostService.deleteBlogPost(this.id);
  }
}
