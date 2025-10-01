import { Component, input } from '@angular/core';

@Component({
  selector: 'sf-no-content',
  template: `
    <div class="no-content">
      <img src="images/oh-no.png" alt="error-img" />
      <p>{{ errorMessage() }}</p>
    </div>
  `,
  standalone: true,
})
export class NoContentComponent {
  readonly errorMessage = input<string>('Δεν υπάρχει περιεχόμενο για προβολή!');
}
