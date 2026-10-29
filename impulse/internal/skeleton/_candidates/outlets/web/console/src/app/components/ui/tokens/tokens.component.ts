import { Component } from '@angular/core';
import { TokensComponent as CccTokensComponent } from '@cccteam/resource-angular/ccc-tokens';

/**
 * The Tokens page composes the library's tokens component, and nothing here names a
 * route or an outlet: the component reads the generated descriptor's tokens block, the
 * routes the console's auth serves for the devices outlet it issues tokens for. The
 * signed-in person lists their own live tokens, issues an application token for a
 * program of their own (its value shown once), and revokes one; a command-line client's
 * grants show here too, once the person approved its request on the device page.
 */
@Component({
  selector: 'app-tokens',
  imports: [CccTokensComponent],
  templateUrl: './tokens.component.html',
  styleUrl: './tokens.component.scss',
})
export class TokensComponent {}
