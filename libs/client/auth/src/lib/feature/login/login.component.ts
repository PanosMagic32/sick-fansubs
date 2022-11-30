import { Component } from '@angular/core';
import { UntypedFormControl, Validators } from '@angular/forms';

@Component({
    selector: 'sick-login',
    templateUrl: './login.component.html',
    styleUrls: [],
})
export class LoginComponent {
    email = new UntypedFormControl('', [Validators.required, Validators.email]);
    password = new UntypedFormControl('', [Validators.required]);
}
