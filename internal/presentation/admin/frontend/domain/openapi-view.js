// the OpenAPI groups come from the contract itself: answered endpoints only;
// schemas read one level deep, and an undefined reference is named, not dropped
// see the openapi-contract-grouping maintainer note.

const GROUPS = [
  { title: 'Authentication', icon: 'vpn_key', match: (path) => path === '/api/admin/login' || path === '/api/admin/logout' },
  { title: 'Config', icon: 'tune', match: (path) => path.startsWith('/api/admin/config') },
  { title: 'Alerts', icon: 'notifications', match: (path) => path.startsWith('/api/admin/alerts') },
  { title: 'Status', icon: 'speed', match: (path) => path === '/api/admin/status' },
  { title: 'Logs', icon: 'article', match: (path) => path === '/api/admin/logs' },
  { title: 'Calls', icon: 'radio', match: (path) => path.startsWith('/api/calls') },
  { title: 'Password', icon: 'lock', match: (path) => path === '/api/admin/password' },
  { title: 'Users', icon: 'person', match: (path) => path.startsWith('/api/admin/user-') },
  { title: 'Capture', icon: 'cloud_upload', match: (path) => path === '/api/call-upload' || path === '/api/trunk-recorder-call-upload' },
  { title: 'Applications', icon: 'apps', match: (path) => path === '/' || path === '/admin' || path === '/admin/' },
];

const METHODS = ['get', 'post', 'put', 'delete', 'patch'];

// config endpoints form one block; the subgroup names the Config menu's resource.
function configSubgroup(path) {
  for (const resource of ['sites', 'talkgroups', 'units']) {
    if (path.endsWith(`/${resource}`)) return resource;
  }

  return 'config';
}

// --- the contract's schemas, read one level deep -------------------------------

function componentSchema(contract, name) {
  return contract?.components?.schemas?.[name];
}

// a schema is named by what it is: component, array item, oneOf, type or format.
function schemaType(schema, contract) {
  if (!schema) return 'object';

  if (schema.$ref) return schema.$ref.split('/').pop();

  if (schema.oneOf) return schema.oneOf.map((variant) => schemaType(variant, contract)).join(' / ');

  if (schema.anyOf) return schema.anyOf.map((variant) => schemaType(variant, contract)).join(' / ');

  if (schema.type === 'array') return `${schemaType(schema.items, contract)}[]`;

  if (schema.type) return schema.format ? `${schema.type} (${schema.format})` : schema.type;

  return 'object';
}

// body fields are the schema's properties through one level of references:
// array item, oneOf variants; an undefined reference shows its name unresolved.
function schemaFields(schema, contract, seen = new Set()) {
  if (!schema) return [];

  if (schema.$ref) {
    const name = schema.$ref.split('/').pop();

    if (seen.has(name)) return [];

    return schemaFields(componentSchema(contract, name), contract, seen.add(name));
  }

  if (schema.type === 'array') return schemaFields(schema.items, contract, seen);

  if (schema.oneOf || schema.anyOf) {
    const fields = new Map();

    for (const variant of schema.oneOf ?? schema.anyOf) {
      for (const field of schemaFields(variant, contract, seen)) {
        if (!fields.has(field.name)) fields.set(field.name, field);
      }
    }

    return [...fields.values()];
  }

  const required = new Set(schema.required ?? []);

  return Object.entries(schema.properties ?? {}).map(([name, field]) => ({
    name,
    type: schemaType(field, contract),
    required: required.has(name),
    description: field?.description ?? '',
  }));
}

function schemaIsUnresolved(schema, contract) {
  if (!schema) return true;

  if (schema.$ref) return componentSchema(contract, schema.$ref.split('/').pop()) === undefined;

  if (schema.type === 'array') return schemaIsUnresolved(schema.items, contract);

  if (schema.oneOf) return schema.oneOf.some((variant) => schemaIsUnresolved(variant, contract));

  return false;
}

// --- the detail of one operation ------------------------------------------------

// A path declares its parameters once; every operation under it answers them.
function endpointParameters(pathItem, operation, contract) {
  return [...(pathItem?.parameters ?? []), ...(operation?.parameters ?? [])].map((parameter) => ({
    name: parameter.name,
    in: parameter.in,
    required: parameter.required === true,
    type: schemaType(parameter.schema, contract),
    enum: parameter.schema?.enum,
    description: parameter.description ?? '',
  }));
}

function endpointRequestBody(operation, contract) {
  const content = operation?.requestBody?.content;

  if (!content) return [];

  return Object.entries(content).map(([contentType, media]) => {
    const body = {
      contentType,
      schema: schemaType(media?.schema, contract),
      fields: schemaFields(media?.schema, contract),
    };

    // only a reference the contract does not define gets the unresolved mark.
    if (schemaIsUnresolved(media?.schema, contract)) body.unresolved = true;

    return body;
  });
}

function endpointResponses(operation) {
  return Object.entries(operation?.responses ?? {}).map(([code, response]) => ({
    code,
    description: response?.description ?? '',
  }));
}

export function buildOpenApiGroups(contract) {
  const groups = [];

  for (const group of GROUPS) {
    const paths = Object.entries(contract?.paths ?? {}).filter(([path]) => group.match(path));

    if (paths.length === 0) continue;

    const endpoints = [];

    for (const [path, operations] of paths) {
      const pathItem = contract.paths[path];

      for (const method of METHODS) {
        const operation = operations[method];

        if (!operation) continue;

        endpoints.push({
          method: method.toUpperCase(),
          path,
          summary: operation.summary ?? '',
          description: operation.description ?? operation.summary ?? '',
          params: endpointParameters(pathItem, operation, contract),
          body: endpointRequestBody(operation, contract),
          responses: endpointResponses(operation),
        });
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

// every path and method of the contract, for the assertion that the section
// names all of them.
export function contractEndpoints(contract) {
  const endpoints = [];

  for (const [path, operations] of Object.entries(contract?.paths ?? {})) {
    for (const method of METHODS) {
      if (operations[method]) endpoints.push(`${method.toUpperCase()} ${path}`);
    }
  }

  return endpoints;
}
