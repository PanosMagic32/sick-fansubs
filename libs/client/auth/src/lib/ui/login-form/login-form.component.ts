import { Component } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { MatSnackBar } from '@angular/material/snack-bar';

import { AuthService } from '../../data-access/auth.service';
import { LoginForm } from '../../data-access/login-form.interface';

@Component({
    selector: 'sick-login-form',
    templateUrl: './login-form.component.html',
    styleUrls: ['./login-form.component.scss'],
})
export class LoginFormComponent {
    loginForm = new FormGroup<LoginForm>({
        username: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
        password: new FormControl('', { nonNullable: true, validators: [Validators.required, Validators.minLength(6)] }),
    });

    // Alternative was of typed forms with FormBuilder/NonNullableFormBuilder
    // The form builder must be injected in the constructor
    // loginForm = this.fb.group({
    //   username: ['', Validators.required],
    //   password: ['', [Validators.required, Validators.minLength(6)]],
    // });

    get formControl() {
        return this.loginForm.controls;
    }

    constructor(private authService: AuthService, private snackBar: MatSnackBar) {}

    onLogin() {
        if (!this.loginForm.value.username || !this.loginForm.value.password) {
            return;
        }

        this.authService.login(this.loginForm.value.username, this.loginForm.value.password).subscribe({
            next: (res) => {
                console.log(res);
                // TODO - handle successfull response
            },
            error: (err) => {
                this.openSnackBar(err.error.message, 'OK');
            },
        });
    }

    private openSnackBar(message: string, action: string) {
        this.snackBar.open(message, action, { duration: 3000 });
    }
}
