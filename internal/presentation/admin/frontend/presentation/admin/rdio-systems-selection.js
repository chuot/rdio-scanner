// rdio-systems-selection: tri-state selection dialog
// (legacy config/systems-selection). The tree Everything → Groups / Tags /
// Systems → Talkgroups is rendered from the pure domain model; every check
// cascades through it and the tree re-renders. Result: '*' or
// [{ id: systemRef, talkgroups: '*' | [talkgroupRef...] }], or null on cancel.

import { openDialog } from '../../../../common/frontend/presentation/ui/primitives.js';
import {
  createSelectionModel,
  emitSelection,
  hydrateSelection,
  setEverything,
  setGroup,
  setSystem,
  setTag,
  setTalkgroup,
} from '../../domain/systems-selection.js';

class RdioSystemsSelection extends HTMLElement {
  setup(config, value) {
    this.model = createSelectionModel(config);

    hydrateSelection(this.model, value);

    this.render();
  }

  disconnectedCallback() {
    // Escape / backdrop close without an explicit action counts as cancel.
    this.onDismiss?.();
  }

  render() {
    const model = this.model;

    // The tree answers every activation by painting itself again — the cascade runs through the rows
    // above and below — and a rebuild throws away the control the keyboard was standing on. A browser
    // puts the focus on the page when the control it was on leaves, so the operator who checks a
    // talkgroup with the keyboard is back at the top of the dialog, and the next Tab key walks the
    // tree from its beginning. The row the operator activated is recorded before the rebuild, and the
    // keyboard is put back on that same row after it.
    const activated = document.activeElement?.dataset?.node;

    const root = document.createElement('div');
    root.className = 'rdio-systems-selection';

    root.append(
      this.checkbox({
        label: 'Everything',
        className: 'everything',
        node: 'everything',
        checked: model.all,
        indeterminate: model.indeterminate.everything,
        onChange: (checked) => {
          setEverything(model, checked);
          this.render();
        },
      }),
    );

    root.append(this.listBlock('Groups', model.groups.map((group, index) => ({
      label: group.label ?? String(group.id),
      node: `group:${group.id}`,
      checked: group.checked,
      indeterminate: model.indeterminate.groups[index],
      onChange: (checked) => {
        setGroup(model, index, checked);
        this.render();
      },
    }))));

    root.append(this.listBlock('Tags', model.tags.map((tag, index) => ({
      label: tag.label ?? String(tag.id),
      node: `tag:${tag.id}`,
      checked: tag.checked,
      indeterminate: model.indeterminate.tags[index],
      onChange: (checked) => {
        setTag(model, index, checked);
        this.render();
      },
    }))));

    const systemsBlock = document.createElement('fieldset');
    systemsBlock.className = 'tree-systems';

    const systemsLegend = document.createElement('legend');
    systemsLegend.textContent = 'Systems';
    systemsBlock.append(systemsLegend);

    model.systems.forEach((system, systemIndex) => {
      systemsBlock.append(
        this.checkbox({
          label: system.label ?? String(system.id),
          className: 'system',
          node: `system:${system.id}`,
          checked: system.all,
          indeterminate: model.indeterminate.systems[systemIndex],
          onChange: (checked) => {
            setSystem(model, systemIndex, checked);
            this.render();
          },
        }),
      );

      const talkgroups = document.createElement('div');
      talkgroups.className = 'indent';

      system.talkgroups.forEach((talkgroup, talkgroupIndex) => {
        talkgroups.append(
          this.checkbox({
            label: `${talkgroup.id} ${talkgroup.label ?? ''}`.trim(),
            node: `talkgroup:${talkgroup.id}`,
            checked: talkgroup.checked,
            onChange: (checked) => {
              setTalkgroup(model, systemIndex, talkgroupIndex, checked);
              this.render();
            },
          }),
        );
      });

      systemsBlock.append(talkgroups);
    });

    root.append(systemsBlock);

    this.replaceChildren(root);

    if (activated) root.querySelector(`[data-node="${activated}"]`)?.focus();
  }

  checkbox({ label, className = '', node, checked, indeterminate = false, onChange }) {
    const wrapper = document.createElement('label');
    wrapper.className = `rdio-tree-item ${className}`.trim();

    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.indeterminate = !!indeterminate;
    input.addEventListener('change', () => onChange(input.checked));

    // The row's own identity, carried on the control: the row is what the operator is looking at, and
    // it is what the rebuild hands the keyboard back to.
    if (node) input.setAttribute('data-node', node);

    const text = document.createElement('span');
    text.textContent = label;

    wrapper.append(input, text);

    return wrapper;
  }

  listBlock(title, items) {
    const block = document.createElement('fieldset');
    block.className = `tree-${title.toLowerCase()}`;

    const legend = document.createElement('legend');
    legend.textContent = title;
    block.append(legend);

    for (const item of items) block.append(this.checkbox(item));

    return block;
  }
}

customElements.define('rdio-systems-selection', RdioSystemsSelection);

export function openSystemsSelectionDialog({ config, value }) {
  return new Promise((resolve) => {
    let settled = false;

    const settle = (result) => {
      if (settled) return;

      settled = true;

      resolve(result);
    };

    const tree = document.createElement('rdio-systems-selection');

    tree.onDismiss = () => settle(null);

    tree.setup(config, value);

    openDialog({
      title: 'Systems and talkgroups',
      content: tree,
      actions: [
        { label: 'Cancel', onClick: () => settle(null) },
        { label: 'Select', onClick: () => settle(emitSelection(tree.model)) },
      ],
    });
  });
}
