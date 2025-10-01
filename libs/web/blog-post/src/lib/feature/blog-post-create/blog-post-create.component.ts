import { ChangeDetectionStrategy, Component, effect, inject, signal } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent } from '@web/shared';

import { ActivatedRoute, Router } from '@angular/router';
import type { CreateBlogPost } from '../../data-access/blog-post.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import type { PostFormModel } from '../../data-access/post-form.interface';
import { BlogPostFormComponent } from '../../ui/blog-post-form/blog-post-form.component';

@Component({
  selector: 'sf-blog-post-create',
  templateUrl: './blog-post-create.component.html',
  styleUrl: './blog-post-create.component.scss',
  imports: [
    BlogPostFormComponent,
    NoContentComponent,
    MatProgressBar,
    MatCard,
    MatCardHeader,
    MatCardContent,
    MatCardActions,
    MatIcon,
    MatButton,
    MatDivider,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class BlogPostCreateComponent {
  private readonly blogPostService = inject(BlogPostService);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);

  private readonly blogPost = signal<CreateBlogPost | null>(null);

  protected readonly createBlogPost = this.blogPostService.createBlogPost(this.blogPost);

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
    downloadLink4k: new FormControl('', {
      nonNullable: true,
    }),
  });

  constructor() {
    effect(() => {
      if (this.createBlogPost.hasValue() && !this.createBlogPost.error() && !this.createBlogPost.isLoading()) {
        this.router.navigate(['/'], { relativeTo: this.activatedRoute });
      }
    });
  }

  onSubmit() {
    if (this.createForm.invalid) return;

    const post: CreateBlogPost = {
      title: this.createForm.value.title ?? '',
      subtitle: this.createForm.value.subtitle ?? '',
      description: this.createForm.value.description ?? '',
      thumbnail: this.createForm.value.thumbnail ?? '',
      downloadLink: this.createForm.value.downloadLink ?? '',
      downloadLink4k: this.createForm.value.downloadLink4k ?? '',
      dateTimeCreated: new Date(),
    };

    this.blogPost.set(post);
  }
}
