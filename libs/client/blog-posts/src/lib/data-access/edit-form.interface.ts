import { FormControl } from '@angular/forms';

export interface EditForm {
  title: FormControl<string>;
  subtitle: FormControl<string>;
  description: FormControl<string>;
  thumbnail: FormControl<string>;
  downloadLink: FormControl<string>;
}
