// rdio-promo: native-app promo snackbar (legacy native/native.component).
// Shown 10 s after boot on Android/iPad/iPhone user agents, with a 10 s
// countdown and the inline-SVG store badges ported from the legacy client.

import appStoreBadge from '../../badges/app-store-badge.svg';
import googlePlayBadge from '../../badges/google-play-badge.svg';
import { showSnackbar } from '../ui/primitives.js';
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

  const badge = document.createElement('div');

  if (deviceType === 'Android') {
    badge.innerHTML = `<a href="https://play.google.com/store/apps/details?id=solutions.saubeo.rdioScanner" target="_blank">${googlePlayBadge}</a>`;
  } else {
    badge.innerHTML = `<a href="https://apps.apple.com/us/app/rdio-scanner/id1563065667#?platform=iphone" target="_blank">${appStoreBadge}</a>`;
  }

  const countdown = startCountdown();

  content.append(message, badge, countdown.node);
  showSnackbar(content, null);

  return true;
}

// legacy rdio-scanner.component.ts ngOnInit: the promo check runs 10 s after boot.
export function schedulePromo({ setTimeoutImpl = setTimeout, userAgent = navigator.userAgent } = {}) {
  return setTimeoutImpl(() => showPromo(userAgent), 10000);
}
