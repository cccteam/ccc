import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@cccteam/resource-angular/types';
import { TokensComponent } from './tokens.component';

let cachedTokensRoute: Route | undefined;

/**
 * tokensRoute wires the Tokens page and lists it in the generated navigation, beside
 * the resource pages registered there. The item carries no permission: every signed-in
 * person has tokens of their own, and the page shows only theirs.
 */
export const tokensRoute = (): Route => {
  if (cachedTokensRoute) {
    return cachedTokensRoute;
  }

  const item: MenuItem = {
    label: 'Tokens',
    route: ['tokens'],
  };
  generatedNavItems.push(item);

  cachedTokensRoute = {
    path: 'tokens',
    loadComponent: (): Promise<Type<TokensComponent>> =>
      import('./tokens.component').then((comp) => comp.TokensComponent),
  };

  return cachedTokensRoute;
};
