/*
 * *****************************************************************************
 * Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

import { ApplicationRef, Injectable } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { SwUpdate, VersionEvent } from '@angular/service-worker';
import { AppUpdateComponent } from './update.component';
import { concat, interval } from 'rxjs';
import { first } from 'rxjs/operators';

@Injectable()
export class AppUpdateService {
  constructor(
    private matDialog: MatDialog,
    private ngAppRef: ApplicationRef,
    private ngSwUpdate: SwUpdate,
  ) {
    if (!ngSwUpdate.isEnabled) {
      return;
    }

    concat(
      this.ngAppRef.isStable.pipe(first((stable) => stable === true)),
      interval(10 * 60 * 1000),
    ).subscribe(() => this.ngSwUpdate.checkForUpdate());

    this.ngSwUpdate.versionUpdates.subscribe((event: VersionEvent) => {
      if (event.type === 'VERSION_READY') {
        this.prompt();
      }
    });
  }

  prompt(): void {
    this.matDialog.open(AppUpdateComponent).afterClosed().subscribe((doUpdate) => {
      if (doUpdate) {
        if (this.ngSwUpdate.isEnabled) {
          this.ngSwUpdate.activateUpdate().then(() => document.location.reload());
        } else {
          document.location.reload();
        }
      }
    });
  }
}