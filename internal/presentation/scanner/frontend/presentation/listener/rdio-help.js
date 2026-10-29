// rdio-help: support snackbar linking to the configured email or the Rdio Scanner discussions
// page, with a 10 s countdown before it dismisses itself.

import { dismissSnackbar, escapeHtml, showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

export function showHelp(email) {
  const content = document.createElement('div');
  content.className = 'rdio-support';

  const message = document.createElement('span');

  if (email) {
    // the email is free text the administrator configured: it shows
    // as the text it is and plants no element.
    const safe = escapeHtml(email);

    message.innerHTML = `For further assistance please contact <b><a href="mailto:${safe}" target="_blank">${safe}</a></b>`;
  } else {
    message.innerHTML =
      'For further assistance please go to <b><a href="https://github.com/chuot/rdio-scanner/discussions" target="_blank">Rdio&nbsp;Scanner&nbsp;discussions</a></b>';
  }

  const countdown = startCountdown();

  content.append(message, countdown.node);
  showSnackbar(content, null);
}

// shared 10 s countdown footer used by the help and promo snackbars; the tag makes the native
// footer a flex row like every other row of that component.
export function startCountdown(tag = 'span') {
  const node = document.createElement(tag);
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
