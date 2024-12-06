import { ChangeDetectionStrategy, Component } from '@angular/core';

import { SignupFormComponent } from '../../ui/signup-form/signup-form.component';

@Component({
  selector: 'sf-signup',
  template: `<sf-signup-form />`,
  imports: [SignupFormComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class SignupComponent {}
