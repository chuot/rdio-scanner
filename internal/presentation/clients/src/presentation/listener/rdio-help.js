// rdio-help: support snackbar (legacy main/support/support.component) linking
// to the configured email or the Rdio Scanner discussions page, with a 10 s
// countdown before it dismisses itself.

import { dismissSnackbar, showSnackbar } from '../../../shared/presentation/ui/primitives.js';

export function showHelp(email) {
  const content = document.createElement('div');
  content.className = 'rdio-support';

  const message = document.createElement('span');

  if (email) {
    message.innerHTML = `For further assistance please contact <b><a href="mailto:${email}" target="_blank">${email}</a></b>`;
  } else {
    message.innerHTML =
      'For further assistance please go to <b><a href="https://github.com/chuot/rdio-scanner/discussions" target="_blank">Rdio&nbsp;Scanner&nbsp;discussions</a></b>';
  }

  const countdown = startCountdown();

  content.append(message, countdown.node);
  showSnackbar(content, null);
}

// Shared 10 s countdown footer used by the help and promo snackbars.
export function startCountdown() {
  const node = document.createElement('span');
  node.className = 'countdown';

  let remaining = 10;
  node.textContent = `${remaining}`;

  const timer = setInterval(() => {
    remaining--;

    if (remaining < 1) {
      clearInterval(timer);
      dismissSnackbar();
    } else {
      node.textContent = `${remaining}`;
    }
  }, 1000);

  return { node, timer };
}
