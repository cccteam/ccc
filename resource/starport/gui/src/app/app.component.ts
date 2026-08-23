import { Component, inject } from '@angular/core';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { RouterOutlet } from '@angular/router';
import { AlertComponent } from '@cccteam/ccc-lib/ui-alert';
import { UiCoreService } from '@cccteam/ccc-lib/ui-core-service';
import { IdleService } from '@cccteam/ccc-lib/ui-idle-service';
import { NotificationService } from '@cccteam/ccc-lib/ui-notification-service';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, MatProgressBarModule, AlertComponent],
  templateUrl: './app.component.html',
  styleUrl: './app.component.scss',
})
export class AppComponent {
  core = inject(UiCoreService);
  notifications = inject(NotificationService);
  private idle = inject(IdleService);

  title = 'Starport';

  constructor() {
    this.idle.start();
  }
}
