import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@shared/topbar/topbar.component';
import { TokensComponent } from './tokens.component';

let cachedTokensRoute: Route | undefined;

/**
 * tokensRoute wires the Tokens page and lists it in the Headquarters group of the
 * generated navigation, beside the Users page. The item carries no permission: every
 * crew member has tokens of their own, and the page shows only theirs.
 *
 * Demonstrates: outlet.tokens.
 */
export const tokensRoute = (): Route => {
  if (cachedTokensRoute) {
    return cachedTokensRoute;
  }

  let group = generatedNavItems.find((item) => item.label === 'Headquarters');
  if (!group) {
    group = { label: 'Headquarters', children: [] };
    generatedNavItems.push(group);
  }
  const item: MenuItem = {
    label: 'Tokens',
    route: ['tokens'],
  };
  group.children = [...(group.children ?? []), item];

  cachedTokensRoute = {
    path: 'tokens',
    loadComponent: (): Promise<Type<TokensComponent>> =>
      import('./tokens.component').then((comp) => comp.TokensComponent),
  };

  return cachedTokensRoute;
};
