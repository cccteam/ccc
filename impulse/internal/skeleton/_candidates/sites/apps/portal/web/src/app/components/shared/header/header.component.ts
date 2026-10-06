import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { Router, RouterModule } from '@angular/router';
import { TenantService } from '@app/tenant/tenant.service';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { IdleService } from '@cccteam/resource-angular/ui-idle-service';
import { tap } from 'rxjs';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [MatButtonModule, MatFormFieldModule, MatSelectModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  private router = inject(Router);
  private idle = inject(IdleService);
  auth = inject(AuthService);
  tenants = inject(TenantService);

  logout(): void {
    this.idle.stop();
    this.auth
      .logout()
      .pipe(tap(() => this.router.navigate(['/login'])))
      .subscribe();
  }
}
