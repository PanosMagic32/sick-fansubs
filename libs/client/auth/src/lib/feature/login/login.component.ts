import { Component, OnInit } from '@angular/core';
import { UntypedFormControl, Validators } from '@angular/forms';

import { AuthService } from '../../data-access/auth.service';

@Component({
  selector: 'sick-login',
  templateUrl: './login.component.html',
  styleUrls: [],
})
export class LoginComponent implements OnInit {
  email = new UntypedFormControl('', [Validators.required, Validators.email]);
  password = new UntypedFormControl('', [Validators.required]);

  constructor(private authService: AuthService) {}

  ngOnInit(): void {}
}
