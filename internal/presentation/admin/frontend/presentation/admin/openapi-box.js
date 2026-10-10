// The OpenAPI entrypoint groups (FEAT-018, CA-005): the API section renders one
// box per group, naming the endpoints that achieve the group's result with one
// curl example. The endpoints named must match docs/openapi-admin.json (the
// contract test asserts this). The box is informational: it never issues requests.

const API_GROUPS = {
  authentication: {
    title: 'Authentication',
    icon: 'vpn_key',
    endpoints: ['POST /api/admin/login', 'POST /api/admin/logout'],
    example: 'curl -X POST -d \'{"password":"secret"}\' https://host/api/admin/login',
  },
  config: {
    title: 'Config',
    icon: 'tune',
    endpoints: [
      'GET /api/admin/config',
      'PUT /api/admin/config',
      'GET /api/admin/config/{section}',
      'PUT /api/admin/config/{section}',
      'GET /api/admin/config/systems/{systemId}/sites',
      'PUT /api/admin/config/systems/{systemId}/sites',
      'GET /api/admin/config/systems/{systemId}/talkgroups',
      'PUT /api/admin/config/systems/{systemId}/talkgroups',
      'GET /api/admin/config/systems/{systemId}/units',
      'PUT /api/admin/config/systems/{systemId}/units',
    ],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'{"maxClients":60}\' https://host/api/admin/config/options',
  },
  alerts: {
    title: 'Alerts',
    icon: 'notifications',
    endpoints: ['GET /api/admin/alerts'],
    example: 'curl -H "Authorization: $TOKEN" https://host/api/admin/alerts',
  },
  logs: {
    title: 'Logs',
    icon: 'article',
    endpoints: ['POST /api/admin/logs'],
    example: 'curl -X POST -H "Authorization: $TOKEN" -d \'{"limit":50}\' https://host/api/admin/logs',
  },
  password: {
    title: 'Password',
    icon: 'password',
    endpoints: ['POST /api/admin/password'],
    example: 'curl -X POST -H "Authorization: $TOKEN" -d \'{"currentPassword":"old","newPassword":"new-secret"}\' https://host/api/admin/password',
  },
  users: {
    title: 'Users',
    icon: 'person',
    endpoints: ['POST /api/admin/user-add', 'POST /api/admin/user-remove'],
    example: 'curl -X POST -H "Authorization: $TOKEN" -d \'{"id":0,"order":1,"label":"Support","phone":"555-0100"}\' https://host/api/admin/user-add',
  },
  ingestion: {
    title: 'Ingestion',
    icon: 'cloud_upload',
    endpoints: ['POST /api/call-upload', 'POST /api/trunk-recorder-call-upload'],
    example: 'curl -F "key=YOUR_API_KEY" -F "audio=@call.mp3;type=audio/mpeg" -F "system=1" -F "talkgroup=1514" -F "timestamp=1721692800000" https://host/api/call-upload',
  },
};

export { API_GROUPS };

class RdioAdminOpenApiBox extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const content = API_GROUPS[this.dataset.group];

    if (!content) return;

    const box = document.createElement('div');
    box.className = 'rdio-admin-openapi-box';

    const title = document.createElement('p');
    title.className = 'box-title';
    title.innerHTML = `<i class="material-icons" aria-hidden="true">${content.icon}</i>${content.title}`;

    const intro = document.createElement('p');
    intro.className = 'caption';
    intro.textContent = "Admin endpoints take the raw JWT in the Authorization header, no Bearer prefix; the capture endpoints authenticate with the key form field.";

    const list = document.createElement('ul');

    for (const endpoint of content.endpoints) {
      const item = document.createElement('li');

      const code = document.createElement('code');
      code.textContent = endpoint;

      item.append(code);
      list.append(item);
    }

    const example = document.createElement('pre');
    example.textContent = content.example;

    box.append(title, intro, list, example);

    this.replaceChildren(box);
  }
}

customElements.define('rdio-admin-openapi-box', RdioAdminOpenApiBox);
