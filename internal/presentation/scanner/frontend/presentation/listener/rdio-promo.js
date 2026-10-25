// rdio-promo: native-app promo snackbar, shown 10 s after boot on Android, iPad, and iPhone
// user agents, with a 10 s countdown and inline-SVG store badges.

import appStoreBadge from '../../assets/badges/app-store-badge.svg';
import googlePlayBadge from '../../assets/badges/google-play-badge.svg';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';
import { startCountdown } from './rdio-help.js';

export function detectDeviceType(userAgent) {
  if (userAgent.includes('Android')) return 'Android';
  if (userAgent.includes('iPad') || userAgent.includes('iPhone')) return 'Apple';
  return null;
}

export function showPromo(userAgent = navigator.userAgent) {
  const deviceType = detectDeviceType(userAgent);

  if (!deviceType) return false;

  const content = document.createElement('div');
  content.className = 'rdio-native';

  const message = document.createElement('p');
  message.textContent = `Do you know that Rdio Scanner works natively on ${deviceType} devices?`;

  const messageBox = document.createElement('div');
  messageBox.append(message);

  const badgeBox = document.createElement('div');

  if (deviceType === 'Android') {
    badgeBox.innerHTML = `<a href="https://play.google.com/store/apps/details?id=solutions.saubeo.rdioScanner" target="_blank">${googlePlayBadge}</a>`;
  } else {
    badgeBox.innerHTML = `<a href="https://apps.apple.com/us/app/rdio-scanner/id1563065667#?platform=iphone" target="_blank">${appStoreBadge}</a>`;
  }

  // one row holds the message and the badge side by side, with the countdown outside it.
  const row = document.createElement('div');
  row.append(messageBox, badgeBox);

  const countdown = startCountdown('div');

  content.append(row, countdown.node);
  showSnackbar(content, null);

  return true;
}

// the promo check runs 10 s after boot.
export function schedulePromo({ setTimeoutImpl = setTimeout, userAgent = navigator.userAgent } = {}) {
  return setTimeoutImpl(() => showPromo(userAgent), 10000);
}
