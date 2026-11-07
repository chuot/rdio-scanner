// presentational box: the section hands it a derived group; detail rows stay
// closed by default; the box never issues requests.

class RdioAdminOpenApiBox extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const group = this.group;

    if (!group) return;

    const box = document.createElement('div');
    box.className = 'rdio-admin-openapi-box';

    const title = document.createElement('p');
    title.className = 'box-title';
    title.innerHTML = `<i class="material-icons" aria-hidden="true">${group.icon}</i><span>${group.title}</span>`;

    box.append(title);

    // The auth sentence is stated once for the whole page, in the section intro;
    // repeating it in every box is noise.
    const label = (text) => {
      const p = document.createElement('p');
      p.className = 'detail-label';
      p.textContent = text;

      return p;
    };

    const item = (nodes) => {
      const li = document.createElement('li');
      li.className = 'detail-item';

      for (const node of nodes) {
        if (node) li.append(node);
      }

      return li;
    };

    const span = (className, text) => {
      const el = document.createElement('span');
      el.className = className;
      el.textContent = text;

      return el;
    };

    const code = (className, text, attrs = {}) => {
      const el = document.createElement('code');
      el.className = className;
      el.textContent = text;

      for (const [name, value] of Object.entries(attrs)) el.setAttribute(name, value);

      return el;
    };

    const block = (className, title, items) => {
      const section = document.createElement('div');
      section.className = `detail-block ${className}`;

      const list = document.createElement('ul');

      for (const li of items) list.append(li);

      section.append(label(title), list);

      return section;
    };

    const parametersBlock = (params) =>
      block(
        'parameters',
        'Parameters',
        params.map((param) =>
          item([
            code('param-name', param.name),
            span('param-in', param.in),
            span('param-type', param.type),
            span('param-required', param.required ? 'required' : 'optional'),
            param.enum?.length ? span('param-values', param.enum.join(', ')) : null,
            param.description ? span('param-description', param.description) : null,
          ]),
        ),
      );

    const bodyBlocks = (bodies) =>
      bodies.map((body) => {
        const section = document.createElement('div');
        section.className = 'detail-block request-body';

        const meta = document.createElement('p');
        meta.className = 'body-meta';
        meta.textContent = body.unresolved
          ? `${body.contentType} · ${body.schema} — the contract does not define it`
          : `${body.contentType} · ${body.schema}`;

        section.append(label('Request body'), meta);

        if (body.fields.length) {
          const list = document.createElement('ul');

          for (const field of body.fields) {
            list.append(
              item([
                code('field-name', field.name, field.required ? { 'data-required': 'true' } : {}),
                span('field-type', field.type),
                field.description ? span('field-description', field.description) : null,
              ]),
            );
          }

          section.append(list);
        }

        return section;
      });

    const responsesBlock = (responses) =>
      block(
        'responses',
        'Responses',
        responses.map((response) => item([code('response-code', response.code), span('response-description', response.description)])),
      );

    const detailFor = (endpoint) => {
      const detail = document.createElement('div');
      detail.className = 'endpoint-detail';

      if (endpoint.description) {
        const description = document.createElement('p');
        description.className = 'endpoint-description';
        description.textContent = endpoint.description;

        detail.append(description);
      }

      if (endpoint.params?.length) detail.append(parametersBlock(endpoint.params));

      for (const section of bodyBlocks(endpoint.body ?? [])) detail.append(section);

      if (endpoint.responses?.length) detail.append(responsesBlock(endpoint.responses));

      return detail;
    };

    const renderRow = (endpoint) => {
      const row = document.createElement('details');
      row.className = 'endpoint';

      const summary = document.createElement('summary');

      const method = span(`method ${endpoint.method.toLowerCase()}`, endpoint.method);

      // a zero-width break after each / lets a long path wrap at separators on narrow
      // viewports.
      const path = code('endpoint-path', endpoint.path.replaceAll('/', '/\u200B'));

      summary.append(method, path);

      if (endpoint.summary) summary.append(span('summary', endpoint.summary));

      row.append(summary, detailFor(endpoint));

      return row;
    };

    const renderList = (endpoints) => {
      const list = document.createElement('div');
      list.className = 'endpoint-list';

      for (const endpoint of endpoints) list.append(renderRow(endpoint));

      return list;
    };

    if (group.subgroups) {
      for (const subgroup of group.subgroups) {
        const subgroupLabel = document.createElement('p');
        subgroupLabel.className = 'group-label';
        subgroupLabel.textContent = subgroup.label;

        box.append(subgroupLabel, renderList(subgroup.endpoints));
      }
    } else {
      box.append(renderList(group.endpoints));
    }

    this.replaceChildren(box);
  }
}

customElements.define('rdio-admin-openapi-box', RdioAdminOpenApiBox);
