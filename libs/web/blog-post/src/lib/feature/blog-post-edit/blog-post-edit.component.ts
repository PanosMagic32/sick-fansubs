import { ChangeDetectionStrategy, Component, inject, input, type OnInit } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { map } from 'rxjs';

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
export default class BlogPostEditComponent implements OnInit {
  private readonly router = inject(Router);
  private readonly blogPostService = inject(BlogPostService);

  readonly id = input.required<string>();

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

  ngOnInit() {
    if (this.id() !== '') {
      this.blogPostService
        .getBlogPostById(this.id())
        .pipe(
          map((blogPost) => ({
            title: blogPost.title,
            subtitle: blogPost.subtitle,
            description: blogPost.description,
            thumbnail: blogPost.thumbnail,
            downloadLinkTorrent: blogPost.downloadLinkTorrent ?? '',
            downloadLink: blogPost.downloadLink,
            downloadLink4kTorrent: blogPost.downloadLink4kTorrent ?? '',
            downloadLink4k: blogPost.downloadLink4k ?? '',
          })),
        )
        .subscribe((blogPost: EditBlogPost) => this.editForm.setValue(blogPost));
    }
  }

  onSave() {
    this.blogPostService.updateBlogPost(this.id(), this.editForm.value as EditBlogPost);
  }

  onDelete() {
    this.blogPostService.deleteBlogPost(this.id());
  }

  onCancel() {
    this.router.navigate(['../..'], { replaceUrl: true });
  }
}
