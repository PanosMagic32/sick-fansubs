import type { FormControl } from '@angular/forms';

export interface SignupFormModel {
  username: FormControl<string>;
  email: FormControl<string>;
  password: FormControl<string>;
  confirmPassword: FormControl<string>;
  avatar?: FormControl<string | null>;
}
