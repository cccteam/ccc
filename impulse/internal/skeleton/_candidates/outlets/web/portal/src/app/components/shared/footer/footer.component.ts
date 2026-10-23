import { Component } from '@angular/core';
import { ReleaseComponent } from '@cccteam/resource-angular/ui-release';

@Component({
  selector: 'app-footer',
  templateUrl: './footer.component.html',
  styleUrl: './footer.component.scss',
  imports: [ReleaseComponent],
})
export class FooterComponent {
  // The application's name, which opens the footer line.
  name = 'Outlets Portal';
}
