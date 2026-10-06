import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { SwUpdate, UnrecoverableStateEvent, VersionEvent } from '@angular/service-worker';
import {
  AppReloader,
  provideAppUpdate,
  RELOAD_LABEL,
  VERSION_READY_MESSAGE,
} from '@cccteam/resource-angular/ui-app-update';
import { NotificationService } from '@cccteam/resource-angular/ui-notification-service';
import { Subject } from 'rxjs';

// The update notice as the console wires it: provideAppUpdate() beside the service worker
// in app.config.ts and nothing else, no component referencing the service. Over a worker
// standing in for the browser's (the real one is off in specs, which never load the app
// config), a new version ready raises one persistent notice with Reload in the alert area
// the shell renders, Reload goes through the library's reloader, and nothing is raised
// before the worker says so. The library's own specs cover the service's every branch;
// this one proves the entry point the console imports starts it in this workspace.
//
// Demonstrates: webapp.update-notice.

class FakeSwUpdate {
  readonly versionUpdates = new Subject<VersionEvent>();
  readonly unrecoverable = new Subject<UnrecoverableStateEvent>();
  readonly isEnabled = true;

  async checkForUpdate(): Promise<boolean> {
    return false;
  }
}

class FakeReloader {
  reloads = 0;

  reload(): void {
    this.reloads++;
  }

  async unregisterWorkers(): Promise<void> {
    return;
  }
}

describe('the update notice', () => {
  let worker: FakeSwUpdate;
  let reloader: FakeReloader;

  beforeEach(() => {
    worker = new FakeSwUpdate();
    reloader = new FakeReloader();
    TestBed.configureTestingModule({
      providers: [
        { provide: SwUpdate, useValue: worker },
        { provide: AppReloader, useValue: reloader },
        provideRouter([]),
        provideAppUpdate(),
      ],
    });
    // The environment initializer provideAppUpdate() registers runs when the injector is
    // built, which the first injection does.
    TestBed.inject(NotificationService);
  });

  afterEach(() => {
    TestBed.resetTestingModule();
  });

  it('raises nothing until the worker has a new version', () => {
    expect(TestBed.inject(NotificationService).notifications()).toEqual([]);
    expect(reloader.reloads).toBe(0);
  });

  it('raises one persistent notice with Reload when a new version is ready, and Reload reloads', () => {
    worker.versionUpdates.next({
      type: 'VERSION_READY',
      currentVersion: { hash: 'old' },
      latestVersion: { hash: 'new' },
    });

    const notices = TestBed.inject(NotificationService).notifications();
    expect(notices.map((notice) => notice.message)).toEqual([VERSION_READY_MESSAGE]);
    expect(notices[0]?.persistent).toBe(true);
    expect(notices[0]?.action?.label).toBe(RELOAD_LABEL);
    expect(reloader.reloads).toBe(0);

    notices[0]?.action?.run();
    expect(reloader.reloads).toBe(1);
  });
});
