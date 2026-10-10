// The OpenAPI entrypoint box (FEAT-018 CA-005, FEAT-024 CA-003): the OpenAPI
// section renders one box per group derived from docs/openapi.json. The box is
// presentational: the section hands it a group from domain/openapi-view.js —
// title, icon, endpoints (method, path, summary), and for Config the subgroups
// by resource. The box is informational: it never issues requests.

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
    const renderList = (endpoints) => {
      const list = document.createElement('ul');

      for (const endpoint of endpoints) {
        const item = document.createElement('li');

        const method = document.createElement('span');
        method.className = `method ${endpoint.method.toLowerCase()}`;
        method.textContent = endpoint.method;

        const code = document.createElement('code');
        code.textContent = endpoint.path;

        item.append(method, code);

        if (endpoint.summary) {
          const summary = document.createElement('span');
          summary.className = 'summary';
          summary.textContent = endpoint.summary;

          item.append(summary);
        }

        list.append(item);
      }

      return list;
    };

    if (group.subgroups) {
      for (const subgroup of group.subgroups) {
        const label = document.createElement('p');
        label.className = 'group-label';
        label.textContent = subgroup.label;

        box.append(label, renderList(subgroup.endpoints));
      }
    } else {
      box.append(renderList(group.endpoints));
    }

    this.replaceChildren(box);
  }
}

customElements.define('rdio-admin-openapi-box', RdioAdminOpenApiBox);
