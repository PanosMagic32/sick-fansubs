import { Component } from '@angular/core';

@Component({
  selector: 'sf-no-content',
  template: `
    <div class="no-content">
      <img src="images/oh-no.png" alt="error-img" />
      <p>Δεν υπάρχει περιεχόμενο για προβολή!</p>
    </div>
  `,
  standalone: true,
})
export class NoContentComponent {}
