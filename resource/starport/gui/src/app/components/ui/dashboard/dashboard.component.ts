import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { RouterModule } from '@angular/router';
import { AuthService } from '@cccteam/ccc-lib/auth-service';

@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, RouterModule],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
}
