import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { CreateBlogPost } from '../../data-access/blog-post.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import type { PostFormModel } from '../../data-access/post-form.interface';
import { BlogPostFormComponent } from '../../ui/blog-post-form/blog-post-form.component';

@Component({
  selector: 'sf-blog-post-create',
  templateUrl: './blog-post-create.component.html',
  styleUrl: './blog-post-create.component.scss',
  imports: [BlogPostFormComponent, MatCard, MatCardHeader, MatCardContent, MatCardActions, MatIcon, MatButton, MatDivider],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class BlogPostCreateComponent {
  private readonly blogPostService = inject(BlogPostService);

  protected readonly createForm = new FormGroup<PostFormModel>({
    title: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    subtitle: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    description: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    thumbnail: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLink: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
  });

  onSubmit() {
    if (this.createForm.invalid) return;

    const post: CreateBlogPost = {
      title: this.createForm.value.title ?? '',
      subtitle: this.createForm.value.subtitle ?? '',
      description: this.createForm.value.description ?? '',
      thumbnail: this.createForm.value.thumbnail ?? '',
      downloadLink: this.createForm.value.downloadLink ?? '',
      dateTimeCreated: new Date(),
    };

    this.blogPostService.createBlogPost(post);
  }
}
