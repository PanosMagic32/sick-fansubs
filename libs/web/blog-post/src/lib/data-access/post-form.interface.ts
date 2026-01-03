import { FormControl } from '@angular/forms';

export interface PostFormModel {
  title: FormControl<string>;
  subtitle: FormControl<string>;
  description: FormControl<string>;
  thumbnail: FormControl<string>;
  downloadLink: FormControl<string>;
  downloadLinkMagnet: FormControl<string>;
  downloadLink4k?: FormControl<string>;
  downloadLink4kMagnet?: FormControl<string>;
}
