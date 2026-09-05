import { Component, computed, input, output } from '@angular/core';
import { Workflows } from '@app/service/zz_gen_resources';
import { Method, Resource } from '@cccteam/resource';

interface Node {
  state: string;
  x: number;
  y: number;
  terminal: boolean;
  isDefault: boolean;
}

interface Edge {
  method: string;
  from: string;
  to: string;
  path: string;
  labelX: number;
  labelY: number;
  live: boolean;
}

/**
 * WorkflowGraphComponent draws one workflow — its states as a route and every declared
 * transition as an edge — from the generated Workflows constant, so nothing here is a
 * hand-maintained copy of server truth. The current state is lit; the edges the row's
 * capability envelope says the session user may fire NOW are drawn live and clickable;
 * every other edge is faint but present. That is the DOT file's own rule (facts drawn,
 * policy not) carried into the product: the graph is a fact about the workflow, the
 * live edges are a fact about you.
 */
@Component({
  selector: 'app-workflow-graph',
  templateUrl: './workflow-graph.component.html',
  styleUrl: './workflow-graph.component.scss',
})
export class WorkflowGraphComponent {
  /** The workflow root resource (Missions or Refits). */
  root = input.required<Resource>();
  /** The row's current state. */
  current = input.required<string>();
  /** The methods the row's Execute envelope lists — the edges lit for this user. */
  executable = input<readonly string[]>([]);
  /** Fires when a live edge is clicked. */
  fire = output<Method>();

  private workflow = computed(() => Workflows.find((w) => w.root === this.root()));

  readonly width = 620;
  readonly height = 170;

  // States laid out as a route: the ones a transition leaves (the default first) on
  // the top row, the terminal states on a second row beneath.
  nodes = computed<Node[]>(() => {
    const wf = this.workflow();
    if (!wf) return [];
    const sources = new Set(wf.transitions.flatMap((t) => t.from));
    const live = [wf.defaultState, ...wf.states.filter((s) => s !== wf.defaultState && sources.has(s))];
    const terminals = wf.states.filter((s) => !sources.has(s));
    const step = (this.width - 80) / Math.max(1, live.length - 1);
    const tstep = (this.width - 80) / (terminals.length + 1);
    return [
      ...live.map((state, i) => ({ state, x: 40 + i * step, y: 45, terminal: false, isDefault: state === wf.defaultState })),
      ...terminals.map((state, i) => ({ state, x: 40 + (i + 1) * tstep, y: 135, terminal: true, isDefault: false })),
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
        // Curve edges so a backward edge (flight_test -> in_refit) and a forward one
        // never overlap; edges to the terminal row drop down.
        const bend = a.y === b.y ? (a.x < b.x ? -28 : 34) : 0;
        const mx = (a.x + b.x) / 2;
        const my = (a.y + b.y) / 2 + bend;
        edges.push({
          method: t.method,
          from,
          to: t.to,
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
