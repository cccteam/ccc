import { Component } from '@angular/core';
import { DeviceApprovalComponent } from '@cccteam/resource-angular/ccc-tokens';

/**
 * The device page is where a command-line client sends the person it wants to act as:
 * the program prints a code and a link to this page with the code in the query
 * (`?userCode=`), the staff auth names the page as its DevicePage, and the library's
 * device approval component reads the code, shows which program asks and for which
 * outlet, and posts the person's approval or denial. The page sits behind the sign-in
 * guard with the console's other pages, so a signed-out person returns to it, code and
 * all, after signing in; it has no navigation item, since nobody opens it but from the
 * program's link.
 */
@Component({
  selector: 'app-device',
  imports: [DeviceApprovalComponent],
  templateUrl: './device.component.html',
  styleUrl: './device.component.scss',
})
export class DeviceComponent {}
