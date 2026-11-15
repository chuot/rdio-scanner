// The update prompt both applications ask: a waiting worker says a new build is waiting, the page
// asks its operator, and only a yes hands that worker its skip and reloads the page.

import { openDialog } from './ui/primitives.js';

let asked = false;

export function promptForUpdate(question) {
  navigator.serviceWorker?.addEventListener('message', (event) => {
    if (event.data?.type !== 'AppUpdateAvailable' || asked) return;

    asked = true;

    openDialog({
      title: 'New version available',
      content: question,
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
}
