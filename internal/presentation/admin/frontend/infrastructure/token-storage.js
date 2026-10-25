// admin token session: sessionStorage rdio-scanner-admin-token, the raw JWT.
// the admin app holds no listener state and never touches localStorage.

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
