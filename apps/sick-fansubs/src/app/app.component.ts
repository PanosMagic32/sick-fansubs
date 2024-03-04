import { Component, OnInit } from '@angular/core';

import { TokenService } from '@sick/client/auth';

@Component({
  selector: 'sick-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss'],
})
export class AppComponent implements OnInit {
  constructor(private readonly tokenService: TokenService) {}

  ngOnInit(): void {
    this.tokenService.getUserIDFromToken();
  }
}
