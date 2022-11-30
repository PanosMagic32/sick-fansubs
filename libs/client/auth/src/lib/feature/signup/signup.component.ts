import { Component, OnInit } from '@angular/core';

import { AuthService } from '../../data-access/auth.service';

@Component({
    selector: 'sick-signup',
    templateUrl: './signup.component.html',
    styleUrls: [],
})
export class SignupComponent implements OnInit {
    constructor(private authService: AuthService) {}

    ngOnInit(): void {}
}
