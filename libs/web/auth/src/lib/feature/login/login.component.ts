import { ChangeDetectionStrategy, Component } from '@angular/core';

import { LoginFormComponent } from '../../ui/login-form/login-form.component';

@Component({
  selector: 'sf-login',
  template: `<sf-login-form />`,
  imports: [LoginFormComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class LoginComponent {}
