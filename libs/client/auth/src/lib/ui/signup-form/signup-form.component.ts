import { Component, OnInit } from '@angular/core';
import { UntypedFormControl, Validators } from '@angular/forms';

@Component({
  selector: 'sick-signup-form',
  templateUrl: './signup-form.component.html',
  styleUrls: [],
})
export class SignupFormComponent implements OnInit {
  firstName = new UntypedFormControl('');
  lastName = new UntypedFormControl('');
  email = new UntypedFormControl('', [Validators.required, Validators.email]);
  password = new UntypedFormControl('', [Validators.required]);
  confirmPassword = new UntypedFormControl('', [Validators.required]);

  constructor() {}

  ngOnInit(): void {}

  getErrorMessage(field: string) {
    if (field === 'email') {
      if (this.email.hasError('required')) {
        return 'You must enter an email';
      }

      return this.email.hasError('email') ? 'Not a valid email' : '';
    } else if (field === 'password') {
      return 'You must enter a password';
    } else {
      return 'You must enter a password';
    }
  }
}
