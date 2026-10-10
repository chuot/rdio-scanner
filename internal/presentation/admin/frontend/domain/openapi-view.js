// The OpenAPI section's view of the contract (FEAT-024, CA-003): the endpoint
// groups the administrator reads are derived from docs/openapi.json, the complete
// API contract (FEAT-022), so the section can only ever name endpoints the server
// answers. The contract carries no tags, so the grouping is by path: the admin
// surface splits into Authentication, Config, Alerts, Logs, Password and Users,
// the ingestion endpoints form Capture, and the static applications form
// Applications. The domain test asserts every path of the contract lands in a
// group: a contract edit that adds an endpoint fails the test until the view
// names it.

const GROUPS = [
  { title: 'Authentication', icon: 'vpn_key', match: (path) => path === '/api/admin/login' || path === '/api/admin/logout' },
  { title: 'Config', icon: 'tune', match: (path) => path.startsWith('/api/admin/config') },
  { title: 'Alerts', icon: 'notifications', match: (path) => path.startsWith('/api/admin/alerts') },
  { title: 'Logs', icon: 'article', match: (path) => path === '/api/admin/logs' },
  { title: 'Password', icon: 'lock', match: (path) => path === '/api/admin/password' },
  { title: 'Users', icon: 'person', match: (path) => path.startsWith('/api/admin/user-') },
  { title: 'Capture', icon: 'cloud_upload', match: (path) => path === '/api/call-upload' || path === '/api/trunk-recorder-call-upload' },
  { title: 'Applications', icon: 'apps', match: (path) => path === '/' || path === '/admin' || path === '/admin/' },
];

const METHODS = ['get', 'post', 'put', 'delete', 'patch'];

// The config endpoints read as one block; the sub-group names the resource each
// path saves, the way the Config section's own menus name them.
function configSubgroup(path) {
  for (const resource of ['sites', 'talkgroups', 'units']) {
    if (path.endsWith(`/${resource}`)) return resource;
  }

  return 'config';
}

export function buildOpenApiGroups(contract) {
  const groups = [];

  for (const group of GROUPS) {
    const paths = Object.entries(contract?.paths ?? {}).filter(([path]) => group.match(path));

    if (paths.length === 0) continue;

    const endpoints = [];

    for (const [path, operations] of paths) {
      for (const method of METHODS) {
        const operation = operations[method];

        if (!operation) continue;

        endpoints.push({ method: method.toUpperCase(), path, summary: operation.summary ?? '' });
      }
    }

    if (group.title === 'Config') {
      const subgroups = new Map();

      for (const endpoint of endpoints) {
        const label = configSubgroup(endpoint.path);

        if (!subgroups.has(label)) subgroups.set(label, []);

        subgroups.get(label).push(endpoint);
      }

      groups.push({ title: group.title, icon: group.icon, subgroups: [...subgroups].map(([label, list]) => ({ label, endpoints: list })) });

      continue;
    }

    groups.push({ title: group.title, icon: group.icon, endpoints });
  }

  return groups;
}

// Every path-and-method pair of the contract, for the coverage assertion: the
// section must name all of them, and nothing it does not answer.
export function contractEndpoints(contract) {
  const endpoints = [];

  for (const [path, operations] of Object.entries(contract?.paths ?? {})) {
    for (const method of METHODS) {
      if (operations[method]) endpoints.push(`${method.toUpperCase()} ${path}`);
    }
  }

  return endpoints;
}
