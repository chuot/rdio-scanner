// The OpenAPI help box (FEAT-016, T019): a small box at the bottom of every
// admin section card naming the entry points that achieve the section's result,
// with one curl example. It is informational: it never issues requests. The
// endpoints it names must match docs/openapi-admin.json (the contract test
// asserts this); the capture endpoint of the Ingestion section lives in
// docs/api.md.

const SECTION_BOXES = {
  access: {
    endpoints: ['GET /api/admin/config/access', 'PUT /api/admin/config/access'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"label":"Support","phone":"555-0100"}]\' https://host/api/admin/config/access',
  },
  apikeys: {
    endpoints: ['GET /api/admin/config/apikeys', 'PUT /api/admin/config/apikeys'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"apikey":"a1b2c3","system":1,"label":"Capture key"}]\' https://host/api/admin/config/apikeys',
  },
  dirwatch: {
    endpoints: ['GET /api/admin/config/dirwatch', 'PUT /api/admin/config/dirwatch'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"path":"/var/spool/recordings","system":1}]\' https://host/api/admin/config/dirwatch',
  },
  downstreams: {
    endpoints: ['GET /api/admin/config/downstreams', 'PUT /api/admin/config/downstreams'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"apikey":"a1b2c3","system":1,"host":"https://other.scanner","systemId":2}]\' https://host/api/admin/config/downstreams',
  },
  groups: {
    endpoints: ['GET /api/admin/config/groups', 'PUT /api/admin/config/groups'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"label":"Dispatch"}]\' https://host/api/admin/config/groups',
  },
  tags: {
    endpoints: ['GET /api/admin/config/tags', 'PUT /api/admin/config/tags'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"label":"Fire","color":"red"}]\' https://host/api/admin/config/tags',
  },
  systems: {
    endpoints: ['GET /api/admin/config/systems', 'PUT /api/admin/config/systems'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"systemRef":1,"label":"Metro","autoPopulate":true}]\' https://host/api/admin/config/systems',
  },
  sites: {
    endpoints: ['GET /api/admin/config/systems/{systemId}/sites', 'PUT /api/admin/config/systems/{systemId}/sites'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"siteRef":77,"label":"East"}]\' https://host/api/admin/config/systems/1/sites',
  },
  talkgroups: {
    endpoints: ['GET /api/admin/config/systems/{systemId}/talkgroups', 'PUT /api/admin/config/systems/{systemId}/talkgroups'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"talkgroupRef":1514,"label":"Ground","groupIds":[1],"tagId":2}]\' https://host/api/admin/config/systems/1/talkgroups',
  },
  units: {
    endpoints: ['GET /api/admin/config/systems/{systemId}/units', 'PUT /api/admin/config/systems/{systemId}/units'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'[{"id":0,"order":1,"unitRef":1001,"label":"Car 1"}]\' https://host/api/admin/config/systems/1/units',
  },
  options: {
    endpoints: ['GET /api/admin/config/options', 'PUT /api/admin/config/options'],
    example: 'curl -X PUT -H "Authorization: $TOKEN" -d \'{"maxClients":60}\' https://host/api/admin/config/options',
  },
  logs: {
    endpoints: ['POST /api/admin/logs'],
    example: 'curl -X POST -H "Authorization: $TOKEN" -d \'{"limit":50}\' https://host/api/admin/logs',
  },
  tools: {
    endpoints: ['GET /api/admin/config', 'PUT /api/admin/config', 'POST /api/admin/password'],
    example: 'curl -X POST -H "Authorization: $TOKEN" -d \'{"currentPassword":"old","newPassword":"new-secret"}\' https://host/api/admin/password',
  },
  ingestion: {
    endpoints: ['POST /api/call-upload'],
    example: 'curl -F "apikey=YOUR_API_KEY" -F "recording=@call.mp3;type=audio/mpeg" -F "system=1" -F "type=DMR" -F "frequency=1514000000" -F "talkgroup=1514" -F "start=1721692800000" https://host/api/call-upload',
  },
};

class RdioAdminOpenApiBox extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const content = SECTION_BOXES[this.dataset.section];

    if (!content) return;

    const box = document.createElement('div');
    box.className = 'rdio-admin-openapi-box';

    const title = document.createElement('p');
    title.className = 'box-title';
    title.textContent = 'OpenAPI entry points';

    const intro = document.createElement('p');
    intro.className = 'caption';
    intro.textContent = 'The section saves through these endpoints (raw JWT in the Authorization header, no Bearer prefix).';

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
