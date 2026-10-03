import { Component, computed } from '@angular/core';
import { Squadrons } from '@app/service/zz_gen_constants';
import { CustomConfigComponent } from '@cccteam/resource-angular/types';

/**
 * The squadron's channel card: the application's own component placed among the
 * Squadrons page's related configs through componentConfig. The library renders it in the
 * squadron's page and hands it the row as parentData; the card reads the callsigns column
 * and says one derived thing, which is what a componentConfig is for: a line no field
 * renders. Its three answers are the nullable array's three states (decode.nullable-slice):
 * filed callsigns, a filed empty list, and no filing yet.
 *
 * Demonstrates: config.component.
 */
@Component({
  selector: 'app-squadron-channel',
  templateUrl: './squadron-channel.component.html',
  styleUrl: './squadron-channel.component.scss',
})
export class SquadronChannelComponent extends CustomConfigComponent {
  /** The one line the card says about the squadron's callsigns. */
  line = computed((): string => {
    const row = this.parentData();
    if (!row) {
      return '';
    }
    const name = String(row[Squadrons.fieldName.name] ?? 'This squadron');
    const callsigns = row[Squadrons.fieldName.callsigns];
    if (callsigns === undefined || callsigns === null) {
      return `${name} has not filed callsigns yet.`;
    }
    if (!Array.isArray(callsigns) || callsigns.length === 0) {
      return `${name} flies silent: an empty filing.`;
    }
    return `${name} answers to ${callsigns.join(' and ')} on the open channel.`;
  });
}
