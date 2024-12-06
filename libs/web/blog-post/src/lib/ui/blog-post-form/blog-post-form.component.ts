import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { type FormGroup, ReactiveFormsModule } from '@angular/forms';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatInput } from '@angular/material/input';

import type { PostFormModel } from '../../data-access/post-form.interface';

@Component({
  selector: 'sf-blog-post-form',
  templateUrl: './blog-post-form.component.html',
  styleUrl: './blog-post-form.component.scss',
  imports: [ReactiveFormsModule, MatFormField, MatLabel, MatError, MatInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BlogPostFormComponent {
  readonly form = input.required<FormGroup<PostFormModel>>();

  get formControls() {
    return this.form().controls;
  }
}
