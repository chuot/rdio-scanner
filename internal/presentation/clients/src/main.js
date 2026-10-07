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
 * Entry router: main.js stays tiny and lazily loads exactly one page entry.
 * The listener entry boots the listener socket and shell; the admin entry
 * must never open the listener socket (contract-server-protocol.md §1).
 */

import '../shared/styles/theme.css';
import '../scanner/styles/rdio.css';
import '../admin/styles/admin.css';

import { openDialog } from '../shared/presentation/ui/primitives.js';

/*
 * PWA update prompt (decision-pwa-custom-sw.md §3): a waiting service
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

const isAdmin = window.location.pathname.replace(/\/+$/, '') === '/admin';

if (isAdmin) {
  import('../admin/admin.js');
} else {
  import('../scanner/listener.js');
}
