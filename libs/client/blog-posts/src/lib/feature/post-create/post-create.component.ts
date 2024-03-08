import { ChangeDetectionStrategy, Component } from '@angular/core';
import { FormGroup, FormControl, Validators } from '@angular/forms';

import { PostForm } from '../../data-access/post-form.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import { BlogPost } from '../../data-access/blog-post.interface';

@Component({
  selector: 'sick-post-create',
  templateUrl: './post-create.component.html',
  styleUrl: './post-create.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PostCreateComponent {
  createForm = new FormGroup<PostForm>({
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
    return this.createForm.controls;
  }

  constructor(private readonly blogPostService: BlogPostService) {}

  onSubmit() {
    if (this.createForm.valid) {
      const post = {
        ...this.createForm.value,
        dateTimeCreated: new Date(),
      } as BlogPost;

      this.blogPostService.createBlogPost(post);
    }
  }
}
