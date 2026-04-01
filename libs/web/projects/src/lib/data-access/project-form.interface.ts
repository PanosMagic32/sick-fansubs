import type { FormArray, FormControl, FormGroup } from '@angular/forms';

export interface BatchDownloadLinkFormModel {
  name: FormControl<string>;
  downloadLinkTorrent: FormControl<string>;
  downloadLink: FormControl<string>;
  downloadLink4kTorrent: FormControl<string>;
  downloadLink4k: FormControl<string>;
}

export interface ProjectFormModel {
  title: FormControl<string>;
  description: FormControl<string>;
  thumbnail: FormControl<string>;
  batchDownloadLinks: FormArray<FormGroup<BatchDownloadLinkFormModel>>;
}
