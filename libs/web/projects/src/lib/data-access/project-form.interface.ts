import type { FormArray, FormControl } from '@angular/forms';

export interface ProjectFormModel {
  title: FormControl<string>;
  description: FormControl<string>;
  thumbnail: FormControl<string>;
  batchDownloadLinks: FormArray<FormControl>;
}
