import { Component, computed, input, output } from '@angular/core';
import { Workflows } from '@app/service/zz_gen_resources';
import { Method, Resource } from '@cccteam/resource';

interface Node {
  state: string;
  x: number;
  y: number;
  terminal: boolean;
}

interface Edge {
  method: string;
  from: string;
  path: string;
  labelX: number;
  labelY: number;
  live: boolean;
}

/**
 * The portal draws the same mission graph the flight deck does, from the portal
 * target's own generated Workflows constant. For a client no edge is lit except Stand
 * Down, live only while the mission is open, claimed, or on hold and only on their
 * company's rows — the row's Execute envelope says so; the page never decides.
 */
@Component({
  selector: 'app-tracker-graph',
  templateUrl: './tracker-graph.component.html',
  styleUrl: './tracker-graph.component.scss',
})
export class TrackerGraphComponent {
  root = input.required<Resource>();
  current = input.required<string>();
  executable = input<readonly string[]>([]);
  fire = output<Method>();

  private workflow = computed(() => Workflows.find((w) => w.root === this.root()));
  readonly width = 620;
  readonly height = 170;

  nodes = computed<Node[]>(() => {
    const wf = this.workflow();
    if (!wf) return [];
    const sources = new Set(wf.transitions.flatMap((t) => t.from));
    const live = [wf.defaultState, ...wf.states.filter((s) => s !== wf.defaultState && sources.has(s))];
    const terminals = wf.states.filter((s) => !sources.has(s));
    const step = (this.width - 80) / Math.max(1, live.length - 1);
    const tstep = (this.width - 80) / (terminals.length + 1);
    return [
      ...live.map((state, i) => ({ state, x: 40 + i * step, y: 45, terminal: false })),
      ...terminals.map((state, i) => ({ state, x: 40 + (i + 1) * tstep, y: 135, terminal: true })),
    ];
  });

  edges = computed<Edge[]>(() => {
    const wf = this.workflow();
    if (!wf) return [];
    const pos = new Map(this.nodes().map((n) => [n.state, n]));
    const executable = new Set(this.executable());
    const edges: Edge[] = [];
    for (const t of wf.transitions) {
      for (const from of t.from) {
        const a = pos.get(from);
        const b = pos.get(t.to);
        if (!a || !b) continue;
        const bend = a.y === b.y ? (a.x < b.x ? -28 : 34) : 0;
        const mx = (a.x + b.x) / 2;
        const my = (a.y + b.y) / 2 + bend;
        edges.push({
          method: t.method,
          from,
          path: `M ${a.x} ${a.y} Q ${mx} ${my} ${b.x} ${b.y}`,
          labelX: mx,
          labelY: my + (bend < 0 ? -2 : 4),
          live: executable.has(t.method) && from === this.current(),
        });
      }
    }
    return edges;
  });

  label(state: string): string {
    return state.replace('_', ' ');
  }

  emit(edge: Edge): void {
    if (edge.live) {
      this.fire.emit(edge.method as Method);
    }
  }
}
