// Admin token session: sessionStorage rdio-scanner-admin-token, the raw JWT.
// The admin application is the only one that holds a session, and it holds no
// listener state: no livefeed map, no pin, no localStorage at all.

export const ADMIN_TOKEN_KEY = 'rdio-scanner-admin-token';

export function createTokenStorage({ sessionStorage }) {
  return {
    getAdminToken() {
      return sessionStorage.getItem(ADMIN_TOKEN_KEY) || '';
    },

    setAdminToken(token) {
      sessionStorage.setItem(ADMIN_TOKEN_KEY, token);
    },

    clearAdminToken() {
      sessionStorage.removeItem(ADMIN_TOKEN_KEY);
    },
  };
}
