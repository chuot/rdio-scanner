/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

/*
 * Scanner entry point: registers the listener elements, boots the listener context, and
 * prompts for a service-worker update. This is the only application with a service worker,
 * so the update prompt lives here and not in a shared bootstrap.
 */

import '../../common/frontend/styles/theme.css';
import './styles/rdio.css';

import './presentation/listener/rdio-app.js';
import './presentation/listener/rdio-controls.js';
import './presentation/listener/rdio-display.js';
import './presentation/listener/rdio-search-panel.js';
import './presentation/listener/rdio-select-panel.js';
import './presentation/listener/rdio-status.js';

import { openDialog } from '../../common/frontend/presentation/ui/primitives.js';
import { getAppContext } from './application/context.js';

/*
 * PWA update prompt: a waiting service
 * worker posts AppUpdateAvailable; the dialog reproduces the legacy
 * AppUpdateComponent text and Yes applies the update before reloading
 * (SwUpdate.activateUpdate() equivalent).
 */
let updatePrompted = false;

navigator.serviceWorker?.addEventListener('message', (ev) => {
  if (ev.data?.type !== 'AppUpdateAvailable' || updatePrompted) return;

  updatePrompted = true;

  openDialog({
    title: 'New version available',
    content: 'A new version of this webapp is available. Would you like to reload the page?',
    actions: [
      { label: 'No' },
      {
        label: 'Yes',
        onClick: async () => {
          const registration = await navigator.serviceWorker.getRegistration();

          registration?.waiting?.postMessage({ type: 'SKIP_WAITING' });

          document.location.reload();
        },
      },
    ],
  });
});

getAppContext();
