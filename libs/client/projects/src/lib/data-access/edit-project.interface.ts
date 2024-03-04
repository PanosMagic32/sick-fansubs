import { FormArray, FormControl } from '@angular/forms';

export interface EditProjectForm {
  title: FormControl<string>;
  description: FormControl<string>;
  thumbnail: FormControl<string>;
  batchDownloadLinks: FormArray<FormControl>;
}
