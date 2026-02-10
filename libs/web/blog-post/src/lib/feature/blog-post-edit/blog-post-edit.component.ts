import { ChangeDetectionStrategy, Component, inject, input, signal, effect } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { EditBlogPost } from '../../data-access/blog-post.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import type { PostFormModel } from '../../data-access/post-form.interface';
import { BlogPostFormComponent } from '../../ui/blog-post-form/blog-post-form.component';

@Component({
  selector: 'sf-blog-post-edit',
  templateUrl: './blog-post-edit.component.html',
  styleUrl: './blog-post-edit.component.scss',
  imports: [BlogPostFormComponent, MatCard, MatCardHeader, MatCardContent, MatCardActions, MatIcon, MatButton, MatDivider],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class BlogPostEditComponent {
  private readonly router = inject(Router);
  private readonly blogPostService = inject(BlogPostService);

  readonly id = input.required<string>();

  private readonly updateRequest = signal<EditBlogPost | null>(null);

  protected readonly blogPost = this.blogPostService.getBlogPostById(this.id);
  protected readonly updateResource = this.blogPostService.updateBlogPost(this.id, this.updateRequest);
  protected readonly deleteResource = signal<boolean>(false);

  protected readonly editForm = new FormGroup<PostFormModel>({
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
    downloadLinkTorrent: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLink: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLink4kTorrent: new FormControl('', {
      nonNullable: true,
    }),
    downloadLink4k: new FormControl('', {
      nonNullable: true,
    }),
  });

  constructor() {
    effect(() => {
      const post = this.blogPost.value();
      if (post) {
        this.editForm.patchValue({
          title: post.title,
          subtitle: post.subtitle,
          description: post.description,
          thumbnail: post.thumbnail,
          downloadLinkTorrent: post.downloadLinkTorrent ?? '',
          downloadLink: post.downloadLink,
          downloadLink4kTorrent: post.downloadLink4kTorrent ?? '',
          downloadLink4k: post.downloadLink4k ?? '',
        });
        this.editForm.markAsPristine();
      }
    });

    effect(() => {
      if (this.updateResource.value()) {
        this.router.navigate(['../..'], { replaceUrl: true });
      }
    });

    effect(() => {
      if (this.deleteResource()) {
        this.router.navigate(['../..'], { replaceUrl: true });
      }
    });
  }

  onSave() {
    if (this.editForm.valid) {
      this.updateRequest.set(this.editForm.getRawValue() as EditBlogPost);
    }
  }

  onDelete() {
    this.deleteResource.set(true);
  }

  onCancel() {
    this.router.navigate(['../..'], { replaceUrl: true });
  }
}
